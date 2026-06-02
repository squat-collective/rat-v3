# Q02 — review tracker

> Live status of the Q02 external review. Update the table as reviewers are recruited and findings land. Each reviewer's findings go in their own `reviews/11-q02-<name>.md` (template below); the synthesis at the bottom feeds the **Q01** (v2-vs-v3) call.

## Reviewers

| lens | reviewer | brief sent | status | findings doc |
|---|---|---|---|---|
| architect/contracts | _(tbd)_ | [architect](Q02-brief-architect.md) | not started | |
| security | _(tbd)_ | [security](Q02-brief-security.md) | not started | |
| SRE/operability | _(tbd)_ | [SRE](Q02-brief-sre.md) | not started | |
| ecosystem _(optional)_ | _(tbd)_ | [ecosystem](Q02-brief-ecosystem.md) | not started | |

Status flow: `not started` → `reached out` → `accepted` → `reviewing` → `findings in` → `synthesized`.
Target: **architect + security** at minimum; + SRE comfortable. Sourcing: [Q02-reviewer-shortlist.md](Q02-reviewer-shortlist.md).

## Findings-doc template

Copy to `reviews/11-q02-<reviewer-or-lens>.md` when a review comes back:

```markdown
# Q02 external review — <reviewer / lens>

Reviewer: <name / role>  ·  Lens: <architect|security|sre|ecosystem>  ·  Date: <YYYY-MM-DD>

## Bottom line
<their one-paragraph verdict: would you make/bet on this, and the single thing to fix first>

## Findings
### <title>
- Severity: Critical | High | Medium | Low | Nit
- Area: premise | contracts | core | data-plane | operability | ecosystem | prior-art
- Finding: <what's wrong / risky / missing>
- Why it matters: <consequence; ideally a concrete failure scenario>
- Suggested direction: <optional>
- Our response: <triage — accept / dispute / defer; link an ADR or backlog item>
```

## Synthesis (once ≥2 reviews are in)

> Fill this when the reviews land. Then it feeds Q01 + decides whether the freeze can leave local/unpushed.

- **Critical findings (must-resolve):** _(none yet)_
- **Cross-reviewer agreement / disagreement:** _(…)_
- **Freeze-reopen triggers** (any frozen-wire regret a reviewer rates High+): _(…)_ — these are the expensive ones; a confirmed one means a `v2` plan, per [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md).
- **Net read on the bet (feeds Q01):** _(go / adjust / reconsider)_
- **Resulting actions:** _(new ADRs / backlog items / a Phase-1 hotfix)_
