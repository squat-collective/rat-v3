# `format/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: format` plugin. Pairs with the wire
> contract [`format.proto`](format.proto) and the golden vectors
> [`format-v1.json`](../../../../conformance/format-v1.json). Status: **v1 (frozen — rat/1, ADR-009)**.

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
- **`Append`/`Merge`/`Overwrite`(table, source, idempotency_key[, merge_keys])` →
  `{result: WriteResult}`** — the caller hands a **source `ArrowStream`** (caller-hosted)
  the format pulls rows from (Flight `DoGet` against the caller's endpoint), and writes
  them. `Merge` requires non-empty `merge_keys` → else `INVALID_ARGUMENT`. Each write
  carries an `idempotency_key` (C1) and the `source` may declare `expected_rows`/
  `expected_batches` (C2) — see **Crash-safety** below.
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

## Conformance obligations — Crash-safety ([ADR-012](../../../../../docs/architecture/adrs/012-crash-safety-additive-fields.md), `rat/1.5`)

The at-least-once write path gets two additive guards (field shapes pinned at `rat/1.5`;
full per-axis vectors land in Phase 1 — demonstrated end-to-end now in
[plugins/composition](../../../../../plugins/composition)):

- **C1 — idempotent writes.** A write submitted with a non-empty `idempotency_key` that
  already committed MUST be a **no-op returning the original `WriteResult` with
  `already_applied=true`** — never a second write. This is the effect-leg twin of catalog
  `MergeBranch`/`CommitTable` idempotency; it makes a reconciler retry of an `append`
  safe. Empty key == not idempotent.
- **C2 — stream completeness.** When the `source` `ArrowStream` declares `expected_rows`
  (or `expected_batches`), the format MUST verify it received exactly that many before
  committing; a stream that ends early (a producer that died mid-send) MUST **fail the
  write**, never commit a partial dataset or return a complete-looking `rows_affected`.
  Absent == the producer could not pre-declare; fall back to the transport's clean
  end-of-stream.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

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
| [`plugins/format/inmemory-go`](../../../../../plugins/format/inmemory-go), [`inmemory-py`](../../../../../plugins/format/inmemory-py) | 1 (wire) | two language code paths; in-process Arrow stand-in |
| [`plugins/format/parquet-py`](../../../../../plugins/format/parquet-py) | 2 (real) | real Parquet files **+ REAL Arrow Flight transport** (both data legs over real TCP `DoGet`) |
| [`plugins/format/delta-py`](../../../../../plugins/format/delta-py) | 2 (real) | real Delta Lake table **+ time travel** (versioned snapshots) |

## Related

[`format.proto`](format.proto) · [`format-v1.json`](../../../../conformance/format-v1.json) ·
[`data.proto`](../../common/v1/data.proto) · [`catalog/v1/CONTRACT.md`](../../catalog/v1/CONTRACT.md)
(branches sit on format snapshots) · [ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
