# rat-engine-duckdb-ml-py — the data-dev plane `engine` (DuckDB + ML)

> 🛰️ **Exploratory** — part of the [data-dev plane experiment](../../../experiments/data-dev-plane/README.md).
> Additive: no new axis, no proto change, the sealed `rat/2.0` surface is untouched.

The [`duckdb-py`](../duckdb-py) engine reference, **extended into the heart of the
data-dev plane**. It demonstrates the experiment's central call — **ML is an engine
extension, not an axis** (README §3): AI/ML "analysis" is *compute over data*, which
is exactly what `rat://engine/v1` already is. So ML lives as DuckDB extensions +
a UDF inside the engine plugin, invoked as plain SQL through the existing capability.
**No `kind: ml`, no 19th axis, no byte of the frozen wire changed.**

## What it adds over `duckdb-py`

| Addition | What it gives you |
|---|---|
| `embed(text, model) → FLOAT[]` UDF ([`embed.py`](embed.py)) | the pluggable inference seam — `hash-256` (default, stdlib, deterministic), `minilm` (sentence-transformers), `ollama:<m>` (remote, e.g. HAL-9000) |
| `vss` extension | HNSW vector index + `array_cosine_distance` / `array_distance` for semantic search |
| `ducklake` + `httpfs`/S3 extensions | read/write a [DuckLake](https://ducklake.select) lakehouse, data as Parquet on S3 |
| `Execute` → `WriteResult.snapshot_id` | surfaces the DuckLake snapshot a write produced, so the catalog can record it (README §10(b)) |

ML is *just SQL* inside `Execute`/`Query` — e.g.:

```sql
UPDATE lake.reviews SET embedding = embed(text, 'hash-256') WHERE embedding IS NULL;
SELECT id, text, array_cosine_distance(embedding::FLOAT[256], embed('battery life','hash-256')::FLOAT[256]) AS dist
FROM lake.reviews ORDER BY dist LIMIT 10;   -- 🔍 semantic search
```

## Still a conformant engine

It keeps the exact `EngineService` surface, so it **passes the same real-SQL golden
vectors** as `duckdb-py`/`datafusion-py`
([`engine-real-v1.json`](../../../contracts/conformance/engine-real-v1.json)) — the ML
extension did not regress the engine. It additionally passes deterministic golden
vectors for the `embed()` UDF
([`engine-embed-v1.json`](../../../contracts/conformance/engine-embed-v1.json)) — so
the ML UDF is tested as rigorously as every other capability (README §10 Q7).

## Findings (DuckLake × vss × DuckDB), carried into experiment README §10

- **DuckLake does not support DuckDB's fixed-size `ARRAY` (`FLOAT[N]`)** — the very
  type `vss`'s HNSW index requires. Embeddings are therefore stored as a **variable
  `LIST` (`FLOAT[]`)** and cast to `FLOAT[N]` at query time. Brute-force cosine works
  directly on the lake; an HNSW *index* needs a derived (non-lake) fixed-array column.
- **List-returning UDFs need `numpy`** (DuckDB marshals list results via numpy) — hence
  `numpy` is in `requirements.txt` even though the engine itself is Arrow-native.
- **DuckLake inlines small writes** into the metadata DB; Parquet files materialize on
  flush/checkpoint. (Relevant once data moves to S3.)

## Configuration

| env | meaning |
|---|---|
| `RAT_PLUGIN_ADDR` | gRPC listen address (default `127.0.0.1:0`) |
| `RAT_DUCKLAKE_META` | DuckLake metadata DB, e.g. `sqlite:/meta/catalog.db` — set (with DATA) to attach a lake as `lake` |
| `RAT_DUCKLAKE_DATA` | Parquet data path, e.g. `/lake/data/` or `s3://bucket/lake/` |
| `OLLAMA_URL` | Ollama endpoint for `embed(model='ollama:*')` (default `http://localhost:11434`) |

Without `RAT_DUCKLAKE_*` the engine runs lake-less (plain SQL + `embed()`), which is
the mode the conformance harness uses.

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/engine/duckdb-ml-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```
