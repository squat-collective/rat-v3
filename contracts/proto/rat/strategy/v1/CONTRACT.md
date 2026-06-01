# `strategy/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugins here are
> real and frozen (`rat/1.1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: strategy` plugin. Pairs with the wire contract
> [`strategy.proto`](strategy.proto) + the conformance vectors below. Status: **v1 (frozen —
> rat/1.1, ADR-009 trigger: two strategy references — fullrefresh-py + scd2-py — validate the
> contract)**.

## What a `strategy` plugin is

A `kind: strategy` plugin (full-refresh, scd2, soft-delete, incremental, …) encodes **HOW data
is transformed and loaded** for one pipeline run. It is the cleanest expression of the capability
model: a strategy `requires` format capabilities (scan/merge/overwrite) + catalog capabilities
(get-table/register-table/commit-table) + optionally engine capabilities, and works across
**every** provider that offers them, naming none. It reaches those providers exclusively through
the core capability-invoke gateway ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md)),
which resolves each capability URI to a concrete provider via the registry and mediates the call.
The strategy never holds a stub, port, or class of any peer plugin.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://strategy/v1/apply` | `Apply` | apply the strategy for one pipeline run |

## The RPCs

- **`Apply(source, target, options, idempotency_key)` → `{result: WriteResult}`** — orchestrate one pipeline run.
  The strategy resolves `source` (a `TableRef`) and `target` (a `TableRef`) via its required
  capabilities, executes its transform/load logic, and returns a `WriteResult` describing the
  outcome. `options` carries strategy-specific per-run parameters (see CONFORMANCE below).
  `source` or `target` empty-identifier → `INVALID_ARGUMENT`; `options` that fail schema
  validation → `INVALID_ARGUMENT` (not a language exception — see CONFORMANCE).

## Conformance obligations

### `options` validation (reviews/08 E9)

`Apply.options` is UTF-8 JSON, encoding-pinned at freeze (reviews/06 API-12). The bytes are
opaque to the core; the strategy decodes them per its declared `metadata_schema` (manifest,
[ADR-011](../../../../../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md)).

**A strategy MUST validate its `options` against its declared `metadata_schema` before executing
any capability call, and MUST map a missing, misspelled, or type-wrong key to `INVALID_ARGUMENT`
— NOT a raw language exception.** A `KeyError`, `TypeError`, or `AttributeError` that escapes the
plugin is a conformance violation. Empty `options` where the schema requires fields → `INVALID_ARGUMENT`.

### Composition and cross-axis proving (ADR-003)

The strategy axis has no standalone per-axis conformance harness. Proving `strategy/v1` requires
two independent reference implementations run against each other on golden data:

1. **Cross-combination gate** ([`composition-v1.json`](../../../../conformance/composition-v1.json)):
   the full-refresh strategy (`fullrefresh-py`) is driven through the four ADR-003 cross-axis
   combinations (baseline + one-axis substitution for format, catalog, engine) via
   `examples/composition/`. Every combination must produce the identical `expected_target`. This
   proves the strategy couples to no concrete provider — the code does not change; only the
   providers wired beneath it change.

2. **SCD2 second reference** ([`strategy-scd2-v1.json`](../../../../conformance/strategy-scd2-v1.json)):
   the `scd2-py` strategy is exercised over two sequential temporal runs on the real composition
   stack (gateway + format + catalog + Arrow Flight). The two-run expected history gates the
   contract's ability to serve a genuinely divergent code path — stateful and temporal, vs.
   full-refresh's stateless transform-and-replace.

Pass **both** vectors via `make conformance` for `strategy/v1` to be considered conformant.

### Capability coupling discipline

A strategy MUST reach every peer axis (catalog, format, engine, …) only through the `invoke`
gateway by capability URI. It MUST NOT:
- Import or directly instantiate a peer plugin's class or stub.
- Bypass the registry to discover peers.
- Bypass the event bus to coordinate state changes.
- Call a capability URI not declared in its manifest `requires` (the gateway enforces C5:
  `PERMISSION_DENIED`).

### Commit-linkage (ADR-010)

A strategy that writes a target table SHOULD close the create→write→register→merge loop by
calling `rat://catalog/v1/commit-table` after `format.Write`, passing the `WriteResult.snapshot_id`
the write returned and a stable `idempotency_key` for the run. This records which snapshot the
write produced in the catalog (the commit-linkage). A strategy that writes an unversioned format
(where `WriteResult.snapshot_id` is absent) simply does not commit-link.

### Idempotent runs ([ADR-012](../../../../../docs/architecture/adrs/012-crash-safety-additive-fields-v1.1.md))

`ApplyRequest.idempotency_key` is the run id under an **at-least-once** scheduler. A strategy
MUST thread it down to its `format.Write` calls (`format.*Request.idempotency_key`) **and** its
`catalog.commit-table` call, so a re-applied run is a **no-op end-to-end** — the format returns
`WriteResult.already_applied=true` and the catalog returns `already_applied=true`, and no effect
leg writes twice. When the strategy consumes a bulk `ArrowStream` (a producer/consumer strategy
like SCD2), it MUST honor the stream's `expected_rows`/`expected_batches` and fail the run on a
truncated transfer rather than write a partial result (C2).

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response fields for normal domain outcomes.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the strategy implements a plain gRPC `StrategyService` server.

## Writing a plugin

1. Implement `StrategyService.Apply(source, target, options)`.
2. Validate `options` (UTF-8 JSON) against your `metadata_schema` **first**; map any missing or
   invalid field to `INVALID_ARGUMENT` before touching any capability.
3. Declare your `requires` set in the manifest (only the capability URIs you actually call). The
   gateway denies calls outside this set.
4. Reach every peer by capability URI through the `invoke` gateway — never by name.
5. Close the create→write→register loop: call `register-table` (idempotent) before writing; call
   `commit-table` after writing (if your format reports a `snapshot_id`).
6. Pass both conformance vectors (composition cross-matrix + SCD2 temporal scenario) via
   `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/strategy/fullrefresh-py`](../../../../../examples/strategy/fullrefresh-py/store.py) | 1 (capability composition) | the capability-composition showcase: read source via catalog → register target → engine.query → format.overwrite → commit-table, coupling to zero concrete providers; proven across the 4-combo composition cross-matrix |
| [`examples/strategy/scd2-py`](../../../../../examples/strategy/scd2-py/store.py) | 2 (stateful/temporal) | the deliberately-divergent second reference: stateful SCD2 history over two temporal runs via format.scan × 2 + format.merge, no engine; different capability mix, same contract |

## Related

[`strategy.proto`](strategy.proto) ·
[`composition-v1.json`](../../../../conformance/composition-v1.json) ·
[`strategy-scd2-v1.json`](../../../../conformance/strategy-scd2-v1.json) ·
[`format/v1/CONTRACT.md`](../../format/v1/CONTRACT.md) (scan/merge/overwrite capabilities the strategy invokes) ·
[`catalog/v1/CONTRACT.md`](../../catalog/v1/CONTRACT.md) (get-table/register-table/commit-table) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (two-reference gate) ·
[ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md) (capability-invoke gateway) ·
[ADR-010](../../../../../docs/architecture/adrs/010-catalog-commit-linkage.md) (commit-linkage) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) E9
