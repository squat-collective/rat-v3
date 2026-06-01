package reconciler

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/lease"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
)

// buildBin compiles core/testplugins/<pkg> to a temp binary the local-process runtime
// can launch.
func buildBin(t *testing.T, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), pkg)
	cmd := exec.Command("go", "build", "-o", bin, "./testplugins/"+pkg)
	cmd.Dir = ".." // the core module root (this test runs in core/reconciler)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

// TestReconcilerDrivesRealRuntime: the real Loop, over the real local-process runtime,
// converges a MIX — a healthy plugin reaches Healthy, while a genuinely crash-looping
// plugin is restarted with real backoff and capped at Degraded (it never pins the
// loop). The end-to-end sre#4 proof against real launched processes.
func TestReconcilerDrivesRealRuntime(t *testing.T) {
	crashBin := buildBin(t, "crashplugin")
	stateBin := buildBin(t, "stateplugin")

	rt := deploymentruntime.NewLocalProcess()
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}
	desired := []Desired{
		{Name: "crash", Launch: &deploymentruntimev1.LaunchSpec{Image: crashBin, Isolation: iso}},
		{Name: "state", Launch: &deploymentruntimev1.LaunchSpec{Image: stateBin, Isolation: iso}},
	}
	r := New(rt, desired, Config{
		BaseBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond,
		CrashLoopCap: 4, ReadinessTimeout: 3 * time.Second,
	})
	loop := &Loop{Elector: lease.NewElector("solo", lease.NewStore(), 5*time.Second), Reconciler: r, Tick: 20 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; r.Shutdown(context.Background()) })

	waitState := func(name string, want State) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if st, _, _ := r.Status(name); st == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		st, att, _ := r.Status(name)
		t.Fatalf("%q did not reach %s within 15s (state=%s attempts=%d)", name, want, st, att)
	}

	// The healthy plugin converges; the crashing one backs off and is capped at Degraded.
	waitState("state", Healthy)
	waitState("crash", Degraded)

	if r.Endpoint("state") == "" {
		t.Error("converged plugin has no endpoint")
	}
	// Degraded is terminal-until-reset: it stays Degraded (the loop isn't hammering it).
	time.Sleep(300 * time.Millisecond)
	if st, _, _ := r.Status("crash"); st != Degraded {
		t.Errorf("crash plugin left Degraded (now %s)", st)
	}
}
