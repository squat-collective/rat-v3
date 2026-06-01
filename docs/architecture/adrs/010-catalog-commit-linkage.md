# ADR-010: Catalog commit-linkage — additive `RegisterTable` + `CommitTable` RPCs

## Status: Accepted (2026-06-01)

## Context

The `catalog/v1` contract froze at `rat/1` ([ADR-009](009-data-plane-contract-freeze-v1.md))
with three RPCs: `GetTable`, `CreateBranch`, `MergeBranch`. ADR-009 recorded one accepted
residual on this axis:

> **R3** — the catalog has no create-table RPC; new-table commit-linkage is GA-deferred
> (`catalog.proto`). All post-freeze changes here are additive (new RPC/fields).

The post-freeze board review ([reviews/08](../../../reviews/08-post-freeze-board-review.md),
finding **B1**) re-graded that residual from "GA-deferred nicety" to **the headline feature's
load-bearing hole**, and three independent specialists (`contracts` #2, `architect` #2, and
`sre` via the atomicity cross-consult) nominated it as their biggest or near-biggest concern —
the strongest convergence signal in the review. The precise gap is two missing verbs:

1. **A pipeline cannot register a new output table.** `GetTable` resolves only *pre-existing*
   tables (unknown → `NOT_FOUND`). A strategy that produces a brand-new target dataset has no
   wire path to create its catalog entry.
2. **A pipeline cannot tell the catalog what a write landed.** After `format.Write` returns a
   `WriteResult.snapshot_id`, nothing on the frozen surface records *which snapshot* now
   represents that table on that branch. The catalog's branch→snapshot pointers are advanced
   only internally by `MergeBranch`; the format's actual snapshot is never linked in.

The consequence is that the **branch-isolated pipeline** — the v2 model RAT keeps and markets
(`overview.md`) — cannot complete its own **create → write → register → merge** loop on the
frozen wire. Both ends of the loop (`CreateBranch`, `MergeBranch`) are contracted; its
load-bearing middle is not. The cross-axis composition
([examples/composition](../../../examples/composition)) only passes because the harness reaches
*into the catalog's private store* to seed the source and target tables out-of-band
(`harness.py` `build_catalog`, the `cat._tables.add(...)` / `INSERT INTO tables` pokes) — i.e.
the green badge is propped up by a stand-in the contract can't express.

The `architect`→`sre` cross-consult sharpened a second, atomicity-flavoured face of the same
seam: `MergeBranch` is crash-safe (`idempotency_key` + `expected_into_snapshot` CAS), but the
*write* leg that precedes it has **no idempotency key**, so convergence under an at-least-once
scheduler rests entirely on the branch-isolation convention. The catalog-side linkage is the
natural place to give that leg a CAS + idempotency guard.

This is the **first** `v1.1` additive in the close-out (`current.md`), and ADR-009 already
declared the shape additive — so this ADR exercises the door the freeze deliberately left open
rather than forcing a `v2`.

## Decision

**Add two RPCs to `CatalogService`, additively (`rat/1.5`): `RegisterTable` and `CommitTable`.**
They are distinct capabilities, distinct methods, distinct privileges.

```proto
//   rat://catalog/v1/register-table -> RegisterTable
//   rat://catalog/v1/commit-table   -> CommitTable

rpc RegisterTable(RegisterTableRequest) returns (RegisterTableResponse) {
  option (rat.common.v1.capability) = "rat://catalog/v1/register-table";
}
rpc CommitTable(CommitTableRequest) returns (CommitTableResponse) {
  option (rat.common.v1.capability) = "rat://catalog/v1/commit-table";
}
```

### 1. `RegisterTable` — create a new output table (the "register" verb)

`RegisterTable(identifier, uri, branch)` → `{table: TableRef}`. It establishes a table's
**existence** so a pipeline can create its own output instead of only reading pre-existing
tables. It is **idempotent**: re-registering an identifier that already exists is a no-op that
returns the existing `TableRef` (no `ALREADY_EXISTS`). Empty `identifier` → `INVALID_ARGUMENT`;
unknown `branch` → `NOT_FOUND`; empty `branch` == `main`.

Idempotent-register (not create-or-409) is the deliberate choice: RAT is a *reconciler* (K8s
for data) where the same desired state ("this output table exists") is declared every run.
Idempotency is the reconcile-safe default, consistent with how the axis already treats it as a
first-class safety property on `MergeBranch`.

### 2. `CommitTable` — record what the write landed (the "commit-linkage" verb)

`CommitTable(identifier, branch, snapshot_id, expected_snapshot, idempotency_key)` →
`{snapshot_id, already_applied}`. It records that the table's data on `branch` is now at
`snapshot_id` — **the value the writer received in `format.WriteResult.snapshot_id`**. This is
the linkage: the catalog learns *exactly* what `format.Write` produced. Semantics deliberately
mirror `MergeBranch` so the axis has one safety model, not two:

- **`snapshot_id`** is writer-supplied and **required** (empty → `INVALID_ARGUMENT`).
  Commit-linkage exists to carry a snapshot; a commit with nothing to link is meaningless.
  (Unversioned formats — where `WriteResult.snapshot_id` is absent — simply do not participate;
  see Q01.)
- **`expected_snapshot`** — optimistic-concurrency guard on the table's current snapshot on
  that branch (empty == unconditional). Mismatch → `FAILED_PRECONDITION`. The twin of
  `MergeBranch.expected_into_snapshot`.
- **`idempotency_key`** — stable id for *this* logical commit (e.g. the run id). A retry with a
  committed key is a no-op returning the original `{snapshot_id, already_applied=true}`. The
  twin of `MergeBranch.idempotency_key`. **This is the write-leg idempotency the B1
  `architect`→`sre` cross-consult flagged** — the catalog-publish half now has the CAS +
  idempotency that the bare write leg lacks.
- The committed table must already be registered (else `NOT_FOUND`).

`CommitTable` returns the resulting `snapshot_id` as a string (mirroring `MergeBranchResponse`)
rather than a `TableRef` — `TableRef` still has no snapshot field (reviews/08 F2, accepted),
and adding one is a separate additive we do not need to close this loop.

### 3. Why two RPCs, not one

The same argument that split `format`'s single `Write` into `Append`/`Merge`/`Overwrite`
(`format.proto`, reviews/06 I-3) applies here: **method-level capabilities make C5 enforcement
method-level.** "Create a new catalog entry" (`register-table`) is a higher privilege than
"record a snapshot on a table you were assigned" (`commit-table`); an operator/ingestion plugin
might hold the former while a pipeline strategy holds only the latter. A single unified
create-or-commit RPC would collapse those into one capability and force a create/update sentinel
of exactly the kind the A1 `snapshot_id` fix just removed. Two verbs also map 1:1 to the two
gaps B1 names ("register a new output table" / "tell the catalog which snapshot").

### 4. The closed loop

```
RegisterTable(target)            create the output table            (rat://catalog/v1/register-table)
  └─ format.Write(target@branch) → WriteResult.snapshot_id = S
CommitTable(target, snap=S)      link the written snapshot          (rat://catalog/v1/commit-table)
  └─ MergeBranch(branch → main)  publish                            (rat://catalog/v1/merge-branch)
```

The full-refresh + SCD2 strategy references and the cross-axis composition are updated to drive
this loop through the gateway, replacing the out-of-band store-poking. The per-axis
`catalog-v1.json` golden vectors gain `register_table`/`commit_table` lifecycle + error steps,
so both independent references (Go in-memory, Python in-memory, sqlite) must conform.

## Consequences

**Positive.**

- The branch-pipeline headline feature **closes its loop on the wire**. The composition stops
  faking table registration out-of-band; the harness no longer touches the catalog's private
  store. The green badge now means what it says for this seam.
- The **write/publish leg gains crash-safety**: `CommitTable`'s `expected_snapshot` CAS +
  `idempotency_key` give the previously-unguarded leg the same retry-and-concurrency safety
  `MergeBranch` has. A duplicate at-least-once fire that re-commits the same key is a no-op.
- **Privilege separation is expressible**: `register-table` (create new tables) and
  `commit-table` (record a snapshot) are independently grantable capabilities.
- The freeze's anti-regret story holds: this is the *first* post-freeze additive, lands with no
  `v2`, and validates that ADR-009 froze the catalog at a shape with room (R3 resolved as
  designed).

**Negative — accepted.**

1. **The catalog surface grows from 3 to 5 RPCs / capabilities.** Against the six-thing-core
   minimalism this is real surface — but it is *plugin-axis* surface, not core surface, and it
   buys the axis's stated purpose (branch-isolated pipelines) which was incomplete without it.
2. **`RegisterTable` idempotent-return hides genuine identifier conflicts.** Re-registering an
   identifier that already exists with a *different* intended shape silently returns the
   existing ref rather than erroring. Richer conflict detection (schema/location compare → a
   real `ALREADY_EXISTS`/`FAILED_PRECONDITION`) is additive later if a backend needs it.
3. **`CommitTable` returns a bare `snapshot_id` string**, perpetuating the `TableRef`
   no-snapshot-field residual (F2). Accepted: enriching `TableRef` with a structured
   snapshot/as-of selector is a separate additive, not needed to close the loop.
4. **The reference snapshot model stays git-style branch-global**, so the references
   demonstrate the *wire* faithfully but a real per-table-history catalog (Iceberg/Nessie) will
   exercise corners the vectors don't (Q02).

**Neutral.**

- `catalog.proto`'s `Status:` stays `v1` (the wire major); the additive lands at the `rat/1.5`
  minor tag, exactly as `strategy/v1` froze at `rat/1.1`. The proto header NOTE that called
  commit-linkage "GA-deferred" is updated to point here.

## Open questions

- **Q01 — Unversioned formats.** When `format.WriteResult.snapshot_id` is *absent* (the A1
  optional-presence case: format cannot report a version), the writer has no snapshot to supply
  and the v1.1 references reject an empty `CommitTable.snapshot_id`. Such formats simply skip
  commit-linkage (the catalog still resolves them by branch). If a future "track an unversioned
  write" need appears, relaxing `snapshot_id` to allow empty (catalog mints) is a non-breaking
  semantic change.
- **Q02 — Per-(table,branch) snapshots vs branch-global.** The references record commits in a
  `(identifier, branch) → snapshot` map but keep the existing branch-global pointer for
  `MergeBranch`. A real catalog tracks per-table history; whether `MergeBranch` should *consume*
  per-table commits (publish exactly the committed snapshots) is left to the first real backend.
- **Q03 — Does `MergeBranch` need to reference commits?** Today `MergeBranch` mints a synthetic
  `snap-<counter>` independent of any `CommitTable`. Linking merge-publish to the set of
  committed table snapshots on the branch is a candidate enrichment, deferred until a backend
  forces it.

## Alternatives considered

1. **A single unified `CommitTable` with create-on-absent (Nessie `Put`-style).** One RPC, one
   capability; create vs update implicit. **Rejected** — collapses the register/commit privilege
   distinction the C5 method-level-capability discipline (and the format `Write`-split
   precedent) exists to preserve, and needs an `optional`/sentinel to express "I expect this
   table to not exist yet" — reintroducing exactly the ambiguity the A1 fix removed.
2. **Catalog-minted snapshot ids (ignore the format's `snapshot_id`).** `CommitTable` just pings
   "something changed on branch B" and the catalog mints `snap-<n>`. **Rejected** — that is not
   *linkage*; B1's gap is specifically "tell the catalog **which snapshot** `format.Write`
   produced." Writer-supplied is the whole point.
3. **Keep R3 GA-deferred; document write→register as admin/out-of-band.** **Rejected** — B1 is
   HIGH and three specialists converged on it; the axis ships *functionally incomplete* for its
   stated purpose, and the fix is cheap and additive. Deferring a known headline-feature hole to
   GA is the kind of "badge over-promises reality" the board warned against.
4. **Add a `snapshot_id`/as-of selector to `TableRef` now and thread the commit through it.**
   **Rejected** — broader than needed to close the loop, touches a shared cross-axis type
   (every data-plane axis), and is independently tracked as F2. Kept minimal and local to the
   catalog axis.

## Migration

This is the design from `rat/1.5` onward; there is no in-place data migration (no running
core). The landing sequence:

1. Add the 2 RPCs + 4 messages to `catalog.proto` (additive; `buf breaking` FILE clean).
2. `make gen-sdks` — regenerate Go/Python/TS/Rust.
3. Implement `register_table`/`commit_table` in all three catalog references (in-memory Go,
   in-memory Py, sqlite Py) + their gRPC servers.
4. Extend `catalog-v1.json` with the new lifecycle + error vectors; extend the three conformance
   harnesses (and add the two capabilities to the Go stub gateway's C5 allowlist).
5. Update `examples/composition` to register + commit through the gateway (drop the out-of-band
   target seeding); have the composition format servicer return a real `snapshot_id`.
6. `make conformance` (still 32/32) + `make composition` (loop now closed) green.

The `rat/1.5` re-cut folds this in (alongside the other close-out items) before the surface is
published; until then the tag is local and movable.

## Related

- [ADR-009](009-data-plane-contract-freeze-v1.md) — froze `catalog/v1` at `rat/1`; this ADR
  resolves its residual **R3**.
- [reviews/08](../../../reviews/08-post-freeze-board-review.md) **B1** (+ `board/contracts.md`
  #2, `board/architect.md` #2) — the finding this ADR closes.
- [`catalog.proto`](../../../contracts/proto/rat/catalog/v1/catalog.proto) /
  [`CONTRACT.md`](../../../contracts/proto/rat/catalog/v1/CONTRACT.md) — the wire + author guide.
- [`format.proto`](../../../contracts/proto/rat/format/v1/format.proto) — the `Write`→
  `WriteResult.snapshot_id` whose value `CommitTable` links; its `Append`/`Merge`/`Overwrite`
  split is the method-level-capability precedent for the two-RPC decision.
- [`common/v1/data.proto`](../../../contracts/proto/rat/common/v1/data.proto) — `WriteResult`
  (the A1 `optional snapshot_id` fix) + `TableRef` (the F2 no-snapshot-field residual).
- [examples/composition](../../../examples/composition) — the cross-axis loop now closed on-wire.
