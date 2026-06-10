// gateway.go — the ADR-005/008 core-mediated invoke gateway, as a faithful
// (non-test) relay so the benchmark measures realistic mediation cost: read the
// rat-callmeta-bin envelope, validate traceparent, re-stamp identity, route by the
// (rat.common.v1.capability) annotation, and relay opaque frames via the passthrough
// codec — for both unary (Invoke) and server-streaming (InvokeServerStream).
package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	runtimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/runtime/v1"
	statev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const callMetaHeader = "rat-callmeta-bin"

type passthroughCodec struct{}

func (passthroughCodec) Marshal(v any) ([]byte, error)      { return v.([]byte), nil }
func (passthroughCodec) Unmarshal(data []byte, v any) error { *(v.(*[]byte)) = data; return nil }
func (passthroughCodec) Name() string                       { return "proto" }

type gateway struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	provider     *grpc.ClientConn
	routes       map[string]string
	callerPlugin string
}

func capabilityRoutes() map[string]string {
	routes := map[string]string{}
	for _, fd := range []protoreflect.FileDescriptor{
		statev1.File_rat_state_v1_state_proto,
		runtimev1.File_rat_runtime_v1_runtime_proto,
	} {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			ms := svc.Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				if capURI, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string); capURI != "" {
					routes[capURI] = fmt.Sprintf("/%s/%s", svc.FullName(), m.Name())
				}
			}
		}
	}
	return routes
}

func newGateway(provider *grpc.ClientConn) *gateway {
	return &gateway{provider: provider, routes: capabilityRoutes(), callerPlugin: "rat-bench"}
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

func (g *gateway) openCall(ctx context.Context, capURI string) (context.Context, string, error) {
	in := readCallMeta(ctx)
	if !wellFormedTraceparent(in.GetTrace().GetTraceparent()) {
		return nil, "", status.Error(codes.InvalidArgument, "C1: missing or ill-formed traceparent")
	}
	method, ok := g.routes[capURI]
	if !ok {
		return nil, "", status.Errorf(codes.NotFound, "no provider for capability %q", capURI)
	}
	downstream := &commonv1.RequestContext{
		Trace: in.GetTrace(),
		Identity: &commonv1.Identity{
			CallerPlugin: g.callerPlugin,
			Subject:      in.GetIdentity().GetSubject(),
			Tenant:       in.GetIdentity().GetTenant(),
		},
		DeadlineUnixMs: in.GetDeadlineUnixMs(),
	}
	b, err := proto.Marshal(downstream)
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "marshal call-meta: %v", err)
	}
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b)), method, nil
}

func (g *gateway) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	octx, method, err := g.openCall(ctx, req.GetCapability())
	if err != nil {
		return nil, err
	}
	var result []byte
	if err := g.provider.Invoke(octx, method, req.GetPayload(), &result, grpc.ForceCodec(passthroughCodec{})); err != nil {
		return nil, err
	}
	return &corev1.InvokeResponse{Result: result}, nil
}

func (g *gateway) InvokeServerStream(req *corev1.InvokeServerStreamRequest, up grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) error {
	octx, method, err := g.openCall(up.Context(), req.GetCapability())
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
			return err
		}
	}
}
