// gateway_test.go — THROWAWAY STUB of the ADR-005/008 core capability-invoke
// gateway, pointed at the runtime plugin. This is the ADR-008 implementation: the
// gateway now mediates a SERVER-STREAMING capability via InvokeServerStream.
//
// It is the same axis-generic stub as the unary axes, plus a streaming relay:
// enforce C5 + validate traceparent + stamp the downstream rat-callmeta-bin
// envelope (ADR-007) ONCE at stream-open, emit one C8 audit per stream, then open
// a downstream server-streaming call with the passthrough codec and relay each
// response frame's opaque bytes upstream — never deserializing an ExecuteResponse.
// Capability→method routing is read from the (rat.common.v1.capability) annotation.
package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	runtimev1 "github.com/squat-collective/rat-v3/gen/rat/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const callMetaHeader = "rat-callmeta-bin"

// passthroughCodec relays the gRPC message body as raw bytes; Name()=="proto"
// keeps the provider's proto codec on the wire. Works for streaming too (the codec
// is applied per frame).
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
	svc := runtimev1.File_rat_runtime_v1_runtime_proto.Services().ByName("RuntimeService")
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

// openStream does the shared enforce → route → re-stamp → audit, returning the
// downstream context (with the re-stamped envelope) + the resolved method.
func (g *stubGateway) openStream(ctx context.Context, cap string) (context.Context, string, error) {
	in := readCallMeta(ctx)
	if !wellFormedTraceparent(in.GetTrace().GetTraceparent()) {
		return nil, "", status.Error(codes.InvalidArgument, "C1: missing or ill-formed traceparent")
	}
	if !g.allowed[cap] {
		return nil, "", status.Errorf(codes.PermissionDenied, "C5: caller %q not authorized for capability %q", g.callerPlugin, cap)
	}
	method, ok := g.routes[cap]
	if !ok {
		return nil, "", status.Errorf(codes.NotFound, "no provider method for capability %q", cap)
	}
	tenant := in.GetIdentity().GetTenant()
	downstream := &commonv1.RequestContext{
		Trace:          in.GetTrace(),
		Identity:       &commonv1.Identity{CallerPlugin: g.callerPlugin, Subject: in.GetIdentity().GetSubject(), Tenant: tenant},
		DeadlineUnixMs: in.GetDeadlineUnixMs(),
	}
	metaBytes, err := proto.Marshal(downstream)
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "marshal call-meta: %v", err)
	}
	g.mu.Lock()
	g.audit = append(g.audit, fmt.Sprintf("cap=%s method=%s caller=%s tenant=%q", cap, method, g.callerPlugin, tenant))
	g.mu.Unlock()
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(metaBytes)), method, nil
}

// InvokeServerStream mediates a server-streaming capability (ADR-008): one
// request opens the downstream stream; each response frame is relayed verbatim.
func (g *stubGateway) InvokeServerStream(req *corev1.InvokeServerStreamRequest, up grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) error {
	octx, method, err := g.openStream(up.Context(), req.GetCapability())
	if err != nil {
		return err
	}
	ds, err := g.provider.NewStream(octx, &grpc.StreamDesc{ServerStreams: true}, method, grpc.ForceCodec(passthroughCodec{}))
	if err != nil {
		return err
	}
	if err := ds.SendMsg(req.GetPayload()); err != nil {
		return err
	}
	if err := ds.CloseSend(); err != nil {
		return err
	}
	for {
		var frame []byte
		switch err := ds.RecvMsg(&frame); err {
		case nil:
			if err := up.Send(&corev1.InvokeServerStreamResponse{Result: frame}); err != nil {
				return err
			}
		case io.EOF:
			return nil
		default:
			return err // provider's gRPC status (incl. INVALID_ARGUMENT) relayed verbatim
		}
	}
}

func (g *stubGateway) auditLog() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.audit...)
}
