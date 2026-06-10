package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/squat-collective/rat-v3/core/lease"
	"github.com/squat-collective/rat-v3/core/reconciler"
	"github.com/squat-collective/rat-v3/core/registry"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/deploymentruntime/v1"
)

// fakeCtlRuntime is a deployment-runtime stub: every launched instance reports `status`.
type fakeCtlRuntime struct {
	deploymentruntimev1.UnimplementedDeploymentRuntimeServiceServer
	mu                   sync.Mutex
	status               deploymentruntimev1.HealthStatus
	launches, terminates int
	seq                  int
}

func (f *fakeCtlRuntime) Launch(_ context.Context, req *deploymentruntimev1.LaunchRequest) (*deploymentruntimev1.LaunchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches++
	f.seq++
	return &deploymentruntimev1.LaunchResponse{InstanceId: fmt.Sprintf("inst-%d", f.seq), Endpoint: fmt.Sprintf("127.0.0.1:91%02d", f.seq)}, nil
}
func (f *fakeCtlRuntime) Healthcheck(_ context.Context, _ *deploymentruntimev1.HealthcheckRequest) (*deploymentruntimev1.HealthcheckResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &deploymentruntimev1.HealthcheckResponse{Status: f.status}, nil
}
func (f *fakeCtlRuntime) Terminate(_ context.Context, _ *deploymentruntimev1.TerminateRequest) (*deploymentruntimev1.TerminateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminates++
	return &deploymentruntimev1.TerminateResponse{Terminated: true}, nil
}
func (f *fakeCtlRuntime) terms() int { f.mu.Lock(); defer f.mu.Unlock(); return f.terminates }

// driveLoop ticks the reconciler until ctx is cancelled (stands in for reconciler.Loop).
func driveLoop(ctx context.Context, rec *reconciler.Reconciler) {
	tk := time.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			rec.Reconcile(context.Background(), time.Now())
		}
	}
}

const engManifestYAML = `api_version: rat.dev/v1
kind: engine
metadata:
  name: eng
  version: "0.1"
provides:
  - capability: rat://engine/v1/execute
`

// TestControlServiceLive: a plugin registered against a RUNNING daemon (here: a live
// registry + reconcile loop) launches, becomes Healthy, is authorizable + listed, and
// Deregister terminates + retracts it — the ADR-027 control path end to end (fake runtime).
func TestControlServiceLive(t *testing.T) {
	rt := &fakeCtlRuntime{status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY}
	reg, _ := registry.New(nil)
	rec := reconciler.New(rt, nil, reconciler.Config{
		BaseBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		CrashLoopCap: 5, ReadinessTimeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go driveLoop(ctx, rec)
	ctl := &controlService{reg: reg, rec: rec, gwAddr: "127.0.0.1:0", readyTO: 2 * time.Second}

	// before: nothing
	if lp, _ := ctl.ListPlugins(ctx, &corev1.ListPluginsRequest{}); len(lp.GetPlugins()) != 0 {
		t.Fatalf("empty plane should list 0 plugins, got %d", len(lp.GetPlugins()))
	}

	// REGISTER live
	resp, err := ctl.RegisterPlugin(ctx, &corev1.RegisterPluginRequest{
		Name: "eng", ManifestYaml: []byte(engManifestYAML),
		Launch: &corev1.LaunchSpec{Image: "img", Isolation: "i9"},
	})
	if err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if resp.GetState() != "Healthy" || resp.GetEndpoint() == "" {
		t.Fatalf("register resp = %+v, want Healthy with an endpoint", resp)
	}
	// the live registry now authorizes a caller of this capability
	if reg.ProviderOf("rat://engine/v1/execute") != "eng" {
		t.Fatal("registry did not learn the live-registered provider")
	}
	// ListPlugins reflects it
	lp, _ := ctl.ListPlugins(ctx, &corev1.ListPluginsRequest{})
	if len(lp.GetPlugins()) != 1 || lp.GetPlugins()[0].GetName() != "eng" || lp.GetPlugins()[0].GetState() != "Healthy" {
		t.Fatalf("ListPlugins = %+v, want [eng Healthy]", lp.GetPlugins())
	}

	// re-register is idempotent (a refresh, not an error)
	if _, err := ctl.RegisterPlugin(ctx, &corev1.RegisterPluginRequest{
		Name: "eng", ManifestYaml: []byte(engManifestYAML), Launch: &corev1.LaunchSpec{Image: "img"},
	}); err != nil {
		t.Fatalf("idempotent re-register should succeed: %v", err)
	}

	// DEREGISTER live
	dr, err := ctl.DeregisterPlugin(ctx, &corev1.DeregisterPluginRequest{Name: "eng"})
	if err != nil || !dr.GetWasPresent() {
		t.Fatalf("Deregister = %+v, err=%v, want was_present", dr, err)
	}
	if reg.ProviderOf("rat://engine/v1/execute") != "" {
		t.Fatal("capability not retracted after deregister")
	}
	if rt.terms() == 0 {
		t.Fatal("instance was not terminated on deregister")
	}
	if lp, _ := ctl.ListPlugins(ctx, &corev1.ListPluginsRequest{}); len(lp.GetPlugins()) != 0 {
		t.Fatalf("plane should be empty after deregister, got %d", len(lp.GetPlugins()))
	}
}

// TestControlServiceRealLoop: the same as TestControlServiceLive but driven by the REAL
// reconciler.Loop + lease.Elector (the daemon's actual loop), booting with an EMPTY initial
// desired set — the exact shape of a daemon started with only a driver. Isolates whether the
// loop+elector converge a LIVE-added plugin (vs the manual-tick driveLoop).
func TestControlServiceRealLoop(t *testing.T) {
	rt := &fakeCtlRuntime{status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY}
	reg, _ := registry.New(nil)
	rec := reconciler.New(rt, nil, reconciler.Config{
		BaseBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		CrashLoopCap: 5, ReadinessTimeout: time.Second,
	})
	loop := &reconciler.Loop{
		Elector:    lease.NewElector("test", lease.NewStore(), 10*time.Second),
		Reconciler: rec,
		Tick:       20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)
	ctl := &controlService{reg: reg, rec: rec, gwAddr: "127.0.0.1:0", readyTO: 2 * time.Second}

	resp, err := ctl.RegisterPlugin(ctx, &corev1.RegisterPluginRequest{
		Name: "eng", ManifestYaml: []byte(engManifestYAML),
		Launch: &corev1.LaunchSpec{Image: "img", Isolation: "i9"},
	})
	if err != nil {
		t.Fatalf("RegisterPlugin via real loop+elector: %v", err)
	}
	if resp.GetState() != "Healthy" {
		t.Fatalf("state=%s, want Healthy", resp.GetState())
	}
}

// TestControlServiceRollback: a plugin that never becomes Healthy is rolled back — the
// registry + desired set are left clean (no partial state), the ADR-027 failure guarantee.
func TestControlServiceRollback(t *testing.T) {
	rt := &fakeCtlRuntime{status: deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY}
	reg, _ := registry.New(nil)
	rec := reconciler.New(rt, nil, reconciler.Config{
		BaseBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond,
		CrashLoopCap: 2, ReadinessTimeout: 50 * time.Millisecond, // → Degraded fast
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go driveLoop(ctx, rec)
	ctl := &controlService{reg: reg, rec: rec, gwAddr: "127.0.0.1:0", readyTO: time.Second}

	_, err := ctl.RegisterPlugin(ctx, &corev1.RegisterPluginRequest{
		Name: "eng", ManifestYaml: []byte(engManifestYAML),
		Launch: &corev1.LaunchSpec{Image: "img", Isolation: "i9"},
	})
	if err == nil {
		t.Fatal("RegisterPlugin should FAIL for a plugin that never becomes healthy")
	}
	// rolled back: no registry entry, no desired entry
	if reg.Plugin("eng") != nil {
		t.Fatal("failed register left a registry entry (no rollback)")
	}
	if names := rec.DesiredNames(); len(names) != 0 {
		t.Fatalf("failed register left a desired entry: %v", names)
	}
}
