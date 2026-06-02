package supervisor

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// attachedState is an already-running state provider (the attach-mode case: the
// orchestrator started it; the daemon only dials it). Tags its value with the serving
// PID so the routed call is provably served by THIS process, not the gateway.
type attachedState struct {
	statev1.UnimplementedStateServiceServer
}

func (attachedState) Get(_ context.Context, req *statev1.GetRequest) (*statev1.GetResponse, error) {
	return &statev1.GetResponse{Found: true, Value: []byte(fmt.Sprintf("attached pid=%d key=%s", os.Getpid(), req.GetKey()))}, nil
}

// TestAttachRoutesThroughRunningPlugin: a state provider is already running on a TCP
// port; Attach dials it (no launch), registers it + a caller driver, and the gateway
// routes a C5-authorized call to it. An undeclared capability is denied. This is the
// attach-mode keystone for the always-on compose stack (ADR-020 S1) — compose starts
// the plugins, the daemon connects by address, no docker-in-docker.
func TestAttachRoutesThroughRunningPlugin(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	psrv := grpc.NewServer()
	statev1.RegisterStateServiceServer(psrv, attachedState{})
	go func() { _ = psrv.Serve(lis) }()
	t.Cleanup(psrv.Stop)
	addr := lis.Addr().String()

	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-test-caller"}, Requires: caps("rat://state/v1/get")}
	provider := &manifest.Manifest{Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-attached-state"}, Provides: caps("rat://state/v1/get", "rat://state/v1/put")}

	audit := &gateway.MemAuditor{}
	ctx := context.Background()
	plane, err := Attach(ctx, []PluginSpec{
		{Manifest: caller},                   // a driver — registered (for its requires), not dialed
		{Manifest: provider, Endpoint: addr}, // attach mode — dial the already-running provider
	}, audit, 5*time.Second, statev1.File_rat_state_v1_state_proto)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := serveGateway(t, plane.Gateway)

	// Authorized call → routed through the gateway to the ATTACHED provider.
	payload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	getCtx, cancel := context.WithTimeout(callerCtx("rat-test-caller"), 3*time.Second)
	defer cancel()
	resp, err := client.Invoke(getCtx, &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: payload})
	if err != nil {
		t.Fatalf("Invoke get: %v", err)
	}
	var gr statev1.GetResponse
	if err := proto.Unmarshal(resp.GetResult(), &gr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !gr.GetFound() || string(gr.GetValue())[:8] != "attached" {
		t.Errorf("get response %q — expected the attached provider's value", gr.GetValue())
	}

	// Undeclared capability → C5 deny, even though the attached provider offers put.
	putPayload, _ := proto.Marshal(&statev1.PutRequest{Key: "k1", Value: []byte("x")})
	_, err = client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{Capability: "rat://state/v1/put", Payload: putPayload})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("put (undeclared) = %v, want PermissionDenied", status.Code(err))
	}
}
