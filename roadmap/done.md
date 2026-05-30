# Done — completed work log

Reverse chronological. Each entry: date, what was accomplished, links to artifacts (commits, files, ADRs).

---

## 2026-05-30 — Freeze-blocker #5: capability annotations + format.Write split

**What:** reviews/06 I-3 + I-4 (AUTH-8 + AUTH-9). Freeze-critical parts done; cross-axis annotation rollout deferred as additive.

1. **`common/v1/annotations.proto` (new):** `extend google.protobuf.MethodOptions { string capability = 70001; }` — the machine-readable capability⇄method binding. The mapping from capability URI → `(service, method)` previously lived only in comments, unreadable by the C5 enforcement gateway (ADR-005) and C6 conformance harness. Must be in the frozen `rat/1` surface (freeze-dependency). buf accepts the custom option; `annotations.pb.go` generates.

2. **Split `format.Write` → `Append`/`Merge`/`Overwrite` (breaking → freeze):** the single `Write` RPC keyed by a `WriteMode` enum meant a plugin that `provides` only `append` couldn't be enforced at method level. Now each is a distinct RPC 1:1 with a capability; `overwrite` gets the `rat://format/v1/overwrite` URI it previously lacked. `WriteMode` removed; per-RPC request+response messages (`{Append,Merge,Overwrite}Request/Response` — buf STANDARD requires a unique response type per RPC, so no shared `WriteResponse`); `merge_keys` only on Merge.

3. **Annotated format (5 methods) + engine (3).** **Engine needed NO split** — execute/query/preview were already 3 distinct RPCs 1:1 with capabilities; the blocker's "split engine.Execute" wording didn't apply, so engine just got the annotation. Noted in-proto.

**Deferred (additive, NOT freeze-blocking):** roll `(rat.capability)` across the other 14 axis services — adding a method option is wire-compatible (`buf breaking` FILE doesn't flag it). Tracked in backlog; land before the C5 gateway / C6 harness.

**Verified:** buf lint 0 / build 0 / generate 39 Go files (annotations.pb.go + the split format messages); both example manifests re-validate (deltalake's scan/merge/append capabilities all survive the split); dup-free. (Caught + fixed a buf STANDARD failure pre-commit — initial shared `WriteResponse` violated "unique response type per RPC".)

**Next:** freeze-blocker #6 — add `core/v1/invoke.proto` (the ADR-005 core-mediated capability-invoke service).

**Files:** `contracts/proto/rat/common/v1/annotations.proto` (new), `contracts/proto/rat/format/v1/format.proto` (split + annotate), `contracts/proto/rat/engine/v1/engine.proto` (annotate).

---

## 2026-05-30 — Freeze-blocker #4: auditlog.proto tamper-evidence + completeness

**What:** reviews/06 C-3 (SEC-5 ⊕ API-5) — the audit trail was "tamper-evident" in name only and couldn't report partial failure. Four coupled fixes to `auditlog.proto`:

1. **Core authors the chain, not the sink/caller:** `id` + `prev_hash` are core-assigned; `Append` is **core-only** (capability not grantable to ordinary plugins) → no plugin can inject records or fork the chain, no `prev_hash` races.
2. **Each record core-signed:** added `AuditRecord.signature` (Ed25519 over the canonical bytes) → a third-party sink can *verify* the chain but can't forge/reorder/drop without detection.
3. **Canonical serialization pinned in-contract:** specified the deterministic proto3 form the signature/hash cover (ascending field order, each field once, minimal varints, defaults omitted, no unknowns) → cross-impl verification is well-defined. Un-retrofittable once chains exist → pre-freeze.
4. **Per-record response, prefix-only commit:** replaced `AppendResponse.appended:int64` with `repeated RecordAck` (`AppendStatus` COMMITTED/DUPLICATE/REJECTED + `RejectCode` BAD_SIGNATURE/CHAIN_BREAK/STORAGE_ERROR); commit is a contiguous prefix (a REJECTED entry ⇒ all later uncommitted, so no fork on partial failure); `last_committed_id`/`last_committed_hash` watermark lets a reconnecting emitter resume with no gap. `APPEND_STATUS_DUPLICATE` is the idempotent-retry valve. Two prose invariants captured: a dropped/rejected record is itself a meta-audit event; duplicate-on-retry must not double-append.

**Verified:** buf lint 0 / build 0 / generate 38 Go files (RecordAck + 2 enums compile into the existing auditlog package files — no new file count); dup-free; no stale `appended` refs.

**Next:** freeze-blocker #5 — `annotations.proto` + `(rat.capability)` method option + split `Write`/`engine.Execute` per-mode (do together).

**Files:** `contracts/proto/rat/auditlog/v1/auditlog.proto`.

---

## 2026-05-30 — Freeze-blocker #3: state.proto (key grammar + Put tri-state + CAS conformance)

**What:** reviews/06 #3 — three coupled fixes to `state.proto` (the tier-0 state backend the reconciler depends on):

1. **Key/prefix grammar (SEC-2):** `key`/`prefix` were unconstrained strings → naive namespace-prefix concat could be escaped. Now a stated conformance rule: reject empty key / >512B / non-UTF-8 / NUL+control chars / `.`+`..` traversal segments → `INVALID_ARGUMENT`. Makes C3 isolation a real boundary, not comment-deep.
2. **Put outcome tri-state (C-4 / API-1 reconciler axis):** replaced `PutResponse.committed:bool` with a `PutOutcome` enum — `COMMITTED` / `CONFLICT` (lost CAS race, deterministic, didn't write) / `UNKNOWN` (timeout/partition, may-or-may-not have committed). `committed:bool` couldn't express UNKNOWN, which is fatal for lease fencing (a "maybe-committed" renewal can't be relied on).
3. **CAS conformance + DynamoDB (C-4 / ARCH-3):** turned "MUST be linearizable" from prose into a stated conformance obligation (single-key linearizable CAS + ordered Watch, gated by the 0f suite); resolved the contradiction where overview.md advertised DynamoDB (eventually-consistent default) as a cloud state backend → split-brain leader election. DynamoDB now annotated as strongly-consistent-mode-or-solo-only in the topology table; removed it from the proto's plugin-examples list.

**Verified:** buf lint 0 / build 0 / generate 38. No remaining references to the old `committed` field.

**Next:** freeze-blocker #4 — audit `AppendResponse` → per-record `RecordAck` (prefix-only) + canonical-serialization pin + core-assigned id/prev_hash.

**Files:** `contracts/proto/rat/state/v1/state.proto`, `docs/architecture/overview.md` (topology footnote).

---

## 2026-05-30 — Freeze-blocker #2: format capability URI rename

**What:** reviews/06 C-2 (API-7 ⊕ AUTH-1) — `format` was the one axis whose capability URI (`rat://format-capability/v1/*`) didn't match its proto package (`rat.format.v1`), breaking the contract-triple's "URI mirrors the package coordinate" invariant for the most-referenced axis. Renamed `rat://format-capability/v1/*` → `rat://format/v1/*`.

**Changed (live contract + architecture doc):** `format.proto` (capability map + RPC comments), `strategy.proto` (prose), `rat-format-deltalake.plugin.yaml` + `rat-strategy-scd2.plugin.yaml` (the `provides`/`requires` URIs), `INVALID-examples.md`, `schema/README.md`, and `docs/architecture/overview.md` (`kind: format-capability` → `kind: format` + the URI string).

**Deliberately NOT changed:** historical records — `reviews/00,02,06` and `docs/conversations/*` keep `format-capability` (reviews/06 literally documents it as the bug; rewriting history would be dishonest).

**Verified:** buf lint 0 / build 0 / generate 38; both example manifests re-validate against the schema (containerized).

**Next:** freeze-blocker #3 — state.proto (key/prefix grammar + Put tri-state + CAS conformance/DynamoDB).

**Files:** `contracts/proto/rat/format/v1/format.proto`, `contracts/proto/rat/strategy/v1/strategy.proto`, `contracts/examples/{rat-format-deltalake,rat-strategy-scd2}.plugin.yaml`, `contracts/examples/INVALID-examples.md`, `contracts/schema/README.md`, `docs/architecture/overview.md`.

---

## 2026-05-30 — Freeze-blocker #1: context.proto keystone rewrite (3-principal identity)

**What:** Applied the first + highest-leverage freeze-blocker from [reviews/06](../reviews/06-proto-contract-review.md) C-1 (SEC-1 ⊕ AUTH-12, the keystone). Rewrote `contracts/proto/rat/common/v1/context.proto`, replacing the single forgeable, semantically-ambiguous `subject` string with three distinct principals + structural trace/identity separation. Commit `322126c`.

**New `RequestContext` shape:**
- `TraceContext` (traceparent/tracestate/correlation_id) — caller-supplied, propagated verbatim, diagnostic-only.
- `Identity` — all fields CORE-stamped, never trusted from the wire:
  - `caller_plugin` — immediate caller, server-derived from the hop's channel credential, **re-derived every hop, never propagated**. C3 state namespace = `(caller_plugin, tenant)`.
  - `subject` — a `SubjectAssertion` (core signature + `bound_correlation_id` + `expires_unix_ms`), not a bare string. Verification contract: every consuming hop verifies the signature, checks `bound_correlation_id == inbound correlation_id` (anti-replay/confused-deputy), and checks TTL. Propagated.
  - `tenant` — server-stamped, propagated, never caller-writable (C7 structural).
- `deadline_unix_ms` — unchanged hint.

**Downstream coherence (other half of AUTH-12):** `state.proto` C3 namespace now keys on `identity.caller_plugin` (was the contradictory `subject (the calling plugin)`); comment refs → `context.identity.{tenant,subject}` in storage/secret/billing/tenancy/identity. Composes with ADR-005 (the core-mediated gateway stamps `caller_plugin` per hop).

**Verified (containerized):** buf lint 0, build 0, generate 38 Go files; dup-scan clean (python-verified across all 6 touched files).

**Next:** freeze-blocker #2 — rename `format` capability URIs `rat://format-capability/v1/*` → `rat://format/v1/*`.

**Files:** `contracts/proto/rat/common/v1/context.proto` (rewrite), `state/v1/state.proto`, `storage/v1/storage.proto`, `secret/v1/secret.proto`, `billing/v1/billing.proto`, `tenancy/v1/tenancy.proto`.

---

## 2026-05-30 — ADR-005: capability invocation model — core-mediated

**What:** Resolved the one open design decision from [reviews/06](../reviews/06-proto-contract-review.md) C-6 (AUTH-2 ⊕ ARCH-2) — how a plugin actually *calls* a capability it `requires`, which the protos never expressed on the wire. Wrote [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md).

**Decision:** **core-mediated** (systems-architect's position) over direct-dial (plugin-author's). Control-plane capability calls go through a new core capability-invoke service (`core/v1/invoke.proto`); the core resolves capability→provider via the registry, enforces C2/C3/C5/C7/C8 + stamps C1 trace, and proxies. The calling plugin still orchestrates the *sequence* (core is a switchboard, not an orchestrator). Bulk Arrow bytes still bypass the core (the data-plane exception is preserved).

**Why not direct-dial:** scoped-token direct-dial distributes enforcement to every callee → re-introduces the honor-system the security review condemned (the first plugin that validates loosely or skips audit silently breaks a platform invariant, with nothing central to catch it). Latency — the only thing direct-dial wins — is the dimension a control plane cares least about, and bytes already bypass the core. A direct-dial fast-path stays available as a future superseding ADR *if* profiling proves a need.

**Freeze impact:** the freeze artifact is the new `core/v1/invoke.proto` (now freeze-blocker item #6 in current.md); `RequestContext` does NOT gain provider-routing fields. reviews/06 C-6 updated open → resolved.

**Files:** `docs/architecture/adrs/005-capability-invocation-model.md` (new), `docs/architecture/adrs/README.md`, `reviews/06-proto-contract-review.md`, `roadmap/*`.

---

## 2026-05-30 — Proto contract review (adversarial agent-team) → reviews/06

**What:** Ran a 4-expert agent-team peer review of the 20 sub-phase-0b proto files +
`schema/plugin.v1.json`, pre-freeze (per ADR-003). Lenses: api-designer (proto/gRPC),
plugin-author (implementability), security-eng (wire-vs-comment enforcement),
systems-architect (composition/failure). Reviewers worked cold (not given the prior
architecture reviews' answers), cross-challenged each other, and classified every finding on
**severity × freeze-gate**. Output: [`reviews/06-proto-contract-review.md`](../reviews/06-proto-contract-review.md).

**Headline:** the protos are clean as individual services, but the cross-plugin properties that
are the RAT thesis (call-by-capability invocation, per-plugin/tenant isolation, tamper-evident
audit) are asserted in comments but **not enforced by the fields** — comment-deep. **Contract
is NOT ready to freeze** — **15 freeze-blockers + 1 open design decision (AUTH-2 invocation
model)**; ~28 further findings are GA-deferrable.

**15 freeze-blockers (cannot fix post-freeze)** — top: the identity keystone (forgeable +
contradictorily-defined `subject` → C3 unbuildable); format capability URI naming breaks the
triple; state key grammar + `state.Put` outcome tri-state + CAS-linearizability-conformance (+
DynamoDB eventual-consistency → split-brain leader election, a NEW critical); audit
AppendResponse shape; async event-bus envelope (no `event.proto`); `MergeBranch`
idempotency/expected-snapshot; `secret.Resolve.found` semantics; Arrow protocol+role; split
`Write` per-mode; `rat.capability` annotation; `Ingest` streaming shape; timestamp type;
`slots.target` wrap.

**Method notes:** keystone hit independently by 3/4 lenses; the sharpest find (confused-deputy
assertion-replay → per-hop `correlation_id` enforcement) only emerged from the team's converged
fix; one finding conceded down (API-8), one reviewer self-discarded 4 unverified findings.

**⚠️ Correction (committed `0201892`, after first version `b9be88b`):** systems-architect's
ballot was lost in transit (tool acked, message never landed). The first report version was
written without it and **wrongly recorded AUTH-2 as direct-dial-by-consensus** plus three items
as GA. When the ballot arrived, the report was corrected — all changes toward *more* severe:
AUTH-2 is now a documented **open disagreement** (systems-architect: core-mediated /
plugin-author: direct-dial; needs an ADR), and `state.Put` tri-state, the async event envelope,
and `MergeBranch` request-shape were upgraded to freeze-blockers (12 → 15). Provenance noted in
the report appendix.

**Next:** resolve the AUTH-2 model (ADR) + apply the 15 freeze-blocker fixes (start with the
`context.proto` keystone — everything keys off it), re-running buf each step.

**Files:** `reviews/06-proto-contract-review.md` (commits `b9be88b` + correction `0201892`), `roadmap/*`.

---

## 2026-05-30 — Agent-teams flag pinned into project settings

Declared `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` in the project-committed `.claude/settings.json` `env` block so the repo self-documents its reliance on the experimental agent-teams feature (previously set only in user-global `~/.claude/settings.json`). Flag is experimental/unofficial — may change on product update. Doc: `https://code.claude.com/docs/en/settings.md` (`env` block pattern).

**Files:** `.claude/settings.json`, `roadmap/done.md`.

---

## 2026-05-30 — PostToolUse auto-format hook: evaluated and rejected

**What:** Evaluated the deferred `PostToolUse` auto-format hook from `backlog.md` (the
trigger condition — 20 `.proto` files committed — was met). Decision: **do not land the
hook; adopt manual-batch formatting instead.**

**Decision rationale:** Containerized formatter in a synchronous `PostToolUse` hook is a
latency antipattern for this project. Each `Edit|Write` call would spin up a Podman
container (500ms–2s startup) even though `gofmt` itself runs in <50ms and `buf format`
in <200ms. The container overhead is 10–40x the tool cost, paid after every single file
edit, blocking Claude from proceeding to the next tool call. At this project's
development pace (heavy multi-file sessions), that latency accumulates into real friction.

**Alternative landed:** none needed in `.claude/`. The `permissions.allow` array already
permits `buf format *` and `go fmt *` via Bash tool. The model is expected to run a
batch format (`buf format -w` via the established Podman invocation) before committing,
consistent with how every other containerized tool is used in this project. A
`scripts/fmt.sh` helper can be added when Go code is actively being written — a Bash
script, not a hook.

**Doc citation:** `https://code.claude.com/docs/en/hooks-guide.md` — PostToolUse +
`Edit|Write` matcher pattern confirmed; latency tradeoff is Claude Code engineer judgment,
not a doc constraint.

**Files:** `roadmap/backlog.md` (hook row cut, decision noted), `roadmap/done.md` (this
entry).

---

## 2026-05-30 — Phase 0 sub-phase 0b complete: long tail (experience + billing)

**What:** Drafted the final four axes — all v1 axes now have a proto.

**New protos (`contracts/proto/rat/`):**
- `ui/v1` — Describe/RenderSlot. Deliberately thin (a UI mostly consumes the API
  gateway); the contract is about exposing surfaces + hosting `contributes.slots`
  portal contributions (overview.md contract triple).
- `notifications/v1` — Send (severity + target + attributes; secrets-redaction
  obligation noted, I13).
- `marketplace/v1` — Search/Get. **reviews/02 N2**: provided/required capabilities,
  conformance results, and signature are MANDATORY listing fields so any
  marketplace can answer the "works on MY deployment?" capability-fit question —
  the one hard job of a marketplace on a pluggable-everything platform.
- `billing/v1` — Record (per-tenant metering by construction via context.tenant, C7).

**Verified (containerized):** `buf lint` 0 findings (exit 0), `buf build` 0
errors, `buf generate` **38 Go files**, dup-scan clean.

**Milestone:** **sub-phase 0b axis protos COMPLETE — 20 proto files total** (18
axis services + 2 common). Every v1 plugin axis from ADR-001 now has a wire
contract. Critical concerns with a wire home: C1, C2, C3, C5, C7 + I8/I9/I13.

**What 0b still needs before it's fully done:** per-kind manifest schemas derived
from these protos (the 0a→0b handoff in `schema/README.md`). Then 0c: the
event-bus envelope (C1 trace in async events, not just RPCs).

**Files:** `contracts/proto/rat/{ui,notifications,marketplace,billing}/v1/*.proto`,
`contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b cont'd (batch 3): 5 control-plane axes

**What:** Added the remaining bootstrap/ops control-plane axes. Data plane was
already complete; this finishes most of the control plane.

**New protos (`contracts/proto/rat/`):**
- `deploymentruntime/v1` — Launch/Terminate/Healthcheck; **tier-0** (where plugins
  run); **I9 minimum isolation profile** is wire-level contract (non-root,
  cap_drop ALL, no-new-privileges, read-only FS, blocked metadata egress) — the
  trust boundary the "install many 3rd-party plugins" bet leans on.
- `scheduler/v1` — Schedule/Cancel/WatchDue (a clock, not an orchestrator).
- `secret/v1` — Resolve; **I13 secret contract**: refs in manifests/events/logs,
  values resolved on demand with TTL, never persisted, redaction a core duty.
- `observability/v1` — Ingest (client-stream). **Export sink only** — the core's
  own `/metrics` + OTel are NATIVE and do not depend on this plugin (reviews/03
  I1); "observability: none" still leaves the core self-observable.
- `auditlog/v1` — Append; **I8 mandatory audit**: append-only, hash-chained
  (prev_hash) tamper-evident records. Audit ALWAYS emits (core-local fallback);
  this axis decides WHERE the trail goes, never WHETHER it exists.

**Verified (containerized):** `buf lint` 0 findings (exit 0), `buf build` 0
errors, `buf generate` 30 Go files, dup-scan clean. No streaming-name issues this
batch (watched the *Response rule proactively).

**Phase status:** 0b now has **14 of ~20 axis protos** — data plane complete (6),
control plane nearly complete (8: state, identity, tenancy, deployment-runtime,
scheduler, secret, observability, audit-log). Remaining: experience axes (ui,
notifications, marketplace) + billing. Critical concerns now wired: C1, C2, C3,
C5, C7, plus I8/I9/I13 isolation/audit/secret contracts.

**Files:** `contracts/proto/rat/{deploymentruntime,scheduler,secret,observability,auditlog}/v1/*.proto`,
`contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b cont'd: 6 more axis protos + lint fix

**What:** Added six more axis service contracts (rest of the data plane + the
three Critical-carrying control-plane axes) and **corrected a lint failure that
slipped into the prior commit** (`e79910c`).

**New protos (`contracts/proto/rat/`):**
- `engine/v1/engine.proto` — Execute/Query/Preview ⇒ `rat://engine/v1/{execute,query,preview}`.
- `catalog/v1/catalog.proto` — GetTable/CreateBranch/MergeBranch (branch-isolated runs).
- `storage/v1/storage.proto` — VendCredentials; **C7 enforcement point** (creds
  scoped to `context.tenant` + prefix, short-TTL — the mis-scope that reviews/01
  Finding 3 warned defeats tenancy).
- `state/v1/state.proto` — Get/Put/List/Watch; **tier-0** (bootstrap-critical),
  **C3** (per-plugin + per-tenant namespacing, deny-by-default cross-plugin), CAS
  `Put` backs the leader-election lease (ADR-002 D5).
- `identity/v1/identity.proto` — Authenticate/Authorize; **C2** (per-plugin token,
  constant-time compare — inherits v2 ADR-020; default is NOT anonymous-root).
- `tenancy/v1/tenancy.proto` — Decide; **C7** as *structural* (core enforces
  isolation; plugin only computes policy — explicitly rejects the "isolation is
  4 plugins agreeing" reading from reviews/01).

**Lint correction:** renamed streaming response types to satisfy buf STANDARD —
`runtime.ExecuteEvent` → `ExecuteResponse` (this is the finding that was wrongly
reported as passing in `e79910c`), and pre-empted the same on the new
`state.WatchEvent` → `WatchResponse`.

**Verified (containerized):** `buf lint` **0 findings** (genuinely, exit 0 this
time), `buf build` **0 errors**, `buf generate` **20 Go files**, dup-scan clean.

**Phase status:** 0b now has **9 of ~20 axis protos** (format, runtime, strategy,
engine, catalog, storage, state, identity, tenancy) + 2 common protos. Critical
concerns C1/C2/C3/C5/C7 now have a wire home.

**Files:** `contracts/proto/rat/{engine,catalog,storage,state,identity,tenancy}/v1/*.proto`,
`contracts/proto/rat/runtime/v1/runtime.proto` (fix),
`contracts/proto/rat/state/v1/state.proto`, `contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b started: first axis protos + buf toolchain

**What:** Drafted the first three axis service contracts + the cross-cutting
request context, and stood up the `buf` proto toolchain (containerized).

**Protos (`contracts/proto/rat/`):**
- `common/v1/context.proto` — **C1 bake-in**: `RequestContext` (traceparent +
  tracestate + correlation_id mandatory; subject for C2/C5; tenant for C7;
  deadline hint). Travels as field 1 of every RPC. Pulled forward from 0c
  because every axis proto imports it.
- `common/v1/data.proto` — shared data-plane handoff types (`TableRef`,
  `ArrowStream`, `WriteResult`). Encodes the "control plane carries refs, bulk
  bytes go out-of-band as Arrow" invariant from overview.md.
- `format/v1/format.proto` — `Resolve`/`Write`/`Maintain` ⇒
  `rat://format-capability/v1/{scan,merge,append,maintain}`.
- `runtime/v1/runtime.proto` — `Execute` (server-streaming) ⇒
  `rat://runtime/v1/execute`.
- `strategy/v1/strategy.proto` — `Apply` ⇒ `rat://strategy/v1/apply`.

These three axes were chosen first because the example manifests (0a) already
reference their capability URIs — so the manifest↔wire loop now closes.

**Toolchain:** `contracts/buf.yaml` (lint STANDARD, breaking FILE),
`contracts/buf.gen.yaml` (Go SDK wired; other SDKs in 0d/0e),
`contracts/.gitignore` (generated `gen/` excluded as build artifacts).

**Verified (containerized, per container-only rule):** `buf build` and
`buf generate` passed (`docker.io/bufbuild/buf:1.47.2`, run with `--userns=keep-id`
+ writable HOME). `buf generate` produced 8 Go files (git-ignored artifacts).

**⚠️ Correction (recorded in the next entry's commit):** this commit was
described at the time as "buf lint 0 findings" — that was WRONG. `runtime.proto`
still returned `stream ExecuteEvent`, which buf STANDARD flags (response type must
be `*Response`-named). Lint was actually FAILING (1 finding) at the time of
`e79910c`; build/generate passed (lint findings don't block them) and that was
misread as lint passing. Fixed in the following commit (`ExecuteEvent` →
`ExecuteResponse`).

**Note:** several Write calls glitched mid-session (duplicated lines / wrong
paths); caught via dup-scan + buf, all files rewritten clean and re-verified.

**Files:** `contracts/proto/**` (5 protos), `contracts/buf.yaml`,
`contracts/buf.gen.yaml`, `contracts/.gitignore`, `contracts/README.md`,
`roadmap/*`.

---

## 2026-05-30 — Phase 0 entered: sub-phase 0a manifest schema drafted

**What:** Entered Phase 0 (Lock the contracts) and produced the first contract artifact — the manifest envelope schema. Created the `contracts/` workspace.

**Artifacts (all in `contracts/`):**
- `schema/plugin.v1.json` — manifest **envelope** schema, JSON Schema 2020-12 (per ADR-002 D3). Validates the structure common to every axis: `api_version`/`kind`/`metadata`/`provides`/`requires`/`suggests`/`contributes`/`metadata_schema`, plus the capability-URI grammar `rat://<axis>/v<major>/<capability>`.
- `schema/README.md` — design notes; records the **per-kind schema decision** (envelope-first now, per-kind schemas layered in 0b as each axis proto lands) and the documented gaps (semantic capability validity needs `rat plugin validate`, 0f).
- `examples/rat-strategy-scd2.plugin.yaml` — canonical valid manifest (from overview.md, extended with Critical fields).
- `examples/rat-format-deltalake.plugin.yaml` — second valid manifest (signed/team+ trust block).
- `examples/INVALID-examples.md` — negative test vectors (future 0f corpus).
- `README.md` — contracts workspace entry point + status table.

**Critical concerns baked in (synthesis):** C4 resource asks/limits (`resources`, **mandatory**), C5 capability enforcement (`provides` is the enforced declaration, minItems 1), C8 supply-chain trust (`trust` block, optional@solo / required@team+).

**Verified:** ran a containerized validator (Podman, `python:3.12-slim` + `jsonschema`) — schema is meta-valid, both examples pass, all 4 negative vectors correctly rejected. ALL GREEN.

**Phase status:** Phase 0 moved not-started → in-flight; sub-phase 0a substantially drafted (schema + examples done; per-kind schemas deferred to 0b).

**Note on the commitment gate:** `phases.md` flags a 12–18mo runway + GTM commitment as a pre-Phase-0 gate. Tom chose to proceed (sandbox/exploratory). Gate noted, not formally cleared.

**Files:** `contracts/` (new tree, 6 files), `roadmap/current.md`, `roadmap/phases.md`, `roadmap/done.md`.

---

## 2026-05-30 — Core language locked: Go (ADR-004)

Wrote [ADR-004](../docs/architecture/adrs/004-core-language-go.md) to **ratify and lock** the Go decision that [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D1 had already made. The decision itself wasn't new — D1 said "Core language: Go" all along — but the project *prose* (CLAUDE.md "Rust or Go") and the just-landed allowlist (both toolchains) were still treating it as open. ADR-004 closes that gap before code starts.

### Why an ADR if D1 already said Go
The gap between "decided in the ADR" and "treated as open in prose/tooling" is exactly the kind of drift the ADR-first discipline exists to catch. ADR-004 also records something D1 left implicit: Go is the **committed default**, with the door to re-language kept open as a Phase-0 validation checkpoint per D1's own re-language meta-principle (not via a prototype spike — ADR-002 specifies none).

### Changes
- **`docs/architecture/adrs/004-core-language-go.md`** — new ADR (Accepted).
- **`docs/architecture/adrs/002-founding-tech-stack.md`** — D1 cell annotated "Locked & ratified by ADR-004" (cross-link only; decision unchanged).
- **`docs/architecture/adrs/README.md`** — index row for ADR-004.
- **`.claude/settings.json`** — **trimmed to the Go toolchain**: dropped the 7 Cargo/Rust allow-rules (`cargo build/test/check/clippy/run/fmt/doc`) added in the prior entry. The "both toolchains until locked" rationale no longer holds. 26 → 19 rules.
- **`roadmap/current.md`** — updated.

### Rationale source
Grounded in ADR-002 D1: ecosystem alignment (etcd/NATS/K8s/Temporal/Crossplane all Go), mature gRPC tooling (`grpc-go`), faster MVP, larger cloud-native contributor pool, plus plugin-SDK ergonomics (no borrow-checker barrier for plugin authors — the ADR-001 bet). Contracts remain language-neutral, so third-party plugins stay any-language.

---

## 2026-05-30 — `.claude/settings.json` coding-phase allowlist

> **Superseded in part (same day):** the Cargo/Rust rules added here were removed once the language locked to Go — see the ADR-004 entry above.


Pre-coding permissions audit (via `claude-engineer` agent). Expanded the `permissions.allow` array to cover both candidate toolchains — the Go vs Rust language decision from ADR-002 is still open ("Both / undecided"), so both are pre-allowed so Phase 0 can start in either direction without permission-prompt friction.

### Changes

- **`.claude/settings.json`** — added `$schema` key (IDE autocomplete); populated `permissions.allow` with 26 command rules:
  - **Go:** `go build/test/vet/mod/generate/run/fmt`
  - **Rust/Cargo:** `cargo build/test/check/clippy/run/fmt/doc`
  - **Protobuf (buf):** `buf generate/lint/breaking/format`
  - **Podman:** `podman build/run/compose` (per Tom's container-only rule in root CLAUDE.md)
  - **Git (safe):** `git commit/add/diff/log/status`

### Deliberately omitted (keep prompting)

`git push`, `git reset`, `git rebase`, `podman rm`, `podman rmi` — destructive or remote-affecting; prompt behavior preserved.

### Two deferred items queued in backlog

(See `backlog.md` — "Claude Code config: deferred until first code file" section.)

### Rationale

`go test ./...`, `cargo clippy`, `buf generate` etc. are not in Claude Code's built-in read-only set and would prompt on every invocation. Listing them in `permissions.allow` removes the friction without relaxing any destructive-command guardrails. Cite: `https://code.claude.com/docs/en/permissions.md`.

---

## 2026-05-30 — `.claude/` configuration landed

Project-specific Claude Code setup created. Same minimalist discipline as the architecture: built-ins first, custom additions justified, docs cited.

### Files added
- `.claude/README.md` — entry point + structure
- `.claude/settings.json` — `permissions.allow` empty (honest: every common command in transcripts was either auto-allowed or mutating; nothing meaningful to allowlist)
- `.claude/rules/plugin-architecture.md` — founding constitutional invariant from ADR-001 (always-load, no `paths:` frontmatter). Codifies the 6-thing core + 16+ axes; the "tier 0" callout from synthesis Finding 6; cross-cutting concerns owned by the core.
- `.claude/rules/claude-environment.md` — meta-discipline for `.claude/` itself. Built-in first, cite official docs, minimal surface, quarterly audit. Names the built-in agents + skills explicitly so future sessions can't drift.
- `.claude/agents/claude-engineer.md` — specialized custom agent (model: sonnet; tools: Read/Edit/Write/Bash/WebFetch/Grep/Glob) for Claude Code config work. Reads `https://code.claude.com/docs/` before recommending changes; distinct from built-in `claude-code-guide` (research-only) — `claude-engineer` can make changes.

### Files updated
- Root `CLAUDE.md` — new principle #10 "Maintain the Claude Code environment as architecture"; directory map extended with `.claude/` tree
- `.gitignore` — excludes `.claude/settings.local.json` (per-user overrides not committed)

### What was NOT added
- ❌ Hooks — the synthesis warned against premature automation; CLAUDE.md rules are enough for now
- ❌ Custom skills — built-in skills (deep-research, code-review, etc.) cover the needs
- ❌ MCP servers — nothing project-specific yet
- ❌ Other custom agents — built-ins (claude, Explore, Plan, general-purpose, claude-code-guide) cover everything except Claude-Code-config-itself, which is what claude-engineer is for

### Rationale
Tom asked for the setup as part of "before anything of this could you tell me the claude setup for this new sandbox." The audit surfaced that the project had only CLAUDE.md rules — no agents, hooks, settings. Rather than adding a wide surface, we mirrored the architecture's discipline: a minimal `.claude/` core (rules + one custom agent) that future additions must justify against built-ins.

The `claude-engineer` agent is the operational equivalent of ADR-003's "two reference implementations before contract freeze" rule — it forces every Claude Code config change to go through a docs-citing, built-in-first filter, instead of accumulating ad-hoc custom additions over time.

---

## 2026-05-30 — Phase −1 complete

The full architectural-design + adversarial-review phase, landed in one day with Claude.

### Roadmap structure + ADR-003 + post-synthesis doc updates
- Created `roadmap/` directory with CLAUDE.md (maintenance rules), README, phases.md, current.md, done.md, backlog.md
- Wrote **ADR-003** — "Two independent reference implementations before any contract freezes" (the C9 forcing function from synthesis)
- Updated **ADR-001 Migration section** — Phase 0 timeline shifted to 4-6 months; Critical cross-cutting concerns baked in; all 5 phases expanded with post-synthesis scope
- Updated **vision.md** — added "Anti-goals" section: (1) no new plugin axis in year 1 until 100 daily users on core, (2) effort ratio must invert from 95/5 architecture-to-GTM toward 60/40
- Updated **ADRs index** with ADR-003
- Updated **root CLAUDE.md** with roadmap reference + maintenance rule

### 5-perspective adversarial team review
- Spawned `rat-v3-review` team with 5 teammates (adversarial-architect, plugin-ecosystem-builder, operations-sre, security-reviewer, product-gtm) running in parallel via the experimental agent-teams feature
- Each produced a deep critique against the founding ADRs
- Wrote **synthesis** (`reviews/00-synthesis.md`) — 5 cross-cutting themes converged across all 5 reviewers, 10 Critical findings, 17 Important findings, 26 prospective ADRs, 2 roadmap shifts
- Headline finding: *the architecture is sound; the cross-cutting concerns that genuinely have to span plugins (trust, conformance, observability, distribution) have no owner; the proposed mitigations for the documented failure modes don't escape them*
- 5 review files: `01-adversarial-architect.md`, `02-plugin-ecosystem-builder.md`, `03-operations-sre.md`, `04-security-reviewer.md`, `05-product-gtm.md`
- Team gracefully shut down post-synthesis

### Founding ADRs landed
- **ADR-001** — "Everything is a plugin" (the founding principle: 6-thing core + 16+ plugin axes)
- **ADR-002** — "Founding tech stack + strategy decisions" (Go + NATS + JSON Schema + Apache 2.0 + K8s patterns + 7 other decisions across 10 questions; meta-principle: AI-rewrite escape hatch lowers tech-choice stakes)
- Both ADRs include rejected-alternatives tables, structured Consequences sections, and links to the conversations that produced them
- Conversation distillations committed: `docs/conversations/2026-05-30-the-vision-conversation.md` + `docs/conversations/2026-05-30-tech-stack-decisions.md`

### Initial scaffold
- Created `~/sandbox/rat/` project directory + git init
- Project CLAUDE.md with working principles (contracts before code, six-thing-core discipline, ADR-first, honest tradeoffs, capture-ideas-where-they're-born, save load-bearing conversations)
- README + .gitignore
- Vision document (the thesis) — minimal core + everything pluggable
- Architecture overview document — the integrated picture
- ADRs README with template + numbering/discipline rules
- Sub-CLAUDE.md files for `docs/architecture/adrs/`, `ideas/`, `docs/conversations/`
- ideas/inbox.md with 6 seed entries (later expanded to 11)
- research/prior-art/README.md (K8s, OSGi, VSCode, NATS, Temporal, etc.)
- research/competitors.md (Snowflake, Databricks, dbt, Airflow, Iceberg, MotherDuck — the landscape)
- 14 files, ~1276 lines, 1 commit (`7d57eab`)

### Git commits this session

```
536c9c1 docs(review): synthesis + remaining 2 reviews — 5-perspective adversarial audit
4d2af82 docs(review): security threat model (STRIDE) for RAT v3
778e79d docs(review): 3rd-party plugin author ecosystem review
dbdcde5 docs(review): adversarial architect review
33c1ec0 docs(adr): lock founding tech stack — Go, NATS, Apache 2.0, K8s patterns (ADR-002)
7d57eab chore: initial scaffold for RAT v3
```

(This entry's own commit lands separately — see git log for `docs(roadmap+adr): ...`.)

### What's true at end of day 2026-05-30

- Project lives at `~/sandbox/rat/`, git-initialized, ~3000 lines of architecture + thinking
- 3 Accepted ADRs (001, 002, 003)
- 2 conversation distillations
- 5 adversarial reviews + 1 synthesis
- 11 ideas captured in `ideas/inbox.md`
- Research scaffold present (prior art + competitors)
- Roadmap structure operational; this file is the proof
- **No code yet.** Awaiting Tom's commitment decision before Phase 0 starts.
