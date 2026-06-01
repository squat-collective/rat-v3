// Package deploymentruntime implements deployment-runtime axis plugins for the
// spike core (ADR-016). LocalProcess is the first real (non-dry-run) runtime: it
// launches a plugin as an isolated child OS process and enforces the I9 minimum.
package deploymentruntime

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type instance struct {
	cmd      *exec.Cmd
	endpoint string
	exited   bool
}

// LocalProcess is a `kind: deployment-runtime` plugin that runs each launched
// plugin as a child OS process (real PID isolation — the `chmod +x ./rat`
// runtime). It enforces the PROCESS-level subset of the I9 isolation profile; the
// full profile (read-only-fs, metadata-egress, seccomp) is the podman runtime's
// job (ADR-016 §4). Implements the frozen DeploymentRuntimeService.
type LocalProcess struct {
	deploymentruntimev1.UnimplementedDeploymentRuntimeServiceServer
	mu        sync.Mutex
	seq       int
	instances map[string]*instance
}

// NewLocalProcess returns an empty runtime.
func NewLocalProcess() *LocalProcess {
	return &LocalProcess{instances: map[string]*instance{}}
}

// Launch execs LaunchSpec.image (a plugin binary) as a child process — after
// enforcing the I9 minimum — and returns the address the core dials. The child
// binds RAT_PLUGIN_ADDR (a free loopback port the runtime allocates).
func (r *LocalProcess) Launch(_ context.Context, req *deploymentruntimev1.LaunchRequest) (*deploymentruntimev1.LaunchResponse, error) {
	spec := req.GetSpec()
	if err := checkI9Minimum(spec); err != nil {
		return nil, err // shared trust gate: image required + the I9 minimum (isolation.go)
	}
	// run_as_non_root honoring: a local-process runtime runs the child as its OWN
	// uid (it cannot drop to another uid without privileges). If the runtime itself
	// is root it cannot honor the profile, so it refuses rather than lie.
	if os.Geteuid() == 0 {
		return nil, status.Error(codes.FailedPrecondition,
			"I9: local-process runtime is running as root and cannot honor run_as_non_root (use a container runtime)")
	}

	addr, err := freeLoopbackAddr()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "allocate endpoint: %v", err)
	}

	cmd := exec.Command(spec.GetImage())
	cmd.Env = childEnv(addr, spec.GetEnv())
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr         // surface plugin logs on the runtime's stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group → clean group-kill on Terminate

	if err := cmd.Start(); err != nil {
		return nil, status.Errorf(codes.Internal, "launch %q: %v", spec.GetImage(), err)
	}

	inst := &instance{cmd: cmd, endpoint: addr}
	// Reap the child + track exit so Healthcheck stays accurate (no zombies).
	go func() {
		_ = cmd.Wait()
		r.mu.Lock()
		inst.exited = true
		r.mu.Unlock()
	}()

	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("inst-%d", r.seq)
	r.instances[id] = inst
	r.mu.Unlock()

	return &deploymentruntimev1.LaunchResponse{InstanceId: id, Endpoint: addr}, nil
}

// Healthcheck reports HEALTHY when the child is alive AND its endpoint accepts a
// connection (readiness). The detail records the honored (process-level) profile.
func (r *LocalProcess) Healthcheck(_ context.Context, req *deploymentruntimev1.HealthcheckRequest) (*deploymentruntimev1.HealthcheckResponse, error) {
	r.mu.Lock()
	inst, ok := r.instances[req.GetInstanceId()]
	exited := ok && inst.exited
	r.mu.Unlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown instance %q", req.GetInstanceId())
	}
	if exited {
		return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY, Detail: "process exited"}, nil
	}
	if !endpointAccepts(inst.endpoint) {
		return &deploymentruntimev1.HealthcheckResponse{Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNKNOWN, Detail: "process up, endpoint not yet accepting"}, nil
	}
	return &deploymentruntimev1.HealthcheckResponse{
		Status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY,
		Detail: fmt.Sprintf("isolation honored (process-level): non-root euid=%d, own process group; cap-drop/no-new-privs requested — full profile (read-only-fs/metadata-egress/seccomp) needs a container runtime", os.Geteuid()),
	}, nil
}

// Terminate kills the child's process group and reaps it.
func (r *LocalProcess) Terminate(_ context.Context, req *deploymentruntimev1.TerminateRequest) (*deploymentruntimev1.TerminateResponse, error) {
	r.mu.Lock()
	inst, ok := r.instances[req.GetInstanceId()]
	if ok {
		delete(r.instances, req.GetInstanceId())
	}
	r.mu.Unlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown instance %q", req.GetInstanceId())
	}
	if inst.cmd.Process != nil {
		_ = syscall.Kill(-inst.cmd.Process.Pid, syscall.SIGKILL) // the child is its group leader (Setpgid)
		_ = inst.cmd.Process.Kill()                              // fallback for the leader itself
	}
	return &deploymentruntimev1.TerminateResponse{Terminated: true}, nil
}

func freeLoopbackAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func endpointAccepts(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// childEnv is the minimal environment a launched plugin gets: its endpoint + any
// manifest-declared env. NEVER secrets (those come via the secret-backend axis).
func childEnv(addr string, extra map[string]string) []string {
	env := []string{"RAT_PLUGIN_ADDR=" + addr}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
