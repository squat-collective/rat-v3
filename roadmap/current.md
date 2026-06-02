# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (🎉 **Phase 1 SEALED — `rat/2.0`.** All 9 board exit criteria met; the core is built + enforced against real launched plugins. Freeze stays **local/unpushed**; **Q02** external review owed; Phase 2+ are user-pull-gated. Branching discipline in force — `main` is the sealed line; work continues on the next `phase-N`.)

## Status one-liner

**Phase 0 (lock the contracts) — 🎉 SEALED (`rat/1.5`).** **Phase 1 (the core) — 🎉 SEALED (`rat/2.0`).** Every board exit criterion is real + green against real launched plugins: **C5** (capability enforcement, real providers) · **C4** (audit-every-decision + terminal stream-close) · **C3** (deadline bound + idle backstop) · **C1** (idempotency, incl. a durable ledger surviving a real backend crash) · **D1** (two enforcing deployment-runtimes incl. podman full-I9) · **D2** (Arrow bulk-leg ticket gate) · **D3** (storage-cred isolation) · **D4** (`declared == conformed`, ed25519) · **sre#4** (reconciler crash-loop backoff/jitter + leader-election lease-thrash guard). `make core-test` + `make core-test-podman` + `make breaking` green. **Next: cut the `phase-1` → `main` seal (`rat/2.0`)** — a phase-boundary decision (Q02 external review still owed).

## ✅ Commitment gate — CLEARED (ADR-015)

ADR-013 deferred the gate to the spike's report; [reviews/10](../reviews/10-phase-1-spike-exit.md) delivered it (frozen wire held), and Tom made the call: **commit to the full core build.** Scope: this clears **Phase-0 → Phase-1**. The later user-pull gates remain hard — phases.md **Gate B** (≥10 real solo users), **Gate C/D**, and **Q02** (external peer review, still owed — schedule *during* the build). Rationale (Q01, why v3 over v2) recorded in ADR-015 from the founding premise; refine there if your conviction is framed differently.

## 🎉 Phase 1 — COMPLETE & SEALED (`rat/2.0`)

The six things + cross-cutting enforcement ([ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md)) are built and green against **real launched plugins**: `core/{manifest, registry (+ NewVerified), gateway, supervisor, deploymentruntime (local-process + podman), lease, reconciler, conformance, arrowticket, composition}`. All 9 board exit criteria below are met; `phase-1` is merged to `main` and tagged `rat/2.0`.

**Definition of done** (the board's exit criteria — [reviews/10](../reviews/10-phase-1-spike-exit.md)): **✅ C5** (extended to real providers — DONE) · **✅ C4** terminal audit incl. denials (DONE) · **✅ C3** (no-deadline idle-timeout backstop — DONE) · **✅ D1** a real *enforcing* deployment-runtime (podman, full I9 profile — DONE) · **✅ D2** (wire into the real bulk leg — DONE) · **✅ D3** storage-cred isolation (DONE) · **✅ D4** conformance-attestation *enforced* (DONE) · **✅ C1** (against real backends — DONE) · **✅ sre#4** reconciler crash-loop backoff/jitter + lease-thrash guard (DONE). **🎉 ALL 9 MET.**

**Immediate next concrete step:** 🎉 **Phase 1 is SEALED — `rat/2.0`** (merge `6b85477` carried sre#4, the last of the 9; this commit records the seal). All board exit criteria met, all green against real launched plugins, frozen wire intact. **Now owed / next:**
- **Q02 — external peer review** (still owed per [reviews/09](../reviews/09-phase-1-gate-review.md) / [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)): the spike/core has only had internal adversarial review; get outside eyes on the architecture before broad commitment. The freeze stays **local/unpushed** until then. ✅ **Recruiting kit COMPLETE** → [brief](../reviews/Q02-external-review-brief.md) + [outreach note](../reviews/Q02-outreach-note.md) + tailored [security](../reviews/Q02-brief-security.md) / [SRE](../reviews/Q02-brief-sre.md) / [ecosystem](../reviews/Q02-brief-ecosystem.md) / [architect](../reviews/Q02-brief-architect.md) briefs (all 5 internal lenses); **the only remaining Q02 step is human: recruit reviewer(s)** (OSGi/K8s/VSCode/Temporal-class) + run it.
- **Phase 2+ are user-pull-gated** — phases.md **Gate B** (≥10 real solo users) must be met before building further. Phase 2 is NOT started.
- Residual follow-ons (non-blocking, in backlog): write-leg idempotency vs a real *idempotent format* ref (C1 residual); an explicit cloud metadata-egress drop + structured `IsolationAttestation` (D-series GA); core audit-record signing + hash chain (C4/C8 GA, seeded by D4's ed25519).

CI (`make core-test` + `make core-test-podman` + `make breaking`) green.

## What's NOT in flight

- **Phase 2–5** — gated (Gate B: ≥10 solo users; Gate C/D). Not started.
- The broader **GTM / distribution** work (vision.md anti-goals) — Phase 4; the commitment cleared here is the *core build*, not the GTM motion.

## Branching (in force)

Work on `phase-1` (integration) or `phase-1-<slug>` (topic) sub-branches — **never commit to `main`** (a `PreToolUse` hook blocks it). Sub-branches merge back `--no-ff`. Full rules: [`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When this stream produces concrete output: update `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
