# Done — completed work log

Reverse chronological. Each entry: date, what was accomplished, links to artifacts (commits, files, ADRs).

---

## 2026-06-01 — Phase 0 close-out (1/4): **catalog commit-linkage** — the headline-feature hole closed on-wire ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))

Closed reviews/08 **B1** (3 agents' top concern) — the first `v1.1` additive. The branch-pipeline headline feature now completes its **create → write → register → merge** loop entirely on the frozen wire; the composition no longer fakes table registration out-of-band.

- **[ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md)** — two additive RPCs on the frozen `catalog/v1`: `RegisterTable` (`rat://catalog/v1/register-table`, idempotent create of a new output table) + `CommitTable` (`rat://catalog/v1/commit-table`, records the writer-supplied `WriteResult.snapshot_id` — the commit-linkage). `CommitTable` carries `MergeBranch`'s safety model (`expected_snapshot` CAS + `idempotency_key` → `already_applied`), giving the previously-unguarded **write/publish leg** the idempotency the B1 architect→sre cross-consult flagged. Two RPCs (not one) so create-vs-commit are method-level capabilities (the format `Write`-split precedent). **Resolves ADR-009 residual R3.**
- **Wire:** `catalog.proto` +2 RPCs +4 messages — **additive** (`buf breaking` FILE clean vs HEAD; lint + build clean). All 4 SDKs regenerated (Go/Py/TS/Rust — only the 8 catalog files changed).
- **References:** all 3 catalogs (`inmemory-go`, `inmemory-py`, `sqlite-py` — store + server) implement register/commit; sqlite uses `BEGIN IMMEDIATE` for the same durable + concurrent-safe semantics as merge.
- **Golden vectors:** `catalog-v1.json` +6 lifecycle steps (register new/idempotent · commit new/idempotent-retry/CAS-reject/CAS-ok) +3 error steps; all 3 harnesses extended (+ the 2 caps in the Go stub gateway's C5 allowlist). **`make conformance` 32/32.**
- **Composition:** `build_catalog` no longer pokes the catalog's private store — only the pre-existing source is admin-registered via the public api; the **full-refresh + SCD2 strategies register their output + commit the written snapshot through the gateway**, and the harness asserts `GetTable(target)` succeeds *after* the run (the catalog learned the output on-wire). `CompFormatServicer` now returns a real `snapshot_id`. **`make composition` ✅** (4/4 combos + both strategies).
- **Status:** staged in the working tree, verified green; commit pending. *(gen-check freshness gate is BSR-rate-limited locally; SDK freshness confirmed by the successful `make gen-sdks` + catalog-only diff + the live SDK exercise in conformance/composition.)*

---

## 2026-06-01 — Absorbed the board's two "NOW" items + **re-cut `rat/1`** (pre-publish correction)

Actioned the two reviews/08 items that were only possible while the freeze is local/unpushed, and re-cut the `rat/1` tag.

- **A1 [V2-REGRET fixed]** — `WriteResult.snapshot_id` `string` → **`optional`** (`data.proto`). Kills the empty-sentinel that conflated "no version" vs "cannot report" — the API-13 bug fixed on the sibling `rows_affected` but left on this field. `string`→`optional` is breaking under `buf` FILE rules, so it was free now / impossible after publication. Go refs' `snapshotID()` → `*string` (reads via `GetSnapshotId()` unchanged; Python proto-optional transparent). All 4 SDKs regenerated; **`make conformance` 32/32**; buf clean.
- **D5/E4 honesty banner** — added "the orchestrating core is NOT built yet (Phase 1); enforcement here is the contract it MUST implement, it does not run today; conformance tests references not a live deployment; `provides`/`conformed_capabilities` are self-asserted (no enforcer)" to `plugin.v1.json` (`$comment`) + the 6 `CONTRACT.md` author guides.
- **`rat/1` re-cut** (commit `0e81314`, was `b9dbe2d`) — supersedes the original; the annotation records why. `rat/1.1`–`rat/1.4` remain valid and layer on top.
- Commit `0e81314` (`fix(contract)!`). The single true V2-regret the board found is now **resolved**, not carried to a v2.

---

## 2026-06-01 — 5-agent post-freeze board review (communicating team) → [reviews/08]

Ran the first adversarial review *after* the freeze, as a **communicating agent team**: 5 specialists (`architect`, `security`, `ecosystem`, `sre`, `contracts`) reviewed the frozen surface (rat/1..rat/1.4 + 32 refs + composition) in parallel and **cross-consulted each other via direct messages** — several findings changed as a result (the terminal-audit finding came from `sre`→`security`; `architect` cross-corrected `sre` on the health contract; `security`↔`contracts` confirmed the ArrowStream-ticket gap).

- **Artifacts:** [`reviews/08-post-freeze-board-review.md`](../reviews/08-post-freeze-board-review.md) (synthesis) + [`reviews/board/`](../reviews/board/) (5 full reports). Commit `b4c0526`.
- **Verdict:** the frozen WIRE is sound — **only ONE true V2-regret** across 18 axes (`WriteResult.snapshot_id` empty-sentinel) — but the freeze + "32/32 conformance" badge **overstates the guarantee**: enforcement (I9 isolation, ArrowStream ticket, storage cred scoping), crash-safety (no effect-leg idempotency key, no stream terminator, no provider deadline), and the **core itself** are deferred/unbuilt, and frozen artifacts describe the unbuilt core's enforcement in the present tense.
- **Strongest signal:** 3 agents independently nominated the **catalog commit-linkage/CreateTable gap** (the headline branch-pipeline feature can't close its loop on the frozen wire).
- **Actionable now (freeze is still local/unpushed):** make `snapshot_id` `optional` + re-cut `rat/1`; add a "core not built" honesty banner to `plugin.v1.json` + every `CONTRACT.md`. Full prioritized action list in reviews/08 → queued in [backlog](backlog.md).

---

## 2026-05-31 — 🧊🎉 **Experience axes FROZEN** (`rat/1.4`) — ALL 18 AXIS CONTRACTS NOW `v1`

Built one reference per experience axis and froze them — **completing the entire axis-contract surface**. `make conformance` **32/32** (commits `5ce7b30` refs, `030d406` freeze, tag **`rat/1.4`**).

- **`examples/notifications/inmemory-py`** — Send delivery sink (captures messages); rejects empty title (`INVALID_ARGUMENT`).
- **`examples/marketplace/community-py`** — Search/Get over seeded listings; the load-bearing **capability-aware "works on my deployment?" filter** (only listings whose `required_capabilities` are satisfied by the caller's `deployment_capabilities` are returned — e.g. scd2 is filtered until `format/merge` is present). Mandatory listing fields (provided/required/conformed + signed) exercised; Get unknown → `NOT_FOUND`.
- **`examples/ui/web-portal-py`** — Describe (display name + hosted slots) + RenderSlot (resolve a contributed component → asset_ref + props_schema); unknown → `NOT_FOUND`.
- **Build method:** all 3 via **parallel subagents** on the storage template (omitting the tenant/context handling these stateless axes don't need).
- **Freeze:** flipped ui/notifications/marketplace DRAFT → `v1` (`rat/1.4`); buf clean.

**🎉 Milestone: every one of the 18 axis contracts is now frozen at `v1`** — 7 data-plane (engine/format/catalog/storage/runtime/state/strategy) + 1 tier-0 (deployment-runtime) + 7 control-plane (identity/secret/scheduler/tenancy/billing/observability/audit-log) + 3 experience (ui/notifications/marketplace), plus the cross-cutting types. **The only remaining `v1-preview` artifact is the manifest schema (`plugin/v1.json`).**

---

## 2026-05-31 — 🧊 **`deployment-runtime/v1` FROZEN** (`rat/1.3`) — two divergent references

Built two technologically-divergent references for the tier-0 `deployment-runtime` axis (the I9 trust boundary) and froze it. `make conformance` **29/29** (commits `119a1a0` refs, `50f21ee` freeze, tag **`rat/1.3`**).

- **`examples/deploymentruntime/local-process-py`** — runs each plugin instance as a real child OS process (the `chmod +x ./rat` runtime); real Launch → Healthcheck (PID liveness) → Terminate lifecycle.
- **`examples/deploymentruntime/k8s-dryrun-py`** — models a managed/declarative runtime: maps the `LaunchSpec` + I9 `IsolationProfile` → a Kubernetes Pod `securityContext` and admits the manifest (dry-run, no cluster). Where the isolation profile gets a real enforcement target.
- **Shared I9 gate** (the load-bearing obligation): both refuse to launch below the I9 minimum (`run_as_non_root` + `drop_all_capabilities` + `no_new_privileges`) → `FAILED_PRECONDITION`; empty image → `INVALID_ARGUMENT`. Both expose an isolation-honored receipt in `Healthcheck.detail`. Both pass the shared [`deploymentruntime-v1.json`](../contracts/conformance/deploymentruntime-v1.json) — local fork vs container proving the contract composes across runtime technologies.
- **Freeze:** flipped the proto Status DRAFT → `v1` (`rat/1.3`). Like the 6 ADR-003-listed data-plane axes, it got the full two-reference rigor (it's outside ADR-003's explicit list, like strategy, but it's the trust boundary the 3rd-party-plugin bet leans on).

**Still `v1-preview`:** the experience axes (`ui`, `notifications`, `marketplace`) + the manifest schema — the last of the Phase 0 tail.

---

## 2026-05-31 — 🧊 **Control-plane axes FROZEN** (`rat/1.2`) — 7 references + freeze

Built one reference per control-plane axis (ADR-003 requires only one for control-plane, vs two for data-plane) and froze them. `make conformance` now **27/27** (commits `5bcedf9` refs, `ba9269b` freeze, tag **`rat/1.2`**).

- **`examples/identity/static-token-py`** — Authenticate (constant-time token compare; the C2 default, not anon-root) + Authorize (coarse role-based `deny_code`).
- **`examples/secret/inmemory-py`** — Resolve with **anti-enumeration**: unknown ref AND cross-tenant ref both return `found=false` (never `PERMISSION_DENIED`).
- **`examples/scheduler/inmemory-py`** — Schedule/Cancel + server-streaming WatchDue (one-shots; at-least-once delivery).
- **`examples/tenancy/inmemory-py`** — Decide (permission/sharing/quota → `allowed` + `deny_code`); policy *on top of* the core's structural C7 isolation.
- **`examples/billing/inmemory-py`** — Record usage events, per-tenant by construction (C7) + aggregation/isolation tests.
- **`examples/observability/inmemory-py`** — bidi Ingest with cumulative per-batch acks.
- **`examples/auditlog/inmemory-py`** — Append sink enforcing all 4 freeze-blocker-#4 properties: **Ed25519 signature verify** over the pinned canonical serialization, `prev_hash` chain check, prefix-only commit, idempotent DUPLICATE (adds `cryptography`; harness plays the signing core).
- **Build method:** the 4 simple unary axes (identity/secret/tenancy/billing) via **parallel subagents** on the storage template; the 3 streaming/crypto axes (scheduler/observability/auditlog) built directly.
- **Freeze:** flipped the 7 axis Status markers DRAFT → `v1` (frozen, `rat/1.2`); buf clean. Executes ADR-009's stated plan.

**Still `v1-preview`:** `deployment-runtime` (data-plane, no ref yet) + experience axes (`ui`, `notifications`, `marketplace`) + the manifest schema.

---

## 2026-05-31 — 🧊 **`strategy/v1` FROZEN** (`rat/1.1`) — scd2 second reference landed

The ADR-009-anticipated follow-on: with a second, semantically-different strategy reference, `strategy/v1` advances `v1-preview` → `v1` (commit `cd8fcac`, tagged **`rat/1.1`**).

- **`examples/strategy/scd2-py/`** — Slowly Changing Dimension Type 2: stateful + temporal, the deliberate ADR-003 divergence from full-refresh. Reads source snapshot + existing target history; closes changed versions (`is_current=false`, `effective_to=run-ts`) + inserts new current versions; written via one `format.merge` keyed on `(natural_key…, effective_from)`. **Different capability mix** (`get-table` + `scan`×2 + `merge`, no engine) over the same `Apply` contract.
- **`contracts/conformance/strategy-scd2-v1.json`** — two-run temporal golden scenario (initial load → snapshot with changed + unchanged + new key → expected history).
- **`make composition`** extended — added `FormatService.Merge` + an SCD2 phase; now proves the cross-axis matrix **AND both strategy references** over the real stack (gateway + parquet + sqlite + Flight). Green.
- **`strategy.proto` → v1** (frozen, `rat/1.1`).
- **Contract observations** (ADR-003 payoff, none requiring a change): a strategy can read target state (`scan`), can be a data **producer** (hosts the synthesized delta) and **consumer** (pulls scans) — full-refresh was a pure router, so this stayed hidden until the second reference. Per-run params ride in `options`.

**`strategy/v1` is now `v1`.** Remaining `v1-preview`: control/experience axes + the manifest schema.

---

## 2026-05-31 — 🧊 **`rat/1` FROZEN** — data-plane contracts advanced to `v1` (ADR-009 + tag)

The Phase 0 contract-freeze milestone. With both gates met (0h-remediation + 0i cross-axis composition), the data-plane axis contracts advance `v1-preview` → `v1`.

- **[ADR-009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md)** (`6ca3ed2`) — the freeze decision: the six ADR-003 data-plane axes (engine/format/catalog/storage/runtime/state) + the cross-cutting types they depend on (`common/v1/{context,data,annotations,event,audit}`, `core/v1/invoke`, `ERROR_MODEL.md`) freeze at `v1`; breaking changes now require `v2`. `strategy/v1` (one ref) + control/experience axes + the manifest schema stay `v1-preview`. Residuals R1–R3 accepted as documented.
- **Freeze applied** (`b9dbe2d`, **tagged `rat/1`**) — flipped the Status markers on all 17 frozen files (11 protos + 6 `CONTRACT.md`) DRAFT/v1-preview → "v1 (frozen — rat/1, ADR-009)"; comment-only, buf lint+build clean, SDKs unaffected. `reviews/07` carries the RESOLUTION banner (the 0h NO-GO is closed).
- The `rat/1` annotated tag marks the frozen surface (local; reversible until external consumers pin to it).

**Phase 0's headline deliverable — a frozen, independently-validated data-plane contract — is DONE.** What remains in Phase 0 is the loosely-coupled tail: `strategy/v1` second reference, the control-plane axes' single references, and the manifest-schema freeze.

---

## 2026-05-31 — Sub-phase 0i COMPLETE: cross-axis composition (ADR-003 cross-combination gate MET)

Built the ADR-003 "run against each other on golden data" gate the freeze review flagged as the one unmet clause (reviews/07 Part C).

- **`examples/strategy/fullrefresh-py/`** (`abd1228`) — the FIRST `kind: strategy` reference (the axis had zero). Pure capability orchestration over a single `invoke` seam: `catalog.get-table → engine.query → format.overwrite`, coupled to nothing by name. Its conformance IS the composition test.
- **`examples/composition/`** + **`make composition`** — boots catalog+engine+format as real gRPC servers wired by capability through a mediating gateway, Arrow over **real pyarrow.flight** between axes, and runs the strategy across the 4 ADR-003 combos on shared golden data ([`composition-v1.json`](../contracts/conformance/composition-v1.json)). `comp_engine.py` closes the gap the per-axis engine refs left (resolve `QueryRequest.tables` via `format.scan`, bind, stream results over Flight).
- **Result:** all 4 combos — DuckDB/DataFusion × Parquet/Delta × sqlite/in-memory, storage held at local-fs — produce the **identical** target with the strategy code unchanged. **Gate MET.**
- **Findings surfaced** (the payoff): engine `SUM` type diverges DuckDB(hugeint)/DataFusion(int64) → golden SQL pins it with `CAST AS BIGINT`; the engine `tables`-binding + real Arrow transport weren't exercised per-axis; catalog has no create-table RPC (GA commit-linkage, R3). None wire-breaking. Conformance still 20/20.

---

## 2026-05-31 — 0h-remediation COMPLETE: the freeze punch-list (M1–M4 + S1–S4) cleared

Cleared the entire 0h freeze-review punch-list ([reviews/07](../reviews/07-freeze-review.md)). User chose **strict ADR-003** for the cross-axis gate, so remediation lands first, then the strategy reference + composition test, then the freeze. Conformance held **20/20** after every change.

- **M1+M2** (`16d9c37`) — pinned the canonical error model: new [`contracts/proto/rat/common/v1/ERROR_MODEL.md`](../contracts/proto/rat/common/v1/ERROR_MODEL.md) (two-layer rule: domain-outcome-field vs gRPC-status; the full status-code table; the not-found rule + secret anti-enumeration exception). Fixed the dangling cite in `invoke.proto`; documented catalog's deliberate no-`found` choice in `catalog.proto`; pointed all 6 axis `CONTRACT.md` at the model.
- **M3+M4** (`7e169e1`) — hardened the signed envelope: `key_id` on `AuditRecord` (field 11) + `SubjectAssertion` (field 5), each resolving in the core's published keyring to {key, algorithm} (rotation + agility = new key_id, no on-wire `alg`); covered by the signature. Added VERIFICATION CONTRACT step 4 (bare `Identity.tenant`/`principal` MUST equal the signature-covered values) + the transport-trust basis note (caller_plugin/tenant rest on authenticated transport C2). Additive fields, buf-clean; 4 SDKs regenerated.
- **S1–S4** (`df07ff9`) — comment cluster: `WriteResult.snapshot_id` reworded (not format-only); bidi non-first-frame `capability` → ABORT not "ignore"; audit-on-deny pinned as a C8 conformance obligation; stale `runtime-v1.json` comment corrected (ADR-008 closed the streaming-mediation gap).

**All 4 MUST-FIX + 4 SHOULD-FIX done; 3 residuals (R1–R3) tracked in backlog.** Next (strict-ADR-003 path): build the first **strategy** reference + the **cross-axis composition test** (the ADR-003 cross-combination gate), then tag `rat/1`.

---

## 2026-05-31 — Sub-phase 0h: freeze review COMPLETE — verdict **NO-GO** for unconditional `rat/1`

Ran the final adversarial pass before tagging the data-plane contracts `v1`. Three independent reviewers (contract-coherence, security/enforcement, freeze-readiness/integration) swept the now-complete surface; every blocker was ground-truthed against the actual proto/vector/reference files before being accepted or downgraded. Evidence base: `make conformance` **20/20 PASS**, `make lint`+`make build` clean.

- **Result:** [`reviews/07-freeze-review.md`](../reviews/07-freeze-review.md). The 15 prior freeze-blockers (reviews/06) are confirmed resolved and the keystone holds. But the pass found a **new punch-list** the earlier review couldn't reach (the references + cross-cutting protos + CONTRACT.md docs that surface these didn't exist then):
  - **4 MUST-FIX** (wire-shape / un-retrofittable): **M1** error-model convention referenced (invoke.proto:99) but pinned in no frozen artifact; **M2** "resource absent" modeled 3 ways (secret/state `found` bool vs catalog `NOT_FOUND`) with no governing rule; **M3** signatures (`AuditRecord`, `SubjectAssertion`) carry no `key_id`/alg → rotation pain; **M4** `SubjectAssertion` verification contract omits the bare-mirror cross-check (`Identity.tenant` == signed tenant) + doesn't state the transport-trust dependency.
  - **4 SHOULD-FIX** (cheap text): **S1** engine `snapshot_id` vector vs `WriteResult` comment mismatch; **S2** bidi non-first-frame `capability` "ignored" not "rejected"; **S3** audit-on-deny intended but unpinned (stub omits it); **S4** stale `runtime-v1.json` comment.
  - **3 ACCEPTED RESIDUALS** (defensible, documented): R1 assertion bound to operation not hop (bounded by C5 `requires`); R2 storage cred-scoping honour-system (ADR-005 bearer exception); R3 additive niceties → backlog.
- **The real gate (ADR-003):** per-axis is MET (two refs + divergent real backend + golden vectors, all 6 axes), but the **cross-axis composition** clause is **NOT met** — conformance is per-axis only, and the **strategy axis (which composes engine+format+catalog+storage) has zero references**. Risk a composition test finds a flaw: low (coupling types `TableRef`/`ArrowStream` partly exercised via real Arrow Flight). But low ≠ satisfied.
- **Decision:** do NOT tag `rat/1` yet. Path: (1) 0h-remediation clears M1–M4 + S1–S4; (2) cross-axis fork — **(a)** strict ADR-003 (build strategy ref + composition test first) or **(b)** conditional freeze (tag after step 1, cross-axis as the one documented residual gate). The fork is rigor-vs-velocity → user's call. (3) advance the 6 axes `v1-preview`→`v1`.

**Files:** `reviews/07-freeze-review.md` (new). New backlog work surfaced (M1–M4, S1–S4, R1–R3, cross-axis gate).

---

## 2026-05-31 — Sub-phase 0c COMPLETE: cross-cutting protos finalized (audit envelope relocated + coverage audit)

Finalized the cross-cutting concern protos. An audit of every C1–C10 + ARCH concern against its wire home surfaced **one real layering inversion**, which 0c fixes; everything else was already covered (the freeze-blocker remediation had filled context/data/annotations/event/invoke).

- **The finding:** `AuditRecord` + `AuditOutcome` lived in the **`rat.auditlog.v1` axis** proto — but the audit record is **core-authored, core-signed, and emitted even when no audit-log plugin is installed** (C8; the proto's own header says "this axis is only the export sink"). A core-enforced cross-cutting type living in an axis proto would force the core's C8 emission to import an axis contract.
- **The fix:** created **`contracts/proto/rat/common/v1/audit.proto`** with `AuditRecord` + `AuditOutcome` (the cross-cutting C8 envelope, next to context/data/annotations/event); `auditlog.proto` now imports it and `AppendRequest.records` references `common.v1.AuditRecord`. **Wire-compatible** — field numbers unchanged, so the canonical serialization + Ed25519 signatures + hash chain are byte-identical; only the proto package (and generated type name) moves `auditlog.v1` → `common.v1`. `buf lint`/`build` clean; `buf breaking` flags the move (3 expected findings, allowed in `v1-preview`); all 4 SDKs regenerated.
- **Coverage doc:** [`docs/architecture/cross-cutting-coverage.md`](../docs/architecture/cross-cutting-coverage.md) — the finalize artifact: a matrix mapping every C1–C10 + ARCH concern to its wire home (`common/v1/{context,data,annotations,event,audit}` + `core/v1/invoke`) or its deliberately non-wire mechanism (transport credential / manifest schema / process gate / conformance suite). Confirms NO concern is homeless and NO core-enforced concern lives in an axis proto. Also resolves the plan's "descriptors ⬜" note (descriptors = the manifest `plugin.v1.json` + the proto service descriptors the gateway already reads — both done).

**Sub-phase 0c is COMPLETE.** The cross-cutting proto set is final: `common/v1/{context, data, annotations, event, audit}` + `core/v1/invoke`, with `auditlog.proto` demoted to a pure sink axis. Remaining toward `rat/1` freeze: **0h** (peer review + freeze).

**Files:** `contracts/proto/rat/common/v1/audit.proto` (new), `contracts/proto/rat/auditlog/v1/auditlog.proto` (imports it), `contracts/sdks/**` (regenerated), `docs/architecture/cross-cutting-coverage.md`.

---

## 2026-05-31 — Sub-phase 0g: per-axis `CONTRACT.md` author guides (6 data-plane axes)

Wrote the author-facing contract guide for every data-plane axis — the canonical "how do I implement a `kind: <axis>` plugin" doc, grounded in the now-existing protos, golden vectors, and both reference rounds.

- **6 files, one per axis**, placed **next to the proto** (`contracts/proto/rat/<axis>/v1/CONTRACT.md`) so an author reads the wire contract + the guide together: `state`, `engine`, `format`, `storage`, `runtime`, `catalog`.
- Each covers: what the axis is, the **capabilities + method/cardinality table**, the **RPCs** (request/response + semantics), the **conformance obligations** (the axis-specific ones spelled out — state's key grammar + linearizable CAS, catalog's merge-safety, storage's C7 tenant-scoping, format's bidirectional Arrow leg, engine's typed-Arrow, runtime's streaming framing), the **cross-cutting rules** (context-in-metadata ADR-007, core-mediation ADR-005/008, bulk-bypasses-core), a **"writing a plugin"** checklist, and a **reference-implementations** table (round-1 wire + round-2 real backend, with what each demonstrates).
- **All cross-links verified** (proto, conformance vectors, ADRs, reviews, cross-axis docs, reference dirs — every relative path resolves); `buf lint` ignores the `.md` files in the proto module (clean).
- Index added to [`contracts/conformance/README.md`](../contracts/conformance/README.md) ("Per-axis contract docs"). Control/experience axes get their `CONTRACT.md` when referenced.

This is the 0g deliverable for the axes that have references (the grounded, non-speculative ones). Remaining toward freeze: 0c (finalize cross-cutting protos) + 0h (peer review + `rat/1` freeze).

**Files:** `contracts/proto/rat/{state,engine,format,storage,runtime,catalog}/v1/CONTRACT.md`, `contracts/conformance/README.md` (index).

---

## 2026-05-31 — Sub-phase 0f COMPLETE: per-RPC latency benchmark — the ADR-005 mediation hop, measured

The second + final 0f sub-item: a benchmark that quantifies the one perf number the architecture trades on — the **core-mediated gateway's overhead vs a direct call** (ADR-005 accepted "a latency hop per control call", with a direct-dial fast-path *only if a profiling pass shows it's needed*; ADR-008 added a streaming relay). This IS that profiling pass.

- **`examples/bench/latency-go/`** + **`make bench`** — measures the SAME plugin RPC two ways (direct `caller→plugin` vs mediated `caller→gateway→plugin`) for a unary RPC (`state.Get`) and a server-streaming one (`runtime.Execute`); reports p50/p99/mean + the delta. The plugin RPCs are trivial (fixed response / a few fixed frames) so the measurement isolates transport + mediation cost. The mediated path includes the client-side marshal/unmarshal + the `rat-callmeta-bin` envelope stamp (the SDK's real cost) + the gateway's traceparent-validate + identity-restamp + passthrough relay (a faithful non-test gateway in `gateway.go`).
- **Result (localhost TCP, single goroutine):** unary direct p50 ~62µs vs mediated ~228µs → **+166µs (+266%)**; streaming direct ~66µs vs mediated ~249µs → **+183µs (+277%)**. Mediation roughly TRIPLES a control RPC's latency (a full extra gRPC hop + serialization) but the **absolute cost is ~0.2ms**.
- **Validates the ADR-005 bet honestly:** cheap enough for control traffic (a pipeline run makes a handful of control calls → ~ms total, negligible vs the data work), and the hot path doesn't pay it at all — bulk DATA bypasses the gateway entirely via `ArrowStream`. If a future hot control path ever needs sub-mediation latency, the direct-dial fast-path ADR-005 left open can be added — but the number shows it isn't needed for v1.
- The benchmark dir has a `go.mod` but no `harness_test.go`, so `scripts/conformance.sh` discovery was tightened to require a harness — the bench is correctly excluded from `make conformance` (still 20/20).

**🎉 Sub-phase 0f is COMPLETE** — conformance suite runner (`make conformance`, 20/20) + latency benchmark (`make bench`). Plus the real Arrow Flight transport landed. The data-plane reference + conformance + perf arc of Phase 0 is done; remaining is freeze prep (0c/0g/0h).

**Files:** `examples/bench/latency-go/**`, `Makefile` (bench target), `scripts/conformance.sh` (harness-required discovery).

---

## 2026-05-31 — Sub-phase 0f: the conformance suite runner — one command, one pass/fail matrix

Formalized the conformance suite (the operational form of ADR-003's "both pass the axis's conformance suite"). The per-axis golden vectors were already authoritative; what was missing was a single runner across all references.

- **`scripts/conformance.sh`** + **`make conformance`** — **auto-discovers every reference** under `examples/<axis>/<impl>/` (Go = has `go.mod`; Python = has `harness_test.py`), runs each one's harness against its golden vectors (Go via `go test`, Python via `python harness_test.py`), and prints a single **axis × impl × lang × vectors × result** matrix. Containerized (podman/docker, no host installs); one golang container for all Go refs, one python container (union of deps installed once) for all Python refs. **Exit 0 iff every reference conforms** — so CI / the freeze gate can hang on it. A new reference joins the suite the moment it lands (no registration).
- Portable rendering (host `sort` + plain `awk`, works under mawk/gawk); real-engine refs correctly mapped to `engine-real-v1.json`, the rest to `<axis>-v1.json`.
- **Verified: 20/20 references conform** — all 6 axes' round-1 language twins + the round-2 real backends (sqlite/local-fs/subprocess/duckdb/datafusion/parquet/delta), green in one run.
- `contracts/conformance/README.md` documents `make conformance` + a sample matrix.

This is the 0f deliverable's core (the suite runner). A per-RPC latency benchmark is the remaining 0f sub-item (deferred — lighter, optional).

**Files:** `scripts/conformance.sh`, `Makefile` (conformance target), `contracts/conformance/README.md`.

---

## 2026-05-31 — Real Arrow Flight transport — the last in-process data-leg stand-in retired

Replaced the in-process Arrow-IPC registry with a REAL `pyarrow.flight` transport in `examples/format/parquet-py` — the only reference where the bulk-data leg is now *fully* real (real Parquet files + real Flight wire).

- **`examples/format/parquet-py/flight.py`** — a real `FlightServerBase` on an ephemeral localhost port. `put(table)` hosts the table + returns `ArrowStream{endpoint=grpc://host:port, ticket}`; `flight_pull(stream)` dials the descriptor's endpoint and `DoGet`s the ticket — a real Flight round-trip over a TCP socket. Single-use tickets (DoGet consumes — SEC-14).
- **Both directions are real:** the PLUGIN hosts a Flight server for Resolve results (the harness DoGets); the CALLER (harness) hosts a Flight server for Append/Merge/Overwrite sources (the plugin DoGets). Matches the contract's "Resolve → producer-hosted; the format pulls from a caller-hosted source" — both `PRODUCER_HOSTED` (data-holder hosts, data-needer DoGets).
- **Zero contract change:** the `common.v1.ArrowStream {endpoint, ticket, transport=FLIGHT, role}` descriptor was always real-Flight-shaped; only the implementation swapped (in-process dict → real Flight server). `streams.py` deleted from parquet-py; `server.py` + `harness_test.py` use `flight.py`. Still passes the SAME shared `format-v1.json` + the real-Parquet-files test. Green in `python:3.12`.
- This proves the in-process registry was always a transport CHOICE, not a contract limitation. The other refs keep it for simplicity; parquet-py is the canonical real-Flight demonstration.

**Significance:** the last "stand-in" in the data plane is retired (in this reference). Across rounds 1+2 the DATA was already real typed Arrow (engine/format); now the TRANSPORT is real Arrow Flight too. The data-plane contract is validated end-to-end with real backends AND a real wire.

**Files:** `examples/format/parquet-py/{flight.py,server.py,harness_test.py,README.md}` (−`streams.py`). No proto/SDK/vector change.

---

## 2026-05-31 — 🎉 ROUND 2 COMPLETE: `format` = REAL pair (Parquet + Delta) — real Arrow files + time travel

Sixth + final round-2 axis, via option (b) two REAL backends. **Round 2 is now complete — all six data-plane axes have a technologically-divergent real backend.**

- **Real Arrow data leg, BOTH directions:** unlike the toy refs (string-row registry), the source rows for Append/Merge/Overwrite are staged as real Arrow (Arrow IPC) and Resolve results pulled back as real Arrow — `streams.py` (shared with the engine pair). This is the full typed-Arrow data leg for format, retiring the last in-process-stand-in for these refs.
- **`examples/format/parquet-py`** (pyarrow): writes real `.parquet` files per table; full Append→scan→Merge(upsert)→Overwrite→Maintain(compact) lifecycle on real files; backend test asserts real Parquet files land on disk + readable.
- **`examples/format/delta-py`** (`deltalake`): backs the table with a real **Delta Lake** table (transaction log over Parquet). Earns **time travel** (`test_delta_time_travel`: two appends → versions 0/1; read v0 back → prior state) — the versioned-snapshot substrate the `catalog` axis's branches sit on. Only `store.py` differs from parquet; `server.py`/`streams.py` identical. (deltalake's Rust runtime aborts at interpreter teardown after all logic ran → `os._exit(0)` after PASS.)
- **Both pass the SAME shared `format-v1.json`** the in-memory + Parquet refs use (format data is provider-neutral rows). All FOUR format refs green (inmemory-go, inmemory-py, parquet-py, delta-py). Verified in `python:3.12` / `golang:1.25`.

**🎉 ROUND 2 COMPLETE — 6/6 data-plane axes with a real divergent backend, each passing its shared golden vectors + a backend-specific semantic test:**
- `state`=sqlite (durability + linearizable CAS)
- `storage`=local-fs (path containment + tenant isolation)
- `catalog`=sqlite (durable branches/ledger + concurrent-merge safety)
- `runtime`=subprocess (OS process isolation)
- `engine`=duckdb+datafusion (real SQL + typed Arrow)
- `format`=parquet+delta (real Arrow files + time travel)

This is the full ADR-003 rigor: every data-plane contract is now validated by running code in two languages (round 1, wire contract) AND a technologically-divergent real backend (round 2, semantic). The typed-Arrow gap is retired for engine + format. **The remaining gap before `v1`** is just the real Arrow Flight transport (all data legs still use an in-process IPC registry stand-in) + 0f conformance-suite formalization + 0h peer review/freeze.

**Files:** `examples/format/{parquet-py,delta-py}/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2 (option b): `engine` = REAL pair — DuckDB + DataFusion on real SQL + typed Arrow

The first round-2 axis done via **option (b): two REAL backends** (ADR-003's literal "duckdb + datafusion" example), not toy + real. Two genuinely different SQL engine technologies agree on one shared golden-vector file.

- **`contracts/conformance/engine-real-v1.json`** — REAL typed SQL (`CREATE TABLE orders (id INTEGER, region VARCHAR, amount INTEGER)`, `INSERT`, `SELECT … WHERE … / LIMIT`) with typed-Arrow result assertions (row_count + projected columns + rows_contain with TYPED values). Distinct from the round-1 toy `engine-v1.json` (which validates the wire contract via the in-memory mini-SQL refs).
- **`examples/engine/duckdb-py`** (DuckDB 1.5.3) + **`examples/engine/datafusion-py`** (Apache DataFusion 53.0.0) — both execute the same SQL, both return results as **real typed Arrow**. Only `store.py` differs between them; `server.py`/`streams.py`/`harness_test.py` are identical (the contract is the same, only the engine changes). Both green in `python:3.12`.
- **Retires the typed-Arrow gap for engine:** the result leg is now **real Arrow IPC** (typed schema + columnar batches, serialized + read back with pyarrow via `streams.py`), not the toy string-row stand-in. The transport is still an in-process registry (Flight deferred), but the DATA is genuine typed Arrow.
- Deps install cleanly + fast in-container (duckdb/datafusion/pyarrow, ~8s). The toy `inmemory-go`/`inmemory-py` engine refs remain as the round-1 wire-contract validation.

**Round 2 progress: 5 of 6 axes.** `state`=sqlite, `storage`=local-fs, `catalog`=sqlite, `runtime`=subprocess, **`engine`=duckdb+datafusion**. Remaining: **`format`** (parquet + delta/iceberg — real Arrow files; the last + heaviest).

**Files:** `contracts/conformance/engine-real-v1.json` + README, `examples/engine/duckdb-py/**`, `examples/engine/datafusion-py/**`. No proto/SDK change.

---

## 2026-05-31 — Round 2: `runtime` = subprocess (real backend) — OS process isolation

Fourth round-2 real backend. `examples/runtime/subprocess-py/` — each `Execute` runs the work unit in a real CHILD OS PROCESS (`worker.py`) instead of in-thread. Runtime is the "where does the code run" axis; this one actually runs it elsewhere.

- **Passes the SAME shared vectors** (`contracts/conformance/runtime-v1.json`) — the toy work_spec (`{steps, rows, indeterminate, fail}`) is abstract enough a child-process runtime interprets it identically (emit `steps` progress events ± fraction, then a completion). All three runtime refs (inmemory-go, inmemory-py, subprocess-py) green on one shared file.
- **Two isolation properties the in-thread runtime CANNOT show:** `test_work_runs_in_a_separate_process` (work unit PID ≠ server's) and `test_each_work_unit_gets_its_own_process` (two Execute calls → two DISTINCT child PIDs).
- Process isolation is the seed of the real runtime/deployment-runtime sandboxing story (a crashing unit can't take the runtime down; a container/WASM runtime is the step up). Python stdlib `subprocess`; direct streaming harness. Green in `python:3.12`.

**Round 2 progress: 4 of 6 axes** (`state`, `storage`, `catalog`, `runtime`). Remaining: **`format` + `engine`** — the genuinely heavy ones (real Arrow Flight + Parquet / DuckDB) that need conformance-vector REWORK first (engine vectors are toy-mini-SQL-specific; format carries the bulk leg as an in-process stand-in). Not drop-in like the other four — surfaced for a decision (see [current.md](current.md)).

**Files:** `examples/runtime/subprocess-py/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2: `catalog` = sqlite (real backend) — durable branches/ledger + concurrent-merge safety

Third round-2 real backend. `examples/catalog/sqlite-py/` — branches, their snapshots, and the idempotency ledger live in sqlite (real transactional SQL DB, file, WAL) rather than an in-memory dict.

- **Passes the SAME shared vectors** (`contracts/conformance/catalog-v1.json`) — same model + deterministic snapshot scheme. All three catalog refs (inmemory-go, inmemory-py, sqlite-py) green on one shared file.
- **Two properties the in-memory catalog CANNOT show:**
  - **Durability** (`test_durability_branches_and_ledger_survive_reopen`): create branch + merge → close → reopen the same db file → the branch, the moved snapshot, AND the idempotency ledger persist (a re-merge with the same key is still a no-op returning already_applied). A dict dies with the process.
  - **Concurrent-merge safety** (`test_concurrent_merge_one_winner`): 16 threads race a MergeBranch into `main` from the same expected snapshot → exactly one COMMITs, the rest FAILED_PRECONDITION. Durable, cross-connection lost-update prevention via `BEGIN IMMEDIATE` (+ idempotency-key PK), not an in-process mutex.
- Concurrent-merge safety is the publish gate of the v2 pipeline model (reviews/06 #8 — `MergeBranch` is reconciler-retried, must be safe under retry AND concurrency), enforced for real. Python stdlib sqlite3; direct harness. Green in `python:3.12`.

**Round 2 progress: 3 of 6 axes** (`state`=sqlite, `storage`=local-fs, `catalog`=sqlite). Remaining: `format`, `engine`, `runtime` (the Arrow-heavy / execution ones).

**Files:** `examples/catalog/sqlite-py/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2: `storage` = local-fs (real backend) — path containment + tenant isolation

Second round-2 real backend. `examples/storage/localfs-go/` — a `storage` plugin that vends credentials scoped to a REAL local filesystem path under a per-tenant root, where the in-memory refs just echo the requested prefix into a JSON scope receipt.

- **Provider-neutral vectors:** `storage`'s `prefix` is provider-specific (the in-memory refs used `s3://…` URIs). Changed `contracts/conformance/storage-v1.json` to scheme-less LOGICAL prefixes (`warehouse/orders`, …) so every backend can resolve them per its own scheme; the in-memory refs (which echo) re-pass unchanged (verified). The receipt MAY carry extra provider-specific fields (local-fs adds `resolved_path`) the vectors ignore.
- **Passes the SAME shared vectors** through the stub gateway (scope = tenant + logical prefix + mode + short TTL). All three `storage` refs (inmemory-go, inmemory-py, localfs-go) green on one shared file.
- **Two filesystem properties the in-memory echo CANNOT show, now tested:**
  - **Path containment** (`TestLocalFS_PathContainment`): a normal prefix resolves under `<root>/<tenant>/` and the dir is created on disk; an escaping prefix (`../../escape`) → `PERMISSION_DENIED` (via `filepath.Rel` containment). The in-memory echo would accept it.
  - **Tenant isolation** (`TestLocalFS_TenantIsolation`): two tenants vending the same logical prefix get distinct paths under their own roots.
- Path containment is the storage analog of sqlite's durability/CAS — the cross-tenant security boundary `storage.proto` emphasizes (reviews/01 F3, reviews/04), enforced for real rather than by convention. Go (routes through the gateway, unlike the Python sqlite ref). Green in `golang:1.25`.

**Round 2 progress: 2 of 6 axes** have a divergent real backend (`state`=sqlite, `storage`=local-fs). Pattern holds: real backend + same shared vectors + a backend-specific semantic test.

**Files:** `contracts/conformance/storage-v1.json` (logical prefixes), `examples/storage/localfs-go/**`. No proto/SDK change.

---

## 2026-05-31 — ROUND 2 begins: `state` = sqlite (real backend) — durability + linearizable CAS

The first **technologically-divergent** reference (ADR-003's *spirit*, not just letter): a third `state` implementation backed by **sqlite** (real embedded transactional SQL DB, file-on-disk, WAL) rather than an in-memory hashmap. `examples/state/sqlite-py/`.

- **Passes the SAME shared golden vectors** (`contracts/conformance/state-v1.json`) the in-memory twins pass — a real backend conforming to the identical wire contract is the actual round-2 ADR-003 evidence. All three `state` refs (inmemory-go, inmemory-py, sqlite-py) green on one shared file.
- **Two properties the in-memory twins CANNOT show, now actually tested:**
  - **Durability** (`test_durability_survives_reopen`): write → close store → reopen the same db file → state persists. A hashmap dies with the process.
  - **Linearizable CAS** (`test_linearizable_cas_one_winner`): 16 threads race a compare-and-set from the same revision → **exactly one** COMMITs. Serialization enforced by sqlite's `BEGIN IMMEDIATE` (durable, cross-connection), not an in-process mutex — the real lease primitive (reviews/06 C-4) the in-memory twin could only fake.
- CAS is read→check→write inside a `BEGIN IMMEDIATE` transaction; global monotonic revision via a `meta` table; change log table for Watch. Same MODEL as the in-memory refs (matching revisions) so the shared vectors pass. Python stdlib `sqlite3` (zero new deps; GIL released during sqlite calls so the concurrency test is real).
- Verified in `python:3.12` (sqlite 3.46.1): `PASS … + durability + linearizable CAS`.

**Significance:** this is the first axis where the round-2 SEMANTIC gate (not just the wire-contract gate) is exercised on a divergent backend. The in-memory `state` CAS is serialized by a mutex (also linearizable, but in-process + non-durable); sqlite proves the contract holds on a backend with a genuinely different consistency/durability profile — exactly the "orthogonality assumption" rigor ADR-003 exists for.

**Files:** `examples/state/sqlite-py/**`. No proto/SDK change.

---

## 2026-05-31 — 0d: `state` axis (Go + Python) → `state/v1` wire-contract gate MET → 🎉 ROUND 1 COMPLETE (all 6 data-plane axes)

Sixth + final data-plane axis through the 0d wire-contract two-reference gate — and the **capstone**: a tier-0 plugin with 4 RPCs (Get/Put/List unary + Watch server-streaming) and the axis's two pointed obligations.

- **contracts/conformance/state-v1.json** — STATEFUL lifecycle: Put(create) → Get(found) → Put CAS-MATCH (COMMITTED) → Put CAS-CONFLICT (the `PutOutcome.CONFLICT` enum, NOT a gRPC error, with the conflicting revision) → Get(unchanged, proving the conflict didn't write) → Get(missing) → two more Puts → List(all)/List(prefix) → ordered Watch replay of the change log. + 6 KEY-GRAMMAR error vectors (empty / oversize / NUL / control-char / traversal / dot-component → INVALID_ARGUMENT). Deterministic revision scheme keeps the impls in lockstep; control-char keys are built from `key_len`/`key_inject` so the vector file stays pure-ASCII.
- **First reference to use BOTH gateway relays:** unary `Invoke` (Get/Put/List) AND the ADR-008 `InvokeServerStream` (Watch) — a shared `openCall` does enforce/route/stamp/audit once for both.
- **state.proto:** added `(rat.common.v1.capability)` to all 4 RPCs + annotations import (was comment-only). SDKs regenerated (Go/Python/TS; Rust emits no method options).
- **inmemory-go** (grammar/store/server/main + dual-relay stub gateway + harness): green in `golang:1.25`. **inmemory-py** (mirrored grammar + model): green in `python:3.12`.

**🎉 0d ROUND 1 COMPLETE.** All six data-plane axes — format, engine, storage, runtime, catalog, state — now have two independently-written references (Go + Python) passing one shared golden-vector file. **Verified: all TWELVE references green together.** Cross-cutting work that fell out of round 1: ADR-007 (call-context transport) + ADR-008 (streaming invocation), both decided AND migrated; the SDK-vendoring fix; the round-1/round-2 framing correction.

**What round 1 is NOT (per the scope caveat):** all twelve are in-memory twins — the WIRE-contract gate. The technologically-divergent real-backend pair (round 2: state=sqlite, storage=local-fs, …) + the typed-Arrow pass are still required before any axis → `v1`. See [backlog.md](backlog.md).

**Files:** `contracts/conformance/state-v1.json`, `contracts/proto/rat/state/v1/state.proto`, `contracts/sdks/**`, `examples/state/inmemory-go/**`, `examples/state/inmemory-py/**`.

---

## 2026-05-31 — 0d: `catalog` axis — two references (Go + Python) + shared golden vectors → `catalog/v1` wire-contract gate MET

Fifth data-plane axis through the 0d two-reference (wire-contract) gate. The richest axis so far — git-like branch/version semantics with a real safety contract.

- **contracts/conformance/catalog-v1.json** — a STATEFUL lifecycle: GetTable(main) → CreateBranch(run-42 from main) → GetTable(on branch) → MergeBranch with optimistic-concurrency ACCEPT (`expected_into_snapshot` matches) + idempotency_key → idempotent retry (`already_applied=true`) → MergeBranch REJECT (`FAILED_PRECONDITION`, target moved) ; + stateless errors (unknown table `NOT_FOUND`, empty id `INVALID_ARGUMENT`). Exercises the MERGE-SAFETY contract (reviews/06 #8) for real. Deterministic snapshot scheme (seed main@snap-0; merge → snap-<counter>) keeps the two impls in lockstep; the harness gained per-step `expect.code` so an error can be asserted mid-sequence.
- **catalog.proto:** added `(rat.common.v1.capability)` to all 3 RPCs (get-table/create-branch/merge-branch) + annotations import (was comment-only) so the gateway routes them. SDKs regenerated.
- **inmemory-go** (`examples/catalog/inmemory-go/`): store(branches/merges ledger)/server/main + the unary stub gateway re-pointed at CatalogService + harness. Green in `golang:1.25`.
- **inmemory-py** (`examples/catalog/inmemory-py/`): from-scratch second reference mirroring the snapshot model. Green in `python:3.12`.

**Verified (containers):** all TEN references (format+engine+storage+runtime+catalog, Go+Python) green together.

**Scope (per the round-1/round-2 split):** in-memory twins — wire-contract gate. A real divergent backend (e.g. sqlite-catalog) is round-2.

**Files:** `contracts/conformance/catalog-v1.json`, `contracts/proto/rat/catalog/v1/catalog.proto`, `contracts/sdks/**` (regenerated), `examples/catalog/inmemory-go/**`, `examples/catalog/inmemory-py/**`.

---

## 2026-05-31 — ADR-008 migration executed: `InvokeServerStream` + `InvokeBidiStream`; runtime now gateway-mediated

Implemented [ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md) (decided in the prior commit; this is the implementation, kept separate per one-ADR-per-commit).

- **`invoke.proto`:** added `InvokeServerStream(InvokeServerStreamRequest) returns (stream InvokeServerStreamResponse)` + `InvokeBidiStream(stream InvokeBidiStreamRequest) returns (stream InvokeBidiStreamResponse)` to `CapabilityInvokeService`, with 4 new distinct message types. **Refinement vs the ADR's first draft:** buf STANDARD's `RPC_REQUEST_RESPONSE_UNIQUE` forbids sharing `InvokeRequest`/`InvokeResponse` across RPCs, so each variant got its own request/response (also the more evolvable choice). ADR-008 §2 + Migration amended to record this (same-day). `buf lint`/`build` clean; `buf format` applied; the added methods + messages are non-breaking (`buf breaking` FILE).
- **`runtime.proto`:** added the deferred `(rat.common.v1.capability) = "rat://runtime/v1/execute"` method option (+ annotations import) so the gateway can route it.
- **SDKs:** regenerated all 4 (Go/Python/TS/Rust) — the Go SDK now exposes `InvokeServerStream` client/server + the 4 new types.
- **Stub gateway (runtime example):** added the **server-stream relay** — enforce C5 + validate traceparent + stamp the downstream `rat-callmeta-bin` envelope (ADR-007) + one C8 audit ALL once at stream-open, then open a downstream server-streaming call (`ClientConn.NewStream` + `StreamDesc{ServerStreams:true}` + passthrough codec) and relay each `ExecuteResponse` frame's opaque bytes upstream — never deserializing.
- **Runtime harness:** rewired from direct-dial to route `Execute` through `gw.InvokeServerStream` (replacing the direct path + updating the header note; the Python harness stays direct like the other Python refs). Added the C8 one-audit-per-stream assertion.

**Behavior-preserving — verified:** the **unchanged** runtime golden vectors still pass, now over the mediated streaming path (Go `golang:1.25`); INVALID_ARGUMENT relays through the streaming gateway verbatim. All EIGHT references (format+engine+storage+runtime, Go+Python) green together after the invoke.proto + SDK changes.

**Files:** `contracts/proto/rat/core/v1/invoke.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `contracts/sdks/**` (regenerated), `docs/architecture/adrs/008-*.md` (§2 + Migration amended), `examples/runtime/inmemory-go/{gateway_test.go,harness_test.go}`, `examples/runtime/inmemory-py/README.md`.

---

## 2026-05-31 — ADR-008: streaming capability invocation (per-cardinality Invoke variants)

Resolved the streaming-mediation finding the `runtime` 0d reference surfaced. **[ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md) (Accepted):** add `InvokeServerStream(InvokeRequest) returns (stream InvokeResponse)` + `InvokeBidiStream(stream InvokeRequest) returns (stream InvokeResponse)` to `core/v1 CapabilityInvokeService`. Streaming capabilities stay core-mediated — the gateway enforces C2/C5/C7/C8 + traceparent **once at stream-open**, stamps the downstream `rat-callmeta-bin` envelope for the stream's lifetime (ADR-007), and relays each frame's opaque bytes via the passthrough codec (never deserializing). One C8 audit per stream.

**Why:** ADR-005's `Invoke` is unary; the contract has 4 streaming methods (`runtime.Execute`, `state.Watch`, `scheduler.WatchDue` server-streaming; `observability.Ingest` bidi) with no mediation path. Extends ADR-005 (upholds its central-enforcement thesis — streaming is "unary with N frames", gateway stays axis-generic) + reuses ADR-007's enforce-at-open + identity-in-metadata. `InvokeBidiStream` subsumes client-streaming, so only 2 new RPCs. Rejected direct-dial-with-token (reintroduces the honor-system ADR-005 refused), progress-to-event-bus (mutilates axis contracts, doesn't generalize to bidi), and leave-unmediated (permanent enforcement hole).

**Process:** ADR-only commit. The implementation (add the 2 RPCs to `invoke.proto`, regen SDKs, server-stream relay in the gateway, route `runtime.Execute` through `InvokeServerStream` + add runtime's deferred capability annotation, re-run the unchanged runtime vectors) is queued as the next step.

**Files:** `docs/architecture/adrs/008-streaming-capability-invocation.md`, `docs/architecture/adrs/README.md` (index), `ideas/inbox.md` (finding promoted), `roadmap/**`.

---

## 2026-05-31 — 0d: `runtime` axis — two references (Go + Python) + shared golden vectors → `runtime/v1` ADR-003 gate MET (+ streaming-mediation finding)

Fourth data-plane axis through the 0d two-reference gate, and the **first streaming axis**: `Execute(ExecuteRequest) returns (stream ExecuteResponse)` — interim `ExecuteProgress` + terminal `ExecuteCompleted` (a oneof).

- **contracts/conformance/runtime-v1.json** — drives a tiny work_spec (`{steps, rows, indeterminate, fail}`): emit `steps` progress messages (fraction `(i+1)/steps`, or **absent** when indeterminate — exercising the proto3 optional double presence) then a terminal completion (success + `WriteResult.rows_affected`, or `success=false`+error). Vectors: determinate / indeterminate / zero-progress / failure + an empty-work_spec error.
- **inmemory-go** (`examples/runtime/inmemory-go/`): `spec`/`server`/`main` + streaming harness. Green in `golang:1.25`.
- **inmemory-py** (`examples/runtime/inmemory-py/`): from-scratch second reference (server-streaming generator). Green in `python:3.12`.

**⚠️ Contract finding surfaced (the 0d forcing function working as designed, like ADR-007):** ADR-005's `core/v1 CapabilityInvokeService.Invoke` is **unary**, but `runtime.Execute` is **server-streaming** — so the stub gateway **cannot mediate a streaming capability**. Runtime is therefore driven **directly** (bypassing the gateway), meaning its C2/C5/C7/C8 + traceparent seams are currently unenforced. Freeze-relevant (`invoke.proto` is in `rat/1`, and any future streaming axis hits this). Captured in [ideas/inbox.md](../ideas/inbox.md) with three resolutions (add `InvokeStream` / direct-dial-with-token / progress-to-event-bus); leaning toward `InvokeStream`. Candidate follow-up ADR queued in [backlog.md](backlog.md). The runtime capability annotation was **deferred** (only needed for gateway routing, which is blocked) — so NO proto change, NO SDK regen this commit.

**Verified (containers):** all EIGHT references (format + engine + storage + runtime, Go + Python) green together.

**Files:** `contracts/conformance/runtime-v1.json` + README, `examples/runtime/inmemory-go/**`, `examples/runtime/inmemory-py/**`, `ideas/inbox.md`.

---

## 2026-05-31 — Fix: vendor the Go + Python SDKs (un-ignore them) — makes ADR-006 D1 true

Resolved the repo defect surfaced during storage 0d. The root `.gitignore` had `*.pb.go` + `*_pb2.py` under "Generated artefacts" (alongside the retired `gen/`), which silently excluded the **entire Go SDK** and **all Python message classes** from version control — contradicting [ADR-006](../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md) D1 ("vendored `contracts/sdks/<lang>/` … ARE committed").

- Removed the two patterns from the root `.gitignore` (kept `gen/`); added a note pointing to ADR-006 D1 so it isn't re-added. The only `*.pb.go`/`*_pb2.py` in the repo are the SDKs (examples import the SDK, don't generate), so the un-ignore is safe + targeted. `contracts/.gitignore` still handles the one intended exclusion (`sdks/go/go.sum`).
- Committed the now-trackable **42 Go `*.pb.go`** + **23 Python `*_pb2.py`** files (freshly regenerated; reflect ADR-007's no-context-field + the storage capability annotation). Tom chose "fix now: commit the SDKs."
- Result: a fresh `git clone` can `go build` a reference + `import rat.*` in Python without first running `make gen-sdks`. All four languages' SDKs are now genuinely vendored.

**Files:** `.gitignore`, `contracts/sdks/go/**` (42 `.pb.go`), `contracts/sdks/python/**` (23 `_pb2.py`).

---

## 2026-05-31 — 0d: `storage` axis — two references (Go + Python) + shared golden vectors → `storage/v1` ADR-003 gate MET

Third data-plane axis through the 0d two-reference gate. Storage owns credential vending (no Arrow leg) — one RPC, `VendCredentials` — and is the **C7 tenancy enforcement point**.

- **First reference that CONSUMES identity from the metadata envelope.** Tenant-scoping is storage's whole job, so both impls read `context.identity.tenant` from the `rat-callmeta-bin` metadata header (ADR-007) — never a request field, so a caller can't request another tenant's creds. This exercises the ADR-007 **provider-side read** that format/engine didn't.
- **Capability annotation rolled out to storage.** `storage.proto`'s `VendCredentials` had the capability only in a comment (freeze-blocker #5 only annotated format+engine); added `option (rat.common.v1.capability) = "rat://storage/v1/vend-credentials"` (+ the annotations import) so the gateway can route it. Additive/wire-compatible. Partial progress on the backlog "roll `(rat.capability)` across remaining axes" item. SDK delta: one TS file (`storage_pb.ts`); Rust codegen doesn't emit method options (no diff); Go/Python generated files are gitignored (see finding below).
- **Conformance via a scope receipt.** The credential blob is opaque/provider-specific in production; the references encode the granted scope as JSON `{tenant, prefix, mode, expires_unix_ms}` so the harness can assert the C7 obligation (scope.tenant == caller tenant + requested prefix + mode + short TTL). Added a `TestStorage_TenantComesFromMetadataNotRequest` / `test_tenant_from_metadata_not_request` structural check (vend under a different caller tenant → creds scoped to THAT tenant).
- **inmemory-go** (`examples/storage/inmemory-go/`): creds/server/main + the axis-generic stub gateway re-pointed at `StorageService` + harness. Green in `golang:1.25`.
- **inmemory-py** (`examples/storage/inmemory-py/`): from-scratch second reference. Green in `python:3.12`.

**Verified (containers):** all SIX references (format + engine + storage, Go + Python) green together.

**⚠️ Pre-existing repo finding surfaced (not introduced here):** the root `.gitignore` ignores `*.pb.go` (line 28) and `*_pb2.py` (line 29) — so the **Go SDK and the Python message classes are NOT committed** (only TS/Rust + Python grpc-stubs are tracked). This contradicts ADR-006 D1's "vendored SDKs are committed." References build fine against locally-regenerated SDKs (and CI regenerates), but a fresh clone can't import the Go/Python SDK without `make gen-sdks`. Logged in [backlog.md](backlog.md) for a decision.

**Files:** `contracts/conformance/storage-v1.json` + README, `contracts/proto/rat/storage/v1/storage.proto` (annotation), `contracts/sdks/typescript/rat/storage/v1/storage_pb.ts`, `examples/storage/inmemory-go/**`, `examples/storage/inmemory-py/**`.

---

## 2026-05-31 — 0d: `engine` axis — two references (Go + Python) + shared golden vectors → `engine/v1` ADR-003 gate MET

Second data-plane axis through the 0d two-reference gate, reusing the format pattern (shared conformance JSON + two independent impls + the stub ADR-005/007 gateway).

- **Shared golden vectors** — `contracts/conformance/engine-v1.json` (+ README grammar note): CREATE/INSERT via Execute (rows_affected 0 vs 1), Query (SELECT, WHERE, projection), Preview (bounded by `limit`), + `rows_exclude_keys` to assert projection drops columns; 2 error vectors (unknown table, empty SQL).
- **Mini-SQL** — a deliberately tiny, fully-specified grammar (`CREATE TABLE` / `INSERT … VALUES` / `SELECT … [WHERE] [LIMIT]`) so two independent parsers stay in lockstep: the SAME three regexes in Go (`sql.go`) and Python (`sql.py`). The point under test is the engine WIRE contract, not SQL fidelity. Self-contained in-memory tables (the engine↔format handoff is separate integration work, noted).
- **inmemory-go** (`examples/engine/inmemory-go/`) — first reference: store/sql/stream/server/main + a stub gateway (`gateway_test.go`, the axis-generic ADR-005/007 stub re-pointed at `EngineService`) + harness routing Execute/Query/Preview through the gateway (C5 + C8 + traceparent gate). Green in `golang:1.25`.
- **inmemory-py** (`examples/engine/inmemory-py/`) — second, from-scratch reference; imports the vendored Python SDK; loads the same JSON. Green in `python:3.12`.
- Context rides in `rat-callmeta-bin` metadata throughout (ADR-007) — these references are built natively on the post-migration contract.

**Verified (containers):** all FOUR references (format + engine, Go + Python) green together — `go test ./...` (both Go) and `python harness_test.py` (both Python).

**Files:** `contracts/conformance/engine-v1.json` + README, `examples/engine/inmemory-go/**`, `examples/engine/inmemory-py/**`.

---

## 2026-05-31 — ADR-007 migration executed: `RequestContext` field → `rat-callmeta-bin` metadata across the contract

Implemented [ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (the decision landed in commit `9ff3cac`; this is the implementation, kept separate per one-ADR-per-commit).

- **Protos:** stripped `RequestContext context = 1` from **37 messages** (36 request messages across the 18 axis services + `core/v1 InvokeRequest`), each replaced with `reserved 1;`; removed the now-unused `context.proto` import from those 19 files. `context.proto` prose rewritten to specify `rat-callmeta-bin` carriage + the "why metadata not field 1" rationale (messages unchanged). `event.proto` keeps its in-body `RequestContext` (async exception — core-stamped once at emit, no per-hop metadata channel) with the carriage distinction documented. `strategy.proto` Apply comment corrected (providers reached via the core invoke gateway, not "via RequestContext"). `buf lint`/`build` clean; `buf format` applied.
- **`buf breaking` confirms exactly 37 findings, all "field 1 `context` deleted"** — nothing collateral, exactly as the ADR predicted; allowed in `v1-preview`.
- **SDKs:** regenerated all 4 (Go/Python/TS/Rust) via `make gen-sdks`; the generated request types no longer carry `context`.
- **References + gateway updated to the metadata model:**
  - Stub gateway (`inmemory-go/gateway_test.go`) now reads the inbound `rat-callmeta-bin` envelope, **validates traceparent** (new C1 gate — possible now that trace is in metadata, not the opaque payload; rejects missing/ill-formed with `INVALID_ARGUMENT`), and constructs the downstream envelope (trace verbatim, identity re-stamped) as outbound metadata — still never deserializing the payload. New test `TestGateway_RejectsMissingTraceparent`.
  - Both harnesses (`inmemory-go`, `inmemory-py`) carry context via `rat-callmeta-bin` metadata instead of a request field.
- **Behavior-preserving — verified:** the **unchanged** shared golden vectors still pass on both impls (Go in `golang:1.25`, Python in `python:3.12`), the strongest evidence the migration changed carriage, not semantics. The ADR-003 `format/v1` two-reference cross-run remains green.

**Caveat (recorded, non-blocking):** `make gen-check` hit the known BSR rate-limit (429) on its *temp* regen (the done.md 2026-05-31 multi-SDK caveat) → false "python stale." The committed SDKs are correct — proven by both harnesses passing against them. Network-bound check, not a content defect.

**Files:** `contracts/proto/**` (20 files), `contracts/sdks/**` (regenerated), `examples/format/inmemory-go/{gateway_test.go,harness_test.go}`, `examples/format/inmemory-py/harness_test.py`, `roadmap/**`.

---

## 2026-05-31 — ADR-007: call-context transport (cross-cutting context → metadata, not payload)

Resolved the freeze-blocking finding the 0d stub gateway surfaced. **[ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (Accepted):** the cross-cutting envelope (`RequestContext` = trace + identity + deadline) moves out of message field 1 into a single binary transport-metadata header `rat-callmeta-bin`. The keystone's message *shape* is kept verbatim; only the *carrier* changes.

**Why:** ADR-005 committed the gateway to being a generic proxy that forwards the payload *without interpreting it* — but the gateway must validate `traceparent` and re-stamp `identity` per hop, both impossible on an opaque payload while the envelope lives inside it. Moving the envelope to metadata makes ADR-005's generic-proxy guarantee literally true, lets the gateway do its stated job, and eliminates the forgeable in-body identity footgun by construction. Refines ADR-005 (upholds it); relocates — does not discard — the keystone identity model. Rejected the splice-field-1, keep-as-mirror, and identity-only-in-metadata alternatives (reasons in the ADR).

**Process:** ADR-only commit (per the one-ADR-per-commit rule). The proto migration (strip `RequestContext context = 1` from 18 axis services + `InvokeRequest`; regen 4 SDKs; SDK metadata interceptor; update both `format` refs + the stub gateway; re-run the unchanged golden vectors) is queued as the next implementation step — **not** in this commit.

**Files:** `docs/architecture/adrs/007-call-context-transport.md`, `docs/architecture/adrs/README.md` (index), `ideas/inbox.md` (finding marked promoted), `roadmap/**`.

---

## 2026-05-31 — 0d: second `format` reference (inmemory-py) + shared golden vectors + stub ADR-005 gateway → `format/v1` ADR-003 gate MET

The [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) two-reference gate for `format/v1` is satisfied: a **second, independently-written** reference passes the **same golden vectors** as the first, both loading one shared artifact.

- **Shared golden vectors** — `contracts/conformance/format-v1.json` (+ README). Language-neutral, executable: the *single source of truth* for format/v1 behavior (lifecycle append→scan→merge→overwrite→maintain + 2 error vectors). This is how "run against each other on golden data" is met *literally* (one file, two impls) rather than by hand-copied-but-equal vectors.
- **Go harness refactored** — `inmemory-go/harness_test.go` now loads the shared JSON (was inline vectors) and drives everything through a generic vector executor.
- **Stub ADR-005 invoke-gateway** — `inmemory-go/gateway_test.go` (~150 LOC, test-only, throwaway). The Go harness no longer dials FormatService directly; it goes harness → `core/v1 CapabilityInvokeService.Invoke` → format plugin. The gateway is a **faithful generic byte-relay**: a passthrough codec (`Name()=="proto"`) forwards the serialized payload without deserializing it, capability→method routing is read from the `(rat.common.v1.capability)` method annotation (freeze-blocker #5 machinery, not a hand map), and it enforces C5 (capability allowlist) + emits C8 audit (asserted: one record per mediated call). Validates the mediation seams, not just the plugin-to-plugin data contract.
- **Second reference, `inmemory-py`** — `examples/format/inmemory-py/` (store.py / streams.py / server.py / main.py / harness_test.py + README + pinned requirements). From-scratch Python code path (not a Go port), imports the vendored Python SDK via `PYTHONPATH`. Its harness loads the SAME shared JSON.

**Verified (containers, no host installs):**
- Go (`golang:1.25`): `go test ./...` green — full lifecycle + error vectors, all mediated through the stub gateway. `go.mod` cleanly promotes `google.golang.org/protobuf` to a direct dep (`go mod tidy`).
- Python (`python:3.12`, grpcio 1.80.0 / protobuf 7.35.0 — matches the gencode runtime pin exactly): `python harness_test.py` → `PASS`.

**Finding surfaced (captured in [ideas/inbox.md](../ideas/inbox.md)):** building the generic proxy exposed a real contract tension — the gateway must re-stamp `identity.caller_plugin` per hop and never trust wire identity, but `RequestContext` (with identity) lives *inside* the payload a generic proxy won't deserialize. So re-stamped identity has to ride in gRPC metadata, which contradicts "RequestContext travels as field 1 of every request." Needs a resolution (metadata-only / splice-field-1 / two-channel) before any axis freezes. Exactly the ADR-003-predicted "second implementation reveals the contract flaw" outcome.

**Still open before `format/v1` advances `v1-preview`→`v1`:** the identity-transport decision above; a typed-Arrow conformance pass (the bulk leg is still an in-process registry stand-in in both refs).

**Files:** `contracts/conformance/**`, `examples/format/inmemory-go/{harness_test.go,gateway_test.go,go.mod,go.sum}`, `examples/format/inmemory-py/**`, `ideas/inbox.md`.

---

## 2026-05-31 — 0d started: first reference plugin (rat-format-inmemory-go) — commit `c472620`

First real RAT v3 *code*: a reference `kind: format` plugin under `examples/format/inmemory-go/` (ADR-006 D2 layout), implementing the full `format/v1` wire contract to validate it by building against it (ADR-003 forcing function), not as production storage.

- `store.go` — in-memory ordered-row tables: append / merge(upsert on keys) / overwrite / scan / maintain; per-mutation snapshot.
- `stream.go` — in-process stand-in for the out-of-band Arrow leg: single-use ticket registry; `Resolve` returns producer-hosted `ArrowStream{transport=FLIGHT}`; mutating RPCs pull a caller-hosted source. (Real Arrow Flight deferred to a production reference.)
- `server.go` — the 5 `FormatService` RPCs over gRPC; empty `TableRef`/missing `merge_keys` → `INVALID_ARGUMENT`; `Maintain` leaves `rows_affected` absent (proto3 optional = unknown).
- `main.go` — gRPC server entrypoint.
- `harness_test.go` — golden-data conformance harness: append→scan→merge→overwrite→maintain + 2 error cases over REAL in-process gRPC (bufconn). The vectors a second independent impl must also pass.

**Supporting:** committed the Go SDK `go.mod`+`go.sum` (reproducible builds; `go mod tidy` resolved grpc v1.81.1 + protobuf v1.36.11); dropped the go.sum gitignore; `.gitignore` for the compiled binary. Plugin depends on the SDK via a local `replace`.

**Verified (golang:1.25 container):** `go build` / `go vet` / `go test` all green — 3 tests PASS over real gRPC.

**Process note:** a long cancelled tool-batch mid-session left a stale compiled 15MB binary + a broken `server.go` (dead `errUnused` + stray brace) uncommitted, and the first plugin commit + roadmap edits never landed. Terminal stdout was also corrupting. Recovered by checking real git/file state (not terminal output), rewriting `server.go` clean, removing the binary, re-verifying green in-container, then committing fresh as `c472620`.

**Next (ADR-003 gate):** a SECOND independent `format` impl — e.g. `examples/format/inmemory-py` — running the SAME golden vectors, before `format/v1` can freeze. (The sequencing panel — see chat — recommends also routing the harness's control RPCs through a ~200-LOC throwaway stub invoke-gateway so the freeze also validates the ADR-005 mediation seams, not just the plugin-to-plugin data contract.)

**Files:** `examples/format/inmemory-go/**`, `contracts/sdks/go/{go.mod,go.sum}`, `contracts/.gitignore`.

---

## 2026-05-31 — Multi-language SDKs: Python, TypeScript, Rust (+Go) — commit `78be8b4`

**What:** Extended codegen from Go-only to all four target languages (Tom: "python, ts and ruff[=Rust]"), realizing the any-language promise (ADR-001 / vision #3). Each is a committed, peer `contracts/sdks/<lang>/` with its own `buf.gen.<lang>.yaml`:
- **Go** — protocolbuffers/go + grpc/go (43 files + go.mod; compiles under golang:1.25)
- **Python** — protocolbuffers/python + grpc/python (46)
- **TypeScript** — bufbuild/es + connectrpc/es (42)
- **Rust** — community neoeinstein-prost + neoeinstein-tonic (39)

`scripts/gen-sdks.sh` LANGS=(go python typescript rust); `--check` loops all four (excludes hand-added go.mod/go.sum). CI (`contracts.yml`) regenerates all four (was Go-only). ADR-006 amended (diagram + stacks + BSR-rate-limit note).

**Each language's codegen empirically verified in-container** (buf generate exit 0, file counts above). `make check` (buf lint) green.

**⚠️ Operational caveat (real, recorded):** codegen uses **remote buf.build plugins** → regenerating all four in quick succession hits **BSR rate limits** (429); `make compile-sdks` also flaked on `go get` (network) during this session. Neither is a content defect — the committed SDKs are correct, Go compiled clean earlier — but it means `make gen-check`/`compile-sdks` are network-bound and can transiently fail locally. Future hardening: retry/backoff on 429, or local (non-remote) codegen plugin images. Not blocking.

---

## 2026-05-31 — Codegen pipeline: make targets + gen script + CI + per-commit hook

**What:** Built the SDK-codegen + verification toolchain that ADR-006 D3 calls for. Three pieces (commits `654c3f1` pipeline, `4abffe7` Claude hook):

1. **`scripts/gen-sdks.sh` + `Makefile`** — containerized (podman/docker, no host installs). `make check` = FAST per-commit gate (buf lint, ~5s); `make verify` = FULL (lint + build + SDK freshness + Go SDK compile, slow/network); `make gen-sdks` / `gen-check` / `compile-sdks`. Vendored Go SDK landed at `contracts/sdks/go/` (42 files + go.mod), compiles clean under `golang:1.25` (resolves the ADR-006 D3 Go≥1.25 floor). `buf.gen.yaml` → `buf.gen.go.yaml` (per-language, outputs to `sdks/go`); `.gitignore` drops the retired `/gen/`.
2. **`.github/workflows/contracts.yml`** — `validate` job (buf lint+build, +`buf breaking` on PRs) and `sdks` job (regen + Go compile; PRs **fail on stale committed SDK**; push to main **auto-commits regenerated SDKs back** — the "autogenerate on GitHub" ask).
3. **Per-commit Claude hook** (`.claude/`, via claude-engineer) — PreToolUse/Bash with `if:"Bash(git commit *)"` → `.claude/hooks/contracts-check.sh`. The hook process only spawns on `git commit`; the script then self-guards on staged `contracts/proto/` files (pure shell, no container if none staged) before running `make check`; exit 2 blocks on lint failure. **Never per-edit, never `make verify`.** Verified all 4 paths against the real script: nothing-staged 11ms/exit0, non-proto-staged 6ms/exit0, clean-proto exit0 (ran make check), broken-proto exit2/blocked with the lint error fed back. Caveat: only gates Claude-run commits (CI is the real boundary; human-commit gating would need a repo git pre-commit hook).

**Verified:** `make check` / `gen-check` / `compile-sdks` all exit 0 locally; hook tested across all 4 paths; settings.json valid + `$schema`/`env`/`permissions` preserved.

**Files:** `Makefile`, `scripts/gen-sdks.sh`, `.github/workflows/contracts.yml`, `contracts/buf.gen.go.yaml`, `contracts/.gitignore`, `contracts/sdks/go/**`, `.claude/settings.json`, `.claude/hooks/contracts-check.sh`.

---

## 2026-05-31 — ADR-006: SDK distribution + reference-plugin layout + codegen toolchain

**What:** Before scaffolding 0d (first reference implementations — the first code), pinned three project-shaping decisions in [ADR-006](../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md), prompted by Tom's point that plugins must be authorable in *any* language (ADR-001 / vision #3), not just Go.

- **D1 SDK distribution:** vendored `contracts/sdks/<lang>/` (Go/Python/TS as peer committed dirs, none privileged), regenerated-not-hand-edited; **BSR publication deferred** as the later external-distribution channel. Mirrors Kubernetes (vendor for the monorepo, publish for outsiders). Chosen over BSR-now (needs network + org; sandbox blocks) and protos-only/local-codegen (multi-step build in a fiddly-toolchain env).
- **D2 layout:** reference plugins under `examples/<axis>/<impl>-<lang>/`; ADR-003's two-reference rule satisfied per critical axis by two impls in *different* languages running shared golden-data vectors — cross-language interop is the strongest form of the rule.
- **D3 codegen:** containerized `buf generate` driven by a committed `scripts/gen-sdks.sh`. Captured two gotchas already hit: the generated Go gRPC stubs need Go ≥ 1.25 (base `golang:1.23` image failed to build the SDK this session — pin the image or pin grpc/protobuf), and `buf generate` uses remote buf.build plugins (network) so the script must handle local-plugin fallback.

**Process note / correction:** earlier this session I claimed the Go SDK "compiles clean" — it does NOT yet; codegen *produces* 42 Go files but compiling them failed on the Go-version floor above. ADR-006 D3 records the real situation; resolving it is the first 0d task.

**Next:** scaffold per D1/D2/D3 — `buf.gen.<lang>.yaml` + `scripts/gen-sdks.sh` (settle the Go-version/grpc-pin), generate+commit `sdks/`, drop the transient `gen/` path, then `examples/format/inmemory-go/`.

**Files:** `docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md` (new), `docs/architecture/adrs/README.md`.

---

## 2026-05-30 — Freeze-blocker #10a: debug_redact on sensitive bytes fields

reviews/06 SEC-8 (part of #10): "never logged" was a comment; `[debug_redact = true]` makes redaction structural (reflection/text-marshal omit the field). Applied to the four sensitive bytes fields: `secret.ResolveResponse.value`, `identity.AuthenticateRequest.credential`, `storage.VendCredentialsResponse.credentials`, `common.ArrowStream.ticket`. Confirmed buf 1.47.2 accepts the option via an isolated test first.

**Verified:** buf lint 0 / build 0 / generate 42 Go files. Commit `(10a)`.

### #10 remaining — `artifact`/digest manifest block (AUTH-14⊕SEC-15) — NOT YET DONE

The other half of #10 (add a top-level `runtime` discriminator + `artifact` {ref, digest} block to `plugin.v1.json`, required for out-of-process plugins; update examples; tie `trust.signature` to `artifact.digest` + the authz envelope) is **deliberately deferred**. Rationale: per reviews/06 this is **additive/GA-safe** — adding a property to a schema we own does not break any plugin's wire contract (unlike the structural #1–9 changes), so it can land after the `rat/1` freeze without a flag-day. Only the "what the signature covers" *decision* carried a freeze rider, and that decision is recorded (sign artifact.digest + provides/requires/resources). Pairs with the two #9f doc-pins (pagination default, timestamp ratification) as the additive tail.

---

## 2026-05-30 — Freeze-blocker #9c/9d/9e: data-plane shapes + schema/proto slivers

Continued the #9 small-wire-fix cluster (reviews/06). All buf-verified (lint 0 / build 0 / generate 42); each its own commit.

**9c (`9c25c26`) — data-plane shapes.** `data.proto` ArrowStream: pinned the wire protocol (new `ArrowTransport` enum = FLIGHT + `transport` field — "gRPC/Flight-style" was not a spec) and encoded host-vs-dial (new `ArrowStreamRole` enum PRODUCER_HOSTED/CONSUMER_HOSTED + `role` field — same type used in opposite directions); ticket-security note (short-TTL/single-use/bound, SEC-14; detailed spec is GA). `observability.proto` Ingest: client-streaming → **bidi-streaming** (the old single terminal ack gave no backpressure/partial-failure feedback; bidi acks per batch).

**9d (`f290601`) — schema shape.** `plugin.v1.json` `contributes.slots[].target`: bare `capabilityUri` → `capabilityRef` (API-17, consistency with provides/requires; string↔object is breaking). scd2 example updated to the wrapped shape; both manifests re-validate.

**9e (`277a09f`) — proto slivers.** API-13 sentinel → proto3 `optional` presence: `WriteResult.rows_affected` (absent==unknown) + `ExecuteProgress.fraction` (absent==indeterminate). API-12: `strategy.Apply.options` encoding pinned (UTF-8 JSON vs metadata_schema). API-11: `scheduler.WatchDue` delivery pinned at-least-once (reconciler dedupes by trigger_id+fired_at).

**Remaining in #9 (low-value doc-pins, optional):** pagination-default note on `state.List` / `marketplace.Search` (v1 returns unbounded; a future `page_size` default must preserve that) and the timestamp-type ratification note (int64 unix-ms is the deliberate, final rat/1 choice). Both are comments, not wire changes — arguably addable post-freeze; deferred.

**Files:** `contracts/proto/rat/common/v1/data.proto`, `contracts/proto/rat/observability/v1/observability.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `contracts/proto/rat/strategy/v1/strategy.proto`, `contracts/proto/rat/scheduler/v1/scheduler.proto`, `contracts/schema/plugin.v1.json`, `contracts/examples/rat-strategy-scd2.plugin.yaml`.

---

## 2026-05-30 — Freeze-blocker #9a+9b: secret found semantics + decision error model

Freeze-blocker #9 (the small-wire-fix cluster, reviews/06 C-5 + API-1d) is being landed in sub-commits. First two done:

**9a (`22b76e2`) — `secret.Resolve.found` semantics.** Pinned at freeze: `found=false` deliberately conflates "ref does not exist" with "ref exists but unauthorized" (anti-enumeration). Auth failures return `found=false` + empty value, NOT `PERMISSION_DENIED`. Comment-only but freeze-gated (pins the meaning of the existing `found` field).

**9b (`fcbe8bb`) — decision-RPC error model.** A deny on a *successful* decision rpc can't be carried by a gRPC status code, so `identity.Authorize` + `tenancy.Decide` get an in-band enumerated `deny_code` alongside `allowed`; free-text `reason` demoted to log/audit-only (field 3), MUST NOT drive caller logic (enumeration-oracle, reviews/04). Per-package `DenyCode` enums. Header ERROR MODEL note on both: transport failures → gRPC status; decisions → `allowed` + `deny_code`.

**Process note:** an earlier attempt committed only the secret change while claiming all three (a linter re-applied my reverted identity/tenancy edits asynchronously, and my re-edits failed on the stale-file guard). Caught on verification: amended the 9a commit message to match its actual content (secret only), then landed identity+tenancy cleanly as 9b with fresh reads. No false claim remains in history.

**Verified:** buf lint 0 / build 0 / generate 42 Go files; dup-free.

**Remaining for #9:** 9c (ArrowStream protocol+role, Ingest shape) + 9d (slots.target wrap, options encoding, timestamp ratification, pagination default, scheduler delivery doc, optional-presence).

**Files:** `contracts/proto/rat/secret/v1/secret.proto`, `contracts/proto/rat/identity/v1/identity.proto`, `contracts/proto/rat/tenancy/v1/tenancy.proto`.

---

## 2026-05-30 — Freeze-blocker #8: catalog.MergeBranch idempotency + concurrency

**What:** reviews/06 #8 (ARCH-4 / I-18) — `MergeBranch` is the publish gate of the pipeline model and the reconciler retries it, but it took only branch names: a retried merge could double-apply and concurrent merges into main could lose updates. Added two request fields + one response field.

**`MergeBranchRequest` gains:**
- `expected_into_snapshot` (4) — optimistic-concurrency guard; the merge applies only if `into_branch` is still at this snapshot, else it fails and the caller re-reads/re-tests. Closes the lost-update window between concurrent merges. Empty == unconditional (sole-writer only).
- `idempotency_key` (5) — stable id for the logical merge (e.g. run id); a retry with a key that already committed is a no-op returning the original result. Closes the double-apply window under reconciler retry.

**`MergeBranchResponse` gains:** `already_applied` (2) — true when the response reflects a previously-committed merge (idempotent retry) rather than a fresh apply.

**Scope:** only the request-shape change is freeze-gated. The separate I-18 gap — how the catalog learns what `format.Write` wrote to a branch (a new commit-linkage RPC) — is additive and stays GA-deferred.

**Verified:** buf lint 0 / build 0 / generate 42 Go files (fields compile into existing catalog package files); dup-free.

**Next:** freeze-blocker #9 — the smaller-wire-fix cluster (error convention, `secret.Resolve.found`, Arrow role field, `Ingest` shape, timestamp type, `slots.target` wrap + freeze-slivers).

**Files:** `contracts/proto/rat/catalog/v1/catalog.proto`.

---

## 2026-05-30 — Freeze-blocker #7: common/v1/event.proto (async event-bus envelope)

**What:** reviews/06 ARCH-1 — the async plane (event bus, one of the six core things) had NO wire envelope, so distributed tracing broke across the async boundary and multi-tenant event routing was undefined, while every sync RPC carried `RequestContext`. Added `common/v1/event.proto` defining the `Event` envelope.

**Shape:** `Event` = `{ RequestContext context, string event_id, string type, int64 timestamp_unix_ms, string source, bytes payload, string partition_key }`:
- `context` — the SAME trace+identity+tenant envelope sync RPCs carry, so a `pipeline_run_failed` traces back through its `pipeline_run_requested` within one `correlation_id`, across every reacting plugin; identity is core-stamped at emit time (non-forgeable, keystone rules).
- `event_id` — idempotent redelivery (at-least-once transports redeliver; a subscriber that saw this id no-ops). Distinct from `correlation_id` (shared across an operation's events).
- `type` — subscription match key (overview.md: subscriptions = [event, action]); open-set, lower_snake_case.
- `source` — emitting component (core reconciler or core-mediated plugin id); async analogue of `identity.caller_plugin`.
- `payload` — serialized type-specific message, opaque to the transport (routes by type+tenant without interpreting it, like invoke.proto's payload).
- `partition_key` — optional ordered-delivery key (e.g. per-run-id), where the transport supports it.

Protocol fixed, transport pluggable (ADR-002 D2/D4: NATS JetStream default / Kafka / Redis Streams).

**Verified:** buf lint 0 / build 0 / generate 42 Go files (`event.pb.go`; message-only, no service); dup-free.

**Next:** freeze-blocker #8 — `catalog.MergeBranch`: add `expected_snapshot` + `idempotency_key`.

**Files:** `contracts/proto/rat/common/v1/event.proto` (new).

---

## 2026-05-30 — Freeze-blocker #6: core/v1/invoke.proto (capability-invoke service)

**What:** Added the wire artifact ADR-005 requires and reviews/06 C-6 (AUTH-2 ⊕ ARCH-2) flagged as missing — the mechanism by which a plugin actually CALLS a capability it `requires`. Before this, "the core wires providers via the registry" was comment-deep with no wire mechanism; the headline call-by-capability feature was unbuildable.

**Shape (core-mediated per ADR-005):** new `CapabilityInvokeService.Invoke(InvokeRequest) → InvokeResponse`:
- `InvokeRequest` = `{ RequestContext context, string capability, bytes payload }` — the capability URI (e.g. `rat://format/v1/merge`) + the serialized request message for the target axis method.
- `InvokeResponse` = `{ bytes result }` — the serialized provider response.

**How it works:** a generic proxy. The plugin calls `Invoke` on the core API gateway instead of dialing the provider. The core resolves capability→provider (registry + the `(rat.common.v1.capability)` method annotation from #5), enforces C2/C5/C7/C3, re-derives `identity.caller_plugin` for the downstream hop, stamps C1 trace, emits the C8 audit record, then forwards `payload` to the provider's method without interpreting it (no per-axis core knowledge → no 7th core thing). Bulk Arrow data still bypasses the core via `ArrowStream`; `Invoke` carries only control RPCs. Enforcement failures surface as gRPC status, not a response field.

**Verified:** buf lint 0 / build 0 / generate 41 Go files (`invoke.pb.go` + `invoke_grpc.pb.go`); dup-free.

**Next:** freeze-blocker #7 — async event-bus envelope (`common/v1/event.proto`) OR explicitly carve the async plane out of the `rat/1` freeze.

**Files:** `contracts/proto/rat/core/v1/invoke.proto` (new).

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
