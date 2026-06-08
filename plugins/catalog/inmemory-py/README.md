# rat-catalog-inmemory-py — second `catalog` reference (ADR-003)

> ⚠️ **WIRE-CONTRACT REFERENCE ONLY — NOT A STARTER TEMPLATE.** This round-1 reference validates the `catalog/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory stand-in** — it deliberately fakes things a real plugin must not copy (in-process data stand-ins, ignored hints). For a production-shaped implementation, copy the **round-2 real backend** instead: [`sqlite-py`](../sqlite-py). See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The **second independent** `kind: catalog` reference. It satisfies the
[ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
**wire-contract** gate for `catalog/v1`: an independently-written implementation
(different language, different code path) that passes the **same golden vectors** as
the first ([`inmemory-go`](../inmemory-go)).

A catalog owns table metadata + git-like **branch/version** semantics — the basis
for branch-isolated pipeline runs. The three RPCs under test:

| RPC | What the reference does |
|---|---|
| `GetTable` | resolve `identifier` (+ optional branch) → `TableRef`; unknown table → `NOT_FOUND`, empty id → `INVALID_ARGUMENT` |
| `CreateBranch` | open a branch as a copy of a source branch's snapshot |
| `MergeBranch` | merge under the **MERGE-SAFETY** contract — optimistic concurrency (`expected_into_snapshot` → `FAILED_PRECONDITION` on mismatch) + idempotency (`idempotency_key` → `already_applied`) |

## Deterministic snapshot model (shared with the Go reference)

Both references model the same scheme so they stay in lockstep: branches are
global; one table (`warehouse.sales.orders`) is **seeded** on `main` at `snap-0`;
`CreateBranch` copies the source's snapshot; `MergeBranch` bumps a global counter
and sets the target to `snap-<counter>` (first merge → `snap-1`). That determinism
is what lets the vectors hard-code `expected_into_snapshot: "snap-0"` and assert the
optimistic-concurrency accept/reject paths.

> Round-1 note: this is an **in-memory** reference (validates the wire contract). A
> real divergent backend (e.g. a sqlite-backed catalog) is round-2 work per ADR-003.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/catalog/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-catalog-inmemory-py conformed to catalog/v1 golden vectors`.
