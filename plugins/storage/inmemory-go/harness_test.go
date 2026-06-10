// harness_test.go — conformance/golden-data harness for the storage/v1 axis.
//
// Loads contracts/conformance/storage-v1.json (the SAME file the Python reference
// loads) and drives StorageService.VendCredentials through the stub core-mediated
// gateway. The harness sets the caller's tenant in the rat-callmeta-bin envelope;
// the gateway re-stamps it downstream; the storage server reads it to scope the
// vended credentials. The harness then decodes the (conformance) scope receipt and
// asserts the C7 obligation: vended scope.tenant == the caller's tenant, plus the
// requested prefix + mode, plus a short TTL.
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	storagev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// ---- golden-vector model (mirrors contracts/conformance/storage-v1.json) ----

type scope struct {
	Tenant string `json:"tenant"`
	Prefix string `json:"prefix"`
	Mode   string `json:"mode"`
}

type expectation struct {
	CredentialsPresent bool   `json:"credentials_present"`
	Scope              *scope `json:"scope"`
	Code               string `json:"code"`
}

type vstep struct {
	Step   string      `json:"step"`
	Op     string      `json:"op"`
	Prefix string      `json:"prefix"`
	Mode   string      `json:"mode"`
	Expect expectation `json:"expect"`
}

type vectors struct {
	Axis            string  `json:"axis"`
	Tenant          string  `json:"tenant"`
	CredentialsTTLs int64   `json:"credentials_ttl_seconds"`
	Lifecycle       []vstep `json:"lifecycle"`
	Errors          []vstep `json:"errors"`
}

const vectorPath = "../../../contracts/conformance/storage-v1.json"

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatalf("read golden vectors %s: %v", vectorPath, err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if v.Axis != "storage/v1" {
		t.Fatalf("vectors axis = %q, want storage/v1", v.Axis)
	}
	return v
}

func modeEnum(s string) storagev1.AccessMode {
	switch s {
	case "READ":
		return storagev1.AccessMode_ACCESS_MODE_READ
	case "WRITE":
		return storagev1.AccessMode_ACCESS_MODE_WRITE
	case "READ_WRITE":
		return storagev1.AccessMode_ACCESS_MODE_READ_WRITE
	default:
		return storagev1.AccessMode_ACCESS_MODE_UNSPECIFIED
	}
}

// ---- harness wiring: plugin behind the stub core gateway ----

type rig struct {
	gw     corev1.CapabilityInvokeServiceClient
	core   *stubGateway
	tenant string
}

func bufDial(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRig(t *testing.T, tenant string) *rig {
	t.Helper()

	plis := bufconn.Listen(1 << 20)
	psrv := grpc.NewServer()
	storagev1.RegisterStorageServiceServer(psrv, newServer())
	go func() { _ = psrv.Serve(plis) }()
	t.Cleanup(psrv.Stop)
	providerConn := bufDial(t, plis)

	core := newGateway(providerConn, "rat-strategy-test", []string{"rat://storage/v1/vend-credentials"})

	glis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(gsrv, core)
	go func() { _ = gsrv.Serve(glis) }()
	t.Cleanup(gsrv.Stop)

	return &rig{gw: corev1.NewCapabilityInvokeServiceClient(bufDial(t, glis)), core: core, tenant: tenant}
}

func tctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// withCallMeta attaches the rat-callmeta-bin envelope (ADR-007) carrying the
// caller's tenant — which storage reads to scope the creds — and a well-formed
// traceparent (or the gateway rejects the call).
func (r *rig) withCallMeta(ctx context.Context) context.Context {
	rc := &commonv1.RequestContext{
		Trace: &commonv1.TraceContext{
			Traceparent:   "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			CorrelationId: "corr-golden",
		},
		Identity: &commonv1.Identity{Tenant: r.tenant},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b))
}

func (r *rig) vend(ctx context.Context, prefix, mode string) (*storagev1.VendCredentialsResponse, error) {
	payload, err := proto.Marshal(&storagev1.VendCredentialsRequest{Prefix: prefix, Mode: modeEnum(mode)})
	if err != nil {
		return nil, err
	}
	out, err := r.gw.Invoke(r.withCallMeta(ctx), &corev1.InvokeRequest{
		Capability: "rat://storage/v1/vend-credentials", Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	var resp storagev1.VendCredentialsResponse
	if err := proto.Unmarshal(out.GetResult(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ---- the tests ----

func TestStorageConformance_GoldenVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t, v.Tenant)
	ctx := tctx(t)
	ttlMs := v.CredentialsTTLs * 1000
	const slackMs = 2000

	for _, s := range v.Lifecycle {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			before := nowMs()
			resp, err := r.vend(ctx, s.Prefix, s.Mode)
			after := nowMs()
			if err != nil {
				t.Fatalf("vend: %v", err)
			}
			if s.Expect.CredentialsPresent && len(resp.GetCredentials()) == 0 {
				t.Fatalf("credentials empty, want present")
			}
			// Decode the conformance scope receipt and assert the C7 binding.
			var got scopeReceipt
			if err := json.Unmarshal(resp.GetCredentials(), &got); err != nil {
				t.Fatalf("decode scope receipt: %v", err)
			}
			if e := s.Expect.Scope; e != nil {
				if got.Tenant != e.Tenant {
					t.Fatalf("scope.tenant = %q, want %q", got.Tenant, e.Tenant)
				}
				if got.Prefix != e.Prefix {
					t.Fatalf("scope.prefix = %q, want %q", got.Prefix, e.Prefix)
				}
				if got.Mode != e.Mode {
					t.Fatalf("scope.mode = %q, want %q", got.Mode, e.Mode)
				}
			}
			// Short-TTL: expiry ~ now + TTL (bounded both ways).
			exp := resp.GetExpiresUnixMs()
			if exp < before+ttlMs-slackMs || exp > after+ttlMs+slackMs {
				t.Fatalf("expires_unix_ms = %d, want within [%d, %d]", exp, before+ttlMs-slackMs, after+ttlMs+slackMs)
			}
			if exp != got.ExpiresUnixMs {
				t.Fatalf("response expires %d != receipt expires %d", exp, got.ExpiresUnixMs)
			}
		})
	}

	if got, want := len(r.core.auditLog()), len(v.Lifecycle); got != want {
		t.Fatalf("audit log = %d entries, want one per mediated call (%d)", got, want)
	}
}

func TestStorageConformance_ErrorVectors(t *testing.T) {
	v := loadVectors(t)
	r := newRig(t, v.Tenant)
	ctx := tctx(t)

	for _, s := range v.Errors {
		s := s
		t.Run(s.Step, func(t *testing.T) {
			_, err := r.vend(ctx, s.Prefix, s.Mode)
			if err == nil {
				t.Fatalf("%s: want error %s, got nil", s.Step, s.Expect.Code)
			}
			if got := status.Code(err); got != wantCode(t, s.Expect.Code) {
				t.Fatalf("%s: status = %s, want %s", s.Step, got, s.Expect.Code)
			}
		})
	}
}

// TestStorage_TenantComesFromMetadataNotRequest is the C7 structural check: the
// vended tenant tracks the rat-callmeta-bin envelope, and there is no request
// field that could override it. Vending under a different caller tenant yields
// creds scoped to THAT tenant — proving the binding is to metadata, not the body.
func TestStorage_TenantComesFromMetadataNotRequest(t *testing.T) {
	r := newRig(t, "globex") // different tenant than the vectors' "acme"
	ctx := tctx(t)
	resp, err := r.vend(ctx, "s3://bucket/x", "READ")
	if err != nil {
		t.Fatalf("vend: %v", err)
	}
	var got scopeReceipt
	if err := json.Unmarshal(resp.GetCredentials(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Tenant != "globex" {
		t.Fatalf("scope.tenant = %q, want globex (the metadata tenant)", got.Tenant)
	}
}

func wantCode(t *testing.T, name string) codes.Code {
	t.Helper()
	switch name {
	case "INVALID_ARGUMENT":
		return codes.InvalidArgument
	case "PERMISSION_DENIED":
		return codes.PermissionDenied
	case "NOT_FOUND":
		return codes.NotFound
	default:
		t.Fatalf("unmapped expected code %q", name)
		return codes.Unknown
	}
}
