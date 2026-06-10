package supervisor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squat-collective/rat-v3/core/deploymentruntime"
	"github.com/squat-collective/rat-v3/core/gateway"
	"github.com/squat-collective/rat-v3/core/manifest"
	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/gen/rat/deploymentruntime/v1"
	statev1 "github.com/squat-collective/rat-v3/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func buildStatePlugin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "stateplugin")
	cmd := exec.Command("go", "build", "-o", bin, "./testplugins/stateplugin")
	cmd.Dir = ".." // the core module root (this test runs in core/supervisor)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stateplugin: %v\n%s", err, out)
	}
	return bin
}

func caps(uris ...string) []manifest.CapabilityRef {
	out := make([]manifest.CapabilityRef, len(uris))
	for i, u := range uris {
		out[i] = manifest.CapabilityRef{Capability: u}
	}
	return out
}

func callerCtx(caller string) context.Context {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01", CorrelationId: "c"},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: "t1"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(context.Background(), "rat-callmeta-bin", string(b))
}

func serveGateway(t *testing.T, gw *gateway.Gateway) corev1.CapabilityInvokeServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, gw)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return corev1.NewCapabilityInvokeServiceClient(conn)
}

// TestBringUpRoutesThroughLaunchedPlugin: the supervisor launches a real state
// plugin via the local-process runtime, registers it, and the gateway routes a
// C5-authorized call to the LAUNCHED process (distinct PID); an undeclared
// capability is denied; Shutdown terminates the child.
func TestBringUpRoutesThroughLaunchedPlugin(t *testing.T) {
	bin := buildStatePlugin(t)
	rt := deploymentruntime.NewLocalProcess()
	ctx := context.Background()

	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-test-caller"}, Requires: caps("rat://state/v1/get")}
	provider := &manifest.Manifest{Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-test-state"}, Provides: caps("rat://state/v1/get", "rat://state/v1/put")}
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}

	audit := &gateway.MemAuditor{}
	plane, err := BringUp(ctx, rt, []PluginSpec{
		{Manifest: caller}, // a driver — registered (for its requires), not launched
		{Manifest: provider, Launch: &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: iso}},
	}, audit, 5*time.Second, statev1.File_rat_state_v1_state_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := serveGateway(t, plane.Gateway)

	// Authorized call -> routed through the gateway to the LAUNCHED provider process.
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
	var childPID int
	if _, err := fmt.Sscanf(string(gr.GetValue()), "pid=%d", &childPID); err != nil {
		t.Fatalf("parse pid from %q: %v", gr.GetValue(), err)
	}
	if childPID == 0 || childPID == os.Getpid() {
		t.Errorf("call served by PID %d (test PID %d) — expected the launched child process", childPID, os.Getpid())
	}

	// Undeclared capability -> C5 deny, even though the launched provider offers put.
	putPayload, _ := proto.Marshal(&statev1.PutRequest{Key: "k1", Value: []byte("x")})
	_, err = client.Invoke(callerCtx("rat-test-caller"), &corev1.InvokeRequest{Capability: "rat://state/v1/put", Payload: putPayload})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("put (undeclared) = %v, want PermissionDenied", status.Code(err))
	}
}

// TestBringUpRefusesBadPluginTearsDown: a launch that fails the I9 gate aborts the
// bring-up with an error (and leaves nothing running to leak).
func TestBringUpRefusesBadPlugin(t *testing.T) {
	bin := buildStatePlugin(t)
	rt := deploymentruntime.NewLocalProcess()
	ctx := context.Background()

	provider := &manifest.Manifest{Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-bad-state"}, Provides: caps("rat://state/v1/get")}
	// Isolation below the I9 minimum -> Launch must fail -> BringUp errors.
	_, err := BringUp(ctx, rt, []PluginSpec{
		{Manifest: provider, Launch: &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true}}},
	}, &gateway.MemAuditor{}, 5*time.Second, statev1.File_rat_state_v1_state_proto)
	if err == nil {
		t.Fatal("BringUp succeeded with a below-I9 plugin; want an error")
	}
}
