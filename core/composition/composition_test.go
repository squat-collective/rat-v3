package composition

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/registry"
	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

const callMetaHeader = "rat-callmeta-bin" // ADR-007 wire constant

const (
	sourceTable = "warehouse.sales.orders"
	outputTable = "warehouse.sales.summary"
)

// ── fake providers: honor the frozen RPCs + the C1 idempotency contract ──────

type fakeCatalog struct {
	catalogv1.UnimplementedCatalogServiceServer
	mu          sync.Mutex
	committed   map[string]string // identifier -> current committed snapshot
	commitByKey map[string]string // idempotency_key -> snapshot (idempotent replay)
	commitCount map[string]int    // identifier -> # of real (non-replay) commits
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{committed: map[string]string{}, commitByKey: map[string]string{}, commitCount: map[string]int{}}
}

func (c *fakeCatalog) GetTable(_ context.Context, req *catalogv1.GetTableRequest) (*catalogv1.GetTableResponse, error) {
	return &catalogv1.GetTableResponse{Table: &commonv1.TableRef{Identifier: req.GetIdentifier(), Uri: "mem://" + req.GetIdentifier()}}, nil
}

func (c *fakeCatalog) RegisterTable(_ context.Context, req *catalogv1.RegisterTableRequest) (*catalogv1.RegisterTableResponse, error) {
	// Idempotent by contract: re-registering returns the existing ref.
	return &catalogv1.RegisterTableResponse{Table: &commonv1.TableRef{Identifier: req.GetIdentifier(), Uri: "mem://" + req.GetIdentifier()}}, nil
}

func (c *fakeCatalog) CommitTable(_ context.Context, req *catalogv1.CommitTableRequest) (*catalogv1.CommitTableResponse, error) {
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

type fakeFormat struct {
	formatv1.UnimplementedFormatServiceServer
	mu         sync.Mutex
	seq        int
	writeByKey map[string]string // idempotency_key -> snapshot (idempotent replay)
	writeCount map[string]int    // table identifier -> # of real (non-replay) writes
}

func newFakeFormat() *fakeFormat {
	return &fakeFormat{writeByKey: map[string]string{}, writeCount: map[string]int{}}
}

func (f *fakeFormat) Overwrite(_ context.Context, req *formatv1.OverwriteRequest) (*formatv1.OverwriteResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k := req.GetIdempotencyKey(); k != "" {
		if snap, ok := f.writeByKey[k]; ok {
			// At-least-once replay: a no-op returning the ORIGINAL result (C1).
			return &formatv1.OverwriteResponse{Result: &commonv1.WriteResult{SnapshotId: proto.String(snap), AlreadyApplied: true, RowsAffected: proto.Int64(0)}}, nil
		}
	}
	f.seq++
	snap := fmt.Sprintf("snap-%d", f.seq)
	f.writeCount[req.GetTable().GetIdentifier()]++
	if k := req.GetIdempotencyKey(); k != "" {
		f.writeByKey[k] = snap
	}
	return &formatv1.OverwriteResponse{Result: &commonv1.WriteResult{SnapshotId: proto.String(snap), AlreadyApplied: false, RowsAffected: proto.Int64(42)}}, nil
}

// ── harness: registry + gateway in front of the fake providers ───────────────

func bufServer(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func callerCtx(caller string) context.Context {
	rc := &commonv1.RequestContext{
		Trace: &commonv1.TraceContext{
			Traceparent:   "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01",
			CorrelationId: "corr-comp",
		},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: "t1"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(context.Background(), callMetaHeader, string(b))
}

type harness struct {
	client  corev1.CapabilityInvokeServiceClient
	catalog *fakeCatalog
	format  *fakeFormat
	audit   *gateway.MemAuditor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cat, fmtp := newFakeCatalog(), newFakeFormat()
	catConn := bufServer(t, func(s *grpc.Server) { catalogv1.RegisterCatalogServiceServer(s, cat) })
	fmtConn := bufServer(t, func(s *grpc.Server) { formatv1.RegisterFormatServiceServer(s, fmtp) })

	// Manifests: a strategy that declares exactly the four caps the pipeline uses,
	// a catalog providing the bookkeeping caps, a format providing the write caps.
	strategy := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-comp-strategy"},
		Requires: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://format/v1/overwrite", "rat://catalog/v1/commit-table")}
	catalogM := &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-comp-catalog"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/merge-branch")}
	formatM := &manifest.Manifest{Kind: "format", Metadata: manifest.Metadata{Name: "rat-comp-format"},
		Provides: caps("rat://format/v1/overwrite", "rat://format/v1/scan", "rat://format/v1/append", "rat://format/v1/merge")}

	reg, err := registry.New([]*manifest.Manifest{strategy, catalogM, formatM})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	audit := &gateway.MemAuditor{}
	gw := gateway.New(reg, map[string]*grpc.ClientConn{"rat-comp-catalog": catConn, "rat-comp-format": fmtConn}, audit,
		catalogv1.File_rat_catalog_v1_catalog_proto, formatv1.File_rat_format_v1_format_proto)
	gwConn := bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) })
	return &harness{client: corev1.NewCapabilityInvokeServiceClient(gwConn), catalog: cat, format: fmtp, audit: audit}
}

func caps(uris ...string) []manifest.CapabilityRef {
	out := make([]manifest.CapabilityRef, len(uris))
	for i, u := range uris {
		out[i] = manifest.CapabilityRef{Capability: u}
	}
	return out
}

// invoke marshals req, calls the gateway capability, and unmarshals the result.
func invoke(ctx context.Context, client corev1.CapabilityInvokeServiceClient, capURI string, req, resp proto.Message) error {
	payload, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	out, err := client.Invoke(ctx, &corev1.InvokeRequest{Capability: capURI, Payload: payload})
	if err != nil {
		return err
	}
	if resp != nil {
		return proto.Unmarshal(out.GetResult(), resp)
	}
	return nil
}

// runPipeline plays the strategy's capability sequence through the gateway. If
// crashAfterWrite, it returns right after the format write (before commit-table),
// modelling a crash mid-strategy. Returns the write snapshot + whether it committed.
func runPipeline(t *testing.T, h *harness, runID string, crashAfterWrite bool) (writeSnap, finalSnap string, committed bool) {
	t.Helper()
	ctx := callerCtx("rat-comp-strategy")

	var gt catalogv1.GetTableResponse
	if err := invoke(ctx, h.client, "rat://catalog/v1/get-table", &catalogv1.GetTableRequest{Identifier: sourceTable}, &gt); err != nil {
		t.Fatalf("get-table: %v", err)
	}
	if gt.GetTable().GetIdentifier() != sourceTable {
		t.Fatalf("get-table returned %q, want %q", gt.GetTable().GetIdentifier(), sourceTable)
	}

	var rt catalogv1.RegisterTableResponse
	if err := invoke(ctx, h.client, "rat://catalog/v1/register-table", &catalogv1.RegisterTableRequest{Identifier: outputTable}, &rt); err != nil {
		t.Fatalf("register-table: %v", err)
	}

	var ow formatv1.OverwriteResponse
	owReq := &formatv1.OverwriteRequest{
		Table:          rt.GetTable(),
		Source:         &commonv1.ArrowStream{Transport: commonv1.ArrowTransport_ARROW_TRANSPORT_FLIGHT, Role: commonv1.ArrowStreamRole_ARROW_STREAM_ROLE_PRODUCER_HOSTED},
		IdempotencyKey: runID,
	}
	if err := invoke(ctx, h.client, "rat://format/v1/overwrite", owReq, &ow); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	writeSnap = ow.GetResult().GetSnapshotId()

	if crashAfterWrite {
		return writeSnap, "", false // crash before commit-table
	}

	var ct catalogv1.CommitTableResponse
	ctReq := &catalogv1.CommitTableRequest{Identifier: outputTable, SnapshotId: writeSnap, IdempotencyKey: runID}
	if err := invoke(ctx, h.client, "rat://catalog/v1/commit-table", ctReq, &ct); err != nil {
		t.Fatalf("commit-table: %v", err)
	}
	return writeSnap, ct.GetSnapshotId(), true
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestCompositionPipeline: the full multi-axis pipeline runs through the enforcing
// gateway; the catalog records exactly the snapshot the format produced (commit-linkage,
// ADR-010); every hop is authorized + audited.
func TestCompositionPipeline(t *testing.T) {
	h := newHarness(t)
	writeSnap, finalSnap, committed := runPipeline(t, h, "run-1", false)
	if !committed {
		t.Fatal("pipeline did not commit")
	}
	if writeSnap == "" || finalSnap != writeSnap {
		t.Errorf("commit-linkage broken: write produced %q, catalog committed %q", writeSnap, finalSnap)
	}
	if got := h.catalog.committed[outputTable]; got != writeSnap {
		t.Errorf("catalog committed snapshot = %q, want %q", got, writeSnap)
	}
	if got := h.format.writeCount[outputTable]; got != 1 {
		t.Errorf("format wrote %d times, want 1", got)
	}
	// Four authorized hops, all allowed, routed to the right providers.
	recs := h.audit.Records()
	if len(recs) != 4 {
		t.Fatalf("audit has %d records, want 4 (get-table, register, overwrite, commit)", len(recs))
	}
	for _, r := range recs {
		if !r.Allowed {
			t.Errorf("hop %q denied: %s", r.Capability, r.Reason)
		}
	}
}

// TestCrashMidStrategyRecovers (C1): a strategy that crashes after the format write
// but before commit-table recovers on an at-least-once re-run with the SAME run id —
// the replayed write is a no-op (already_applied) so the data is NOT written twice,
// and the table commits exactly once. This is the crash-mid-strategy exit case
// (ADR-014 §5) the existing idempotency fields (ADR-012) must make safe.
func TestCrashMidStrategyRecovers(t *testing.T) {
	h := newHarness(t)

	// Attempt 1: crashes after the write, before commit.
	writeSnap1, _, committed1 := runPipeline(t, h, "run-7", true)
	if committed1 {
		t.Fatal("attempt 1 should have crashed before commit")
	}
	if writeSnap1 == "" {
		t.Fatal("attempt 1 produced no write snapshot")
	}

	// Attempt 2: full re-run, same run id (the reconciler retry).
	writeSnap2, finalSnap, committed2 := runPipeline(t, h, "run-7", false)
	if !committed2 {
		t.Fatal("attempt 2 did not commit")
	}

	// The replayed write returned the ORIGINAL snapshot, and did NOT write again.
	if writeSnap2 != writeSnap1 {
		t.Errorf("replay produced a new snapshot %q, want the original %q (not idempotent!)", writeSnap2, writeSnap1)
	}
	if got := h.format.writeCount[outputTable]; got != 1 {
		t.Errorf("format wrote %d times across crash+retry, want exactly 1 (double-apply!)", got)
	}
	if got := h.catalog.commitCount[outputTable]; got != 1 {
		t.Errorf("catalog committed %d times, want exactly 1", got)
	}
	if finalSnap != writeSnap1 {
		t.Errorf("final committed snapshot = %q, want %q", finalSnap, writeSnap1)
	}
}

// TestCompositionDeniesUndeclaredMidPipeline (C5): C5 holds inside the composition,
// not just in isolation — the strategy declares overwrite, not merge, so a merge
// attempt is denied by the gateway even though the format provider offers it.
func TestCompositionDeniesUndeclaredMidPipeline(t *testing.T) {
	h := newHarness(t)
	ctx := callerCtx("rat-comp-strategy")
	err := invoke(ctx, h.client, "rat://format/v1/merge",
		&formatv1.MergeRequest{Table: &commonv1.TableRef{Identifier: outputTable}, MergeKeys: []string{"id"}}, &formatv1.MergeResponse{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("merge (undeclared) code = %v, want PermissionDenied", status.Code(err))
	}
}
