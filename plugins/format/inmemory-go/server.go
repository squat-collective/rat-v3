// server.go — the FormatService gRPC implementation.
//
// Implements the five format/v1 RPCs against the in-memory store, honoring the
// wire contract: each mutating RPC pulls rows from its caller-hosted source
// ArrowStream and returns a WriteResult; Resolve returns a producer-hosted
// ArrowStream the caller pulls from. RequestContext is accepted on every call
// (the reference does not forge/trust identity — it just threads the context, as
// a real plugin would before the core-mediated gateway stamps it).
package main

import (
	"context"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	formatv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/format/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// formatServer implements formatv1.FormatServiceServer.
type formatServer struct {
	formatv1.UnimplementedFormatServiceServer
	store   *store
	streams *streamRegistry
}

func newServer() *formatServer {
	return &formatServer{store: newStore(), streams: newStreamRegistry()}
}

// tableID extracts the identifier from a TableRef, rejecting an empty ref with
// INVALID_ARGUMENT (a transport-level failure per the error-model convention).
func tableID(t *commonv1.TableRef) (string, error) {
	if t == nil || t.GetIdentifier() == "" {
		return "", status.Error(codes.InvalidArgument, "table.identifier is required")
	}
	return t.GetIdentifier(), nil
}

func writeResult(rows, snapshot int64) *commonv1.WriteResult {
	return &commonv1.WriteResult{
		RowsAffected: &rows,
		SnapshotId:   snapshotID(snapshot),
	}
}

// Resolve — rat://format/v1/scan. Returns a producer-hosted ArrowStream the
// caller pulls matching rows from.
func (s *formatServer) Resolve(_ context.Context, req *formatv1.ResolveRequest) (*formatv1.ResolveResponse, error) {
	id, err := tableID(req.GetTable())
	if err != nil {
		return nil, err
	}
	rows := s.store.scan(id, req.GetColumns())
	return &formatv1.ResolveResponse{Stream: s.streams.put(rows)}, nil
}

// Append — rat://format/v1/append.
func (s *formatServer) Append(_ context.Context, req *formatv1.AppendRequest) (*formatv1.AppendResponse, error) {
	id, err := tableID(req.GetTable())
	if err != nil {
		return nil, err
	}
	rows := s.streams.pull(req.GetSource())
	n, snap := s.store.append(id, rows)
	return &formatv1.AppendResponse{Result: writeResult(n, snap)}, nil
}

// Merge — rat://format/v1/merge. merge_keys are required (you can't upsert
// without a match key).
func (s *formatServer) Merge(_ context.Context, req *formatv1.MergeRequest) (*formatv1.MergeResponse, error) {
	id, err := tableID(req.GetTable())
	if err != nil {
		return nil, err
	}
	if len(req.GetMergeKeys()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "merge_keys is required for Merge")
	}
	rows := s.streams.pull(req.GetSource())
	n, snap := s.store.merge(id, req.GetMergeKeys(), rows)
	return &formatv1.MergeResponse{Result: writeResult(n, snap)}, nil
}

// Overwrite — rat://format/v1/overwrite.
func (s *formatServer) Overwrite(_ context.Context, req *formatv1.OverwriteRequest) (*formatv1.OverwriteResponse, error) {
	id, err := tableID(req.GetTable())
	if err != nil {
		return nil, err
	}
	rows := s.streams.pull(req.GetSource())
	n, snap := s.store.overwrite(id, rows)
	return &formatv1.OverwriteResponse{Result: writeResult(n, snap)}, nil
}

// Maintain — rat://format/v1/maintain. No-op upkeep; bumps the snapshot.
func (s *formatServer) Maintain(_ context.Context, req *formatv1.MaintainRequest) (*formatv1.MaintainResponse, error) {
	id, err := tableID(req.GetTable())
	if err != nil {
		return nil, err
	}
	snap := s.store.maintain(id)
	// rows_affected is genuinely unknown for maintenance → leave it absent
	// (proto3 optional), per WriteResult's documented semantics.
	return &formatv1.MaintainResponse{Result: &commonv1.WriteResult{SnapshotId: snapshotID(snap)}}, nil
}
