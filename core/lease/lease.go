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

// Backend is the lease substrate the Elector drives: a single-key, linearizable
// compare-and-set lease. Two implementations satisfy it — the in-memory Store (solo,
// the default) and StateStore (a CAS over the state-backend axis, the multi-replica HA
// path). Both expose the same fencing contract:
//
//   - Acquire succeeds only if the lease is unheld or EXPIRED at now; on success the
//     caller holds it until now+ttl under a fresh token.
//   - Renew extends the lease iff candidate still holds it under `token`; it returns the
//     token to carry into the next Renew (unchanged for the in-memory store; the new CAS
//     revision for StateStore, which re-stamps the key every write).
//
// THE err RETURN IS LOAD-BEARING (sre AV-1). ok=false with err==nil means the lease was
// genuinely LOST (a deterministic CAS conflict / expiry) — step down. A non-nil err means
// the backend could not CONFIRM the outcome (timeout/partition — the state/v1
// PUT_OUTCOME_UNKNOWN case): the holder must NOT treat that as lost, but hold leadership
// until its LOCAL ttl genuinely expires (the Elector does this). Collapsing the two into a
// bare bool — as the spike did — turns every transient backend blip into a leadership
// flap, the exact split-brain-adjacent thrash a durable backend makes real.
type Backend interface {
	Acquire(candidate string, now time.Time, ttl time.Duration) (ok bool, token uint64, err error)
	Renew(candidate string, token uint64, now time.Time, ttl time.Duration) (ok bool, newToken uint64, err error)
}

// Store is the in-memory Backend: a single-key, linearizable CAS lease behind a mutex
// (the solo default — no external dependency). The fencing token (version) is monotonic
// and bumped on every ACQUISITION (a new leadership term), never on a renewal — so a stale
// holder that lost and regained the lease is distinguishable. It never errors (the backend
// is local), so it always returns a nil err.
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
// Renew, not re-Acquire). The in-memory store never errors.
func (s *Store) Acquire(candidate string, now time.Time, ttl time.Duration) (ok bool, token uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != "" && now.Before(s.expiry) {
		return false, 0, nil
	}
	s.holder = candidate
	s.expiry = now.Add(ttl)
	s.version++
	return true, s.version, nil
}

// Renew extends the lease iff candidate still holds it under the same token and it
// has not expired. It does NOT bump the token (same term), so newToken == token. ok=false
// (err==nil) means the lease was genuinely lost (expired and/or taken over) — the caller
// must step down. The in-memory store never errors.
func (s *Store) Renew(candidate string, token uint64, now time.Time, ttl time.Duration) (ok bool, newToken uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != candidate || s.version != token || !now.Before(s.expiry) {
		return false, 0, nil
	}
	s.expiry = now.Add(ttl)
	return true, token, nil
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
	store Backend
	ttl   time.Duration

	leader      bool
	token       uint64
	acquiredAt  time.Time
	leaseExpiry time.Time // local view of when our lease lapses (the AV-1 transient-hold bound)
}

// NewElector returns a candidate for the shared Backend. ttl is the lease lifetime;
// drive Step more often than ttl (the Run loop uses ttl/renewDivisor) so the margin
// absorbs latency spikes. The Backend may be the in-memory Store (solo) or StateStore
// (multi-replica HA over the state axis).
func NewElector(id string, store Backend, ttl time.Duration) *Elector {
	return &Elector{id: id, store: store, ttl: ttl}
}

// Step advances election at now and reports whether this candidate is leader after.
//
// A leader renews. A renewal that COMMITS keeps leadership and carries the (possibly new)
// fencing token forward. A renewal that is genuinely REFUSED (ok=false, err==nil — a CAS
// conflict / expiry) steps down. A renewal that is UNCERTAIN (err!=nil — the backend
// couldn't confirm) does NOT step down while our LOCAL lease is still valid: we hold
// leadership until leaseExpiry, then step down (AV-1 — transient backend errors must not
// thrash leadership). A non-leader tries to acquire, which the Backend permits only once
// any prior lease has genuinely expired; an acquire error leaves it a follower.
func (e *Elector) Step(now time.Time) bool {
	if e.leader {
		ok, newToken, err := e.store.Renew(e.id, e.token, now, e.ttl)
		switch {
		case ok:
			e.token = newToken
			e.leaseExpiry = now.Add(e.ttl)
			return true
		case err != nil && now.Before(e.leaseExpiry):
			// Uncertain renewal, but our local lease has not lapsed — hold leadership.
			return true
		default:
			// Genuinely lost (conflict/expiry), or uncertain AND our local lease has lapsed.
			e.leader = false
		}
	}
	if ok, token, err := e.store.Acquire(e.id, now, e.ttl); err == nil && ok {
		e.leader, e.token, e.acquiredAt, e.leaseExpiry = true, token, now, now.Add(e.ttl)
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
