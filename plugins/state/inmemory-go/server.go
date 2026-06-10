// server.go — the StateService gRPC implementation.
//
// Get/Put/List unary; Watch server-streaming (mediated via core InvokeServerStream,
// ADR-008). Every key/prefix is validated against the KEY GRAMMAR (grammar.go)
// → INVALID_ARGUMENT on violation. Put returns the PutOutcome enum (COMMITTED /
// CONFLICT) — a CAS conflict is a normal outcome, NOT a gRPC error. RequestContext
// is NOT a field (ADR-007); this reference ignores identity.
package main

import (
	"context"

	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
)

type stateServer struct {
	statev1.UnimplementedStateServiceServer
	store *store
}

func newServer() *stateServer { return &stateServer{store: newStore()} }

func (s *stateServer) Get(_ context.Context, req *statev1.GetRequest) (*statev1.GetResponse, error) {
	if err := validateKey(req.GetKey(), false); err != nil {
		return nil, err
	}
	found, value, rev := s.store.get(req.GetKey())
	return &statev1.GetResponse{Found: found, Value: value, Revision: rev}, nil
}

func (s *stateServer) Put(_ context.Context, req *statev1.PutRequest) (*statev1.PutResponse, error) {
	if err := validateKey(req.GetKey(), false); err != nil {
		return nil, err
	}
	committed, rev := s.store.put(req.GetKey(), req.GetValue(), req.GetIfRevision())
	outcome := statev1.PutOutcome_PUT_OUTCOME_COMMITTED
	if !committed {
		outcome = statev1.PutOutcome_PUT_OUTCOME_CONFLICT
	}
	return &statev1.PutResponse{Outcome: outcome, Revision: rev}, nil
}

// CreateIfAbsent atomically creates the key only if absent (ADR-049). COMMITTED == created;
// CONFLICT == the key already existed (no write). The atomicity (exactly-one-creator under
// contention) lives in the store; an in-memory backend never returns UNKNOWN.
func (s *stateServer) CreateIfAbsent(_ context.Context, req *statev1.CreateIfAbsentRequest) (*statev1.CreateIfAbsentResponse, error) {
	if err := validateKey(req.GetKey(), false); err != nil {
		return nil, err
	}
	created, rev := s.store.createIfAbsent(req.GetKey(), req.GetValue())
	outcome := statev1.PutOutcome_PUT_OUTCOME_COMMITTED
	if !created {
		outcome = statev1.PutOutcome_PUT_OUTCOME_CONFLICT
	}
	return &statev1.CreateIfAbsentResponse{Outcome: outcome, Revision: rev}, nil
}

func (s *stateServer) List(_ context.Context, req *statev1.ListRequest) (*statev1.ListResponse, error) {
	if err := validateKey(req.GetPrefix(), true); err != nil {
		return nil, err
	}
	return &statev1.ListResponse{Keys: s.store.list(req.GetPrefix())}, nil
}

// Watch streams the backlog of changes under the prefix in revision order, then
// ends the stream. A real Watch would stay open + stream live changes; this
// reference bounds it for deterministic conformance (the ordered-replay is the
// property under test). Routes through the ADR-008 InvokeServerStream relay.
func (s *stateServer) Watch(req *statev1.WatchRequest, stream statev1.StateService_WatchServer) error {
	if err := validateKey(req.GetPrefix(), true); err != nil {
		return err
	}
	for _, e := range s.store.watchBacklog(req.GetPrefix(), req.GetFromRevision()) {
		if err := stream.Send(&statev1.WatchResponse{
			Type:     statev1.WatchResponse_TYPE_PUT,
			Key:      e.key,
			Value:    e.value,
			Revision: e.revision,
		}); err != nil {
			return err
		}
	}
	return nil
}
