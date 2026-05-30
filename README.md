# 🐀 RAT v3

A from-scratch reimagining of the RAT data platform.

**Premise:** a data platform is a minimal control plane that orchestrates self-describing plugins. The core does six things; everything else — state backend, auth, scheduler, UI, engine, format, catalog, storage, deployment runtime, tenancy, billing, observability — is a plugin.

**Status:** architecture-only. No product code yet. We're building the design first; implementation follows once the contracts are right.

## Read in this order

1. [docs/vision.md](docs/vision.md) — the thesis: why this design, why now.
2. [docs/architecture/overview.md](docs/architecture/overview.md) — the full architecture in one document.
3. [docs/architecture/adrs/](docs/architecture/adrs/) — numbered architectural decisions.
4. [docs/conversations/](docs/conversations/) — distilled design conversations.
5. [ideas/inbox.md](ideas/inbox.md) — open thinking, captured as it sparks.

## Working on this project

See [CLAUDE.md](CLAUDE.md) for the working principles, capture flow, and commit discipline. The short version: **contracts before code, ADR for every architectural decision, capture ideas where they're born, save the conversations that matter.**

## Relationship to RAT v2

The currently-shipping RAT (`~/sandbox/ratatouille-v2/ratatouille/`) continues in production. RAT v3 is parallel — built on lessons from v2, with a different premise. They coexist; v3 doesn't replace v2 day one.

## License

TBD (this is a design repo; license decision is itself a future ADR).
