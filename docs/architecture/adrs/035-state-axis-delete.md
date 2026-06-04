# ADR-035: Add `Delete` to the state axis (`rat://state/v1/delete`)

## Status: Accepted (2026-06-04) — additive amendment to the frozen state axis (rat/1)

## Context

The state axis ([ADR-009](009-data-plane-contract-freeze-v1.md), tier-0, frozen at `rat/1`) provides
`Get` / `Put` / `List` / `Watch` — but **no `Delete`**. A key/value store you can write to but never
delete from is a genuine gap: every `Put` is permanent, `List` only grows.

It surfaced concretely while building **RatFS** (code-fs as a native VS Code folder, this session's
filesystem-op audit): **delete-file, delete-folder, rename, and move are all unbuildable** because
rename = copy **+ delete**, and there is no delete. code-fs is S3-backed and *can* delete (S3
`RemoveObject`) — but with no capability for it, the surface cannot invoke one.

This is the **most sacred contract** (tier-0, the reconciler's lease backend builds on it). Adding a
method is **additive** (wire-compatible, `buf breaking`-clean), but it has a conformance implication:
existing state-backends (e.g. `state-sqlite`) don't implement `Delete`.

## Decision

Add one RPC to `StateService`:

```proto
// rat://state/v1/delete — remove one key (plugin+tenant relative), optionally compare-and-set.
rpc Delete(DeleteRequest) returns (DeleteResponse) {
  option (rat.common.v1.capability) = "rat://state/v1/delete";
}

message DeleteRequest {
  reserved 1;            // RequestContext travels in rat-callmeta-bin (ADR-007)
  string key = 2;        // plugin-relative; subject to the KEY GRAMMAR (file header). Non-empty.
  int64 if_revision = 3; // CAS: if > 0, delete only if the current revision matches; 0 == unconditional.
}

message DeleteResponse {
  bool found = 1;        // true if the key existed and was removed; false if it was already absent.
}
```

### Semantics
- **Idempotent.** Deleting an absent key returns `found = false` — **not** an error.
- **CAS.** `if_revision > 0` deletes only if the current revision matches; a mismatch surfaces as
  gRPC `FAILED_PRECONDITION` (same fencing rigor as `Put`'s CAS — deleting a lease key *is*
  releasing the lease, so a multi-replica-lease backend MUST make it linearizable).
- **Namespace.** The C3 per-plugin+tenant prefix applies to `key` exactly as for `Get`/`Put` — the
  plugin never deletes outside its namespace.

### `Delete` is OPTIONAL per backend
A state-backend **MAY** return `UNIMPLEMENTED` for `Delete` (an append-only / lease-only backend has
no meaningful delete). It declares `rat://state/v1/delete` in `provides` **only if** it supports it;
consumers MUST handle `UNIMPLEMENTED` gracefully. This keeps every existing backend conformant with
**no immediate change** — `state-sqlite` adds `Delete` when it chooses; **code-fs** implements it now.

## Consequences

**Positive.**
- Closes the KV gap; unblocks **delete / rename / move** for code-fs and any state consumer.
- **Additive** — `buf breaking` clean; existing backends are unaffected because `Delete` is optional.
- Targeted: the immediate need is delete on the *existing* state-backed code-fs — this delivers it
  without a new axis or a core re-architecture.

**Negative — accepted.**
- The **frozen tier-0 contract grows a method.** A real (if additive) amendment to the most sacred
  axis — justified by a genuine gap + the additive/optional design, and gated by this ADR.
- **Conformance surface grows:** the axis suite gains an *optional* `Delete` vector; backends that
  advertise it must pass it (idempotency + CAS + namespace isolation).
- **Lease fencing:** `Delete` on a CAS/lease key releases the lease — multi-replica-lease backends
  owe it the same linearizability as `Put`-CAS (called out above).

**Neutral.** code-fs provides `state/v1/delete`; other backends opt in over time.

## Alternatives considered

- **The fs axis ([ADR-032](032-filesystem-axis.md), Deferred)** — a *new* axis with
  read/write/list/stat/**delete**. Rejected *for this need*: heavier (new proto + core recompile),
  when the immediate gap is just delete on the existing state-backed code-fs. The fs axis remains the
  path to *richer* semantics (stat, real directories, git-backing) — this ADR does not preclude it;
  it unblocks delete now without it.
- **Tombstone via `Put` (empty value).** Rejected: leaves a ghost key in `List` — not a delete.
- **Leave delete unsupported.** Rejected: delete is table-stakes for a filesystem *and* a real KV
  gap independent of code-fs.

## Migration

Additive: add the `Delete` RPC + messages to `state.proto`; `make gen-sdks`; `make breaking` clean
(new method). The gateway routes it automatically (the state descriptor is already in
`routableDescriptors`; rebuilt to pick up the regenerated proto). **code-fs** implements `Delete`
(S3 `RemoveObject`) + adds `rat://state/v1/delete` to `provides`; **RatFS** gains `delete`/`rename`.
`state-sqlite` and other backends add `Delete` opportunistically (optional).

## Related

- [ADR-009](009-data-plane-contract-freeze-v1.md) — the state-axis v1 freeze this amends.
- [ADR-032](032-filesystem-axis.md) — the fs axis (the richer alternative, still deferred).
- [ADR-017](017-pre-unfreeze-contract-amendment-gate.md) — the contract-amendment discipline.
- The RatFS filesystem-op audit (this session) — where the gap became concrete.
