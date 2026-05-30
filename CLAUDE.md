# 🐀 RAT v3 — A data platform reimagined for the next decade

> *"The core does six things. Everything else is a plugin."*

This project is a from-scratch reimagining of RAT (the v2 codebase living at `~/sandbox/ratatouille-v2/ratatouille/`). It is NOT an evolution — it's a parallel design built on a different premise.

The premise: **a data platform is a minimal control plane that orchestrates self-describing plugins**. State backend, auth, scheduler, UI, engine, format, catalog, storage, deployment runtime — *all* are plugins. The core's job is to validate manifests, reconcile desired state, and route events. Nothing else.

This repo holds the **architectural thinking, ADRs, and ongoing design** for RAT v3 — not yet any product code. The shape of the project today is: docs, decisions, conversations, captured ideas, prior-art notes.

## What RAT v3 IS

- A 5-10k LOC core (Rust or Go) that runs a registry + reconciler + event bus + identity gateway + state gateway + API gateway. **Six things.**
- A 16+ axis plugin model where every load-bearing concern (state, auth, scheduling, deployment, engine, format, catalog, storage, tenancy, billing, observability, UI, …) is a plugin.
- A single contract triple: `.proto` for services + `plugin.yaml` for manifests + URI-shaped capability strings.
- A reconciliation model (K8s for data): operators declare desired state; the core drives convergence; plugins react to events.
- A platform that scales from `chmod +x ./rat` (solo dev) to multi-tenant cloud SaaS — same binary, different plugin sets.

## What RAT v3 IS NOT

- **Not** an evolution of RAT v2. The current ratatouille-v2 codebase has too many baked-in assumptions (postgres-mandatory, ratd-as-orchestrator, portal-as-only-UI) to incrementally evolve. v3 is a *parallel* design.
- **Not** "another data platform." The differentiator is *fully pluggable everything*. If a competitor can do X without a plugin, we did it wrong.
- **Not** a Kubernetes replacement. K8s-shaped contracts (image refs, healthchecks, resource asks) — but for data orchestration, not generic workloads. Sits on top of K8s, docker, podman, lambda — runtime is a plugin axis.
- **Not** an ORM, query language, or warehouse. We don't ship SQL semantics. Engines (DuckDB, ClickHouse, Spark, …) bring their own.
- **Not** scope-creep-friendly. Adding anything to the core is presumed wrong until proven otherwise.

## Working principles

1. **Contracts before code.** Every architectural decision starts with the proto + manifest shape. Implementation comes second. If we can't agree on the contract, we don't agree on the design.
2. **Six-thing-core discipline.** Resist adding anything to the core. When tempted, write an ADR proving why it can't be a plugin. Track the temptation count — that's a leading indicator of architectural drift.
3. **ADR-first for architectural decisions.** Numbered ADRs in `docs/architecture/adrs/`. No "we'll figure it out in code" — write the decision down, including the rejected alternatives.
4. **Honest tradeoff documentation.** Every decision has a cost. Write the cost down in the ADR's `Consequences` section. No design is free.
5. **Reference prior art before reinventing.** OSGi, VSCode, K8s, NATS, Temporal, Cargo, npm — all solved adjacent problems. Read their docs. Cite in `research/prior-art/`. Don't waste cycles re-discovering well-trodden patterns.
6. **Capture ideas where they're born.** Anything that sparks during a conversation → `ideas/inbox.md`. Ideas that become real → promoted to an ADR or design doc. Don't trust memory.
7. **Save the conversations that matter.** Long Claude sessions where architectural shape emerged → distill into `docs/conversations/YYYY-MM-DD-<topic>.md`. Future-us needs to know how we got here.
8. **Test the deployment topology, not the feature.** When we ship code, the test is "can a solo user `chmod +x && ./rat` AND can a hybrid-cloud team compose a plane?" — not "does feature X work." Architecture proves itself across topologies.
9. **Keep the roadmap fresh.** [`roadmap/`](roadmap/) is the single source of truth for *what we're doing, what's done, what's next.* **After every working session that produces concrete output, update the roadmap** in this order: `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). A stale roadmap is worse than no roadmap. Full rules in [`roadmap/CLAUDE.md`](roadmap/CLAUDE.md).

## Directory map

```
rat/
├── CLAUDE.md                 # this file
├── README.md                 # human-facing project overview
├── docs/
│   ├── vision.md             # the core thesis (read this first)
│   ├── architecture/
│   │   ├── overview.md       # the full architecture in one document
│   │   └── adrs/             # numbered architectural decisions
│   │       ├── README.md     # ADR index + template
│   │       ├── 001-everything-is-a-plugin.md
│   │       ├── 002-founding-tech-stack.md
│   │       └── 003-two-references-before-contract-freeze.md
│   └── conversations/        # distilled Claude sessions
│       └── YYYY-MM-DD-*.md
├── reviews/                  # adversarial reviews of the architecture
│   └── 00-synthesis.md       # multi-perspective synthesis (read second to vision.md)
├── roadmap/                  # what we're doing, what's done, what's next
│   ├── CLAUDE.md             # roadmap maintenance rules
│   ├── README.md             # entry point
│   ├── current.md            # ← always-current; read on every new session
│   ├── phases.md             # phased plan (Phase 0 → 5)
│   ├── done.md               # completion log (reverse chronological)
│   └── backlog.md            # queued work
├── ideas/
│   ├── inbox.md              # capture-as-you-go
│   └── CLAUDE.md             # ideas rules
└── research/
    ├── prior-art/            # K8s, OSGi, etc. — what to learn from
    └── competitors.md        # data platform landscape
```

## How to work on it

**Reading order for a new session:**
1. This file (you're here).
2. **[`roadmap/current.md`](roadmap/current.md)** — what's in flight + the immediate next step. Always read this; it tells you what to do.
3. `docs/vision.md` — the *why*.
4. `docs/architecture/overview.md` — the *what*.
5. Latest entry in `docs/conversations/` — the *how we got here*.
6. `reviews/00-synthesis.md` — adversarial review findings that shaped the current direction.
7. `ideas/inbox.md` — the *what's bubbling*.

**Capture flow:**
- New idea? → `ideas/inbox.md`.
- New architectural decision? → new ADR in `docs/architecture/adrs/`.
- Long session that shaped the design? → new entry in `docs/conversations/`.
- New research finding? → `research/prior-art/<topic>.md`.

**Commit discipline:**
- Doc-only commits are fine + encouraged. Land thinking as it solidifies.
- One commit per logical decision. ADRs land one-per-commit.
- Conventional commits: `docs(adr):`, `docs(vision):`, `docs(arch):`, `ideas:`, `research:`.
- Co-author Claude where Claude did the writing (we do here).

**When code finally lands:**
- Contracts first: `.proto` + `plugin.yaml` schemas before any implementation.
- Reference plugin per axis as forcing functions for the contracts.
- Core last: implement the core only after enough plugins exist to stress-test it.

## Relationship to the v2 codebase

The current RAT (`~/sandbox/ratatouille-v2/ratatouille/`) is the production project Tom's been shipping. It's *very good* — has ADRs 024-026 driving its own decoupling. RAT v3 is the bigger bet: what if we started with the lessons of v2 instead of evolving v2?

v3 doesn't replace v2 day one. The two coexist:
- v2 continues to ship features + ADRs 025/026 (which sharpen its decoupling story).
- v3 grows from architecture → contracts → reference plugins → core.
- Eventually (12-18 months?) v3 reaches feature parity and v2 becomes the "v1" we maintain in parallel.

**Many architectural ideas in v3 originate as ADRs in v2.** Cross-reference shamelessly. When v3 adopts a v2 ADR's pattern, cite it. When v3 diverges from v2, write *why* — that's the load-bearing learning.

## Parent CLAUDE config

The user-wide config at `~/CLAUDE.md` still applies — podman-not-host, ideas-go-to-`~/ideas.md`-for-cross-project, HAL-9000 for AI work, etc. **Project-specific** ideas live in `ideas/inbox.md` here.

---

🐀 *"The core does six things. Everything else is a plugin." Repeat until it's instinct.*
