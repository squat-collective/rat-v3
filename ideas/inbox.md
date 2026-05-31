# Ideas inbox

Append new ideas at the bottom. Format: `## YYYY-MM-DD — [tags] one-line title` + a few sentences.

---

## 2026-05-30 — [naming] Project codename

`RAT v3` is the working title since the folder is `rat/`. Possible future renames if we want a distinct identity:
- **Pith** (the central core of a stem)
- **Atrium** (the central courtyard everything flows through)
- **Conduit**
- **Sieve**
- **Burrow** (where RATs live — ties to brand)

Decision can wait until we ship. The internal codename matters less than the eventual product name.

---

## 2026-05-30 — [marketplace, distribution] Plugin distribution as a first-class concern

VSCode's marketplace + Cargo's crates.io are why those ecosystems flourished. RAT v3 needs a plugin marketplace from year 1, not as an afterthought. Options:
- GitHub-based: discover via topic tag `rat-plugin`, install via `gh` CLI.
- Dedicated registry: like crates.io, hosted by the project.
- Multi-source: operator points at N registries (one curated, one community, one internal).

Open question: should the marketplace ALSO be a plugin? (Yes, almost certainly. `kind: marketplace`.)

---

## 2026-05-30 — [contracts, schemas] Generate manifest schema from proto?

If proto files define the gRPC service shapes, we could generate the `plugin.yaml` manifest schemas from them. Reduces drift between "what a plugin says it provides" and "what the proto actually defines."

Tradeoff: protobuf and JSON Schema have different expressiveness. Generation works for simple cases; complex constraints (cross-field validation) might not transfer. Worth a spike.

Related: Q03 in [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md).

---

## 2026-05-30 — [event-bus, performance] Event bus failure modes

If the event bus is the coordination substrate, what happens when it's degraded?
- Stale notifications → reconciler converges slowly but eventually
- Lost events → reconciler needs idempotent retries (every loop iteration)
- Out-of-order events → reconciler must be reorder-tolerant

This argues for the reconciler being **the source of truth, not the events**. Events are hints for "you might want to look now"; the reconciler always re-reads state. Same model as K8s.

Future ADR: event-bus durability semantics + reconciler-as-source-of-truth.

---

## 2026-05-30 — [security] Plugin sandboxing

A 3rd-party `rat-plugin-foo` runs *somewhere* in the operator's environment. Trust model:
- Solo: same process as core; full trust. Container model overkill.
- Team: containerized; trust at the network level.
- Enterprise: signed images + capability whitelist + network policy.

Each level is a different `deployment-runtime` plugin doing different isolation. The core doesn't enforce sandboxing — the deployment-runtime does. Worth an ADR when we start implementing the runtime axis.

Related: v2's ADR-017 (Python pipeline trust model) — same pattern.

---

## 2026-05-30 — [migration] Bridge plugins from v2 to v3

**Resolved in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D7**: no migration plan now; build a tool reactively if a real production user surfaces. v2 has no production users today, so pre-building optimizes for users who don't exist.

---

## 2026-05-30 — [meta-principle, language-choice] AI-assisted rewriting lowers language-choice stakes

Tom's reasoning during Q01: *"let's go with Go we could rewrite it with AI if we want to go rust someday"*. This is a load-bearing meta-principle worth banking. When picking foundational tech (language, framework, etc.), the cost of "wrong choice" has shifted dramatically — AI-assisted refactoring of a 10k-LOC codebase is genuinely viable. So: bias toward velocity-friendly + ecosystem-aligned choices NOW; accept that re-language is a 2-4 week project later if needed. **Don't over-optimize for "perfect long-term language."** This applies recursively to framework choices, ORM choices, serialization choices, etc.

Save as principle for the project; cite in future ADRs when stuck on "this technology choice is hard."

---

## 2026-05-30 — [v2, strategy] Should v2 keep shipping?

**Q11 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** Implication of D7: if v2 has no production users and v3 is the real target, should we still implement v2's ADR-025 (on-demand planes) and ADR-026 (manifest+registry)? Those ADRs were valuable as *thinking* — they shaped v3's design — but actually building them in v2 might be wasted effort. Worth a separate decision when there's bandwidth.

Open question: when do we declare v2 feature-frozen?

---

## 2026-05-30 — [bundles] Default `rat-bundle-solo` composition

**Q12 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** What exact plugin set ships in the default solo bundle? Probably:
- state-backend: sqlite
- secret-backend: env
- scheduler-backend: in-process
- identity: anonymous
- deployment-runtime: local-process
- ui: web-portal
- engine: duckdb-embedded
- runtime: pyarrow-embedded
- format: iceberg-embedded
- catalog: sqlite-iceberg-catalog (or simpler — file-based catalog?)
- storage: local-fs
- observability: stdout
- marketplace: community-marketplace

But each is a real choice. Becomes ADR-003 (or similar) when Phase 0 lands. Versions matter — bundle pins specific plugin versions for reproducibility.

---

## 2026-05-30 — [security] Plugin authentication to core

**Q13 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** When a plugin contacts the core (or core contacts a plugin), what auth model? Options:
- Mutual TLS (cluster-style)
- Bearer tokens (simple but rotation matters)
- Both (mTLS for production, bearer for dev)
- None for solo (in-process), upgrade for team+

Probably the last — auth model varies by deployment-runtime. Future ADR when core API hardens.

---

## 2026-05-30 — [marketplace, UX] Marketplace plugin's discovery shape

**Q14 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** The marketplace plugin needs a UX: search by capability, by name, by author? Trust badges? Reviews? Compatibility checking (does this plugin work on my deployment)?

Worth a dedicated ADR when the marketplace plugin is being built. Look at: VSCode marketplace UX, Cargo's crates.io, Helm Hub, OperatorHub.io for patterns.

---

## 2026-05-31 — [contract, ADR-005] Where does re-stamped identity ride: payload.context or channel metadata? `[promoted → docs/architecture/adrs/007-call-context-transport.md]`

> **Resolved by [ADR-007](../docs/architecture/adrs/007-call-context-transport.md):** the whole cross-cutting envelope (trace + identity) moves out of the payload into a `rat-callmeta-bin` transport-metadata header — option (a), refined. This upholds ADR-005's generic-proxy guarantee (the gateway parses zero payload bytes) and keeps the keystone `RequestContext` shape verbatim, only changing its carrier. The reasoning below is kept as the historical record.

**Surfaced by building the 0d stub invoke-gateway** (`examples/format/inmemory-go/gateway_test.go`) — exactly the kind of gap ADR-003 predicts a real implementation exposes.

ADR-005 / `core/v1/invoke.proto` says the gateway is a **generic proxy**: it routes by capability and forwards `payload` **without interpreting it**. But two clauses collide:
1. The gateway must **re-derive `identity.caller_plugin`** for the downstream hop and **never trust wire-supplied identity** (keystone, `context.proto`).
2. Every axis request carries `RequestContext` (incl. `identity`) **inside the payload** (field 1).

A proxy that doesn't deserialize the payload **cannot rewrite the embedded `identity`**. So the re-stamped identity has to travel somewhere the proxy *can* set without parsing bytes — i.e. **gRPC metadata** on the downstream call — and the providing plugin would read identity from metadata, not from `payload.context.identity`. That contradicts "RequestContext travels as field 1 of every request" (`context.proto`).

Three resolutions to weigh (→ likely a follow-up ADR amending 005/context):
- **(a) Identity rides in channel metadata; payload.context.identity is advisory/ignored.** Keeps the proxy truly generic. Cost: the "every RPC carries identity in field 1" invariant weakens to "field-1 context carries trace + deadline; identity is in metadata."
- **(b) The gateway DOES splice field 1.** It interprets only the well-known `RequestContext context = 1` prefix (uniform across all axes by construction) and rewrites `identity`, forwarding the rest opaquely. Costs "forwards payload without interpreting it" purity, but only for one structurally-guaranteed field.
- **(c) Two-channel: trace in payload, identity wholly out-of-band (metadata + the signed `SubjectAssertion`).** Most faithful to "never trust wire identity," most plumbing.

The stub does **(a)** (stamps `x-rat-caller-plugin`/`x-rat-tenant` into outbound metadata) and the reference plugin ignores identity entirely, so behavior is correct under any choice — but the **frozen** contract must pick one before `format/v1` (and every axis) is GA. Freeze-blocking-adjacent: touches `context.proto` + `invoke.proto`, both in the `rat/1` surface.

Open question: which of (a)/(b)/(c) — pick before any axis freezes.
Related: [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md), `contracts/proto/rat/common/v1/context.proto`, `contracts/proto/rat/core/v1/invoke.proto`, [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (the rule that made this surface).

---

## 2026-05-31 — [contract, ADR-005] The core-mediated Invoke is unary-only — server-streaming capabilities have no mediation path

**Surfaced by building the 0d `runtime` reference** — the 0d forcing function (ADR-003) exposing a contract gap, like the ADR-007 identity-transport finding before it.

`runtime/v1`'s `Execute(ExecuteRequest) returns (stream ExecuteResponse)` is **server-streaming** (interim `ExecuteProgress` + terminal `ExecuteCompleted`). But ADR-005's `core/v1 CapabilityInvokeService.Invoke(InvokeRequest) returns (InvokeResponse)` is **unary** — it cannot carry a streamed response. So a strategy that `requires: rat://runtime/v1/execute` has **no core-mediated way to invoke it**: the gateway can route+enforce a unary call, not a stream. (Every other 0d axis so far — format/engine/storage — is unary and routes cleanly through the stub gateway; runtime had to be driven DIRECTLY, bypassing the gateway, which means its C2/C5/C7/C8 + traceparent seams are currently unenforced.)

This is freeze-relevant: `invoke.proto` is in the `rat/1` surface, and *any* axis with a streaming method (runtime today; future engine/observability streams) hits this.

Three resolutions to weigh (→ a candidate follow-up ADR, "streaming capability invocation"):
- **(a) Add `InvokeStream(InvokeRequest) returns (stream InvokeResponse)` to `invoke.proto`.** The gateway becomes a streaming generic byte-relay (same passthrough-codec trick, but it relays a stream of `result` frames). Enforcement (C2/C5/C7/C8 + traceparent) happens once at stream open, identity stamped into the downstream metadata as today. Cleanest + most consistent with the unary model; the gateway stays axis-generic. Cost: a second RPC in the frozen core surface + streaming relay plumbing.
- **(b) Streaming capabilities are direct-dial with a gateway-issued, capability-scoped token** (like the ArrowStream bulk-data leg, which already bypasses the core). The gateway mints a short-TTL token at a unary "open" call; the caller dials the provider's stream directly with it; the provider validates. Mirrors `storage.VendCredentials` / the bytes path. Cost: distributes enforcement to the callee (the exact honor-system ADR-005 rejected for control calls) — but maybe acceptable for the *streaming* subset since progress is liveness, not authz-bearing.
- **(c) Progress moves to the async event bus (`common/v1 Event`); `Execute` becomes unary** returning only the terminal result, with `ExecuteProgress` re-published as events keyed by `correlation_id`. Keeps the invoke contract unary. Cost: liveness loses request-scoped backpressure + becomes best-effort; couples runtime to the bus.

Leaning **(a)** — it preserves the central-enforcement property ADR-005 is built on and keeps the gateway generic; streaming is just the unary relay with N response frames. But it needs the ADR to weigh (b)'s perf argument for genuinely high-volume streams.

Open question: pick (a)/(b)/(c) before `runtime/v1` (or any streaming axis) routes through the gateway — and before `invoke.proto` freezes.
Related: [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md), [ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (same "0d reveals the gap" pattern), `contracts/proto/rat/core/v1/invoke.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `examples/runtime/inmemory-go/harness_test.go` (the direct-dial workaround + its header note).
