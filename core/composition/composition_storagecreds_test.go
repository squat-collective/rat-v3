package composition

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/le-squat/rat/core/deploymentruntime"
	"github.com/le-squat/rat/core/gateway"
	"github.com/le-squat/rat/core/manifest"
	"github.com/le-squat/rat/core/supervisor"
	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
	corev1 "github.com/le-squat/rat/gen/rat/core/v1"
	deploymentruntimev1 "github.com/le-squat/rat/gen/rat/deploymentruntime/v1"
	storagev1 "github.com/le-squat/rat/gen/rat/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// D3 — storage-cred isolation. The real `localfs-go` storage ref is launched (isolated)
// behind the gateway; credentials it vends are scoped to (tenant, prefix, mode, TTL),
// the tenant comes ONLY from the gateway-re-stamped metadata envelope (not a request
// field), per-tenant roots are isolated, and a prefix that climbs out of the tenant
// root is refused. It also shows the defense-in-depth layering: C5 authorizes the
// vend-credentials CAPABILITY, then the storage plugin enforces tenancy CONTAINMENT —
// so a containment refusal is the provider's (C5-allowed in the audit), distinct from
// a C5 denial.
//
// NOTE (C2 deferred): the spike trusts the tenant claimed in the inbound envelope; the
// full core re-derives it from the authenticated channel. The scoping mechanism proven
// here is unchanged — only the source of the (trusted) tenant tightens with C2.

// callerCtxTenant is callerCtx with an explicit tenant (D3 varies it per caller).
func callerCtxTenant(caller, tenant string) context.Context {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01", CorrelationId: "corr-d3"},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: tenant},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(context.Background(), callMetaHeader, string(b))
}

// d3ScopeReceipt mirrors the localfs-go conformance credential blob.
type d3ScopeReceipt struct {
	Tenant        string `json:"tenant"`
	Prefix        string `json:"prefix"`
	Mode          string `json:"mode"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
	ResolvedPath  string `json:"resolved_path"`
}

// TestD3StorageCredsScopedAndIsolated brings up the real local-fs storage ref behind
// the gateway and proves cred scoping + tenant isolation + path containment, all
// vector-tested (not honor-system).
func TestD3StorageCredsScopedAndIsolated(t *testing.T) {
	bin := buildRef(t, "storage/localfs-go")
	root := t.TempDir() // the storage backend's filesystem root (per-tenant subdirs land here)

	rt := deploymentruntime.NewLocalProcess()
	iso := &deploymentruntimev1.IsolationProfile{RunAsNonRoot: true, DropAllCapabilities: true, NoNewPrivileges: true}

	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-d3-caller"},
		Requires: caps("rat://storage/v1/vend-credentials")}
	noStorage := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-d3-nostorage"}} // declares nothing → C5 deny
	storageM := &manifest.Manifest{Kind: "storage", Metadata: manifest.Metadata{Name: "rat-storage-localfs-go"},
		Provides: caps("rat://storage/v1/vend-credentials")}

	audit := &gateway.MemAuditor{}
	ctx := context.Background()
	plane, err := supervisor.BringUp(ctx, rt, []supervisor.PluginSpec{
		{Manifest: caller},
		{Manifest: noStorage},
		{Manifest: storageM, Launch: &deploymentruntimev1.LaunchSpec{Image: bin, Isolation: iso, Env: map[string]string{"RAT_STORAGE_ROOT": root}}},
	}, audit, 10*time.Second, storagev1.File_rat_storage_v1_storage_proto)
	if err != nil {
		t.Fatalf("BringUp: %v", err)
	}
	t.Cleanup(func() { plane.Shutdown(ctx) })

	client := corev1.NewCapabilityInvokeServiceClient(bufServer(t, func(s *grpc.Server) { corev1.RegisterCapabilityInvokeServiceServer(s, plane.Gateway) }))

	vend := func(tenant, prefix string, mode storagev1.AccessMode) (d3ScopeReceipt, error) {
		var resp storagev1.VendCredentialsResponse
		if err := invoke(callerCtxTenant("rat-d3-caller", tenant), client, "rat://storage/v1/vend-credentials",
			&storagev1.VendCredentialsRequest{Prefix: prefix, Mode: mode}, &resp); err != nil {
			return d3ScopeReceipt{}, err
		}
		var r d3ScopeReceipt
		if e := json.Unmarshal(resp.GetCredentials(), &r); e != nil {
			t.Fatalf("scope receipt not JSON: %v (%q)", e, resp.GetCredentials())
		}
		if resp.GetExpiresUnixMs() <= 0 {
			t.Errorf("vended creds carry no TTL (expires_unix_ms=%d)", resp.GetExpiresUnixMs())
		}
		return r, nil
	}

	// 1. Scoped to the caller's tenant + the requested prefix + mode.
	acme, err := vend("acme", "warehouse/orders", storagev1.AccessMode_ACCESS_MODE_READ)
	if err != nil {
		t.Fatalf("vend (acme, declared): %v", err)
	}
	if acme.Tenant != "acme" || acme.Prefix != "warehouse/orders" || acme.Mode != "READ" {
		t.Errorf("acme receipt = %+v, want bound to {acme, warehouse/orders, READ}", acme)
	}

	// 2. Tenant isolation: globex vends the SAME logical prefix but gets a DIFFERENT,
	// per-tenant root — the tenant is the gateway-supplied one, not anything the caller
	// put in the request.
	globex, err := vend("globex", "warehouse/orders", storagev1.AccessMode_ACCESS_MODE_READ)
	if err != nil {
		t.Fatalf("vend (globex, declared): %v", err)
	}
	if globex.Tenant != "globex" {
		t.Errorf("globex receipt tenant = %q, want globex", globex.Tenant)
	}
	if acme.ResolvedPath == globex.ResolvedPath {
		t.Errorf("acme and globex resolved to the SAME path %q — tenant isolation broken", acme.ResolvedPath)
	}
	if !strings.Contains(acme.ResolvedPath, "/acme/") || !strings.Contains(globex.ResolvedPath, "/globex/") {
		t.Errorf("resolved paths are not under per-tenant roots: acme=%q globex=%q", acme.ResolvedPath, globex.ResolvedPath)
	}
	t.Logf("isolated: acme=%q globex=%q", acme.ResolvedPath, globex.ResolvedPath)

	// 3. Path containment: acme cannot climb out of its root into globex's → the storage
	// plugin (not C5) refuses with PERMISSION_DENIED.
	if _, err := vend("acme", "../globex/secrets", storagev1.AccessMode_ACCESS_MODE_READ); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("escaping prefix = %v, want PermissionDenied (containment)", status.Code(err))
	}

	// 4. Validation: an empty prefix is INVALID_ARGUMENT.
	if _, err := vend("acme", "", storagev1.AccessMode_ACCESS_MODE_READ); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty prefix = %v, want InvalidArgument", status.Code(err))
	}

	// 5. C5: a caller that never declared vend-credentials is denied — no creds vended.
	var resp storagev1.VendCredentialsResponse
	if err := invoke(callerCtxTenant("rat-d3-nostorage", "acme"), client, "rat://storage/v1/vend-credentials",
		&storagev1.VendCredentialsRequest{Prefix: "warehouse/orders", Mode: storagev1.AccessMode_ACCESS_MODE_READ}, &resp); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("vend by an undeclared caller = %v, want PermissionDenied (C5)", status.Code(err))
	}

	// Defense-in-depth in the audit: every vend the caller DECLARED is C5-allowed — even
	// the ones the storage plugin then refused for containment/validation (those denials
	// are the provider's). Only the undeclared caller is a C5 denial.
	denies := 0
	for _, r := range audit.Records() {
		if !r.Allowed {
			denies++
		}
	}
	if denies != 1 {
		t.Errorf("audit has %d C5 denials, want exactly 1 (only the undeclared caller; containment/validation refusals are provider-side, C5-allowed)", denies)
	}
}
