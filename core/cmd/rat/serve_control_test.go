package main

// serve_control_test.go is the ADR-027 wire proof: a REAL `rat serve` daemon (booted with
// only a caller driver) is driven through the ControlService over the actual socket to
// RegisterPlugin / DeregisterPlugin a provider against the RUNNING plane — and the gateway's
// C5 decision changes accordingly, with no restart. It registers a register-only PROVIDER
// driver (a manifest that `provides` the capability, no launch) so the proof is deterministic:
// the capability flips from PERMISSION_DENIED (no provider) → routed (provider known, so C5
// allows; routing then fails UNAVAILABLE since a driver has no live conn) → PERMISSION_DENIED
// again after deregister. The launch+health path is proven separately, with the real loop +
// elector, in control_test.go (a real-process launch is environment-flaky in CI sandboxes —
// see TestReconcilerDrivesRealRuntime).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	statev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const stateDriverManifest = `api_version: rat/1
kind: state-backend
metadata:
  name: rat-state-decl
  version: "0.1"
provides:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/put
`

func TestServeControlLiveRegister(t *testing.T) {
	ratBin := goBuild(t, "./cmd/rat", "rat")

	// Boot with ONLY the caller driver (requires get). No provider of get yet.
	planeDir := t.TempDir()
	planePath := filepath.Join(planeDir, "plane.yaml")
	planeYAML := "addr: 127.0.0.1:0\nruntime: local\nhealth_timeout: 10s\nplugins:\n  - name: rat-caller\n    manifest: " + mustAbs(t, "manifests/caller.plugin.yaml") + "\n"
	if err := os.WriteFile(planePath, []byte(planeYAML), 0o644); err != nil {
		t.Fatalf("write plane: %v", err)
	}

	daemon := exec.Command(ratBin, "serve", "--plane", planePath)
	var stdout, stderr safeBuf
	daemon.Stdout, daemon.Stderr = &stdout, &stderr
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- daemon.Wait() }()
	t.Cleanup(func() {
		if daemon.Process != nil {
			_ = daemon.Process.Kill()
		}
	})
	addr := waitServing(t, &stderr, waitCh)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	invoke := corev1.NewCapabilityInvokeServiceClient(conn)
	control := corev1.NewControlServiceClient(conn)

	getPayload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	getCode := func() codes.Code {
		ctx, cancel := context.WithTimeout(callerCtx(context.Background(), "rat-caller"), 5*time.Second)
		defer cancel()
		_, err := invoke.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: getPayload})
		return status.Code(err)
	}

	// (1) before: no provider of get → C5 denies.
	if c := getCode(); c != codes.PermissionDenied {
		t.Fatalf("get before register = %v, want PermissionDenied (no provider)", c)
	}

	// (2) LIVE-register a provider of get against the running daemon — no restart.
	if _, err := control.RegisterPlugin(context.Background(), &corev1.RegisterPluginRequest{
		Name: "rat-state-decl", ManifestYaml: []byte(stateDriverManifest),
	}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	// (3) after: the live registry now has a provider, so C5 AUTHORIZES the call — the
	// decision flipped without a restart (routing then fails UNAVAILABLE: a register-only
	// driver has no live conn, which is exactly the point — C5 passed, the wire changed).
	if c := getCode(); c == codes.PermissionDenied {
		t.Fatalf("get after LIVE register is still PermissionDenied — the live registry did not take effect")
	}

	// (4) ListPlugins reflects the live plane.
	lp, err := control.ListPlugins(context.Background(), &corev1.ListPluginsRequest{})
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	var names []string
	for _, p := range lp.GetPlugins() {
		names = append(names, p.GetName())
	}
	if !contains(names, "rat-caller") || !contains(names, "rat-state-decl") {
		t.Fatalf("ListPlugins = %v, want both rat-caller and rat-state-decl", names)
	}

	// (5) LIVE-deregister → C5 denies again (provider retracted), no restart.
	dr, err := control.DeregisterPlugin(context.Background(), &corev1.DeregisterPluginRequest{Name: "rat-state-decl"})
	if err != nil || !dr.GetWasPresent() {
		t.Fatalf("DeregisterPlugin = %+v err=%v, want was_present", dr, err)
	}
	if c := getCode(); c != codes.PermissionDenied {
		t.Fatalf("get after LIVE deregister = %v, want PermissionDenied (provider gone)", c)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
