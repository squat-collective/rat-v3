package deploymentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// podmanContainerPort is the fixed in-container port a launched plugin binds (via
// RAT_PLUGIN_ADDR); the runtime publishes it to an ephemeral host port and returns
// that as the dialable endpoint.
const podmanContainerPort = "50051"

// podmanNonRootUser is the non-root uid:gid the container process runs as
// (run_as_non_root, actually enforced — not merely asserted).
const podmanNonRootUser = "1000:1000"

type podmanInstance struct {
	containerID string
	endpoint    string
	receipt     isolationReceipt
}

// Podman is a `kind: deployment-runtime` plugin that runs each launched plugin as an
// isolated CONTAINER via rootless podman. Unlike LocalProcess (which honors only the
// process-level I9 subset), Podman ENFORCES the FULL I9 profile at the kernel level —
// run_as_non_root, drop_all_capabilities, no_new_privileges, read_only_root_fs,
// block_metadata_egress, seccomp — which is exactly the reviews/08 D1 honesty gap the
// v1 references could only self-attest. Implements the frozen DeploymentRuntimeService;
// D1 completes when this passes a full-profile isolation vector (ADR-016 §4).
type Podman struct {
	deploymentruntimev1.UnimplementedDeploymentRuntimeServiceServer
	bin string // the podman binary (default "podman"; overridable for tests)

	// DataRoot, when set, gives each launched plugin a PERSISTENT host directory
	// (<DataRoot>/<plugin_id>) mounted at /data — surviving Terminate+relaunch, so a
	// stateful plugin's durable state (e.g. a SQLite ledger) outlives a backend crash
	// (C1). Empty == ephemeral only (/tmp tmpfs). A persistent peer to the tmpfs scratch.
	DataRoot string

	mu        sync.Mutex
	seq       int
	instances map[string]*podmanInstance
}

// NewPodman returns a runtime that shells out to the `podman` CLI.
func NewPodman() *Podman {
	return &Podman{bin: "podman", instances: map[string]*podmanInstance{}}
}

// Launch starts spec.image as an isolated container under the FULL I9 profile and
// returns the host endpoint the core dials. It refuses (shared gate) below the I9
// minimum. The container binds RAT_PLUGIN_ADDR inside; podman publishes it to a free
// loopback host port.
func (r *Podman) Launch(ctx context.Context, req *deploymentruntimev1.LaunchRequest) (*deploymentruntimev1.LaunchResponse, error) {
	spec := req.GetSpec()
	if err := checkI9Minimum(spec); err != nil {
		return nil, err
	}
	iso := spec.GetIsolation()

	// The I9 profile mapped 1:1 onto podman's real enforcement surface.
	args := []string{
		"run", "-d",
		"--user", podmanNonRootUser, // run_as_non_root  → uid != 0 inside
		"--cap-drop=ALL",                   // drop_all_capabilities → CapEff == 0
		"--security-opt=no-new-privileges", // no_new_privileges → NoNewPrivs == 1
		"--read-only",                      // read_only_root_fs → writes to / fail (EROFS)
		// read-only root + a writable /tmp tmpfs is the canonical hardened pattern: the
		// root fs stays immutable, but a stateful plugin still gets ephemeral scratch
		// (e.g. a SQLite WAL db at /tmp). nosuid,nodev keep the scratch unprivileged.
		"--tmpfs", "/tmp:rw,nosuid,nodev",
		// block_metadata_egress: force the container onto a PRIVATE bridge network with
		// its OWN netns — explicitly, never inheriting a host-network default (some
		// nested/CI environments default --network to host, which would both defeat
		// metadata isolation AND break port publishing). On a private netns the host
		// link-local metadata endpoint (169.254.169.254) is not reachable from inside —
		// verified empirically by the prober. An explicit egress drop for
		// host-network/CNI deployments is the GA refinement; the netns is the D1 control.
		"--network=bridge",
		"-p", "127.0.0.1::" + podmanContainerPort, // publish to an ephemeral host port
		"-e", "RAT_PLUGIN_ADDR=0.0.0.0:" + podmanContainerPort,
	}
	// Persistent state (C1): when DataRoot is set, mount a per-plugin host directory at
	// /data — it survives Terminate+relaunch, so a stateful plugin's durable state (e.g.
	// a SQLite ledger) outlives a backend crash. 0777 (forced past umask) so the non-root
	// container uid can write it; :Z relabels for SELinux. A production runtime would map
	// ownership to the plugin's uid instead of world-writable.
	if r.DataRoot != "" {
		dir := filepath.Join(r.DataRoot, req.GetPluginId())
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return nil, status.Errorf(codes.Internal, "create data dir %s: %v", dir, err)
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			return nil, status.Errorf(codes.Internal, "chmod data dir %s: %v", dir, err)
		}
		args = append(args, "-v", dir+":/data:Z")
	}
	// seccomp: empty / "RuntimeDefault" → podman's default profile (RuntimeDefault-
	// equivalent) already applies; a named profile is passed through.
	if s := iso.GetSeccompProfile(); s != "" && s != "RuntimeDefault" {
		args = append(args, "--security-opt=seccomp="+s)
	}
	for k, v := range spec.GetEnv() {
		args = append(args, "-e", k+"="+v) // NEVER secrets (secret-backend axis)
	}
	args = append(args, spec.GetImage())

	out, err := r.podman(ctx, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "podman run %q: %v: %s", spec.GetImage(), err, out)
	}
	cid := strings.TrimSpace(out)

	endpoint, err := r.publishedEndpoint(ctx, cid)
	if err != nil {
		_, _ = r.podman(ctx, "rm", "-f", cid) // don't leak a container we can't reach
		return nil, status.Errorf(codes.Internal, "resolve endpoint for %s: %v", cid, err)
	}

	receipt := isolationReceipt{
		Kind: "podman",
		IsolationHonored: honoredProfile{
			RunAsNonRoot:        true,
			DropAllCapabilities: true,
			NoNewPrivileges:     true,
			ReadOnlyRootFs:      true,
			BlockMetadataEgress: true,
		},
		SeccompProfile: effectiveSeccomp(iso.GetSeccompProfile()),
	}

	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("podman-%d", r.seq)
	r.instances[id] = &podmanInstance{containerID: cid, endpoint: endpoint, receipt: receipt}
	r.mu.Unlock()

	return &deploymentruntimev1.LaunchResponse{InstanceId: id, Endpoint: endpoint}, nil
}

// Healthcheck reports HEALTHY when the container is running AND its endpoint accepts a
// connection (readiness). The detail carries the structured isolation receipt
// (CONTRACT.md) — what this runtime actually enforced. Unknown instance → NotFound
// (matching LocalProcess's Go convention).
func (r *Podman) Healthcheck(ctx context.Context, req *deploymentruntimev1.HealthcheckRequest) (*deploymentruntimev1.HealthcheckResponse, error) {
	r.mu.Lock()
	inst, ok := r.instances[req.GetInstanceId()]
	r.mu.Unlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown instance %q", req.GetInstanceId())
	}
	detail, _ := json.Marshal(inst.receipt)
	running, err := r.containerRunning(ctx, inst.containerID)
	if err != nil {
		return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNKNOWN, Detail: fmt.Sprintf("inspect failed: %v", err)}, nil
	}
	if !running {
		return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY, Detail: string(detail)}, nil
	}
	if !endpointAccepts(inst.endpoint) {
		return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNKNOWN, Detail: "container up, endpoint not yet accepting: " + string(detail)}, nil
	}
	return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY, Detail: string(detail)}, nil
}

// Terminate force-removes the container. Unknown instance → NotFound (matching
// LocalProcess's Go convention).
func (r *Podman) Terminate(ctx context.Context, req *deploymentruntimev1.TerminateRequest) (*deploymentruntimev1.TerminateResponse, error) {
	r.mu.Lock()
	inst, ok := r.instances[req.GetInstanceId()]
	if ok {
		delete(r.instances, req.GetInstanceId())
	}
	r.mu.Unlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown instance %q", req.GetInstanceId())
	}
	// -t 0: terminate now (skip the SIGTERM grace before SIGKILL) — a slow-to-SIGTERM
	// plugin (e.g. a Python gRPC server) must not make Terminate block for 10s.
	if out, err := r.podman(ctx, "rm", "-f", "-t", "0", inst.containerID); err != nil {
		return nil, status.Errorf(codes.Internal, "podman rm %s: %v: %s", inst.containerID, err, out)
	}
	return &deploymentruntimev1.TerminateResponse{Terminated: true}, nil
}

// publishedEndpoint resolves the ephemeral host port podman assigned to the
// container's published port, as a dialable "127.0.0.1:<port>".
func (r *Podman) publishedEndpoint(ctx context.Context, cid string) (string, error) {
	format := `{{ (index (index .NetworkSettings.Ports "` + podmanContainerPort + `/tcp") 0).HostPort }}`
	out, err := r.podman(ctx, "inspect", "--format", format, cid)
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	port := strings.TrimSpace(out)
	if port == "" || port == "<no value>" {
		return "", fmt.Errorf("no published host port for %s/tcp", podmanContainerPort)
	}
	return "127.0.0.1:" + port, nil
}

func (r *Podman) containerRunning(ctx context.Context, cid string) (bool, error) {
	out, err := r.podman(ctx, "inspect", "--format", "{{.State.Running}}", cid)
	if err != nil {
		return false, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out) == "true", nil
}

// podman runs the podman CLI, returning combined output (for diagnostics on error).
func (r *Podman) podman(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// effectiveSeccomp reports the profile actually in force: an empty request means
// podman's default profile (RuntimeDefault-equivalent) applies.
func effectiveSeccomp(requested string) string {
	if requested == "" {
		return "RuntimeDefault"
	}
	return requested
}
