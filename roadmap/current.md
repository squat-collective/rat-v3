# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (Phase 0 **SEALED** `rat/1.5`; **Phase 1 ENTERED as a time-boxed contract-de-risking spike** per [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md), after a 13-agent gate re-confirmation ([reviews/09](../reviews/09-phase-1-gate-review.md)) returned *proceed-with-conditions*. **Branching discipline now in force** — work on `phase-1` / `phase-1-<slug>`, never `main`.)

## Status one-liner

**Phase 0 (lock the contracts) — 🎉 COMPLETE & SEALED (`rat/1.5`).** **Phase 1 (the core) — IN FLIGHT as a time-boxed 2–4 week spike** ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)). A 13-agent board ([reviews/09](../reviews/09-phase-1-gate-review.md)) re-confirmed readiness (`proceed-with-conditions`, strong-majority) and verified *live* that nothing was dropped from [reviews/08](../reviews/08-post-freeze-board-review.md). Tom chose to **de-risk the freeze with a real enforcer before committing the full ~3-month core**; the 12–18mo runway commitment is **consciously deferred to the spike's exit report**.

## ✅ Commitment gate — RECORDED (no longer "acknowledged but uncleared")

The pre-Phase-0 gate ([phases.md](phases.md) "Decision gates": 12–18mo runway + GTM) was *acknowledged, not cleared*. [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) resolves it into an explicit decision: **proceed in exploratory mode via a time-boxed spike; revisit the full commitment after the spike reports.** Still owed before that full commitment: the v2-vs-v3 opportunity-cost answer (ADR-013 **Q01**) + external peer review (**Q02** — the dissent flagged zero external human review).

## In flight — the Phase-1 spike

**Goal:** convert the board's one un-dissolved risk — frozen *obligations* (capability enforcement, bytes-plane isolation, crash-mid-run atomicity) that froze as prose MUSTs with no conformance vector and have never been exercised by a real enforcer; green certifies *shapes*, not *obligations* — into an explicit test. Stand up the minimum real core and **try to break a frozen contract while the freeze is still local/cheap.**

**Spike scope:**
- A minimal **registry** + **capability-invocation gateway** that *enforces* (not the throwaway stub).
- **C5** capability enforcement for real (deny a capability not in the manifest `requires`).
- A **crash-mid-strategy** composition case: kill a strategy mid-`Apply` → assert no double-apply (C1) + a truncated stream fails the write (C2). **A discovered need for a strategy commit/abort wire shape = a freeze-reopen trigger, not a routine bug.**
- Exercise **C3** (provider-call deadline) + **D2** (ArrowStream-ticket TTL/single-use/binding) — the prose-only, no-vector guarantees where a latent wire-shape regret would hide.

**Immediate next concrete step:** ✅ **ADR-014 landed** (the spike-core shape: a manifest-driven registry + capability-invoke gateway). **Now building `phase-1-registry-core`** — scaffold the `core/` Go module (`replace` → the SDK), seed the gateway from `examples/bench/latency-go/gateway.go`, add the manifest-loading **registry** + **manifest-derived C5** (replacing the stubs' hardcoded allowlist), and **wire CI from commit 1** (`buf breaking` + `make {conformance,composition,validate-manifests}` + `go test ./core/...`). Keep the freeze **local/unpushed** (no remote, no BSR) until C5 enforcement passes.

## Phase-1 definition-of-done (the board's exit criteria)

The core isn't "done" until these *pass* (reviews/08 + [reviews/09](../reviews/09-phase-1-gate-review.md)):
- **C5** capability enforcement (`declared == provided`, enforced) · **C4** audit-on-every-decision incl. denials + stream-terminal · **C3** provider-call deadline + streaming idle-timeout · **D1** isolation-profile conformance (a real *enforcing* deployment-runtime — podman, not dry-run) · **D2/D3** ArrowStream-ticket + storage-cred isolation vectors · **D4** conformance attestation *enforced* (`declared == conformed`, not self-asserted) · **C1** at-least-once re-runs don't double-apply (additive fields landed `rat/1.5`; the *enforced* test is here) · **sre#4** reconciler crash-loop backoff + jitter (promoted to an explicit exit gate).

## What's NOT in flight

- **Phase 2–5** — not started.
- The **full ~3-month core build** — gated on the spike's exit report + Tom's commitment call.
- Remaining `v1.1`-additive contract niceties (`WriteResult` insert/update/delete breakdown, `TableRef` snapshot/as-of, `health/v1` probe, etc.) — queued in [backlog.md](backlog.md); land opportunistically or as the spike drives them out.

## Branching (now in force)

Work on `phase-1` (integration) or `phase-1-<slug>` (topic) sub-branches — **never commit to `main`** (a `PreToolUse` hook blocks it). Sub-branches merge back `--no-ff`. Full rules: [`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When this stream produces concrete output: update `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
