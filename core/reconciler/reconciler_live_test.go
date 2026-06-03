package reconciler

import (
	"context"
	"strings"
	"testing"
	"time"

	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
)

func sawPrefix(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// TestAddRemoveDesiredLive: a plugin added to a RUNNING reconciler converges (launch →
// Healthy → Rewire.Bind) through the unchanged path, and removing it terminates + unbinds
// it and stops reconciling it — the live ControlService backbone (ADR-027).
func TestAddRemoveDesiredLive(t *testing.T) {
	rt := &fakeRuntime{}
	rt.set(healthy)
	spy := &rewireSpy{}
	cfg := testCfg()
	cfg.Rewire = spy
	r := New(rt, nil, cfg) // empty desired set
	ctx := context.Background()
	base := time.Unix(0, 0)

	r.Reconcile(ctx, base) // empty plane → nothing happens
	if l, _ := rt.counts(); l != 0 {
		t.Fatalf("empty desired should launch nothing, got %d", l)
	}

	// live add
	if err := r.AddDesired(Desired{Name: "X", Launch: &deploymentruntimev1.LaunchSpec{Image: "img"}}); err != nil {
		t.Fatalf("AddDesired: %v", err)
	}
	if err := r.AddDesired(Desired{Name: "X"}); err == nil {
		t.Fatal("duplicate AddDesired should error")
	}

	r.Reconcile(ctx, base)         // launch
	r.Reconcile(ctx, sec(base, 1)) // observe healthy → bind
	if st, _, _ := r.Status("X"); st != Healthy {
		t.Fatalf("X state = %v, want Healthy", st)
	}
	if !sawPrefix(spy.seq(), "bind:X@") {
		t.Fatalf("expected the gateway to bind X, calls=%v", spy.seq())
	}
	if names := r.DesiredNames(); len(names) != 1 || names[0] != "X" {
		t.Fatalf("DesiredNames = %v, want [X]", names)
	}

	// live remove
	r.RemoveDesired(ctx, "X")
	if _, term := rt.counts(); term != 1 {
		t.Fatalf("RemoveDesired should terminate the instance once, got %d", term)
	}
	if !sawPrefix(spy.seq(), "unbind:X") {
		t.Fatalf("expected the gateway to unbind X, calls=%v", spy.seq())
	}
	if names := r.DesiredNames(); len(names) != 0 {
		t.Fatalf("DesiredNames after remove = %v, want []", names)
	}

	// a removed plugin is no longer reconciled — no relaunch.
	lBefore, _ := rt.counts()
	r.Reconcile(ctx, sec(base, 2))
	if lAfter, _ := rt.counts(); lAfter != lBefore {
		t.Fatalf("removed plugin was relaunched: %d -> %d", lBefore, lAfter)
	}
}
