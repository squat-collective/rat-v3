package composition

import (
	"context"
	"testing"
	"time"

	"github.com/squat-collective/rat-v3/core/deploymentruntime"
	"github.com/squat-collective/rat-v3/core/gateway"
	"github.com/squat-collective/rat-v3/core/manifest"
	"github.com/squat-collective/rat-v3/core/supervisor"
	catalogv1 "github.com/squat-collective/rat-v3/gen/rat/catalog/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
)

// C1 against REAL backends. The crash-mid-strategy at-least-once idempotency was proven
// against the in-repo fakes (composition_test.go); here it is re-proven against the real
// catalog refs, whose commit-key ledgers are genuine (an in-memory map in inmemory-go, a
// durable SQL table in sqlite-py). The format leg can't be re-proven — the real
// inmemory-go format deliberately ignores idempotency_key — so C1-real rides on the
// catalog's CommitTable/MergeBranch idempotency.

func c1GatewayClient(t *testing.T, plane *supervisor.Plane) corev1.CapabilityInvokeServiceClient {
	t.Helper()
	return corev1.NewCapabilityInvokeServiceClient(bufServer(t, func(s *grpc.Server) {
		corev1.RegisterCapabilityInvokeServiceServer(s, plane.Gateway)
	}))
}

func commitTable(t *testing.T, c corev1.CapabilityInvokeServiceClient, ctx context.Context, identifier, snapshot, key string) *catalogv1.CommitTableResponse {
	t.Helper()
	var ct catalogv1.CommitTableResponse
	if err := invoke(ctx, c, "rat://catalog/v1/commit-table",
		&catalogv1.CommitTableRequest{Identifier: identifier, SnapshotId: snapshot, IdempotencyKey: key}, &ct); err != nil {
		t.Fatalf("commit-table: %v", err)
	}
	return &ct // pointer: a proto message contains a mutex and must not be copied
}

// TestC1AgainstRealCatalogRetry: at-least-once replay against the real inmemory-go
// catalog (launched, behind the gateway) is a no-op — a retry with the same
// idempotency_key returns already_applied with the ORIGINAL result, even if the retry's
// payload drifted. Covers both the commit leg (CommitTable) and the publish leg
// (MergeBranch). Runs in core-test (no podman).
func TestC1AgainstRealCatalogRetry(t *testing.T) {
	bin := buildRef(t, "catalog/inmemory-go")
	rt := deploymentruntime.NewLocalProcess()
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}

	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-c1-caller"},
		Requires: caps("rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/create-branch", "rat://catalog/v1/merge-branch")}
	catalogM := &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-catalog-inmemory-go"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/create-branch", "rat://catalog/v1/merge-branch")}

	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
		{Manifest: caller},
		{Manifest: catalogM, Launch: &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: iso}},
	}, &gateway.MemAuditor{}, 10*time.Second, catalogv1.File_rat_catalog_v1_catalog_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := c1GatewayClient(t, plane)
	callCtx := callerCtx("rat-c1-caller")

	// Register the output table, then commit a snapshot under run key K.
	var rt0 catalogv1.RegisterTableResponse
	if err := invoke(callCtx, client, "rat://catalog/v1/register-table", &catalogv1.RegisterTableRequest{Identifier: outputTable}, &rt0); err != nil {
		t.Fatalf("register-table: %v", err)
	}
	first := commitTable(t, client, callCtx, outputTable, "snap-A", "K1")
	if first.GetAlreadyApplied() || first.GetSnapshotId() != "snap-A" {
		t.Fatalf("first commit = {snap:%q applied:%v}, want {snap-A, false}", first.GetSnapshotId(), first.GetAlreadyApplied())
	}
	// Replay with the SAME key: a no-op returning the original (already_applied).
	replay := commitTable(t, client, callCtx, outputTable, "snap-A", "K1")
	if !replay.GetAlreadyApplied() || replay.GetSnapshotId() != "snap-A" {
		t.Errorf("replay commit = {snap:%q applied:%v}, want {snap-A, true}", replay.GetSnapshotId(), replay.GetAlreadyApplied())
	}
	// Drift: a retry whose payload changed but key is the same still returns the ORIGINAL
	// — the key anchors the result, so an at-least-once retry can't double-apply or drift.
	drift := commitTable(t, client, callCtx, outputTable, "snap-DIFFERENT", "K1")
	if !drift.GetAlreadyApplied() || drift.GetSnapshotId() != "snap-A" {
		t.Errorf("drifted replay = {snap:%q applied:%v}, want the original {snap-A, true}", drift.GetSnapshotId(), drift.GetAlreadyApplied())
	}

	// Publish leg: MergeBranch is idempotent under the same key too.
	if err := invoke(callCtx, client, "rat://catalog/v1/create-branch", &catalogv1.CreateBranchRequest{Branch: "run-1"}, &catalogv1.CreateBranchResponse{}); err != nil {
		t.Fatalf("create-branch: %v", err)
	}
	merge := func() *catalogv1.MergeBranchResponse {
		var mb catalogv1.MergeBranchResponse
		if err := invoke(callCtx, client, "rat://catalog/v1/merge-branch",
			&catalogv1.MergeBranchRequest{Branch: "run-1", IntoBranch: "main", IdempotencyKey: "M1"}, &mb); err != nil {
			t.Fatalf("merge-branch: %v", err)
		}
		return &mb // pointer: proto message contains a mutex
	}
	m1 := merge()
	if m1.GetAlreadyApplied() {
		t.Errorf("first merge already_applied=true, want false")
	}
	m2 := merge()
	if !m2.GetAlreadyApplied() || m2.GetSnapshotId() != m1.GetSnapshotId() {
		t.Errorf("merge replay = {snap:%q applied:%v}, want {%q, true}", m2.GetSnapshotId(), m2.GetAlreadyApplied(), m1.GetSnapshotId())
	}
}

// TestC1DurableLedgerSurvivesRestartViaPodman: the gold-standard crash-safety proof —
// the idempotency ledger survives a real BACKEND crash. The sqlite catalog is launched
// by the podman runtime with a PERSISTENT data volume; a commit is recorded, the catalog
// container is then TORN DOWN (Shutdown), and a fresh container is relaunched on the SAME
// durable db — a replay with the same key still returns already_applied. The durable SQL
// ledger outlived the crash: something an in-memory backend (or our fakes) fundamentally
// cannot do. ("Podman" in the name → make core-test-podman runs it.)
func TestC1DurableLedgerSurvivesRestartViaPodman(t *testing.T) {
	requirePodman(t)
	image := buildSqliteCatalogImage(t)
	dataRoot := t.TempDir() // persists across the simulated backend crash (host dir, not the container)
	ctx := context.Background()

	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-c1-caller"},
		Requires: caps("rat://catalog/v1/commit-table")}
	// SAME manifest name across both cycles → same plugin_id → same /data dir → same db.
	catalogM := &manifest.Manifest{Kind: "catalog", Metadata: manifest.Metadata{Name: "rat-catalog-sqlite-py"},
		Provides: caps("rat://catalog/v1/get-table", "rat://catalog/v1/register-table", "rat://catalog/v1/commit-table", "rat://catalog/v1/create-branch", "rat://catalog/v1/merge-branch")}
	full := &deploymentruntimev1.IsolationProfile{
		RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true,
		ReadOnlyRootFs: true, BlockMetadataEgress: true, SeccompProfile: "RuntimeDefault",
	}
	launch := &deploymentruntimev1.LaunchSpec{
		Image: image, Isolation: full,
		Env: map[string]string{"RAT_CATALOG_DB": "/data/catalog.db", "PYTHONDONTWRITEBYTECODE": "1"},
	}

	bringUp := func() *supervisor.Plane {
		rt := deploymentruntime.NewPodman()
		rt.DataRoot = dataRoot // persistent /data, keyed by plugin_id
		plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
			{Manifest: caller},
			{Manifest: catalogM, Launch: launch},
		}, &gateway.MemAuditor{}, 40*time.Second, catalogv1.File_rat_catalog_v1_catalog_proto)
		if err != nil {
			t.Fatalf("BringUp: %v", err)
		}
		return plane
	}

	callCtx := callerCtx("rat-c1-caller")

	// Cycle 1: commit under key K against the durable sqlite catalog, then CRASH it
	// (Shutdown removes the container; the db on the persistent volume survives).
	plane1 := bringUp()
	first := commitTable(t, c1GatewayClient(t, plane1), callCtx, "warehouse.sales.orders", "snap-durable", "K-dur")
	if first.GetAlreadyApplied() || first.GetSnapshotId() != "snap-durable" {
		plane1.Shutdown(ctx)
		t.Fatalf("first commit = {snap:%q applied:%v}, want {snap-durable, false}", first.GetSnapshotId(), first.GetAlreadyApplied())
	}
	plane1.Shutdown(ctx) // ← the backend crash: container torn down

	// Cycle 2: relaunch on the SAME durable db. The commit-key ledger survived, so a
	// replay with the same key is a no-op — proven across a real backend restart.
	plane2 := bringUp()
	t.Cleanup(func() { plane2.Shutdown(ctx) })
	client2 := c1GatewayClient(t, plane2)

	replay := commitTable(t, client2, callCtx, "warehouse.sales.orders", "snap-durable", "K-dur")
	if !replay.GetAlreadyApplied() || replay.GetSnapshotId() != "snap-durable" {
		t.Fatalf("replay after restart = {snap:%q applied:%v}, want {snap-durable, true} — the durable ledger did NOT survive the crash", replay.GetSnapshotId(), replay.GetAlreadyApplied())
	}
	// Drift after restart: same key, different payload → still the original.
	drift := commitTable(t, client2, callCtx, "warehouse.sales.orders", "snap-other", "K-dur")
	if !drift.GetAlreadyApplied() || drift.GetSnapshotId() != "snap-durable" {
		t.Errorf("drifted replay after restart = {snap:%q applied:%v}, want {snap-durable, true}", drift.GetSnapshotId(), drift.GetAlreadyApplied())
	}
	t.Logf("durable ledger survived a backend crash+relaunch: replay already_applied=%v snap=%q", replay.GetAlreadyApplied(), replay.GetSnapshotId())
}
