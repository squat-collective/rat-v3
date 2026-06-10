package main

// lease.go — leader-election backend selection for `rat serve` (gap #1 / ADR-043).
//
// The reconciler runs under a single-leader lease. SOLO (the default) uses an in-memory lease:
// one process, no external dependency. MULTI-REPLICA HA points the lease at a SHARED state-backend
// (RAT_LEASE_STATE_ADDR) so every `rat serve` replica contends on the same state/v1 key and the
// backend's linearizable CAS elects exactly one leader across the fleet. The shared backend is the
// fleet's etcd analogue — external/attached, reachable by every replica, NOT a plugin a replica
// launches (that would be circular).

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rat-dev/rat/core/lease"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// leaseCandidateID is this replica's election identity. It MUST be unique per process — two
// replicas sharing an id would be indistinguishable holders, defeating mutual exclusion — so a
// random suffix is appended even when an instance id (ADR-023) is present.
func leaseCandidateID(instance string) string {
	id := "rat-serve"
	if instance != "" {
		id += "-" + instance
	}
	return id + "-" + newPluginToken()[:8]
}

// newLeaseBackend selects the election backend. Default: the in-memory Store (solo). When
// RAT_LEASE_STATE_ADDR is set, the lease lives in that shared state-backend via state/v1 CAS
// (real multi-replica HA). RAT_LEASE_KEY overrides the lease key (default rat/lease/rat-serve).
// Returns a closer for the dialed conn (no-op for in-memory).
func newLeaseBackend() (lease.Backend, func(), error) {
	addr := os.Getenv("RAT_LEASE_STATE_ADDR")
	if addr == "" {
		return lease.NewStore(), func() {}, nil
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("dial lease state-backend %s: %w", addr, err)
	}
	key := os.Getenv("RAT_LEASE_KEY")
	if key == "" {
		key = "rat/lease/rat-serve"
	}
	backend := lease.NewStateStore(stateCAS{client: statev1.NewStateServiceClient(conn)}, key, 3*time.Second)
	return backend, func() { _ = conn.Close() }, nil
}

// stateCAS adapts a rat.state.v1 client onto lease.StateCAS, mapping PutOutcome onto the
// committed / conflict / uncertain trichotomy the lease fencing depends on (the UNKNOWN case
// becomes the err the Elector holds leadership through — AV-1).
type stateCAS struct{ client statev1.StateServiceClient }

func (a stateCAS) Get(ctx context.Context, key string) ([]byte, int64, bool, error) {
	r, err := a.client.Get(ctx, &statev1.GetRequest{Key: key})
	if err != nil {
		return nil, 0, false, err
	}
	return r.GetValue(), r.GetRevision(), r.GetFound(), nil
}

func (a stateCAS) Put(ctx context.Context, key string, value []byte, ifRevision int64) (bool, int64, error) {
	r, err := a.client.Put(ctx, &statev1.PutRequest{Key: key, Value: value, IfRevision: ifRevision})
	if err != nil {
		return false, 0, err
	}
	switch r.GetOutcome() {
	case statev1.PutOutcome_PUT_OUTCOME_COMMITTED:
		return true, r.GetRevision(), nil
	case statev1.PutOutcome_PUT_OUTCOME_CONFLICT:
		return false, r.GetRevision(), nil
	default: // UNKNOWN / UNSPECIFIED → unconfirmed; the lease treats this as "uncertain"
		return false, 0, fmt.Errorf("state put unconfirmed (outcome=%s)", r.GetOutcome())
	}
}
