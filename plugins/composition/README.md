# Cross-axis composition test — the ADR-003 "run against each other" gate

> This is **sub-phase 0i**: the cross-combination integration test that
> [ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
> requires before the data-plane contracts can freeze (`rat/1`). The per-axis
> conformance suite (`make conformance`) proves each axis works *alone*; this proves
> the axes **compose** — which the [freeze review](../../reviews/07-freeze-review.md)
> (Part C) flagged as the one ADR-003 clause not yet met.

## What it does

`make composition` boots the data-plane reference plugins **together** and runs a real
pipeline across them, for each of ADR-003's four cross-combinations:

| combo | engine | format | catalog | (storage) |
|---|---|---|---|---|
| baseline | DuckDB | Parquet | sqlite | local-fs (held A) |
| format-sub | DuckDB | **Delta** | sqlite | local-fs |
| catalog-sub | DuckDB | Parquet | **in-memory** | local-fs |
| engine-sub | **DataFusion** | Parquet | sqlite | local-fs |

The pipeline is driven by the **full-refresh strategy reference**
([`plugins/strategy/fullrefresh-py`](../strategy/fullrefresh-py)), which couples to
nothing by name — only by capability URI, through a mediating gateway:

```
strategy.Apply
  ├─ rat://catalog/v1/get-table       resolve the source logical name → TableRef
  ├─ rat://catalog/v1/register-table  create the pipeline's OWN output table (ADR-010)
  ├─ rat://engine/v1/query            run the transform SQL, binding the source ref…
  │     └─ rat://format/v1/scan       …which the engine resolves via the format, pulling
  │                                    the source as REAL Arrow over Flight, then streams
  │                                    its transformed result back over Flight
  ├─ rat://format/v1/overwrite        write the result stream into the target
  └─ rat://catalog/v1/commit-table    record WHICH snapshot the write produced (commit-
                                       linkage) — the create→write→register loop closed
```

Every combination must produce the **identical** target
([`composition-v1.json`](../../contracts/conformance/composition-v1.json) →
`expected_target`). The strategy code never changes across the substitutions — that
invariance, proven on golden data, is the gate.

### Plus: the strategy axis's two references

After the cross-axis matrix, `make composition` runs a **second phase** that exercises
the SECOND strategy reference — [`scd2-py`](../strategy/scd2-py) — over the same real
stack (gateway + parquet + sqlite + Flight): two temporal loads from
[`strategy-scd2-v1.json`](../../contracts/conformance/strategy-scd2-v1.json), asserting
the SCD2 history. Two independent strategies (full-refresh + SCD2) over one
`strategy/v1` contract is the ADR-003 two-reference gate for the strategy axis — what
lets it freeze (`rat/1.1`).

## Why this is a faithful gate (and what's a stand-in)

- **Real cross-axis Arrow handoff over Flight.** The engine↔format legs are real
  `pyarrow.flight` round-trips over TCP sockets ([`flight.py`](flight.py)) — the exact
  handoff [engine.proto](../../contracts/proto/rat/engine/v1/engine.proto) calls out as
  "where 'fits != works' bites hardest." The data is genuine typed Arrow produced by
  real engines and written by real table formats.
- **Real backends.** Parquet/Delta files on disk, sqlite catalog, DuckDB/DataFusion
  SQL — the per-axis references' actual stores, imported unchanged.
- **Decoupled by capability.** The [gateway](gateway.py) resolves every call from the
  provider's `(rat.common.v1.capability)` annotations; no plugin names appear in the
  strategy or the engine.
- **Stand-in:** the gateway routes *typed* requests rather than the opaque byte-relay
  the Go stub proved ([state/inmemory-go](../state/inmemory-go/gateway_test.go) +
  ADR-005/007/008) — the subject here is cross-*axis data flow*, not the relay
  mechanics. The strategy runs in-process (it is the caller); the three providers are
  real gRPC servers it reaches only through the gateway.

## Findings surfaced (the ADR-003 payoff)

Composition surfaced real cross-axis assumptions the per-axis suites could not:

1. **Engine SUM result type diverges (DuckDB vs DataFusion).** DuckDB's `SUM(int)`
   yields a 128-bit decimal/hugeint; DataFusion's yields `int64`. An engine
   substitution would therefore change the *result schema* — and Delta is strict about
   schema. The golden `transform_sql` pins it with `CAST(SUM(amount) AS BIGINT)`, so any
   conformant engine emits an identical `int64` column. This is a contract-usage
   lesson: **cross-engine portability requires explicit result typing**, invisible until
   two engines run the same pipeline.
2. **The engine's `tables` binding + Arrow transport were not actually exercised
   per-axis.** The engine references ignored `QueryRequest.tables` and carried results
   on an in-process stand-in incompatible with the format's real Flight. Composition
   forced the intended behavior ([`comp_engine.py`](comp_engine.py)): resolve each
   source ref via `format.scan`, bind it, stream results over real Flight.
3. **The catalog had no create-table / commit-linkage RPC — now CLOSED on-wire
   ([ADR-010](../../docs/architecture/adrs/010-catalog-commit-linkage.md)).** Originally
   `GetTable` resolved only *pre-existing* tables, so the harness seeded the source +
   target by poking the catalog's private store — the freeze review's residual **R3** /
   [reviews/08](../../reviews/08-post-freeze-board-review.md) **B1**. With the additive
   `RegisterTable` + `CommitTable` RPCs, the strategy now **creates its own output table
   and records the snapshot it wrote through the gateway**: only the pre-existing *source*
   is admin-registered (via the catalog's public api), and the harness asserts
   `GetTable(target)` succeeds *after* the run — proving the catalog learned the
   pipeline's output on the wire, not from out-of-band poking. The create→write→register
   loop closes on the frozen surface.

None of the first two is a wire-breaking flaw — they are usage/conformance lessons; the
third was the one real functional hole and is now resolved additively. The four
combinations pass, and the ADR-003 cross-combination gate is **met**.

## Run it

```sh
make composition
```

Containerized (python:3.12, union of the real backends' deps); no host installs.
Exit 0 iff all four combinations produce the identical target.
