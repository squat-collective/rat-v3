// gateway_test.go — THROWAWAY STUB of the ADR-005 core capability-invoke gateway.
//
// This is NOT the real RAT core. It exists only so the 0d format conformance
// harness drives the plugin through the CORE-MEDIATED control path
// (core/v1 CapabilityInvokeService.Invoke) instead of dialing the plugin's
// FormatService directly. That validates the mediation SEAMS ADR-005 puts between
// a calling plugin and a providing plugin — the part of the contract a plain
// plugin-to-plugin test can't exercise:
//
//   - capability⇄method routing read from the (rat.common.v1.capability) method
//     annotation (freeze-blocker #5) — NOT a hand-maintained map;
//   - C5 capability enforcement (caller must be allowed the capability);
//   - C1/C2 identity re-derivation on the downstream hop (here: stamped into
//     outbound gRPC metadata — see the FINDING note below);
//   - C8 audit emission for every mediated call;
//   - the "generic proxy forwards `payload` WITHOUT interpreting it" guarantee:
//     this gateway relays the serialized axis request/response as raw bytes via a
//     passthrough codec and never deserializes a FormatService message.
//
// Faithfulness matters here: a stub that special-cased each format method would
// hide exactly the coupling ADR-005 promises the core does NOT have. This one is
// axis-agnostic — point it at any service whose methods carry the capability
// annotation and it routes them.
package main

import (
	"context"
	"fmt"
	"sync"

	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// passthroughCodec relays the gRPC message body as raw bytes. Its Name() is
// "proto" ON PURPOSE: that keeps the wire content-subtype compatible with the
// provider's standard proto codec, so the provider deserializes the relayed bytes
// with ITS codec while the gateway never (de)serializes them. This is the
// transparent-proxy trick (cf. mwitkow/grpc-proxy) and is what makes the gateway a
// genuine generic byte relay rather than a typed shim.
type passthroughCodec struct{}

func (passthroughCodec) Marshal(v any) ([]byte, error)      { return v.([]byte), nil }
func (passthroughCodec) Unmarshal(data []byte, v any) error { *(v.(*[]byte)) = data; return nil }
func (passthroughCodec) Name() string                       { return "proto" }

// stubGateway implements core/v1 CapabilityInvokeService against a single
// downstream provider connection. Multi-provider registry resolution is the real
// core's job; the stub points at one provider (the format plugin under test).
type stubGateway struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	provider     *grpc.ClientConn // the providing plugin (dialed once)
	routes       map[string]string // capability URI -> "/rat.format.v1.FormatService/Merge"
	allowed      map[string]bool   // C5: capabilities the caller's manifest permits
	callerPlugin string            // identity.caller_plugin the core re-derives for this hop

	mu    sync.Mutex
	audit []string // C8: stub audit sink (the real core emits an AuditRecord)
}

// capabilityRoutes walks a service descriptor and builds the capability→method
// table straight from the (rat.common.v1.capability) annotation on each method.
// This is the freeze-blocker #5 machinery proving the annotation is enough to
// route by — no per-axis knowledge baked into the gateway.
func capabilityRoutes() map[string]string {
	routes := map[string]string{}
	svc := formatv1.File_rat_format_v1_format_proto.Services().ByName("FormatService")
	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		m := methods.Get(i)
		capURI, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string)
		if capURI == "" {
			continue // a method without a capability annotation is not invocable by URI
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
	return &stubGateway{
		provider:     provider,
		routes:       capabilityRoutes(),
		allowed:      allow,
		callerPlugin: callerPlugin,
	}
}

// Invoke mediates one capability call: enforce → route → re-stamp identity →
// audit → relay bytes. It never looks inside `payload`.
func (g *stubGateway) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	cap := req.GetCapability()

	// C5 — capability enforcement. The caller's manifest must permit this
	// capability; the provider must serve it (here: it's in the route table).
	if !g.allowed[cap] {
		return nil, status.Errorf(codes.PermissionDenied, "C5: caller %q not authorized for capability %q", g.callerPlugin, cap)
	}
	method, ok := g.routes[cap]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no provider method for capability %q", cap)
	}

	// C1/C2 — re-derive identity.caller_plugin for the DOWNSTREAM hop and
	// propagate trace. The keystone (context.proto) says caller_plugin is
	// re-derived per hop and never trusted from the wire; the core stamps it.
	//
	// FINDING (0d, surfaced by building this stub): the axis request messages
	// carry RequestContext *inside* the payload, but a generic proxy that does
	// not deserialize the payload cannot rewrite that embedded context. So the
	// gateway stamps the downstream identity into gRPC METADATA instead. Whether
	// frozen identity rides in payload.context or in channel metadata is a real
	// open question this reference surfaced — see roadmap. For the stub the
	// provider ignores both (it does not trust identity), so behavior is correct
	// either way; the seam is what we're exercising.
	tenant := req.GetContext().GetIdentity().GetTenant()
	out := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-rat-caller-plugin", g.callerPlugin,
		"x-rat-tenant", tenant,
	))

	// C8 — mandatory audit emission, even with no audit-log plugin installed.
	g.mu.Lock()
	g.audit = append(g.audit, fmt.Sprintf("cap=%s method=%s caller=%s tenant=%q", cap, method, g.callerPlugin, tenant))
	g.mu.Unlock()

	// Generic byte relay. ForceCodec(passthroughCodec) makes the client side pass
	// req.Payload through untouched while the provider deserializes it with its
	// own proto codec (Name()=="proto"). The gateway gains no per-axis knowledge.
	var result []byte
	if err := g.provider.Invoke(out, method, req.GetPayload(), &result, grpc.ForceCodec(passthroughCodec{})); err != nil {
		return nil, err // provider's gRPC status (incl. INVALID_ARGUMENT) relayed verbatim
	}
	return &corev1.InvokeResponse{Result: result}, nil
}

// auditLog returns a copy of the emitted audit lines (for the harness to assert
// every mediated call was recorded — C8 is not optional).
func (g *stubGateway) auditLog() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.audit...)
}
