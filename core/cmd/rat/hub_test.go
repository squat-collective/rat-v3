package main

import (
	"context"
	"fmt"
	"net"
	"testing"

	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeWorkspace is a real workspace daemon's stand-in: it implements both the capability-invoke
// service (unary + server-stream) AND the control service (ListPlugins) with the default proto
// codec, so the hub's transparent proxy is exercised against an ordinary gRPC server.
type fakeWorkspace struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	corev1.UnimplementedControlServiceServer
}

func (fakeWorkspace) Invoke(_ context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	return &corev1.InvokeResponse{Result: append([]byte("echo:"), req.GetPayload()...)}, nil
}

func (fakeWorkspace) InvokeServerStream(_ *corev1.InvokeServerStreamRequest, stream grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) error {
	for i := 0; i < 3; i++ {
		if err := stream.Send(&corev1.InvokeServerStreamResponse{Result: []byte(fmt.Sprintf("frame-%d", i))}); err != nil {
			return err
		}
	}
	return nil
}

func (fakeWorkspace) ListPlugins(_ context.Context, _ *corev1.ListPluginsRequest) (*corev1.ListPluginsResponse, error) {
	return &corev1.ListPluginsResponse{Plugins: []*corev1.PluginStatus{{Name: "engine-spark", Kind: "engine", State: "Healthy"}}}, nil
}

func startFakeWorkspace(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("workspace listen: %v", err)
	}
	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, fakeWorkspace{})
	corev1.RegisterControlServiceServer(srv, fakeWorkspace{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func startHub(t *testing.T, resolve func(string) (string, bool)) (string, *hubServer) {
	t.Helper()
	pool := newConnPool()
	t.Cleanup(pool.closeAll)
	hub := &hubServer{pool: pool, resolve: resolve} // identityAddr "" = no edge auth
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hub listen: %v", err)
	}
	srv := grpc.NewServer(grpc.ForceServerCodec(proxyCodec{}), grpc.UnknownServiceHandler(hub.proxyStream))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), hub
}

// TestHubTransparentProxy: the hub forwards every method to the selected workspace over a POOLED
// connection — unary Invoke, server-streaming InvokeServerStream, AND ControlService.ListPlugins —
// with no per-method code. An unknown workspace is NotFound. After several calls the pool holds ONE
// reused connection (not a dial per call — gap #5).
func TestHubTransparentProxy(t *testing.T) {
	wsAddr := startFakeWorkspace(t)
	hubAddr, hub := startHub(t, func(ws string) (string, bool) {
		if ws == "ws1" {
			return wsAddr, true
		}
		return "", false
	})

	conn, err := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial hub: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	toWs1 := func() context.Context {
		return metadata.AppendToOutgoingContext(context.Background(), workspaceHeader, "ws1")
	}

	// 1. unary Invoke forwards + the response round-trips.
	inv := corev1.NewCapabilityInvokeServiceClient(conn)
	resp, err := inv.Invoke(toWs1(), &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: []byte("hi")})
	if err != nil || string(resp.GetResult()) != "echo:hi" {
		t.Fatalf("unary via hub = (%q, %v), want (echo:hi, nil)", resp.GetResult(), err)
	}

	// 2. server-streaming forwards every frame.
	stream, err := inv.InvokeServerStream(toWs1(), &corev1.InvokeServerStreamRequest{Capability: "rat://state/v1/watch"})
	if err != nil {
		t.Fatalf("open stream via hub: %v", err)
	}
	var frames []string
	for {
		msg, rerr := stream.Recv()
		if rerr != nil {
			break
		}
		frames = append(frames, string(msg.GetResult()))
	}
	if len(frames) != 3 || frames[0] != "frame-0" || frames[2] != "frame-2" {
		t.Fatalf("streamed frames via hub = %v, want [frame-0 frame-1 frame-2]", frames)
	}

	// 3. ControlService.ListPlugins forwards too (remote admin) — same transparent proxy, no code.
	lp, err := corev1.NewControlServiceClient(conn).ListPlugins(toWs1(), &corev1.ListPluginsRequest{})
	if err != nil || len(lp.GetPlugins()) != 1 || lp.GetPlugins()[0].GetName() != "engine-spark" {
		t.Fatalf("ListPlugins via hub = (%v, %v), want one plugin engine-spark", lp.GetPlugins(), err)
	}

	// 4. an unknown workspace is NotFound.
	bad := metadata.AppendToOutgoingContext(context.Background(), workspaceHeader, "nope")
	if _, err := inv.Invoke(bad, &corev1.InvokeRequest{Capability: "rat://state/v1/get"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown workspace code = %v, want NotFound", status.Code(err))
	}

	// 5. connections are POOLED: all the ws1 calls reused ONE conn (gap #5).
	hub.pool.mu.Lock()
	n := len(hub.pool.conns)
	hub.pool.mu.Unlock()
	if n != 1 {
		t.Fatalf("pool holds %d conns after several calls, want 1 (pooled, not dialed per call)", n)
	}
}

// TestHubRequiresWorkspace: a call with no rat-workspace is InvalidArgument (the hub can't route it).
func TestHubRequiresWorkspace(t *testing.T) {
	hubAddr, _ := startHub(t, func(string) (string, bool) { return "", false })
	conn, err := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial hub: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = corev1.NewCapabilityInvokeServiceClient(conn).Invoke(context.Background(), &corev1.InvokeRequest{Capability: "rat://state/v1/get"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no workspace code = %v, want InvalidArgument", status.Code(err))
	}
}
