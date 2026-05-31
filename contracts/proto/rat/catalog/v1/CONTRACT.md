# `catalog/v1` — plugin contract (author guide)

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

## The RPCs

- **`GetTable(identifier, branch)` → `{table: TableRef}`** — empty `branch` == main.
  Empty identifier → `INVALID_ARGUMENT`; unknown table/branch → `NOT_FOUND`.
- **`CreateBranch(branch, from_branch)` → `{branch}`** — empty `from_branch` == tip of
  main.
- **`MergeBranch(branch, into_branch, expected_into_snapshot, idempotency_key)` →
  `{snapshot_id, already_applied}`** — the publish gate of the pipeline model. See
  MERGE SAFETY below.

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

Pass [`catalog-v1.json`](../../../../conformance/catalog-v1.json): the stateful
get → create-branch → branch-isolated read → merge(accept) → idempotent-retry →
merge(reject, `FAILED_PRECONDITION`) lifecycle + the `NOT_FOUND`/`INVALID_ARGUMENT`
errors.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the catalog implements a plain gRPC `CatalogService` server.

## Writing a plugin

1. Implement `CatalogService` (GetTable/CreateBranch/MergeBranch) over your catalog.
2. Implement `MergeBranch` with the optimistic-concurrency guard (`expected_into_snapshot`
   → `FAILED_PRECONDITION` on mismatch) + the idempotency ledger (`idempotency_key` →
   `already_applied`). These MUST be safe under concurrency.
3. Pass [`catalog-v1.json`](../../../../conformance/catalog-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/catalog/inmemory-go`](../../../../../examples/catalog/inmemory-go), [`inmemory-py`](../../../../../examples/catalog/inmemory-py) | 1 (wire) | two language code paths; git-like global branches |
| [`examples/catalog/sqlite-py`](../../../../../examples/catalog/sqlite-py) | 2 (real) | sqlite — DURABLE branches + idempotency ledger (survive reopen) + CONCURRENT-MERGE safety (16 racers → exactly one winner via `BEGIN IMMEDIATE`) |

## Related

[`catalog.proto`](catalog.proto) · [`catalog-v1.json`](../../../../conformance/catalog-v1.json) ·
[`format/v1/CONTRACT.md`](../../format/v1/CONTRACT.md) (snapshots branches sit on) ·
[reviews/06](../../../../../reviews/06-proto-contract-review.md) #8
