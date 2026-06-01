package gateway

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/registry"
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

// fakeState is a trivial StateService provider (the "rat-test-state" plugin):
// Get returns a deterministic value; everything else stays Unimplemented.
type fakeState struct {
	statev1.UnimplementedStateServiceServer
}

func (fakeState) Get(_ context.Context, req *statev1.GetRequest) (*statev1.GetResponse, error) {
	return &statev1.GetResponse{Found: true, Value: []byte("v:" + req.GetKey()), Revision: 7}, nil
}

// bufServer boots an in-process gRPC server and returns a client conn to it.
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

// callerCtx builds an outgoing context with a well-formed rat-callmeta-bin envelope
// declaring the calling plugin (the value the gateway derives C5's caller from).
func callerCtx(caller string) context.Context {
	rc := &commonv1.RequestContext{
		Trace: &commonv1.TraceContext{
			Traceparent:   "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01",
			CorrelationId: "corr-1",
		},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: "t1"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(context.Background(), callMetaHeader, string(b))
}

// testRegistry models the C5 setup: a caller that declares requiring state/get,
// and a state provider that provides get/put/watch. The caller does NOT declare
// put or watch — so those must be denied even though the provider offers them.
func testRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	caller := &manifest.Manifest{
		Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-test-caller"},
		Requires: []manifest.CapabilityRef{{Capability: "rat://state/v1/get"}},
	}
	provider := &manifest.Manifest{
		Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-test-state"},
		Provides: []manifest.CapabilityRef{
			{Capability: "rat://state/v1/get"},
			{Capability: "rat://state/v1/put"},
			{Capability: "rat://state/v1/watch"},
		},
	}
	reg, err := registry.New([]*manifest.Manifest{caller, provider})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	return reg
}

func newTestGateway(t *testing.T) (corev1.CapabilityInvokeServiceClient, *MemAuditor) {
	t.Helper()
	providerConn := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeState{}) })
	audit := &MemAuditor{}
	gw := New(testRegistry(t), map[string]*grpc.ClientConn{"rat-test-state": providerConn}, audit,
		statev1.File_rat_state_v1_state_proto)
	gwConn := bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) })
	return corev1.NewCapabilityInvokeServiceClient(gwConn), audit
}

// TestInvokeAllowed: a declared capability is authorized, relayed to the provider,
// and the provider's response comes back intact — with one allow audit record.
func TestInvokeAllowed(t *testing.T) {
	client, audit := newTestGateway(t)
	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	resp, err := client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if err != nil {
		t.Fatalf("Invoke(get) err = %v, want nil", err)
	}
	var gr statev1.GetResponse
	if err := proto.Unmarshal(resp.GetResult(), &gr); err != nil {
		t.Fatalf("unmarshal relayed result: %v", err)
	}
	if !gr.GetFound() || string(gr.GetValue()) != "v:k1" {
		t.Errorf("relayed GetResponse = {found:%v value:%q}, want {true v:k1}", gr.GetFound(), gr.GetValue())
	}
	recs := audit.Records()
	if len(recs) != 1 || !recs[0].Allowed || recs[0].Caller != "rat-test-caller" || recs[0].Provider != "rat-test-state" {
		t.Errorf("audit = %+v, want one allow record (caller rat-test-caller, provider rat-test-state)", recs)
	}
}

// TestInvokeDeniedUndeclaredCapability: the provider provides put, but the caller
// never declared requiring it — the core denies it (the self-assertion the stubs faked).
func TestInvokeDeniedUndeclaredCapability(t *testing.T) {
	client, audit := newTestGateway(t)
	payload, _ := proto.Marshal(&statev1.PutRequest{Key: "k1", Value: []byte("x")})
	_, err := client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/put", Payload: payload,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke(put) code = %v, want PermissionDenied", status.Code(err))
	}
	recs := audit.Records()
	if len(recs) != 1 || recs[0].Allowed {
		t.Errorf("audit = %+v, want one DENY record", recs)
	}
}

// TestInvokeDeniedUnknownCaller: a caller with no manifest is denied.
func TestInvokeDeniedUnknownCaller(t *testing.T) {
	client, _ := newTestGateway(t)
	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	_, err := client.Invoke(callerCtx("rat-ghost"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke(get) as unknown caller code = %v, want PermissionDenied", status.Code(err))
	}
}

// TestInvokeServerStreamEnforcesAtOpen: streaming capabilities are authorized at
// open (ADR-008) — a caller that didn't declare watch is denied before any frame.
func TestInvokeServerStreamEnforcesAtOpen(t *testing.T) {
	client, audit := newTestGateway(t)
	payload, _ := proto.Marshal(&statev1.WatchRequest{})
	stream, err := client.InvokeServerStream(callerCtx("rat-test-caller"), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stream open/recv code = %v, want PermissionDenied", status.Code(err))
	}
	recs := audit.Records()
	if len(recs) != 1 || recs[0].Allowed {
		t.Errorf("audit = %+v, want one DENY record for watch", recs)
	}
}

// TestInvokeMissingTraceparent: a call with no envelope is a C1 reject before
// authorization (no audit record — it never reached a capability decision).
func TestInvokeMissingTraceparent(t *testing.T) {
	client, audit := newTestGateway(t)
	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	_, err := client.Invoke(context.Background(), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Invoke without traceparent code = %v, want InvalidArgument", status.Code(err))
	}
	if recs := audit.Records(); len(recs) != 0 {
		t.Errorf("audit = %+v, want no record (rejected before the C5 decision)", recs)
	}
}
