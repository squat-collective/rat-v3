// Package formatsvc is the promoted composition-test format fake: a single
// FormatService implementation honoring the frozen RPCs + the C1 at-least-once
// idempotency contract (ADR-012).
//
// Like catalogsvc, it lives as an importable package so the SAME implementation
// backs both the in-process composition test (bufconn) and the launched
// formatplugin binary — one impl, no divergence.
//
// Overwrite embeds the serving process's PID in the free-form WriteResult
// snapshot_id (snap-<seq>-pid=<N>), so a launched caller can prove the write ran
// in a distinct OS process. The PID rides along verbatim through commit-table, so
// commit-linkage (write snapshot == committed snapshot) is unaffected.
package formatsvc

import (
	"context"
	"fmt"
	"os"
	"sync"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	formatv1 "github.com/squat-collective/rat-v3/gen/rat/format/v1"
	"google.golang.org/protobuf/proto"
)

// Server is the format fake. Safe for concurrent use.
type Server struct {
	formatv1.UnimplementedFormatServiceServer
	mu         sync.Mutex
	seq        int
	writeByKey map[string]string // idempotency_key -> snapshot (idempotent replay)
	writeCount map[string]int    // table identifier -> # of real (non-replay) writes
}

// New returns an empty format.
func New() *Server {
	return &Server{writeByKey: map[string]string{}, writeCount: map[string]int{}}
}

// Overwrite replaces the target's data. Idempotent on idempotency_key: an
// at-least-once replay is a no-op returning the ORIGINAL snapshot with
// already_applied=true and rows_affected=0 (C1). New snapshots carry the serving
// PID so a launched caller can attribute the write to a distinct process.
func (f *Server) Overwrite(_ context.Context, req *formatv1.OverwriteRequest) (*formatv1.OverwriteResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k := req.GetIdempotencyKey(); k != "" {
		if snap, ok := f.writeByKey[k]; ok {
			return &formatv1.OverwriteResponse{Result: &commonv1.WriteResult{SnapshotId: proto.String(snap), AlreadyApplied: true, RowsAffected: proto.Int64(0)}}, nil
		}
	}
	f.seq++
	snap := fmt.Sprintf("snap-%d-pid=%d", f.seq, os.Getpid())
	f.writeCount[req.GetTable().GetIdentifier()]++
	if k := req.GetIdempotencyKey(); k != "" {
		f.writeByKey[k] = snap
	}
	return &formatv1.OverwriteResponse{Result: &commonv1.WriteResult{SnapshotId: proto.String(snap), AlreadyApplied: false, RowsAffected: proto.Int64(42)}}, nil
}

// WriteCount returns the number of real (non-replay) writes for identifier
// (in-process test introspection; meaningless across a launched boundary).
func (f *Server) WriteCount(identifier string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeCount[identifier]
}
