// harness_test.go — the conformance/golden-data harness for the format/v1 axis.
//
// This is the ADR-003 forcing function: it drives the FormatService through its
// full lifecycle (append → scan → merge → overwrite → maintain) over a REAL
// in-process gRPC connection, asserting behavior against golden expectations.
// The same vectors must pass for a second, independent format impl (e.g.
// inmemory-py) before the format contract can freeze.
//
// Running over real gRPC (not direct method calls) is deliberate: it exercises
// the actual wire — serialization of TableRef/ArrowStream/WriteResult, the
// RequestContext envelope, the per-RPC request/response messages — which is what
// the contract review (reviews/06) said only a real implementation can validate.
package main

import (
	"context"
	"net"
	"testing"
	"time"

	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const tbl = "warehouse.sales.orders"

// dialInProc spins the server on an in-memory bufconn listener and returns a
// connected client + the server (so the test can stage source streams on it).
func dialInProc(t *testing.T) (formatv1.FormatServiceClient, *formatServer) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	impl := newServer()
	formatv1.RegisterFormatServiceServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return formatv1.NewFormatServiceClient(conn), impl
}

func ctx(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func ref() *commonv1.TableRef { return &commonv1.TableRef{Identifier: tbl} }

// stageSource stashes rows in the server's stream registry and returns a
// caller-hosted ArrowStream descriptor pointing at them (what a real caller would
// hand to a mutating RPC).
func stageSource(impl *formatServer, rows []row) *commonv1.ArrowStream {
	return impl.streams.put(rows)
}

// scanAll resolves the table and returns the rows the producer-hosted stream
// yields.
func scanAll(t *testing.T, c formatv1.FormatServiceClient, impl *formatServer) []row {
	t.Helper()
	cx, cancel := ctx(t)
	defer cancel()
	resp, err := c.Resolve(cx, &formatv1.ResolveRequest{Context: &commonv1.RequestContext{}, Table: ref()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return impl.streams.pull(resp.GetStream())
}

func TestFormatLifecycle_GoldenVectors(t *testing.T) {
	c, impl := dialInProc(t)
	cx, cancel := ctx(t)
	defer cancel()

	// 1. Append two rows.
	ap, err := c.Append(cx, &formatv1.AppendRequest{
		Context: &commonv1.RequestContext{},
		Table:   ref(),
		Source: stageSource(impl, []row{
			{"id": "1", "name": "alice"},
			{"id": "2", "name": "bob"},
		}),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := ap.GetResult().GetRowsAffected(); got != 2 {
		t.Fatalf("Append rows_affected = %d, want 2", got)
	}

	// 2. Scan → 2 rows.
	if got := len(scanAll(t, c, impl)); got != 2 {
		t.Fatalf("after Append, scan = %d rows, want 2", got)
	}

	// 3. Merge: upsert id=2 (update bob→robert) + insert id=3.
	mg, err := c.Merge(cx, &formatv1.MergeRequest{
		Context:   &commonv1.RequestContext{},
		Table:     ref(),
		MergeKeys: []string{"id"},
		Source: stageSource(impl, []row{
			{"id": "2", "name": "robert"},
			{"id": "3", "name": "carol"},
		}),
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := mg.GetResult().GetRowsAffected(); got != 2 {
		t.Fatalf("Merge rows_affected = %d, want 2", got)
	}
	rows := scanAll(t, c, impl)
	if len(rows) != 3 {
		t.Fatalf("after Merge, scan = %d rows, want 3", len(rows))
	}
	// id=2 must now read "robert" (updated, not duplicated).
	for _, r := range rows {
		if r["id"] == "2" && r["name"] != "robert" {
			t.Fatalf("merged id=2 name = %q, want robert", r["name"])
		}
	}

	// 4. Overwrite: replace everything with one row.
	ov, err := c.Overwrite(cx, &formatv1.OverwriteRequest{
		Context: &commonv1.RequestContext{},
		Table:   ref(),
		Source:  stageSource(impl, []row{{"id": "9", "name": "zoe"}}),
	})
	if err != nil {
		t.Fatalf("Overwrite: %v", err)
	}
	if got := ov.GetResult().GetRowsAffected(); got != 1 {
		t.Fatalf("Overwrite rows_affected = %d, want 1", got)
	}
	if got := len(scanAll(t, c, impl)); got != 1 {
		t.Fatalf("after Overwrite, scan = %d rows, want 1", got)
	}

	// 5. Maintain: succeeds; rows_affected is unknown (absent), snapshot present.
	mn, err := c.Maintain(cx, &formatv1.MaintainRequest{Context: &commonv1.RequestContext{}, Table: ref()})
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if mn.GetResult().GetSnapshotId() == "" {
		t.Fatalf("Maintain snapshot_id empty, want set")
	}
}

func TestResolve_EmptyTableRef_InvalidArgument(t *testing.T) {
	c, _ := dialInProc(t)
	cx, cancel := ctx(t)
	defer cancel()
	_, err := c.Resolve(cx, &formatv1.ResolveRequest{Context: &commonv1.RequestContext{}, Table: &commonv1.TableRef{}})
	if err == nil {
		t.Fatal("Resolve with empty table.identifier: want error, got nil")
	}
}

func TestMerge_NoMergeKeys_InvalidArgument(t *testing.T) {
	c, impl := dialInProc(t)
	cx, cancel := ctx(t)
	defer cancel()
	_, err := c.Merge(cx, &formatv1.MergeRequest{
		Context: &commonv1.RequestContext{},
		Table:   ref(),
		Source:  stageSource(impl, []row{{"id": "1"}}),
	})
	if err == nil {
		t.Fatal("Merge without merge_keys: want error, got nil")
	}
}
