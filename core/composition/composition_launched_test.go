package composition

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/supervisor"
	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This is the SAME composition pipeline as composition_test.go — but the catalog
// and format providers are now launched as isolated child processes by the
// supervisor (manifests -> Launch -> healthcheck -> dial -> register -> gateway),
// instead of running in-process behind bufconn. The frozen contracts, the
// manifest-derived C5 enforcement, and the C1 crash-recovery all hold across the
// real process boundary — proven by the providers reporting distinct OS PIDs in
// their response payloads (ADR-016 / D1; roadmap composition-through-launched).

var pidRe = regexp.MustCompile(`pid=(\d+)`)

// pidFrom extracts the serving process PID a provider embedded in a free-form
// response field (catalog: TableRef.uri; format: WriteResult.snapshot_id).
func pidFrom(t *testing.T, s string) int {
	t.Helper()
	m := pidRe.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no pid in %q (provider did not tag its response)", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("bad pid in %q: %v", s, err)
	}
	return n
}

// buildPlugin compiles ./testplugins/<pkg> to a temp binary the runtime can launch.
func buildPlugin(t *testing.T, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), pkg)
	cmd := exec.Command("go", "build", "-o", bin, "./testplugins/"+pkg)
	cmd.Dir = ".." // the core module root (this test runs in core/composition)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

// compositionManifests are the same three manifests the in-process harness uses:
// a strategy/driver declaring exactly the four caps the pipeline invokes, plus a
// catalog and a format declaring what they provide.
func compositionManifests() (strategy, catalogM, formatM *manifest.Manifest) {
	strategy = &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-comp-strategy"},
		Requires: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://format/v1/overwrite", "rat://catalog/v1/commit-table")}
	catalogM = &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-comp-catalog"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/merge-branch")}
	formatM = &manifest.Manifest{Kind: "format", Metadata: manifest.Metadata{Name: "rat-comp-format"},
		Provides: caps("rat://format/v1/overwrite", "rat://format/v1/scan", "rat://format/v1/append", "rat://format/v1/merge")}
	return
}

// bringUpLaunched builds the two provider binaries, launches them behind the
// gateway via the local-process runtime, and returns a client + the audit sink.
// The strategy is registered-only (the test itself is the caller); the catalog
// and format are launched, isolated child processes.
func bringUpLaunched(t *testing.T) (corev1.CapabilityInvokeServiceClient, *gateway.MemAuditor) {
	t.Helper()
	catBin := buildPlugin(t, "catalogplugin")
	fmtBin := buildPlugin(t, "formatplugin")

	rt := deploymentruntime.NewLocalProcess()
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}
	strategy, catalogM, formatM := compositionManifests()

	audit := &gateway.MemAuditor{}
	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
		{Manifest: strategy}, // a driver — registered (for its requires), not launched
		{Manifest: catalogM, Launch: &deploymentruntimev1.LaunchSpec{Image: catBin, Isolation: iso}},
		{Manifest: formatM, Launch: &deploymentruntimev1.LaunchSpec{Image: fmtBin, Isolation: iso}},
	}, audit, 5*time.Second, catalogv1.File_rat_catalog_v1_catalog_proto, formatv1.File_rat_format_v1_format_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	gwConn := bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, plane.Gateway) })
	return corev1.NewCapabilityInvokeServiceClient(gwConn), audit
}

// TestCompositionThroughLaunchedProviders: the full get-table -> register ->
// overwrite -> commit-table pipeline runs through the gateway to LAUNCHED, isolated
// provider processes. Commit-linkage holds across the process boundary, the catalog
// and format report DISTINCT OS PIDs (neither is the test process), C5 still denies
// an undeclared capability, and every decision is audited (C4).
func TestCompositionThroughLaunchedProviders(t *testing.T) {
	client, audit := bringUpLaunched(t)

	r := runPipeline(t, client, "run-1", false)
	if !r.committed {
		t.Fatal("pipeline did not commit")
	}
	if r.writeSnap == "" || r.finalSnap != r.writeSnap {
		t.Errorf("commit-linkage broken across the process boundary: write produced %q, catalog committed %q", r.writeSnap, r.finalSnap)
	}

	// Distinct PIDs: the catalog (from the get-table URI) and the format (from the
	// write snapshot) ran in separate child processes, neither of which is the test.
	catPID := pidFrom(t, r.sourceURI)
	fmtPID := pidFrom(t, r.writeSnap)
	self := os.Getpid()
	if catPID == self || fmtPID == self {
		t.Errorf("a provider served from the test process (pid %d): catalog=%d format=%d — expected launched children", self, catPID, fmtPID)
	}
	if catPID == fmtPID {
		t.Errorf("catalog and format share pid %d — expected two isolated processes", catPID)
	}
	t.Logf("served by isolated processes: test=%d catalog=%d format=%d", self, catPID, fmtPID)

	// C4: the four pipeline hops are all authorized + audited.
	recs := audit.Records()
	if len(recs) != 4 {
		t.Fatalf("audit has %d records, want 4 (get-table, register, overwrite, commit)", len(recs))
	}
	for _, rec := range recs {
		if !rec.Allowed {
			t.Errorf("hop %q denied: %s", rec.Capability, rec.Reason)
		}
	}

	// C5 holds through launched providers: the strategy declares overwrite, not
	// merge, so a merge attempt is denied at the gateway even though the launched
	// format process offers it. The denial is audited too (5th record).
	err := invoke(callerCtx("rat-comp-strategy"), client, "rat://format/v1/merge",
		&formatv1.MergeRequest{Table: &commonv1.TableRef{Identifier: outputTable}, MergeKeys: []string{"id"}}, &formatv1.MergeResponse{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("merge (undeclared) code = %v, want PermissionDenied", status.Code(err))
	}
	recs = audit.Records()
	if len(recs) != 5 || recs[4].Allowed {
		t.Fatalf("C5 deny not audited: got %d records, last allowed=%v", len(recs), recs[len(recs)-1].Allowed)
	}
}

// TestCompositionThroughLaunchedRecovers (C1): the crash-mid-strategy recovery
// holds through launched providers too. Attempt 1 crashes after the format write
// (before commit-table); attempt 2 re-runs with the same run id against the SAME
// launched processes — the replayed write is an idempotent no-op (already_applied,
// original snapshot), so the data is not written twice and the table commits once.
func TestCompositionThroughLaunchedRecovers(t *testing.T) {
	client, audit := bringUpLaunched(t)

	// Attempt 1: crashes after the write, before commit.
	r1 := runPipeline(t, client, "run-7", true)
	if r1.committed {
		t.Fatal("attempt 1 should have crashed before commit")
	}
	if r1.writeSnap == "" {
		t.Fatal("attempt 1 produced no write snapshot")
	}
	fmtPID := pidFrom(t, r1.writeSnap)
	if fmtPID == os.Getpid() {
		t.Fatalf("format write served from the test process (pid %d) — expected the launched child", fmtPID)
	}

	// Attempt 2: full re-run, same run id (the reconciler retry), same processes.
	r2 := runPipeline(t, client, "run-7", false)
	if !r2.committed {
		t.Fatal("attempt 2 did not commit")
	}
	if r2.writeSnap != r1.writeSnap {
		t.Errorf("replay produced a new snapshot %q, want the original %q (not idempotent!)", r2.writeSnap, r1.writeSnap)
	}
	if !r2.writeReplay {
		t.Error("replayed overwrite not flagged already_applied — the write leg is not idempotent across the boundary")
	}
	if r2.finalSnap != r1.writeSnap {
		t.Errorf("final committed snapshot = %q, want %q", r2.finalSnap, r1.writeSnap)
	}

	// C4: every hop across both attempts (3 + 4) was authorized + audited.
	recs := audit.Records()
	if len(recs) != 7 {
		t.Fatalf("audit has %d records, want 7 (attempt1: get/register/overwrite; attempt2: +commit)", len(recs))
	}
	for _, rec := range recs {
		if !rec.Allowed {
			t.Errorf("hop %q denied: %s", rec.Capability, rec.Reason)
		}
	}
}
