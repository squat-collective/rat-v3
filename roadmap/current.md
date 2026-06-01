# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (🎉 **Phase 0 contract surface COMPLETE** — all 18 axes frozen `v1` (`rat/1`→`rat/1.4`), 32 references conform (`make conformance` 32/32), cross-axis composition green, a 5-agent board review done ([reviews/08](../reviews/08-post-freeze-board-review.md)), and its one V2-regret fixed + `rat/1` re-cut (`0e81314`). **Now: COMPLETE & SEAL Phase 0** — close the four remaining gaps → cut a complete contract **`v1.1`** → then start **Phase 1 (the core)**.)

## Status one-liner

**Phase 0 (lock the contracts) — contract surface is DONE; closing it out.** Every axis contract + the cross-cutting types are frozen at `v1`, backed by 32 conformance-passing references + a real cross-axis composition. The post-freeze board review confirmed the freeze was sound (one V2-regret, now fixed) and produced a precise punch-list. **The chosen next thread (2026-06-01): finish and seal Phase 0** — land the four close-out items below, cut contract `v1.1`, and hand Phase 1 a *complete* target. The board's enforcement + crash-safety findings (C1–C5, D1–D5) are deferred to **Phase 1 as its acceptance criteria** (they only become real once the core exists).

> Commitment-gate note: `phases.md` flags a 12–18mo runway + GTM commitment as a pre-Phase-0 gate. Tom chose to proceed in exploratory/sandbox mode. Gate acknowledged, not formally cleared — revisit before investing the full Phase 1 core build.

## Active stream — Phase 0 close-out (→ cut contract `v1.1`)

**Status:** in-flight (entered 2026-06-01). Ordered by value; (1) and (2) are substantive, (3) is docs, (4) is the tag.

1. ⬜ **Catalog commit-linkage** — the board's #1 functional gap ([reviews/08](../reviews/08-post-freeze-board-review.md) B1; 3 agents' top concern). The branch-pipeline headline feature **cannot close its loop on the frozen wire** — there's no `RegisterTable` / commit RPC, so a strategy can't register a new output table or tell the catalog what `format.Write` landed; the composition only passes because the harness fakes it out-of-band. **Step:** short ADR (additive RPC to a frozen axis) → add `RegisterTable` (+ commit-linkage) to `catalog.proto` (additive, no break) → golden vector → update `examples/composition` to use it instead of out-of-band seeding. *This is the first concrete step.*
2. ⬜ **Manifest schema freeze + per-kind schemas** — `plugin.v1.json` is the **only** remaining `v1-preview` artifact AND the only thing a plugin author hand-writes, and the 18 per-kind schemas don't exist (so the most basic author mistake — wrong required capability for the kind — ships silently; [reviews/08](../reviews/08-post-freeze-board-review.md) E2/#3). **Step:** finalize the envelope schema → derive + author the 18 per-kind `provides`/`requires` schemas from the frozen protos → freeze. (Optional: a `rat plugin validate` stub against the INVALID-examples corpus.)
3. ⬜ **Doc tail** — `overview.md` drift (rename the phantom `plane-manager-plugin` → `deployment-runtime`; add a tier-0 callout; reconcile "core never commands"; reviews/08 E4); the **12 missing control/experience `CONTRACT.md`** (reviews/08 E1); label round-1 refs `WIRE-CONTRACT ONLY — NOT A STARTER TEMPLATE` (E3); start the temptation ledger (E7).
4. ⬜ **Cut contract `v1.1`** — tag the completed, sealed surface; record in `done.md`. *(Optional while re-cutting: fold in the cheap additive crash-safety fields — `idempotency_key`, `ArrowStream` terminator — see backlog; they're additive so they can also wait for Phase 1.)*

**Immediate next concrete step:** item (1) — write the catalog commit-linkage ADR, then add the `RegisterTable`/commit RPC to `catalog.proto` (additive), a golden vector, and wire `examples/composition` to use it. Verify `buf lint/build` + `make conformance` + `make composition` after.

## After this stream — Phase 1 (the core), reframed

Phase 1 builds the 5–10k-LOC core (registry + reconciler + event bus + identity/state/API gateways). The board review gave it **testable exit criteria**: the core isn't "done" until the enforcement + crash-safety findings *pass* —

- **C5** capability enforcement (manifest `requires`/`provides` checked per call) · **C4** audit-on-every-decision incl. denials + stream-terminal · **C3** provider-call deadline propagation · **D1** isolation-profile conformance (a real enforcing deployment-runtime, not dry-run) · **D2/D3** ArrowStream-ticket + storage-cred isolation vectors · **D4** conformance-attestation verification (`declared == conformed`).

i.e. the board converted "the core will enforce X" into a definition of done. Full list: [reviews/08](../reviews/08-post-freeze-board-review.md) + [backlog.md](backlog.md).

## What's NOT in flight

- **Phase 1–5** — not started (Phase 1 begins after the close-out + `v1.1` cut).
- The board's `v1.1` *additive* contract fixes beyond catalog commit-linkage (idempotency key, ArrowStream terminator, terminal audit record, `WriteResult` merge breakdown, `TableRef` snapshot/as-of) — queued in [backlog.md](backlog.md); land opportunistically with the `v1.1` cut or as Phase 1 drives them out.
- The board's *enforcement-layer* findings — deferred to Phase 1 as acceptance criteria (above).

## Maintenance reminder

When this stream produces concrete output: update `done.md` → `current.md` → `phases.md` (if a phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
