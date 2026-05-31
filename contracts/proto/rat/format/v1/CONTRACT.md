# `format/v1` — plugin contract (author guide)

> Canonical guide for implementing a `kind: format` plugin. Pairs with the wire
> contract [`format.proto`](format.proto) and the golden vectors
> [`format-v1.json`](../../../../conformance/format-v1.json). Status: **v1-preview**.

## What a `format` plugin is

A `kind: format` plugin (Iceberg, Delta Lake, Hudi, Parquet-files, …) owns
table-format semantics: resolving refs to physical data, writing data, doing
maintenance. Every `strategy` requires a `format`; it is the most-referenced data-plane
axis.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://format/v1/scan` | `Resolve` | resolve a table ref → a readable Arrow stream |
| `rat://format/v1/append` | `Append` | append rows (no key matching) |
| `rat://format/v1/merge` | `Merge` | upsert rows matched on `merge_keys` |
| `rat://format/v1/overwrite` | `Overwrite` | replace existing data |
| `rat://format/v1/maintain` | `Maintain` | compaction / snapshot expiry / manifest rewrite |

`Append`/`Merge`/`Overwrite` are **separate RPCs**, not one `Write` keyed by a mode
enum (reviews/06 I-3) — so each capability is method-level enforceable and the manifest
`provides` set maps 1:1 to callable methods. A plugin that doesn't support branching/
merge simply does not `provide` `merge`.

## The RPCs (and the bidirectional data leg)

- **`Resolve(table, columns, predicate)` → `{stream: ArrowStream}`** — returns a
  **producer-hosted** `ArrowStream` the caller pulls matching record batches from
  (Flight `DoGet`). `columns`/`predicate` are projection/pushdown hints.
- **`Append`/`Merge`/`Overwrite`(table, source[, merge_keys])` → `{result: WriteResult}`**
  — the caller hands a **source `ArrowStream`** (caller-hosted) the format pulls rows
  from (Flight `DoGet` against the caller's endpoint), and writes them. `Merge` requires
  non-empty `merge_keys` → else `INVALID_ARGUMENT`.
- **`Maintain(table)` → `{result: WriteResult}`** — idempotent upkeep; `rows_affected`
  may be absent (unknown).

So format moves Arrow **both directions**: sources flow IN (write RPCs), results flow
OUT (Resolve). Both are out-of-band `ArrowStream`s, never through the control plane.

## Conformance obligations

- Pass [`format-v1.json`](../../../../conformance/format-v1.json): the
  append → scan → merge(upsert) → overwrite → maintain lifecycle + 2 error vectors
  (empty `TableRef` / missing `merge_keys` → `INVALID_ARGUMENT`). `WriteResult.rows_affected`
  uses proto3 `optional` for presence (absent == unknown, distinct from 0).
- C6: every capability has a golden-data vector; "capability declared" is meaningless
  without "capability conformed" (reviews/02 Stage5).

## Cross-cutting (every axis)

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md)).
- **Bulk data bypasses the core** ([`data.proto`](../../common/v1/data.proto)): the
  `ArrowStream` carries `{endpoint, ticket (single-use, SEC-14), transport=FLIGHT, role}`.
  `PRODUCER_HOSTED` → the consumer DoGets; the data-holder hosts a Flight server.

## Writing a plugin

1. Implement `FormatService` (Resolve/Append/Merge/Overwrite/Maintain) over your format.
2. For reads, host the result on a Flight server + return a `PRODUCER_HOSTED`
   `ArrowStream`. For writes, DoGet the source from the caller's endpoint.
3. Implement merge as upsert-on-keys; reject missing `merge_keys`.
4. Pass [`format-v1.json`](../../../../conformance/format-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/format/inmemory-go`](../../../../../examples/format/inmemory-go), [`inmemory-py`](../../../../../examples/format/inmemory-py) | 1 (wire) | two language code paths; in-process Arrow stand-in |
| [`examples/format/parquet-py`](../../../../../examples/format/parquet-py) | 2 (real) | real Parquet files **+ REAL Arrow Flight transport** (both data legs over real TCP `DoGet`) |
| [`examples/format/delta-py`](../../../../../examples/format/delta-py) | 2 (real) | real Delta Lake table **+ time travel** (versioned snapshots) |

## Related

[`format.proto`](format.proto) · [`format-v1.json`](../../../../conformance/format-v1.json) ·
[`data.proto`](../../common/v1/data.proto) · [`catalog/v1/CONTRACT.md`](../../catalog/v1/CONTRACT.md)
(branches sit on format snapshots) · [ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
