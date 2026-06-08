# rat-strategy-incremental-embed-py — the data-dev plane ELT (`kind: strategy`)

> 🛰️ **Exploratory** — part of the [data-dev plane experiment](../../../experiments/data-dev-plane/README.md).
> Additive: the frozen `strategy/v1` surface is unchanged.

A **real incremental embedding ELT** — not the toy [`fullrefresh`](../fullrefresh-py).
Per run (idempotent via the run id), it does the §5.4 pattern:

1. **register** the target (own the output) + ensure its schema (CTAS-from-source, empty);
2. **watermark** — stage only source rows newer than the target's high-water mark,
   entirely **server-side** (a subquery over the target's `max(watermark)` — the strategy
   never reads the value back, so there's no Arrow round-trip);
3. **merge** — upsert the staged rows on the business key (changed rows get
   `embedding = NULL` so they re-embed);
4. **embed** — `embed()` **only** the rows that need it (`embedding IS NULL`) — the
   incremental win;
5. **flush + snapshot** — force inlined data out to Parquet, then commit the snapshot.

```
run 1 (initial):     staged=N   embedded=N   ← full load
run 2 (new rows):    staged=Δ   embedded=Δ   ← only the delta
run 2 replay:        staged=0   embedded=0   ← idempotent (C1)
```

## Couples to nothing — composes by capability

Its only dependency is the `invoke(capability_uri, request) -> response` seam (the core
capability-invoke gateway, ADR-005). It `requires` four capabilities and names no
concrete plugin:

```
rat://catalog/v1/get-table       resolve/verify the source (must be landed)
rat://catalog/v1/register-table  own the target output table
rat://engine/v1/execute          CTAS / watermark-stage / merge / embed / flush
rat://catalog/v1/commit-table    record the snapshot (idempotency_key = run id)
```

Note the **absence of any `format` capability**: this stack has no format plugin —
DuckLake subsumes it, and the engine writes the lake directly. So unlike the generic
full-refresh/scd2 strategies (which `format.overwrite`/`format.merge`), this one writes
through `engine.execute`. **Finding F8** (README §10): in a DuckLake world a strategy
addresses tables as `<alias>.<identifier>` in SQL (the engine attaches the lake) instead
of going through `format.scan` indirection — DuckLake-aware in *addressing*, still
plugin-agnostic in *binding*.

## Options (the `ApplyRequest.options` JSON — API-12 encoding pin)

```json
{
  "key": "id",
  "text_column": "text",
  "embed_model": "hash-256",
  "watermark_column": "_ingested_at",
  "columns": ["id", "text", "rating", "_ingested_at"],
  "alias": "lake"
}
```

`source`/`target` are the `ApplyRequest` table identifiers (e.g. `reviews_raw` → `reviews`).

## Run it

Driven end-to-end (two runs + an idempotent replay + semantic search) by the
experiment's [`run-strategy.py`](../../../experiments/data-dev-plane/run-strategy.py) /
`make data-dev-strategy`. Like the other strategy references it has no standalone
`main.py` — a strategy only runs against a live gateway + providers.
