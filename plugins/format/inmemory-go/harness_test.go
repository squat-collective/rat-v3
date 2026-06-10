// harness_test.go — the conformance/golden-data harness for the format/v1 axis.
//
// This is the ADR-003 forcing function. It loads the language-neutral golden
// vectors from contracts/conformance/format-v1.json (the SAME file the Python
// reference loads) and drives the FormatService through its full lifecycle
// (append → scan → merge → overwrite → maintain) plus error cases.
//
// Two things are deliberate:
//
//  1. It runs over REAL gRPC, not direct method calls — exercising serialization
//     of TableRef/ArrowStream/WriteResult and the RequestContext envelope, which
//     the contract review (reviews/06) said only a real implementation validates.
//
//  2. Every control call is routed through the CORE-MEDIATED path — the stub
//     core/v1 CapabilityInvokeService (gateway_test.go), not a direct
//     FormatService client. So the harness validates the ADR-005 mediation seams
//     (capability routing, C5 enforcement, C8 audit, generic byte relay) on top
//     of the plugin-to-plugin data contract. The plugin never sees a direct dial.
//
// The data ("bulk") leg stays in-process: source rows are staged on the plugin's
// own stream registry (shared object, same process) and scan results are pulled
// back from it — the real Arrow Flight wire is deferred to a production reference.
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	formatv1 "github.com/squat-collective/rat-v3/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/format-v1.json) ----

type expectation struct {
	RowsAffected       *int64              `json:"rows_affected"`
	RowsAffectedAbsent bool                `json:"rows_affected_absent"`
	SnapshotIDSet      bool                `json:"snapshot_id_set"`
	RowCount           *int                `json:"row_count"`
	RowsContain        []map[string]string `json:"rows_contain"`
	Code               string              `json:"code"`
}

type vstep struct {
	Step          string              `json:"step"`
	Op            string              `json:"op"`
	Source        []map[string]string `json:"source"`
	MergeKeys     []string            `json:"merge_keys"`
	TableOverride *string             `json:"table_override"` // pointer: "" overrides, absent = default table
	Expect        expectation         `json:"expect"`
}

type vectors struct {
	Axis      string  `json:"axis"`
	Table     string  `json:"table"`
	Lifecycle []vstep `json:"lifecycle"`
	Errors    []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/format-v1.json"

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
	if v.Axis != "format/v1" {
		t.Fatalf("vectors axis = %q, want format/v1", v.Axis)
	}
	return v
}

// ---- harness wiring: plugin behind the stub core gateway ----

// rig holds the mediated client plus a handle to the plugin impl (to stage/pull
// the in-process bulk leg) and the gateway (to assert C8 audit).
type rig struct {
	gw   corev1.CapabilityInvokeServiceClient
	impl *formatServer
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

// newRig stands up two real gRPC hops: harness → core gateway → format plugin.
func newRig(t *testing.T) *rig {
	t.Helper()

	// Hop 2: the format plugin.
	plis := bufconn.Listen(1 << 20)
	psrv := grpc.NewServer()
	impl := newServer()
	formatv1.RegisterFormatServiceServer(psrv, impl)
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	// The core gateway, pointed at the plugin, with the caller permitted every
	// format capability (its manifest `requires` them all).
	core := newGateway(providerConn, "rat-strategy-test", []string{
		"rat://format/v1/scan",
		"rat://format/v1/append",
		"rat://format/v1/merge",
		"rat://format/v1/overwrite",
		"rat://format/v1/maintain",
	})

	// Hop 1: the core's capability-invoke gateway.
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

// invoke mediates one typed call: marshal req → CapabilityInvokeService.Invoke →
// unmarshal the relayed result into resp. This is exactly what a real calling
// plugin's generated SDK stub does on top of Invoke.
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

// withCallMeta attaches the rat-callmeta-bin envelope a calling plugin's SDK sets
// on every control call (ADR-007): a well-formed traceparent + correlation id +
// the caller-supplied tenant (which the core re-stamps). Without it the gateway
// rejects the call for a missing traceparent. Note: context rides in metadata, NOT
// in the request body — the request messages no longer have a context field.
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

func toRows(src []map[string]string) []row {
	out := make([]row, len(src))
	for i, m := range src {
		out[i] = row(m)
	}
	return out
}

func tableRef(id string) *commonv1.TableRef { return &commonv1.TableRef{Identifier: id} }

// scan resolves the table through the gateway and pulls the rows the returned
// producer-hosted stream yields (from the plugin's in-process registry).
func (r *rig) scan(ctx context.Context, table string) ([]row, error) {
	var resp formatv1.ResolveResponse
	if err := r.invoke(ctx, "rat://format/v1/scan",
		&formatv1.ResolveRequest{Table: tableRef(table)}, &resp); err != nil {
		return nil, err
	}
	return r.impl.streams.pull(resp.GetStream()), nil
}

// ---- the tests ----

func TestFormatConformance_GoldenVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Lifecycle {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			switch s.Op {
			case "append":
				var resp formatv1.AppendResponse
				if err := r.invoke(ctx, "rat://format/v1/append", &formatv1.AppendRequest{
					Table: tableRef(v.Table),
					Source: r.impl.streams.put(toRows(s.Source)),
				}, &resp); err != nil {
					t.Fatalf("append: %v", err)
				}
				assertWrite(t, resp.GetResult(), s.Expect)
			case "merge":
				var resp formatv1.MergeResponse
				if err := r.invoke(ctx, "rat://format/v1/merge", &formatv1.MergeRequest{
					Table: tableRef(v.Table),
					MergeKeys: s.MergeKeys, Source: r.impl.streams.put(toRows(s.Source)),
				}, &resp); err != nil {
					t.Fatalf("merge: %v", err)
				}
				assertWrite(t, resp.GetResult(), s.Expect)
			case "overwrite":
				var resp formatv1.OverwriteResponse
				if err := r.invoke(ctx, "rat://format/v1/overwrite", &formatv1.OverwriteRequest{
					Table: tableRef(v.Table),
					Source: r.impl.streams.put(toRows(s.Source)),
				}, &resp); err != nil {
					t.Fatalf("overwrite: %v", err)
				}
				assertWrite(t, resp.GetResult(), s.Expect)
			case "maintain":
				var resp formatv1.MaintainResponse
				if err := r.invoke(ctx, "rat://format/v1/maintain",
					&formatv1.MaintainRequest{Table: tableRef(v.Table)}, &resp); err != nil {
					t.Fatalf("maintain: %v", err)
				}
				assertWrite(t, resp.GetResult(), s.Expect)
			case "scan":
				rows, err := r.scan(ctx, v.Table)
				if err != nil {
					t.Fatalf("scan: %v", err)
				}
				assertScan(t, rows, s.Expect)
			default:
				t.Fatalf("unknown op %q", s.Op)
			}
		})
	}

	// C8: every mediated call must have produced an audit record. Lifecycle has
	// one Invoke per step (scans included).
	if got, want := len(r.core.auditLog()), len(v.Lifecycle); got != want {
		t.Fatalf("audit log = %d entries, want one per mediated call (%d)", got, want)
	}
}

func TestFormatConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			table := v.Table
			if s.TableOverride != nil {
				table = *s.TableOverride
			}
			var err error
			switch s.Op {
			case "scan":
				_, err = r.scan(ctx, table)
			case "merge":
				var resp formatv1.MergeResponse
				err = r.invoke(ctx, "rat://format/v1/merge", &formatv1.MergeRequest{
					Table: tableRef(table),
					MergeKeys: s.MergeKeys, Source: r.impl.streams.put(toRows(s.Source)),
				}, &resp)
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

// TestGateway_RejectsMissingTraceparent exercises the ADR-007 gate the metadata
// carriage makes possible: the gateway validates traceparent (now in metadata,
// readable without parsing the payload) and rejects a call that omits the
// rat-callmeta-bin envelope.
func TestGateway_RejectsMissingTraceparent(t *testing.T) {
	r := newRig(t)
	ctx := tctx(t)
	payload, err := proto.Marshal(&formatv1.ResolveRequest{Table: tableRef("warehouse.sales.orders")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Note: ctx WITHOUT withCallMeta → no traceparent reaches the gateway.
	_, err = r.gw.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://format/v1/scan", Payload: payload})
	if err == nil {
		t.Fatal("Invoke without traceparent: want error, got nil")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("status = %s, want INVALID_ARGUMENT", got)
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
		t.Fatalf("scan = %d rows, want %d", len(rows), *e.RowCount)
	}
	for _, want := range e.RowsContain {
		if !containsRow(rows, want) {
			t.Fatalf("scan rows %v missing expected %v", rows, want)
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
