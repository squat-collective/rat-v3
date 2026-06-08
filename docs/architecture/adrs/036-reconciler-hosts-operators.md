# ADR-036: The reconciler hosts operators — generic resource reconciliation via operator plugins

## Status: Proposed (2026-06-04) — SKETCH. Owes the ADR-003 axis obligations + the temptation-ledger verdict below before Accepted.

## Context

The reconciler (core thing #5) today reconciles the **plugin set**: desired plugins in state →
launch/converge via the deployment-runtime. That is *one* kind of resource, hardcoded.

Declarative automation — the thing that makes v3 *"K8s for data"* — needs the reconciler to drive
convergence of **arbitrary domain resources**: a pipeline that should run on a schedule, a table
that should stay fresh within an SLA, a materialization that should exist. Crucially, the **domain
logic** (what a pipeline *is*, how to make a table fresh) **must not enter the core** — that would be
a 7th thing and would make the domain un-swappable (six-thing rule, [ADR-001](001-everything-is-a-plugin.md)).

Kubernetes solved exactly this: a generic API server + control loop; domain logic in **controllers
(operators)** that watch resources and reconcile them; `controller-runtime` as the generic framework.
The question this ADR answers — and the reason it exists — is the **temptation-ledger question**:
*can the reconciler host domain operators generically without becoming a 7th core thing?*

## Decision

Generalize the reconciler from *"reconciles the plugin set"* to a **generic operator-hosting control
loop**: it reconciles resources of **any kind**, dispatching each kind's convergence to the
**operator plugin** that owns it. The core gains loop *mechanics*, never domain *semantics*.

### 1. The operator contract — a new plugin axis (additive, NOT core)
`rat://operator/v1/reconcile`:
```proto
service OperatorService {
  rpc Reconcile(ReconcileRequest) returns (ReconcileResponse) {
    option (rat.common.v1.capability) = "rat://operator/v1/reconcile";
  }
}
message ReconcileRequest {
  string resource_kind = 2;  // e.g. "rat://pipeline/v1"
  string resource_key  = 3;  // a REF, not the object — the operator reads current state itself
}
message ReconcileResponse {
  int64 requeue_after_ms = 1;     // k8s reconcile.Result{RequeueAfter}; 0 = don't requeue
  ReconcileOutcome outcome = 2;   // OK / RETRY / FAILED
}
```
The operator reads desired+actual from the **state gateway** itself (as a k8s controller reads from
the API server — avoids stale-object races) and acts through the capabilities it `requires`
(`runtime`, `engine`, …). It returns a requeue hint.

### 2. Operators declare what they own
A manifest field **`reconciles: [<resource-kind URI>]`** (e.g. `rat://pipeline/v1`). The **registry**
indexes it; the reconciler resolves *"kind K → owning operator"* the same way the gateway resolves
capabilities. No name-coupling — kind ownership is declared, discovered, swappable.

### 3. The core reconciler stays generic — it owns ONLY the loop mechanics
- **Watch / trigger:** `state/v1/watch` (exists) on resource prefixes **+** event-bus events **+** a
  scheduler plugin's "due" markers all funnel into **one work queue** keyed by `(kind, key)`.
- **Dispatch:** for each item, resolve the owning operator (registry) → `Invoke`
  `rat://operator/v1/reconcile` (API gateway).
- **Requeue / backoff:** honor `requeue_after_ms`; exponential backoff + jitter on error (sre#4).
- **Leader election:** one reconciler leader across replicas via the **state-backend CAS lease**
  (Put/Delete CAS — [ADR-035](035-state-axis-delete.md), exists).
- **Status + audit:** write per-resource reconcile status to state (the IDE's reconciliation view
  reads it); a C8 audit record per reconcile.

It **never learns what a pipeline or a table is.**

### 4. The plugin-set loop becomes the built-in (tier-0) "deployment operator"
Today's launch-the-desired-plugins behavior is reframed as the **first, built-in operator**
(kind = plugin). It **must** stay built-in — bootstrap chicken-and-egg: *you cannot launch the
operator-that-launches-operators as a plugin*. Domain operators (pipeline, freshness, …) are
**plugins** the built-in operator launches, after which the reconciler hosts them. Exactly k8s:
core controllers built-in; custom controllers deployed as pods the core runs.

### 5. Six-thing check (the temptation-ledger verdict)
Generic operator hosting is the reconciler's **existing job — drive convergence — generalized** from
one kind (plugins) to many. It uses **only existing core pieces**: registry (resolve owner),
state-watch (trigger), event bus (trigger), API gateway (invoke), state CAS (leader lease). All
domain logic lives in **operator plugins**. **Verdict: NOT a 7th core thing; the count stays 6.**
(Chicken-and-egg also confirms the reconciler itself can't be a plugin — it's the loop the system
converges on, bootstrap-critical — while domain operators *are* plugins.) Log this examination in
`roadmap/done.md`'s temptation ledger.

## Consequences

**Positive.**
- Declarative automation (pipelines, freshness, materializations) with **zero new core
  responsibility** — the K8s-for-data model, honestly.
- **Uniformity:** everything converges through one loop; the plugin set is just kind #0.
- Operators are **plugins** → the domain is open-ended + swappable; the IDE observes a *generic*
  reconcile status across kinds.
- **Scales:** leader-elected loop (CAS lease) + stateless operator workers + NATS triggers.

**Negative — accepted.**
- A **new plugin axis** (`operator/v1`) — additive surface, but owes [ADR-003](003-two-references-before-contract-freeze.md)
  (two references + conformance) before freeze.
- The reconciler grows real machinery (work queue, backoff, leader election, status) — but it's the
  controller-runtime it was always *implied* to be; this complexity is inherent to the convergence
  guarantee, **not** scope creep.
- **Failure-domain risk:** a buggy operator can hot-loop. The core MUST bound it (backoff,
  max-requeue, per-operator concurrency caps) — that's *enforcement*, not domain knowledge.
- The built-in **deployment operator stays tier-0** (bootstrap) — not hot-swappable like domain
  operators; name it as such (plugin-architecture.md tier-0).

**Neutral.** "Resource kinds" need a light registration (which state prefix a kind lives under); a
convention (kind URI → state prefix) suffices to start (Q01).

## Alternatives considered

- **Bake pipeline/freshness logic into the core reconciler.** Rejected — a 7th thing (the core would
  hold domain semantics); violates the thesis; un-swappable.
- **A standalone "pipeline orchestrator" service** (v2's `ratd` shape) beside the reconciler.
  Rejected — duplicates the control loop, leader election, watch, and status the reconciler already
  owns; fragments convergence into two engines.
- **Operators poll state themselves; no core dispatch.** Rejected — every operator re-implements
  watch/queue/backoff/leader-election; providing those *once* is the entire point of a
  controller-runtime. (Operators MAY still read state directly for desired/actual — they just don't
  own the loop.)
- **Push the full resource object in `ReconcileRequest`.** Rejected — k8s learned to pass a *ref* and
  let the controller read current state (avoids stale-object races on requeue).

## Open questions

- **Q01 — resource-kind registry:** how a kind URI maps to its state prefix + (optional) schema —
  convention vs explicit registration.
- **Q02 — operator fairness/safety:** per-operator queue depth, max in-flight, rate limits (the
  hot-loop guardrail).
- **Q03 — deletion / finalizers:** reconciling resource *deletion* (cleanup) — the k8s finalizer
  pattern; builds on `state/v1/delete` ([ADR-035](035-state-axis-delete.md)).
- **Q04 — the two references** for `operator/v1` (ADR-003): the built-in deployment operator + a
  pipeline operator before the axis freezes.
- **Q05 — parallelism:** multi-replica operator *workers* under a single reconciler *leader* — where
  concurrency lives.

## Migration

Additive: define `operator/v1` (proto + the manifest `reconciles` field); the reconciler gains the
work-queue + registry-resolve + `Invoke`-reconcile + backoff + leader-election (CAS) + status path;
reframe the existing plugin-set loop as the built-in **deployment operator**. First domain operator:
a **pipeline operator** (watches pipeline desired-state, runs the `runtime`). **Depends on:** the
reconcile loop actually running in the daemon (the deferred finding, [done.md](../../roadmap/done.md)) +
the event bus wired alongside the daemon.

## Related

- [ADR-001](001-everything-is-a-plugin.md) — the six-thing core; this is a logged temptation-ledger
  examination (verdict: not a 7th thing).
- [ADR-035](035-state-axis-delete.md) — `Delete` (finalizers/cleanup) + the CAS lease the leader uses.
- [ADR-002](002-founding-tech-stack.md) D5 — leader-election lease; [reviews/06] — CAS conformance.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule `operator/v1` owes.
- Kubernetes `controller-runtime` — the prior art this mirrors (generic loop, domain in controllers).
