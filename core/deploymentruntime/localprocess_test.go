package deploymentruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deploymentruntimev1 "github.com/le-squat/rat/gen/rat/deploymentruntime/v1"
	statev1 "github.com/le-squat/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func minProfile() *deploymentruntimev1.IsolationProfile {
	return &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}
}

// buildStatePlugin compiles the standalone state plugin into a temp binary.
func buildStatePlugin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "stateplugin")
	cmd := exec.Command("go", "build", "-o", bin, "./testplugins/stateplugin")
	cmd.Dir = ".." // the core module root (this test runs in core/deploymentruntime)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stateplugin: %v\n%s", err, out)
	}
	return bin
}

// TestLaunchLifecycleRunsInChildProcess: launch a plugin binary, health-check it,
// dial + call it, and prove the work ran in a DISTINCT OS process — then terminate.
func TestLaunchLifecycleRunsInChildProcess(t *testing.T) {
	bin := buildStatePlugin(t)
	rt := NewLocalProcess()
	ctx := context.Background()

	lr, err := rt.Launch(ctx, &deploymentruntimev1.LaunchRequest{
		PluginId: "rat-test-state",
		Spec:     &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: minProfile()},
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() { _, _ = rt.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: lr.GetInstanceId()}) })

	// Health-check until HEALTHY (bounded).
	deadline := time.Now().Add(5 * time.Second)
	for {
		hc, err := rt.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: lr.GetInstanceId()})
		if err != nil {
			t.Fatalf("Healthcheck: %v", err)
		}
		if hc.GetStatus() == deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin never became healthy: %s", hc.GetDetail())
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Dial the launched plugin's endpoint and call it.
	conn, err := grpc.NewClient(lr.GetEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", lr.GetEndpoint(), err)
	}
	defer conn.Close()
	getCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := statev1.NewStateServiceClient(conn).Get(getCtx, &statev1.GetRequest{Key: "k1"})
	if err != nil {
		t.Fatalf("Get via launched plugin: %v", err)
	}

	// The plugin tags its response with its own PID — prove it's a DIFFERENT process.
	val := string(resp.GetValue())
	if !strings.Contains(val, "key=k1") {
		t.Errorf("unexpected response %q", val)
	}
	var childPID int
	if _, err := fmt.Sscanf(val, "pid=%d", &childPID); err != nil {
		t.Fatalf("could not parse pid from %q: %v", val, err)
	}
	if childPID == 0 || childPID == os.Getpid() {
		t.Errorf("plugin ran in PID %d (test PID %d) — expected a distinct child process", childPID, os.Getpid())
	}

	// Terminate, then the instance is gone.
	tr, err := rt.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: lr.GetInstanceId()})
	if err != nil || !tr.GetTerminated() {
		t.Fatalf("Terminate: err=%v terminated=%v", err, tr.GetTerminated())
	}
	if _, err := rt.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: lr.GetInstanceId()}); status.Code(err) != codes.NotFound {
		t.Errorf("Healthcheck after Terminate = %v, want NotFound", status.Code(err))
	}
}

// TestLaunchRefusesBelowI9Minimum: the I9 gate — a profile missing any of
// non-root / cap-drop / no-new-privs is refused (the trust boundary, not a nicety).
func TestLaunchRefusesBelowI9Minimum(t *testing.T) {
	rt := NewLocalProcess()
	_, err := rt.Launch(context.Background(), &deploymentruntimev1.LaunchRequest{
		PluginId: "x",
		// run_as_non_root only — missing drop_all_capabilities + no_new_privileges.
		Spec: &deploymentruntimev1.LaunchSpec{Image: "/bin/true", Isolation: &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true}},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Launch below the I9 minimum = %v, want FailedPrecondition", status.Code(err))
	}
}

// TestLaunchRejectsEmptyImage: no binary to exec → INVALID_ARGUMENT.
func TestLaunchRejectsEmptyImage(t *testing.T) {
	rt := NewLocalProcess()
	_, err := rt.Launch(context.Background(), &deploymentruntimev1.LaunchRequest{
		PluginId: "x",
		Spec:     &deploymentruntimev1.LaunchSpec{Image: "", Isolation: minProfile()},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Launch with empty image = %v, want InvalidArgument", status.Code(err))
	}
}
