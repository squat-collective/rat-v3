// createifabsent_test.go — ADR-049 atomic create-if-absent: the conformance CONCURRENCY vector
// (N simultaneous creates of one key → exactly one COMMITTED) + the RPC-level COMMITTED→CONFLICT
// contract. The concurrency property is the whole point — an "atomic" create without a race test is
// honor-system (ADR-049 §3).
package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	statev1 "github.com/squat-collective/rat-v3/gen/rat/state/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCreateIfAbsentAtomicSingleCreator: 32 goroutines race to create ONE key; exactly one wins.
// This is the property the lease bootstrap (ADR-043 Q01) + the Arrow-ticket store (ADR-048) depend on.
func TestCreateIfAbsentAtomicSingleCreator(t *testing.T) {
	s := newStore()
	const n = 32
	var created int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := s.createIfAbsent("lease/rat-serve", []byte("v")); ok {
				atomic.AddInt32(&created, 1)
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("concurrent createIfAbsent of one key: %d creators won, want exactly 1", created)
	}
}

// TestCreateIfAbsentRPC: first create COMMITTED (new rev); second CONFLICT at the EXISTING rev with the
// original value preserved (no overwrite); a malformed key is INVALID_ARGUMENT (KEY GRAMMAR).
func TestCreateIfAbsentRPC(t *testing.T) {
	srv := newServer()
	ctx := context.Background()

	r1, err := srv.CreateIfAbsent(ctx, &statev1.CreateIfAbsentRequest{Key: "lease/x", Value: []byte("first")})
	if err != nil || r1.GetOutcome() != statev1.PutOutcome_PUT_OUTCOME_COMMITTED || r1.GetRevision() == 0 {
		t.Fatalf("first create = (%v, %+v), want COMMITTED with rev>0", err, r1)
	}

	r2, err := srv.CreateIfAbsent(ctx, &statev1.CreateIfAbsentRequest{Key: "lease/x", Value: []byte("second")})
	if err != nil || r2.GetOutcome() != statev1.PutOutcome_PUT_OUTCOME_CONFLICT {
		t.Fatalf("second create = (%v, %+v), want CONFLICT", err, r2)
	}
	if r2.GetRevision() != r1.GetRevision() {
		t.Errorf("CONFLICT revision = %d, want the existing %d", r2.GetRevision(), r1.GetRevision())
	}

	g, err := srv.Get(ctx, &statev1.GetRequest{Key: "lease/x"})
	if err != nil || string(g.GetValue()) != "first" {
		t.Errorf("value after conflicting create = %q, want %q (no overwrite)", g.GetValue(), "first")
	}

	if _, err := srv.CreateIfAbsent(ctx, &statev1.CreateIfAbsentRequest{Key: "../escape", Value: nil}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("malformed key code = %v, want InvalidArgument", status.Code(err))
	}
}
