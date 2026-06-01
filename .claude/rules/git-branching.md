# Git branching — always work on a nice branch

> No `paths:` on purpose. Loads every session. Codifies the RAT v3 branching
> discipline as we enter Phase 1 (the Go core build). Companion to the `main`-guard
> in [`.claude/hooks/contracts-check.sh`](../hooks/contracts-check.sh).
> Doc basis: https://code.claude.com/docs/en/settings.md (CLAUDE.md hierarchy, always-load rules).

## The rule

**Never commit directly to `main`.** `main` is the sealed-state line; commits land
there only as a phase-seal merge. For all active work, you are on a branch. A
`PreToolUse` hook enforces this deterministically — but the discipline is yours first.

## Branch topology

```
main                          ← sealed phases only; tagged rat/N.M (rat/1.5 = Phase 0)
 └── phase-1                  ← long-lived integration branch for the Phase 1 core build
      ├── phase-1-<slug>      ← short-lived topic branches forked off phase-1
      └── phase-1-<slug>        merge back to phase-1 --no-ff; never directly to main
```

- **`main` is the sealed-state line.** Phase-seal tags (`rat/1.5`, `rat/2.0`, …) point at
  commits on `main`. It also carries foundational `.claude/` scaffolding (this rule + the
  hook) as the phase baseline. Phase *content* lands on `main` only via a phase-seal merge.
- **`phase-N` is the living integration branch.** All of a phase's work accumulates here.
  It stays open for the whole phase (months). When the phase's exit criteria pass, `phase-N`
  merges to `main` as one merge commit and the next `rat/` tag is cut.
- **`phase-N-<slug>` sub-branches** are short-lived. They fork off `phase-N`, do one
  ADR / subsystem / contract iteration, and merge back into `phase-N` — never to `main`.

## Why hyphens, not slashes (`phase-1-x`, not `phase-1/x`)

Git stores refs as files under `.git/refs/heads/`, so a ref **cannot be both a file and a
directory**. A branch literally named `phase-1` blocks creating `phase-1/anything` (the
"directory/file conflict": `fatal: cannot lock ref … 'phase-1' exists`). Since we want
`phase-1` to *be* the integration branch, sub-branches use a **hyphen** separator. Flat refs,
no conflict, still visually grouped (`phase-1-*` sort together). *Learned by dogfooding —
the first sub-branch creation failed; see ADR-013 / reviews/09.*

## Naming conventions

| Branch | Pattern | Example |
|--------|---------|---------|
| Phase integration | `phase-N` | `phase-1` |
| Topic / subsystem | `phase-N-<slug>` | `phase-1-registry-core` |
| ADR-driven | `phase-N-adr-NNN-<short-title>` | `phase-1-adr-013-commitment-gate` |
| Phase hotfix | `phase-N-hotfix-<slug>` | `phase-1-hotfix-reconciler-panic` |

Slug rules: lowercase, hyphens only (no slashes — see above). Prefer ADR-numbered branch
names when the work is ADR-driven, so the branch is self-describing.

## Merge rules

- `phase-N-<slug>` → `phase-N`: merge commit (no fast-forward — keeps topology readable).
  ```bash
  git checkout phase-N
  git merge --no-ff phase-N-<slug>
  ```
- `phase-N` → `main`: **only** when the phase's exit criteria pass (Phase 1: C3–C5, D1–D4 +
  sre#4). Merge commit, then tag immediately.
  ```bash
  git checkout main
  git merge --no-ff phase-N
  git tag rat/2.0
  ```
  The phase-seal uses `git merge` + `git tag` (not `git commit`), so the `main`-guard does
  not block it — the guard blocks only direct `git commit` on `main`.
- Never `git rebase` a sub-branch onto `main` directly. Rebase is fine *within* a sub-branch
  before merging it into `phase-N`.

## Tag convention (preserved)

`rat/N.M` — phase-major / iteration-minor. Phase 0 sealed at `rat/1.5`; Phase 1 seals at
`rat/2.0`. Tags are annotated and point at commits on `main`. Tags are branch-independent —
renaming/branching never touches them.

## Recommended session start

```bash
git branch                     # confirm you are NOT on main
git log --oneline -5 phase-1   # orient to what's landed
```

If you find yourself on `main` with uncommitted work, stash and switch:
```bash
git stash && git checkout phase-1 && git stash pop
```

## What this rule does NOT govern

- Commit message format → [`CLAUDE.md`](../../CLAUDE.md) (conventional commits, Claude co-author).
- Roadmap updates → [`roadmap/CLAUDE.md`](../../roadmap/CLAUDE.md).
- Contract conformance gate → [`.claude/hooks/contracts-check.sh`](../hooks/contracts-check.sh).

## Related

- [`.claude/rules/claude-environment.md`](claude-environment.md) — the built-in-first discipline this follows.
- [`.claude/hooks/contracts-check.sh`](../hooks/contracts-check.sh) — hosts both the contract gate and the `main`-guard.
- ADR-013 / reviews/09 — where this branching model was adopted (and the slash bug caught).
