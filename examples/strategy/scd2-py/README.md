# `rat-strategy-scd2-py` — the second `kind: strategy` reference (SCD2)

The reference that lets `strategy/v1` freeze: a **deliberately divergent** second
implementation of the strategy axis ([ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)).
Where [`fullrefresh-py`](../fullrefresh-py) is a stateless transform-and-replace, **SCD2**
(Slowly Changing Dimension, Type 2) is stateful and temporal — and it stresses the
`strategy/v1` contract along axes full-refresh never touches.

## What it does

For each run it reads the incoming **source snapshot** and the existing **target
history**, then writes a versioned history:

- **new** natural key → insert a current version (`is_current=true`, `effective_from=<run ts>`, `effective_to=""`)
- **changed** tracked columns → close the open version (`is_current=false`, `effective_to=<run ts>`) **and** insert a new current version
- **unchanged** → no-op
- **deleted** from source → close the open version

The version delta is written with a single `format.merge` keyed on
`(natural_key…, effective_from)`: closures match the existing open version and replace
it; new versions are inserts.

## Why it's the right second reference

It exercises a **different capability mix** over the same `Apply(source, target,
options)` contract — the whole point of ADR-003 (the contract must serve both):

| | full-refresh | **SCD2** |
|---|---|---|
| capabilities | `catalog.get-table` · `engine.query` · `format.overwrite` | `catalog.get-table` · `format.scan` (×2) · `format.merge` |
| reads target? | no | **yes** (`format.scan` on the target ref) |
| data-plane role | pure router (passes the engine's stream to the format) | **consumer + producer** (pulls the scans, hosts the synthesized delta) |
| state | stateless | temporal / incremental |

## Contract observations it surfaced (the ADR-003 payoff)

1. **Reading existing target state needs no new RPC** — `format.scan` on the target ref
   already covers it.
2. **A strategy can be a data producer.** SCD2 *synthesizes* the version delta, so it
   hosts those rows on an Arrow stream ([`flight.py`](flight.py) `host_rows`) for
   `format.merge` to pull — like any data-plane producer. Full-refresh never produces
   data, so this stayed hidden until the second strategy.
3. **A strategy can be a data consumer** — it pulls the scan `ArrowStream` itself.
   Together these show a strategy can sit anywhere on the data plane, and the
   contract's two seams (control-plane `invoke` + out-of-band Arrow legs) cover all of it.
4. **Per-run parameters ride in `options`.** The natural key, tracked columns, and the
   run's `effective_from` timestamp travel in `strategy.proto`'s metadata-schema'd
   `options` bytes — the contract's per-run parameter bag; no dedicated fields needed.

None required a contract change → `strategy/v1` is validated by two independent
strategies and freezes ([ADR-009](../../../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md)
trigger).

## How it's tested

Over the **real composition stack** (gateway + parquet format + sqlite catalog + real
Arrow Flight), in [`examples/composition`](../../composition)'s SCD2 phase: two temporal
loads from [`strategy-scd2-v1.json`](../../../contracts/conformance/strategy-scd2-v1.json)
(initial load, then a snapshot with a changed + an unchanged + a new key), asserting the
resulting history. Run `make composition`.

## Files

- [`store.py`](store.py) — the SCD2 logic (pure; `invoke` + `host_rows` + `pull` injected)
- [`server.py`](server.py) — `StrategyService.Apply` gRPC servicer (brings its own Flight host)
- [`flight.py`](flight.py) — the Arrow host/pull legs (SCD2 touches bulk data)
- [`requirements.txt`](requirements.txt) — gRPC + the SDK + pyarrow
