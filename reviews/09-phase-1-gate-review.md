# Reviews 09 — Phase-1 commitment-gate re-confirmation (13-agent board)

**Date:** 2026-06-01
**Trigger:** Phase 0 sealed (`rat/1.5`). Before committing to the full Phase 1 core build,
re-confirm two things: *is now the moment?* and *did we miss absolutely nothing?*
**Method:** a deterministic workflow — an **8-area completeness audit** (parallel) → a
**4-lens board** (informed by the audit) → a **chair synthesis**. 13 agents total. Audit
agents on Sonnet (factual verification); board + chair on Opus (judgment). Full per-agent
transcript: workflow run `wf_a4a6cb93-8c4`.
**Verdict:** **proceed-with-conditions** (strong-majority).
**Decision taken (Tom):** a **time-boxed 2–4 week contract-de-risking spike** — see
[ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md).

---

## "Did we miss anything?" — essentially no.

All 8 audit areas returned `minor-gaps` (none `major`). Crucially, **nothing was dropped from
[reviews/08](08-post-freeze-board-review.md)** — every finding traces to fixed-in-v1.1, a
Phase-1 acceptance criterion, or the backlog. The freeze *quality* was independently
re-verified **this session** (not trusted from the roadmap):

- `make conformance` **32/32**, `make composition` green, `make validate-manifests` **32/32** — run live.
- [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md)'s two-reference
  bar is genuinely met on all 6 critical data-plane axes — divergent real backends
  (duckdb+datafusion, parquet+delta, sqlite, localfs, subprocess) over real Arrow Flight.
- Exactly **one** true v2-regret across 18 axes (`WriteResult.snapshot_id`) — fixed
  pre-publication. The biggest discovered gap (catalog commit-linkage B1) was absorbed
  additively ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md)).

| Audit area | Verdict | Phase-1-blocking gaps |
|---|---|---|
| Contract-surface freeze | minor-gaps | 0 |
| Conformance & composition | minor-gaps | 0 |
| Manifest schema (ADR-011) | minor-gaps | 0 |
| Documentation completeness | minor-gaps | 0 |
| ADRs (001–012, index) | minor-gaps | 0 |
| Roadmap internal consistency | minor-gaps | 0 |
| Nothing-dropped vs reviews/08 | minor-gaps | **1** (sre#4 not in explicit Phase-1 ACs) |
| Phase-0 scope vs delivery (incl. ADR-003) | minor-gaps | **1** (commitment gate unmet) |

---

## Board tally

| Lens | Vote | Confidence |
|---|---|---|
| Architecture-readiness | proceed-with-conditions | high |
| **Business / runway / GTM** | **WAIT** (the dissent) | high |
| Skeptic (steelman wait) | proceed-with-conditions | medium |
| Pragmatist (steelman proceed) | proceed-with-conditions | high |

---

## The two blocking gaps

1. **sre#4 — reconciler crash-loop backoff + jitter** was in the backlog + the reconciler
   impl line-item + Phase-4 hardening + slated for ADR-022, but **not** in the explicit Phase-1
   exit-criteria list. Without elevating it, the core can ship re-making the K8s CrashLoopBackoff
   mistake. ~half-day promotion, *zero* net-new scope. → now an explicit Phase-1 AC.
2. **The 12–18mo commitment gate is unmet** by the project's own rules — a *governance* decision
   for Tom, not a code blocker. Recorded via [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md).

---

## The 8 recommended conditions

1. Tom records an explicit commitment-gate decision **before the full core build** (a conscious
   "continue exploratory, re-evaluate after the spike" is valid; leaving it undeclared is not). ✅ ADR-013.
2. Write down the v2 opportunity-cost decision, **or** time-box a 2–4 week de-risking spike. ✅ chose the spike (ADR-013); v2 answer = ADR-013 Q01 (owed before full commitment).
3. Promote **sre#4** into the explicit Phase-1 acceptance list (phases.md + current.md). ✅
4. Treat **C3/C4/C5 + D1/D2/D3/D4 + C1** as the literal Phase-1 definition-of-done (D4 = `declared == conformed` *enforced*); keep the honesty banner until a real enforcer exists; implement the prose-only/no-vector guarantees (C3 deadline, D2 ticket, D1 isolation) first. ✅ ADR-013 §3.
5. Add the **crash-mid-strategy** composition case as a Phase-1 exit test; a failure = a **freeze-reopen trigger**, not a routine bug. ✅ ADR-013 §1.
6. Keep the freeze **LOCAL/unpushed** until C5 enforcement passes against the real core. ✅ ADR-013 §3 (already local).
7. Wire **CI on `phase-1` from commit 1**: `buf breaking` (no committed buf baseline today) + the three make gates. → backlog (Phase-1 setup).
8. Reconcile the **roadmap-internal contradictions** early: phases.md still listed the completed C1 as a pending AC; "C3–C5, D1–D5" implied D5 (done) is deferred; deliverable counts stale (Java SDK dropped; `20 protos/12 refs` vs actual `24/32`); current.md still tagged landed work "(Staged; commit pending.)". ✅ fixed this session.

---

## Dissent (preserved — not averaged away)

The **business/runway/GTM lens** votes **WAIT (high)** on git-verified facts: the entire
project (scaffold → seal) is a **3-day, 112-commit** artifact against a "4–6 month" Phase-0
self-estimate → **zero soak time**, **self-asserted** conformance (the C5 `gateway_test.go` is
a `THROWAWAY STUB`; `plugin.v1.json` admits "no enforcer exists yet"), and **no external human
review**. The **skeptic** reinforces: 32/32 green certifies the wire **shapes**, not the
**obligations** (isolation, capability enforcement, crash-mid-run atomicity) — the hardest,
costliest-to-change guarantees have never been exercised by a real enforcer.

The conditions dissolve the *timing* objection (record the commitment; keep the freeze local;
spike to de-risk). The one piece they **cannot** dissolve, preserved as live: **no amount of
technical readiness answers whether a from-scratch v3 core is a better runway bet than
continuing v2's already-shipping decoupling.** That is strategic judgment — which is exactly
why the final call is Tom's, and why the chosen path (a spike) buys evidence before the bet.

---

## Decision

Tom chose the **time-boxed 2–4 week contract-de-risking spike**: stand up a minimal real core
+ capability enforcer and actively try to break a frozen contract (C5 + crash-mid-strategy +
C3/D2), keeping the freeze local so any regret is cheap to fix. The 12–18mo commitment is
deferred to the spike's exit report. Recorded in
[ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md).

## Related

- [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) — the decision this review produced.
- [reviews/08](08-post-freeze-board-review.md) — the post-freeze review whose findings this re-confirmed nothing-dropped.
- [roadmap/current.md](../roadmap/current.md) · [roadmap/phases.md](../roadmap/phases.md) — reconciled per condition #8.
