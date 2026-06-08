// Package gateway is the spike core's capability-invoke gateway (ADR-014):
// the core/v1 CapabilityInvokeService (ADR-005/008) whose C5 authorization is
// DERIVED from the registry's manifest data — not a hardcoded allowlist.
//
// It is seeded from plugins/bench/latency-go/gateway.go (the faithful non-test
// relay: read the rat-callmeta-bin envelope, validate traceparent, re-stamp
// identity, route by the (rat.common.v1.capability) annotation, relay opaque
// frames via a passthrough codec). The difference that matters — and the whole
// point of the C5 spike — is that openCall asks registry.Authorize and emits an
// audit record for every decision, allow or deny (C4).
package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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

// AuditRecord is emitted for EVERY capability decision (C4) — allow or deny — and,
// for STREAMS, once more when the stream closes (the terminal record). The decision
// record carries Allowed/Reason; the terminal record carries Terminal/Outcome/Frames/
// Error. Both share Capability/Caller/Provider/Correlation so a stream's open and
// close link up. (The spike's simplified shape; the frozen wire type
// common/v1.AuditRecord adds the core signature + hash chain — GA.)
type AuditRecord struct {
	Capability  string
	Caller      string
	Provider    string
	Correlation string // correlation_id from the call envelope (links a stream's open + close)
	Allowed     bool   // the C5 decision (decision records)
	Reason      string // decision rationale / deny message (decision records)

	// C4 terminal stream-close record: ADR-008 enforces authz at OPEN, so a stream's
	// decision record is emitted there; this terminal record records how the stream
	// ENDED. Set only on the record emitted when a server-stream closes.
	Terminal bool   // true == the stream's terminal close record
	Outcome  string // "success" | "error" | "canceled" | "timeout" (→ AUDIT_OUTCOME_* at GA)
	Frames   int    // response frames relayed before close
	Error    string // transport/provider error when Outcome == "error"
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

// defaultStreamIdleTimeout backstops a server-stream that goes silent (C3): if no
// frame arrives within this window the gateway cuts the stream, so a hung provider
// can't pin it forever even when the call carries NO deadline (the deadline bound
// covers the deadline-set case — reviews/10). It is generous because a legitimately
// quiet long-lived stream (e.g. watch) is normal; such providers should emit periodic
// keepalive frames, or a deployment tunes Gateway.StreamIdleTimeout.
const defaultStreamIdleTimeout = 5 * time.Minute

// Gateway implements core/v1 CapabilityInvokeService. It resolves a capability to
// its provider via the registry, enforces C5 against declared manifests, audits
// the decision, then relays opaque frames to the provider's gRPC connection.
type Gateway struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	reg       *registry.Registry
	provMu    sync.RWMutex                // guards providers for runtime re-bind (ADR-022)
	providers map[string]*grpc.ClientConn // plugin name -> live connection
	routes    map[string]string           // capability URI -> "/<svc.Full>/<Method>"
	auditor   Auditor

	// StreamIdleTimeout is the C3 idle backstop for server-streams (see
	// defaultStreamIdleTimeout). New sets it to the default; override it before the
	// gateway starts serving. <= 0 falls back to the default.
	StreamIdleTimeout time.Duration
}

// New builds a gateway. The route table (capability -> method) is derived from the
// (rat.common.v1.capability) annotation on the supplied service descriptors — pass
// the axis file descriptors whose plugins are connected.
func New(reg *registry.Registry, providers map[string]*grpc.ClientConn, auditor Auditor, descriptors ...protoreflect.FileDescriptor) *Gateway {
	if auditor == nil {
		auditor = noopAuditor{}
	}
	// Own the map (copy it) so SetProvider can mutate it under the lock without racing a
	// caller that still holds the passed-in map.
	owned := make(map[string]*grpc.ClientConn, len(providers))
	for k, v := range providers {
		owned[k] = v
	}
	return &Gateway{
		reg:               reg,
		providers:         owned,
		routes:            buildRoutes(descriptors),
		auditor:           auditor,
		StreamIdleTimeout: defaultStreamIdleTimeout,
	}
}

// SetProvider binds (or re-binds) the live connection for a provider by name. This is the
// runtime re-wire the reconciler needs when a relaunched plugin comes up on a NEW endpoint,
// and the hook a runtime-registration path uses to add a provider (ADR-022). It is
// concurrency-safe against in-flight Invoke/relay reads. It returns the previous connection
// (nil if none) so the caller can Close() it after draining.
func (g *Gateway) SetProvider(name string, conn *grpc.ClientConn) *grpc.ClientConn {
	g.provMu.Lock()
	defer g.provMu.Unlock()
	prev := g.providers[name]
	g.providers[name] = conn
	return prev
}

// RemoveProvider drops a provider (e.g. one the reconciler marked Degraded). It returns the
// removed connection (nil if none) so the caller can Close() it.
func (g *Gateway) RemoveProvider(name string) *grpc.ClientConn {
	g.provMu.Lock()
	defer g.provMu.Unlock()
	prev := g.providers[name]
	delete(g.providers, name)
	return prev
}

// provider returns the live connection for name (nil if unbound), read-locked so a
// concurrent SetProvider/RemoveProvider can't tear the map mid-read.
func (g *Gateway) provider(name string) *grpc.ClientConn {
	g.provMu.RLock()
	defer g.provMu.RUnlock()
	return g.providers[name]
}

// idleTimeout is the effective server-stream idle backstop (guards a zero value).
func (g *Gateway) idleTimeout() time.Duration {
	if g.StreamIdleTimeout > 0 {
		return g.StreamIdleTimeout
	}
	return defaultStreamIdleTimeout
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

// openedCall is the resolved, authorized call: the downstream context (bounded by
// min(channel, deadline_unix_ms) — C3), the routed method + provider connection, a
// cancel the caller MUST defer, and the identity bits the terminal audit record needs.
type openedCall struct {
	ctx         context.Context
	method      string
	conn        *grpc.ClientConn
	cancel      context.CancelFunc
	caller      string
	provider    string
	correlation string
}

// openCall is the shared C1-check -> C5-authorize(+audit) -> route -> re-stamp ->
// deadline-bound path for both unary and streaming invocations. It records the ONE
// decision audit record (allow or deny — C4). On a denied/failed decision it returns
// (nil, status); on success it returns the openedCall (caller defers oc.cancel()).
func (g *Gateway) openCall(ctx context.Context, capURI string) (*openedCall, error) {
	in := readCallMeta(ctx)
	if !wellFormedTraceparent(in.GetTrace().GetTraceparent()) {
		return nil, status.Error(codes.InvalidArgument, "C1: missing or ill-formed traceparent")
	}
	// caller_plugin is derived from the inbound envelope. NOTE: real channel
	// authentication (C2) is deferred (ADR-014) — for the spike the caller is taken
	// from the call-meta; the full core re-derives it from the authenticated channel.
	caller := in.GetIdentity().GetCallerPlugin()
	correlation := in.GetTrace().GetCorrelationId()

	// C5 — the decision DERIVED from declared manifests; audited either way (C4).
	d := g.reg.Authorize(caller, capURI)
	g.auditor.Record(AuditRecord{Capability: capURI, Caller: caller, Provider: d.Provider, Correlation: correlation, Allowed: d.Allowed, Reason: d.Reason})
	if !d.Allowed {
		return nil, status.Error(codes.PermissionDenied, d.Reason)
	}

	method, ok := g.routes[capURI]
	if !ok {
		// A provider declares it, but no loaded descriptor maps it to a method — a
		// wiring gap in the core's setup, not a caller error.
		return nil, status.Errorf(codes.Internal, "no route for capability %q (descriptor not loaded)", capURI)
	}
	conn := g.provider(d.Provider)
	if conn == nil {
		return nil, status.Errorf(codes.Unavailable, "provider %q for %q is not connected", d.Provider, capURI)
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
		return nil, status.Errorf(codes.Internal, "marshal call-meta: %v", err)
	}
	octx := metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b))

	// C3 — bound the provider call by the soft deadline when it is sooner than the
	// channel deadline, so a hung provider can't pin the gateway. deadline_unix_ms
	// == 0 means "no soft deadline"; the channel deadline (if any) still applies and
	// propagates to the downstream call via octx.
	cancel := context.CancelFunc(func() {})
	if soft := in.GetDeadlineUnixMs(); soft > 0 {
		softTime := time.UnixMilli(soft)
		if dl, hasDL := octx.Deadline(); !hasDL || softTime.Before(dl) {
			octx, cancel = context.WithDeadline(octx, softTime)
		}
	}
	return &openedCall{ctx: octx, method: method, conn: conn, cancel: cancel, caller: caller, provider: d.Provider, correlation: correlation}, nil
}

// Invoke is the unary capability call: authorize, then relay the opaque payload
// to the provider's method and return its opaque result.
func (g *Gateway) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	oc, err := g.openCall(ctx, req.GetCapability())
	if err != nil {
		return nil, err
	}
	defer oc.cancel()
	var result []byte
	if err := oc.conn.Invoke(oc.ctx, oc.method, req.GetPayload(), &result, grpc.ForceCodec(passthroughCodec{})); err != nil {
		return nil, err
	}
	return &corev1.InvokeResponse{Result: result}, nil
}

// InvokeServerStream is the server-streaming capability call: authorize at open
// (ADR-008 enforce-at-open), relay opaque response frames, then emit the C4 terminal
// stream-close audit record — so a stream's audit trail is open-decision + close-
// outcome. A stream DENIED at open never opens, so it gets only the deny record.
func (g *Gateway) InvokeServerStream(req *corev1.InvokeServerStreamRequest, up grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) error {
	oc, err := g.openCall(up.Context(), req.GetCapability())
	if err != nil {
		return err // denied/failed at open; openCall already recorded the decision
	}
	defer oc.cancel()

	frames, relayErr := g.relayServerStream(oc, req.GetPayload(), up)
	outcome, errMsg := streamOutcome(relayErr)
	g.auditor.Record(AuditRecord{
		Capability: req.GetCapability(), Caller: oc.caller, Provider: oc.provider, Correlation: oc.correlation,
		Allowed: true, Terminal: true, Outcome: outcome, Frames: frames, Error: errMsg,
	})
	return relayErr
}

// relayServerStream opens the downstream server-stream and relays opaque frames to
// the upstream, returning the number relayed and the terminating error (nil on a
// clean EOF). The count + error feed the terminal audit record.
//
// C3 idle backstop: a streamCtx (child of oc.ctx, so the deadline bound still
// applies) is cut by a time.AfterFunc watchdog if no frame arrives within the idle
// window — reset on each frame. A silent provider therefore can't pin the stream even
// with no deadline set. On a RecvMsg failure the cause is attributed: the parent
// deadline/cancel, our idle watchdog, or a genuine provider/transport error.
func (g *Gateway) relayServerStream(oc *openedCall, payload []byte, up grpc.ServerStreamingServer[corev1.InvokeServerStreamResponse]) (int, error) {
	idle := g.idleTimeout()
	streamCtx, cancel := context.WithCancel(oc.ctx)
	defer cancel()
	ds, err := oc.conn.NewStream(streamCtx, &grpc.StreamDesc{ServerStreams: true}, oc.method, grpc.ForceCodec(passthroughCodec{}))
	if err != nil {
		return 0, err
	}
	if err := ds.SendMsg(payload); err != nil {
		return 0, err
	}
	if err := ds.CloseSend(); err != nil {
		return 0, err
	}
	watchdog := time.AfterFunc(idle, cancel) // fires → cancels streamCtx → RecvMsg returns
	defer watchdog.Stop()
	frames := 0
	for {
		var frame []byte
		err := ds.RecvMsg(&frame)
		if err == nil {
			watchdog.Reset(idle) // a frame arrived; restart the idle window
			if err := up.Send(&corev1.InvokeServerStreamResponse{Result: frame}); err != nil {
				return frames, err
			}
			frames++
			continue
		}
		if err == io.EOF {
			return frames, nil
		}
		switch {
		case oc.ctx.Err() != nil: // parent deadline (C3 bound) or upstream cancel
			return frames, status.FromContextError(oc.ctx.Err()).Err()
		case streamCtx.Err() != nil: // our idle watchdog cut a silent provider
			return frames, status.Errorf(codes.DeadlineExceeded, "stream idle timeout: no frame within %s", idle)
		default:
			return frames, err // a genuine provider/transport error
		}
	}
}

// streamOutcome classifies a stream's terminating error into the terminal record's
// outcome. nil == clean EOF (success); cancellation == canceled; a deadline/idle cut
// == timeout; anything else == error. (At GA these collapse to AUDIT_OUTCOME_SUCCESS
// vs _ERROR; the spike keeps the finer label so an idle-cut is legible in the trail.)
func streamOutcome(err error) (outcome, errMsg string) {
	switch {
	case err == nil:
		return "success", ""
	case errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled:
		return "canceled", err.Error()
	case errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded:
		return "timeout", err.Error()
	default:
		return "error", err.Error()
	}
}
