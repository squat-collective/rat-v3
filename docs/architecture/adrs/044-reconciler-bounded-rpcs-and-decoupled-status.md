# ADR-044: Bounded reconciler RPCs + a decoupled status read path

## Status: Accepted (2026-06-10)

## Context

The code-level review of `core/` found **gap #4** in the reconciler. The convergence pass held a
single mutex across its *entire* run, and that run made **blocking runtime RPCs** while holding it:

```go
// before
func (r *Reconciler) Reconcile(ctx, now) {
    r.mu.Lock()
    defer r.mu.Unlock()
    for _, d := range r.desired {
        r.reconcileOne(ctx, d, now) // → Healthcheck / Launch / Terminate RPCs, all under r.mu
    }
}
func (r *Reconciler) Status(name) (...) { r.mu.Lock(); ... }   // same mutex
func (r *Reconciler) Endpoint(name) string { r.mu.Lock(); ... } // same mutex
```

Two failure modes followed (backlog AV-3):

1. **A hung RPC pins the whole loop.** `status()` called `Healthcheck` with no deadline. One plugin
   whose runtime call wedges blocks the pass — and every *other* plugin's reconcile behind it —
   indefinitely.
2. **A hung RPC blinds the control/observability read path.** `Status` and `Endpoint` take the same
   mutex the pass holds across that wedged RPC, so `rat status` / `ListPlugins` / the bring-up
   readiness poll all hang too. "Why won't my plugin come up?" becomes unanswerable precisely when
   you most need the answer.

This is the SRE review's standing point in miniature: the cross-cutting property (does the control
plane stay observable under a partial fault?) was not a correctness condition the reconciler upheld.

## Decision

**Bound every runtime RPC with a per-call deadline, and serve the status read path from a published
snapshot that the reconcile mutex never guards.**

### 1. Deadline-bounded runtime RPCs

`Healthcheck`, `Launch`, and `Terminate` each run under `context.WithTimeout(parent, RPCTimeout)`
(default 5s, configurable). A wedged call is cut; `status()` treats a deadline (or any error) as
`UNHEALTHY` — the instance is unreachable — and the plugin flows through the existing
crash-loop/backoff path. The pass is now bounded by `RPCTimeout` per plugin instead of unbounded.

### 2. A decoupled status snapshot

The reconciler keeps a separate `statusView` snapshot under its own `RWMutex`. Each `reconcileOne`
republishes the plugin's observable status (state, attempts, next-retry, endpoint) into it.
`Status`/`Endpoint` read only that snapshot — so a status read **never** waits on the reconcile
mutex, and therefore never waits on a wedged RPC. The snapshot is briefly stale during a pass
(eventually consistent), which is the correct tradeoff for an observational read.

### 3. The desired set gets its own lock

`desired` moves onto a dedicated `RWMutex`, so `DesiredNames`/`AddDesired` don't queue behind a
pass either. `Reconcile` snapshots `desired` once at the start (a brief read-lock) and iterates the
copy.

### What is deliberately NOT changed

The reconcile pass still holds the plugins mutex across the pass, so a plugin is reconciled
serially and `RemoveDesired`/`Shutdown` still synchronize with a pass (now *bounded* by
`RPCTimeout`). Holding it is what keeps `RemoveDesired` from racing a `Launch` and resurrecting a
just-removed plugin or orphaning its instance. **Fully parallel per-plugin reconcile is a separate,
larger change** (lock-free apply with per-plugin re-validation) and is out of scope here — AV-3
asked for bounded RPCs + a decoupled read path, which is exactly this.

## Consequences

### Positive

- **A wedged plugin no longer pins the loop or blinds the control plane.** Proven by a test: a
  reconcile pass stuck in a hung `Healthcheck` still answers `Status` in well under a millisecond,
  and the per-call deadline cuts the hung RPC so the pass completes on its own (the test never
  releases it). The suite runs green under `-race`.
- **No wire change, no contract change, six-thing count unchanged.** It is the reconciler upholding
  a correctness condition (stay observable under a partial fault), not a new responsibility.
- **Bounded blast radius for the operator paths.** `RemoveDesired`/`Shutdown` wait at most
  `RPCTimeout` × remaining-plugins for a pass, never forever.

### Negative / costs

- **Status is eventually consistent.** A reader may see the prior pass's view for a plugin mid-pass.
  Acceptable — and far better than a blocking, "fresh-or-hung" read.
- **The pass is bounded, not parallel.** `N` wedged plugins can still cost up to `N × RPCTimeout`
  per pass. Full per-plugin concurrency (and fairness budgets) remains future work — named here so
  it isn't mistaken for done.
- **A second + third lock.** Three locks (desired / plugins / snapshot) with a documented order
  (`desMu` → `mu` → `snapMu`) replace one. The order is asserted by the `-race` suite.

## Alternatives considered

- **Just add RPC deadlines, keep one mutex.** Bounds "pins the loop" but not "blinds Status" — a
  status read still waits up to `RPCTimeout` behind a wedged pass. Half the fix; rejected.
- **Drop the pass-wide mutex entirely (lock-free per-plugin apply).** The full parallel reconciler.
  Correct end state, but it introduces real resurrection/orphan races (a `Launch` returning after a
  concurrent `RemoveDesired`) that need careful re-validation. Too large for this gap; deferred.
- **A dedicated metrics goroutine polling state.** Redundant with the snapshot and adds a moving
  part; the snapshot is the same data without the extra goroutine.

## Related

- backlog AV-3 — "bound the reconciler's runtime RPCs with per-call deadlines; give Status()/Endpoint() a read path that doesn't share the reconcile mutex." Closed here.
- ADR-042 / ADR-043 — the sibling gap-#2 / gap-#1 fixes from the same code-level review.
- reviews/03 (operations-sre) — the standing "stay observable under partial fault" point this applies.
- Future: parallel per-plugin reconcile + per-plugin fairness budgets (the larger concurrency step this explicitly defers).
