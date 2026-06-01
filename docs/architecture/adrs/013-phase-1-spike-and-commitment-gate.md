# ADR-013: Phase 1 entry — a time-boxed contract-de-risking spike + the commitment-gate posture

## Status: Accepted (2026-06-01)

## Context

Phase 0 sealed (`rat/1.5`). Before committing to the full Phase 1 core build (~3 months,
~12–15k LOC — [phases.md](../../../roadmap/phases.md)), we re-confirmed readiness via a
13-agent board review ([reviews/09](../../../reviews/09-phase-1-gate-review.md)): an 8-area
completeness audit → a 4-lens board (architecture-readiness, business/runway/GTM, skeptic,
pragmatist) → chair synthesis.

**Verdict: proceed-with-conditions (strong-majority).** The *engineering* question is settled
and independently re-verified this session (not trusted from the roadmap): `rat/1.5` verified;
`make conformance` 32/32, `make composition` green, `make validate-manifests` 32/32 all ran
live; [ADR-003](003-two-references-before-contract-freeze.md)'s two-reference bar is genuinely
met on all 6 critical data-plane axes with technologically-divergent real backends over real
Arrow Flight; exactly one true v2-regret was found across 18 axes (`WriteResult.snapshot_id`)
and fixed pre-publication; the largest discovered gap (catalog commit-linkage B1) was absorbed
additively ([ADR-010](010-catalog-commit-linkage.md)).

Two things blocked an *unconditional* go:

1. **The project's own commitment gate is unmet.** [phases.md](../../../roadmap/phases.md)
   ("Decision gates that BLOCK phase entry") treats a 12–18mo runway + GTM commitment as a hard
   pre-Phase-0 gate; [current.md](../../../roadmap/current.md) records it as "acknowledged, not
   formally cleared." Phase 1 is an *irreversible* spend; starting it under an undeclared
   "exploratory" label silently converts the unmet gate into committed spend — the project's
   own modal failure mode ("ship more architecture, no users").
2. **Green certifies shapes, not obligations.** The dissent (business lens, WAIT, high
   confidence) and the skeptic's strongest caution reduce — as *timing* objections — to one
   fact no technical check answers: the hardest guarantees (capability enforcement, bytes-plane
   isolation, crash-mid-run atomicity) froze as prose MUSTs with **no conformance vector** and
   have **never been exercised by a real enforcer** (the C5 gateway is a `THROWAWAY STUB`;
   `plugin.v1.json` admits "no enforcer exists yet"). Phase 1 could discover a frozen-contract
   flaw (e.g. the strategy axis needs a commit/abort shape) that is expensive to fix
   post-publish. The freeze is still **local/unpushed**, which keeps any such fix cheap.

The 12–18mo commitment is Tom's call, not the board's, and it hinges on a strategic question no
engineering check answers: why rebuild from scratch in v3 when v2 already ships the same
plug-everything thesis (v2 ADR-024/025/026)?

## Decision

**Phase 1 begins as a time-boxed 2–4 week contract-de-risking spike, not the full core
commitment. The 12–18mo runway commitment gate is consciously deferred to the spike's report**
— recorded as a deliberate "continue exploratory, decide after the spike" (which phases.md
blesses as a valid answer), **not left undeclared** (the one option the board ruled out).

### 1. Spike goal — break a frozen contract while it's still cheap

Convert the skeptic's one un-dissolved risk into an explicit test: stand up the minimum *real*
core and try to make a frozen obligation fail.

- A minimal **registry** + **capability-invocation gateway** that actually *enforces* (not the
  throwaway stub).
- **C5 capability enforcement** for real: a plugin invoking a capability not in its manifest
  `requires` is denied (`declared == provided` enforced, not self-asserted).
- A **crash-mid-strategy** composition case (reviews/08 C5): kill a strategy mid-`Apply`;
  assert the at-least-once re-run does not double-apply (C1 `idempotency_key`) and a truncated
  stream fails the write (C2). **If the strategy axis turns out to need a commit/abort wire
  shape, that is a freeze-reopen trigger, not a routine bug** — and this is the cheapest moment
  to find it.
- Exercise **C3** (provider-call deadline on the bytes leg) and **D2** (ArrowStream-ticket
  TTL/single-use/binding) — the prose-only guarantees with no conformance vector, where a
  latent wire-shape regret would hide.

### 2. The spike's exit report decides the full commitment

- **No contract regret found** → the freeze is validated by a real enforcer; Tom makes the
  explicit 12–18mo call (with the v2 opportunity-cost answer written down — Q01) and the full
  core build proceeds.
- **A contract regret found** → reopen the freeze *additively* (or `v2` the affected axis)
  while still local; re-seal; then decide.

Either way, the spike answers "is the freeze real?" with evidence instead of self-assertion —
the thing the dissent correctly said we do not have today.

### 3. Standing conditions for Phase 1 (from the board)

- **Keep the freeze LOCAL/unpushed** (no BSR publish, no remote) until at least C5 enforcement
  passes against the real core — the highest-value lever for absorbing a regret additively.
- **Wire CI on the `phase-1` branch from commit 1:** `buf breaking` (no committed buf baseline
  exists today — the "additive-only" claim rests on source inspection) + the three make gates
  (`conformance`, `composition`, `validate-manifests`).
- The deferred enforcement findings are the literal **Phase-1 definition-of-done** (passing
  acceptance tests): **C5, C4, C3, D1, D2/D3, D4** (D4 = `declared == conformed` *enforced* by
  the core, not self-asserted) + **C1** (the additive fields landed in `rat/1.5`; the
  *enforced* no-double-apply test is Phase 1) + **sre#4** (reconciler crash-loop backoff +
  jitter — promoted to an explicit exit gate; previously only in the backlog + reconciler
  line-item).
- Keep the "core not built / self-asserted" honesty banner on `plugin.v1.json` + every
  `CONTRACT.md` until a real enforcer exists.

## Consequences

**Positive.**
- The highest residual risk (a frozen obligation hiding a wire-shape flaw) is attacked *first*,
  while the freeze is still cheap to change — not discovered months into the full build.
- The runway commitment will be made on *evidence* (the spike report), not on green badges that
  certify shapes, not obligations.
- Bounded downside: 2–4 weeks of sunk engineering vs. a 3-month commitment to a possibly-flawed
  freeze.

**Negative — accepted.**
1. **The commitment decision is deferred, not made.** The project stays in a self-declared
   exploratory posture; if Tom never clears the gate, v3 remains a design corpus. Explicitly
   acceptable (phases.md allows a recorded "continue exploratory"); what's *not* acceptable —
   leaving it undeclared — is resolved by this ADR recording it.
2. **The spike is not throwaway** — its registry + enforcer become Phase-1 seed code, so
   "spike" discipline (time-box, narrow scope) must be held, or it silently becomes the full
   build with the gate never cleared.
3. **The v2-vs-v3 opportunity-cost question is postponed, not answered** (Q01). The spike
   de-risks the *contracts*; it does not answer *why v3 over evolving v2*. Still owed before
   the full commitment.

**Neutral.** Phase 1's status becomes "in-flight (spike)" rather than "not-started"; the
acceptance criteria are unchanged in substance (sre#4 is *promoted*, not new scope).

## Open questions

- **Q01** — Why a from-scratch v3 core over continuing v2's already-shipping decoupling (v2
  ADR-024/025/026)? Owed before the full 12–18mo commitment; the spike does not answer it.
- **Q02** — External peer review (backlog: "Recruit external peer reviewers"); the dissent
  flagged *zero* external human review. Fold into the full-commitment gate, not the spike.

## Alternatives considered

1. **Commit to the full core now.** Rejected for now: the hardest guarantees are unproven by
   any enforcer; committing 3 months before a single real enforcement path runs is the
   expensive-failure case the skeptic named. Available again after a clean spike report.
2. **Continue "exploratory" with no scope change (re-eval at the C5 milestone).** A valid board
   option, but vaguer — "re-eval at C5" inside a full build still spends the runway before the
   gate. The spike makes the de-risking the *explicit deliverable* and the gate a discrete
   checkpoint.
3. **Wait — stay on v2** (the business lens's vote). Rejected as the headline path: it discards
   the cheap-fix window (freeze still local) and the board majority found the contracts sound
   enough to test. Preserved as a live *outcome* of the spike (if the spike finds the freeze
   deeply wrong, redirecting to v2 is on the table).
4. **A pure planning ADR, no code.** Rejected: more docs produce zero new signal on the
   obligations-vs-shapes gap — only a running enforcer does.

## Migration

This is the Phase-0→Phase-1 transition. Sequence: this ADR + [reviews/09](../../../reviews/09-phase-1-gate-review.md)
recorded → roadmap reconciled (sre#4 promoted; C1/D5 drift fixed; deliverable counts
corrected) → branching discipline landed (this session) → spike work on `phase-1-<slug>`
sub-branches with CI wired from commit 1 → spike exit report → commitment-gate decision.

## Related

- [reviews/09](../../../reviews/09-phase-1-gate-review.md) — the 13-agent board review this ADR records.
- [reviews/08](../../../reviews/08-post-freeze-board-review.md) — the post-freeze review whose deferred findings (C3–C5, D1–D4, C1, sre#4) are Phase-1's definition-of-done.
- [ADR-009](009-data-plane-contract-freeze-v1.md) · [ADR-012](012-crash-safety-additive-fields.md) — the freeze + the C1/C2 additive crash-safety fields whose *enforcement* the spike tests.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference bar the audit confirmed met.
- [phases.md](../../../roadmap/phases.md) Phase 1 + "Decision gates" · [current.md](../../../roadmap/current.md) — the gate this ADR resolves into a recorded decision.
- [`.claude/rules/git-branching.md`](../../../.claude/rules/git-branching.md) — the branching model adopted this session (whose slash bug this ADR's own branch caught).
