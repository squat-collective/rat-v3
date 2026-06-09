package composition

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/supervisor"
	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// C5 against a REAL-backend provider in a REAL container: the SQLite catalog ref
// (plugins/catalog/sqlite-py), launched by the podman runtime under the FULL I9
// profile, behind the gateway. Declared caps (get-table, commit-table) route to real
// SQLite and return real results; an undeclared cap the ref implements (merge-branch)
// is DENIED. This ties C5 + the supervisor + the podman runtime together end-to-end.
// Gated by RAT_PODMAN_TEST → runs under `make core-test-podman`, SKIPs in core-test.

func requirePodman(t *testing.T) {
	t.Helper()
	if os.Getenv("RAT_PODMAN_TEST") == "" {
		t.Skip("podman live test: run `make core-test-podman` (sets RAT_PODMAN_TEST=1)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not on PATH")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// buildSqliteCatalogImage assembles a minimal build context — the sqlite-py ref's
// source plus the generated Python SDK (the rat package) — and builds a container
// image the podman runtime can launch. The Dockerfile is inline (and tiny) because
// the context spans two repo locations (the ref + the SDK); keeping it with the
// assembly keeps the two in sync.
func buildSqliteCatalogImage(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..") // repo root, from core/composition
	ctx := t.TempDir()
	for _, f := range []string{"main.py", "server.py", "store.py"} {
		copyFile(t, filepath.Join(root, "plugins", "catalog", "sqlite-py", f), filepath.Join(ctx, f))
	}
	if out, err := exec.Command("cp", "-r", filepath.Join(root, "contracts", "sdks", "python", "rat"), filepath.Join(ctx, "rat")).CombinedOutput(); err != nil {
		t.Fatalf("copy python SDK: %v\n%s", err, out)
	}
	dockerfile := "FROM docker.io/library/python:3.12-slim\n" +
		"COPY rat /app/rat\n" +
		"COPY main.py server.py store.py /app/\n" +
		"WORKDIR /app\n" +
		"RUN pip install --no-cache-dir grpcio==1.80.0 protobuf==7.35.0\n" +
		"ENV PYTHONPATH=/app PYTHONDONTWRITEBYTECODE=1\n" +
		"ENTRYPOINT [\"python\", \"main.py\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	tag := "localhost/rat-catalog-sqlite:test"
	if out, err := exec.Command("podman", "build", "-t", tag, ctx).CombinedOutput(); err != nil {
		t.Fatalf("podman build sqlite catalog: %v\n%s", err, out)
	}
	return tag
}

// TestC5SqliteCatalogViaPodman: C5 enforced against the SQLite catalog ref launched as
// a real container by the podman runtime under the full I9 profile. ("Podman" in the
// name → picked up by `make core-test-podman`.)
func TestC5SqliteCatalogViaPodman(t *testing.T) {
	requirePodman(t)
	image := buildSqliteCatalogImage(t)

	rt := deploymentruntime.NewPodman()
	full := &deploymentruntimev1.IsolationProfile{
		RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true,
		ReadOnlyRootFs: true, BlockMetadataEgress: true, SeccompProfile: "RuntimeDefault",
	}
	// Declares get-table + commit-table — NOT merge-branch.
	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-comp-strategy"},
		Requires: caps("rat://catalog/v1/get-table", "rat://catalog/v1/commit-table")}
	catalogM := &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-catalog-sqlite-py"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/create-branch", "rat://catalog/v1/merge-branch")}

	audit := &gateway.MemAuditor{}
	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
		{Manifest: caller},
		{Manifest: catalogM, Launch: &deploymentruntimev1.LaunchSpec{
			Image:     image,
			Isolation: full,
			// read-only root → the SQLite WAL db lives on the runtime's /tmp tmpfs.
			Env: map[string]string{"RAT_CATALOG_DB": "/tmp/catalog.db", "PYTHONDONTWRITEBYTECODE": "1"},
		}},
	}, audit, 30*time.Second, catalogv1.File_rat_catalog_v1_catalog_proto)
	if err != nil {
		t.Fatalf("BringUp (podman + sqlite catalog): %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := corev1.NewCapabilityInvokeServiceClient(bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, plane.Gateway) }))
	callCtx := callerCtx("rat-comp-strategy")

	// ALLOW: get-table on the seeded table → REAL SQLite result.
	var gt catalogv1.GetTableResponse
	if err := invoke(callCtx, client, "rat://catalog/v1/get-table", &catalogv1.GetTableRequest{Identifier: sourceTable}, &gt); err != nil {
		t.Fatalf("get-table (declared) via the sqlite container: %v", err)
	}
	if gt.GetTable().GetIdentifier() != sourceTable {
		t.Errorf("get-table returned %q, want %q", gt.GetTable().GetIdentifier(), sourceTable)
	}
	t.Logf("real sqlite container served: identifier=%q uri=%q branch=%q", gt.GetTable().GetIdentifier(), gt.GetTable().GetUri(), gt.GetTable().GetBranch())

	// ALLOW: commit-table → REAL SQLite write (commit-linkage persisted to the WAL db).
	var ct catalogv1.CommitTableResponse
	if err := invoke(callCtx, client, "rat://catalog/v1/commit-table",
		&catalogv1.CommitTableRequest{Identifier: sourceTable, SnapshotId: "snap-c5", IdempotencyKey: "c5-podman"}, &ct); err != nil {
		t.Fatalf("commit-table (declared) via the sqlite container: %v", err)
	}
	if ct.GetSnapshotId() != "snap-c5" {
		t.Errorf("commit-table snapshot = %q, want snap-c5", ct.GetSnapshotId())
	}

	// DENY: merge-branch is undeclared by the caller → C5 PermissionDenied, even though
	// the real sqlite catalog implements it.
	if err := invoke(callCtx, client, "rat://catalog/v1/merge-branch",
		&catalogv1.MergeBranchRequest{Branch: "run", IntoBranch: "main"}, &catalogv1.MergeBranchResponse{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("merge-branch (undeclared) = %v, want PermissionDenied", status.Code(err))
	}

	// C4: allow, allow, deny — all audited.
	recs := audit.Records()
	if len(recs) != 3 || !recs[0].Allowed || !recs[1].Allowed || recs[2].Allowed {
		t.Fatalf("audit = %+v, want [allow get-table, allow commit-table, deny merge-branch]", recs)
	}
}
