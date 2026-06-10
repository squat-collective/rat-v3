# 🐀 RAT v3

A from-scratch reimagining of the RAT data platform.

**Premise:** a data platform is a minimal control plane that orchestrates self-describing plugins. The core does six things; everything else — state backend, auth, scheduler, UI, engine, format, catalog, storage, deployment runtime, tenancy, billing, observability — is a plugin.

**⚡ Want to see it run first? [QUICKSTART.md](QUICKSTART.md) — five minutes, every command verified.**

**Status (2026-06-10):** **Phases 0–9 are SEALED**; `main` is the sealed line at **`rat/6.13`**.
- **Phase 0** (`rat/1.5`) froze the 18-axis contract surface (`.proto` + per-kind manifest schemas + the `rat://` capability grammar), with two technologically-divergent reference implementations per data-plane axis before freeze (ADR-003) and golden-data conformance vectors.
- **Phase 1** (`rat/2.0`) built a real, tested control-plane core that enforces all nine board exit criteria **against real launched plugins** — capability authz + audit + deadline-bounding at the gateway, two enforcing deployment-runtimes (incl. podman full-isolation), a supervisor, a reconciler with leader-election + crash-loop backoff, conformance attestation, and the Arrow bytes-leg ticket.
- **Phases 2–9** (`rat/2.5`–`6.0`) made it a product: the one `rat` binary (projects, plugin authoring + packaging, marketplace with ed25519 signing, live control), three platform topologies (launch / attach / socket-mount), and the data-platform bundle. The `rat/6.x` line then landed the **7 core hardenings** (ADRs 042–048) and the `state/v1` **create-if-absent** amendment + its full adoption (ADR-049) — every change additive on the frozen wire (`make breaking` clean throughout).

**Gates:** the user-pull gates still bind ([roadmap/phases.md](roadmap/phases.md), Gate B+) before broad new product phases. Q02 (external *human* peer review) is owed but set aside as impractical for a solo dev — validated practically instead (the data-dev-plane experiment, since extracted to the `rat-data-dev` repo). The repo stays **local/unpushed** for now. Live state is always in [roadmap/current.md](roadmap/current.md).

## What's here

| dir | what |
|---|---|
| [`QUICKSTART.md`](QUICKSTART.md) | RAT in five minutes — build, boot, call, get refused by C5, read the audit |
| [`core/`](core/) | the Go control-plane core (the six things + cross-cutting enforcement) — sealed `rat/2.0`, hardened through `rat/6.13` |
| [`contracts/`](contracts/) | the frozen `.proto` wire + per-axis `CONTRACT.md` author guides + committed SDKs + golden vectors ([amending procedure](contracts/AMENDING.md)) |
| [`plugins/`](plugins/) | 40 reference + demo plugins across the 18 frozen axes |
| [`platform/`](platform/) | the batteries-included data-platform demo (a dbt medallion driven through the audited gateway) |
| [`docs/`](docs/) | the vision, the architecture overview, the numbered ADRs + the [how-to guides](docs/guides/) |
| [`reviews/`](reviews/) | the internal adversarial reviews + the Q02 external-review kit |
| [`roadmap/`](roadmap/) | where we are / what's done / what's next ([current.md](roadmap/current.md) first) |

## Read in this order

1. [QUICKSTART.md](QUICKSTART.md) — **run it first** (five minutes); reading about a platform you've already touched is twice as fast.
2. [roadmap/current.md](roadmap/current.md) — where the project is right now.
3. [docs/vision.md](docs/vision.md) — the thesis: why this design, why now.
4. [docs/architecture/overview.md](docs/architecture/overview.md) — the full architecture in one document.
5. [docs/guides/](docs/guides/) — *doing* things: [authoring a plugin](docs/guides/authoring-a-plugin.md) · [building a platform](docs/guides/building-a-platform.md) · [amending the contracts](contracts/AMENDING.md).
6. [docs/architecture/adrs/](docs/architecture/adrs/) — the numbered architectural decisions (ADR-001/002/003 are the load-bearing three; the rest are reference).
7. [reviews/00-synthesis.md](reviews/00-synthesis.md) — the adversarial review that shaped the design.
8. [core/README.md](core/README.md) — the built core.

## Working on this project

See [CONTRIBUTING.md](CONTRIBUTING.md) for the practical rules and [CLAUDE.md](CLAUDE.md) for the working principles + capture flow. The short version: **contracts before code, ADR for every architectural decision, keep the roadmap fresh, and never commit to `main`** — it only receives `--no-ff` seal-merges from topic branches (a hook enforces this). Current active work: see the branching section of [roadmap/current.md](roadmap/current.md).

## Relationship to RAT v2

The currently-shipping RAT (`~/sandbox/ratatouille-v2/ratatouille/`) continues in production. RAT v3 is parallel — built on lessons from v2, with a different premise. They coexist; v3 doesn't replace v2 day one.

## License

TBD (license decision is itself a future ADR).
