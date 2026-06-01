package deploymentruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func fullProfile() *deploymentruntimev1.IsolationProfile {
	return &deploymentruntimev1.IsolationProfile{
		RunAsNonRoot:        true,
		DropAllCapabilities: true,
		NoNewPrivileges:     true,
		ReadOnlyRootFs:      true,
		BlockMetadataEgress: true,
		SeccompProfile:      "RuntimeDefault",
	}
}

// requirePodman gates the LIVE test: it runs only under `make core-test-podman`
// (which sets RAT_PODMAN_TEST=1 in a privileged go+podman container). In the plain
// `make core-test` image there is no podman, so the test SKIPs rather than fails.
func requirePodman(t *testing.T) {
	t.Helper()
	if os.Getenv("RAT_PODMAN_TEST") == "" {
		t.Skip("podman live test: run `make core-test-podman` (sets RAT_PODMAN_TEST=1)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not on PATH")
	}
}

// buildProbeImage compiles the probe plugin static (CGO_ENABLED=0) and bakes it into
// a FROM-scratch image the podman runtime can launch — no base image to pull.
func buildProbeImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")
	build := exec.Command("go", "build", "-o", bin, "./testplugins/probeplugin")
	build.Dir = ".." // the core module root (this test runs in core/deploymentruntime)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build probeplugin: %v\n%s", err, out)
	}
	dockerfile := "FROM scratch\nCOPY probe /probe\nENTRYPOINT [\"/probe\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	tag := "localhost/rat-probe:test"
	if out, err := exec.Command("podman", "build", "-t", tag, dir).CombinedOutput(); err != nil {
		t.Fatalf("podman build: %v\n%s", err, out)
	}
	return tag
}

func allZeroHex(s string) bool { return s != "" && strings.Trim(s, "0") == "" }

// probeReport mirrors probeplugin's in-sandbox self-report JSON.
type probeReport struct {
	PID               int    `json:"pid"`
	UID               int    `json:"uid"`
	CapEff            string `json:"cap_eff"`
	NoNewPrivs        string `json:"no_new_privs"`
	RootWritable      bool   `json:"root_writable"`
	MetadataReachable bool   `json:"metadata_reachable"`
}

// TestPodmanFullProfile is the D1 closure proof: the podman runtime launches a plugin
// under the FULL I9 profile, and an in-container prober confirms the KERNEL actually
// enforced every control (not merely that the runtime requested it) — closing the
// reviews/08 D1 honesty gap. Then the lifecycle completes (Healthcheck receipt →
// Terminate → gone).
func TestPodmanFullProfile(t *testing.T) {
	requirePodman(t)
	image := buildProbeImage(t)
	rt := NewPodman()
	ctx := context.Background()

	lr, err := rt.Launch(ctx, &deploymentruntimev1.LaunchRequest{
		PluginId: "rat-probe",
		Spec:     &deploymentruntimev1.LaunchSpec{Image: image, Isolation: fullProfile()},
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		_, _ = rt.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: lr.GetInstanceId()})
	})

	// Health-check until HEALTHY (bounded), then assert the structured receipt claims
	// the full profile.
	var detail string
	deadline := time.Now().Add(20 * time.Second)
	for {
		hc, err := rt.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: lr.GetInstanceId()})
		if err != nil {
			t.Fatalf("Healthcheck: %v", err)
		}
		detail = hc.GetDetail()
		if hc.GetStatus() == deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe never became healthy: %s", detail)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var receipt isolationReceipt
	if err := json.Unmarshal([]byte(detail), &receipt); err != nil {
		t.Fatalf("Healthcheck detail is not the JSON isolation receipt: %v (%q)", err, detail)
	}
	if receipt.Kind != "podman" || !receipt.IsolationHonored.ReadOnlyRootFs || !receipt.IsolationHonored.BlockMetadataEgress {
		t.Errorf("receipt does not claim the full profile: %+v", receipt)
	}

	// Dial the launched container and read its in-sandbox self-report — the EMPIRICAL
	// proof the kernel enforced the profile.
	conn, err := grpc.NewClient(lr.GetEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", lr.GetEndpoint(), err)
	}
	defer conn.Close()
	getCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := statev1.NewStateServiceClient(conn).Get(getCtx, &statev1.GetRequest{Key: "probe"})
	if err != nil {
		t.Fatalf("probe Get: %v", err)
	}
	var p probeReport
	if err := json.Unmarshal(resp.GetValue(), &p); err != nil {
		t.Fatalf("probe report not JSON: %v (%q)", err, resp.GetValue())
	}

	// Each I9 control, ENFORCED (verified from inside the sandbox).
	if p.UID == 0 {
		t.Error("uid=0 inside container — run_as_non_root NOT enforced")
	}
	if !allZeroHex(p.CapEff) {
		t.Errorf("CapEff=%s — drop_all_capabilities NOT enforced (want all-zero)", p.CapEff)
	}
	if p.NoNewPrivs != "1" {
		t.Errorf("NoNewPrivs=%s — no_new_privileges NOT enforced (want 1)", p.NoNewPrivs)
	}
	if p.RootWritable {
		t.Error("root fs writable — read_only_root_fs NOT enforced")
	}
	if p.MetadataReachable {
		t.Error("169.254.169.254 reachable — block_metadata_egress NOT enforced")
	}
	t.Logf("full I9 enforced: uid=%d capEff=%s noNewPrivs=%s rootWritable=%v metadataReachable=%v seccomp=%s",
		p.UID, p.CapEff, p.NoNewPrivs, p.RootWritable, p.MetadataReachable, receipt.SeccompProfile)

	// Terminate → the instance is gone.
	tr, err := rt.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: lr.GetInstanceId()})
	if err != nil || !tr.GetTerminated() {
		t.Fatalf("Terminate: err=%v terminated=%v", err, tr.GetTerminated())
	}
	if _, err := rt.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: lr.GetInstanceId()}); status.Code(err) != codes.NotFound {
		t.Errorf("Healthcheck after Terminate = %v, want NotFound", status.Code(err))
	}
}

// TestPodmanRefusesBelowI9: the shared I9 gate is wired in the podman runtime too —
// a profile below the minimum is refused BEFORE any container is launched (so this
// runs without podman present).
func TestPodmanRefusesBelowI9(t *testing.T) {
	rt := NewPodman()
	_, err := rt.Launch(context.Background(), &deploymentruntimev1.LaunchRequest{
		PluginId: "x",
		// drop_all_capabilities missing.
		Spec: &deploymentruntimev1.LaunchSpec{Image: "ghcr.io/rat-dev/x:1", Isolation: &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, NoNewPrivileges: true}},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Launch below the I9 minimum = %v, want FailedPrecondition", status.Code(err))
	}
}

// TestPodmanRejectsEmptyImage: no image → INVALID_ARGUMENT (pre-exec; no podman needed).
func TestPodmanRejectsEmptyImage(t *testing.T) {
	rt := NewPodman()
	_, err := rt.Launch(context.Background(), &deploymentruntimev1.LaunchRequest{
		PluginId: "x",
		Spec:     &deploymentruntimev1.LaunchSpec{Image: "", Isolation: fullProfile()},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Launch with empty image = %v, want InvalidArgument", status.Code(err))
	}
}
