package gateway

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

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

// fakeStateB is a SECOND, distinguishable state provider — a relaunched plugin's new
// endpoint. Its Get returns a "REBOUND:" prefix so a routed call proves which conn served it.
type fakeStateB struct {
	statev1.UnimplementedStateServiceServer
}

func (fakeStateB) Get(_ context.Context, req *statev1.GetRequest) (*statev1.GetResponse, error) {
	return &statev1.GetResponse{Found: true, Value: []byte("REBOUND:" + req.GetKey())}, nil
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

// TestSetProviderRebind: the gateway re-binds a registered provider's live connection at
// runtime (ADR-022) — the re-wire the reconciler needs when a relaunched plugin comes up
// on a NEW endpoint. A call routes to conn A; after SetProvider swaps to conn B, the same
// call routes to B; after RemoveProvider, it is Unavailable. Concurrency-safe by the RWMutex.
func TestSetProviderRebind(t *testing.T) {
	connA := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeState{}) })
	gw := New(testRegistry(t), map[string]*grpc.ClientConn{"rat-test-state": connA}, &MemAuditor{},
		statev1.File_rat_state_v1_state_proto)
	client := corev1.NewCapabilityInvokeServiceClient(
		bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) }))

	get := func() (string, error) {
		payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
		resp, err := client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: payload})
		if err != nil {
			return "", err
		}
		var gr statev1.GetResponse
		_ = proto.Unmarshal(resp.GetResult(), &gr)
		return string(gr.GetValue()), nil
	}

	if v, err := get(); err != nil || v != "v:k1" {
		t.Fatalf("before rebind = (%q, %v), want (v:k1, nil) — provider A", v, err)
	}

	// Re-bind the SAME registered provider to a NEW connection (B).
	connB := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeStateB{}) })
	if prev := gw.SetProvider("rat-test-state", connB); prev != connA {
		t.Errorf("SetProvider returned prev != connA")
	}
	if v, err := get(); err != nil || v != "REBOUND:k1" {
		t.Fatalf("after rebind = (%q, %v), want (REBOUND:k1, nil) — provider B", v, err)
	}

	// Remove the provider → routing fails Unavailable (unbound).
	if prev := gw.RemoveProvider("rat-test-state"); prev != connB {
		t.Errorf("RemoveProvider returned prev != connB")
	}
	if _, err := get(); status.Code(err) != codes.Unavailable {
		t.Fatalf("after remove = %v, want Unavailable", status.Code(err))
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
	// A stream denied at open never opens → only the deny decision record, no terminal
	// close record (C4: the terminal record is for streams that actually opened).
	recs := audit.Records()
	if len(recs) != 1 || recs[0].Allowed || recs[0].Terminal {
		t.Errorf("audit = %+v, want one DENY decision record (no terminal)", recs)
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

// slowState is a provider whose Get blocks ~2s — used to prove the gateway bounds
// a hung provider by the soft deadline (C3).
type slowState struct {
	statev1.UnimplementedStateServiceServer
}

func (slowState) Get(ctx context.Context, _ *statev1.GetRequest) (*statev1.GetResponse, error) {
	select {
	case <-time.After(2 * time.Second):
		return &statev1.GetResponse{Found: true}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

// callerCtxDeadline is callerCtx plus a soft deadline (deadline_unix_ms) d from now.
func callerCtxDeadline(caller string, d time.Duration) context.Context {
	rc := &commonv1.RequestContext{
		Trace: &commonv1.TraceContext{
			Traceparent:   "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01",
			CorrelationId: "corr-1",
		},
		Identity:       &commonv1.Identity{CallerPlugin: caller, Tenant: "t1"},
		DeadlineUnixMs: time.Now().Add(d).UnixMilli(),
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(context.Background(), callMetaHeader, string(b))
}

// TestInvokeBoundsProviderDeadline (C3): a soft deadline (deadline_unix_ms) sooner
// than the channel deadline bounds the downstream call — a hung provider returns
// DeadlineExceeded fast instead of pinning the gateway.
func TestInvokeBoundsProviderDeadline(t *testing.T) {
	providerConn := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, slowState{}) })
	gw := New(testRegistry(t), map[string]*grpc.ClientConn{"rat-test-state": providerConn}, &MemAuditor{},
		statev1.File_rat_state_v1_state_proto)
	gwConn := bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) })
	client := corev1.NewCapabilityInvokeServiceClient(gwConn)

	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k"})
	start := time.Now()
	_, err := client.Invoke(callerCtxDeadline("rat-test-caller", 150*time.Millisecond), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Invoke against a 2s-slow provider with a 150ms soft deadline = %v, want DeadlineExceeded", status.Code(err))
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("call took %v — the soft deadline did not bound the provider", elapsed)
	}
}

// streamingState serves Watch as a server-stream: it sends `frames` responses, then
// either ends cleanly (EOF) or returns an error (failAfter) — to exercise both
// terminal-audit outcomes.
type streamingState struct {
	statev1.UnimplementedStateServiceServer
	frames    int
	failAfter bool
}

func (s streamingState) Watch(_ *statev1.WatchRequest, srv statev1.StateService_WatchServer) error {
	for i := 0; i < s.frames; i++ {
		if err := srv.Send(&statev1.WatchResponse{}); err != nil {
			return err
		}
	}
	if s.failAfter {
		return status.Error(codes.Internal, "watch boom")
	}
	return nil
}

// newStreamingGateway wires a gateway whose caller DECLARES watch (so it is allowed
// at open) in front of the given streaming provider. idle>0 overrides the C3 stream
// idle timeout (set before serving, so no race with the handler goroutine).
func newStreamingGateway(t *testing.T, prov statev1.StateServiceServer, idle time.Duration) (corev1.CapabilityInvokeServiceClient, *MemAuditor) {
	t.Helper()
	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-watch-caller"},
		Requires: []manifest.CapabilityRef{{Capability: "rat://state/v1/watch"}}}
	provider := &manifest.Manifest{Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-test-state"},
		Provides: []manifest.CapabilityRef{{Capability: "rat://state/v1/watch"}}}
	reg, err := registry.New([]*manifest.Manifest{caller, provider})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	providerConn := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, prov) })
	audit := &MemAuditor{}
	gw := New(reg, map[string]*grpc.ClientConn{"rat-test-state": providerConn}, audit, statev1.File_rat_state_v1_state_proto)
	if idle > 0 {
		gw.StreamIdleTimeout = idle
	}
	gwConn := bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) })
	return corev1.NewCapabilityInvokeServiceClient(gwConn), audit
}

// TestInvokeServerStreamTerminalAuditSuccess (C4): a stream that completes cleanly
// produces TWO audit records — the open decision (allowed) and a terminal close
// record (Outcome=success, Frames=N) that shares the open record's correlation id.
func TestInvokeServerStreamTerminalAuditSuccess(t *testing.T) {
	client, audit := newStreamingGateway(t, streamingState{frames: 3}, 0)
	payload, _ := proto.Marshal(&statev1.WatchRequest{})
	stream, err := client.InvokeServerStream(callerCtx("rat-watch-caller"), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	n := 0
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv frame %d: %v", n, err)
		}
		n++
	}
	if n != 3 {
		t.Errorf("relayed %d frames, want 3", n)
	}
	recs := audit.Records()
	if len(recs) != 2 {
		t.Fatalf("audit = %+v, want 2 (open decision + terminal close)", recs)
	}
	open, term := recs[0], recs[1]
	if !open.Allowed || open.Terminal {
		t.Errorf("record[0] = %+v, want the open allow decision (Terminal=false)", open)
	}
	if !term.Terminal || term.Outcome != "success" || term.Frames != 3 {
		t.Errorf("record[1] = %+v, want terminal {success, Frames:3}", term)
	}
	if term.Correlation == "" || term.Correlation != open.Correlation {
		t.Errorf("terminal correlation %q != open correlation %q (records must link)", term.Correlation, open.Correlation)
	}
}

// TestInvokeServerStreamTerminalAuditError (C4): a stream that errors mid-flight still
// emits a terminal record — Outcome=error, the frames relayed before the failure, and
// the error message — so a broken stream is never a silent gap in the audit trail.
func TestInvokeServerStreamTerminalAuditError(t *testing.T) {
	client, audit := newStreamingGateway(t, streamingState{frames: 1, failAfter: true}, 0)
	payload, _ := proto.Marshal(&statev1.WatchRequest{})
	stream, err := client.InvokeServerStream(callerCtx("rat-watch-caller"), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	var recvErr error
	for {
		if _, recvErr = stream.Recv(); recvErr != nil {
			break
		}
	}
	if status.Code(recvErr) != codes.Internal {
		t.Fatalf("stream end err = %v, want Internal", status.Code(recvErr))
	}
	recs := audit.Records()
	if len(recs) != 2 {
		t.Fatalf("audit = %+v, want 2 (open decision + terminal close)", recs)
	}
	term := recs[1]
	if !term.Terminal || term.Outcome != "error" || term.Frames != 1 || term.Error == "" {
		t.Errorf("record[1] = %+v, want terminal {error, Frames:1, Error set}", term)
	}
}

// idleStreamingState sends `frames` Watch responses, then GOES SILENT (blocks until
// its context is canceled) — a hung provider that sends no EOF and no error, used to
// exercise the C3 idle backstop. It returns when the gateway cuts the stream.
type idleStreamingState struct {
	statev1.UnimplementedStateServiceServer
	frames int
}

func (s idleStreamingState) Watch(_ *statev1.WatchRequest, srv statev1.StateService_WatchServer) error {
	for i := 0; i < s.frames; i++ {
		if err := srv.Send(&statev1.WatchResponse{}); err != nil {
			return err
		}
	}
	<-srv.Context().Done() // hang until the gateway's idle/deadline cut tears the stream down
	return srv.Context().Err()
}

// TestInvokeServerStreamIdleTimeout (C3): with NO deadline set, a provider that sends
// a few frames then goes silent is cut by the gateway's idle backstop — promptly, with
// DeadlineExceeded and a terminal {timeout, Frames:N} record. A hung provider can't pin
// the stream forever.
func TestInvokeServerStreamIdleTimeout(t *testing.T) {
	client, audit := newStreamingGateway(t, idleStreamingState{frames: 2}, 200*time.Millisecond)
	payload, _ := proto.Marshal(&statev1.WatchRequest{})
	stream, err := client.InvokeServerStream(callerCtx("rat-watch-caller"), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	start := time.Now()
	n := 0
	var recvErr error
	for {
		if _, recvErr = stream.Recv(); recvErr != nil {
			break
		}
		n++
	}
	if status.Code(recvErr) != codes.DeadlineExceeded {
		t.Fatalf("stream end err = %v, want DeadlineExceeded (idle cut)", status.Code(recvErr))
	}
	if n != 2 {
		t.Errorf("relayed %d frames before the idle cut, want 2", n)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("idle cut took %v — the backstop did not fire promptly", elapsed)
	}
	recs := audit.Records()
	if len(recs) != 2 {
		t.Fatalf("audit = %+v, want 2 (open + terminal)", recs)
	}
	if term := recs[1]; !term.Terminal || term.Outcome != "timeout" || term.Frames != 2 {
		t.Errorf("record[1] = %+v, want terminal {timeout, Frames:2}", term)
	}
}

// TestInvokeServerStreamRespectsSoftDeadline (C3): the deadline bound applies to
// streams too — a soft deadline sooner than the (default) idle window cuts a silent
// provider, recorded as a terminal {timeout} outcome.
func TestInvokeServerStreamRespectsSoftDeadline(t *testing.T) {
	client, audit := newStreamingGateway(t, idleStreamingState{frames: 1}, 0) // default 5m idle → the deadline fires first
	payload, _ := proto.Marshal(&statev1.WatchRequest{})
	stream, err := client.InvokeServerStream(callerCtxDeadline("rat-watch-caller", 200*time.Millisecond), &corev1.InvokeServerStreamRequest{
		Capability: "rat://state/v1/watch", Payload: payload,
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	start := time.Now()
	var recvErr error
	for {
		if _, recvErr = stream.Recv(); recvErr != nil {
			break
		}
	}
	if status.Code(recvErr) != codes.DeadlineExceeded {
		t.Fatalf("stream end err = %v, want DeadlineExceeded (soft deadline)", status.Code(recvErr))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("deadline cut took %v — the stream was not bounded by the soft deadline", elapsed)
	}
	if term := audit.Records()[len(audit.Records())-1]; !term.Terminal || term.Outcome != "timeout" {
		t.Errorf("terminal record = %+v, want {timeout}", term)
	}
}

// --- C2: channel-authenticated identity on the plugin door (#2 forgery fix) -----------

// bufServerAuth boots an in-process gateway server WITH the plugin-door auth interceptors
// (the 0.0.0.0 callback door), so caller_plugin is derived from the bearer token, not the wire.
func bufServerAuth(t *testing.T, gw *Gateway) corev1.CapabilityInvokeServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(gw.PluginAuthUnaryInterceptor),
		grpc.StreamInterceptor(gw.PluginAuthStreamInterceptor),
	)
	corev1.RegisterCapabilityInvokeServiceServer(srv, gw)
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
	return corev1.NewCapabilityInvokeServiceClient(conn)
}

// authGateway builds a token-authenticating gateway over the fakeState provider.
func authGateway(t *testing.T) (*Gateway, *MemAuditor) {
	t.Helper()
	providerConn := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeState{}) })
	audit := &MemAuditor{}
	gw := New(testRegistry(t), map[string]*grpc.ClientConn{"rat-test-state": providerConn}, audit,
		statev1.File_rat_state_v1_state_proto)
	gw.RequirePluginAuth(true)
	return gw, audit
}

// tokenCtx is callerCtx plus a bearer token header — a plugin presenting its token while the
// envelope claims to be `caller`.
func tokenCtx(caller, token string) context.Context {
	return metadata.AppendToOutgoingContext(callerCtx(caller), pluginTokenHeader, token)
}

// TestPluginAuthTokenIdentityWinsOverEnvelope: the gateway derives caller_plugin from the
// bearer token, NOT the wire envelope. A plugin holding rat-test-caller's token but forging an
// envelope that claims to be rat-test-state is authorized AS rat-test-caller — so its declared
// `requires state/get` lets the call through, and the audit records the token identity.
func TestPluginAuthTokenIdentityWinsOverEnvelope(t *testing.T) {
	gw, audit := authGateway(t)
	gw.SetPluginToken("tok-caller", "rat-test-caller")
	client := bufServerAuth(t, gw)

	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	// Envelope LIES: claims caller is rat-test-state; token says rat-test-caller.
	_, err := client.Invoke(tokenCtx("rat-test-state", "tok-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if err != nil {
		t.Fatalf("Invoke(get) err = %v, want nil (token identity rat-test-caller requires get)", err)
	}
	recs := audit.Records()
	if len(recs) != 1 || !recs[0].Allowed || recs[0].Caller != "rat-test-caller" {
		t.Errorf("audit = %+v, want one allow record with caller=rat-test-caller (token, not envelope)", recs)
	}
}

// TestPluginAuthForgedEnvelopeCannotEscalate: the inverse — a plugin holding rat-test-state's
// token (which `requires` nothing) cannot borrow rat-test-caller's grants by forging the
// envelope. The gateway authorizes AS rat-test-state, which does not declare get → DENIED.
func TestPluginAuthForgedEnvelopeCannotEscalate(t *testing.T) {
	gw, audit := authGateway(t)
	gw.SetPluginToken("tok-state", "rat-test-state")
	client := bufServerAuth(t, gw)

	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	// Envelope claims to be the privileged rat-test-caller; token says rat-test-state.
	_, err := client.Invoke(tokenCtx("rat-test-caller", "tok-state"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke(get) code = %v, want PermissionDenied (rat-test-state does not require get)", status.Code(err))
	}
	recs := audit.Records()
	if len(recs) != 1 || recs[0].Allowed || recs[0].Caller != "rat-test-state" {
		t.Errorf("audit = %+v, want one DENY record with caller=rat-test-state (token, not envelope)", recs)
	}
}

// TestPluginAuthMissingTokenRejected: the plugin door requires a token in launch mode — a call
// with none is Unauthenticated before any capability decision (no audit record).
func TestPluginAuthMissingTokenRejected(t *testing.T) {
	gw, audit := authGateway(t)
	client := bufServerAuth(t, gw)

	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	_, err := client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Invoke(get) with no token code = %v, want Unauthenticated", status.Code(err))
	}
	if recs := audit.Records(); len(recs) != 0 {
		t.Errorf("audit = %+v, want none (rejected before the capability decision)", recs)
	}
}

// TestPluginAuthInvalidTokenRejected: an unknown bearer token is Unauthenticated.
func TestPluginAuthInvalidTokenRejected(t *testing.T) {
	gw, _ := authGateway(t)
	gw.SetPluginToken("tok-caller", "rat-test-caller")
	client := bufServerAuth(t, gw)

	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	_, err := client.Invoke(tokenCtx("rat-test-caller", "bogus"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Invoke(get) with bogus token code = %v, want Unauthenticated", status.Code(err))
	}
}

// TestPluginTokenRevoked: RemovePluginToken drops a plugin's identity (live-deregister) — its
// previously valid token no longer authenticates.
func TestPluginTokenRevoked(t *testing.T) {
	gw, _ := authGateway(t)
	gw.SetPluginToken("tok-caller", "rat-test-caller")
	client := bufServerAuth(t, gw)
	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})

	if _, err := client.Invoke(tokenCtx("rat-test-caller", "tok-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	}); err != nil {
		t.Fatalf("before revoke: Invoke(get) err = %v, want nil", err)
	}
	gw.RemovePluginToken("rat-test-caller")
	if _, err := client.Invoke(tokenCtx("rat-test-caller", "tok-caller"), &corev1.InvokeRequest{
		Capability: "rat://state/v1/get", Payload: payload,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("after revoke: code = %v, want Unauthenticated", status.Code(err))
	}
}

// --- ADR-045: provider selection by label ---------------------------------------------

// selectCtx is callerCtx plus a rat-select label selector header.
func selectCtx(caller, selector string) context.Context {
	return metadata.AppendToOutgoingContext(callerCtx(caller), selectHeader, selector)
}

// TestSelectRoutesByLabel (ADR-045): two providers of ONE capability coexist, labeled
// compute=small/big; a call's selector routes to the matching provider's connection. No
// selector → FailedPrecondition (ambiguous, fail closed). A non-matching selector → likewise.
func TestSelectRoutesByLabel(t *testing.T) {
	connSmall := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeState{}) })  // "v:"+key
	connBig := bufServer(t, func(s *grpc.Server) { statev1.RegisterStateServiceServer(s, fakeStateB{}) })   // "REBOUND:"+key

	small := &manifest.Manifest{
		Kind: "state-backend", Metadata: manifest.Metadata{Name: "state-small", Labels: map[string]string{"compute": "small"}},
		Provides: []manifest.CapabilityRef{{Capability: "rat://state/v1/get"}},
	}
	big := &manifest.Manifest{
		Kind: "state-backend", Metadata: manifest.Metadata{Name: "state-big", Labels: map[string]string{"compute": "big"}},
		Provides: []manifest.CapabilityRef{{Capability: "rat://state/v1/get"}},
	}
	caller := &manifest.Manifest{
		Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-test-caller"},
		Requires: []manifest.CapabilityRef{{Capability: "rat://state/v1/get"}},
	}
	reg, err := registry.New([]*manifest.Manifest{small, big, caller})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	gw := New(reg, map[string]*grpc.ClientConn{"state-small": connSmall, "state-big": connBig}, &MemAuditor{},
		statev1.File_rat_state_v1_state_proto)
	client := corev1.NewCapabilityInvokeServiceClient(
		bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, gw) }))

	get := func(ctx context.Context) (string, error) {
		payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
		resp, err := client.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: payload})
		if err != nil {
			return "", err
		}
		var gr statev1.GetResponse
		_ = proto.Unmarshal(resp.GetResult(), &gr)
		return string(gr.GetValue()), nil
	}

	if v, err := get(selectCtx("rat-test-caller", "compute=big")); err != nil || v != "REBOUND:k1" {
		t.Fatalf("select compute=big = (%q, %v), want (REBOUND:k1, nil) — routed to state-big", v, err)
	}
	if v, err := get(selectCtx("rat-test-caller", "compute=small")); err != nil || v != "v:k1" {
		t.Fatalf("select compute=small = (%q, %v), want (v:k1, nil) — routed to state-small", v, err)
	}
	// No selector with two providers → ambiguous → fail closed.
	if _, err := get(callerCtx("rat-test-caller")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("no selector code = %v, want FailedPrecondition (ambiguous, fail closed)", status.Code(err))
	}
	// A selector matching no provider → fail closed.
	if _, err := get(selectCtx("rat-test-caller", "compute=gpu")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("non-matching selector code = %v, want FailedPrecondition", status.Code(err))
	}
}
