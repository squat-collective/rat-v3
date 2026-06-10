package main

// serve_test.go is the ADR-019 Phase-A exit-criteria proof: it builds the `rat`
// daemon + the stateplugin, runs the daemon as a REAL subprocess serving the
// gateway over TCP, and drives it with a real gRPC client to prove —
//
//   - an authorized capability routes through the launched plugin (C5 allow) and
//     emits an audit line,
//   - an undeclared capability is PERMISSION_DENIED (C5 deny) and is audited,
//   - SIGTERM drains the daemon cleanly (exit 0, "drained" logged, no leak).
//
// This is the first time the core runs as a server, exercised exactly as a real
// operator would: `rat serve --plane plane.yaml`, signalled with SIGTERM.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	statev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// safeBuf is a concurrency-safe buffer — the daemon writes stdout/stderr from its
// own goroutines while the test reads them.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// goBuild compiles a package under the core module to a temp binary.
func goBuild(t *testing.T, pkg, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = "../.." // the core module root (this test runs in core/cmd/rat)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs %s: %v", p, err)
	}
	return a
}

// callerCtx builds an outgoing context carrying the rat-callmeta-bin envelope the
// gateway reads: a well-formed traceparent (C1) + the caller identity C5 checks.
func callerCtx(ctx context.Context, caller string) context.Context {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01", CorrelationId: "smoke-1"},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: "t1"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(ctx, "rat-callmeta-bin", string(b))
}

var servingRe = regexp.MustCompile(`control (\S+)`)

// TestServeRoutesDeniesAndDrains is the Phase-A end-to-end proof (see file header).
func TestServeRoutesDeniesAndDrains(t *testing.T) {
	statePlugin := goBuild(t, "./testplugins/stateplugin", "stateplugin")
	ratBin := goBuild(t, "./cmd/rat", "rat")

	// A temp plane: launch the stateplugin (provides get+put), register a caller
	// driver (requires get only). addr :0 → the daemon picks a free port and logs it.
	planeDir := t.TempDir()
	planePath := filepath.Join(planeDir, "plane.yaml")
	planeYAML := fmt.Sprintf(`addr: 127.0.0.1:0
runtime: local
health_timeout: 10s
plugins:
  - name: rat-state
    manifest: %s
    launch:
      image: %s
      isolation: i9
  - name: rat-caller
    manifest: %s
`, mustAbs(t, "manifests/state.plugin.yaml"), statePlugin, mustAbs(t, "manifests/caller.plugin.yaml"))
	if err := os.WriteFile(planePath, []byte(planeYAML), 0o644); err != nil {
		t.Fatalf("write plane: %v", err)
	}

	daemon := exec.Command(ratBin, "serve", "--plane", planePath)
	var stdout, stderr safeBuf
	daemon.Stdout = &stdout // audit lines (StdoutAuditor)
	daemon.Stderr = &stderr // daemon logs (the "serving on <addr>" line we parse)
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	// A single Wait goroutine reaps the daemon; both the boot-failure check and the
	// drain check read its result (Wait must be called exactly once).
	waitCh := make(chan error, 1)
	go func() { waitCh <- daemon.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited && daemon.Process != nil {
			_ = daemon.Process.Kill() // the Wait goroutine reaps it
		}
	})

	// Wait for the daemon to bring the plugin up and start serving; capture the addr.
	addr := waitServing(t, &stderr, waitCh)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gateway %s: %v", addr, err)
	}
	defer conn.Close()
	client := corev1.NewCapabilityInvokeServiceClient(conn)

	// (1) Authorized: rat-caller requires get → routes to the launched stateplugin.
	getPayload, _ := proto.Marshal(&statev1.GetRequest{Key: "k1"})
	getCtx, cancel := context.WithTimeout(callerCtx(context.Background(), "rat-caller"), 5*time.Second)
	defer cancel()
	resp, err := client.Invoke(getCtx, &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: getPayload})
	if err != nil {
		t.Fatalf("Invoke get (authorized): %v", err)
	}
	var gr statev1.GetResponse
	if err := proto.Unmarshal(resp.GetResult(), &gr); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if !strings.HasPrefix(string(gr.GetValue()), "pid=") {
		t.Errorf("get response %q — expected a pid-tagged value from the launched plugin", gr.GetValue())
	}

	// (2) Undeclared: rat-caller does NOT require put → C5 deny, even though the
	// launched provider offers it.
	putPayload, _ := proto.Marshal(&statev1.PutRequest{Key: "k1", Value: []byte("x")})
	_, err = client.Invoke(callerCtx(context.Background(), "rat-caller"), &corev1.InvokeRequest{Capability: "rat://state/v1/put", Payload: putPayload})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke put (undeclared) = %v, want PermissionDenied", status.Code(err))
	}

	// (3) Both decisions appear in the audit log (allow get, deny put).
	assertAudit(t, &stdout)

	// (4) SIGTERM drains cleanly: exit 0 + "drained" logged.
	if err := daemon.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	select {
	case err := <-waitCh:
		exited = true
		if err != nil {
			t.Fatalf("daemon exited uncleanly on SIGTERM: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(shutdownGrace + 5*time.Second):
		t.Fatalf("daemon did not drain within the grace window after SIGTERM; stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "drained") {
		t.Errorf("daemon did not log a clean drain; stderr:\n%s", stderr.String())
	}
}

// waitServing polls the daemon's stderr for the "serving on <addr>" line, failing
// fast (with the daemon's output) if it exits before serving.
func waitServing(t *testing.T, stderr *safeBuf, waitCh <-chan error) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m := servingRe.FindStringSubmatch(stderr.String()); m != nil {
			return m[1]
		}
		select {
		case err := <-waitCh:
			t.Fatalf("daemon exited before serving (%v); stderr:\n%s", err, stderr.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("daemon never logged a serving address; stderr:\n%s", stderr.String())
	return ""
}

// assertAudit checks the stdout audit log carries an allow for get and a deny for
// put. It polls: the daemon writes the deny record just before returning the gRPC
// error, so the OS-pipe copy into our buffer can lag the client seeing the error.
func assertAudit(t *testing.T, stdout *safeBuf) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var gotAllowGet, gotDenyPut bool
		for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
			if line == "" {
				continue
			}
			var rec struct {
				Capability string `json:"capability"`
				Allowed    bool   `json:"allowed"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("audit line is not JSON: %q (%v)", line, err)
			}
			switch {
			case rec.Capability == "rat://state/v1/get" && rec.Allowed:
				gotAllowGet = true
			case rec.Capability == "rat://state/v1/put" && !rec.Allowed:
				gotDenyPut = true
			}
		}
		if gotAllowGet && gotDenyPut {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("audit log missing decisions (allow get=%v, deny put=%v); stdout:\n%s", gotAllowGet, gotDenyPut, stdout.String())
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
