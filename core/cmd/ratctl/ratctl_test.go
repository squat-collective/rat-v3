package main

// ratctl_test.go proves the client→orchestrator path end to end: it brings up a real
// state plane in-process (the stateplugin launched via the local-process runtime),
// serves the gateway over TCP, and drives it with ratctl's own run() — exactly as a
// detached `ratctl` binary would. An authorized command routes to the launched plugin;
// an undeclared one is PERMISSION_DENIED (C5), surfaced as a gRPC status to the client.

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/supervisor"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func caps(uris ...string) []manifest.CapabilityRef {
	out := make([]manifest.CapabilityRef, len(uris))
	for i, u := range uris {
		out[i] = manifest.CapabilityRef{Capability: u}
	}
	return out
}

// serveStatePlane launches the stateplugin behind the gateway and serves it on a real
// TCP port, returning the dialable address. Cleanup tears the plane + server down.
func serveStatePlane(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "stateplugin")
	build := exec.Command("go", "build", "-o", bin, "./testplugins/stateplugin")
	build.Dir = "../.." // the core module root (this test runs in core/cmd/ratctl)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stateplugin: %v\n%s", err, out)
	}

	provider := &manifest.Manifest{Kind: "state-backend", Metadata: manifest.Metadata{Name: "rat-state"}, Provides: caps("rat://state/v1/get", "rat://state/v1/put")}
	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-caller"}, Requires: caps("rat://state/v1/get")}
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}

	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, deploymentruntime.NewLocalProcess(), []supervisor.PluginSpec{
		{Manifest: caller},
		{Manifest: provider, Launch: &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: iso}},
	}, &gateway.MemAuditor{}, 5*time.Second, statev1.File_rat_state_v1_state_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, plane.Gateway)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestRatctlCallsThroughGateway(t *testing.T) {
	addr := serveStatePlane(t)

	// (1) Authorized command: rat-caller requires get → routes to the launched plugin.
	var out bytes.Buffer
	err := run([]string{"call", "rat://state/v1/get", "--as", "rat-caller", "--data", `{"key":"k1"}`, "--addr", addr}, &out)
	if err != nil {
		t.Fatalf("ratctl call get (authorized): %v", err)
	}
	var gr statev1.GetResponse
	if err := protojson.Unmarshal(out.Bytes(), &gr); err != nil {
		t.Fatalf("response is not a GetResponse protojson: %v\n%s", err, out.String())
	}
	if !gr.GetFound() || !strings.HasPrefix(string(gr.GetValue()), "pid=") {
		t.Errorf("get response %q (found=%v) — expected a pid-tagged value from the launched plugin", gr.GetValue(), gr.GetFound())
	}

	// (2) Undeclared command: rat-caller does NOT require put → C5 deny, surfaced to
	// the client as a PermissionDenied gRPC status.
	var out2 bytes.Buffer
	err = run([]string{"call", "rat://state/v1/put", "--as", "rat-caller", "--data", `{"key":"k1","value":"eA=="}`, "--addr", addr}, &out2)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ratctl call put (undeclared) = %v, want PermissionDenied", err)
	}
}
