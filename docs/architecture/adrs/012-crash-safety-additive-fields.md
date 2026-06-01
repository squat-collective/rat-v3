# ADR-012: Additive crash-safety fields for the data-plane write path (`rat/1.5`)

## Status: Accepted (2026-06-01)

## Context

The post-freeze board review ([reviews/08](../../../reviews/08-post-freeze-board-review.md))
found the run-lifecycle is **not crash-safe** (cluster C, `sre`'s spine). Two of its
findings are pure `[ADDITIVE]` wire gaps that the board recommended absorbing **while the
freeze is still local** (before publication), and they are being folded into the `rat/1.5`
seal that closes Phase 0:

- **C1 — at-least-once scheduler + no effect-leg idempotency key = silent double-apply.**
  `[ADDITIVE]` · HIGH · `sre` #1. The scheduler's `WatchDue` is at-least-once (ADR-pinned),
  dedup exists at the reconciler, but **never at the write**. A duplicate fire → an
  `append` strategy writes twice. Item 1 ([ADR-010](010-catalog-commit-linkage.md)) gave the
  *catalog-commit* leg `idempotency_key` + `already_applied`; the **format-write leg** still
  has none, so convergence under retry rests entirely on the branch-isolation convention.

- **C2 — `ArrowStream` has no termination/completeness signal.** `[ADDITIVE]` · MED-HIGH ·
  `sre` #2 + `contracts` #5. A producer that dies mid-transfer closes the out-of-band stream
  the *same way* a clean finish does → `format.Append` commits a **partial** dataset and
  returns a complete-looking `rows_affected`. The board upgraded this to MED-HIGH on a
  concrete corruption path: the SCD2 reference is producer *and* consumer — a truncated scan
  delivers fewer rows, so SCD2 treats absent keys as DELETED and **closes versions that
  should stay open → silent history corruption**, with no wire signal to detect it.

Both are additive (new fields/values), so they are non-breaking; locking their **shapes**
into `rat/1.5` now — while the data plane is fresh in mind and unpublished — is cheaper than a
post-publication `v1.2`. This ADR follows the established precedent for security/safety
fields whose *enforcement* lands later: the `ArrowStream.ticket` field was frozen at `rat/1`
with its detailed spec marked "enforcement-layer (GA); the field is here so it's in the
frozen shape." Same split here — the fields + obligations are pinned now; the exhaustive
per-axis conformance enforcement is **Phase 1** (where the core that drives at-least-once
exists and C1–C5 become acceptance tests).

## Decision

**Add the C1 + C2 additive fields to the frozen data-plane wire, document their obligations,
and demonstrate them end-to-end in the cross-axis composition. Defer the exhaustive per-axis
conformance vectors to Phase 1.**

### 1. C1 — write-leg idempotency (mirrors the catalog model)

The data plane gets *one* idempotency model, identical to `MergeBranch`/`CommitTable`
([ADR-010](010-catalog-commit-linkage.md)):

- `format.AppendRequest`, `format.OverwriteRequest`, `format.MergeRequest` each gain
  `string idempotency_key` — a stable id for *this* logical write (e.g. the run id). A write
  submitted with a non-empty key that already committed is a **no-op that returns the
  original `WriteResult`**. Empty == not idempotent (each call is a fresh write).
- `strategy.ApplyRequest` gains `string idempotency_key` — the run id the strategy threads
  down to its `format.Write` calls, so a re-applied run is idempotent end-to-end.
- `common.v1.WriteResult` gains `bool already_applied = 3` — true when the response reflects
  a previously-committed write with the same key (the retry was a no-op), exactly like
  `MergeBranchResponse.already_applied`.
- **Obligation:** a conformant `format` plugin's writes MUST be idempotent under a repeated
  `idempotency_key` (return the original result, `already_applied=true`; do not write twice).

### 2. C2 — `ArrowStream` completeness

`common.v1.ArrowStream` gains two optional, presence-tracked declarations:

- `optional int64 expected_rows = 6` — the total row count the producer intends to send.
- `optional int64 expected_batches = 7` — the total Arrow record-batch count.

`optional` (proto3 presence) distinguishes *absent* ("producer cannot pre-declare — fall
back to the transport's own clean end-of-stream") from *present* (a hard count the consumer
checks). **Obligation:** a consumer that pulls an `ArrowStream` with a declared
`expected_rows`/`expected_batches` MUST verify it received that many before treating the
transfer as complete; a stream that ends early (fewer than declared) MUST **fail the write**
— it MUST NOT commit a partial dataset or return a complete-looking `rows_affected`.

### 3. Scope of demonstration vs. enforcement

The field shapes + obligations are locked now and **documented** in the proto comments +
`format`/`strategy` `CONTRACT.md`. They are demonstrated **end-to-end in
[examples/composition](../../../examples/composition)** (the strategy threads an
`idempotency_key`; the format servicer dedups on it and sets `already_applied`; produced
streams declare `expected_rows` and the consumer verifies, failing a truncated stream). The
**per-axis conformance vectors** (an idempotent-write case in `format-v1.json`; a
truncated-stream-→-fail vector) are **Phase 1** — they need the core's at-least-once driver
to be meaningful, and the board explicitly bucketed the enforcement layer there.

## Consequences

**Positive.**

- The data plane now has *one* coherent idempotency model across catalog-commit and
  format-write — a reconciler retry of a whole run is a no-op at every effect leg.
- A truncated bulk transfer is **detectable** instead of silently committing partial data —
  closing the concrete SCD2 history-corruption path the board found.
- The shapes are locked into `rat/1.5` while unpublished — no future `v1.2`/regret for these.

**Negative — accepted.**

1. **Enforcement is deferred to Phase 1**, so at `rat/1.5` a non-conformant plugin can still
   ignore `idempotency_key` or skip the `expected_rows` check — the fields are present and
   the obligation is written, but the *enforcer* (the core + per-axis vectors) is not built.
   This is the same honest split as `ArrowStream.ticket`; the honesty banners already say the
   core is not built. Mitigation: the composition demonstrates the intended behavior, so the
   shapes are proven usable, not speculative.
2. **More surface on the frozen write path** (one field on three format RPCs + strategy + two
   on `WriteResult`/`ArrowStream`). Accepted: it is the minimal additive set that makes the
   write path crash-safe, and it mirrors an existing model so there is nothing new to learn.

**Neutral.** `data.proto`/`format.proto`/`strategy.proto` `Status:` stay `v1`; the additions
land at the `rat/1.5` minor, like the catalog commit-linkage RPCs (ADR-010).

## Alternatives considered

1. **Defer C1/C2 entirely to Phase 1 (tag `rat/1.5` without them).** The board's "optional"
   path. **Rejected here by explicit choice** — locking the field shapes while the surface is
   local/unpublished is free now and avoids a `v1.2`; the shapes are better fixed while the
   data plane is fresh than reverse-engineered later.
2. **Put `idempotency_key` only on `strategy.ApplyRequest`, not on the format RPCs.**
   **Rejected** — the *format* is the effect leg that must dedup; a strategy key that never
   reaches the writer cannot prevent a double-`Append`. The key must travel to where the
   write commits.
3. **A required (non-`optional`) `expected_rows`.** **Rejected** — some producers genuinely
   cannot pre-count (a streaming transform); forcing a count would break them. `optional`
   lets a producer decline to declare and the consumer fall back to transport completeness —
   the A1 presence lesson applied.
4. **A terminal "end-of-stream" control message on the Flight channel instead of a count.**
   **Rejected for rat/1.5** — it changes the bytes-leg protocol (more than a descriptor field),
   and Arrow Flight already signals clean completion; the gap is distinguishing *clean* from
   *truncated*, which a declared count solves additively without touching the transport.

## Migration

Design from `rat/1.5` onward; no running core, no data migration. Sequence: add the fields
(additive, `buf breaking` FILE clean) → `make gen-sdks` → document the obligations in proto
comments + `format`/`strategy` `CONTRACT.md` → thread + honor them in `examples/composition`
→ `make conformance` (still 32/32 — per-axis vectors unchanged) + `make composition` green →
fold into the `rat/1.5` tag. The per-axis idempotency + truncation vectors land in Phase 1.

**Tag-scheme note.** The board/roadmap called the Phase-0 close-out target "`v1.1`" as
shorthand for "the first sealed minor after the `rat/1` freeze." But the git tags
`rat/1.1`–`rat/1.4` were already used for the *incremental per-axis freeze checkpoints*
(strategy `rat/1.1`, control-plane `rat/1.2`, deployment-runtime `rat/1.3`, experience
`rat/1.4`). So the sealed surface — this ADR + ADR-010 + ADR-011 + the doc tail — is tagged
**`rat/1.5`** (the next checkpoint in the same series), not `rat/1.1`. There is one version
*series* (`rat/1.N`), not two; "v1.1" was loose shorthand for the seal, which is `rat/1.5`.

## Related

- [reviews/08](../../../reviews/08-post-freeze-board-review.md) **C1**, **C2** (+ `sre` #1/#2,
  `contracts` #5) — the findings this ADR closes.
- [ADR-010](010-catalog-commit-linkage.md) — the catalog `idempotency_key`/`already_applied`
  model this mirrors (the catalog-commit leg; this ADR is the format-write leg).
- [ADR-009](009-data-plane-contract-freeze-v1.md) — the freeze; this is an additive on top.
- [`common/v1/data.proto`](../../../contracts/proto/rat/common/v1/data.proto) (`WriteResult`,
  `ArrowStream`) · [`format.proto`](../../../contracts/proto/rat/format/v1/format.proto) ·
  [`strategy.proto`](../../../contracts/proto/rat/strategy/v1/strategy.proto).
- [examples/composition](../../../examples/composition) — where C1/C2 are demonstrated.
