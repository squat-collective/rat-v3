# Roadmap maintenance rules

The roadmap is **the single source of truth** for "what are we doing, what have we done, what's queued, where are we." It exists because architectural projects of this scope die when context is lost between sessions. Every Claude session should be able to answer "what's the next concrete step?" by reading `roadmap/current.md` — and every working session should leave the roadmap fresher than it found it.

## Files in this directory

| File | What it holds | Update cadence |
|---|---|---|
| [`README.md`](README.md) | Entry point + reading order | Rarely (when structure changes) |
| [`phases.md`](phases.md) | The phased plan (Phase 0 → 5). Goal, scope, deliverables, estimated time, status per phase. | When a phase boundary moves or scope changes — surface via ADR first, then mirror here |
| [`current.md`](current.md) | What's in flight RIGHT NOW. Active work streams; next concrete step in each. | **Every working session that produces concrete output** |
| [`done.md`](done.md) | Reverse-chronological log of completed work. Date + summary + commit/file links. | Every commit of substantive work |
| [`backlog.md`](backlog.md) | Queued but not started. Future ADRs, identified work, unblocked-but-paused items. | When new work surfaces (from reviews, ideas, conversations) |

## The maintenance rule (load-bearing)

**At the end of every working session that produces concrete output, update the roadmap in this order:**

1. **`done.md`** — append what was accomplished (date, one-line summary, links).
2. **`current.md`** — replace finished items; promote any newly-active work from `backlog.md`; restate the immediate next concrete step.
3. **`phases.md`** — only if a phase's status, scope, or estimated time changed.
4. **`backlog.md`** — only if new work was identified (review findings, idea-inbox promotions, new ADR topics).

If a session produced **no** concrete output (pure conversation, exploration, dead-end), no roadmap update is required.

## What counts as "concrete output"

- A new ADR or doc commit
- A meaningful edit to existing docs (not typo fixes)
- A new research finding committed to `research/`
- A team / review producing artifacts (like the 2026-05-30 5-perspective review)
- (Once code exists) commits to source, schema, contracts, or reference implementations

## What does NOT belong in the roadmap

- **Ideas not yet committed.** Those go to `ideas/inbox.md`.
- **Architectural decisions.** Those go to `docs/architecture/adrs/`.
- **Conversation summaries.** Those go to `docs/conversations/`.
- **Long technical writeups.** Those go to `docs/architecture/`.

The roadmap is a *project-management* artifact. It tracks "where are we, what's next." It does not duplicate content; it links to it.

## Format conventions

- **Dates are ISO** (`2026-05-30`).
- **Status flags** in tables: `not-started` | `in-flight` | `blocked` | `done` | `paused` | `cancelled`.
- **Phase IDs** are stable: Phase 0, Phase 1, etc. They're referenced from ADRs.
- **Effort estimates** in weeks or months, with a `(low / mid / high)` range when there's real uncertainty.
- **Links use repo-relative paths** (`../docs/...`) so they survive moves.

## When the roadmap conflicts with an ADR

The ADR wins. The roadmap is *operational*; ADRs are *architectural*. If you find the roadmap claiming Phase 0 is 2 months but ADR-001 says 4-6 months, the roadmap is stale — update it. The reverse (ADR stale, roadmap fresh) is a red flag: write a new ADR to capture the change *first*, then mirror to the roadmap.

## The "ignore at your peril" line

A stale roadmap is worse than no roadmap. A future Claude session opens `current.md`, sees "next: write ADR-003," reads the existing ADR-003, gets confused. Update or delete — don't leave half-states. This file exists so the rule is hard to miss.

## Why the roadmap exists in addition to ADRs

ADRs answer *what we decided and why*. The roadmap answers *what we're doing about it and when*. They're orthogonal:

- ADRs are immutable once Accepted (per the ADR rules).
- The roadmap is mutable by design — it reflects current reality, not historical record.

Together: ADRs are the spec; the roadmap is the build plan.
