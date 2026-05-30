# ADR rules

Architecture Decision Records. See [README.md](README.md) for the template + index.

## Rules

- **One ADR per concept.** No multi-topic ADRs. If the decision has 5 sub-parts, that's fine — but they all serve one *concept*. If the parts diverge, split into multiple ADRs.
- **ADR-first for architectural decisions.** Before changing a contract, a plugin axis, or the core's shape — write the ADR. Don't backfill ADRs from code; that defeats the purpose.
- **Numbered + Status-tracked.** `NNN-kebab-title.md`. Status moves Proposed → Accepted (or Rejected / Superseded by ADR-XYZ). Update the index in README.md when status changes.
- **Immutable once Accepted.** Edit only typos. If the decision changes, write a NEW ADR superseding the old one. The old ADR stays as the historical record + its Status flips to `Superseded by ADR-NNN`.
- **Cross-link aggressively.** Reference other ADRs, v2 ADRs, conversations, prior art. The web of references is the institutional memory.
- **One ADR per commit.** Don't bundle ADR work with implementation. Land thinking separately.

## Required sections

Every ADR must have:
- `Status`
- `Context`
- `Decision`
- `Consequences` (positive + negative — accepted)
- `Alternatives considered` (with rejection reasons)
- `Related`

Optional sections:
- `Open questions` (deferred decisions, numbered Q01, Q02 for traceability)
- `Migration` (if non-trivial)

## Tone

- Direct. The ADR is the place to be opinionated.
- Honest about cost. Every decision has tradeoffs; list them.
- Cite specifics. "We learned from v2 ADR-024 that..." beats "we noticed that...".
- No prevarication. "We're choosing X" not "we may want to consider X."

## When to write a new ADR vs amending an existing one

| Situation | Action |
|---|---|
| Typo or clarification | Edit in place |
| Add a new sub-decision under the same concept | Edit in place (Status stays) |
| Reverse a decision | New ADR, supersedes old |
| Extend scope to a new concept | New ADR |
| Discovered a missing tradeoff | Edit `Consequences` (note the date) |
| Decision turned out wrong in practice | New ADR documenting the learning + supersedes old |
