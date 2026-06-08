# `engine/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugins here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: engine` plugin. Pairs with the wire
> contract [`engine.proto`](engine.proto) and the golden vectors
> [`engine-v1.json`](../../../../conformance/engine-v1.json) (wire) +
> [`engine-real-v1.json`](../../../../conformance/engine-real-v1.json) (real SQL).
> Status: **v1 (frozen — rat/1, ADR-009)**.

## What an `engine` plugin is

A `kind: engine` plugin (DuckDB, DataFusion, ClickHouse, Spark, Trino, …) is the
**compute axis**: it turns SQL into Arrow. A strategy/runtime hands it SQL + table
refs; it executes and returns an Arrow stream. Engines bring their own SQL semantics
— RAT does not ship a query language.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://engine/v1/execute` | `Execute` | run a statement for effect (DDL, `INSERT … SELECT`) → `WriteResult` |
| `rat://engine/v1/query` | `Query` | run a `SELECT` → results stream out-of-band as Arrow |
| `rat://engine/v1/preview` | `Preview` | bounded sample (`limit`) of a query, for UI/inspection |

The three are already distinct RPCs mapping 1:1 to three capabilities — engine needs
no per-mode split (unlike `format.Write`).

## The RPCs

- **`Execute(sql, tables)` → `{result: WriteResult}`** — `rows_affected` reports the
  effect (0 for DDL, N for an N-row insert). `tables` lists the refs the statement
  touches so the core can resolve providers without parsing SQL.
- **`Query(sql, tables)` → `{stream: ArrowStream}`** — the result rows flow
  **out-of-band as Arrow** via the returned `ArrowStream` (the consumer pulls them);
  the control RPC carries only the descriptor.
- **`Preview(sql, tables, limit)` → `{stream: ArrowStream}`** — same, bounded by
  `limit`.

## Conformance obligations

- Pass [`engine-v1.json`](../../../../conformance/engine-v1.json) (the wire-contract
  vectors — a toy mini-SQL exercising the RPC shapes + error codes) **or**, for a real
  SQL engine, [`engine-real-v1.json`](../../../../conformance/engine-real-v1.json)
  (typed SQL + **typed-Arrow** result assertions: row count, projected column set,
  rows with typed values).
- Unknown table / empty SQL → `INVALID_ARGUMENT`.
- The engine↔format handoff is where "fits ≠ works" bites hardest (reviews/00 Theme 4)
  — a real engine reads its inputs from a `format`/`storage` provider and returns real
  Arrow; the golden data is what catches dialect/Arrow-schema assumptions.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a request field.
- Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the engine implements a plain gRPC `EngineService` server.
- **Bulk data bypasses the core.** Query/Preview results move plugin-to-plugin via
  `ArrowStream` (Arrow Flight at GA), never through the invoke gateway.

## Writing a plugin

1. Implement `EngineService` (Execute/Query/Preview) over your engine.
2. Produce results as real Arrow; hand them back via an `ArrowStream` descriptor
   (`transport=FLIGHT`, `role=PRODUCER_HOSTED` — the consumer DoGets).
3. Map SQL/catalog errors to `INVALID_ARGUMENT`.
4. Pass the conformance vectors via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/engine/inmemory-go`](../../../../../plugins/engine/inmemory-go), [`inmemory-py`](../../../../../plugins/engine/inmemory-py) | 1 (wire) | toy mini-SQL — two language code paths conform to the RPC shapes |
| [`plugins/engine/duckdb-py`](../../../../../plugins/engine/duckdb-py) + [`datafusion-py`](../../../../../plugins/engine/datafusion-py) | 2 (real) | **two real SQL engines** (DuckDB + DataFusion, ADR-003's literal example) agree on real typed Arrow over `engine-real-v1.json` |

## Related

[`engine.proto`](engine.proto) · [`engine-v1.json`](../../../../conformance/engine-v1.json) ·
[`engine-real-v1.json`](../../../../conformance/engine-real-v1.json) ·
[`data.proto`](../../common/v1/data.proto) (`ArrowStream`/`TableRef`/`WriteResult`) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
