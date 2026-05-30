# Proto contract review — RAT v3 0b axis protos (adversarial team review)

> A 4-person expert peer-review team reviewed the 20 sub-phase-0b proto files (18 axis
> services + `common/v1/{context,data}`) plus `schema/plugin.v1.json`, cold (without the
> prior architecture reviews' answers), then cross-challenged each other's findings and
> agreed severity collectively. Lenses: **api-designer** (proto/gRPC design),
> **plugin-author** (implementability), **security-eng** (does the wire enforce the
> comments?), **systems-architect** (composition / failure / diagnosability).
> `buf lint/build/generate` already pass — every finding here is a *design* issue lint cannot catch.

**Date:** 2026-05-30 · **Scope:** `contracts/proto/**` + `contracts/schema/plugin.v1.json` ·
**Status of protos:** DRAFT, pre-freeze (this review exists to find what's wire-breaking to
retrofit *before* `rat/1` freezes — per [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md)).

---

## Executive summary

The 20 protos are **clean as individual services** — consistent style, correct streaming
choices in most places, trace context (`RequestContext`) threaded through every sync RPC,
and the Critical concerns from the prior architecture review (C1/C2/C3/C5/C7, I8/I9/I13)
all have *a* wire home. But the review found a consistent pattern across all four lenses:
**the cross-plugin properties that are the actual thesis of RAT — capability-by-negotiation
invocation, per-plugin/per-tenant isolation, tamper-evident audit — are asserted in proto
*comments* but not enforced by the proto *fields/types*.** They are comment-deep.

Three themes, each surfaced independently by 2–4 lenses (convergence = signal):

1. **Identity is forgeable AND ambiguous (the keystone).** `RequestContext.subject`/`tenant`
   are caller-set strings with no channel binding, and `subject` is even *defined
   contradictorily* (user in `identity.proto`, plugin in `state.proto`). Every isolation and
   attribution claim (C3 state, C7 cred scope, secret scope, audit subject, billing) derives
   from these fields, so all of it is unenforceable as written.
2. **The "call-by-capability" feature is unexpressible on the wire.** A `strategy` that
   `requires` a format-capability has no field, token, or endpoint to actually *call* the
   provider. The headline "orthogonal axes compose" claim has no wire mechanism.
3. **Async + audit + invocation paths are under-specified.** No event-bus envelope (so the
   async coordination path carries no trace/tenant), an audit chain that's decorative not
   tamper-evident, and a state CAS the docs assume is linearizable while advertising an
   eventually-consistent backend (DynamoDB) → split-brain leader election.

**Verdict for the freeze gate:** the contract is **not ready to freeze**, but the blocking set
is bounded — **15 freeze-blockers + 1 open design decision** (AUTH-2 invocation model, on which
the two owning lenses disagree). Most of the *other* ~28 findings are *GA-blockers* — fixable
post-freeze as additive fields + enforcement. The freeze-blockers must be resolved now because
each fix redefines the meaning/shape of an existing field, is a breaking wire change, or pins a
decision (like the invocation model) that later changes an existing field. Fix those, and the
contract earns its freeze.

### The two-axis classification

Every finding carries **severity** (how bad) × **freeze-gate** (when it must be fixed):
- **FREEZE-BLOCKER** — the fix redefines the meaning/shape/grammar of an *existing* field, or
  is a breaking wire change. Impossible under additive-only post-freeze rules → **must fix now.**
- **GA-BLOCKER** — additive new field(s) + enforcement/doc work a frozen `rat/1` can still
  accommodate → real, but deferrable.

---

## ⛔ The freeze-blocker shortlist (must fix before `rat/1`)

These are the only items that genuinely cannot be fixed after freeze. This is the actionable core of the review.

| # | Finding | Lenses | Why it can't wait |
|---|---------|--------|-------------------|
| 1 | **Keystone: identity disambiguation + binding** (SEC-1⊕AUTH-12) | sec, author, api | Pins the *meaning* of the existing `subject` field; redefining it post-freeze breaks every plugin that baked in either reading |
| 2 | **format capability URI naming** (API-7⊕AUTH-1) | api, author, sec | The capability URI string is baked into every format manifest's `provides`/`requires` + examples; renaming later breaks all of them |
| 3 | **State key/prefix grammar** (SEC-2) | sec | Constraining the existing `key`/`prefix` strings later (to forbid `../`, `/`, null) is a breaking tightening |
| 4 | **Audit response shape + prefix-only semantic** (SEC-5⊕API-5) | sec, api | Replacing `AppendResponse.appended:int64` with `RecordAck` + pinning prefix-only failure semantics is non-additive; canonical-serialization pin is un-retrofittable once chains exist |
| 5 | **State CAS linearizability as conformance + backend set** (ARCH-3) | arch | The linearizable-CAS conformance gate and the advertised-backend decision (drop DynamoDB or require strong-consistency mode) must be settled before the lease contract freezes |
| 6 | **`secret.Resolve.found` meaning** (API-1d) | api, sec | Pinning `found:false` to also mean "exists-but-unauthorized" (anti-enumeration) redefines an existing field |
| 7 | **Arrow protocol + role/direction field** (AUTH-3⊕SEC-14, partial) | author, sec | Pinning Flight-vs-bespoke and adding the host-vs-dial role field must be in the frozen `ArrowStream` shape for it to be buildable interoperably (ticket-auth spec itself is GA) |
| 8 | **Split `Write` per-mode + `engine.Execute`** (AUTH-8) | author, api | Splitting an RPC is breaking; needed so capability granularity (append≠merge) is method-level enforceable (C5) |
| 9 | **`rat.capability` method annotation** (AUTH-9) | api, author | Additive, BUT the `rat/common/v1/annotations.proto` extension must ship *inside* the frozen surface and reflect the final method set |
| 10 | **`observability.Ingest` streaming shape** (API-4) | api | client-streaming→bidi/unary is a breaking RPC-shape change |
| 11 | **Timestamps: int64-ms vs `Timestamp`** (API-10) | api | int64↔Timestamp is a breaking type change; ratify the direction now (consistency is not the question — wire type is) |
| 12 | **`slots.target` wrap** (API-17) | api | `string`↔`capabilityRef` object is breaking; pick the end-state (wrap) now |
| 13 | **`state.Put` outcome tri-state** (API-1 state axis / ARCH) | arch, api, sec | `committed:bool` has no "UNKNOWN" state; on a timeout/partition the write may-or-may-not have committed → lease renewal can't fence safely. Adding COMMITTED/CONFLICT/UNKNOWN reinterprets the existing `committed` field → can't be done post-freeze (upgraded from GA after systems-architect's reconciler read) |
| 14 | **Async event-bus envelope** (ARCH-1) | arch | No `common/v1/event.proto` exists, so the entire async/reconciliation plane (the backbone — "core emits events, plugins react") has no wire contract: no trace/correlation/tenant on async. You cannot freeze "the wire contract" with half the comms model unspecified. Minimum: the freeze must explicitly carve async out as unfrozen; better, add the envelope now |
| 15 | **`MergeBranch` idempotency + expected snapshots** (ARCH-4) | arch | `MergeBranchRequest` takes only names → retried/concurrent merges double-apply or lose updates. Adding `expected_snapshot` + `idempotency_key` extends the existing message; the commit-linkage RPC itself is additive/GA, but the request-shape change is freeze-gated on catalog |

**Open decision (owning lenses split — must be decided pre-freeze):**
- **AUTH-2 / ARCH-2 — capability invocation model.** A `strategy`/`engine` has no wire handle to
  call a capability it `requires` (Critical severity; the headline "call-by-capability" feature
  is unbuildable today). The two lenses that own it **disagree on the fix**, and the choice is
  freeze-gated because it determines whether `RequestContext` must carry `{endpoint, token}`:
  - **systems-architect → core-mediated.** Control calls proxied through the core API gateway
    (caller sends a capability URI + payload; core resolves provider, enforces
    capability/identity/tenancy, emits audit, stamps trace, proxies). Rationale: the six
    cross-cutting enforcement properties (C1/C2/C3/C5/C7/C8) can *only* be enforced if the core
    sits on the control path; direct-dial forces every plugin pair to re-implement all six. The
    missing artifact is a **core-facing capability-invoke proto** (the API gateway's own contract
    is absent from all 20 files). Bytes still bypass core via `ArrowStream`.
  - **plugin-author → direct-dial with core-issued scoped tokens.** Core resolves at plan time
    and hands the requirer `{endpoint, short-TTL token scoped to the capability URI}`; callee
    enforces C5 by validating token scope. Rationale: proxy makes the 6-thing core a per-call
    SPOF + forces generic-payload forwarding (core-surface bloat); direct-dial mirrors the
    existing `storage.VendCredentials` pattern and preserves "bypass core for work."
  - **Lead note:** unresolved. Both fixes are additive once the model is chosen, but the *choice*
    is freeze-blocking. The per-hop identity rule (keystone) holds under either. This warrants its
    own ADR before freeze.

**Freeze-slivers** (look GA, but one decision must be pinned now): `options` bytes encoding
(API-12: declare "UTF-8 JSON validated against `metadata_schema`"); sentinel→`optional`
presence (API-13); `state.List` default `page_size` meaning (API-3); scheduler
delivery-semantics doc (API-11); error-handling *convention* choice (API-1/AUTH-6 — pick
gRPC-status+details vs in-band now so authors build one model); the "what the signature
covers" decision (AUTH-14⊕SEC-15).

> **Standing caveat (security-eng):** land the *cheap additive placeholder* fields now as
> insurance (audit signature field, `debug_redact` options, manifest `image` digest), and do
> **not** market security properties — especially "tamper-evident audit" — that the
> enforcement layer hasn't delivered yet. False assurance is worse than a documented gap.

---

## Critical findings

### C-1 · Keystone — `RequestContext` identity is forgeable AND semantically ambiguous
**SEC-1 ⊕ AUTH-12** · authors: security-eng + plugin-author + api-designer (3-lens convergence) · **CRITICAL · FREEZE-BLOCKER**
**Files:** `common/v1/context.proto:37-47`, `identity/v1/identity.proto:44-50`, `state/v1/state.proto:14-19`

Two defects in the same field that compound:
- *Forgeability:* `subject`/`tenant` are plain caller-populated strings with no binding to the
  authenticated channel — no signed assertion, no MAC, no server derivation. A malicious
  plugin sets any `subject`/`tenant`.
- *Ambiguity:* `subject` = authenticated USER in `context.proto`+`identity.proto`, but
  `state.proto` derives the per-plugin C3 namespace from `subject` = "the calling plugin."
  Same field, two principals; there is **no `caller_plugin` field at all**.

Fixing either alone leaves C3 broken (correct-but-forgeable still breaks; bound-but-wrong-meaning still breaks). Blast radius: every isolation/attribution claim — C3 state, C7 storage cred scope, secret scope, billing, audit subject, tenancy `Decide` inputs.

**Agreed fix — three principals, each with a defined trust source:**
1. **`caller_plugin`** — invoking plugin's identity, DERIVED server-side from the C2 channel
   auth (token/mTLS), **re-derived every hop**, never caller-writable, never propagated. C3
   namespace = `(caller_plugin, tenant)`. (On `strategy→format`, `caller_plugin=strategy`; on
   `format→state`, `caller_plugin=format` → format's own namespace. Re-derivation per hop is
   *forced* by C3: a propagated caller would let `format` write into `strategy`'s namespace.)
2. **`subject`** — the end user, as a **core-signed assertion** (not a bare string),
   short-TTL **and** bound to `correlation_id`, **re-validated at every consuming hop**
   (`assertion.correlation_id == inbound RequestContext.correlation_id`, else reject).
   This is the anti-replay / confused-deputy fix: a downstream plugin can't bank a valid
   assertion and replay it for unrelated actions within the tenant.
3. **`tenant`** — server-stamped from the authenticated principal, propagated, not caller-writable.

Trace fields (`traceparent`/`tracestate`/`correlation_id`) stay caller-propagated-verbatim and
must be **structurally separated** from the must-not-trust identity fields. Bonus: audit
records then capture both `subject` (who triggered) and `caller_plugin` (who acted) — dual
attribution for free.

**Freeze rationale:** adding `caller_plugin` is additive, but disambiguating the *existing*
`subject` field's meaning cannot be done post-freeze without breaking plugins that hard-coded
either reading.

### C-2 · `format` capability URI naming breaks the contract triple
**API-7 ⊕ AUTH-1** · api-designer + plugin-author + security-eng · **CRITICAL · FREEZE-BLOCKER**
**File:** `format/v1/format.proto` + `schema/plugin.v1.json` + `examples/`

Every axis maps kind→URI uniformly (`rat.state.v1` ↔ `rat://state/v1/…`) **except `format`**,
which uses `rat://format-capability/v1/{scan,merge,append,maintain}` while the package is
`rat.format.v1`. The schema says the capability URI "mirrors the proto package coordinate" and
per-kind schemas are "derived from the proto" — that derivation yields `rat://format/v1/scan`,
which does **not** match the examples. The triple's own stated invariant is broken for the
single most-referenced axis (every strategy `requires` it). It's also the C5 authz key, so the
string is baked into every format manifest's `provides`/`requires`.
**Fix:** rename capabilities to `rat://format/v1/…` to match the package (recommended), or
rename the package — but the value must be settled pre-freeze. (Resolution is a coin-flip; the
*decision* is the freeze-blocker.)

### C-3 · Audit trail is neither tamper-evident NOR complete
**SEC-5 ⊕ API-5** · security-eng + api-designer · **CRITICAL · FREEZE-BLOCKER** (shape) + GA-hardening
**File:** `auditlog/v1/auditlog.proto`

The header claims an "append-only, tamper-evident" mandatory trail (I8). As written it delivers
neither, and the audited party (a third-party plugin) authors its own tamper-evidence.
- *Integrity:* `prev_hash` is unsigned (anyone who can write recomputes the chain after editing
  → not tamper-*evident*); canonical serialization unspecified (cross-impl verification
  impossible); the third-party sink can drop/reorder/rewrite; `id`/`prev_hash` are
  caller-authored and `Append` isn't core-only → a plugin can inject forged records or fork the
  chain; concurrent emitters race `prev_hash`.
- *Completeness:* `AppendResponse.appended:int64` hides partial failure — committing
  records[0]+[2] but rejecting [1] **forks the chain**; a dropped record (itself a security
  event) can't be signalled.

**Fix (halves interlock):** *core* (not sink, not caller) assigns `id`+`prev_hash` and
serializes into one linear chain; each record core-signed (Ed25519) so a third-party sink
*verifies but can't forge*; canonical serialization pinned in-contract; `Append` core-only.
Replace `appended` with per-record `RecordAck` {core-assigned id, status ∈
COMMITTED/DUPLICATE/REJECTED, enumerated reject_code}; commit is **prefix-only** (a REJECTED
entry ⇒ all later uncommitted); `last_committed_id`+`last_committed_hash` watermark for
deterministic resume; STATUS_DUPLICATE makes Append idempotent under the forced retries. A
dropped/rejected record is itself an auditable meta-event.
**Freeze:** the `AppendResponse` shape change + prefix-only semantic + canonical-serialization
pin are non-additive / un-retrofittable. Signing-enforcement + meta-audit emission are GA-hardening.

### C-4 · State CAS linearizability is prose, not conformance — and contradicts the advertised DynamoDB topology
**ARCH-3** · systems-architect (new — caught by no other lens) · **CRITICAL · FREEZE-BLOCKER**
**File:** `state/v1/state.proto` + `docs/architecture/overview.md`

`state.proto` says CAS "MUST be linearizable," but (a) it's a comment, not a conformance gate
(and `plugin-architecture.md` only promises control-plane axes "one reference + conformance"),
and (b) overview.md's "Full cloud (SaaS)" topology lists **DynamoDB**, whose default reads are
eventually consistent. The reconciler's leader-election lease (ADR-002 D5) is built on this CAS.
Weakly-consistent backend → two leaders → split-brain reconcile → duplicate pipeline runs /
conflicting writes.
**Fix:** make single-key linearizable CAS + ordered `Watch` a **mandatory conformance gate**,
and either drop DynamoDB from the advertised set or require its strongly-consistent mode. The
conformance obligation + backend-set decision must be pinned before the lease contract freezes.

### C-5 · No uniform error model
**API-1 ⊕ AUTH-6 ⊕ ARCH-8** · api-designer + plugin-author + systems-architect · **CRITICAL** · mostly GA, **one FREEZE sliver**
**Files:** every axis (bare `bool`/`int` outcomes); `state.proto`, `identity.proto`, `tenancy.proto`, `runtime.proto`

Every axis signals outcomes with bare bools + free-text strings (`committed`, `found`,
`authenticated`, `success`+`error`, `delivered`…). No typed code, nowhere — callers can't
distinguish NOT_FOUND vs PERMISSION_DENIED vs UNAVAILABLE vs FAILED_PRECONDITION (which
determines retryability). Concrete bite: `state.PutResponse.committed=false` is
indistinguishable between "CAS conflict (reread+retry)", "backend down (backoff)", and "denied
(don't retry)" — and the reconciler's lease is built on that Put (reinforced by C-4).
**Note:** v2's own `api-spec.md` already uses a structured enumerated error code; v3's
bool+free-string is a *regression* from the predecessor's learned design.
**Fix (bounded, NOT "add an Error everywhere"):** (a) transport failures → mandate gRPC
canonical-status mapping + conformance vectors; (b) **decision RPCs** (`identity.Authorize`,
`tenancy.Decide`) → an **enumerated deny-code** on the `allowed:bool` path (a deny there is a
*successful* RPC, so transport codes structurally can't reach it — this is the irreducible
core); (c) free-text reason → **log/audit-only, never returned** to untrusted callers
(anti-enumeration-oracle); (d) **`secret.Resolve.found` → also means exists-but-unauthorized**
(collapse denial into not-found for sensitive lookups). The same enum should populate the
audit record's currently-missing machine-readable cause.
**Freeze:** a/b/c + `VendCredentials` = GA (additive fields, behavioral). **TWO freeze-blockers**
(both confirmed by systems-architect's reconciler read): **API-1d** (`secret.Resolve.found` also
means exists-but-unauthorized — pins an existing field), and **`state.Put` outcome tri-state**
(freeze-blocker #13). systems-architect: idempotent retry does NOT paper over the latter —
"lost CAS race" (ABORTED → step down) vs "backend unavailable" (UNAVAILABLE → backoff) demand
opposite lease behaviors, and the killer is the *ambiguous* `committed=false` on a
timeout/partition (write may-or-may-not have landed; lease fencing needs a third UNKNOWN state).
`committed:bool` has no UNKNOWN → giving `PutResponse` an explicit outcome enum
(COMMITTED/CONFLICT/UNKNOWN) reinterprets the existing field. The error-handling *convention*
choice should also be pinned at freeze so authors build one model.

### C-6 · No wire handle to invoke a required capability ("call-by-capability" unexpressible) — ⚠️ OPEN DECISION
**AUTH-2 ⊕ ARCH-2** · plugin-author + systems-architect · **CRITICAL** · **decision is freeze-blocking; chosen impl is additive**
**Files:** `strategy/v1/strategy.proto`, `engine/v1/engine.proto`, `common/v1/context.proto`, (missing) core API-gateway proto

`strategy.Apply`'s comment says providers are "wired in via the RequestContext + registry
resolution," but neither `RequestContext` nor `ApplyRequest` carries any provider
identity/endpoint/token. A strategy that `requires` `format-capability/merge` + `runtime/execute`
has, at `Apply` time, no endpoint and no auth handle to call them. The cleanest idea in the
design — call-by-capability — is literally not expressible on the wire. Same gap for
`engine`→`storage`/`catalog`.

**The two owning lenses disagree on the fix — this is an unresolved design decision, not a
settled recommendation.** (An earlier draft of this report wrongly recorded direct-dial as team
consensus; that was before systems-architect's ballot was received — see appendix.)

- **systems-architect → CORE-MEDIATED.** Each capability *call* is proxied through the core API
  gateway (caller sends capability URI + payload; core resolves the provider via the registry,
  checks capability/identity/tenancy, emits audit, stamps trace, proxies). The strategy still
  *orchestrates the sequence* — "core never commands" is preserved; the core is a switchboard,
  not an imperative orchestrator. Rationale: the six cross-cutting enforcement properties
  (C1/C2/C3/C5/C7/C8) can only be enforced if the core sits on the control-call path; direct-dial
  forces every plugin pair to re-implement all six (exactly what `plugin-architecture.md`
  forbids). The missing artifact is a **core-facing capability-invoke proto** — the API gateway's
  own contract is absent from all 20 files. Bytes still bypass the core via `ArrowStream`.
- **plugin-author → DIRECT-DIAL with core-issued scoped tokens.** At resolve time the core hands
  the requirer `{endpoint, short-TTL token scoped to the capability URI}`; the plugin
  direct-dials; the callee enforces C5 by validating token scope. Rationale: proxy makes the
  6-thing core a per-call SPOF and forces generic-payload forwarding (core-surface bloat);
  direct-dial mirrors the existing `storage.VendCredentials` pattern and preserves "bypass core
  for work."

**Freeze:** the **decision is freeze-blocking** (mediated vs direct-dial determines whether
`RequestContext`/requests must carry `{endpoint, token}` — an existing-shape question). Whichever
is chosen, the *implementation* is then additive (a new core-invoke proto, or a `ResolvedProviders`
field). The per-hop identity rule from the keystone (C-1) holds under either. **Recommendation:
resolve via a dedicated ADR before freeze.**

---

## Important findings

| ID | Finding | Lenses | Sev · Freeze |
|----|---------|--------|--------------|
| I-1 | **Arrow side channel under-specified + bypasses all core authz** (AUTH-3⊕SEC-14): "Flight-style" not pinned; host-vs-dial role unencoded; `ticket` has no TTL/single-use/binding; the byte leg carries no `RequestContext` (no trace, no authz, no audit) | author, sec | IMPORTANT · **FREEZE** (protocol pin + role field) / GA (ticket-auth) |
| I-2 | **State key/prefix is a string-concat convention, not a typed boundary** (SEC-2): no charset/length/traversal constraint; namespace not structurally separated from client key | sec | IMPORTANT · **FREEZE** |
| I-3 | **Capability granularity finer than RPC method** (AUTH-8): `format` provides scan/merge/append as 3 capabilities but merge+append are one `Write` RPC (enum-keyed) → C5 can't enforce at method level; `WRITE_MODE_OVERWRITE` has no capability URI; `storage` read/write map to no RPC | author, api | IMPORTANT · **FREEZE** (split RPCs) |
| I-4 | **Capability↔method binding lives only in comments** (AUTH-9): C5 gateway + C6 conformance both need machine-readable capability→(service,method); add a `(rat.capability)` method option | api, author | IMPORTANT · **FREEZE-coupled** (annotations.proto in frozen surface) |
| I-5 | **`observability.Ingest` wrong streaming shape** (API-4): client-streaming acks once at stream close → no backpressure/partial-failure feedback for a lifetime telemetry stream | api | IMPORTANT · **FREEZE** |
| I-6 | **Timestamps int64-ms vs `google.protobuf.Timestamp`** (API-10): pervasive + consistent, so a deliberate-choice call — but the type is wire-breaking to change, so ratify now | api | IMPORTANT · **FREEZE** |
| I-7 | **Config delivery + `options` encoding** (AUTH-5⊕API-12): only delivery channel is `LaunchSpec.env`; no Configure/ValidateConfig; `strategy.Apply.options` is bytes with no stated encoding | author, api | IMPORTANT · **FREEZE** (options encoding) / GA (config RPC) |
| I-8 | **Manifest has no artifact/image ref; signature binds to nothing** (AUTH-14⊕SEC-15): `trust.signature` signs "the image digest" the manifest never declares; `additionalProperties:false` even blocks adding one; `provides`/`requires` (the authz basis) aren't signed | author, api, sec | IMPORTANT · GA (additive schema) + freeze rider (what the signature covers) |
| I-9 | **Vended storage creds: scope unverifiable + unbound** (SEC-3): `VendCredentials` returns opaque bytes+expiry; response can't echo granted {prefix,mode,tenant}; creds not bound to caller → replayable | sec | IMPORTANT · GA |
| I-10 | **Secret `Resolve` has no per-plugin authorization** (SEC-4): any authenticated caller can resolve any `secret_ref`; sole gate is the forgeable `tenant` | sec | IMPORTANT · GA |
| I-11 | **IsolationProfile is advisory booleans, no attestation** (SEC-6): enforcer is the third-party deployment-runtime; `LaunchResponse` doesn't attest what was applied; egress model too coarse | sec, arch | IMPORTANT · GA |
| I-12 | **Sink free-fields are a secret/PII exfil channel** (SEC-7): `notifications.Send.body`, `observability` log attributes, `billing` dimensions are free strings to external systems; core can't redact what it can't recognize | sec | IMPORTANT · GA |
| I-13 | **Plugin→core auth (C2) is comments-only** (SEC-10): no registration RPC, no token-issuance/channel-binding field anywhere; the whole authn mechanism is assumed to live outside the frozen contract | sec | IMPORTANT · GA (ties to I-15 + keystone) |
| I-14 | **`Watch` lacks compaction/bookmark/cancel frames** (API-2⊕ARCH-6): `from_revision` resumes, but no "revision compacted → re-List" signal, no progress bookmark, no created/cancelled control frames (etcd-class gap) | api, arch | IMPORTANT · GA (additive frames) |
| I-15 | **No registration/handshake RPC** (AUTH-4): identity.proto promises a "per-plugin token at registration," but no `Register` exists — how does a plugin obtain its token? | author | IMPORTANT · GA |
| I-16 | **No app-level readiness/version RPC** (AUTH-7⊕ARCH-7⊕API-9): only `deploymentruntime.Healthcheck` (process liveness); can't ask a plugin "ready to serve capability X?"; liveness conflated with readiness | author, arch, api | IMPORTANT · GA |
| I-17 | **No idempotency key on mutating RPCs** (AUTH-13): `Write`/`Apply`/`Execute`/`MergeBranch` retried by the reconciler with no dedupe key | author | IMPORTANT · GA |
| I-18 | **catalog↔format commit linkage undefined; `MergeBranch` not idempotent** (ARCH-4): nothing registers what `format.Write` wrote into the catalog's branch; `MergeBranch` takes only names (no expected snapshots) → retried merge double-applies, concurrent merges lose updates. (NB: half-commit *on a branch* is by design — the merge is the gate.) **Split:** the `MergeBranch` request-shape change (`expected_snapshot`+`idempotency_key`) is **freeze-blocker #15**; the commit-linkage RPC is additive/GA | arch | IMPORTANT · split (see #15) |
| I-19 | **Out-of-band ArrowStream has no lifecycle/cancellation/EOS-error** (ARCH-5): bulk transfer happens after the descriptor RPC returns, so the call's deadline doesn't govern it; no clean-EOS vs truncation signal, no flow control | arch | IMPORTANT · GA (overlaps I-1) |
| I-20 | **Launched-instance lifecycle has no lease/owner token** (ARCH-9): nothing binds an instance's lifetime to the core that launched it → core crash between Launch and persisting instance_id = orphan, no GC | arch | IMPORTANT · GA |
| I-21 | **catalog/format ref-resolution division unclear** (AUTH-11): if `Resolve` gets only `identifier`, must the format call the catalog (it can't — C-6), or is `uri` pre-resolved? Unspecified | author | IMPORTANT · GA (CONTRACT.md) |
| I-22 | **No pagination** (API-3⊕ARCH-11): `state.List`, `marketplace.Search` return unbounded `repeated`; `state.List` is the tier-0 reconciler risk | api, arch | IMPORTANT · GA + freeze-sliver (default page_size) |
| I-23 | **`scheduler.WatchDue` no delivery guarantee** (API-11): fired trigger streamed with no ack/lease → lost if reconciler crashes post-delivery | api | IMPORTANT · GA + freeze-sliver (delivery-semantics doc) |

---

## Nice-to-have

- **API-6⊕ARCH-5b** `runtime.Execute` terminal/stream-end semantics undefined (success vs failure on bare stream end) — NICE · GA doc.
- **API-8** generic message-name collisions across packages (`ExecuteResponse` in engine=WriteResult vs runtime=streaming oneof) — NICE (conceded down: package namespacing means no real codegen collision; semantic-clarity hazard only).
- **API-12** `options` bytes content-type — NICE · FREEZE-sensitive (pin encoding).
- **API-13** sentinel values (`-1`=unknown) → proto3 `optional` presence — NICE · FREEZE-sensitive.
- **API-14** no reserved field/enum ranges anywhere — NICE · GA (cheap insurance now).
- **API-15** no authz-decision cache TTL on `Authorize`/`Decide` — NICE · GA.
- **API-17** `contributes.slots[].target` bare URI vs `capabilityRef` object elsewhere — NICE · **FREEZE-BLOCKER** (string↔object; wrap now).
- **SEC-8** secret/credential bytes lack `[debug_redact=true]` — NICE · GA (trivial, do now).
- **SEC-12/16** trace fields propagated verbatim = log-injection vector; marketplace `signed`/`signed_by` are self-asserted → must re-verify at install — NICE.
- **AUTH-10** no within-major capability negotiation (URI carries major only) — NICE · GA (by-design per ADR-002 D4; note only).
- **ARCH-10** `state` has no `Delete` RPC yet `WatchResponse` emits `TYPE_DELETE` — tombstone semantics unspecified — NICE.
- **ARCH-12** `observability.Ingest` single end-ack loses unknown suffix on sever — NICE (overlaps I-5).

---

## What's already right

The protos got real things correct, and the review credits them:
- **Trace context on every *sync* RPC.** `RequestContext` is threaded through all 18 axis
  services consistently — the prior review's #1 complaint (no trace context) is genuinely
  addressed on the sync path.
- **Correct streaming choices** for `runtime.Execute` and `state.Watch` (server-streaming);
  the deadline correctly defers to gRPC as authoritative (`deadline_unix_ms` is an explicit hint).
- **Timestamp representation is at least *consistent*** (int64 unix-ms everywhere) — the issue
  is ratifying it, not cleaning up a mess.
- **Capability-not-implementation negotiation** is the right model and is mostly expressible
  (`provides`/`requires` by capability URI) — the gap is the *invocation* handle (C-6), not the
  negotiation concept.
- **The data-plane "control carries refs, bytes go out-of-band"** split is sound; the gaps are
  in specifying the side channel, not the architecture.
- **Tier-0 / Critical-concern intent is visibly present** — the comments show the authors knew
  about C1/C2/C3/C5/C7/I8/I9/I13; the work remaining is making the *fields* enforce what the
  *comments* promise.

---

## Recommended next actions

**Before freeze (the 15 freeze-blockers + the AUTH-2 open decision + slivers above), in dependency order:**
1. **Rewrite `common/v1/context.proto`** for the three-principal keystone (C-1) — this unblocks
   C3, C7, secret/audit/billing attribution, and the C-6 invocation identity. Everything keys
   off it; do it first.
2. **Rename the format capability URIs** (C-2) — trivial edit, but baked into manifests, so freeze-gated.
3. **Pin `state` key grammar + linearizable-CAS conformance + backend set** (I-2 + C-4) together.
4. **Redo `auditlog` AppendResponse + canonical serialization** (C-3).
5. **Add `common/v1/annotations.proto` + `(rat.capability)` options + split `Write`/`Execute` per-mode** (I-4 + I-3) — do together; the annotation must reflect the final method set.
6. **Decide error-handling convention + `secret.Resolve.found` semantics** (C-5 / API-1d).
7. **Pin `ArrowStream` protocol + role field** (I-1), **`Ingest` shape** (I-5), **timestamp type** (I-6), **`slots.target` wrap** (API-17), and the freeze-slivers (options encoding, pagination default, scheduler delivery doc, optional-presence).
8. **Add the cheap additive placeholders now** (audit signature field, manifest `image` digest, `debug_redact`) even though enforcement is GA — they're free insurance and avoid a later schema bump.

Also resolve, in the same pre-freeze pass:
- **AUTH-2 invocation model (C-6)** — pick mediated vs direct-dial via an ADR; the decision is
  freeze-blocking even though the implementation is then additive.
- **`state.Put` outcome tri-state** (#13) and **`MergeBranch` request shape** (#15) — both extend
  existing messages.
- **Async event-bus envelope** (#14, ARCH-1) — add `common/v1/event.proto` (trace + correlation +
  tenant + event_id + dedup/ordering) **or** explicitly carve the async plane out of the `rat/1`
  freeze. systems-architect's position (adopted): you cannot freeze "the wire contract" while the
  backbone async/reconciliation comms model is unspecified — so this is freeze-blocking *by scope*,
  upgraded from the first draft's GA reading.

**Defer to GA (additive + enforcement):** registration RPC (I-15), readiness RPC (I-16),
idempotency keys (I-17), watch control frames (I-14), cred/secret scope echoes (I-9/I-10),
isolation attestation (I-11), sink-exfil controls (I-12), catalog/format commit-linkage RPC
(I-18, distinct from the freeze-gated `MergeBranch` request-shape change), instance lease (I-20),
and the chosen invocation model's wire fields (C-6, once the model is decided).

**Process note for the freeze itself:** per ADR-003, none of this freezes on paper — the
two-reference-implementation rule still applies. This review is the *paper* pass; the second
forcing function is building real plugins against the revised contracts (sub-phase 0d). Several
findings here (esp. C-6 invocation, I-1 Arrow, I-18 catalog/format) will only be *fully*
validated when two reference plugins actually exchange data.

---

## Appendix — review method

- **4 lenses, cold:** reviewers were given only the protos + minimal platform context, not the
  prior architecture reviews' findings, so convergence is independent signal.
- **Cross-challenge:** every reviewer challenged the others' findings; this *moved* results —
  API-8 was conceded down (no real codegen collision), API-1 was sharpened upward (deny-reason
  as enumeration oracle), and the keystone's confused-deputy / per-hop-`correlation_id`
  refinement only emerged *from* the converged fix.
- **Integrity:** systems-architect discarded 4 of its own initial findings (including a false
  "doesn't compile") that didn't survive re-reading the actual files.
- **Severity + freeze-gate decided collectively**, not stamped by the raising reviewer.
- **Outstanding:** systems-architect's formal final-ballot sign-off on AUTH-2's invocation
  model and the API-1 reconciler axis was not received before writeup; both were resolved by
  team consensus (its substantive ARCH-1..12 findings are all incorporated), so this report
  records those two as team-resolved / provisional-on-sysarch-confirm.
