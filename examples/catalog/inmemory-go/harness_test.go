// harness_test.go — conformance/golden-data harness for the catalog/v1 axis.
//
// Loads contracts/conformance/catalog-v1.json (the SAME file the Python reference
// loads) and drives CatalogService (GetTable / CreateBranch / MergeBranch) through
// the stub core-mediated gateway (ADR-005/007). The lifecycle is STATEFUL — steps
// share one catalog and run in order — and a step may expect a gRPC error
// mid-sequence (the optimistic-concurrency reject after the target branch moves).
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/catalog-v1.json) ----

type tableExpect struct {
	Identifier string `json:"identifier"`
	Branch     string `json:"branch"`
}

type expectation struct {
	Table          *tableExpect `json:"table"`
	Branch         string       `json:"branch"`
	AlreadyApplied *bool        `json:"already_applied"`
	SnapshotIDSet  bool         `json:"snapshot_id_set"`
	Code           string       `json:"code"`
}

type vstep struct {
	Step                 string      `json:"step"`
	Op                   string      `json:"op"`
	Identifier           string      `json:"identifier"`
	Branch               string      `json:"branch"`
	FromBranch           string      `json:"from_branch"`
	IntoBranch           string      `json:"into_branch"`
	ExpectedIntoSnapshot string      `json:"expected_into_snapshot"`
	IdempotencyKey       string      `json:"idempotency_key"`
	Expect               expectation `json:"expect"`
}

type vectors struct {
	Axis      string  `json:"axis"`
	SeedTable string  `json:"seed_table"`
	Lifecycle []vstep `json:"lifecycle"`
	Errors    []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/catalog-v1.json"

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
	if v.Axis != "catalog/v1" {
		t.Fatalf("vectors axis = %q, want catalog/v1", v.Axis)
	}
	return v
}

// ---- harness wiring: plugin behind the stub core gateway ----

type rig struct {
	gw   corev1.CapabilityInvokeServiceClient
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
	catalogv1.RegisterCatalogServiceServer(psrv, newServer())
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	core := newGateway(providerConn, "rat-strategy-test", []string{
		"rat://catalog/v1/get-table",
		"rat://catalog/v1/create-branch",
		"rat://catalog/v1/merge-branch",
	})

	glis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(gsrv, core)
	go func() { _ = gsrv.Serve(glis) }()
	t.Cleanup(gsrv.Stop)

	return &rig{gw: corev1.NewCapabilityInvokeServiceClient(bufDial(t, glis)), core: core}
}

func tctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func withCallMeta(ctx context.Context) context.Context {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", CorrelationId: "corr-golden"},
		Identity: &commonv1.Identity{Tenant: "acme"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b))
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

// runStep performs one vector step + asserts. Shared by the lifecycle (stateful)
// and the errors (stateless) loops; a step with Expect.Code asserts the gRPC code,
// otherwise it asserts the success expectations.
func runStep(t *testing.T, r *rig, ctx context.Context, s vstep) {
	t.Helper()
	switch s.Op {
	case "get_table":
		var resp catalogv1.GetTableResponse
		err := r.invoke(ctx, "rat://catalog/v1/get-table",
			&catalogv1.GetTableRequest{Identifier: s.Identifier, Branch: s.Branch}, &resp)
		if expectedError(t, s, err) {
			return
		}
		assertTable(t, resp.GetTable(), s.Expect.Table)
	case "create_branch":
		var resp catalogv1.CreateBranchResponse
		err := r.invoke(ctx, "rat://catalog/v1/create-branch",
			&catalogv1.CreateBranchRequest{Branch: s.Branch, FromBranch: s.FromBranch}, &resp)
		if expectedError(t, s, err) {
			return
		}
		if s.Expect.Branch != "" && resp.GetBranch() != s.Expect.Branch {
			t.Fatalf("%s: branch = %q, want %q", s.Step, resp.GetBranch(), s.Expect.Branch)
		}
	case "merge_branch":
		var resp catalogv1.MergeBranchResponse
		err := r.invoke(ctx, "rat://catalog/v1/merge-branch", &catalogv1.MergeBranchRequest{
			Branch: s.Branch, IntoBranch: s.IntoBranch,
			ExpectedIntoSnapshot: s.ExpectedIntoSnapshot, IdempotencyKey: s.IdempotencyKey,
		}, &resp)
		if expectedError(t, s, err) {
			return
		}
		assertMerge(t, s, &resp)
	default:
		t.Fatalf("%s: unknown op %q", s.Step, s.Op)
	}
}

// ---- the tests ----

func TestCatalogConformance_GoldenVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Lifecycle {
		s := s
		t.Run(s.Step, func(t *testing.T) { runStep(t, r, ctx, s) })
	}

	if got, want := len(r.core.auditLog()), len(v.Lifecycle); got != want {
		t.Fatalf("audit log = %d entries, want one per mediated call (%d)", got, want)
	}
}

func TestCatalogConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) { runStep(t, r, ctx, s) })
	}
}

// ---- assertions ----

// expectedError returns true (step handled) when the step expects a gRPC code,
// asserting it; otherwise it fatals on any error and returns false.
func expectedError(t *testing.T, s vstep, err error) bool {
	t.Helper()
	if s.Expect.Code != "" {
		if err == nil {
			t.Fatalf("%s: want error %s, got nil", s.Step, s.Expect.Code)
		}
		if got := status.Code(err); got != wantCode(t, s.Expect.Code) {
			t.Fatalf("%s: status = %s, want %s", s.Step, got, s.Expect.Code)
		}
		return true
	}
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", s.Step, err)
	}
	return false
}

func assertTable(t *testing.T, got *commonv1.TableRef, want *tableExpect) {
	t.Helper()
	if want == nil {
		return
	}
	if got.GetIdentifier() != want.Identifier {
		t.Fatalf("table.identifier = %q, want %q", got.GetIdentifier(), want.Identifier)
	}
	if got.GetBranch() != want.Branch {
		t.Fatalf("table.branch = %q, want %q", got.GetBranch(), want.Branch)
	}
}

func assertMerge(t *testing.T, s vstep, resp *catalogv1.MergeBranchResponse) {
	t.Helper()
	if s.Expect.AlreadyApplied != nil && resp.GetAlreadyApplied() != *s.Expect.AlreadyApplied {
		t.Fatalf("%s: already_applied = %v, want %v", s.Step, resp.GetAlreadyApplied(), *s.Expect.AlreadyApplied)
	}
	if s.Expect.SnapshotIDSet && resp.GetSnapshotId() == "" {
		t.Fatalf("%s: snapshot_id empty, want set", s.Step)
	}
}

func wantCode(t *testing.T, name string) codes.Code {
	t.Helper()
	switch name {
	case "INVALID_ARGUMENT":
		return codes.InvalidArgument
	case "NOT_FOUND":
		return codes.NotFound
	case "FAILED_PRECONDITION":
		return codes.FailedPrecondition
	case "PERMISSION_DENIED":
		return codes.PermissionDenied
	default:
		t.Fatalf("unmapped expected code %q", name)
		return codes.Unknown
	}
}
