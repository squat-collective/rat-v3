package lease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeCAS is a linearizable single-key CAS — the state-backend semantics the lease needs,
// in-process for tests. failPut models an UNKNOWN/transport failure (state/v1 PUT_OUTCOME_UNKNOWN).
type fakeCAS struct {
	mu      sync.Mutex
	val     []byte
	rev     int64
	found   bool
	failPut bool
}

func (f *fakeCAS) Get(_ context.Context, _ string) ([]byte, int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.val, f.rev, f.found, nil
}

func (f *fakeCAS) Put(_ context.Context, _ string, value []byte, ifRevision int64) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPut {
		return false, 0, errors.New("backend unavailable")
	}
	if ifRevision > 0 && ifRevision != f.rev {
		return false, f.rev, nil // deterministic CAS CONFLICT
	}
	f.val, f.found = value, true
	f.rev++
	return true, f.rev, nil
}

const sttl = 30 * time.Second

// TestStateStoreTwoContenderOneLeader is the HA property the in-memory Store cannot provide:
// two electors over the SAME shared state key elect exactly one leader (each replica sees the
// other's write through the shared CAS), and leadership stays put while the leader renews.
func TestStateStoreTwoContenderOneLeader(t *testing.T) {
	cas := &fakeCAS{}
	key := "rat/lease/rat-serve"
	a := NewElector("A", NewStateStore(cas, key, time.Second), sttl)
	b := NewElector("B", NewStateStore(cas, key, time.Second), sttl)
	t0 := time.Unix(2000, 0)

	a.Step(t0)
	b.Step(t0)
	if !a.IsLeader() || b.IsLeader() {
		t.Fatalf("after t0: want A leader / B follower, got A=%v B=%v", a.IsLeader(), b.IsLeader())
	}
	for i := 1; i <= 10; i++ {
		now := at(t0, i*10) // renew cadence 10s within the 30s ttl
		a.Step(now)
		b.Step(now)
		if !a.IsLeader() || b.IsLeader() {
			t.Fatalf("step %d: leadership flipped (A=%v B=%v)", i, a.IsLeader(), b.IsLeader())
		}
	}
}

// TestStateStoreFailover: the leader stops stepping (crash); the follower acquires only after
// the lease genuinely EXPIRES, not on the first missed renewal.
func TestStateStoreFailover(t *testing.T) {
	cas := &fakeCAS{}
	key := "rat/lease/rat-serve"
	a := NewElector("A", NewStateStore(cas, key, time.Second), sttl)
	b := NewElector("B", NewStateStore(cas, key, time.Second), sttl)
	t0 := time.Unix(4000, 0)

	a.Step(t0) // A leader, expiry +30
	b.Step(t0)
	if b.Step(at(t0, 10)); b.IsLeader() {
		t.Fatal("B acquired before the lease expired (+10)")
	}
	if b.Step(at(t0, 29)); b.IsLeader() {
		t.Fatal("B acquired before the lease expired (+29)")
	}
	if !b.Step(at(t0, 31)) {
		t.Fatal("B did not fail over after the lease expired")
	}
}

// TestStateStoreTransientHold is the AV-1 fix: a leader whose renewals can't be CONFIRMED
// (backend unreachable) holds leadership until its LOCAL ttl lapses, then steps down — a
// transient blip must not flap leadership.
func TestStateStoreTransientHold(t *testing.T) {
	cas := &fakeCAS{}
	a := NewElector("A", NewStateStore(cas, "rat/lease/rat-serve", time.Second), sttl)
	t0 := time.Unix(6000, 0)
	if !a.Step(t0) || !a.IsLeader() {
		t.Fatal("A did not acquire at t0")
	}

	cas.failPut = true // the state-backend goes unreachable — renewals return UNKNOWN
	if !a.Step(at(t0, 10)) || !a.IsLeader() {
		t.Fatal("A dropped leadership on a transient renewal error within ttl (+10) — AV-1 violated")
	}
	if !a.Step(at(t0, 25)) || !a.IsLeader() {
		t.Fatal("A dropped leadership on a transient renewal error within ttl (+25) — AV-1 violated")
	}
	if a.Step(at(t0, 31)); a.IsLeader() {
		t.Fatal("A retained leadership past its local lease expiry while unable to renew (+31)")
	}
}

// TestStateStoreCASFencing: Renew carries the new CAS revision forward as the fencing token,
// and a renewal under a STALE token deterministically conflicts (ok=false, err==nil — not an error).
func TestStateStoreCASFencing(t *testing.T) {
	cas := &fakeCAS{}
	ss := NewStateStore(cas, "rat/lease/rat-serve", time.Second)
	t0 := time.Unix(7000, 0)

	ok, tok1, err := ss.Acquire("A", t0, sttl)
	if !ok || err != nil {
		t.Fatalf("Acquire = (%v,%v), want (true,nil)", ok, err)
	}
	ok, tok2, err := ss.Renew("A", tok1, at(t0, 10), sttl)
	if !ok || err != nil || tok2 == tok1 {
		t.Fatalf("Renew = (%v, tok %d→%d, %v), want (true, bumped token, nil)", ok, tok1, tok2, err)
	}
	// Renewing under the fresh token commits; under the stale token it conflicts (no error).
	if ok, _, err := ss.Renew("A", tok2, at(t0, 20), sttl); !ok || err != nil {
		t.Fatalf("Renew(fresh token) = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, _, err := ss.Renew("A", tok1, at(t0, 21), sttl); ok || err != nil {
		t.Fatalf("Renew(stale token) = (%v,%v), want (false,nil) — a CAS conflict is not an error", ok, err)
	}
}
