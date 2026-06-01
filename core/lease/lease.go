// Package lease is the reconciler's leader-election substrate (sre#4 — reviews/03
// §incident-runbooks, reviews/09): a single-key linearizable compare-and-set lease,
// plus an Elector with the lease-thrash guard (a TTL margin + a minimum-hold time)
// that keeps leadership stable when renewals lag — the K8s "etcd-slow → apiserver
// flaps" failure that promotes sre#4 to a Phase-1 exit gate.
//
// The Store models what a state-backend provides (overview.md D5: leader election via
// the state-backend's single-key linearizable CAS); in-memory + a mutex make it
// linearizable for the spike. The Elector is a STEP function (no goroutine/sleep) so
// leadership is tested deterministically; the reconciler's Run loop drives Step on a
// jittered tick (jitter prevents renewals across replicas marching in lockstep).
package lease

import (
	"sync"
	"time"
)

// Store is a single-key, linearizable CAS lease. The fencing token (version) is
// monotonic and bumped on every ACQUISITION (a new leadership term), never on a
// renewal — so a stale holder that lost and regained the lease is distinguishable.
type Store struct {
	mu      sync.Mutex
	holder  string
	expiry  time.Time
	version uint64
}

// NewStore returns an unheld lease.
func NewStore() *Store { return &Store{} }

// Acquire is the compare-and-set: it succeeds only if the lease is unheld or has
// EXPIRED at now. On success the caller holds it until now+ttl with a new (higher)
// fencing token. A caller that already holds a live lease gets ok=false (it must
// Renew, not re-Acquire).
func (s *Store) Acquire(candidate string, now time.Time, ttl time.Duration) (ok bool, token uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != "" && now.Before(s.expiry) {
		return false, 0
	}
	s.holder = candidate
	s.expiry = now.Add(ttl)
	s.version++
	return true, s.version
}

// Renew extends the lease iff candidate still holds it under the same token and it
// has not expired. It does NOT bump the token (same term). ok=false means the lease
// was lost (it expired and/or was taken over) — the caller must step down.
func (s *Store) Renew(candidate string, token uint64, now time.Time, ttl time.Duration) (ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != candidate || s.version != token || !now.Before(s.expiry) {
		return false
	}
	s.expiry = now.Add(ttl)
	return true
}

// Holder reports the live holder at now ("" if none/expired). For tests/observability.
func (s *Store) Holder(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder == "" || !now.Before(s.expiry) {
		return ""
	}
	return s.holder
}

// Elector runs leader election for ONE candidate against a shared Store.
//
// Thrash guard = the TTL margin: a leader renews on every Step, and ttl is chosen
// several Steps long, so a transient gap between Steps (a renewal-latency spike)
// does NOT expire the lease — leadership is retained instead of ping-ponging. A
// follower only Acquires once the lease has GENUINELY expired (a real leader
// failure), giving the documented minimum-hold behaviour: once elected, a leader
// keeps leadership for at least ttl of quiet, and is displaced only by true expiry.
type Elector struct {
	id    string
	store *Store
	ttl   time.Duration

	leader     bool
	token      uint64
	acquiredAt time.Time
}

// NewElector returns a candidate for the shared store. ttl is the lease lifetime;
// drive Step more often than ttl (the Run loop uses ttl/renewDivisor) so the margin
// absorbs latency spikes.
func NewElector(id string, store *Store, ttl time.Duration) *Elector {
	return &Elector{id: id, store: store, ttl: ttl}
}

// Step advances election at now and reports whether this candidate is leader after.
// A leader renews (keeping leadership across transient gaps via the TTL margin); if
// renewal fails it steps down. A non-leader tries to acquire — which the Store only
// permits once any prior lease has expired.
func (e *Elector) Step(now time.Time) bool {
	if e.leader {
		if e.store.Renew(e.id, e.token, now, e.ttl) {
			return true
		}
		e.leader = false // lost the lease (renewed too late / taken over)
	}
	if ok, token := e.store.Acquire(e.id, now, e.ttl); ok {
		e.leader, e.token, e.acquiredAt = true, token, now
		return true
	}
	return false
}

// IsLeader reports leadership as of the last Step.
func (e *Elector) IsLeader() bool { return e.leader }

// HeldSince reports when this candidate last acquired leadership (zero if never/not
// leader) — lets a caller assert the minimum-hold property.
func (e *Elector) HeldSince() time.Time {
	if !e.leader {
		return time.Time{}
	}
	return e.acquiredAt
}
