// harness_test.go — conformance/golden-data harness for the engine/v1 axis.
//
// Loads the language-neutral golden vectors from
// contracts/conformance/engine-v1.json (the SAME file the Python reference loads)
// and drives EngineService through Execute/Query/Preview + error cases over real
// gRPC, routed through the stub core-mediated gateway (ADR-005/007). The unchanged
// vectors must pass for the second independent impl too (ADR-003).
//
// The query-result ("bulk") leg stays in-process: Query/Preview stash result rows
// on the plugin's own stream registry (shared object, same process) and the
// harness pulls them back — the real Arrow Flight wire is deferred.
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	enginev1 "github.com/rat-dev/rat/gen/rat/engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/engine-v1.json) ----

type expectation struct {
	RowsAffected       *int64              `json:"rows_affected"`
	RowsAffectedAbsent bool                `json:"rows_affected_absent"`
	SnapshotIDSet      bool                `json:"snapshot_id_set"`
	RowCount           *int                `json:"row_count"`
	RowsContain        []map[string]string `json:"rows_contain"`
	RowsExcludeKeys    []string            `json:"rows_exclude_keys"`
	Code               string              `json:"code"`
}

type vstep struct {
	Step   string      `json:"step"`
	Op     string      `json:"op"`
	SQL    string      `json:"sql"`
	Limit  int64       `json:"limit"`
	Expect expectation `json:"expect"`
}

type vectors struct {
	Axis      string  `json:"axis"`
	Lifecycle []vstep `json:"lifecycle"`
	Errors    []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/engine-v1.json"

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatalf("read golden vectors %s: %v", vectorPath, err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if v.Axis != "engine/v1" {
		t.Fatalf("vectors axis = %q, want engine/v1", v.Axis)
	}
	return v
}

// ---- harness wiring: plugin behind the stub core gateway ----

type rig struct {
	gw   corev1.CapabilityInvokeServiceClient
	impl *engineServer
	core *stubGateway
}

func bufDial(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRig(t *testing.T) *rig {
	t.Helper()

	plis := bufconn.Listen(1 << 20)
	psrv := grpc.NewServer()
	impl := newServer()
	enginev1.RegisterEngineServiceServer(psrv, impl)
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	core := newGateway(providerConn, "rat-strategy-test", []string{
		"rat://engine/v1/execute",
		"rat://engine/v1/query",
		"rat://engine/v1/preview",
	})

	glis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(gsrv, core)
	go func() { _ = gsrv.Serve(glis) }()
	t.Cleanup(gsrv.Stop)

	return &rig{gw: corev1.NewCapabilityInvokeServiceClient(bufDial(t, glis)), impl: impl, core: core}
}

func tctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (r *rig) invoke(ctx context.Context, capURI string, req, resp proto.Message) error {
	payload, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	out, err := r.gw.Invoke(withCallMeta(ctx), &corev1.InvokeRequest{Capability: capURI, Payload: payload})
	if err != nil {
		return err
	}
	return proto.Unmarshal(out.GetResult(), resp)
}

// withCallMeta attaches the rat-callmeta-bin envelope (ADR-007): context rides in
// metadata, not the request body. A well-formed traceparent is required or the
// gateway rejects the call.
func withCallMeta(ctx context.Context) context.Context {
	rc := &commonv1.RequestContext{
		Trace: &commonv1.TraceContext{
			Traceparent:   "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			CorrelationId: "corr-golden",
		},
		Identity: &commonv1.Identity{Tenant: "acme"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b))
}

// queryRows runs a Query/Preview through the gateway and pulls the result rows the
// returned producer-hosted stream yields (from the plugin's in-process registry).
func (r *rig) queryRows(ctx context.Context, capURI string, req, resp proto.Message, stream func() *commonv1.ArrowStream) ([]row, error) {
	if err := r.invoke(ctx, capURI, req, resp); err != nil {
		return nil, err
	}
	return r.impl.streams.pull(stream()), nil
}

// ---- the tests ----

func TestEngineConformance_GoldenVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Lifecycle {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			switch s.Op {
			case "execute":
				var resp enginev1.ExecuteResponse
				if err := r.invoke(ctx, "rat://engine/v1/execute", &enginev1.ExecuteRequest{Sql: s.SQL}, &resp); err != nil {
					t.Fatalf("execute: %v", err)
				}
				assertWrite(t, resp.GetResult(), s.Expect)
			case "query":
				var resp enginev1.QueryResponse
				rows, err := r.queryRows(ctx, "rat://engine/v1/query",
					&enginev1.QueryRequest{Sql: s.SQL}, &resp, resp.GetStream)
				if err != nil {
					t.Fatalf("query: %v", err)
				}
				assertScan(t, rows, s.Expect)
			case "preview":
				var resp enginev1.PreviewResponse
				rows, err := r.queryRows(ctx, "rat://engine/v1/preview",
					&enginev1.PreviewRequest{Sql: s.SQL, Limit: s.Limit}, &resp, resp.GetStream)
				if err != nil {
					t.Fatalf("preview: %v", err)
				}
				assertScan(t, rows, s.Expect)
			default:
				t.Fatalf("unknown op %q", s.Op)
			}
		})
	}

	if got, want := len(r.core.auditLog()), len(v.Lifecycle); got != want {
		t.Fatalf("audit log = %d entries, want one per mediated call (%d)", got, want)
	}
}

func TestEngineConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			var err error
			switch s.Op {
			case "execute":
				var resp enginev1.ExecuteResponse
				err = r.invoke(ctx, "rat://engine/v1/execute", &enginev1.ExecuteRequest{Sql: s.SQL}, &resp)
			case "query":
				var resp enginev1.QueryResponse
				err = r.invoke(ctx, "rat://engine/v1/query", &enginev1.QueryRequest{Sql: s.SQL}, &resp)
			default:
				t.Fatalf("unknown error-op %q", s.Op)
			}
			if err == nil {
				t.Fatalf("%s: want error %s, got nil", s.Step, s.Expect.Code)
			}
			if got := status.Code(err); got != wantCode(t, s.Expect.Code) {
				t.Fatalf("%s: status = %s, want %s", s.Step, got, s.Expect.Code)
			}
		})
	}
}

// ---- assertions ----

func assertWrite(t *testing.T, res *commonv1.WriteResult, e expectation) {
	t.Helper()
	if e.RowsAffected != nil {
		if res.RowsAffected == nil {
			t.Fatalf("rows_affected absent, want %d", *e.RowsAffected)
		}
		if *res.RowsAffected != *e.RowsAffected {
			t.Fatalf("rows_affected = %d, want %d", *res.RowsAffected, *e.RowsAffected)
		}
	}
	if e.RowsAffectedAbsent && res.RowsAffected != nil {
		t.Fatalf("rows_affected = %d, want absent", *res.RowsAffected)
	}
	if e.SnapshotIDSet && res.GetSnapshotId() == "" {
		t.Fatalf("snapshot_id empty, want set")
	}
}

func assertScan(t *testing.T, rows []row, e expectation) {
	t.Helper()
	if e.RowCount != nil && len(rows) != *e.RowCount {
		t.Fatalf("query = %d rows, want %d", len(rows), *e.RowCount)
	}
	for _, want := range e.RowsContain {
		if !containsRow(rows, want) {
			t.Fatalf("rows %v missing expected %v", rows, want)
		}
	}
	for _, r := range rows {
		for _, k := range e.RowsExcludeKeys {
			if _, present := r[k]; present {
				t.Fatalf("row %v should not contain projected-out key %q", r, k)
			}
		}
	}
}

func containsRow(rows []row, want map[string]string) bool {
	for _, r := range rows {
		match := true
		for k, v := range want {
			if r[k] != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func wantCode(t *testing.T, name string) codes.Code {
	t.Helper()
	switch name {
	case "INVALID_ARGUMENT":
		return codes.InvalidArgument
	case "PERMISSION_DENIED":
		return codes.PermissionDenied
	case "NOT_FOUND":
		return codes.NotFound
	default:
		t.Fatalf("unmapped expected code %q", name)
		return codes.Unknown
	}
}
