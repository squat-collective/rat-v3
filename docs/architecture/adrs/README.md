# ADRs — RAT v3

Architecture Decision Records. One ADR per concept; no multi-topic ADRs. Numbered sequentially; status moves Proposed → Accepted (or Rejected / Superseded).

## Index

| # | Title | Status | Date |
|---|---|---|---|
| [001](001-everything-is-a-plugin.md) | Everything is a plugin | Accepted | 2026-05-30 |

## Template

```markdown
# ADR-NNN: Short title

## Status: Proposed | Accepted | Rejected | Superseded by ADR-XYZ (date)

## Context

What forces are in play? What's the problem? What did we learn from the v2 ADRs (or other prior art) that's relevant here?

## Decision

What we decided. Be specific. Show schema / protocol / code shape where it helps.

If the decision has sub-parts, use level-3 headings:
### 1. Sub-decision A
### 2. Sub-decision B

## Consequences

**Positive.** What we gain.

**Negative — accepted.** What it costs, listed honestly.

**Neutral.** What's different but value-neutral.

## Open questions

Things deferred to future ADRs. Number them Q01, Q02, etc. so they're trackable.

## Alternatives considered

Each option we looked at + why we rejected it. Future-us needs the rejection rationale, not just the chosen design.

## Migration

How we get from current state to this decision. Phase-by-phase if non-trivial. If "this is the design from day 1," say so.

## Related

- Other ADRs (in this repo or v2)
- Proto files, schema docs
- Prior-art references in `research/prior-art/`
- Conversations that shaped this decision
```

## Discipline

- **ADR-first for architectural decisions.** If the change affects a contract, a plugin axis, or the core's shape — write an ADR before code.
- **One ADR per commit.** ADRs land cleanly; no commit mixes an ADR with implementation.
- **ADRs are immutable once Accepted.** Edit only typos. If the decision changes, write a new ADR that supersedes the old one and update the old one's Status.
- **Reject is a valid status.** ADRs we considered and rejected stay in the index — that's the record of "we already thought about this."
- **Cross-link aggressively.** Cite other ADRs, v2 ADRs, prior art, conversations. The web of references is the institutional memory.

## Numbering

- ADR numbers are zero-padded to 3 digits (`001`, `002`, …).
- Numbers are assigned at PR time, not at draft time, to avoid collisions.
- Drafts use `XXX-title.md` until merged.
