package arrowticket

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestValidThenSingleUse(t *testing.T) {
	m := NewMinter([]byte("secret-key"))
	tk, err := m.Mint("stream-1", "rat-strategy", "tenantA", time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := m.Validate(tk, "stream-1", "rat-strategy", "tenantA"); err != nil {
		t.Fatalf("valid ticket rejected: %v", err)
	}
	// Single-use: the second presentation of the same ticket is rejected.
	if err := m.Validate(tk, "stream-1", "rat-strategy", "tenantA"); err != ErrReplay {
		t.Fatalf("replay err = %v, want ErrReplay", err)
	}
}

func TestBindingEnforced(t *testing.T) {
	m := NewMinter([]byte("k"))
	mint := func() []byte {
		tk, _ := m.Mint("stream-1", "rat-strategy", "tenantA", time.Minute)
		return tk
	}
	// Each fresh ticket must fail when presented with a mismatched binding.
	if err := m.Validate(mint(), "stream-1", "rat-strategy", "tenantB"); err != ErrNotBound {
		t.Errorf("cross-tenant err = %v, want ErrNotBound", err)
	}
	if err := m.Validate(mint(), "stream-1", "rat-OTHER", "tenantA"); err != ErrNotBound {
		t.Errorf("cross-caller err = %v, want ErrNotBound", err)
	}
	if err := m.Validate(mint(), "stream-OTHER", "rat-strategy", "tenantA"); err != ErrNotBound {
		t.Errorf("cross-stream err = %v, want ErrNotBound", err)
	}
}

func TestExpiry(t *testing.T) {
	m := NewMinter([]byte("k"))
	tk, _ := m.Mint("s", "c", "t", time.Millisecond)
	m.now = func() time.Time { return time.Now().Add(time.Hour) } // advance the clock past expiry
	if err := m.Validate(tk, "s", "c", "t"); err != ErrExpired {
		t.Fatalf("expired err = %v, want ErrExpired", err)
	}
}

func TestTamperOrWrongKey(t *testing.T) {
	m := NewMinter([]byte("real-key"))
	tk, _ := m.Mint("s", "c", "t", time.Minute)
	// A validator with a different key cannot accept the ticket (forgery/tamper).
	other := NewMinter([]byte("different-key"))
	if err := other.Validate(tk, "s", "c", "t"); err != ErrTampered {
		t.Fatalf("wrong-key err = %v, want ErrTampered", err)
	}
	// Garbage bytes are malformed, not a panic.
	if err := m.Validate([]byte("not-json"), "s", "c", "t"); err != ErrMalformed {
		t.Errorf("malformed err = %v, want ErrMalformed", err)
	}
}

// --- gap #7: shared single-use store closes replay across restart/replicas -------------

// fakeCAS is an atomic create-if-absent set (etcd-txn / Redis-SETNX semantics) for tests.
type fakeCAS struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newFakeCAS() *fakeCAS { return &fakeCAS{seen: map[string]bool{}} }

func (f *fakeCAS) PutIfAbsent(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

// TestSharedStoreClosesReplayAcrossMinters is the gap-#7 property: two minters (a restart, or a
// second replica — same key, SHARED single-use store) cannot both redeem one ticket. A ticket
// consumed via minter A is a replay via minter B. With the default per-process store, B would NOT
// catch it — which is the gap.
func TestSharedStoreClosesReplayAcrossMinters(t *testing.T) {
	key := []byte("producer-key")
	store := NewCASStore(newFakeCAS(), "arrowticket/used/")
	a := NewMinterWithStore(key, store)
	b := NewMinterWithStore(key, store) // a restarted / second-replica producer, same shared store

	tk, err := a.Mint("stream-1", "rat-format", "tenantA", time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := a.Validate(tk, "stream-1", "rat-format", "tenantA"); err != nil {
		t.Fatalf("first redemption (A) rejected: %v", err)
	}
	if err := b.Validate(tk, "stream-1", "rat-format", "tenantA"); err != ErrReplay {
		t.Fatalf("replay via B err = %v, want ErrReplay (shared store must close it)", err)
	}
}

// TestCASStoreAtomicSingleUse: two concurrent redemptions of ONE ticket id yield exactly one
// firstUse — the atomic create-if-absent is what prevents a double-spend race (run under -race).
func TestCASStoreAtomicSingleUse(t *testing.T) {
	store := NewCASStore(newFakeCAS(), "p/")
	var wins int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if firstUse, _ := store.Consume("ticket-xyz"); firstUse {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent Consume firstUse count = %d, want exactly 1", wins)
	}
}

// TestStoreErrorFailsClosed: an unconfirmable single-use check rejects the ticket (fail closed) —
// never silently accepts a possible replay.
func TestStoreErrorFailsClosed(t *testing.T) {
	m := NewMinterWithStore([]byte("k"), errStore{})
	tk, _ := m.Mint("stream-1", "rat-format", "tenantA", time.Minute)
	err := m.Validate(tk, "stream-1", "rat-format", "tenantA")
	if err == nil || err == ErrReplay {
		t.Fatalf("store-error Validate err = %v, want a fail-closed error", err)
	}
}

type errStore struct{}

func (errStore) Consume(string) (bool, error) { return false, errors.New("backend unavailable") }
