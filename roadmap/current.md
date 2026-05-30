# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-05-30 (after the 5-perspective synthesis + ADR-003 + roadmap structure landed)

## Status one-liner

**Architectural design + adversarial review complete (Phase −1 done).** No code yet. Awaiting Tom's commitment decision before entering Phase 0.

## Active streams

### Stream 1 — Awaiting commitment decision

**Status:** blocked on Tom's decision.

The synthesis (reviews/00-synthesis.md) surfaced that this is a **decision gate, not a continuation gate**. Two questions for Tom to answer before Phase 0 kicks off:

1. **Are you committed to building v3 over 12-18 months?** (Includes the runway, not just the appetite.)
2. **Are you committed to doing the GTM work in parallel from day 1?** (Hand-to-hand design partners, demo-loader port, anti-lock-in positioning, dbt→RAT migration.)

If both yes → kick off Phase 0 (4-6 months, see [phases.md](phases.md)).
If either no → freeze the project as a public design corpus; don't accrete more weight.

### Stream 2 — Roadmap + ADR upkeep

**Status:** done as of this commit.

The synthesis raised 26 prospective ADRs; we DIDN'T write all of them. Instead we landed:
- ADR-003 (two-reference rule — the most-cited synthesis recommendation)
- Updated ADR-001 Phase 0 description (bakes Critical concerns into Phase 0)
- Updated vision.md (added GTM anti-goals)
- Created this roadmap structure

The 23 other prospective ADRs are in [backlog.md](backlog.md). They land as they become relevant — most are Phase-0-blocking and get written during Phase 0.

## Immediate next concrete step

**For Tom:** decide on the two commitment questions above.

**For the next Claude session (if Tom commits and Phase 0 kicks off):**
1. Read [phases.md](phases.md) Phase 0 section in full.
2. Start with sub-phase **0a — Manifest schema** (`plugin/v1.json`).
3. Draft the JSON Schema for the manifest envelope. Use [reviews/02-plugin-ecosystem-builder.md](../reviews/02-plugin-ecosystem-builder.md) Stage 2 + [ADR-002 D3](../docs/architecture/adrs/002-founding-tech-stack.md) as inputs.
4. Bake in the Critical-finding fields from the start: `resources: {requests, limits}` block (C4), `trust: {signature, signed_by, attestations}` block (C8), `capabilities` in `requires`/`provides` shape (C5).

**For the next Claude session (if Tom holds):** read this file, [reviews/00-synthesis.md](../reviews/00-synthesis.md), and the most recent conversation in `docs/conversations/`. Don't add architecture; the synthesis is the current resting point.

## What's NOT in flight (paused / cancelled)

- Phase 0 — not started (awaiting commitment)
- Phase 1-5 — not started
- The 23 other prospective ADRs from synthesis — backlogged

## Maintenance reminder

When this stream's status changes (e.g., Tom commits and Phase 0 kicks off, or a new working session produces concrete output):

1. Update this file (`current.md`).
2. Append the completed work to [done.md](done.md).
3. Update [phases.md](phases.md) status table.
4. Promote any new identified work from inbox / reviews → [backlog.md](backlog.md).

See [CLAUDE.md](CLAUDE.md) in this directory for the full maintenance rules.
