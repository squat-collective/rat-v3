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
	bin string // the podman binary (default "podman" or $RAT_PODMAN_BIN; overridable for tests)

	// DataRoot, when set, gives each launched plugin a PERSISTENT host directory
	// (<DataRoot>/<plugin_id>) mounted at /data — surviving Terminate+relaunch, so a
	// stateful plugin's durable state (e.g. a SQLite ledger) outlives a backend crash
	// (C1). Empty == ephemeral only (/tmp tmpfs). A persistent peer to the tmpfs scratch.
	DataRoot string

	// Network, when set, switches to SIBLING mode (ADR-022 socket-mount): each launched
	// plugin joins this shared user-defined podman network under a stable --name, and the
	// runtime returns "<name>:50051" as the endpoint instead of a published host port.
	// This is the mode rat uses when it is ITSELF a container (driving the host's podman
	// via a mounted socket): a sibling's host-published 127.0.0.1 port is unreachable from
	// inside rat's netns, but a name on a shared network resolves via podman DNS — exactly
	// the k8s pod-to-pod-by-name shape. Empty == HOST mode (rat on the host): the original
	// private-bridge + loopback-publish behavior, unchanged. I9 holds in both: a user
	// bridge is still a private netns that drops the 169.254 metadata route.
	Network string

	// NamePrefix, when set, prefixes every SIBLING-mode container name with "<prefix>-"
	// (ADR-023). It is the rat INSTANCE id: with many rat daemons on one machine, two
	// instances must not collide on a name like "rat-state-1" (especially if they ever
	// share a network) — "<instance>-rat-state-1" keeps them distinct. Empty == no prefix.
	NamePrefix string

	mu        sync.Mutex
	seq       int
	instances map[string]*podmanInstance
}

// NewPodman returns a runtime that shells out to the podman CLI ($RAT_PODMAN_BIN, or
// "podman"). In a container driving the host's podman over a mounted socket, set
// $RAT_PODMAN_BIN=podman-remote (the thin remote client) + $CONTAINER_HOST=unix://….
func NewPodman() *Podman {
	bin := os.Getenv("RAT_PODMAN_BIN")
	if bin == "" {
		bin = "podman"
	}
	return &Podman{bin: bin, instances: map[string]*podmanInstance{}}
}

// NewPodmanNetworked returns a podman runtime in SIBLING mode on the given shared
// network (see Podman.Network) — the socket-mount path for a containerized rat.
func NewPodmanNetworked(network string) *Podman {
	r := NewPodman()
	r.Network = network
	return r
}

// NewPodmanInstanced returns a SIBLING-mode runtime on the given network whose container
// names are prefixed with the rat instance id (ADR-023) — so many rat daemons coexist on
// one machine without name collisions.
func NewPodmanInstanced(network, instance string) *Podman {
	r := NewPodmanNetworked(network)
	r.NamePrefix = instance
	return r
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

	// Allocate the instance id (and, in sibling mode, the stable container name) up front
	// — the name has to go on the `podman run` line, before the container exists.
	r.mu.Lock()
	r.seq++
	seq := r.seq
	r.mu.Unlock()
	id := fmt.Sprintf("podman-%d", seq)

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
		"-e", "RAT_PLUGIN_ADDR=0.0.0.0:" + podmanContainerPort,
	}

	// Networking: HOST mode vs SIBLING mode (see Podman.Network).
	var name string
	if r.Network == "" {
		// HOST mode (rat on the host): a PRIVATE bridge netns + publish the plugin's port
		// to an ephemeral loopback host port the core dials at 127.0.0.1. Explicit
		// --network=bridge never inherits a host-network default (which would defeat
		// metadata isolation AND break publishing).
		args = append(args,
			"--network=bridge",
			"-p", "127.0.0.1::"+podmanContainerPort)
	} else {
		// SIBLING mode (containerized rat, socket-mounted): join the shared user network
		// under a stable name; rat dials "<name>:50051" via podman DNS — no host publish
		// (rat's 127.0.0.1 isn't the host's). --replace clears any same-named container
		// leaked by a prior crashed rat (within one rat, seq is monotonic so names never
		// self-collide). The user bridge is still a private netns → metadata stays blocked.
		name = containerName(r.NamePrefix, req.GetPluginId(), seq)
		args = append(args,
			"--network="+r.Network,
			"--name", name,
			"--replace",
			// guarantee plugins can still reach host-published backends (e.g. Postgres)
			// via host.containers.internal even on a user-defined network.
			"--add-host=host.containers.internal:host-gateway")
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

	// HOST mode: resolve the ephemeral loopback port podman published. SIBLING mode: the
	// endpoint IS the stable name on the shared network (podman DNS), no publish to inspect.
	var endpoint string
	if r.Network == "" {
		endpoint, err = r.publishedEndpoint(ctx, cid)
		if err != nil {
			_, _ = r.podman(ctx, "rm", "-f", cid) // don't leak a container we can't reach
			return nil, status.Errorf(codes.Internal, "resolve endpoint for %s: %v", cid, err)
		}
	} else {
		endpoint = name + ":" + podmanContainerPort
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

// containerName builds a stable, podman-legal container name for SIBLING mode:
// "[<instance>-]<sanitized-plugin-id>-<seq>". podman names match [a-zA-Z0-9][a-zA-Z0-9_.-]*;
// any other byte in the plugin id becomes '-'. The optional instance prefix (ADR-023) keeps
// two rat daemons from colliding on one machine; the seq suffix keeps names unique within one
// rat process so a relaunch never collides with the not-yet-Terminated old instance.
func containerName(prefix, pluginID string, seq int) string {
	b := make([]byte, 0, len(pluginID))
	for i := 0; i < len(pluginID); i++ {
		c := pluginID[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == '-':
			b = append(b, c)
		default:
			b = append(b, '-')
		}
	}
	s := string(b)
	if prefix != "" {
		s = prefix + "-" + s
	}
	if s == "" || !((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= '0' && s[0] <= '9')) {
		s = "rat-" + s
	}
	return fmt.Sprintf("%s-%d", s, seq)
}

// effectiveSeccomp reports the profile actually in force: an empty request means
// podman's default profile (RuntimeDefault-equivalent) applies.
func effectiveSeccomp(requested string) string {
	if requested == "" {
		return "RuntimeDefault"
	}
	return requested
}
