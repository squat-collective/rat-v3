// Package catalogsvc is the promoted composition-test catalog fake: a single
// CatalogService implementation honoring the frozen RPCs + the C1 idempotency /
// ADR-010 commit-linkage contract.
//
// It exists as an importable package (not inlined in a _test.go) so the SAME
// implementation backs both the in-process composition test (bufconn) and the
// launched catalogplugin binary (testplugins/catalogplugin). One impl, no
// in-process-vs-binary divergence — that is the point of "promoting" the fake.
//
// Each response embeds the serving process's PID in a free-form field
// (TableRef.uri carries ?pid=<N>), exactly as stateplugin tags GetResponse.value,
// so a caller can prove the work ran in a distinct OS process when launched.
package catalogsvc

import (
	"context"
	"fmt"
	"os"
	"sync"

	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the catalog fake. Safe for concurrent use.
type Server struct {
	catalogv1.UnimplementedCatalogServiceServer
	mu          sync.Mutex
	committed   map[string]string // identifier -> current committed snapshot
	commitByKey map[string]string // idempotency_key -> snapshot (idempotent replay)
	commitCount map[string]int    // identifier -> # of real (non-replay) commits
}

// New returns an empty catalog.
func New() *Server {
	return &Server{committed: map[string]string{}, commitByKey: map[string]string{}, commitCount: map[string]int{}}
}

// ref tags the table URI with the serving PID so a launched caller can prove the
// call was served by a distinct OS process (mirrors stateplugin's pid tagging).
func ref(identifier string) *commonv1.TableRef {
	return &commonv1.TableRef{Identifier: identifier, Uri: fmt.Sprintf("mem://%s?pid=%d", identifier, os.Getpid())}
}

// GetTable resolves an identifier to a TableRef (URI carries the serving PID).
func (c *Server) GetTable(_ context.Context, req *catalogv1.GetTableRequest) (*catalogv1.GetTableResponse, error) {
	return &catalogv1.GetTableResponse{Table: ref(req.GetIdentifier())}, nil
}

// RegisterTable is idempotent by contract: re-registering returns the existing ref.
func (c *Server) RegisterTable(_ context.Context, req *catalogv1.RegisterTableRequest) (*catalogv1.RegisterTableResponse, error) {
	return &catalogv1.RegisterTableResponse{Table: ref(req.GetIdentifier())}, nil
}

// CommitTable records the snapshot a format write produced (commit-linkage,
// ADR-010). Idempotent on idempotency_key: a replay returns the original snapshot
// with already_applied=true and does NOT re-commit (C1, ADR-012).
func (c *Server) CommitTable(_ context.Context, req *catalogv1.CommitTableRequest) (*catalogv1.CommitTableResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k := req.GetIdempotencyKey(); k != "" {
		if snap, ok := c.commitByKey[k]; ok {
			return &catalogv1.CommitTableResponse{SnapshotId: snap, AlreadyApplied: true}, nil
		}
	}
	if req.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "commit-table: snapshot_id is required")
	}
	c.committed[req.GetIdentifier()] = req.GetSnapshotId()
	c.commitCount[req.GetIdentifier()]++
	if k := req.GetIdempotencyKey(); k != "" {
		c.commitByKey[k] = req.GetSnapshotId()
	}
	return &catalogv1.CommitTableResponse{SnapshotId: req.GetSnapshotId(), AlreadyApplied: false}, nil
}

// Committed returns the snapshot currently recorded for identifier (in-process
// test introspection; meaningless across a launched process boundary).
func (c *Server) Committed(identifier string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.committed[identifier]
}

// CommitCount returns the number of real (non-replay) commits for identifier.
func (c *Server) CommitCount(identifier string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.commitCount[identifier]
}
