# rat-engine-inmemory-py — second `engine` reference (ADR-003)

> ⚠️ **WIRE-CONTRACT REFERENCE ONLY — NOT A STARTER TEMPLATE.** This round-1 reference validates the `engine/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory stand-in** — it deliberately fakes things a real plugin must not copy (in-process data stand-ins, ignored hints). For a production-shaped implementation, copy the **round-2 real backend** instead: [`duckdb-py`](../duckdb-py) / [`datafusion-py`](../datafusion-py). See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The **second independent** `kind: engine` reference. It satisfies the
[ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
gate for `engine/v1`: an independently-written implementation (different language,
different code path) that passes the **same golden vectors** as the first
([`inmemory-go`](../inmemory-go)), so the contract can advance from `v1-preview`
toward `v1`.

It is **not** a production query engine. It is a self-contained in-memory
**mini-SQL** engine — it holds its own tables rather than querying a format/storage
provider (the real engine↔format handoff is separate multi-axis integration work).
The control-plane wire contract — `Execute` / `Query` / `Preview` — is what's
validated; query results ride an in-process stream registry standing in for the
real Arrow Flight leg.

## Mini-SQL grammar

Case-sensitive keywords, single-space separated; identifiers/values `[A-Za-z0-9_]+`.
The **same** grammar (and the same three regexes) is implemented by both references
so they stay in lockstep:

```
CREATE TABLE <t> (<col>, <col>, ...)
INSERT INTO <t> VALUES (<v>, <v>, ...)
SELECT <* | col, col, ...> FROM <t> [WHERE <col> = <val>] [LIMIT <n>]
```

## Files

| File | Role |
|---|---|
| `sql.py` | the mini-SQL parser (mirrors `inmemory-go/sql.go`) |
| `store.py` | in-memory tables: create / insert / select(WHERE + projection) |
| `streams.py` | in-process stand-in for the out-of-band Arrow leg |
| `server.py` | the three `EngineService` RPCs; empty/unparseable SQL + unknown table → `INVALID_ARGUMENT` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/engine-v1.json` and drives this impl over real gRPC |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/engine/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-engine-inmemory-py conformed to engine/v1 golden vectors`.
Runs under pytest too (the `test_*` functions).
