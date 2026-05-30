# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-05-30 (after `.claude/` configuration + ADR-003 + roadmap structure + synthesis landed; core language locked to Go via ADR-004 + coding-phase allowlist)

## Status one-liner

**Phase 0 in-flight (entered 2026-05-30).** 0a manifest schema + all 20 0b axis protos drafted & buf-clean. **Adversarial agent-team review ([reviews/06](../reviews/06-proto-contract-review.md)) found the contract is NOT freeze-ready — 15 freeze-blockers.** The 1 open design decision (AUTH-2 invocation model) is now resolved → [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md) (core-mediated). Next: apply the 15 fixes, starting with the identity keystone (`context.proto`).

> Commitment-gate note: `phases.md` flags a 12–18mo runway + GTM commitment as a pre-Phase-0 gate. Tom chose to proceed in exploratory/sandbox mode. Gate acknowledged, not formally cleared — revisit before investing the full 4–6mo of Phase 0.

## Active streams

### Stream 1 — Phase 0: lock the contracts

**Status:** in-flight. Sub-phase 0a drafted.

Entered Phase 0 on 2026-05-30 (exploratory mode — see commitment-gate note above). First artifact: the manifest envelope schema.

**Done so far:**
- `contracts/` workspace created (`schema/`, `proto/`, `examples/`).
- **0a:** `contracts/schema/plugin.v1.json` — manifest envelope schema (JSON Schema 2020-12), Critical fields C4/C5/C8 baked in. Two valid example manifests + negative-vector doc; container-validated (all green). Per-kind-schema decision recorded.
- **0b axis protos COMPLETE:** **20 proto files** (18 axis services + 2 common). Every v1 axis from ADR-001 has a wire contract. Data plane (engine, runtime, format, strategy, catalog, storage), control plane (state, identity, tenancy, deployment-runtime, scheduler, secret, observability, audit-log), experience (ui, notifications, marketplace), business (billing). **buf lint + build + generate all clean**, verified in container across every batch.
- Critical concerns with a wire home: C1 (context), C2 (identity), C3 (state namespacing), C5 (provides/enforcement), C7 (tenant: context + storage scope + tenancy + billing), I8 (audit hash-chain), I9 (deployment isolation profile), I13 (secret contract).

**Next concrete step:** apply the 15 freeze-blockers from [reviews/06](../reviews/06-proto-contract-review.md). The AUTH-2 invocation-model open decision is now **resolved → [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md): core-mediated** (its impl, `core/v1/invoke.proto`, is item 6 below). Dependency order:
1. ✅ **DONE** (commit `322126c`) — Rewrote `common/v1/context.proto` for the three-principal keystone: `TraceContext` (propagate verbatim) + `Identity` {caller_plugin (server-derived per-hop, never propagated), subject (core-signed `SubjectAssertion`: signature + correlation_id-binding + TTL, re-validated per hop), tenant (server-stamped)}. Fixed the AUTH-12 contradiction in `state.proto` (namespace keys on `identity.caller_plugin`) + `context.identity.*` comment refs in storage/secret/billing/tenancy/identity. buf lint+build+generate clean.
2. ✅ **DONE** — Renamed `format` capability URIs `rat://format-capability/v1/*` → `rat://format/v1/*` across format.proto, strategy.proto, both example manifests, INVALID-examples, schema/README, and overview.md (`kind: format-capability` → `kind: format`). Historical files (reviews/, conversations/) deliberately left as-is. buf clean + manifests re-validate.
3. ✅ **DONE** — `state.proto`: constrained `key`/`prefix` grammar (non-empty, ≤512B, UTF-8, no NUL/control chars, no `.`/`..` traversal → INVALID_ARGUMENT); replaced `PutResponse.committed:bool` with `PutOutcome` enum (COMMITTED/CONFLICT/UNKNOWN — the lease-fencing fix); turned linearizable-CAS + ordered-Watch into a stated conformance obligation; resolved the DynamoDB contradiction (strongly-consistent-mode-or-solo-only, annotated in overview.md topology table). buf clean.
4. ✅ **DONE** — `auditlog.proto`: replaced `AppendResponse.appended:int64` with per-record `RecordAck` (`AppendStatus` COMMITTED/DUPLICATE/REJECTED + `RejectCode`), prefix-only commit + `last_committed_id`/`last_committed_hash` chain-head watermark; added core `signature` (Ed25519) to `AuditRecord` + pinned canonical serialization; Append marked core-only; core (not sink/caller) assigns `id`/`prev_hash`. buf clean (generate 38).
5. ✅ **DONE (freeze-critical part)** — Added `common/v1/annotations.proto` defining the `(rat.common.v1.capability)` method option (machine-readable capability⇄method binding for the C5 gateway + C6 harness). Split `format.Write` → `Append`/`Merge`/`Overwrite` RPCs (the breaking change; gives `overwrite` the capability URI it lacked + makes append≠merge method-level enforceable). Annotated format + engine (engine needed NO split — its 3 methods were already 1:1; noted in-proto). buf clean; manifests re-validate. **Additive follow-on (NOT freeze-blocking):** roll `(rat.capability)` across the other 14 axis services — tracked in backlog.
6. **Add `core/v1/invoke.proto`** — the core-mediated capability-invoke service (ADR-005). The missing API-gateway contract; routes by capability URI, enforces C2/C3/C5/C7/C8 + stamps C1. ← **DO NEXT**
7. **Async event-bus envelope** (`common/v1/event.proto`: trace+correlation+tenant+event_id+dedup) OR explicitly carve the async plane out of the `rat/1` freeze.
8. **`catalog.MergeBranch`**: add `expected_snapshot` + `idempotency_key` to the request.
9. Error convention + `secret.Resolve.found` semantics; Arrow protocol+role field; `Ingest` streaming shape; timestamp type; `slots.target` wrap; the freeze-slivers (options encoding, pagination default, scheduler delivery doc, optional-presence).
10. Land cheap additive placeholders now (audit signature field, manifest `image` digest, `debug_redact`).

Re-run `buf lint/build/generate` (containerized) after each. Then 0c event-bus envelope + per-kind schemas, then 0d reference implementations.

**Note:** the `gofmt`/`buf format` PostToolUse hook was evaluated and **rejected** (containerized formatter per-edit is 10–40× the tool cost; batch `buf format` before commits instead — see done.md). Manifest-validator container image for `rat plugin validate` (0f) still TBD.

### Stream 2 — Roadmap + ADR upkeep

**Status:** done as of this commit.

The synthesis raised 26 prospective ADRs; we DIDN'T write all of them. Instead we landed:
- ADR-003 (two-reference rule — the most-cited synthesis recommendation)
- Updated ADR-001 Phase 0 description (bakes Critical concerns into Phase 0)
- Updated vision.md (added GTM anti-goals)
- Created this roadmap structure

The 23 other prospective ADRs are in [backlog.md](backlog.md). They land as they become relevant — most are Phase-0-blocking and get written during Phase 0.

## Immediate next concrete step

Sub-phase **0b — axis protos**. In `contracts/proto/`:
1. Write `strategy/v1/strategy.proto` (the `Apply` RPC ⇒ `rat://strategy/v1/apply`).
2. Write `format/v1/format.proto` (scan/merge/append ⇒ `rat://format-capability/v1/*`).
3. Write `runtime/v1/runtime.proto` (`Execute` ⇒ `rat://runtime/v1/execute`).
4. These three are the ones the example manifests already reference, so they close the loop between manifest and wire contract first.
5. Before generating any SDK: add `buf.yaml` + decide the validator/codegen container image (also unblocks 0f tooling). This is where the Go/buf toolchain in `.claude/settings.json` first gets exercised.
6. As each axis proto lands, derive its per-kind manifest schema (the 0a → 0b handoff recorded in `contracts/schema/README.md`).

Also pending (deferred Claude-config item, now triggerable since the first `.proto`/code is imminent): the `PostToolUse` auto-format hook (`gofmt`/`buf format`) — see [backlog.md](backlog.md). Land it when the first proto/Go file is committed.

## What's NOT in flight (paused / cancelled)

- Phase 0 sub-phases 0c–0h — not started
- Phase 1-5 — not started
- The 23 other prospective ADRs from synthesis — backlogged (ADRs 004–013 land during Phase 0 as contracts are drafted)

## Maintenance reminder

When this stream's status changes (e.g., Tom commits and Phase 0 kicks off, or a new working session produces concrete output):

1. Update this file (`current.md`).
2. Append the completed work to [done.md](done.md).
3. Update [phases.md](phases.md) status table.
4. Promote any new identified work from inbox / reviews → [backlog.md](backlog.md).

See [CLAUDE.md](CLAUDE.md) in this directory for the full maintenance rules.
