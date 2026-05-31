# rat-engine-duckdb-py — ROUND-2 `engine` reference (real SQL engine)

Half of the **round-2** `engine` pair (with [`datafusion-py`](../datafusion-py)) — a
real SQL engine, not the toy regex parser of the in-memory refs. **DuckDB** executes
the typed SQL of [`engine-real-v1.json`](../../../contracts/conformance/engine-real-v1.json)
and returns results as **real typed Arrow**.

This is [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)'s
*literal* example — "two engine implementations (e.g. duckdb + datafusion)" — and the
two real engines passing one shared golden-vector file is the actual two-real-engine
cross-run (option (b): real second pairs, not toy + real). It also **retires the
typed-Arrow gap for engine**: the result leg is real Arrow IPC (typed schema +
columnar batches), serialized + read back with pyarrow, not the toy string-row
stand-in.

| What's tested | How |
|---|---|
| Real SQL | `CREATE TABLE … (id INTEGER, …)` + `INSERT` + `SELECT … WHERE … / LIMIT` on DuckDB |
| `rows_affected` | from the `Execute` (DDL/INSERT) WriteResult |
| Typed Arrow results | `Query`/`Preview` → an `ArrowStream`; the harness reads the Arrow IPC with pyarrow and asserts `row_count` + projected `columns` + `rows_contain` with **typed** values (`{"id": 1, "region": "west"}`) |
| Errors | unknown table / empty SQL → `INVALID_ARGUMENT` |

The toy `inmemory-go`/`inmemory-py` engine refs remain as the round-1 **wire-contract**
validation (on `engine-v1.json`); this pair is the round-2 **semantic** + typed-Arrow
conformance (on `engine-real-v1.json`).

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/engine/duckdb-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```
