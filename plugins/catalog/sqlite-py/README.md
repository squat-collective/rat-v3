# rat-catalog-sqlite-py — ROUND-2 `catalog` reference (real backend)

The **round-2** `catalog` reference: branches, the snapshot of each, and the
idempotency ledger live in **sqlite** (real embedded transactional SQL DB,
file-on-disk, WAL) rather than an in-memory dict.

It passes the **SAME shared golden vectors** as the in-memory catalog references
(`contracts/conformance/catalog-v1.json`) — same model, same deterministic snapshot
scheme — and earns two properties the in-memory catalog can only fake:

| Round-2 property | Test | Why the in-memory catalog can't show it |
|---|---|---|
| **Durability** | `test_durability_branches_and_ledger_survive_reopen` | create a branch + merge → close → reopen the same db file → the branch, the moved snapshot, **and** the idempotency ledger (a re-merge with the same key is still a no-op) all persist | a dict dies with the process |
| **Concurrent-merge safety** | `test_concurrent_merge_one_winner` | 16 threads race a `MergeBranch` into `main` from the same expected snapshot → **exactly one** COMMITs, the rest `FAILED_PRECONDITION` | the in-memory guard is an in-process mutex; here it's sqlite `BEGIN IMMEDIATE` — durable, cross-connection lost-update prevention |

Concurrent-merge safety is the **publish gate** of the v2 pipeline model
(reviews/06 #8 — `MergeBranch` is reconciler-retried and must be safe under retry
AND concurrency). sqlite enforces the optimistic-concurrency guard + idempotency for
real; the in-memory twin only demonstrates the wire shape.

## Files

| File | Role |
|---|---|
| `store.py` | sqlite catalog: branches / tables / merges-ledger / meta-counter; transactional `merge_branch` (`BEGIN IMMEDIATE`) |
| `server.py` | the three `CatalogService` RPCs over the sqlite store |
| `main.py` | gRPC server entrypoint (`$RAT_CATALOG_DB`, `$RAT_PLUGIN_ADDR`) |
| `harness_test.py` | shared-vector cross-run + the two round-2 semantic tests |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/catalog/sqlite-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected:
`PASS — rat-catalog-sqlite-py conformed to catalog/v1 golden vectors + durability + concurrent-merge safety`.
