# Current — what's in flight right now

> **Always read this first when opening a Claude session on this project.**
> Updated: 2026-05-30 (after `.claude/` configuration + ADR-003 + roadmap structure + synthesis landed; core language locked to Go via ADR-004 + coding-phase allowlist)

## Status one-liner

**Phase 0 in-flight (entered 2026-05-30).** 0a manifest schema + all 20 0b axis protos drafted & buf-clean. **Adversarial agent-team review ([reviews/06](../reviews/06-proto-contract-review.md)) found the contract is NOT freeze-ready — 12 freeze-blockers to fix first.** Next: apply those fixes, starting with the identity keystone (`context.proto`).

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

**Next concrete step:** apply the 12 freeze-blockers from [reviews/06](../reviews/06-proto-contract-review.md), in dependency order:
1. **Rewrite `common/v1/context.proto`** for the three-principal keystone (caller_plugin server-derived per-hop; subject = core-signed, correlation_id-bound, re-validated per hop; tenant server-stamped; trace fields structurally separated). Everything keys off this — do it first.
2. **Rename `format` capability URIs** `rat://format-capability/v1/*` → `rat://format/v1/*` (match package; fix examples).
3. **State**: constrain `key`/`prefix` grammar (no traversal) + make linearizable-CAS a conformance gate + resolve the DynamoDB-vs-linearizability contradiction.
4. **Audit**: replace `AppendResponse.appended` with per-record `RecordAck` (prefix-only) + pin canonical serialization + core-assigns id/prev_hash.
5. **annotations.proto + `(rat.capability)` method option + split `Write`/`engine.Execute` per-mode** (do together).
6. Error convention + `secret.Resolve.found` semantics; Arrow protocol+role field; `Ingest` streaming shape; timestamp type; `slots.target` wrap; the freeze-slivers (options encoding, pagination default, scheduler delivery doc, optional-presence).
7. Land cheap additive placeholders now (audit signature field, manifest `image` digest, `debug_redact`).

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
