package composition

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squat-collective/rat-v3/core/deploymentruntime"
	"github.com/squat-collective/rat-v3/core/gateway"
	"github.com/squat-collective/rat-v3/core/manifest"
	"github.com/squat-collective/rat-v3/core/supervisor"
	catalogv1 "github.com/squat-collective/rat-v3/gen/rat/catalog/v1"
	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/gen/rat/deploymentruntime/v1"
	formatv1 "github.com/squat-collective/rat-v3/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// C5 against REAL providers (Go refs): the SAME enforcing gateway + supervisor as
// composition_launched_test.go, but the catalog and format are the canonical ADR-003
// reference implementations (plugins/{catalog,format}/inmemory-go) — built as
// INDEPENDENT modules and launched as isolated processes, not our in-repo fakes. C5
// holds identically: declared caps route + return real results; a capability the real
// provider genuinely implements but the caller never declared is denied.

// buildRef builds an external plugins/<modRel> module to a temp binary the
// local-process runtime can launch. The refs are separate Go modules (own go.mod +
// replace → contracts/sdks/go), so this also proves the core enforces against
// independently-built plugins, the way a third party's would be.
func buildRef(t *testing.T, modRel string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), strings.ReplaceAll(modRel, "/", "-"))
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join("..", "..", "plugins", modRel) // this test runs in core/composition
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build plugins/%s: %v\n%s", modRel, err, out)
	}
	return bin
}

// TestC5AgainstRealGoProviders: the full pipeline runs through the REAL Go catalog +
// format refs (launched, isolated), returning REAL results; C5 then denies two
// capabilities the refs genuinely implement but the strategy never declared
// (format/merge, catalog/merge-branch). Every decision is audited (C4).
func TestC5AgainstRealGoProviders(t *testing.T) {
	catBin := buildRef(t, "catalog/inmemory-go")
	fmtBin := buildRef(t, "format/inmemory-go")

	rt := deploymentruntime.NewLocalProcess()
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}

	// The caller declares exactly the four pipeline caps — NOT merge / merge-branch.
	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-comp-strategy"},
		Requires: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://format/v1/overwrite", "rat://catalog/v1/commit-table")}
	// The real refs provide their FULL surface (incl. merge-branch / merge) — so a deny
	// is a genuine "provider offers it, caller didn't declare it", not "nobody offers it".
	catalogM := &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-catalog-inmemory-go"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/create-branch", "rat://catalog/v1/merge-branch")}
	formatM := &manifest.Manifest{Kind: "format", Metadata: manifest.Metadata{Name: "rat-format-inmemory-go"},
		Provides: caps("rat://format/v1/overwrite", "rat://format/v1/scan", "rat://format/v1/append", "rat://format/v1/merge", "rat://format/v1/maintain")}

	audit := &gateway.MemAuditor{}
	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
		{Manifest: caller}, // driver — registered for its requires, not launched
		{Manifest: catalogM, Launch: &deploymentruntimev1.LaunchSpec{Image: catBin, Isolation: iso}},
		{Manifest: formatM, Launch: &deploymentruntimev1.LaunchSpec{Image: fmtBin, Isolation: iso}},
	}, audit, 10*time.Second, catalogv1.File_rat_catalog_v1_catalog_proto, formatv1.File_rat_format_v1_format_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := corev1.NewCapabilityInvokeServiceClient(bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, plane.Gateway) }))

	// Full pipeline through the real refs → REAL results.
	r := runPipeline(t, client, "c5-real-1", false)
	if !r.committed {
		t.Fatal("pipeline did not commit through the real refs")
	}
	if r.writeSnap == "" || r.finalSnap != r.writeSnap {
		t.Errorf("commit-linkage broken: write %q, committed %q", r.writeSnap, r.finalSnap)
	}
	// The REAL catalog ref returns a catalog://<id>@<branch> URI (our fake returned
	// mem://…?pid=) — proof the call was served by the genuine reference, not a fake.
	if !strings.HasPrefix(r.sourceURI, "catalog://") {
		t.Errorf("source uri %q is not the real catalog ref's shape (catalog://…)", r.sourceURI)
	}
	t.Logf("real Go refs: source=%q writeSnap=%q committed=%q", r.sourceURI, r.writeSnap, r.finalSnap)

	// C4: the four pipeline hops are authorized + audited.
	if recs := audit.Records(); len(recs) != 4 {
		t.Fatalf("audit has %d records after the pipeline, want 4", len(recs))
	}

	// C5 against real providers: each undeclared capability the ref genuinely
	// implements is denied at the gateway.
	denyCtx := callerCtx("rat-comp-strategy")
	if err := invoke(denyCtx, client, "rat://format/v1/merge",
		&formatv1.MergeRequest{Table: &commonv1.TableRef{Identifier: outputTable}, MergeKeys: []string{"id"}}, &formatv1.MergeResponse{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("format/merge (undeclared) = %v, want PermissionDenied", status.Code(err))
	}
	if err := invoke(denyCtx, client, "rat://catalog/v1/merge-branch",
		&catalogv1.MergeBranchRequest{Branch: "run", IntoBranch: "main"}, &catalogv1.MergeBranchResponse{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("catalog/merge-branch (undeclared) = %v, want PermissionDenied", status.Code(err))
	}

	// C4: the two denials are audited too (6 total: 4 allow, then 2 deny).
	recs := audit.Records()
	if len(recs) != 6 {
		t.Fatalf("audit has %d records, want 6 (4 allow + 2 deny)", len(recs))
	}
	for _, rec := range recs[:4] {
		if !rec.Allowed {
			t.Errorf("pipeline hop %q denied: %s", rec.Capability, rec.Reason)
		}
	}
	for _, rec := range recs[4:] {
		if rec.Allowed {
			t.Errorf("undeclared capability %q allowed — C5 breach", rec.Capability)
		}
	}
}
