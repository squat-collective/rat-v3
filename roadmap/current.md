# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-06-01 (🎉 **Phase 0 contract surface COMPLETE** — all 18 axes frozen `v1` (`rat/1`→`rat/1.4`), 32 references conform (`make conformance` 32/32), cross-axis composition green, a 5-agent board review done ([reviews/08](../reviews/08-post-freeze-board-review.md)), and its one V2-regret fixed + `rat/1` re-cut (`0e81314`). **Now: COMPLETE & SEAL Phase 0** — close the four remaining gaps → cut a complete contract **`v1.1`** → then start **Phase 1 (the core)**. 🎉 **ALL FOUR close-out items DONE** ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md) catalog commit-linkage · [ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md) manifest freeze + per-kind schemas · reviews/08 E1/E3/E4/E7 doc tail · [ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields-v1.1.md) C1/C2 crash-safety) — **`rat/1.1` cut, PHASE 0 SEALED.** Now: **Phase 1 (the core).**)

## Status one-liner

**Phase 0 (lock the contracts) — 🎉 COMPLETE & SEALED (`rat/1.1`).** Every axis contract + the cross-cutting types are frozen, backed by 32 conformance-passing references + a real cross-axis composition. The post-freeze board review's punch-list is **cleared**: catalog commit-linkage ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md)), manifest freeze + 18 per-kind schemas ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md)), all 18 `CONTRACT.md` + the doc tail (E1/E3/E4/E7), and the C1/C2 crash-safety additives ([ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields-v1.1.md)) — and **`rat/1.1` is cut over the sealed surface**. **Next: Phase 1 (the core).** The board's remaining enforcement + crash-safety findings (**C3–C5, D1–D5**) are Phase 1's acceptance criteria (they only become real once the core exists).

> Commitment-gate note: `phases.md` flags a 12–18mo runway + GTM commitment as a pre-Phase-0 gate. Tom chose to proceed in exploratory/sandbox mode. Gate acknowledged, not formally cleared — revisit before investing the full Phase 1 core build.

## Completed stream — Phase 0 close-out (→ `rat/1.1`, SEALED 2026-06-01)

**Status:** ✅ **DONE** — all four items landed + committed; `rat/1.1` cut. (Kept here for one session as the record of what sealed Phase 0; moves to history next session.)

1. ✅ **Catalog commit-linkage** — **DONE 2026-06-01 ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))**. The board's #1 functional gap ([reviews/08](../reviews/08-post-freeze-board-review.md) B1; 3 agents' top concern) is closed: additive `RegisterTable` + `CommitTable` RPCs on `catalog/v1` let a strategy create its own output table and record the snapshot `format.Write` produced (commit-linkage, with `MergeBranch`'s CAS + idempotency safety). `catalog.proto` +2 RPCs/+4 msgs (additive, `buf breaking` clean); all 4 SDKs regen'd; 3 catalog refs + 6 golden lifecycle steps; the composition now closes the create→write→register→merge loop on-wire (no out-of-band seeding). `make conformance` **32/32** + `make composition` ✅. Resolves ADR-009 residual R3. *(Staged; commit pending.)*
2. ✅ **Manifest schema freeze + per-kind schemas** — **DONE 2026-06-01 ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md))**. `plugin.v1.json` frozen at `v1` (the last `v1-preview` artifact, gone); rolled `(rat.capability)` across the 12 unannotated axes (additive, SDKs regen'd) so all 18 capability sets are machine-readable; authored 18 per-kind schemas (`schema/kinds/`) with minimal-mandatory-core `provides` rules — the wrong/missing-required-capability mistake now fails fast. New `scripts/validate-manifests.py` + `make validate-manifests` gate (32/32) is the static half of `rat plugin validate`. `make conformance` **32/32**. *(Staged; commit pending.)*
3. ✅ **Doc tail** — **DONE 2026-06-01** (reviews/08 E1/E3/E4/E7). All 18 axes now have a `CONTRACT.md` (12 authored via parallel subagents; caps verified against the protos, links resolve); `overview.md` drift fixed (phantom `plane-manager-plugin` → declarative deployment-runtime convergence + a tier-0 callout); the temptation ledger exists (count 0, pinned in `done.md`); 13 round-1 `inmemory-py` READMEs labeled `WIRE-CONTRACT REFERENCE`. *(Staged; commit pending.)*
4. ✅ **Cut contract `v1.1`** — **DONE 2026-06-01 (with [ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields-v1.1.md)).** Folded the cheap additive crash-safety fields into the seal — **C1** write-leg idempotency (`idempotency_key` on format writes + `strategy.Apply`, `already_applied` on `WriteResult`) + **C2** `ArrowStream` completeness (`expected_rows`/`expected_batches`) — demonstrated end-to-end in the composition (idempotent replay across all 4 combos + a truncation negative). Then tagged **`rat/1.1`** over the sealed surface. `make conformance` **32/32** · `make composition` ✅ · `make validate-manifests` **32/32**.

**Immediate next concrete step:** **Phase 1 — the core.** Build the 5–10k-LOC Go core (registry + reconciler + event bus + identity/state/API gateways). Its definition of done is the board's deferred findings becoming *passing acceptance tests*: **C3** provider-call deadline propagation · **C4** terminal audit record (incl. denials) · **C5** capability enforcement (manifest `requires`/`provides` checked per call) + a crash-mid-strategy composition case · **D1** isolation-profile conformance (a real enforcing deployment-runtime, not dry-run) · **D2/D3** ArrowStream-ticket + storage-cred isolation vectors · **D4** conformance-attestation verification (`declared == conformed`). See [phases.md](phases.md) Phase 1 + [backlog.md](backlog.md). *(Re-confirm the commitment gate above before the full core build.)*

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
