# rat-engine-datafusion-py — ROUND-2 `engine` reference (real SQL engine)

The **second** real engine of the round-2 pair (with [`duckdb-py`](../duckdb-py)):
**Apache DataFusion** (a Rust, Arrow-native query engine). It executes the SAME typed
SQL of [`engine-real-v1.json`](../../../contracts/conformance/engine-real-v1.json) and
returns the SAME typed Arrow results as DuckDB.

Two genuinely different engine technologies agreeing on one shared golden-vector file
is [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)'s
literal "duckdb + datafusion" cross-run — option (b), real second pairs. Only
`store.py` (the DataFusion backend) differs from the DuckDB reference; `server.py`,
`streams.py`, and `harness_test.py` are identical (shared by construction — the engine
contract is the same, only the engine behind it changes).

See [`../duckdb-py/README.md`](../duckdb-py/README.md) for the full description of
what the shared harness asserts (typed SQL, `rows_affected`, real typed-Arrow results,
error codes).

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/engine/datafusion-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```
