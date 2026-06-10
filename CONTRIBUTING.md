# Contributing to RAT v3

> The repo is currently **local/unpushed and solo-maintained** — this file exists so the
> rules are written down *before* they're needed (and because future-us counts as a
> contributor). Everything here is practiced today, not aspirational.

## Prerequisites

`podman` (or docker) + `make` + `git`. **Nothing installs on the host** — every build,
test, codegen, and lint runs in a pinned container (see the [Makefile](Makefile) header).
First contact: [QUICKSTART.md](QUICKSTART.md).

## The gates

| When | Command | What it proves |
|---|---|---|
| every commit | `make check` | buf lint (seconds; a pre-commit hook runs it when protos are staged) |
| before merging | `make verify` | lint + proto build + SDK freshness + SDK compile + core tests |
| touching `contracts/proto/` | `make breaking` | the change is additive vs the sealed `main` baseline |
| touching a reference plugin | `make conformance` | every reference still passes its golden vectors |
| touching manifests/schemas | `make validate-manifests` | examples + per-kind schemas + the INVALID corpus |
| touching golden vectors | `make validate-vectors` | envelope schema + per-file key registry (a typo'd key is otherwise silently skipped) |

Note the asymmetry by design: the commit hook is **lint-only** (fast); `make verify` is
the full gate. A commit that passes the hook can still fail `verify` — run it before you
consider work done.

## Branching (enforced by a hook)

**Never commit to `main`.** It is the sealed line; it only receives `--no-ff` seal-merges
from topic branches, each tagged `rat/N.M` (annotated). Flow:

```bash
git checkout main && git checkout -b <topic-slug>     # e.g. adr-050-foo, dx-sweep
# … work, commit …
git checkout main && git merge --no-ff <topic-slug>   # message: "seal(rat/N.M): …"
git tag -a rat/N.M -m "…"
```

Naming: descriptive slugs (`adr-049-create-if-absent`, `fix-arrowticket-flake`). The
historical `phase-N` integration branches are from the phased era (0–9) and stay for
archaeology. Full rules: [.claude/rules/git-branching.md](.claude/rules/git-branching.md).

## Commits

Conventional commits: `feat(core):`, `fix(conformance):`, `docs(adr):`, `docs(roadmap):`,
`seal(rat/N.M):` for the merge commits. One logical change per commit; ADRs land
one-per-commit. Co-author Claude when Claude did the writing.

## Where things go

| You produced… | It goes to… |
|---|---|
| an architectural decision | a numbered ADR in [docs/architecture/adrs/](docs/architecture/adrs/) — **before** the implementation |
| a contract change | the ADR + [contracts/AMENDING.md](contracts/AMENDING.md) procedure |
| a new plugin | [docs/guides/authoring-a-plugin.md](docs/guides/authoring-a-plugin.md); references need a conformance harness |
| an idea not actionable now | [ideas/inbox.md](ideas/inbox.md) |
| a session that shaped the design | [docs/conversations/](docs/conversations/) |
| any concrete output | a roadmap update: `done.md` → `current.md` (rules in [roadmap/CLAUDE.md](roadmap/CLAUDE.md)) |

## The one architectural rule

**The core does six things; everything else is a plugin.** If your change adds a seventh
thing to the core, stop and write the ADR proving it cannot be a plugin (it almost
certainly can). See [.claude/rules/plugin-architecture.md](.claude/rules/plugin-architecture.md)
— the temptation ledger in [roadmap/done.md](roadmap/done.md) tracks attempts.

## License

TBD (the license decision is itself a future ADR) — which is the honest reason external
PRs can't be accepted yet.
