package lease

import (
	"context"
	"encoding/json"
	"time"
)

// StateStore is the multi-replica Backend: the leader-election lease as a single key in a
// SHARED state-backend, driven by the state/v1 compare-and-set primitive (state.proto C5/CAS).
// This is what makes HA real — every `rat serve` replica points its Elector at the SAME state
// key, so the backend's linearizable CAS elects exactly one leader across the fleet. The
// in-memory Store can't: each process holds its own copy, so every replica would self-elect.
//
// BOOTSTRAP CONSTRAINT. The state-backend that holds the lease must be SHARED and reachable by
// every replica independently of the plane they reconcile — it is the fleet's etcd analogue,
// not a plugin a replica launches (that would be circular: you need the lease to decide who
// launches). In practice it is an external/attached backend at a fixed address.
//
// CREATE RACE (honest caveat). state/v1 has no create-if-absent primitive — `if_revision` can
// CAS an existing key or write unconditionally, but cannot atomically "create only if absent."
// So STEADY-STATE contention (the key already exists) is pure CAS and split-brain-free: exactly
// one Acquire/Renew commits per revision. The ONE race is two replicas creating a
// never-before-existing lease key at the same instant. Mitigate by pre-initializing the key, or
// by staggered replica starts (the second replica then sees the key present and CASes normally).
// A create-if-absent amendment to state/v1 would close it fully (noted in ADR-043).
type StateStore struct {
	cas         StateCAS
	key         string
	callTimeout time.Duration
}

// StateCAS is the minimal slice of the state-backend the lease needs: a linearizable read and
// a compare-and-set write. It keeps this package free of gRPC/proto deps (and trivially
// fakeable in tests); cmd/rat adapts a real rat.state.v1 StateServiceClient onto it.
type StateCAS interface {
	// Get reads key: its value + monotonic revision, found=false (revision 0) if absent.
	Get(ctx context.Context, key string) (value []byte, revision int64, found bool, err error)
	// Put writes value at key. ifRevision>0 requires the current revision to match (CAS);
	// 0 writes unconditionally. committed=false (err==nil) is a deterministic CAS CONFLICT
	// (the write did not happen). A non-nil err is the UNKNOWN/transport case — the outcome
	// is unconfirmed (state/v1 PUT_OUTCOME_UNKNOWN), which the lease treats as "uncertain."
	Put(ctx context.Context, key string, value []byte, ifRevision int64) (committed bool, revision int64, err error)
}

// leaseRecord is the value stored at the lease key.
type leaseRecord struct {
	Holder       string `json:"holder"`
	ExpiryUnixMs int64  `json:"expiry_unix_ms"`
}

// NewStateStore builds a state-backed lease at key, bounding each backend call by callTimeout
// (so a hung state-backend can't pin the reconcile tick — an unconfirmed call surfaces as the
// "uncertain" err the Elector holds through).
func NewStateStore(cas StateCAS, key string, callTimeout time.Duration) *StateStore {
	if callTimeout <= 0 {
		callTimeout = 3 * time.Second
	}
	return &StateStore{cas: cas, key: key, callTimeout: callTimeout}
}

// Acquire takes the lease iff it is unheld or expired at now, via a CAS on the observed
// revision. Two contenders that both observe the same revision both CAS on it; the backend's
// linearizable CAS lets exactly one commit (the loser sees a bumped revision → CONFLICT).
func (s *StateStore) Acquire(candidate string, now time.Time, ttl time.Duration) (bool, uint64, error) {
	ctx, cancel := s.ctx()
	defer cancel()
	val, rev, found, err := s.cas.Get(ctx, s.key)
	if err != nil {
		return false, 0, err
	}
	if found {
		if rec, derr := decodeLease(val); derr == nil &&
			rec.Holder != "" && rec.Holder != candidate && now.UnixMilli() < rec.ExpiryUnixMs {
			return false, 0, nil // held by someone else and still live
		}
	}
	// CAS on the observed revision (an absent key writes unconditionally — the create race above).
	ifRev := int64(0)
	if found {
		ifRev = rev
	}
	committed, newRev, err := s.put(ctx, candidate, now.Add(ttl), ifRev)
	if err != nil {
		return false, 0, err
	}
	if !committed {
		return false, 0, nil // lost the acquire race
	}
	return true, uint64(newRev), nil
}

// Renew re-stamps the lease key under our fencing token via CAS. COMMITTED → renewed (carry
// the new revision forward). CONFLICT (committed=false, err==nil) → genuinely lost. err →
// unconfirmed; the Elector holds leadership until its local ttl lapses (AV-1).
func (s *StateStore) Renew(candidate string, token uint64, now time.Time, ttl time.Duration) (bool, uint64, error) {
	ctx, cancel := s.ctx()
	defer cancel()
	committed, newRev, err := s.put(ctx, candidate, now.Add(ttl), int64(token))
	if err != nil {
		return false, 0, err
	}
	if !committed {
		return false, 0, nil
	}
	return true, uint64(newRev), nil
}

func (s *StateStore) put(ctx context.Context, holder string, expiry time.Time, ifRevision int64) (bool, int64, error) {
	b, _ := json.Marshal(leaseRecord{Holder: holder, ExpiryUnixMs: expiry.UnixMilli()})
	return s.cas.Put(ctx, s.key, b, ifRevision)
}

func (s *StateStore) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.callTimeout)
}

func decodeLease(b []byte) (leaseRecord, error) {
	var r leaseRecord
	err := json.Unmarshal(b, &r)
	return r, err
}
