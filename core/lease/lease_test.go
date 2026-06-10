package lease

import (
	"testing"
	"time"
)

const ttl = 30 * time.Second

func at(base time.Time, secs int) time.Time { return base.Add(time.Duration(secs) * time.Second) }

// TestStoreAcquireRenewExpire: the CAS basics — a held lease blocks others, a renewal
// extends it, and once expired another candidate takes over (and the old token dies).
func TestStoreAcquireRenewExpire(t *testing.T) {
	s := NewStore()
	t0 := time.Unix(1000, 0)
	ok, tok, err := s.Acquire("A", t0, ttl)
	if !ok || tok == 0 || err != nil {
		t.Fatalf("Acquire(A) = (%v,%d,%v), want (true, >0, nil)", ok, tok, err)
	}
	if ok, _, _ := s.Renew("A", tok, at(t0, 10), ttl); !ok {
		t.Fatal("Renew(A) within ttl failed")
	}
	if got, _, _ := s.Acquire("B", at(t0, 20), ttl); got {
		t.Fatal("B acquired a live lease")
	}
	// Last renew was at +10 → expiry +40; B acquires only after that.
	if got, _, _ := s.Acquire("B", at(t0, 41), ttl); !got {
		t.Fatal("B failed to acquire the expired lease")
	}
	if ok, _, _ := s.Renew("A", tok, at(t0, 42), ttl); ok {
		t.Fatal("A renewed with a stale token after losing the lease")
	}
}

// TestTwoContenderMutualExclusion: two electors share one store; exactly one is leader,
// and it stays leader while it keeps renewing.
func TestTwoContenderMutualExclusion(t *testing.T) {
	s := NewStore()
	a, b := NewElector("A", s, ttl), NewElector("B", s, ttl)
	t0 := time.Unix(2000, 0)
	a.Step(t0)
	b.Step(t0)
	if !a.IsLeader() || b.IsLeader() {
		t.Fatalf("after t0: want A leader / B follower, got A=%v B=%v", a.IsLeader(), b.IsLeader())
	}
	for i := 1; i <= 10; i++ {
		now := at(t0, i*10) // renew cadence 10s, ttl 30s
		a.Step(now)
		b.Step(now)
		if !a.IsLeader() || b.IsLeader() {
			t.Fatalf("step %d: leadership flipped (A=%v B=%v)", i, a.IsLeader(), b.IsLeader())
		}
	}
}

// TestThrashGuardRetainsLeadershipUnderLatencySpike: the lease-thrash guard. A's
// renewal is delayed by a latency spike (a 25s gap) but stays within the 30s TTL
// margin — so A keeps leadership and B never steals it. No ping-pong.
func TestThrashGuardRetainsLeadershipUnderLatencySpike(t *testing.T) {
	s := NewStore()
	a, b := NewElector("A", s, ttl), NewElector("B", s, ttl)
	t0 := time.Unix(3000, 0)
	a.Step(t0) // A leader, expiry t0+30
	b.Step(t0) // B follower
	// B keeps trying while A is "slow"; it must not steal within the margin.
	b.Step(at(t0, 10))
	b.Step(at(t0, 20))
	if b.IsLeader() {
		t.Fatal("B stole leadership during A's renewal-latency spike — thrash guard failed")
	}
	// A renews late (+25) but inside the TTL margin → retains leadership.
	if !a.Step(at(t0, 25)) {
		t.Fatal("A lost leadership despite renewing within the TTL margin")
	}
	if h := s.Holder(at(t0, 25)); h != "A" {
		t.Fatalf("holder = %q after A's late renewal, want A", h)
	}
}

// TestFailoverAfterLeaderStops: a real leader failure (stops stepping) — the follower
// acquires only after the lease genuinely EXPIRES, not on the first missed renewal.
func TestFailoverAfterLeaderStops(t *testing.T) {
	s := NewStore()
	a, b := NewElector("A", s, ttl), NewElector("B", s, ttl)
	t0 := time.Unix(4000, 0)
	a.Step(t0) // A leader, expiry +30
	b.Step(t0)
	// A crashes. B can't acquire before expiry.
	if b.Step(at(t0, 10)); b.IsLeader() {
		t.Fatal("B acquired before the lease expired (+10)")
	}
	if b.Step(at(t0, 29)); b.IsLeader() {
		t.Fatal("B acquired before the lease expired (+29)")
	}
	// After expiry, B fails over.
	if !b.Step(at(t0, 31)) {
		t.Fatal("B did not fail over after the lease expired")
	}
}

// TestMinimumHold: a leader that keeps renewing holds ONE continuous term (its
// acquire time never restarts) despite a contender — the minimum-hold property.
func TestMinimumHold(t *testing.T) {
	s := NewStore()
	a, b := NewElector("A", s, ttl), NewElector("B", s, ttl)
	t0 := time.Unix(5000, 0)
	a.Step(t0)
	term := a.HeldSince()
	for i := 1; i <= 6; i++ {
		now := at(t0, i*5)
		a.Step(now)
		b.Step(now)
	}
	if !a.IsLeader() {
		t.Fatal("A lost leadership while actively renewing")
	}
	if a.HeldSince() != term {
		t.Fatal("A's leadership term restarted (re-acquired) — leadership was not continuously held")
	}
}
