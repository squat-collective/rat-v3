# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (🎯 **Phase-1 commitment gate CLEARED** — [ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md): the spike validated the frozen contracts ([reviews/10](../reviews/10-phase-1-spike-exit.md), no freeze-reopen), so RAT v3 now commits to the **full Phase-1 core build**. Exploratory posture ended. Branching discipline in force — work on `phase-1` / `phase-1-<slug>`, never `main`.)

## Status one-liner

**Phase 0 (lock the contracts) — 🎉 SEALED (`rat/1.5`).** **Phase 1 (the core) — COMMITTED full build, in flight.** The contract-de-risking spike is done ([reviews/10](../reviews/10-phase-1-spike-exit.md)): a real registry + capability-invoke gateway enforce **C5** from manifests, the cross-axis pipeline runs through it, **C1** crash-recovery + **C3** deadline-bound + **D2** ticket all green, and `make breaking` confirms the frozen wire is intact. On that evidence the **12–18mo commitment gate is CLEARED** ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md)).

## ✅ Commitment gate — CLEARED (ADR-015)

ADR-013 deferred the gate to the spike's report; [reviews/10](../reviews/10-phase-1-spike-exit.md) delivered it (frozen wire held), and Tom made the call: **commit to the full core build.** Scope: this clears **Phase-0 → Phase-1**. The later user-pull gates remain hard — phases.md **Gate B** (≥10 real solo users), **Gate C/D**, and **Q02** (external peer review, still owed — schedule *during* the build). Rationale (Q01, why v3 over v2) recorded in ADR-015 from the founding premise; refine there if your conviction is framed differently.

## In flight — the full Phase-1 core build

The six things + cross-cutting enforcement ([ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md)), with the spike's `core/{manifest,registry,gateway,composition,arrowticket}` as the seed (C5/C1/C3/D2 already real + green against fake providers).

**Definition of done** (the board's exit criteria — [reviews/10](../reviews/10-phase-1-spike-exit.md)): **C5** (extend to real providers) · **C4** terminal audit incl. denials · **C3** (add the no-deadline idle-timeout backstop) · **D1** a real *enforcing* deployment-runtime (podman, not in-process) · **D2** (wire into the real bulk leg) · **D3** storage-cred isolation · **D4** conformance-attestation *enforced* · **C1** (against real backends) · **sre#4** reconciler crash-loop backoff/jitter.

**Immediate next concrete step:** **D1 — a real process-isolating deployment-runtime.** The spike ran providers in-process; the next increment runs a reference plugin as an isolated child process (the `chmod +x ./rat` runtime / a podman launcher) and re-runs the composition through it — turning the fake providers real and unblocking D3 (storage-cred scoping) + C5-against-real-providers. Likely a `phase-1-adr-016-<core-bootstrap-real>` (the registry loading + launching plugins via a deployment-runtime, vs today's dial-already-running). On `phase-1-<slug>` sub-branches; **CI** (`make core-test` + `make breaking`) from commit 1; keep the freeze **local/unpushed** until C5 passes against real providers; **schedule Q02** external review.

## What's NOT in flight

- **Phase 2–5** — gated (Gate B: ≥10 solo users; Gate C/D). Not started.
- The broader **GTM / distribution** work (vision.md anti-goals) — Phase 4; the commitment cleared here is the *core build*, not the GTM motion.

## Branching (in force)

Work on `phase-1` (integration) or `phase-1-<slug>` (topic) sub-branches — **never commit to `main`** (a `PreToolUse` hook blocks it). Sub-branches merge back `--no-ff`. Full rules: [`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When this stream produces concrete output: update `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
