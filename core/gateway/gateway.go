// Package gateway is the spike core's capability-invoke gateway (ADR-014):
// the core/v1 CapabilityInvokeService (ADR-005/008) whose C5 authorization is
// DERIVED from the registry's manifest data — not a hardcoded allowlist.
//
// It is seeded from examples/bench/latency-go/gateway.go (the faithful non-test
// relay: read the rat-callmeta-bin envelope, validate traceparent, re-stamp
// identity, route by the (rat.common.v1.capability) annotation, relay opaque
// frames via a passthrough codec). The difference that matters — and the whole
// point of the C5 spike — is that openCall asks registry.Authorize and emits an
// audit record for every decision, allow or deny (C4).
package gateway

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/rat-dev/rat/core/registry"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const callMetaHeader = "rat-callmeta-bin"

// AuditRecord is emitted for EVERY capability decision (C4) — allow or deny.
type AuditRecord struct {
	Capability string
	Caller     string
	Provider   string
	Allowed    bool
	Reason     string
}

// Auditor sinks audit records. The spike ships an in-memory one; a real
// audit-log plugin is a later axis.
type Auditor interface{ Record(AuditRecord) }

type noopAuditor struct{}

func (noopAuditor) Record(AuditRecord) {}

// MemAuditor is a thread-safe in-memory Auditor (spike default + test sink).
type MemAuditor struct {
	mu      sync.Mutex
	records []AuditRecord
}

// Record appends r. Safe for concurrent use.
func (a *MemAuditor) Record(r AuditRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, r)
}

// Records returns a copy of the recorded decisions.
func (a *MemAuditor) Records() []AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditRecord(nil), a.records...)
}

// Gateway implements core/v1 CapabilityInvokeService. It resolves a capability to
// its provider via the registry, enforces C5 against declared manifests, audits
// the decision, then relays opaque frames to the provider's gRPC connection.
type Gateway struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	reg       *registry.Registry
	providers map[string]*grpc.ClientConn // plugin name -> live connection
	routes    map[string]string           // capability URI -> "/<svc.Full>/<Method>"
	auditor   Auditor
}

// New builds a gateway. The route table (capability -> method) is derived from the
// (rat.common.v1.capability) annotation on the supplied service descriptors — pass
// the axis file descriptors whose plugins are connected.
func New(reg *registry.Registry, providers map[string]*grpc.ClientConn, auditor Auditor, descriptors ...protoreflect.FileDescriptor) *Gateway {
	if auditor == nil {
		auditor = noopAuditor{}
	}
	return &Gateway{
		reg:       reg,
		providers: providers,
		routes:    buildRoutes(descriptors),
		auditor:   auditor,
	}
}

func buildRoutes(fds []protoreflect.FileDescriptor) map[string]string {
	routes := map[string]string{}
	for _, fd := range fds {
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

type passthroughCodec struct{}

func (passthroughCodec) Marshal(v any) ([]byte, error)      { return v.([]byte), nil }
func (passthroughCodec) Unmarshal(data []byte, v any) error { *(v.(*[]byte)) = data; return nil }
func (passthroughCodec) Name() string                       { return "proto" }

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

// openCall is the shared C1-check -> C5-authorize(+audit) -> route -> re-stamp
// path for both unary and streaming invocations. On a denied/failed decision it
// returns the appropriate gRPC status; on success it returns the downstream
// context, the resolved method path, and the provider connection to relay to.
func (g *Gateway) openCall(ctx context.Context, capURI string) (context.Context, string, *grpc.ClientConn, error) {
	in := readCallMeta(ctx)
	if !wellFormedTraceparent(in.GetTrace().GetTraceparent()) {
		return nil, "", nil, status.Error(codes.InvalidArgument, "C1: missing or ill-formed traceparent")
	}
	// caller_plugin is derived from the inbound envelope. NOTE: real channel
	// authentication (C2) is deferred (ADR-014) — for the spike the caller is taken
	// from the call-meta; the full core re-derives it from the authenticated channel.
	caller := in.GetIdentity().GetCallerPlugin()

	// C5 — the decision DERIVED from declared manifests; audited either way (C4).
	d := g.reg.Authorize(caller, capURI)
	g.auditor.Record(AuditRecord{Capability: capURI, Caller: caller, Provider: d.Provider, Allowed: d.Allowed, Reason: d.Reason})
	if !d.Allowed {
		return nil, "", nil, status.Error(codes.PermissionDenied, d.Reason)
	}

	method, ok := g.routes[capURI]
	if !ok {
		// A provider declares it, but no loaded descriptor maps it to a method — a
		// wiring gap in the core's setup, not a caller error.
		return nil, "", nil, status.Errorf(codes.Internal, "no route for capability %q (descriptor not loaded)", capURI)
	}
	conn := g.providers[d.Provider]
	if conn == nil {
		return nil, "", nil, status.Errorf(codes.Unavailable, "provider %q for %q is not connected", d.Provider, capURI)
	}

	// Re-stamp identity for the downstream hop; trace is propagated verbatim (ADR-007).
	downstream := &commonv1.RequestContext{
		Trace: in.GetTrace(),
		Identity: &commonv1.Identity{
			CallerPlugin: caller,
			Subject:      in.GetIdentity().GetSubject(),
			Tenant:       in.GetIdentity().GetTenant(),
		},
		DeadlineUnixMs: in.GetDeadlineUnixMs(),
	}
	b, err := proto.Marshal(downstream)
	if err != nil {
		return nil, "", nil, status.Errorf(codes.Internal, "marshal call-meta: %v", err)
	}
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b)), method, conn, nil
}

// Invoke is the unary capability call: authorize, then relay the opaque payload
// to the provider's method and return its opaque result.
func (g *Gateway) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	octx, method, conn, err := g.openCall(ctx, req.GetCapability())
	if err != nil {
		return nil, err
	}
	var result []byte
	if err := conn.Invoke(octx, method, req.GetPayload(), &result, grpc.ForceCodec(passthroughCodec{})); err != nil {
		return nil, err
	}
	return &corev1.InvokeResponse{Result: result}, nil
}

// InvokeServerStream is the server-streaming capability call: authorize at open
// (ADR-008 enforce-at-open), then relay opaque response frames.
func (g *Gateway) InvokeServerStream(req *corev1.InvokeServerStreamRequest, up grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) error {
	octx, method, conn, err := g.openCall(up.Context(), req.GetCapability())
	if err != nil {
		return err
	}
	ds, err := conn.NewStream(octx, &grpc.StreamDesc{ServerStreams: true}, method, grpc.ForceCodec(passthroughCodec{}))
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
