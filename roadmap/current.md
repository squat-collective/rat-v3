# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (🎯 **Phase-1 commitment gate CLEARED** — [ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md): the spike validated the frozen contracts ([reviews/10](../reviews/10-phase-1-spike-exit.md), no freeze-reopen), so RAT v3 now commits to the **full Phase-1 core build**. Exploratory posture ended. Branching discipline in force — work on `phase-1` / `phase-1-<slug>`, never `main`.)

## Status one-liner

**Phase 0 (lock the contracts) — 🎉 SEALED (`rat/1.5`).** **Phase 1 (the core) — COMMITTED full build, in flight.** The contract-de-risking spike is done ([reviews/10](../reviews/10-phase-1-spike-exit.md)): a real registry + capability-invoke gateway enforce **C5** from manifests, the cross-axis pipeline runs through it, **C1** crash-recovery + **C3** deadline-bound + **D2** ticket all green, and `make breaking` confirms the frozen wire is intact. On that evidence the **12–18mo commitment gate is CLEARED** ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md)).

## ✅ Commitment gate — CLEARED (ADR-015)

ADR-013 deferred the gate to the spike's report; [reviews/10](../reviews/10-phase-1-spike-exit.md) delivered it (frozen wire held), and Tom made the call: **commit to the full core build.** Scope: this clears **Phase-0 → Phase-1**. The later user-pull gates remain hard — phases.md **Gate B** (≥10 real solo users), **Gate C/D**, and **Q02** (external peer review, still owed — schedule *during* the build). Rationale (Q01, why v3 over v2) recorded in ADR-015 from the founding premise; refine there if your conviction is framed differently.

## In flight — the full Phase-1 core build

The six things + cross-cutting enforcement ([ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md)), with the spike's `core/{manifest,registry,gateway,composition,arrowticket}` as the seed (C5/C1/C3/D2 already real + green against fake providers).

**Definition of done** (the board's exit criteria — [reviews/10](../reviews/10-phase-1-spike-exit.md)): **✅ C5** (extended to real providers — DONE) · **✅ C4** terminal audit incl. denials (DONE) · **✅ C3** (no-deadline idle-timeout backstop — DONE) · **✅ D1** a real *enforcing* deployment-runtime (podman, full I9 profile — DONE) · **✅ D2** (wire into the real bulk leg — DONE) · **✅ D3** storage-cred isolation (DONE) · **✅ D4** conformance-attestation *enforced* (DONE) · **C1** (against real backends) · **sre#4** reconciler crash-loop backoff/jitter.

**Immediate next concrete step:** 🎯 **7 of 9 Phase-1 exit criteria cleared.** Done: **C5** (capability enforcement, real providers) · **C4** (audit every decision + terminal stream-close) · **C3** (deadline bound + no-deadline idle backstop) · **D1** (two enforcing deployment-runtimes: `local-process` + `podman` full-I9, kernel-verified) · **D2** (the `ArrowStream` ticket is the only gate on a real bulk leg — HMAC/TTL/single-use/binding enforced end-to-end through a real transfer) · **D3** (storage-cred scoping: tenant-isolated, contained, through the gateway) · **D4** (the core verifies `declared == conformed` via signed ed25519 attestations — `registry.NewVerified`). Recent commits `9e7edca` (D4) · `af6e55c` (D2). **Next on `phase-1` — the last two DoD items:** **C1 against real backends** — the crash-mid-strategy at-least-once idempotency is proven against fakes (composition); re-prove it against a **real idempotent backend** (the sqlite catalog has commit-key idempotency; the inmemory-go format does NOT, so it needs the right backend) · **sre#4** reconciler crash-loop backoff + jitter + lease-thrash guard. Then the Phase-1 acceptance criteria are met → the `phase-1` → `main` seal (`rat/2.0`) comes into view. CI (`make core-test` + `make core-test-podman` + `make breaking`) green; **schedule Q02** external review; keep the freeze **local/unpushed**.

## What's NOT in flight

- **Phase 2–5** — gated (Gate B: ≥10 solo users; Gate C/D). Not started.
- The broader **GTM / distribution** work (vision.md anti-goals) — Phase 4; the commitment cleared here is the *core build*, not the GTM motion.

## Branching (in force)

Work on `phase-1` (integration) or `phase-1-<slug>` (topic) sub-branches — **never commit to `main`** (a `PreToolUse` hook blocks it). Sub-branches merge back `--no-ff`. Full rules: [`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When this stream produces concrete output: update `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
