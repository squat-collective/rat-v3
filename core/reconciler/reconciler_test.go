package reconciler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	deploymentruntimev1 "github.com/squat-collective/rat-v3/gen/rat/deploymentruntime/v1"
)

// fakeRuntime is a controllable deployment-runtime: Healthcheck returns whatever
// status the test sets, and Launch can be made to fail — so the reconciler's state
// machine + backoff schedule are exercised deterministically (real process timing is
// not).
type fakeRuntime struct {
	deploymentruntimev1.UnimplementedDeploymentRuntimeServiceServer
	mu                   sync.Mutex
	status               deploymentruntimev1.HealthStatus
	launchErr            error
	launches, terminates int
	seq                  int
}

func (f *fakeRuntime) set(s deploymentruntimev1.HealthStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *fakeRuntime) Launch(_ context.Context, req *deploymentruntimev1.LaunchRequest) (*deploymentruntimev1.LaunchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.launchErr != nil {
		return nil, f.launchErr
	}
	f.launches++
	f.seq++
	// Seq-based endpoint so a relaunch yields a DISTINCT address — the case the gateway
	// re-bind must follow (a crashed plugin comes back on a new endpoint).
	return &deploymentruntimev1.LaunchResponse{InstanceId: fmt.Sprintf("inst-%d", f.seq), Endpoint: fmt.Sprintf("127.0.0.1:90%02d", f.seq)}, nil
}

func (f *fakeRuntime) Healthcheck(_ context.Context, _ *deploymentruntimev1.HealthcheckRequest) (*deploymentruntimev1.HealthcheckResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &deploymentruntimev1.HealthcheckResponse{Status: f.status}, nil
}

func (f *fakeRuntime) Terminate(_ context.Context, _ *deploymentruntimev1.TerminateRequest) (*deploymentruntimev1.TerminateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminates++
	return &deploymentruntimev1.TerminateResponse{Terminated: true}, nil
}

func (f *fakeRuntime) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches, f.terminates
}

const (
	unhealthy = deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY
	healthy   = deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY
	unknown   = deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNKNOWN
)

func testCfg() Config {
	return Config{BaseBackoff: time.Second, MaxBackoff: 4 * time.Second, CrashLoopCap: 5, ReadinessTimeout: time.Hour}
}

func desiredP() []Desired {
	return []Desired{{Name: "P", Launch: &deploymentruntimev1.LaunchSpec{Image: "img"}}}
}

func sec(base time.Time, s int) time.Time { return base.Add(time.Duration(s) * time.Second) }

// rewireSpy records the reconciler's Bind/Unbind calls (the seam the daemon wires to
// gateway.SetProvider/RemoveProvider).
type rewireSpy struct {
	mu    sync.Mutex
	calls []string
}

func (s *rewireSpy) Bind(name, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "bind:"+name+"@"+endpoint)
}

func (s *rewireSpy) Unbind(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "unbind:"+name)
}

func (s *rewireSpy) seq() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// TestRewireOnRelaunch: the reconciler drives the gateway re-wire across a crash (ADR-022).
// A healthy plugin is Bound at its endpoint; on crash it is Unbound; on relaunch it is
// Bound again at the NEW endpoint — so routing self-heals automatically.
func TestRewireOnRelaunch(t *testing.T) {
	fake := &fakeRuntime{status: healthy}
	spy := &rewireSpy{}
	cfg := testCfg()
	cfg.Rewire = spy
	r := New(fake, desiredP(), cfg)
	ctx := context.Background()
	t0 := time.Unix(0, 0)

	r.Reconcile(ctx, t0) // launch (inst-1)
	r.Reconcile(ctx, t0) // check → healthy → Bind
	ep1 := r.Endpoint("P")

	fake.set(unhealthy)  // the plugin crashes
	r.Reconcile(ctx, t0) // Healthy → lost → Unbind + fail (terminate, backoff)
	fake.set(healthy)    // it recovers

	r.Reconcile(ctx, sec(t0, 2)) // past backoff → relaunch (inst-2, new endpoint)
	r.Reconcile(ctx, sec(t0, 2)) // check → healthy → Bind (new endpoint)
	ep2 := r.Endpoint("P")

	got := spy.seq()
	if len(got) != 3 || got[0] != "bind:P@"+ep1 || got[1] != "unbind:P" || got[2] != "bind:P@"+ep2 {
		t.Fatalf("rewire calls = %v, want [bind:P@%s unbind:P bind:P@%s]", got, ep1, ep2)
	}
	if ep1 == "" || ep2 == "" || ep1 == ep2 {
		t.Errorf("expected distinct non-empty endpoints across relaunch, got %q and %q", ep1, ep2)
	}
}

// TestCrashLoopBackoffCapAndNoHammer (sre#4 core): a never-healthy plugin is retried on
// an exponential, capped schedule (1s, 2s, 4s, 4s — base*2^n capped at MaxBackoff), hits
// Degraded at the crash-loop cap, and is then left ALONE (no further launches/terminates
// — the loop isn't hammered).
func TestCrashLoopBackoffCapAndNoHammer(t *testing.T) {
	fake := &fakeRuntime{status: unhealthy}
	r := New(fake, desiredP(), testCfg())
	ctx := context.Background()
	t0 := time.Unix(0, 0)

	// One failure cycle = launch pass + check pass (which fails). Returns the new attempt
	// count and the scheduled gap to the next retry.
	failCycle := func(now time.Time) (attempts int, gap time.Duration) {
		r.Reconcile(ctx, now) // launch
		r.Reconcile(ctx, now) // check → unhealthy → fail
		_, attempts, next := r.Status("P")
		return attempts, next.Sub(now)
	}

	now := t0
	wantGaps := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second} // exp, then capped
	for i, want := range wantGaps {
		att, gap := failCycle(now)
		if att != i+1 || gap != want {
			t.Fatalf("failure %d: attempts=%d gap=%s, want attempts=%d gap=%s", i+1, att, gap, i+1, want)
		}
		// A pass mid-backoff must NOT relaunch.
		lBefore, _ := fake.counts()
		r.Reconcile(ctx, now.Add(gap/2))
		if l, _ := fake.counts(); l != lBefore {
			t.Fatalf("relaunched during backoff after failure %d", i+1)
		}
		now = now.Add(gap)
	}
	// 5th failure (== cap) → Degraded.
	failCycle(now)
	if st, att, _ := r.Status("P"); st != Degraded || att != 5 {
		t.Fatalf("after the cap: state=%s attempts=%d, want Degraded/5", st, att)
	}
	// Degraded → no more activity, ever.
	l0, t0c := fake.counts()
	for i := 0; i < 10; i++ {
		r.Reconcile(ctx, sec(now, 3600*(i+1)))
	}
	if l, tc := fake.counts(); l != l0 || tc != t0c {
		t.Errorf("hammered after Degraded: launches %d->%d terminates %d->%d", l0, l, t0c, tc)
	}
}

// TestConvergesAndStaysHealthy: a healthy plugin is launched once and stays converged.
func TestConvergesAndStaysHealthy(t *testing.T) {
	fake := &fakeRuntime{status: healthy}
	r := New(fake, desiredP(), testCfg())
	ctx := context.Background()
	t0 := time.Unix(0, 0)

	r.Reconcile(ctx, t0) // launch
	r.Reconcile(ctx, t0) // check → healthy
	if st, att, _ := r.Status("P"); st != Healthy || att != 0 {
		t.Fatalf("state=%s attempts=%d, want Healthy/0", st, att)
	}
	if r.Endpoint("P") == "" {
		t.Error("healthy plugin has no endpoint")
	}
	for i := 1; i <= 5; i++ {
		r.Reconcile(ctx, sec(t0, i*30))
	}
	if l, _ := fake.counts(); l != 1 {
		t.Errorf("relaunched a healthy plugin %d times, want 1", l)
	}
}

// TestReadinessWaitThenTimeout: an UNKNOWN (still-starting) plugin is waited on within
// the readiness window, and fails (→ backoff) only once the window elapses.
func TestReadinessWaitThenTimeout(t *testing.T) {
	fake := &fakeRuntime{status: unknown}
	cfg := testCfg()
	cfg.ReadinessTimeout = 10 * time.Second
	r := New(fake, desiredP(), cfg)
	ctx := context.Background()
	t0 := time.Unix(0, 0)

	r.Reconcile(ctx, t0)         // launch (launchedAt=t0)
	r.Reconcile(ctx, sec(t0, 5)) // UNKNOWN, within readiness → wait, no failure
	if st, att, _ := r.Status("P"); st != Pending || att != 0 {
		t.Fatalf("within readiness: state=%s attempts=%d, want Pending/0", st, att)
	}
	if _, tc := fake.counts(); tc != 0 {
		t.Fatalf("terminated while still within the readiness window")
	}
	r.Reconcile(ctx, sec(t0, 11)) // past readiness, still UNKNOWN → fail
	if _, att, _ := r.Status("P"); att != 1 {
		t.Fatalf("past readiness: attempts=%d, want 1", att)
	}
}

// TestRecoveryResetsBackoff: a plugin that recovers (becomes healthy) clears its
// consecutive-failure count — a later crash starts the backoff schedule fresh.
func TestRecoveryResetsBackoff(t *testing.T) {
	fake := &fakeRuntime{status: unhealthy}
	r := New(fake, desiredP(), testCfg())
	ctx := context.Background()
	now := time.Unix(0, 0)

	r.Reconcile(ctx, now) // launch
	r.Reconcile(ctx, now) // fail #1
	now = sec(now, 1)
	r.Reconcile(ctx, now) // relaunch
	r.Reconcile(ctx, now) // fail #2
	if _, att, _ := r.Status("P"); att != 2 {
		t.Fatalf("attempts before recovery = %d, want 2", att)
	}
	now = sec(now, 2) // its next retry is due
	fake.set(healthy)
	r.Reconcile(ctx, now) // relaunch
	r.Reconcile(ctx, now) // check → healthy → reset
	if st, att, _ := r.Status("P"); st != Healthy || att != 0 {
		t.Fatalf("after recovery: state=%s attempts=%d, want Healthy/0", st, att)
	}
}

// TestLaunchErrorCrashLoops: a plugin whose Launch itself keeps failing (e.g. an I9
// refusal / bad image) crash-loops through the same backoff+cap path to Degraded.
func TestLaunchErrorCrashLoops(t *testing.T) {
	fake := &fakeRuntime{launchErr: errors.New("I9 refusal")}
	cfg := testCfg()
	cfg.CrashLoopCap = 3
	r := New(fake, desiredP(), cfg)
	ctx := context.Background()
	now := time.Unix(0, 0)

	r.Reconcile(ctx, now) // launch → error → fail #1
	if _, att, _ := r.Status("P"); att != 1 {
		t.Fatalf("attempts=%d after first launch error, want 1", att)
	}
	now = sec(now, 1)
	r.Reconcile(ctx, now) // fail #2
	now = sec(now, 2)
	r.Reconcile(ctx, now) // fail #3 → Degraded
	if st, _, _ := r.Status("P"); st != Degraded {
		t.Fatalf("state=%s after %d launch errors, want Degraded", st, cfg.CrashLoopCap)
	}
}

// blockingRuntime's Healthcheck blocks until released OR the call's deadline fires — so a
// test can simulate a wedged plugin and assert the reconciler bounds it (AV-3).
type blockingRuntime struct {
	deploymentruntimev1.UnimplementedDeploymentRuntimeServiceServer
	mu      sync.Mutex
	seq     int
	release chan struct{}
}

func (f *blockingRuntime) Launch(_ context.Context, _ *deploymentruntimev1.LaunchRequest) (*deploymentruntimev1.LaunchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return &deploymentruntimev1.LaunchResponse{InstanceId: fmt.Sprintf("inst-%d", f.seq), Endpoint: "127.0.0.1:9000"}, nil
}

func (f *blockingRuntime) Healthcheck(ctx context.Context, _ *deploymentruntimev1.HealthcheckRequest) (*deploymentruntimev1.HealthcheckResponse, error) {
	select {
	case <-f.release:
		return &deploymentruntimev1.HealthcheckResponse{Status: healthy}, nil
	case <-ctx.Done():
		return nil, ctx.Err() // the per-call deadline (AV-3) cut us
	}
}

func (f *blockingRuntime) Terminate(_ context.Context, _ *deploymentruntimev1.TerminateRequest) (*deploymentruntimev1.TerminateResponse, error) {
	return &deploymentruntimev1.TerminateResponse{Terminated: true}, nil
}

// TestStatusReadPathUnblockedByHungHealthcheck (AV-3): a reconcile pass wedged in a runtime
// RPC must neither (1) blind Status — it reads the published snapshot, off the reconcile
// mutex — nor (2) pin the loop forever — the per-call deadline cuts the hung RPC and the pass
// completes on its own (we never release it).
func TestStatusReadPathUnblockedByHungHealthcheck(t *testing.T) {
	fake := &blockingRuntime{release: make(chan struct{})}
	cfg := testCfg()
	cfg.RPCTimeout = 50 * time.Millisecond
	r := New(fake, desiredP(), cfg)
	ctx := context.Background()
	t0 := time.Unix(0, 0)

	r.Reconcile(ctx, t0) // launch (fast) → P Pending with an instance

	done := make(chan struct{})
	go func() { r.Reconcile(ctx, t0); close(done) }() // pass 2: Healthcheck blocks, holding the reconcile mutex
	time.Sleep(10 * time.Millisecond)                 // let the pass enter the hung healthcheck

	// (1) Status returns promptly from the snapshot despite the pass holding the reconcile mutex.
	start := time.Now()
	if st, _, _ := r.Status("P"); st != Pending {
		t.Fatalf("Status during hung healthcheck = %s, want Pending (from the snapshot)", st)
	}
	if d := time.Since(start); d > 25*time.Millisecond {
		t.Fatalf("Status blocked %s behind the hung healthcheck — read path not decoupled", d)
	}

	// (2) The RPC deadline cuts the hung healthcheck, so the pass completes without us releasing it.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(fake.release)
		t.Fatal("reconcile pass never completed — the RPC deadline did not bound the hung Healthcheck")
	}
}
