# ADR-008: Streaming capability invocation — per-cardinality Invoke variants, enforce-at-open

**Status:** Accepted
**Date:** 2026-05-31
**Deciders:** Tom, Claude (architecture session)
**Extends:** [ADR-005](005-capability-invocation-model.md) (the unary `Invoke` this generalizes) and [ADR-007](007-call-context-transport.md) (whose enforce-at-open + identity-in-metadata model it reuses).
**Surfaced by:** the 0d `runtime` reference (`examples/runtime/inmemory-go/harness_test.go`) — the ADR-003 forcing function exposing a contract gap a real implementation reveals. See [done.md 2026-05-31](../../../roadmap/done.md) + [ideas/inbox.md](../../../ideas/inbox.md).

---

## Context

[ADR-005](005-capability-invocation-model.md) made control-plane capability calls **core-mediated**: a caller invokes `core/v1 CapabilityInvokeService.Invoke(InvokeRequest) returns (InvokeResponse)` and the core's gateway resolves the provider, enforces the cross-cutting properties (C2/C5/C7/C8 + traceparent), stamps identity into the downstream `rat-callmeta-bin` envelope ([ADR-007](007-call-context-transport.md)), and relays the opaque payload without interpreting it. The decisive reason ADR-005 chose mediation over direct-dial was that **central enforcement is the only model under which "declared = enforced" is true** rather than an honor system every callee must re-implement correctly.

But `Invoke` is **unary** (`InvokeRequest → InvokeResponse`). Building the `runtime` 0d reference exposed that this is not enough: `runtime/v1`'s `Execute(ExecuteRequest) returns (stream ExecuteResponse)` is **server-streaming** (interim `ExecuteProgress` liveness + a terminal `ExecuteCompleted`). A unary `Invoke` cannot carry a streamed response, so **a server-streaming capability has no core-mediated invocation path.** The runtime reference had to be driven **directly**, bypassing the gateway — which means its C2/C5/C7/C8 + traceparent seams are currently unenforced.

This is not a runtime quirk. The contract already has four streaming methods across three axes:

| Method | Cardinality |
|---|---|
| `runtime.Execute` | server-streaming |
| `state.Watch` | server-streaming |
| `scheduler.WatchDue` | server-streaming |
| `observability.Ingest` | bidirectional |

Every one of these is a capability a plugin can `require`, and none can be mediated by the unary `Invoke`. The gap is freeze-relevant: `invoke.proto` is in the `rat/1` surface, and leaving it unary-only either (a) permanently exempts all streaming capabilities from enforcement, or (b) forces every streaming axis to mutilate its contract to avoid streaming. Both are unacceptable.

This is the same shape of finding as [ADR-007](007-call-context-transport.md): a load-bearing prior decision (here, ADR-005's central-enforcement thesis) collides with a wire reality (streaming) only a real implementation reveals.

## Decision

**Streaming capabilities are core-mediated too. The core's invoke service gains per-cardinality `Invoke` variants — generic byte-relays that enforce once at stream-open — so the gateway mediates streams exactly as it mediates unary calls, and remains axis-generic.**

### 1. Reject direct-dial for streams (consistency with ADR-005)

A streaming capability invocation is still a capability call: WHO may invoke `rat://runtime/v1/execute` is an authorization decision that must be enforced centrally. Streaming does not make it "data plane." (The bulk-data Arrow leg is exempt from mediation precisely because it is set up *by* a prior mediated control RPC that already enforced authz; a streaming capability method IS that control RPC — there is no prior call to enforce on, so direct-dialing it skips enforcement entirely.) The honor-system ADR-005 rejected for unary control calls is rejected here for the same reason.

### 2. Add two streaming variants to `CapabilityInvokeService`

```proto
service CapabilityInvokeService {
  rpc Invoke(InvokeRequest) returns (InvokeResponse);                          // unary (ADR-005)
  rpc InvokeServerStream(InvokeRequest) returns (stream InvokeResponse);       // server-streaming
  rpc InvokeBidiStream(stream InvokeRequest) returns (stream InvokeResponse);  // bidi (+ client-streaming)
}
```

- **`InvokeServerStream`** mediates server-streaming capabilities (`runtime.Execute`, `state.Watch`, `scheduler.WatchDue`). One `InvokeRequest` `{capability, payload}`; the gateway relays a stream of `InvokeResponse`, each `result` being one serialized axis response frame (e.g. one `ExecuteResponse`).
- **`InvokeBidiStream`** mediates bidirectional capabilities (`observability.Ingest`) **and** pure client-streaming (the provider simply returns a single response frame). The **first** `InvokeRequest` frame establishes the `capability` (and triggers enforcement); subsequent request frames carry only `payload` and MUST leave `capability` empty.

No `InvokeClientStream` is added — `InvokeBidiStream` subsumes it. The existing unary `Invoke` is unchanged. The caller's generated SDK selects the variant from the target method's cardinality (known from the method descriptor + the `(rat.common.v1.capability)` annotation).

`InvokeRequest` / `InvokeResponse` are reused as-is (`{capability, payload}` / `{result}`) — no new message types.

### 3. Enforce once at stream-open; stamp identity for the stream's lifetime

When a stream opens, the gateway does exactly what unary `Invoke` does — on the first (or only) `InvokeRequest`:

- validate traceparent (C1) + caller authentication (C2);
- check the caller `requires` and the provider `provides` the capability (C5), routing via the registry + the `(rat.common.v1.capability)` annotation;
- apply tenant scoping (C7) + derive the state namespace (C3);
- stamp the downstream `rat-callmeta-bin` envelope (trace verbatim, `caller_plugin` re-derived, subject propagated, tenant stamped — ADR-007) for the **whole** stream;
- emit **one** C8 audit record per mediated stream (at open), not one per frame.

Then it relays every frame's opaque bytes via the passthrough codec (`Name() == "proto"`), **never deserializing** — the same generic-proxy guarantee as unary (ADR-005). Cancellation + deadlines propagate via the gRPC context; gRPC per-stream flow control composes through the passthrough relay.

### 4. Streaming-invoke carries CONTROL streams; bulk data stays on the Arrow leg

These variants mediate **control** streams — liveness (`ExecuteProgress`), watches (`state.Watch`), telemetry framing (`observability.Ingest`). They are not the bulk-bytes path. Genuinely high-volume data still flows out-of-band via `common/v1 ArrowStream` (`overview.md` "data plane bypasses core for bytes"), unchanged. Guidance for axis authors: a streaming capability method is for control/liveness framing; if you are moving rows, return an `ArrowStream` descriptor, do not stream the rows through the invoke gateway.

## Consequences

### Positive
- **"Declared = enforced" holds for every cardinality.** Streaming capabilities get the same central C2/C5/C7/C8 + traceparent enforcement as unary — the property ADR-005 exists to guarantee, now true for the 4 streaming methods (and any future one) instead of being silently exempt.
- **The gateway stays axis-generic, even for streams.** It relays opaque frame bytes; it gains no per-axis knowledge of `ExecuteResponse` or `IngestResponse`. The six-thing core does not grow a 7th thing — this is the API gateway + registry doing their existing jobs over a stream.
- **Consistent with ADR-005 + ADR-007.** Enforce-at-open + identity-in-metadata is reused verbatim; streaming is "unary with N frames," not a new model.
- **Minimal, additive frozen-surface growth.** Two new RPCs on an existing service; `buf breaking` does not flag added methods. `runtime` (and later `state`/`scheduler`/`observability`) can finally route through the gateway.

### Negative — accepted
- **More surface in the frozen core.** Two extra RPCs the contract must support forever. Accepted: they are the minimum to cover the cardinalities the contract already uses.
- **The gateway grows streaming-relay plumbing.** Server-stream and especially bidi relays (half-close, cancellation, error propagation across two streams) are more complex than the unary relay. Accepted as the cost of central enforcement for streams; it is generic plumbing, written once.
- **Enforce-at-open means a long-lived stream's authz is evaluated once.** If a caller's capability is revoked mid-stream, an already-open stream runs to completion. Accepted for v1 (same as any bearer-at-open model); periodic re-auth / mid-stream revocation is an Open question, not a v1 requirement. Mitigation: control streams are typically short or watch-style (cheap to re-establish).

### Neutral
- The SDK must pick the right `Invoke*` variant by capability cardinality — done by generated code from the method descriptor, invisible to plugin authors.
- Pure client-streaming has no axis user today; `InvokeBidiStream` covers it if one appears.

## Open questions

- **Q01 — mid-stream authz revocation.** Re-validate periodically on long-lived streams, or accept open-stream-survives-revocation? Defer; enforce-at-open is the v1 model. Revisit if a long-lived watch capability needs tighter revocation.
- **Q02 — backpressure end-to-end.** gRPC per-stream flow control should compose through the passthrough relay (a slow caller backpressures the provider through the gateway); confirm in the implementation, especially for bidi.
- **Q03 — cancellation/deadline fidelity.** Caller-cancel and deadline expiry must propagate to the downstream stream and tear it down; verify in the relay (standard gRPC context propagation, but bidi needs care).

## Alternatives considered

### 1. Direct-dial with a gateway-issued, capability-scoped token (the inbox option b)
A unary "open" call mints a short-TTL token; the caller dials the provider's stream directly; the provider validates. Mirrors the `ArrowStream` bulk leg + `storage.VendCredentials`. **Rejected:** it reintroduces the exact honor-system ADR-005 rejected — enforcement (capability scope, tenancy, audit, trace) is re-implemented by every streaming callee, in any language, forever, and the first one that validates loosely silently breaks an invariant. The `ArrowStream` analogy does not transfer: the bytes leg bypasses the core *because a prior mediated control RPC already authorized it and returned the descriptor*. A streaming capability method IS the control RPC — there is no prior mediated call, so direct-dial skips enforcement at the one point it matters. Streaming liveness is not special enough to abandon central enforcement of *who may invoke the capability*.

### 2. Progress → event bus; `Execute` becomes unary (the inbox option c)
Re-publish `ExecuteProgress` as `common/v1 Event`s keyed by `correlation_id`; make `Execute` unary returning only the terminal result. **Rejected:** it fixes the wrong layer — reshaping every streaming axis contract to dodge an *invoke-contract* limitation. It loses request-scoped liveness + backpressure (events are best-effort + reorderable per the event-bus failure-modes thinking), couples every streaming axis to the bus, and does not generalize: `observability.Ingest` is bidi and cannot become unary. Fix the invoke contract once; don't mutilate `runtime`, `state`, `scheduler`, and `observability` around its gap.

### 3. Leave streaming capabilities un-mediated (the reference's current direct-dial, permanently)
**Rejected:** this is the status quo the finding flags. It permanently exempts all four streaming methods (and every future one) from C2/C5/C7/C8 — "declared = enforced" becomes false for any streaming capability, and the enforcement hole grows with each streaming axis. The whole point of ADR-005 is that exemptions like this are how the security model rots.

### 4. One universal bidi `InvokeStream` for all cardinalities
Use a single `InvokeBidiStream(stream InvokeRequest) returns (stream InvokeResponse)` for unary, server-streaming, and bidi alike (unary = 1 req, 1 resp). **Rejected as the sole mechanism:** forcing unary calls through a bidi RPC loses gRPC's unary ergonomics (clean call sites, deadlines) and the existing `Invoke`, and obscures cardinality at the call site. **Partially adopted:** `InvokeBidiStream` *does* cover both bidi and client-streaming, so the final set is three methods (`Invoke` + `InvokeServerStream` + `InvokeBidiStream`), not four.

## Migration

Pre-freeze; additive. This is an ADR-only commit (one-ADR-per-commit). The implementation lands separately:

1. **`invoke.proto`** — add `InvokeServerStream` + `InvokeBidiStream` to `CapabilityInvokeService` (reusing `InvokeRequest`/`InvokeResponse`); document enforce-at-open + the first-frame-establishes-capability rule for bidi. `buf lint/build` clean; added methods are non-breaking.
2. **Regenerate the 4 SDKs.**
3. **Stub gateway** — add a server-stream relay (the runtime case); enforce once at open, stamp downstream `rat-callmeta-bin`, relay frames via the passthrough codec, one C8 audit per stream.
4. **Route `runtime.Execute` through `InvokeServerStream`** in `examples/runtime/inmemory-go` (replacing the direct-dial workaround + updating its header note); add `runtime.proto`'s **deferred** `(rat.common.v1.capability) = "rat://runtime/v1/execute"` annotation (+ import) so the gateway can route it. Re-run the **unchanged** runtime golden vectors (must stay green — behavior-preserving, like ADR-007's migration).
5. The bidi relay (`observability.Ingest`) + the other server-streams (`state.Watch`, `scheduler.WatchDue`) get wired when those axes are referenced in 0d.

## Related

- [ADR-005](005-capability-invocation-model.md) — the unary core-mediated `Invoke` this generalizes; its central-enforcement thesis is the reason direct-dial is rejected here too.
- [ADR-007](007-call-context-transport.md) — enforce-at-open + identity-in-`rat-callmeta-bin`-metadata, reused verbatim for streams.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule whose forcing function surfaced this (the `runtime` reference).
- [ADR-001](001-everything-is-a-plugin.md) — the six-thing core; the streaming relay is still the API gateway + registry, no 7th thing.
- `contracts/proto/rat/core/v1/invoke.proto` — gains the two variants; `contracts/proto/rat/runtime/v1/runtime.proto`, `state.proto`, `scheduler/v1/scheduler.proto`, `observability/v1/observability.proto` — the streaming methods this lets the gateway mediate.
- [ideas/inbox.md](../../../ideas/inbox.md) — the 2026-05-31 finding this ADR resolves.
- [.claude/rules/plugin-architecture.md](../../../.claude/rules/plugin-architecture.md) — the cross-cutting-concerns rule (capability enforcement) this preserves for streams.
