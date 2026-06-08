# `catalog/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugins here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: catalog` plugin. Pairs with the wire
> contract [`catalog.proto`](catalog.proto) and the golden vectors
> [`catalog-v1.json`](../../../../conformance/catalog-v1.json). Status: **v1 (frozen — rat/1, ADR-009)**.

## What a `catalog` plugin is

A `kind: catalog` plugin (Nessie, Unity, Lakekeeper, Glue, Polaris) owns table metadata
+ **branch/version semantics**. It is what makes **branch-isolated pipeline runs** (the
v2 pipeline model RAT keeps) possible. Not every catalog supports branching (Glue
doesn't) — those plugins simply don't `provide` the branch capabilities, and the
registry won't wire a branch-requiring strategy to them. Capability negotiation, not
feature flags.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://catalog/v1/get-table` | `GetTable` | resolve an identifier (+ branch) → a `TableRef` |
| `rat://catalog/v1/create-branch` | `CreateBranch` | open an isolated branch for a pipeline run |
| `rat://catalog/v1/merge-branch` | `MergeBranch` | merge a completed run's branch back to main |
| `rat://catalog/v1/register-table` | `RegisterTable` | register a NEW output table (idempotent) — ADR-010 |
| `rat://catalog/v1/commit-table` | `CommitTable` | record the snapshot a `format.Write` produced (commit-linkage) — ADR-010 |

## The RPCs

- **`GetTable(identifier, branch)` → `{table: TableRef}`** — empty `branch` == main.
  Empty identifier → `INVALID_ARGUMENT`; unknown table/branch → `NOT_FOUND`.
- **`CreateBranch(branch, from_branch)` → `{branch}`** — empty `from_branch` == tip of
  main.
- **`MergeBranch(branch, into_branch, expected_into_snapshot, idempotency_key)` →
  `{snapshot_id, already_applied}`** — the publish gate of the pipeline model. See
  MERGE SAFETY below.
- **`RegisterTable(identifier, uri, branch)` → `{table: TableRef}`** — create a new
  output table so a pipeline can write its own target (not only read pre-existing
  tables). **Idempotent**: re-registering an existing identifier returns the existing
  ref (no `ALREADY_EXISTS` — reconcile-safe). Empty `identifier` → `INVALID_ARGUMENT`;
  unknown `branch` → `NOT_FOUND`; empty `branch` == main. (ADR-010.)
- **`CommitTable(identifier, branch, snapshot_id, expected_snapshot, idempotency_key)` →
  `{snapshot_id, already_applied}`** — record the snapshot a `format.Write` produced
  (the value it returned in `WriteResult.snapshot_id`) for the table on the branch:
  the **commit-linkage**. See COMMIT-LINKAGE SAFETY below. (ADR-010.)

## Conformance obligations — MERGE SAFETY (reviews/06 #8)

`MergeBranch` is reconciler-RETRIED and must be safe under retry AND concurrency. Two
fields make it so, and the vectors gate on both:

- **`expected_into_snapshot`** — optimistic concurrency on the target. The merge applies
  only if `into_branch` is still at this snapshot; otherwise → `FAILED_PRECONDITION`
  (the caller re-reads + re-tests). Without it, two concurrent merges into main
  silently lose one side (lost-update).
- **`idempotency_key`** — a stable id for THIS logical merge (e.g. the run id). A retry
  with a key that already committed is a no-op returning the original result
  (`already_applied=true`), so a reconciler retry can't double-apply.

## Conformance obligations — COMMIT-LINKAGE SAFETY (ADR-010 / reviews/08 B1)

`CommitTable` is the create→write→**register**→merge loop's link: the catalog learns
exactly what `format.Write` landed. It carries the SAME safety model as `MergeBranch`
(the write/publish leg that previously had none — the B1 `architect`→`sre` cross-consult),
and the vectors gate on it:

- **`snapshot_id` is writer-supplied + REQUIRED.** It is the value `format.Write`
  returned in `WriteResult.snapshot_id`; the catalog records it verbatim and echoes it
  back (the linkage). Empty → `INVALID_ARGUMENT`. An unversioned format that cannot
  report a snapshot simply does not commit-link.
- **`expected_snapshot`** — optimistic concurrency on the table's current snapshot on
  that branch; mismatch → `FAILED_PRECONDITION`. The twin of
  `MergeBranch.expected_into_snapshot`.
- **`idempotency_key`** — a retry with a key that already committed is a no-op returning
  the original (`already_applied=true`). The twin of `MergeBranch.idempotency_key`.
- The committed table MUST already be registered (`RegisterTable`) — unknown → `NOT_FOUND`.

`RegisterTable` MUST be idempotent (re-registering an identifier returns the existing
ref, no error) so a repeatedly-run pipeline declaring its output every run is safe.

Pass [`catalog-v1.json`](../../../../conformance/catalog-v1.json): the stateful
get → create-branch → branch-isolated read → merge(accept) → idempotent-retry →
merge(reject, `FAILED_PRECONDITION`) → register(new) → register(idempotent) →
commit(new) → commit(idempotent-retry) → commit(CAS-reject, `FAILED_PRECONDITION`) →
commit(CAS-ok) lifecycle + the `NOT_FOUND`/`INVALID_ARGUMENT` errors (incl. commit of an
unregistered table + empty `snapshot_id`).

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the catalog implements a plain gRPC `CatalogService` server.

## Writing a plugin

1. Implement `CatalogService` (GetTable/CreateBranch/MergeBranch/RegisterTable/CommitTable)
   over your catalog.
2. Implement `MergeBranch` **and** `CommitTable` with the optimistic-concurrency guard
   (`expected_*` → `FAILED_PRECONDITION` on mismatch) + the idempotency ledger
   (`idempotency_key` → `already_applied`). These MUST be safe under concurrency. Make
   `RegisterTable` idempotent.
3. Pass [`catalog-v1.json`](../../../../conformance/catalog-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/catalog/inmemory-go`](../../../../../plugins/catalog/inmemory-go), [`inmemory-py`](../../../../../plugins/catalog/inmemory-py) | 1 (wire) | two language code paths; git-like global branches |
| [`plugins/catalog/sqlite-py`](../../../../../plugins/catalog/sqlite-py) | 2 (real) | sqlite — DURABLE branches + idempotency ledger (survive reopen) + CONCURRENT-MERGE safety (16 racers → exactly one winner via `BEGIN IMMEDIATE`) |

## Related

[`catalog.proto`](catalog.proto) · [`catalog-v1.json`](../../../../conformance/catalog-v1.json) ·
[`format/v1/CONTRACT.md`](../../format/v1/CONTRACT.md) (snapshots branches sit on) ·
[reviews/06](../../../../../reviews/06-proto-contract-review.md) #8
