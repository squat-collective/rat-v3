package statecas

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/le-squat/rat/core/arrowticket"
	statev1 "github.com/le-squat/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeStateClient is an atomic create-if-absent backend (the state/v1 semantics), in-process.
type fakeStateClient struct {
	mu      sync.Mutex
	seen    map[string]bool
	rev     int64
	unknown bool // return PUT_OUTCOME_UNKNOWN (unconfirmed)
	unimpl  bool // return gRPC Unimplemented (backend lacks the capability)
}

func newFakeStateClient() *fakeStateClient { return &fakeStateClient{seen: map[string]bool{}} }

func (f *fakeStateClient) CreateIfAbsent(_ context.Context, in *statev1.CreateIfAbsentRequest, _ ...grpc.CallOption) (*statev1.CreateIfAbsentResponse, error) {
	if f.unimpl {
		return nil, status.Error(codes.Unimplemented, "no create-if-absent")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unknown {
		return &statev1.CreateIfAbsentResponse{Outcome: statev1.PutOutcome_PUT_OUTCOME_UNKNOWN}, nil
	}
	if f.seen[in.GetKey()] {
		return &statev1.CreateIfAbsentResponse{Outcome: statev1.PutOutcome_PUT_OUTCOME_CONFLICT, Revision: f.rev}, nil
	}
	f.seen[in.GetKey()] = true
	f.rev++
	return &statev1.CreateIfAbsentResponse{Outcome: statev1.PutOutcome_PUT_OUTCOME_COMMITTED, Revision: f.rev}, nil
}

// TestPutIfAbsentMapping: COMMITTED → first use; CONFLICT → not first use; UNKNOWN and Unimplemented
// → fail closed (error).
func TestPutIfAbsentMapping(t *testing.T) {
	cas := New(newFakeStateClient(), time.Second)
	if first, err := cas.PutIfAbsent("k"); err != nil || !first {
		t.Fatalf("first PutIfAbsent = (%v,%v), want (true,nil)", first, err)
	}
	if first, err := cas.PutIfAbsent("k"); err != nil || first {
		t.Fatalf("second PutIfAbsent = (%v,%v), want (false,nil) — already recorded", first, err)
	}
	if _, err := New(&fakeStateClient{unknown: true, seen: map[string]bool{}}, time.Second).PutIfAbsent("k"); err == nil {
		t.Error("UNKNOWN outcome must fail closed (error), got nil")
	}
	if _, err := New(&fakeStateClient{unimpl: true, seen: map[string]bool{}}, time.Second).PutIfAbsent("k"); err == nil {
		t.Error("Unimplemented backend must fail closed (error), got nil")
	}
}

// TestTicketStoreEndToEnd drives the full chain Minter → CASStore → statecas → CreateIfAbsent: a
// ticket validates once, then a replay is refused via the state-backed store.
func TestTicketStoreEndToEnd(t *testing.T) {
	client := newFakeStateClient()
	store := arrowticket.NewCASStore(New(client, time.Second), "tickets/used/")
	m := arrowticket.NewMinterWithStore([]byte("producer-key"), store)

	tk, err := m.Mint("stream-1", "rat-format", "tenantA", time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := m.Validate(tk, "stream-1", "rat-format", "tenantA"); err != nil {
		t.Fatalf("first validate rejected: %v", err)
	}
	if err := m.Validate(tk, "stream-1", "rat-format", "tenantA"); err != arrowticket.ErrReplay {
		t.Fatalf("replay via the state-backed store = %v, want ErrReplay", err)
	}
}

// TestTicketStoreSharedAcrossMinters is the gap-#7 property over the REAL state-CAS adapter: two
// minters (a restart / second replica) sharing one state backend can't both redeem a ticket.
func TestTicketStoreSharedAcrossMinters(t *testing.T) {
	client := newFakeStateClient() // the SHARED backend
	key := []byte("producer-key")
	store := func() *arrowticket.CASStore { return arrowticket.NewCASStore(New(client, time.Second), "tickets/used/") }
	a := arrowticket.NewMinterWithStore(key, store())
	b := arrowticket.NewMinterWithStore(key, store()) // restarted / second-replica producer

	tk, err := a.Mint("stream-1", "rat-format", "tenantA", time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := a.Validate(tk, "stream-1", "rat-format", "tenantA"); err != nil {
		t.Fatalf("first redemption (A) rejected: %v", err)
	}
	if err := b.Validate(tk, "stream-1", "rat-format", "tenantA"); err != arrowticket.ErrReplay {
		t.Fatalf("replay via B (shared state store) = %v, want ErrReplay", err)
	}
}
