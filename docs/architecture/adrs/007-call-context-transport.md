# ADR-007: Call-context transport — cross-cutting context rides in transport metadata, not the payload

**Status:** Accepted
**Date:** 2026-05-31
**Deciders:** Tom, Claude (architecture session)
**Refines:** [ADR-005](005-capability-invocation-model.md) (upholds its generic-proxy guarantee). Relocates — does **not** discard — the carrier of the keystone `RequestContext` ([reviews/06](../../../reviews/06-proto-contract-review.md) C-1).
**Surfaced by:** the 0d stub invoke-gateway (`plugins/format/inmemory-go/gateway_test.go`) — the ADR-003 forcing function exposing a contract flaw a real implementation reveals. See [done.md 2026-05-31](../../../roadmap/done.md) + [ideas/inbox.md](../../../ideas/inbox.md).

---

## Context

[ADR-005](005-capability-invocation-model.md) made control-plane capability calls **core-mediated**: a caller invokes `CapabilityInvokeService.Invoke(capability, payload)` and the core's gateway resolves the provider, enforces the cross-cutting properties (C2/C3/C5/C7/C8), stamps C1 trace + re-derives `caller_plugin` per hop, and proxies the call. ADR-005 committed — as a load-bearing, accepted property — that the gateway is a **generic proxy that "forwards `payload` WITHOUT interpreting it"** and "never learns what a `format` or `engine` message means" (ADR-005 §Wire-shape, §Consequences).

Separately, the keystone `context.proto` rewrite (freeze-blocker #1) put a `RequestContext` — `{TraceContext trace, Identity identity, deadline}` — as **field 1 of every request message in every axis**, with two distinct handling rules baked into its structure:

- `TraceContext` → **propagate verbatim** (caller-supplied, relayed unchanged).
- `Identity` → **re-derive/re-stamp every hop, never trust the wire copy** (the core stamps `caller_plugin` from the authenticated channel, carries a core-signed `SubjectAssertion`, stamps `tenant`).

Building the stub gateway to validate ADR-005 surfaced a **direct contradiction between these two accepted commitments**:

1. The gateway must **re-stamp `identity` per hop** and **never trust the wire-supplied identity**. But `identity` lives *inside the payload* (field 1). A proxy that does not deserialize the payload **cannot rewrite the embedded identity** — so a forgeable, un-restampable identity reaches the provider in the message body.
2. The gateway must **reject RPCs lacking a well-formed `traceparent`** (`context.proto`: "the core rejects RPCs without a well-formed traceparent"). But `traceparent` also lives inside the payload — a generic proxy **cannot read it to validate it** without parsing the message.

In short: **the gateway needs to read, validate, and rewrite the cross-cutting envelope, but ADR-005 forbids it from parsing the payload the envelope currently lives in.** One of the two has to move. ADR-005's generic-proxy guarantee is the more load-bearing commitment (it's what keeps the six-thing core from growing per-axis knowledge, and what makes "declared = enforced" centralizable). So the envelope moves out of the payload.

This is exactly the failure mode [ADR-003](003-two-references-before-contract-freeze.md) predicts: *contracts designed in a vacuum are wrong in ways only the second real implementation reveals.* The stub is that implementation; this ADR is the pre-freeze fix.

## Decision

**The cross-cutting call context travels as a transport-metadata header, not as a field in the message body. The payload of every control RPC is the bare axis message.**

### 1. `RequestContext` becomes a metadata envelope, carried verbatim in shape

The keystone's `RequestContext` message — and its `TraceContext`, `Identity`, `SubjectAssertion` sub-messages — are **kept exactly as designed**. Only the *carrier* changes: instead of `RequestContext context = 1` in each request message, the serialized `RequestContext` rides in a single **binary gRPC metadata header `rat-callmeta-bin`** on every control RPC.

- Removed: the `RequestContext context = 1` field from every axis request message (all 18 axis services) **and** from `core/v1 InvokeRequest`. Field number 1 is reserved on those messages.
- Unchanged: `context.proto`'s message definitions. The three-principal model, the never-trust-wire-identity rule, and the structural trace/identity separation all survive — they move from "field-1 convention" to "metadata-header schema."
- The payload (`InvokeRequest.payload`, and the axis request it encodes) now carries **only axis data**. The gateway never needs to touch it.

### 2. The gateway reads, validates, and re-stamps the envelope — in metadata, never in the payload

On each mediated hop the gateway operates purely on `rat-callmeta-bin`:

- **Reads** the inbound envelope; **rejects** the call if `trace.traceparent`/`correlation_id` are absent or ill-formed (now possible — it's in metadata, not the opaque body).
- **Constructs** the outbound envelope: `trace` copied **verbatim** (the propagate-rule); `identity` **freshly stamped** — `caller_plugin` re-derived for this hop, `subject` (the core-signed `SubjectAssertion`) propagated, `tenant` stamped.
- **Sets** it as `rat-callmeta-bin` on the provider-bound call and proxies `payload` untouched.

This makes ADR-005's "forwards payload without interpreting it" **literally true** rather than aspirational. The two handling rules `context.proto` defined now map cleanly onto two metadata operations the proxy *can* perform without business-data knowledge: copy-trace, stamp-identity.

### 3. Providers trust the envelope because they authenticate the core as the transport peer

`caller_plugin` and `tenant` are **core-asserted** values. A provider accepts them because the channel peer **is the core** (plugin-to-core auth — the C2 mechanism). A non-core peer's identity metadata is ignored: **only the core may assert caller identity on a mediated hop.** The `subject` assertion is additionally self-verifiable regardless of peer (core signature over `(principal, tenant, bound_correlation_id, expires)` + `bound_correlation_id == trace.correlation_id` + TTL), exactly as the keystone specified — that verification contract is unchanged, only its carrier moved.

### 4. Plugin ergonomics are preserved by the SDK, not by a payload field

Generated SDK interceptors marshal/unmarshal `rat-callmeta-bin` transparently. A plugin's `Resolve(ctx, req)` still receives a populated `RequestContext`-shaped object — **reconstructed from metadata**, not read from `req`. Plugin authors never hand-roll headers. The one deliberate loss: a plugin can no longer read `req.context.identity` off the message body — because it isn't there. That removes the footgun (a careless plugin trusting a forgeable in-body mirror) by construction.

### 5. The bulk-data leg is unchanged

Identity never rides the out-of-band Arrow bytes path. `ArrowStream` tickets (short-TTL, single-use, bound to `{caller_plugin, tenant, stream}` — SEC-14) remain the sole gate for the data leg. This ADR touches the control path only.

## Consequences

### Positive
- **ADR-005's generic-proxy guarantee is upheld to the letter.** The gateway provably parses zero payload bytes — the property that keeps the six-thing core free of per-axis knowledge is now structurally enforced, not merely intended.
- **The gateway can finally do its stated job.** Validating `traceparent` and re-stamping `identity` per hop are both possible without payload interpretation — the two things ADR-005 + the keystone require but the in-payload carrier made impossible.
- **The forgeable-identity footgun is eliminated by construction.** There is no in-body identity field to trust; "never trust the wire copy" stops being an honor-system property and becomes "there is no wire copy in the body."
- **The keystone's design survives verbatim.** Three principals, signed subject assertion, trace/identity separation — all retained. Only the carrier changed, so the hard security thinking is not re-litigated.
- **Aligns with how the rest of the industry carries cross-cutting context** — auth and trace ride in headers/metadata (JWT, mTLS, W3C `traceparent`), not in message bodies. RAT was the outlier; this corrects it.

### Negative — accepted
- **Mechanical churn across the contract surface.** Removing `RequestContext context = 1` from ~all request messages in the 18 axis protos + `InvokeRequest`, then regenerating the 4 SDKs and updating the two `format` references + the stub gateway. Pre-freeze and mostly find-and-replace, but real. (This is the ADR-003 bargain: pay it now, in `v1-preview`, not as a flag-day after `v1`.)
- **Context is no longer visible in the message schema.** A reader of `format.proto` alone no longer sees that every call carries trace+identity. Mitigation: the `rat-callmeta-bin` header + its `RequestContext` schema are documented in `context.proto`'s header and each axis's `CONTRACT.md` (0g); the SDK surfaces it in every method signature, so it stays discoverable where authors actually work.
- **Couples the contract to a metadata-bearing transport.** gRPC provides this; RAT is committed to gRPC ([ADR-002](002-founding-tech-stack.md)). A hypothetical future non-gRPC transport must provide an equivalent header sidechannel (noted as out-of-scope below).

### Neutral
- `deadline_unix_ms` rides in the same envelope; nothing special.
- `core/v1 InvokeRequest` slims to `{capability, payload}` — a simplification.
- The shared golden vectors (`contracts/conformance/format-v1.json`) are unaffected — they assert on data behavior, not on context carriage — so the conformance cross-run re-passes after the migration with no vector change.

## Open questions

- **Q01 — one blob vs split headers.** Carry the whole `RequestContext` as one `rat-callmeta-bin`, or split into `rat-caller-plugin` / `rat-tenant` (ASCII) + `rat-subject-bin` (binary)? Leaning single blob (atomic, one proto type, simplest SDK). Revisit only if an L7 component needs to route on `tenant` without deserializing.
- **Q02 — W3C interop mirror.** Also emit standard `traceparent`/`tracestate` ASCII metadata for off-the-shelf tracing tooling, in addition to the in-blob trace? Probably yes (cheap, ecosystem-friendly); the in-blob copy stays authoritative for RAT correlation. Defer to the observability reference work.
- **Q03 — non-gRPC transports.** If a second transport is ever introduced, define its equivalent header carriage + the gateway's stamping there. Out of scope until real.

## Alternatives considered

### 1. Splice the in-payload envelope (gateway decodes field 1, re-stamps, re-encodes)
The gateway interprets *only* the universal `RequestContext context = 1` prefix (axis-independent by construction), rewrites `identity`, and re-emits the message with the rest of the bytes untouched. **Rejected:** it violates ADR-005's accepted "forwards payload WITHOUT interpreting it" guarantee — adopting it would require *amending ADR-005*, trading away the property that keeps the core axis-generic. It also forces a partial decode + re-serialization of every control message on every hop, and the "it only touches field 1" carve-out is a slippery precedent (the next reviewer asks why the gateway can read field 1 but not field 2). Strictly more coupling than moving the envelope out.

### 2. Keep `identity` in the payload as a non-authoritative mirror the SDK overwrites
Leave the field; document that it's advisory and have the consuming SDK ignore the body copy in favor of one stamped elsewhere. **Rejected:** it leaves a forgeable identity in the message body that a careless plugin — or any non-SDK / cross-language consumer that reads the proto directly — can read and trust. "It's only a mirror" is precisely the honor-system property the security review (reviews/04) and the cross-cutting-concerns rule refuse to depend on. A field that must-not-be-trusted but is-right-there is a latent CVE.

### 3. Identity in metadata, but trace stays in the payload
Move only the part that must be re-stamped; let trace ride along in the opaque body (the proxy relays it verbatim for free). **Rejected:** the gateway must *reject* RPCs lacking a well-formed `traceparent`, which means it must *read* trace — impossible if trace is in the un-parsed body. Once traceparent must be in metadata for validation, splitting the envelope across two carriers buys nothing and complicates verbatim propagation. The envelope is cohesive; keep it whole.

### 4. Leave it implicit (status quo)
**Rejected:** this *is* the finding. The contradiction is freeze-blocking for **every** axis (it touches the universal context carrier), and it cannot be fixed additively after `v1` without a flag day — exactly the inversion [ADR-003](003-two-references-before-contract-freeze.md) exists to prevent.

## Migration

Pre-freeze; mechanical. No deployed plugins exist. Order (one implementation commit, separate from this ADR):

1. **`context.proto`** — keep the messages; revise the header prose from "field 1 of every request" to "carried in the `rat-callmeta-bin` metadata header"; specify the header name, binary encoding, and the gateway's read/validate/stamp contract.
2. **Strip the field** — remove `RequestContext context = 1` from every axis request message (18 services) and from `core/v1 InvokeRequest`; `reserved 1;` on each. `buf lint/build/generate` clean; `buf breaking` will flag it (expected + allowed in `v1-preview`).
3. **Regenerate the 4 SDKs** (`scripts/gen-sdks.sh`) and add the interceptor that marshals/unmarshals `rat-callmeta-bin` ↔ a context object in each language's SDK (Go first, matching the reference).
4. **Update the references** — `inmemory-go` + `inmemory-py` read context from metadata; the **stub gateway** switches from its ad-hoc `x-rat-caller-plugin`/`x-rat-tenant` headers to the formal `rat-callmeta-bin` envelope, and adds the missing-traceparent rejection. Re-run the shared golden-vector cross-run (unchanged vectors; must stay green).
5. **Sync** [reviews/06](../../../reviews/06-proto-contract-review.md) C-1 note + the roadmap.

This refines ADR-005 (which stays **Accepted** — its decision is upheld, not reversed). The keystone's *field-1 placement* is superseded by this ADR; its *message design* is retained unchanged.

## Related

- [ADR-005](005-capability-invocation-model.md) — core-mediated invocation; this ADR makes its generic-proxy guarantee literally enforceable.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule whose forcing function surfaced this; the stub gateway is the "second implementation reveals the flaw" case in action.
- [ADR-001](001-everything-is-a-plugin.md) — the six-thing core; keeping the gateway payload-blind is what stops a 7th (per-axis) thing creeping in.
- `contracts/proto/rat/common/v1/context.proto` — the keystone messages (retained); `contracts/proto/rat/core/v1/invoke.proto` — `InvokeRequest` slims to `{capability, payload}`.
- [reviews/06](../../../reviews/06-proto-contract-review.md) C-1 (keystone identity) — the design this relocates; [reviews/04](../../../reviews/04-security-reviewer.md) — the honor-system critique that kills alternative 2.
- [.claude/rules/plugin-architecture.md](../../../.claude/rules/plugin-architecture.md) — the cross-cutting-concerns rule (trace propagation + plugin-to-core auth + capability enforcement) this carriage decision serves.
- [ideas/inbox.md](../../../ideas/inbox.md) — the 2026-05-31 finding this ADR resolves.
