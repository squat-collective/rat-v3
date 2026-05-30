# ADR-005: Capability invocation model — core-mediated control plane

**Status:** Accepted
**Date:** 2026-05-30
**Deciders:** Tom, Claude (architecture session)
**Resolves:** [reviews/06](../../../reviews/06-proto-contract-review.md) finding C-6 (AUTH-2 ⊕ ARCH-2) — the open design decision flagged as freeze-blocking.

---

## Context

The proto contract review ([reviews/06](../../../reviews/06-proto-contract-review.md)) found that the headline RAT idea — **call-by-capability**, where a plugin declares `requires: rat://format/v1/merge` and the platform wires it to *some* provider — has **no wire mechanism**. A `strategy.Apply` orchestrates a `format` and a `runtime` it `requires`, but neither `RequestContext` nor `ApplyRequest` carries any handle (endpoint, token, routing) to actually *call* the resolved providers. The proto comments assert "the core wires providers in via the registry," but no field or service expresses it. The same gap exists for `engine`→`storage`/`catalog`.

The review's two owning lenses **disagreed on the fix**, and the disagreement is freeze-blocking because the two answers imply different wire shapes (whether requests must carry `{endpoint, token}`). This ADR resolves it.

The decision interacts with three load-bearing prior commitments:
- **The six-thing core** ([ADR-001](001-everything-is-a-plugin.md)) — one of the six is the **API gateway** ("single entry point; routes via identity gateway"). Another is the **registry** (resolves `(kind, name, version)` + capability lookups).
- **The cross-cutting-concerns rule** ([plugin-architecture.md](../../../.claude/rules/plugin-architecture.md), from [reviews/00-synthesis.md](../../../reviews/00-synthesis.md) C1–C10): trace propagation, plugin-to-core auth, state-gateway isolation, **capability enforcement at runtime (declared = enforced)**, mandatory audit emission, and tenancy are **properties the core must enforce because no plugin can be trusted to**. The security review's central finding was that deferring these to plugins is an unenforceable honor system.
- **"Data plane bypasses core for bytes"** ([overview.md](../overview.md)) — the perf-critical Arrow byte transfer goes plugin-to-plugin out-of-band (`ArrowStream`), never through the core.

## Decision

**Control-plane capability calls are core-mediated. Bulk data stays direct (out-of-band).**

When a plugin invokes a capability it `requires`, it does **not** dial the provider directly. It calls a new **core capability-invoke service** (the API gateway's own contract, currently missing from all 20 protos) with a capability URI + the request payload. The core:
1. resolves the capability URI → concrete provider via the **registry**,
2. enforces the cross-cutting properties on that hop — **C2** (caller authenticated), **C5** (caller actually `requires` this capability; provider actually `provides` it), **C7** (tenant scoping), **C3** (state namespace derivation), **C8** (audit emission),
3. stamps/propagates **C1** trace context and re-derives `caller_plugin` per hop (the keystone rule, [reviews/06](../../../reviews/06-proto-contract-review.md) C-1),
4. proxies the call to the provider and relays the response.

The core is a **switchboard, not an orchestrator** — the calling plugin still decides the *sequence* of capability calls (resolve → transform → write); "the core never commands" ([overview.md](../overview.md)) is preserved. The core only routes + enforces, one call at a time, on request.

**The bulk-data leg is the explicit exception.** Small control RPCs (file lists, metadata, `Resolve`/`Write` descriptors) are proxied; the Arrow byte stream they set up still flows plugin-to-plugin via `ArrowStream` and never touches the core. So the mediated path carries control traffic only — the performance-critical bytes path is unchanged.

### Wire shape this implies (for the freeze)

- A new `rat/core/v1/invoke.proto` — the capability-invoke service. This is the freeze-relevant artifact: it must exist in `rat/1`.
- `RequestContext` does **not** gain `{endpoint, token}` provider-routing fields (the direct-dial shape is rejected — see below). It keeps the three-principal identity model from the keystone.
- The actual `invoke` payload is an opaque envelope addressed by capability URI; the gateway routes by the envelope without interpreting the inner message (a generic proxy — it gains no per-axis knowledge).

## Consequences

### Positive
- **The cross-cutting properties are enforceable at one point.** C2/C3/C5/C7/C8 + C1 are applied by the core on every control hop, exactly as plugin-architecture.md requires — not re-implemented (or faked) by every plugin pair. This is the decisive reason: it's the only model under which "declared = enforced" is true rather than aspirational.
- **Capability negotiation becomes trustworthy.** A `strategy` calling `rat://format/v1/merge` cannot reach a provider that doesn't actually `provide` it, and a provider cannot serve a caller that doesn't `require` it — the gateway checks both against the manifests. This closes the "capability is an unenforced promise" hole the ecosystem review (reviews/02) named.
- **Uniform diagnosability + audit.** Every control call has a trace span and an audit record by construction, because the core is on the path. The "undiagnosable failure domain" finding (reviews/03) is structurally answered for control traffic.
- **The six-thing core absorbs this without a 7th thing.** Invocation routing is the API gateway + registry doing their existing jobs; no new core responsibility. (Temptation count unchanged.)

### Negative — accepted
- **A latency hop per control call.** Each capability call is now caller → core → provider instead of caller → provider. Mitigation: control RPCs are small and not the hot path (bytes bypass the core); the API gateway is active-active (per [ADR-002](002-founding-tech-stack.md) D5, only the *reconciler* is leader-only — replicas "serve API"), so it scales horizontally. If a future profiling pass shows control-call latency is genuinely unacceptable for some axis, a direct-dial fast-path can be added as a superseding ADR — but as an *optimization with a proven need*, not a v1 default, because enforcement can't be retrofitted onto a frozen direct-dial contract.
- **The gateway forwards opaque payloads.** It proxies capability-addressed envelopes it doesn't parse. This is generic-proxy plumbing, accepted as the cost of central enforcement. It is *not* per-axis core bloat — the gateway never learns what a `format` or `engine` message means.
- **The core is on the control path** (the SPOF objection). Accepted and bounded: it's already the API gateway for all external traffic; making internal capability calls use the same path is consistent, and HA is the same active-active story. A wedged core stops control calls regardless of this decision.

### Neutral
- The missing `invoke.proto` becomes a small, well-scoped addition rather than a sprawling per-axis change.

## Alternatives considered

### Direct-dial with core-issued capability-scoped tokens (the rejected finalist)
At resolve time the core hands the requirer `{endpoint, short-TTL token scoped to the capability URI}`; the plugin dials the provider directly; the callee validates the token's scope. Argued by the plugin-author lens: avoids the per-call SPOF + latency hop, mirrors the existing `storage.VendCredentials` pattern, and preserves "bypass core for work."

**Rejected because it distributes enforcement to every callee.** Each provider plugin would have to correctly validate the token, enforce capability scope, apply tenancy, emit audit, and propagate trace — i.e. re-implement the six cross-cutting properties, in any language, correctly, forever. That is precisely the honor-system the security review (reviews/04) and the cross-cutting-concerns rule reject: the first plugin that validates loosely, skips the audit emit, or mis-scopes the tenant silently breaks an invariant the platform claims to guarantee, and nothing central catches it. The `VendCredentials` analogy actually argues *for* mediation: credential vending is the one narrow, high-rigor exception we allow precisely because it must hand out a bearer capability for the bytes path — generalizing that bearer-token pattern to *all* control calls multiplies the blast radius of every callee's bugs. Direct-dial optimizes the dimension (latency) the control plane cares least about, at the cost of the dimension (uniform enforcement) it cares most about.

### Leave it implicit / out of the frozen contract
Rejected. This is the status quo the review flagged — "wired via the registry" with no wire mechanism. Freezing `rat/1` without the invoke contract means the headline feature is unbuildable and the gap can't be filled additively without deciding mediated-vs-direct first (the freeze-blocker). The decision must be in `rat/1`.

## Migration

This is pre-freeze design; nothing to migrate. Next actions:
- Add `contracts/proto/rat/core/v1/invoke.proto` during the freeze-blocker fix pass (it's one of the 15).
- The keystone `context.proto` rewrite (also a freeze-blocker) proceeds independently — `caller_plugin` re-derived per hop is exactly what the mediated gateway stamps, so the two fixes compose.
- Update [reviews/06](../../../reviews/06-proto-contract-review.md) C-6 status from "open decision" to "resolved by ADR-005" when the roadmap is synced.

## Related

- [reviews/06](../../../reviews/06-proto-contract-review.md) — finding C-6 (AUTH-2 ⊕ ARCH-2), which this ADR resolves; C-1 (keystone identity) which composes with it.
- [ADR-001](001-everything-is-a-plugin.md) — the six-thing core (API gateway + registry do the routing).
- [ADR-002](002-founding-tech-stack.md) — D5 (leader election is reconciler-only; API gateway is active-active → the mediation path scales).
- [plugin-architecture.md](../../../.claude/rules/plugin-architecture.md) — the cross-cutting-concerns rule that makes central enforcement non-negotiable.
- [reviews/00-synthesis.md](../../../reviews/00-synthesis.md) — C1–C10, the enforcement properties; [reviews/04](../../../reviews/04-security-reviewer.md) — the honor-system critique direct-dial would reintroduce.
- [docs/architecture/overview.md](../overview.md) — "data plane bypasses core for bytes" (the exception this ADR preserves).
