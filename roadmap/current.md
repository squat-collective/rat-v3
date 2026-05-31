# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-05-31 (🧊 **`rat/1` → `rat/1.3` FROZEN.** Data-plane (`rat/1`, [ADR-009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md)) + `strategy/v1` (`rat/1.1`) + **7 control-plane axes** (`rat/1.2`) + **`deployment-runtime/v1`** (`rat/1.3`: two divergent refs — local-process + k8s-dryrun, sharing the I9 gate). **`make conformance` 29/29**; `make composition` green. **Frozen `v1` = 15 axes** + `common/v1/*` + `core/v1/invoke` + `ERROR_MODEL.md`. **Still `v1-preview` (the whole remaining tail):** the 3 experience axes (ui/notifications/marketplace) + the manifest schema. **Next:** experience-axis refs + manifest freeze fully close Phase 0 — OR start **Phase 1 (the core)**; Gate A (`rat/1`) is satisfied. USER'S CALL.)

## Status one-liner

**Phase 0 in-flight (entered 2026-05-30).** 0a schema + all 20 0b axis protos drafted; adversarial agent-team review ([reviews/06](../reviews/06-proto-contract-review.md)) found 15 freeze-blockers + the AUTH-2 open decision (resolved → [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md), core-mediated). **Freeze-blocker remediation: #1–#9 done (the keystone + all structural wire fixes) + #10a (debug_redact); every change buf lint/build/generate-clean.** Remaining is the *additive/GA-safe* tail only — #10b (manifest `artifact`/digest block) + #9f doc-pins — which can land after the `rat/1` freeze without breaking plugins. So: **all freeze-BLOCKING structural changes are complete; the contract is materially freeze-ready** pending the additive tail + the ADR-003 two-reference rule (0d). Multi-language SDKs (Go/Python/TS/Rust) generated + committed under `contracts/sdks/`. **0d — `format/v1` ADR-003 two-reference gate MET (2026-05-31):** two independent references — `examples/format/inmemory-go/` (now mediated through a stub ADR-005 invoke-gateway) and `examples/format/inmemory-py/` (from-scratch second impl) — both pass the SAME shared golden vectors (`contracts/conformance/format-v1.json`), green in golang:1.25 and python:3.12 respectively. The stub invoke-gateway (panel recommendation) landed too, so the cross-run exercises the ADR-005 control-path mediation seams, not just plugin-to-plugin. **`format/v1` cannot advance `v1-preview`→`v1` yet:** two blockers remain — (1) the identity-transport decision the gateway surfaced (payload.context vs channel metadata; see ideas/inbox.md), (2) a typed-Arrow conformance pass (bulk leg is still an in-process stand-in). **Next:** resolve the identity-transport question (likely an ADR amending 005), then pick the second data-plane axis for 0d (`engine` or `storage`).

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
6. ✅ **DONE** — Added `core/v1/invoke.proto`: `CapabilityInvokeService.Invoke(capability URI + opaque payload bytes) → opaque result bytes` (ADR-005 core-mediated). Generic proxy — gateway routes by capability + resolves provider via registry + (rat.capability) annotation, enforces C2/C5/C7/C3 + stamps C1 + emits C8 audit, relays the serialized axis request/response without interpreting it. Bulk data still bypasses core via ArrowStream. buf clean (generate 41).
7. ✅ **DONE** — Added `common/v1/event.proto`: the `Event` envelope for the async plane (ARCH-1). Carries the same `RequestContext` sync RPCs carry (trace+identity+tenant) so tracing/tenant-isolation work across the async boundary, plus `event_id` (idempotent redelivery), `type` (subscription match), `timestamp`, `source`, opaque `payload`, optional `partition_key` (ordered delivery). Core-stamped identity; transport pluggable (ADR-002 D2/D4), routes by type+tenant without interpreting payload. buf clean (generate 42).
8. ✅ **DONE** — `catalog.MergeBranch`: added `expected_into_snapshot` (optimistic-concurrency guard vs lost-update from concurrent merges) + `idempotency_key` (no-op on reconciler retry) to `MergeBranchRequest`; added `already_applied` bool to the response. The separate commit-linkage RPC (how catalog learns what format.Write wrote) stays GA-deferred. buf clean (generate 42).
9. 🔶 **IN PROGRESS** — the small-wire-fix cluster, split into sub-commits:
   - ✅ **9a** (`22b76e2`) — `secret.Resolve.found` semantics pinned (anti-enumeration).
   - ✅ **9b** (`fcbe8bb`) — decision-RPC error model: `deny_code` enum on `identity.Authorize` + `tenancy.Decide`; `reason` demoted to log-only.
   - ✅ **9c** (`9c25c26`) — `data.proto` ArrowStream protocol (`transport`=FLIGHT) + role/direction field; `observability.Ingest` → bidi-streaming.
   - ✅ **9d** (`f290601`) — `slots.target` wrapped in `capabilityRef` (schema + scd2 example, re-validated).
   - ✅ **9e** (`277a09f`) — sentinel→`optional` (rows_affected, fraction); `options` UTF-8-JSON encoding pin; scheduler at-least-once delivery doc.
   - ⬜ **9f** (optional doc-pins, low value) — `state.List`/`marketplace.Search` pagination-default note; timestamp int64-ms ratification note. Comments only, arguably post-freeze-safe. ← deferred; **#10 next**
10. Land cheap additive placeholders (manifest `image` digest, `debug_redact`; audit signature already done in #4).

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

**🎉 0d ROUND 1 + ROUND 2 are BOTH COMPLETE.** All six data-plane axes have:
- **Round 1 (wire contract):** two independently-written language refs (Go + Python) passing one shared golden-vector file — 12 refs, all green; routed through the stub gateway (unary + server-streaming).
- **Round 2 (semantic):** a technologically-divergent REAL backend passing the same vectors + a backend-specific test: `state`=sqlite (durability + linearizable CAS), `storage`=local-fs (containment + isolation), `catalog`=sqlite (durable branches + concurrent-merge safety), `runtime`=subprocess (process isolation), `engine`=duckdb+datafusion (real SQL + typed Arrow), `format`=parquet+delta (real Arrow files + time travel).

ADR-007 + ADR-008 decided AND migrated; typed-Arrow gap retired for engine+format (real Arrow IPC). The full ADR-003 rigor is satisfied for every data-plane contract.

**Genuinely remaining toward `rat/1` freeze (no longer reference-impl work):**
1. **Real Arrow Flight transport — ✅ DONE** (`examples/format/parquet-py/flight.py`): the data leg now crosses real TCP sockets via Flight `DoGet`, both directions, with zero contract change — proving the in-process registry was always a transport choice. (Other refs keep the in-process stand-in for simplicity; making them all Flight is optional polish.)
2. **Sub-phase 0f — ✅ COMPLETE.** Conformance suite runner (`make conformance` → auto-discovers + runs every reference → one pass/fail matrix; **20/20 conform**; CI/freeze-gateable) + per-RPC latency benchmark (`make bench` → core-mediation hop ≈ **+0.2ms / +270%** at p50, unary + streaming; absolute is sub-ms, bulk bypasses via ArrowStream — validates ADR-005's "the hop is acceptable" bet with real numbers).
3. **Sub-phase 0g — ✅ DONE for the data-plane axes**: per-axis `CONTRACT.md` author guides (`contracts/proto/rat/{state,engine,format,storage,runtime,catalog}/v1/CONTRACT.md`), indexed in the conformance README. Control/experience axes get theirs when referenced.
4. **Sub-phase 0c — ✅ DONE.** Cross-cutting protos finalized: audited every C1–C10/ARCH concern → [coverage doc](../docs/architecture/cross-cutting-coverage.md); the one finding (`AuditRecord` in the auditlog axis proto) fixed by moving it to `common/v1/audit.proto` (wire-compatible). Set is final: `common/v1/{context,data,annotations,event,audit}` + `core/v1/invoke`.
5. **Sub-phase 0h** — peer review + the `rat/1` freeze itself. The remaining gate: a final adversarial pass over the now-complete contract + reference + conformance surface, then tag the data-plane axis contracts `v1`.
4. **Polish (not blocking):** real **SDK metadata interceptor** (so plugin code gets the reconstructed context automatically); wire `InvokeBidiStream` (`observability.Ingest` bidi relay) when referenced; roll `(rat.capability)` across the remaining control/experience axis services (strategy, identity, tenancy, deployment-runtime, scheduler, secret, observability, audit-log, ui, notifications, marketplace, billing).

SDK distribution + layout + codegen are decided in **[ADR-006](../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md)**: vendored `contracts/sdks/<lang>/` (Go/Python/TS peers), reference plugins under `examples/<axis>/<impl>-<lang>/`, containerized `buf generate` via `scripts/gen-sdks.sh`.

**0d — `format/v1` ADR-003 gate is now MET (2026-05-31).** Two independent references both pass the SAME shared golden vectors (`contracts/conformance/format-v1.json`):
- **inmemory-go** (commit `c472620`, then refactored): now loads the shared JSON and runs **through the stub core-mediated gateway** (`gateway_test.go`, a faithful ADR-005 generic byte-relay — routes by the `(rat.common.v1.capability)` annotation, enforces C5, emits C8 audit). Green in `golang:1.25`.
- **inmemory-py** (`examples/format/inmemory-py/`): from-scratch second reference, imports the vendored Python SDK, loads the same JSON. Green in `python:3.12` (grpcio 1.80.0 / protobuf 7.35.0).

> **Scope caveat on "ADR-003 gate MET" (be honest about this):** the per-axis "gate MET" claims below mean the **wire-contract two-reference cross-run** — two independent *code paths* (Go + Python) pass one shared golden-vector file. They do **not** yet satisfy ADR-003's stronger intent: "different *underlying technologies* … different consistency/semantic profiles" (its iceberg-vs-delta / sqlite-vs-postgres examples). Both impls per axis are in-memory twins, so the semantic-divergence + real-data-leg findings ADR-003 cares most about are still deferred to **round 2** (see gate #2 + #3). Round 1 (now) locks the wire contract for all 6 axes; round 2 makes one reference per axis a real divergent backend before that axis → `v1`.

**What gates the data-plane axes (`format/v1`, `engine/v1`, …) advancing `v1-preview` → `v1`:**
1. **Identity-transport — DONE ✅ ([ADR-007](../docs/architecture/adrs/007-call-context-transport.md) decided + migrated).** `RequestContext` moved from message field 1 to the `rat-callmeta-bin` metadata header across all 37 control sites; the gateway now validates traceparent + re-stamps identity from metadata without touching the payload; all refs green on the unchanged vectors. No longer a blocker.
2. **Round-2: a technologically-divergent reference per axis.** The current cross-runs use inmemory-go + inmemory-py — same underlying tech (a hashmap), two languages. ADR-003's intent needs one *real backend* per axis with a different consistency/semantic profile (e.g. `storage`=local-fs, `state`=sqlite — both cheap + high-value; `format`=parquet/iceberg; `engine`=duckdb). This is where the "orthogonality assumption" failures surface. Required before any axis → `v1`. (See [backlog.md](backlog.md).)
3. **Typed-Arrow conformance pass** — every data-plane ref with a bulk leg (format + engine) still carries it as an in-process registry stand-in; the real Arrow Flight wire is unexercised. (storage + runtime have no bulk leg under test.) Shared remaining item before those axes freeze; naturally folds into round 2.

The deferred **additive tail** is still open and cheap to clear while in the contracts: #10b (manifest `artifact`/digest block), #9f doc-pins, per-kind manifest schemas, and rolling `(rat.capability)` across the other 14 axis services.

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
