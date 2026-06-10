// Package statecas backs the Arrow-ticket single-use store with a state/v1 backend's atomic
// create-if-absent (ADR-049), so a producer's replay defense survives RESTART + REPLICAS (gap #7 /
// ADR-048): the consumed-ticket set lives in the SHARED state axis, not a per-process map. It is the
// producer-side bridge from arrowticket.SingleUseCAS onto state/v1 — the ticket analogue of the
// lease's state-CAS adapter (ADR-043/049). arrowticket stays gRPC/proto-free; this adapter carries
// the dependency.
package statecas

import (
	"context"
	"fmt"
	"time"

	"github.com/squat-collective/rat-v3/core/arrowticket"
	statev1 "github.com/squat-collective/rat-v3/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Client is the slice of rat.state.v1.StateServiceClient the adapter needs — the real generated
// client satisfies it. Narrow so a test fakes ONE method, not the whole service.
type Client interface {
	CreateIfAbsent(ctx context.Context, in *statev1.CreateIfAbsentRequest, opts ...grpc.CallOption) (*statev1.CreateIfAbsentResponse, error)
}

// CAS implements arrowticket.SingleUseCAS over a state/v1 backend's CreateIfAbsent.
type CAS struct {
	client      Client
	callTimeout time.Duration
}

var _ arrowticket.SingleUseCAS = (*CAS)(nil)

// New returns a state-backed SingleUseCAS. callTimeout <= 0 defaults to 3s (a hung backend surfaces
// as a fail-closed error, never a silent admit). Wrap it in arrowticket.NewCASStore to get a
// SingleUseStore for a Minter (NewMinterWithStore).
func New(client Client, callTimeout time.Duration) *CAS {
	if callTimeout <= 0 {
		callTimeout = 3 * time.Second
	}
	return &CAS{client: client, callTimeout: callTimeout}
}

// PutIfAbsent records key via the atomic state/v1 CreateIfAbsent: COMMITTED → created (first use);
// CONFLICT → already recorded (a replay). An error (UNKNOWN / transport / the backend lacking the
// optional capability) FAILS CLOSED — an unconfirmable single-use check must not silently admit a
// possible replay, so the ticket is refused rather than honored.
func (a *CAS) PutIfAbsent(key string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.callTimeout)
	defer cancel()
	r, err := a.client.CreateIfAbsent(ctx, &statev1.CreateIfAbsentRequest{Key: key, Value: []byte("1")})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return false, fmt.Errorf("statecas: backend has no create-if-absent (state/v1 ADR-049) — cannot back a shared ticket store")
		}
		return false, err
	}
	switch r.GetOutcome() {
	case statev1.PutOutcome_PUT_OUTCOME_COMMITTED:
		return true, nil
	case statev1.PutOutcome_PUT_OUTCOME_CONFLICT:
		return false, nil
	default: // UNKNOWN / UNSPECIFIED → unconfirmed; fail closed
		return false, fmt.Errorf("statecas: create-if-absent unconfirmed (outcome=%s)", r.GetOutcome())
	}
}
