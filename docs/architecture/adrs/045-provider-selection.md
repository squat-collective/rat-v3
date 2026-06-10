# ADR-045: Provider selection — N coexisting providers of a capability, chosen by label

## Status: Accepted (2026-06-10, rev. 3)

> Decision-first artifact. This ADR changes a founding registry invariant, so it is written and
> reviewed **before** any code (CLAUDE.md #3, ADR rules). Rev. 2 reshapes it around Tom's requirement:
> **two distinct providers of one capability must coexist, and a flow picks which compute to use** —
> built on a label/selector primitive so new selection dimensions cost no core or contract change
> ([[prefer-extensible-primitives]]).

## Context

The code-level review of `core/` found **gap #3**: the registry refuses two plugins that provide the
same capability.

```go
// core/registry/registry.go:62-65
for _, capURI := range m.ProvidesCaps() {
    if other, dup := r.providerOf[capURI]; dup {
        return fmt.Errorf("capability %q provided by both %q and %q "+
            "(no provider-selection policy in the spike)", capURI, other, m.Metadata.Name)
    }
}
```

`providerOf` maps `capability → one plugin name`, and the whole routing path assumes exactly one
provider per capability. The refusal is a deliberate spike simplification, but the driving requirement
is concrete (Tom, 2026-06-10):

> *"We need sometimes 2 of the same service with the same cap, to use different computes depending on
> the flow."*

E.g. `engine-duckdb` (small/local) and `engine-spark` (big/cluster) **both** provide
`rat://engine/v1/execute`, **coexisting**, and a given flow runs on the compute it needs — light
transforms on duckdb, the heavy nightly job on spark. The single-provider registry makes this
impossible: it rejects the plane at load.

### The distinction that organizes everything: eligibility vs selection

- **Eligibility** — *who may serve a capability.* Capability negotiation: a plugin is eligible iff it
  `provides` the capability and the caller `requires` it. Unchanged; the contract triple working as
  designed.
- **Selection** — *which eligible provider serves this call.* Today this layer does not exist — the
  registry collapses it by refusing all but one provider. This ADR gives it a home.

The founding invariant constrains where it can live
([`plugin-architecture.md`](../../../.claude/rules/plugin-architecture.md)): **callers couple to
capabilities, never to peer plugin names.** So a flow must not say "call `engine-spark`." It expresses
*intent* — "I need the big-compute `engine/v1/execute`" — and the platform maps intent → provider.
Exactly as a Kubernetes Pod consumes a *Service* (a stable name) and the operator decides what backs
it, or as Istio routes to a *subset* by label.

### Three shapes (the registry refuses all three)

- **(C) Per-flow selection among non-interchangeable providers** — *the driver.* Distinct providers
  (different compute) coexist; the flow chooses by *class*, not name. `engine-duckdb` vs `engine-spark`.
- **(B) Disambiguation** — the operator pins one of several distinct providers plane-wide. A degenerate
  case of (C): the plane's default selection.
- **(A) Replication** — N interchangeable *instances of one plugin* behind a capability, load-balanced
  (replicas/failover/canary). The K8s Service→Endpoints model. The throughput/HA win, but a separate,
  larger change (needs replica counts in the desired set).

(C) is what Tom needs now; (B) falls out of it for free; (A) composes on top later.

## Decision (recommended)

**Replace the registry's single-provider map with a provider SET, give each provider LABELS, and route
each call to the provider whose labels satisfy a SELECTOR. The selector is resolved from a precedence
chain so it can be authored at any altitude — plane binding now, call-time / profiles later — with
zero core change. Selection is always deterministic and never caller-by-name.**

This makes **labels + selectors the primitive**, because it is the open-set extension point: a new
selection dimension (`gpu=true`, `region=eu`, `cost=cheap`, `latency=low`) is a new *label*, never a
core or wire change ([[prefer-extensible-primitives]]).

### 1. Registry holds a labeled set; eligibility unchanged

`providerOf` becomes `map[capability] → []provider{name, labels}`. `Authorize` still answers
eligibility (caller `requires` ∧ some plugin `provides`) and now returns the eligible *set*; it no
longer fails on duplicate providers. The unique-plugin-name invariant stays.

### 2. Labels: manifest self-describes, plane overrides

A plugin declares descriptive labels in its manifest (`metadata.labels: {compute: big, gpu: "true"}`)
— honest self-*description* of what it IS (distinct from self-*ranking*, which we reject). The plane
may add/override labels. So a new provider ships usable defaults; the operator tunes only when needed.
(This is the more adaptable source — see Q01.)

### 3. Selection = a selector resolved by a precedence chain

A call is routed to a provider whose labels match a **selector**. The selector is resolved, highest
precedence first:

1. **call-time selector** (the flow asks, e.g. `select: {compute: big}`) — additive, *later*;
2. **plane / per-flow binding** (the operator binds a pipeline/stage to a selector or provider) — **v1**;
3. **plane default** for the capability;
4. **single eligible provider** → it is selected with no selector at all (the zero-config common case).

If, after resolution, the matching set is **empty or ambiguous (>1 non-interchangeable match)**,
**fail closed** with a precise error (Q01). Among *interchangeable instances* (mode A), the gateway
load-balances (v2).

### 4. v1 authoring surface: plane per-flow binding (on the label engine)

v1 ships the **plane per-flow binding** (Tom's pick): the plane / pipeline config binds a capability to
a selector (or directly to a provider) per pipeline or stage. The flow code stays compute-agnostic.
Critically it is built *on the label primitive*, so call-time selectors and named profiles are purely
additive afterward — same matching engine, just authored elsewhere.

```yaml
# plane: two engines coexist, labeled; selection is per-flow desired-state
providers:
  - plugin: engine-duckdb          # manifest labels {compute: small}
  - plugin: engine-spark           # manifest labels {compute: big}
pipelines:
  nightly-heavy:
    select: { rat://engine/v1/execute: {compute: big} }    # → engine-spark
  dev-light:
    select: { rat://engine/v1/execute: {compute: small} }  # → engine-duckdb
```

### 5. The gateway routes to a matching, HEALTHY provider

`openCall` resolves the selector, filters the eligible set by labels, and routes only to a provider the
reconciler reports `Healthy` (the gap-#4 snapshot). Selection happens *after* the C5 authorization
decision; the audit record's `Provider` is the selected one.

### 6. The selector model is open-ended (multi-criteria)

`compute` is just the first label — the engine knows nothing about compute. A provider carries an
**open set** of labels and a selector is a set of key=value pairs matched against them, so **any**
dimension is a new label, never a core/contract change:

```yaml
- plugin: engine-spark-gpu   labels: {compute: big, gpu: "true", region: eu, cost: high}
- plugin: engine-duckdb      labels: {compute: small, region: eu, cost: low}

nightly-heavy: { select: { rat://engine/v1/execute: {compute: big, region: eu} } }   # AND across keys
dev-cheap:     { select: { rat://engine/v1/execute: {cost: low} } }
```

- **v1 match semantics:** exact-equality AND across every key in the selector (a provider matches iff
  it carries every selector key with the exact value). Empty selector matches all. An over-constrained
  selector matching zero providers **fails closed** (Q01).
- **v1.5+ (additive, same engine, no core change):** richer operators — `region in [eu, us]`,
  `gpu != true`, set membership, and **preference/fallback** ("prefer `big`, else `small`"). Flagged
  here so the door is explicit; deferred until a real need lands.

**Carriage (no wire change).** The resolved selector travels on the call as a `rat-select`
transport-metadata header (`k=v,k=v`), exactly as `rat-plugin-token` does (ADR-042) — additive, the
frozen `InvokeRequest` is untouched. The gateway reads it, matches against the registry's provider
labels, and routes. WHERE the value is authored is the precedence chain: **v1** the operator sets it
via the driver/pipeline config (so flow *code* stays compute-agnostic — the plane authored it);
**v1.5** the flow attaches it per call. Same header, same matching engine, different author.

### 7. Not a 7th core thing

The **registry** (#1) indexes a labeled set, the **API gateway** (#6) matches a selector at route time,
the **reconciler** (#5) supplies health; policy is operator desired-state (the plane). The existing
things doing a richer version of their jobs — same shape as ADR-027/036. Count stays six; logged in the
temptation ledger as *examined, not a temptation*.

## Staging

| Stage | Scope | Unblocks |
|---|---|---|
| **v1 (this ADR)** | registry labeled SET · manifest+plane labels · **plane per-flow binding** over the label engine · fail-closed on empty/ambiguous · gateway routes to the matching healthy provider | **Tom's case:** two engines coexist, each flow runs on its compute (mode C); disambiguation (B) for free |
| **v1.5 (additive, no core change)** | **call-time selector** + **named profiles** — just new places the selector is authored over the same engine | dynamic per-run compute choice; profile sugar |
| **v2 (follow-on ADR)** | `replicas: N` in the desired set · load-balance across interchangeable instances matching the selector | replicas, failover, throughput (mode A) |
| **v3 (follow-on ADR)** | weighted / canary policies | progressive rollout |

The label primitive is the reason v1.5/v2/v3 cost no contract change — they add labels, authoring sites,
and policies over the same matching engine.

## Consequences

### Positive

- **Tom's requirement is met in v1, extensibly.** Two computes coexist and flows select per-flow; new
  selection dimensions are new labels, forever, with no core/wire change.
- **Capability-coupling preserved.** Flows/planes express intent over labels; never a caller→plugin
  name edge. No regression of plugin-architecture.md.
- **No wire / contract-triple change.** Labels live in the manifest (`metadata.labels`, additive) +
  the plane; selection is registry + gateway behavior. Frozen protos untouched. (Manifest label
  parsing is the one new authored field.)
- **Composes with the other fixes.** Health-aware routing reuses the gap-#4 reconciler snapshot; v2
  replicas reuse the same selector → matching-set path.
- **One primitive, many altitudes.** Plane binding now; call-time + profiles later are additive, so we
  never repaint the engine to add a new way to choose.

### Negative / costs

- **The plane gains a concept (labels + `select`).** More to learn — but only when running >1 provider
  of a capability; single-provider planes stay zero-config.
- **Selection is a new failure surface.** A wrong selector routes a flow to the wrong compute; the
  fail-closed default + a clear plane-load/route error mitigate, but it is real new operator rope.
- **A per-call matching step in the gateway hot path.** Label matching is O(eligible providers) per
  call — tiny, but more than today's single-map lookup; design it allocation-free.
- **v1 is selection, not load-balancing.** Two *distinct* computes chosen per flow — yes. N
  interchangeable *replicas* of one, balanced — that's v2. Naming it so v1 isn't mistaken for HA.

## Decisions settled

- **Q01 — RESOLVED (rev. 3):** labels come from **manifest self-description + plane override** (a new
  provider ships usable; the operator tunes only when needed). Selection **fails closed** when a
  resolved selector matches zero or >1 *distinct* providers (deterministic; matches the spike's
  safety — the operator refines the selector). *Rejected:* plane-only labels (forces per-deployment
  hand-labeling — the opposite of adapt-quickly); a manifest `priority` auto-tiebreak as default (a
  plugin shouldn't self-rank — may return as an opt-in operator tiebreak later).
- **Q02 — RESOLVED (rev. 2):** selector authored as **plane / driver-config binding** for v1, over the
  label primitive; call-time selector + profiles are additive (v1.5).
- **Q03 — RESOLVED (rev. 2):** replicas/load-balancing (mode A) is **v2**, on its own ADR.
- **Q04 — (v2) policies.** round-robin first; weighted/canary in v3. Deferred until v2.

## Alternatives considered

- **Keep the registry strict (status quo).** Rejected: it is the gap — Tom's two-compute case is
  impossible.
- **Caller selects by plugin name (`provider:` in `requires`/the call).** Rejected outright:
  reintroduces caller→plugin-name coupling, violating the founding invariant. Intent over labels, never
  a name.
- **A fixed enum of compute classes in the contract.** Rejected: every new dimension (gpu, region,
  cost) would reopen the contract. Labels are the open-set primitive precisely to avoid this
  ([[prefer-extensible-primitives]]).
- **Plugins self-rank (OSGi service ranking), registry auto-picks highest.** Rejected as primary: puts
  selection authority in the plugin, which can declare itself preferred — an operator decision. May
  return as an opt-in tiebreak only (Q01).
- **Round-robin across all eligible providers by default.** Rejected as default: silently balances
  across *distinct, non-interchangeable* computes (duckdb vs spark have different semantics/cost) — a
  correctness footgun. Balancing is only safe across *interchangeable instances* (mode A, v2, opt-in).
- **Do replicas + LB + selection at once.** Rejected: too much core surface (registry + gateway
  matching + reconciler replicas + health-aware LB) for one reviewable step; staging lets v1 unblock
  Tom's case safely while v2 takes the reconciler change deliberately.

## Migration

- v1 is backward compatible: existing single-provider planes are unaffected (no labels/`select`
  needed). The only behavior change is that a plane with two providers of one capability — which
  **errors today** — now resolves via labels + a per-flow binding, or errors with a *better* message
  telling the operator to add a selector. No existing valid plane breaks.
- `providerOf` changes single → labeled set; contained to the registry package + the gateway's
  resolution call. Covered by existing registry/gateway tests plus new label/selection/ambiguity cases.
- `metadata.labels` is an additive, optional manifest field (no schema break; absent == no labels).

## Related

- [`core/registry/registry.go:62-65`](../../../core/registry/registry.go) — the refusal this lifts.
- [`plugin-architecture.md`](../../../.claude/rules/plugin-architecture.md) — capability-not-name coupling (the constraint on where selection lives).
- [[prefer-extensible-primitives]] — the steer behind choosing labels/selectors as the primitive.
- backlog **P-1** — eligibility vs selection / the plane binding desired-state language; this ADR resolves it.
- ADR-027 / ADR-036 — prior "existing core things doing more, not a 7th thing" precedents.
- ADR-044 — the reconciler health snapshot the gateway's health-aware selection reuses.
- Prior art: Kubernetes labels/selectors + Services/Endpoints; Istio DestinationRule subset routing; DNS SRV weights; OSGi service ranking.
