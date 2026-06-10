package lease

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrCreateIfAbsentUnsupported is returned by a StateCAS whose backend lacks the optional
// state/v1 create-if-absent capability (ADR-049). The lease catches it and falls back to a
// guarded unconditional create (the legacy cold-start path), so a CAS-only backend still works.
var ErrCreateIfAbsentUnsupported = errors.New("lease: backend has no create-if-absent (state/v1 ADR-049)")

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
// COLD-START RACE — CLOSED when the backend supports create-if-absent (ADR-049). STEADY-STATE
// contention (the key already exists) is pure CAS and split-brain-free. The historical race —
// two replicas creating a never-before-existing key at the same instant — is now closed by the
// atomic `CreateIfAbsent` (exactly one creator wins). A backend WITHOUT that optional capability
// falls back to a guarded unconditional create (the legacy path: still racy on simultaneous cold
// start — mitigate by pre-init / staggered starts), so a CAS-only backend keeps working.
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
	// CreateIfAbsent atomically creates key with value ONLY if absent (state/v1 create-if-absent,
	// ADR-049 — the cold-start primitive). committed=true → created (we are the sole creator);
	// committed=false (err==nil) → the key already existed (a concurrent creator won). A non-nil
	// err is the UNKNOWN/transport case. An implementation whose backend lacks the optional
	// capability MUST return ErrCreateIfAbsentUnsupported so the lease falls back.
	CreateIfAbsent(ctx context.Context, key string, value []byte) (committed bool, revision int64, err error)
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

// Acquire takes the lease iff it is unheld or expired at now. When the key EXISTS (expired or
// ours), it CAS-overwrites on the observed revision — the backend's linearizable CAS lets exactly
// one of two same-revision contenders commit. When the key is ABSENT (cold start), it uses the
// atomic CreateIfAbsent (ADR-049) so exactly one of two simultaneous creators wins — closing the
// cold-start race; a backend lacking that capability falls back to a guarded unconditional create.
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
		// Expired or already ours → CAS-overwrite on the observed revision.
		return s.outcome(s.put(ctx, candidate, now.Add(ttl), rev))
	}

	// Absent → atomic create-if-absent (closes the cold-start race); fall back to a guarded
	// unconditional create when the backend lacks the optional capability (ADR-049 / Q04).
	committed, newRev, err := s.createIfAbsent(ctx, candidate, now.Add(ttl))
	if errors.Is(err, ErrCreateIfAbsentUnsupported) {
		committed, newRev, err = s.put(ctx, candidate, now.Add(ttl), 0)
	}
	return s.outcome(committed, newRev, err)
}

// outcome maps a (committed, revision, err) write result onto the Backend's (ok, token, err):
// err → uncertain; !committed → lost (conflict/already-created); committed → acquired.
func (s *StateStore) outcome(committed bool, newRev int64, err error) (bool, uint64, error) {
	if err != nil {
		return false, 0, err
	}
	if !committed {
		return false, 0, nil
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

func (s *StateStore) createIfAbsent(ctx context.Context, holder string, expiry time.Time) (bool, int64, error) {
	b, _ := json.Marshal(leaseRecord{Holder: holder, ExpiryUnixMs: expiry.UnixMilli()})
	return s.cas.CreateIfAbsent(ctx, s.key, b)
}

func (s *StateStore) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.callTimeout)
}

func decodeLease(b []byte) (leaseRecord, error) {
	var r leaseRecord
	err := json.Unmarshal(b, &r)
	return r, err
}
