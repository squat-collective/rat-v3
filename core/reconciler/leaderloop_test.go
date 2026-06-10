package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/squat-collective/rat-v3/core/lease"
)

// TestLeaderReconcilesFollowerIdleAndFailover: two replicas contend for one lease;
// only the leader converges the plane, the follower stays idle, the thrash guard keeps
// leadership stable under a renewal-latency spike, and on a leader crash the follower
// fails over (after the lease expires) and resumes reconciling. Step-driven (shared
// store is the single-key CAS) so the whole election story is deterministic.
func TestLeaderReconcilesFollowerIdleAndFailover(t *testing.T) {
	const leaseTTL = 30 * time.Second
	store := lease.NewStore()
	fake := &fakeRuntime{status: healthy}

	elA, rcA := lease.NewElector("A", store, leaseTTL), New(fake, desiredP(), testCfg())
	elB, rcB := lease.NewElector("B", store, leaseTTL), New(fake, desiredP(), testCfg())
	ctx := context.Background()
	// step = one Loop tick: advance election; converge only if leader.
	step := func(el *lease.Elector, rc *Reconciler, now time.Time) {
		if el.Step(now) {
			rc.Reconcile(ctx, now)
		}
	}
	t0 := time.Unix(0, 0)

	// Both contend at t0 → A wins (it stepped first), B is the follower.
	step(elA, rcA, t0)
	step(elB, rcB, t0)
	if !elA.IsLeader() || elB.IsLeader() {
		t.Fatalf("after contention: A=%v B=%v, want A leader / B follower", elA.IsLeader(), elB.IsLeader())
	}

	// A (leader) converges P over a couple of ticks; B (follower) never touches it.
	step(elA, rcA, t0) // A: check → healthy
	step(elB, rcB, t0) // B: not leader → no reconcile
	if st, _, _ := rcA.Status("P"); st != Healthy {
		t.Fatalf("leader A did not converge P (state=%s)", st)
	}
	if st, _, _ := rcB.Status("P"); st != Pending {
		t.Fatalf("follower B reconciled while not leader (state=%s)", st)
	}

	// Thrash guard: A's renewal is delayed to +25 (within the 30s TTL) while B keeps
	// contending at +10/+20 — A keeps leadership, no ping-pong.
	step(elB, rcB, sec(t0, 10))
	step(elB, rcB, sec(t0, 20))
	step(elA, rcA, sec(t0, 25))
	if !elA.IsLeader() || elB.IsLeader() {
		t.Fatalf("thrash guard failed: A=%v B=%v at +25", elA.IsLeader(), elB.IsLeader())
	}

	// Failover: A crashes (stops stepping). B can't take over until the lease expires
	// (last renew at +25 → expiry +55); it acquires only after that.
	step(elB, rcB, sec(t0, 40))
	if elB.IsLeader() {
		t.Fatal("B took over before A's lease expired")
	}
	step(elB, rcB, sec(t0, 56)) // past expiry → B acquires + reconciles (launch)
	step(elB, rcB, sec(t0, 56)) // B: check → healthy
	if !elB.IsLeader() {
		t.Fatal("B did not fail over after A's lease expired")
	}
	if st, _, _ := rcB.Status("P"); st != Healthy {
		t.Fatalf("after failover, new leader B did not converge P (state=%s)", st)
	}
}
