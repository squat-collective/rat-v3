// harness_test.go — conformance/golden-data harness for the runtime/v1 axis.
//
// Loads contracts/conformance/runtime-v1.json (the SAME file the Python reference
// loads) and drives RuntimeService.Execute over real server-streaming gRPC,
// collecting the progress messages + terminal completion and asserting against the
// vectors.
//
// MEDIATED via the stub core gateway's InvokeServerStream (ADR-008). Earlier this
// axis had to be driven directly because the gateway's only Invoke was unary; the
// ADR-008 migration added a server-streaming relay, so runtime now routes through
// the core-mediated path like every other axis — exercising the C1/C5/C8 +
// identity-stamp seams on a STREAMING call (the gateway relays each ExecuteResponse
// frame's opaque bytes without deserializing it).
package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	runtimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/runtime-v1.json) ----

type completedExpect struct {
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	RowsAffected *int64 `json:"rows_affected"`
}

type expectation struct {
	ProgressCount          *int             `json:"progress_count"`
	ProgressHasFraction    bool             `json:"progress_has_fraction"`
	ProgressFractionAbsent bool             `json:"progress_fraction_absent"`
	FinalFraction          *float64         `json:"final_fraction"`
	Completed              *completedExpect `json:"completed"`
	Code                   string           `json:"code"`
}

type vstep struct {
	Step   string          `json:"step"`
	Op     string          `json:"op"`
	Work   json.RawMessage `json:"work"`
	Expect expectation     `json:"expect"`
}

type vectors struct {
	Axis      string  `json:"axis"`
	Lifecycle []vstep `json:"lifecycle"`
	Errors    []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/runtime-v1.json"

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
	if v.Axis != "runtime/v1" {
		t.Fatalf("vectors axis = %q, want runtime/v1", v.Axis)
	}
	return v
}

// workSpecBytes turns the vector's `work` into the work_spec wire bytes. A JSON
// `null` (or absent) → empty bytes (the INVALID_ARGUMENT case); otherwise the raw
// JSON object is sent as-is (the runtime parses it).
func workSpecBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}

// ---- harness wiring: plugin behind the stub core gateway (ADR-008) ----

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

	// Hop 2: the runtime plugin.
	plis := bufconn.Listen(1 << 20)
	psrv := grpc.NewServer()
	runtimev1.RegisterRuntimeServiceServer(psrv, newServer())
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	core := newGateway(providerConn, "rat-strategy-test", []string{"rat://runtime/v1/execute"})

	// Hop 1: the core's capability-invoke gateway.
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

// execute drives one Execute through the gateway's InvokeServerStream, unmarshals
// each relayed frame back into an ExecuteResponse, and returns the collected
// progress + terminal completion (nil if the stream errored first).
func (r *rig) execute(ctx context.Context, spec []byte) ([]*runtimev1.ExecuteProgress, *runtimev1.ExecuteCompleted, error) {
	payload, err := proto.Marshal(&runtimev1.ExecuteRequest{WorkSpec: spec})
	if err != nil {
		return nil, nil, err
	}
	stream, err := r.gw.InvokeServerStream(withCallMeta(ctx), &corev1.InvokeServerStreamRequest{
		Capability: "rat://runtime/v1/execute", Payload: payload,
	})
	if err != nil {
		return nil, nil, err
	}
	var progs []*runtimev1.ExecuteProgress
	var done *runtimev1.ExecuteCompleted
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return progs, done, err
		}
		var resp runtimev1.ExecuteResponse
		if err := proto.Unmarshal(frame.GetResult(), &resp); err != nil {
			return progs, done, err
		}
		if p := resp.GetProgress(); p != nil {
			progs = append(progs, p)
		}
		if c := resp.GetCompleted(); c != nil {
			done = c
		}
	}
	return progs, done, nil
}

// ---- the tests ----

func TestRuntimeConformance_GoldenVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Lifecycle {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			progs, done, err := r.execute(ctx, workSpecBytes(s.Work))
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			assertExecute(t, progs, done, s.Expect)
		})
	}

	// C8: one audit record per mediated stream.
	if got, want := len(r.core.auditLog()), len(v.Lifecycle); got != want {
		t.Fatalf("audit log = %d entries, want one per mediated stream (%d)", got, want)
	}
}

func TestRuntimeConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			_, _, err := r.execute(ctx, workSpecBytes(s.Work))
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

func assertExecute(t *testing.T, progs []*runtimev1.ExecuteProgress, done *runtimev1.ExecuteCompleted, e expectation) {
	t.Helper()
	if e.ProgressCount != nil && len(progs) != *e.ProgressCount {
		t.Fatalf("progress count = %d, want %d", len(progs), *e.ProgressCount)
	}
	if e.ProgressHasFraction {
		for i, p := range progs {
			if p.Fraction == nil {
				t.Fatalf("progress[%d] fraction absent, want present", i)
			}
		}
	}
	if e.ProgressFractionAbsent {
		for i, p := range progs {
			if p.Fraction != nil {
				t.Fatalf("progress[%d] fraction = %v, want absent", i, *p.Fraction)
			}
		}
	}
	if e.FinalFraction != nil {
		if len(progs) == 0 {
			t.Fatalf("final_fraction expected but no progress messages")
		}
		last := progs[len(progs)-1]
		if last.Fraction == nil || *last.Fraction != *e.FinalFraction {
			t.Fatalf("final fraction = %v, want %v", last.Fraction, *e.FinalFraction)
		}
	}
	if e.Completed != nil {
		if done == nil {
			t.Fatalf("no terminal completed message")
		}
		if done.GetSuccess() != e.Completed.Success {
			t.Fatalf("completed.success = %v, want %v", done.GetSuccess(), e.Completed.Success)
		}
		if e.Completed.Error != "" && done.GetError() != e.Completed.Error {
			t.Fatalf("completed.error = %q, want %q", done.GetError(), e.Completed.Error)
		}
		if e.Completed.RowsAffected != nil && done.GetResult().GetRowsAffected() != *e.Completed.RowsAffected {
			t.Fatalf("completed rows_affected = %d, want %d", done.GetResult().GetRowsAffected(), *e.Completed.RowsAffected)
		}
	}
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
