// server.go — the EngineService gRPC implementation.
//
// Implements the three engine/v1 RPCs against the in-memory mini-SQL store:
//   - Execute (rat://engine/v1/execute): CREATE / INSERT for effect → WriteResult.
//   - Query   (rat://engine/v1/query):   SELECT → producer-hosted ArrowStream.
//   - Preview (rat://engine/v1/preview): SELECT, bounded by `limit` → ArrowStream.
//
// RequestContext is NOT a field here (ADR-007): call context rides in the
// rat-callmeta-bin metadata header. This reference does not need identity, so it
// simply ignores it — a conformant choice for a plugin that performs no per-caller
// authorization of its own.
package main

import (
	"context"

	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
	enginev1 "github.com/le-squat/rat/gen/rat/engine/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type engineServer struct {
	enginev1.UnimplementedEngineServiceServer
	store   *store
	streams *streamRegistry
}

func newServer() *engineServer {
	return &engineServer{store: newStore(), streams: newStreamRegistry()}
}

func writeResult(rows, snapshot int64) *commonv1.WriteResult {
	return &commonv1.WriteResult{RowsAffected: &rows, SnapshotId: snapshotID(snapshot)}
}

// Execute runs a CREATE or INSERT for effect.
func (s *engineServer) Execute(_ context.Context, req *enginev1.ExecuteRequest) (*enginev1.ExecuteResponse, error) {
	st, err := parseSQL(req.GetSql())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	switch st.kind {
	case "create":
		snap := s.store.create(st.table, st.cols)
		return &enginev1.ExecuteResponse{Result: writeResult(0, snap)}, nil
	case "insert":
		n, snap, err := s.store.insert(st.table, st.vals)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return &enginev1.ExecuteResponse{Result: writeResult(n, snap)}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "Execute requires a CREATE or INSERT statement")
	}
}

// Query runs a SELECT and returns a producer-hosted ArrowStream of the results.
func (s *engineServer) Query(_ context.Context, req *enginev1.QueryRequest) (*enginev1.QueryResponse, error) {
	rows, err := s.runSelect(req.GetSql(), 0)
	if err != nil {
		return nil, err
	}
	return &enginev1.QueryResponse{Stream: s.streams.put(rows)}, nil
}

// Preview runs a SELECT bounded by req.limit (a sample for UI/inspection).
func (s *engineServer) Preview(_ context.Context, req *enginev1.PreviewRequest) (*enginev1.PreviewResponse, error) {
	rows, err := s.runSelect(req.GetSql(), req.GetLimit())
	if err != nil {
		return nil, err
	}
	return &enginev1.PreviewResponse{Stream: s.streams.put(rows)}, nil
}

// runSelect parses + executes a SELECT, applying the effective row limit (the min
// of the SQL LIMIT, if any, and previewLimit, if > 0).
func (s *engineServer) runSelect(sql string, previewLimit int64) ([]row, error) {
	st, err := parseSQL(sql)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if st.kind != "select" {
		return nil, status.Error(codes.InvalidArgument, "Query/Preview requires a SELECT statement")
	}
	rows, err := s.store.selectRows(st)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	limit := int64(-1)
	if st.hasLimit {
		limit = st.limit
	}
	if previewLimit > 0 && (limit < 0 || previewLimit < limit) {
		limit = previewLimit
	}
	if limit >= 0 && int64(len(rows)) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
