# ADR-009: Freeze the data-plane axis contracts at `v1` (`rat/1`)

## Status: Accepted (2026-05-31)

## Context

[ADR-003](003-two-references-before-contract-freeze.md) set a hard process gate: no
data-plane contract may advance `v1-preview` → `v1` until **two independent reference
implementations exist, both pass the axis's conformance suite, and have been run
against each other on golden data** (the cross-combination matrix). Phase 0 has been
building toward that gate axis by axis. This ADR records that the gate is met for the
data-plane and freezes those contracts.

Two things had to be true before this ADR could be written:

1. **The freeze review punch-list is cleared.** The 0h freeze review
   ([reviews/07](../../../reviews/07-freeze-review.md)) — a final adversarial pass over
   the complete contract + reference + conformance surface — returned **NO-GO for an
   unconditional freeze**, finding 4 must-fix items (M1 error-model convention unpinned,
   M2 not-found modeled inconsistently, M3 signatures lacked `key_id`, M4 assertion
   verification gap) + 4 should-fix items (S1–S4). All 8 are now remediated (commits
   `16d9c37`, `7e169e1`, `df07ff9`), buf-clean, conformance held 20/20.

2. **The ADR-003 cross-combination gate is met.** Previously only the *per-axis* gate
   was satisfied (two refs per axis on shared golden vectors). Sub-phase **0i**
   (`abd1228`) built the first `strategy` reference + a cross-axis composition test
   ([examples/composition](../../../examples/composition)) that boots catalog + engine +
   format together, wired by capability, with Arrow over real Flight, and runs the
   strategy across ADR-003's four cross-combinations (baseline + format/catalog/engine
   substitution, storage held at A) on shared golden data
   ([composition-v1.json](../../../contracts/conformance/composition-v1.json)). All four
   produce the identical target with the strategy code unchanged.

## Decision

**The data-plane axis contracts and the cross-cutting types they depend on advance
from `v1-preview` to `v1` and are tagged `rat/1`. Breaking changes to them now require
a `v2` (the ADR-003 rule applies in reverse).**

### What freezes at `v1` (`rat/1`)

The six ADR-003-gated data-plane axes — each with two independent references, a
technologically-divergent real backend, passing shared golden vectors, and exercised
in the cross-combination test:

- `engine/v1` — DuckDB + DataFusion (real typed Arrow)
- `format/v1` — Parquet + Delta
- `catalog/v1` — sqlite + in-memory
- `storage/v1` — local-fs + in-memory
- `runtime/v1` — subprocess + in-memory
- `state/v1` — sqlite + in-memory

…and the **cross-cutting wire types** every data-plane axis depends on, which the
references + composition exercised and the freeze review hardened:

- `common/v1/context.proto` (incl. the M3/M4 `key_id` + verification hardening)
- `common/v1/data.proto`, `common/v1/annotations.proto`, `common/v1/event.proto`,
  `common/v1/audit.proto`
- `core/v1/invoke.proto` (the capability-invoke gateway, all three cardinalities)
- `common/v1/ERROR_MODEL.md` (the M1/M2 pinned convention)

### What stays `v1-preview`

- **`strategy/v1`** — data-plane, but **not** in ADR-003's two-reference list, and it
  currently has **one** reference (`fullrefresh-py`). It is proven in composition, but
  in keeping with the two-reference discipline it stays `v1-preview` until a second,
  semantically-different strategy (e.g. scd2, soft-delete) lands. Its shape is not
  expected to change; this is conservatism, not doubt.
- **Control-plane axes** (`identity`, `secret`, `scheduler`, `tenancy`, `audit-log`
  sink, `observability`, `notifications`, `marketplace`, `billing`) and **experience**
  (`ui`) — ADR-003 §"does NOT apply" requires only one reference + conformance for these;
  they freeze when their reference work lands, not here.
- **The plugin manifest schema** (`plugin/v1`) — iterate until it stabilizes across the
  remaining reference work (ADR-003).

### Accepted residuals (documented, not blocking)

The freeze review's three residuals are accepted into `v1` as known, additive-or-
bounded properties (tracked in [backlog](../../../roadmap/backlog.md)):

- **R1** — `SubjectAssertion` is bound to the operation (`correlation_id`), not the
  hop/capability: a bounded confused-deputy, blast radius = the operation's C5-declared
  capability set. Revisit if finer user-presence proof is needed.
- **R2** — storage `VendCredentials` tenant-scoping is a per-impl property (ADR-005's
  one acknowledged direct-dial bearer exception; the core can't inspect an STS blob).
- **R3** — the catalog has no create-table RPC; new-table commit-linkage is GA-deferred
  (catalog.proto). All post-freeze changes here are additive (new RPC/fields).

## Consequences

**Positive.**

- The headline ADR-003 risk — discovering a wire-breaking flaw *after* publication — is
  retired for the data plane: six axes are proven by independent real backends AND a
  cross-axis pipeline, not by design-in-a-vacuum.
- Plugin authors get a stable target. The references double as starter templates.
- The cross-cutting envelope (identity/audit/error-model) is frozen with its security
  hardening in place (keystone + M3/M4), so the un-retrofittable parts are locked
  correctly.

**Negative — accepted.**

1. **Breaking changes now cost a `v2` + flag day.** That is the entire point of the
   gate; the cost is intended. Mitigation: the two-reference + composition gates make a
   forced early `v2` unlikely.
2. **`strategy/v1` lagging the other data-plane axes at `v1-preview`** is a small
   asymmetry — a strategy author integrates against a still-movable contract for now.
   Mitigation: its shape is composition-proven; the second reference is queued.
3. **The freeze is only as good as the conformance + composition coverage.** Golden
   data is finite; a contract assumption outside the vectors could still surface.
   Mitigation: the suites are additive — new vectors extend coverage without breaking
   `v1`.

**Neutral.**

- The `rat/1` git tag marks the frozen commit; it is local until the project publishes.
  Tagging is reversible (a tag is a pointer) right up until external consumers pin to it.

## Alternatives considered

1. **Conditional freeze — tag `rat/1` after remediation, treat cross-axis composition as
   a tracked residual.** The freeze review offered this as the faster path. **Rejected
   because** the user chose strict ADR-003: build the composition first. It was built,
   it passed, and a literal gate beats a promised one — the whole reason ADR-003 exists.
2. **Freeze `strategy/v1` too (one reference + composition).** **Rejected because** ADR-003's
   two-reference discipline is the project's anti-regret rule; honoring it for the
   composing axis costs little (its shape is stable) and avoids a special-case exception
   so soon after writing the rule.
3. **Keep everything `v1-preview` until the control plane also has references.**
   **Rejected because** ADR-003 deliberately scopes the hard gate to the data plane; the
   data-plane contracts are the expensive-to-change, highly-coupled ones, and they are
   ready now. Waiting for loosely-coupled control-plane axes would forfeit the value of
   a stable data-plane target.

## Migration

- Proto `Status:` headers for the frozen files move `DRAFT (pre-freeze)` / `v1-preview`
  → `v1 (frozen — rat/1)`; each axis `CONTRACT.md` status line likewise.
- Tag the freezing commit `rat/1`.
- `buf breaking` becomes a CI gate against `rat/1` for the frozen packages (additive-only).
- The roadmap records Phase 0's contract-freeze milestone reached.

## Related

- [ADR-003](003-two-references-before-contract-freeze.md) — the gate this ADR satisfies.
- [reviews/07-freeze-review.md](../../../reviews/07-freeze-review.md) — the 0h freeze
  review (punch-list + cross-axis gap) this ADR closes.
- [examples/composition](../../../examples/composition) — the 0i cross-combination test.
- [ADR-005](005-capability-invocation-model.md) / [007](007-call-context-transport.md) /
  [008](008-streaming-capability-invocation.md) — the cross-cutting decisions now frozen.
