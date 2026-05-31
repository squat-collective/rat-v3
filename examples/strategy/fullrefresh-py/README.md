# `rat-strategy-fullrefresh-py` — the first `kind: strategy` reference

The strategy axis is "the cleanest expression of the capability model"
([strategy.proto](../../../contracts/proto/rat/strategy/v1/strategy.proto),
[reviews/02](../../../reviews/02-plugin-ecosystem-builder.md)): a strategy `requires`
capabilities and works across **every** provider that offers them, naming none. This
reference makes that concrete — and is the forcing function that lets the cross-axis
[composition test](../../composition) exist (the ADR-003 cross-combination gate).

## What it does

A **full refresh**: read a source, transform it with SQL, **overwrite** a target. The
whole implementation ([`store.py`](store.py)) depends on exactly one thing — an
`invoke(capability_uri, request) → response` seam (the core capability-invoke gateway,
[ADR-005](../../../docs/architecture/adrs/005-capability-invocation-model.md)). It
composes three capabilities **by URI**:

| capability | what the strategy does with it |
|---|---|
| `rat://catalog/v1/get-table` | resolve the source + target logical names → `TableRef`s |
| `rat://engine/v1/query` | run the transform SQL, handing the engine the source ref to bind |
| `rat://format/v1/overwrite` | write the engine's result Arrow stream into the target |

It declares these in `REQUIRES` (the manifest `requires` set); the gateway denies
anything outside it (C5). It holds no stub, port, or class of any concrete
engine/format/catalog — so DuckDB↔DataFusion, Parquet↔Delta, sqlite↔in-memory catalog
all swap underneath **without changing this code**. That invariance, proven on golden
data, is what the composition test verifies.

## Not a per-axis conformance ref

Unlike the other references, there is no `harness_test.py` here: a strategy's
correctness is inherently *cross-axis* (it has no behavior without the providers it
orchestrates), so its conformance is the [composition test](../../composition)
(`make composition`), not a solo golden-vector run. `store.py` holds the orchestration
the composition exercises over the real gateway; [`server.py`](server.py) is the thin
`StrategyService` gRPC wrapper for when it runs as a standalone plugin.

## Files

- [`store.py`](store.py) — the full-refresh orchestration (the reference substance)
- [`server.py`](server.py) — `StrategyService.Apply` gRPC servicer over the store
- [`requirements.txt`](requirements.txt) — just gRPC + the SDK (no backend deps)
