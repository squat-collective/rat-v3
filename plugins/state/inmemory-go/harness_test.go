// harness_test.go — conformance/golden-data harness for the state/v1 axis.
//
// Loads contracts/conformance/state-v1.json (the SAME file the Python reference
// loads) and drives StateService through the stub core-mediated gateway: Get/Put/
// List via unary Invoke, Watch via the ADR-008 InvokeServerStream relay. The
// lifecycle is STATEFUL; the errors array exercises the KEY GRAMMAR. Bad keys are
// built from key_len ("a"*N, oversize) / key_inject ("a"+chr(N)+"b", NUL/control)
// so the vector file stays pure-ASCII valid JSON.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/state-v1.json) ----

type watchEventExpect struct {
	Type     string `json:"type"`
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
}

type expectation struct {
	Found       *bool              `json:"found"`
	Value       string             `json:"value"`
	Revision    *int64             `json:"revision"`
	Outcome     string             `json:"outcome"`
	Keys        []string           `json:"keys"`
	WatchEvents []watchEventExpect `json:"watch_events"`
	Code        string             `json:"code"`
}

type vstep struct {
	Step         string      `json:"step"`
	Op           string      `json:"op"`
	Key          string      `json:"key"`
	Value        string      `json:"value"`
	Prefix       string      `json:"prefix"`
	IfRevision   int64       `json:"if_revision"`
	FromRevision int64       `json:"from_revision"`
	KeyLen       int         `json:"key_len"`
	KeyInject    *int        `json:"key_inject"`
	Expect       expectation `json:"expect"`
}

type vectors struct {
	Axis      string  `json:"axis"`
	Lifecycle []vstep `json:"lifecycle"`
	Errors    []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/state-v1.json"

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
	if v.Axis != "state/v1" {
		t.Fatalf("vectors axis = %q, want state/v1", v.Axis)
	}
	return v
}

// resolveKey builds the request key: key_len → "a"*N (oversize); key_inject →
// "a"+chr(N)+"b" (NUL/control); else the literal key.
func resolveKey(s vstep) string {
	if s.KeyLen > 0 {
		return strings.Repeat("a", s.KeyLen)
	}
	if s.KeyInject != nil {
		return "a" + string(rune(*s.KeyInject)) + "b"
	}
	return s.Key
}

func outcomeEnum(t *testing.T, name string) statev1.PutOutcome {
	switch name {
	case "COMMITTED":
		return statev1.PutOutcome_PUT_OUTCOME_COMMITTED
	case "CONFLICT":
		return statev1.PutOutcome_PUT_OUTCOME_CONFLICT
	case "UNKNOWN":
		return statev1.PutOutcome_PUT_OUTCOME_UNKNOWN
	default:
		t.Fatalf("unmapped outcome %q", name)
		return statev1.PutOutcome_PUT_OUTCOME_UNSPECIFIED
	}
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
	statev1.RegisterStateServiceServer(psrv, newServer())
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	core := newGateway(providerConn, "rat-strategy-test", []string{
		"rat://state/v1/get", "rat://state/v1/put", "rat://state/v1/list", "rat://state/v1/watch",
		"rat://state/v1/create-if-absent",
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

func (r *rig) watch(ctx context.Context, prefix string, fromRev int64) ([]*statev1.WatchResponse, error) {
	payload, err := proto.Marshal(&statev1.WatchRequest{Prefix: prefix, FromRevision: fromRev})
	if err != nil {
		return nil, err
	}
	stream, err := r.gw.InvokeServerStream(withCallMeta(ctx), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	var events []*statev1.WatchResponse
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, err
		}
		var ev statev1.WatchResponse
		if err := proto.Unmarshal(frame.GetResult(), &ev); err != nil {
			return events, err
		}
		events = append(events, &ev)
	}
	return events, nil
}

func runStep(t *testing.T, r *rig, ctx context.Context, s vstep) {
	t.Helper()
	switch s.Op {
	case "get":
		var resp statev1.GetResponse
		err := r.invoke(ctx, "rat://state/v1/get", &statev1.GetRequest{Key: resolveKey(s)}, &resp)
		if expectedError(t, s, err) {
			return
		}
		assertGet(t, s, &resp)
	case "put":
		var resp statev1.PutResponse
		err := r.invoke(ctx, "rat://state/v1/put",
			&statev1.PutRequest{Key: resolveKey(s), Value: []byte(s.Value), IfRevision: s.IfRevision}, &resp)
		if expectedError(t, s, err) {
			return
		}
		assertPut(t, s, &resp)
	case "list":
		var resp statev1.ListResponse
		err := r.invoke(ctx, "rat://state/v1/list", &statev1.ListRequest{Prefix: s.Prefix}, &resp)
		if expectedError(t, s, err) {
			return
		}
		assertKeys(t, s, resp.GetKeys())
	case "watch":
		events, err := r.watch(ctx, s.Prefix, s.FromRevision)
		if expectedError(t, s, err) {
			return
		}
		assertWatch(t, s, events)
	case "create-if-absent":
		var resp statev1.CreateIfAbsentResponse
		err := r.invoke(ctx, "rat://state/v1/create-if-absent",
			&statev1.CreateIfAbsentRequest{Key: resolveKey(s), Value: []byte(s.Value)}, &resp)
		if expectedError(t, s, err) {
			return
		}
		e := s.Expect
		if e.Outcome != "" && resp.GetOutcome() != outcomeEnum(t, e.Outcome) {
			t.Fatalf("%s: outcome = %s, want %s", s.Step, resp.GetOutcome(), e.Outcome)
		}
		if e.Revision != nil && resp.GetRevision() != *e.Revision {
			t.Fatalf("%s: revision = %d, want %d", s.Step, resp.GetRevision(), *e.Revision)
		}
	default:
		t.Fatalf("%s: unknown op %q", s.Step, s.Op)
	}
}

// ---- the tests ----

func TestStateConformance_GoldenVectors(t *testing.T) {
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

func TestStateConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) { runStep(t, r, ctx, s) })
	}
}

// ---- assertions ----

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

func assertGet(t *testing.T, s vstep, resp *statev1.GetResponse) {
	t.Helper()
	e := s.Expect
	if e.Found != nil && resp.GetFound() != *e.Found {
		t.Fatalf("%s: found = %v, want %v", s.Step, resp.GetFound(), *e.Found)
	}
	if e.Value != "" && string(resp.GetValue()) != e.Value {
		t.Fatalf("%s: value = %q, want %q", s.Step, string(resp.GetValue()), e.Value)
	}
	if e.Revision != nil && resp.GetRevision() != *e.Revision {
		t.Fatalf("%s: revision = %d, want %d", s.Step, resp.GetRevision(), *e.Revision)
	}
}

func assertPut(t *testing.T, s vstep, resp *statev1.PutResponse) {
	t.Helper()
	e := s.Expect
	if e.Outcome != "" && resp.GetOutcome() != outcomeEnum(t, e.Outcome) {
		t.Fatalf("%s: outcome = %s, want %s", s.Step, resp.GetOutcome(), e.Outcome)
	}
	if e.Revision != nil && resp.GetRevision() != *e.Revision {
		t.Fatalf("%s: revision = %d, want %d", s.Step, resp.GetRevision(), *e.Revision)
	}
}

func assertKeys(t *testing.T, s vstep, got []string) {
	t.Helper()
	want := s.Expect.Keys
	if len(got) != len(want) {
		t.Fatalf("%s: keys = %v, want %v", s.Step, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: keys = %v, want %v", s.Step, got, want)
		}
	}
}

func assertWatch(t *testing.T, s vstep, events []*statev1.WatchResponse) {
	t.Helper()
	want := s.Expect.WatchEvents
	if len(events) != len(want) {
		t.Fatalf("%s: watch events = %d, want %d", s.Step, len(events), len(want))
	}
	for i, w := range want {
		ev := events[i]
		if ev.GetType() != watchType(t, w.Type) || ev.GetKey() != w.Key || ev.GetRevision() != w.Revision {
			t.Fatalf("%s: event[%d] = {%s %s rev %d}, want {%s %s rev %d}",
				s.Step, i, ev.GetType(), ev.GetKey(), ev.GetRevision(), w.Type, w.Key, w.Revision)
		}
	}
}

func watchType(t *testing.T, name string) statev1.WatchResponse_Type {
	t.Helper()
	switch name {
	case "PUT":
		return statev1.WatchResponse_TYPE_PUT
	case "DELETE":
		return statev1.WatchResponse_TYPE_DELETE
	default:
		t.Fatalf("unmapped watch type %q", name)
		return statev1.WatchResponse_TYPE_UNSPECIFIED
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
