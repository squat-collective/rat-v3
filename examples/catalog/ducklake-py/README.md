# rat-catalog-ducklake-py — the data-dev plane `catalog` (DuckLake)

> 🛰️ **Exploratory** — part of the [data-dev plane experiment](../../../experiments/data-dev-plane/README.md).
> Additive: the frozen `catalog/v1` surface is unchanged.

A [DuckLake](https://ducklake.select)-backed `rat://catalog/v1` plugin. DuckLake is a
lakehouse format that keeps **all** table metadata (schema, snapshots, file lists) in a
**SQL database**, with data as **Parquet on object storage** — so snapshots,
time-travel and ACID come for free as SQL transactions. It **subsumes the `format`
axis** (it writes the Parquet *and* records it in one transaction), so this stack has
no separate `format` plugin (experiment README §4).

## The catalog/engine boundary — the §10(b) resolution

DuckLake unifies write+commit in one DuckDB transaction, while RAT separates `engine`
(compute) from `catalog` (metadata). This plugin takes the **README §10(b)** path:

- the **engine** ([`duckdb-ml-py`](../../engine/duckdb-ml-py)) owns compute — it creates
  the table (DDL), writes the data, and *produces the DuckLake snapshot*, returning it
  in `WriteResult.snapshot_id`;
- the **catalog** (this plugin) owns the RAT-axis metadata view — it **records** what
  landed (`CommitTable`) and **resolves** refs (`GetTable`).

Both attach the **same** DuckLake (shared metadata DB + same Parquet data path).

| catalog/v1 RPC | realization | backing |
|---|---|---|
| `GetTable` | verify table exists (`information_schema`) + resolve its **real current snapshot** (`lake.snapshots()`) | **real DuckLake** |
| `RegisterTable` | idempotently track the table on the RAT axis (the engine's DDL created it) | catalog bookkeeping |
| `CommitTable` | record the engine-reported `snap-N`; idempotent + optimistic-concurrency guard (ADR-010) | catalog bookkeeping, **real snapshot value** |
| `CreateBranch` / `MergeBranch` | **thin tracker** — branch tips + merge over snapshot lineage | catalog bookkeeping |

**Settled vs spike.** Table existence and snapshot resolution are genuinely
DuckLake-backed (the lake is the source of truth). The **branch model is a deliberate
thin tracker** — this DuckLake build has no native branch primitive, so mapping RAT
branches onto DuckLake snapshot lineage is the open **§10 Q2** spike. The catalog keeps
branch/merge bookkeeping in its **own** sqlite DB; it never writes DuckLake's internal
metadata (no peer-state writes).

## Findings

- We only ever `SELECT snapshot_id` from `lake.snapshots()` — selecting `snapshot_time`
  pulls a `timestamptz` conversion that needs `pytz`. Avoided, so the catalog has no
  pytz dependency.

## Run the self-test (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/catalog/ducklake-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python selftest.py'
```

`selftest.py` (not `harness_test.py`) is intentional: this catalog is **not yet** in
the frozen `catalog/v1` golden-vector conformance suite — full parity waits on the
branch-model spike. It is exercised end-to-end by the experiment's
[`run-local.py`](../../../experiments/data-dev-plane/run-local.py).
