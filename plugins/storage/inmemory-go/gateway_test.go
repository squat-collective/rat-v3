// gateway_test.go — THROWAWAY STUB of the ADR-005 core capability-invoke gateway,
// pointed at the storage plugin. Axis-generic (same shape as the format/engine
// stubs); only capabilityRoutes() names StorageService.
//
// For storage the mediation path matters extra: the gateway re-stamps the tenant
// into the downstream rat-callmeta-bin envelope (ADR-007), and the storage server
// reads THAT to scope the vended credentials. So this stub exercises the full
// caller→core→provider tenant-propagation the C7 obligation depends on.
//
// (callMetaHeader / readCallMeta's RequestContext type are shared with server.go,
// which already defines callMetaHeader.)
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	storagev1 "github.com/squat-collective/rat-v3/gen/rat/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type passthroughCodec struct{}

func (passthroughCodec) Marshal(v any) ([]byte, error)      { return v.([]byte), nil }
func (passthroughCodec) Unmarshal(data []byte, v any) error { *(v.(*[]byte)) = data; return nil }
func (passthroughCodec) Name() string                       { return "proto" }

type stubGateway struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	provider     *grpc.ClientConn
	routes       map[string]string
	allowed      map[string]bool
	callerPlugin string

	mu    sync.Mutex
	audit []string
}

func capabilityRoutes() map[string]string {
	routes := map[string]string{}
	svc := storagev1.File_rat_storage_v1_storage_proto.Services().ByName("StorageService")
	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		m := methods.Get(i)
		capURI, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string)
		if capURI == "" {
			continue
		}
		routes[capURI] = fmt.Sprintf("/%s/%s", svc.FullName(), m.Name())
	}
	return routes
}

func newGateway(provider *grpc.ClientConn, callerPlugin string, allowed []string) *stubGateway {
	allow := map[string]bool{}
	for _, c := range allowed {
		allow[c] = true
	}
	return &stubGateway{provider: provider, routes: capabilityRoutes(), allowed: allow, callerPlugin: callerPlugin}
}

func readCallMeta(ctx context.Context) *commonv1.RequestContext {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil
	}
	vals := md.Get(callMetaHeader)
	if len(vals) == 0 {
		return nil
	}
	var rc commonv1.RequestContext
	if proto.Unmarshal([]byte(vals[0]), &rc) != nil {
		return nil
	}
	return &rc
}

func wellFormedTraceparent(tp string) bool {
	p := strings.Split(tp, "-")
	return len(p) == 4 && len(p[0]) == 2 && len(p[1]) == 32 && len(p[2]) == 16 && len(p[3]) == 2
}

func (g *stubGateway) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	cap := req.GetCapability()

	in := readCallMeta(ctx) // ADR-007: context is in metadata, not the payload.
	if !wellFormedTraceparent(in.GetTrace().GetTraceparent()) {
		return nil, status.Error(codes.InvalidArgument, "C1: missing or ill-formed traceparent")
	}
	if !g.allowed[cap] {
		return nil, status.Errorf(codes.PermissionDenied, "C5: caller %q not authorized for capability %q", g.callerPlugin, cap)
	}
	method, ok := g.routes[cap]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no provider method for capability %q", cap)
	}

	tenant := in.GetIdentity().GetTenant()
	downstream := &commonv1.RequestContext{
		Trace: in.GetTrace(),
		Identity: &commonv1.Identity{
			CallerPlugin: g.callerPlugin,
			Subject:      in.GetIdentity().GetSubject(),
			Tenant:       tenant, // re-stamped; the storage server reads THIS to scope creds
		},
		DeadlineUnixMs: in.GetDeadlineUnixMs(),
	}
	metaBytes, err := proto.Marshal(downstream)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal call-meta: %v", err)
	}
	out := metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(metaBytes))

	g.mu.Lock()
	g.audit = append(g.audit, fmt.Sprintf("cap=%s method=%s caller=%s tenant=%q", cap, method, g.callerPlugin, tenant))
	g.mu.Unlock()

	var result []byte
	if err := g.provider.Invoke(out, method, req.GetPayload(), &result, grpc.ForceCodec(passthroughCodec{})); err != nil {
		return nil, err
	}
	return &corev1.InvokeResponse{Result: result}, nil
}

func (g *stubGateway) auditLog() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.audit...)
}
