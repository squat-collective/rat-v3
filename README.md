# 🐀 RAT v3

A from-scratch reimagining of the RAT data platform.

**Premise:** a data platform is a minimal control plane that orchestrates self-describing plugins. The core does six things; everything else — state backend, auth, scheduler, UI, engine, format, catalog, storage, deployment runtime, tenancy, billing, observability — is a plugin.

**Status (2026-06):** **Phase 0 (contracts) and Phase 1 (the core) are SEALED** — `rat/1.5` and `rat/2.0`.
- **Phase 0** froze the 18-axis contract surface (`.proto` + per-kind manifest schemas + the `rat://` capability grammar), with two technologically-divergent reference implementations per data-plane axis before freeze (ADR-003) and golden-data conformance vectors.
- **Phase 1** built a real, tested control-plane core that enforces all nine board exit criteria (C1/C3/C4/C5, D1/D2/D3/D4, sre#4) **against real launched plugins** — capability authz + audit + deadline-bounding at the gateway, two enforcing deployment-runtimes (incl. podman full-isolation), a supervisor, a reconciler with leader-election + crash-loop backoff, conformance attestation, and the Arrow bytes-leg ticket. The frozen wire held throughout (`make breaking` clean).

**Next gate:** **Q02 — external peer review** (the kit is ready in [reviews/](reviews/); recruiting is the only remaining step). The freeze stays **local/unpushed** until then, and Phase 2+ are user-pull-gated. Live state is always in [roadmap/current.md](roadmap/current.md).

## What's here

| dir | what |
|---|---|
| [`core/`](core/) | the Go control-plane core (the six things + cross-cutting enforcement) — sealed at `rat/2.0` |
| [`contracts/`](contracts/) | the frozen `.proto` wire + generated SDKs + golden-data conformance vectors |
| [`plugins/`](plugins/) | 30+ reference plugins across the 18 axes |
| [`docs/`](docs/) | the vision, the architecture overview, and the numbered ADRs |
| [`reviews/`](reviews/) | the internal adversarial reviews + the Q02 external-review kit |
| [`roadmap/`](roadmap/) | where we are / what's done / what's next ([current.md](roadmap/current.md) first) |

## Read in this order

1. [roadmap/current.md](roadmap/current.md) — **where the project is right now** (read this first).
2. [docs/vision.md](docs/vision.md) — the thesis: why this design, why now.
3. [docs/architecture/overview.md](docs/architecture/overview.md) — the full architecture in one document.
4. [docs/architecture/adrs/](docs/architecture/adrs/) — the numbered architectural decisions.
5. [reviews/00-synthesis.md](reviews/00-synthesis.md) — the adversarial review that shaped the design.
6. [core/README.md](core/README.md) — the built core.

## Working on this project

See [CLAUDE.md](CLAUDE.md) for the working principles, capture flow, and commit/branching discipline. The short version: **contracts before code, ADR for every architectural decision, keep the roadmap fresh, work on `phase-N` branches (never `main`).**

## Relationship to RAT v2

The currently-shipping RAT (`~/sandbox/ratatouille-v2/ratatouille/`) continues in production. RAT v3 is parallel — built on lessons from v2, with a different premise. They coexist; v3 doesn't replace v2 day one.

## License

TBD (license decision is itself a future ADR).
