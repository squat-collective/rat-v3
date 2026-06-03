# Backlog — queued but not started

Work that's been identified (from reviews, ideas, conversations) but isn't actively in flight. **This isn't a wish list** — every entry should be specific enough that the next Claude session knows what "starting" it means.

When an item moves to active work, promote it: cut it from here, add it to [current.md](current.md), update [phases.md](phases.md) status if applicable.

---

## 🛰️ `rat serve` — make the sealed core runnable ([ADR-019](../docs/architecture/adrs/019-rat-serve-daemon.md), **Accepted** 2026-06-02 → now the active next build, see [current.md](current.md))

> **Promoted to active.** ADR-019 is Accepted and written to be executed cold (Implementation map + runbook + kickoff checklist; all 7 decisions resolved). The detail below is retained as the at-a-glance summary; the authoritative spec is the ADR.

**Surfaced by the data-dev-plane experiment** (finding F9 + the "why not use the core gateway?" thread): the Phase-1 core is built+tested but is a **library, not a server** — no entrypoint, the gateway is only served over `bufconn` in tests. So the experiment uses a BFF stand-in. `rat serve` assembles the sealed core into a daemon.

The assembly already exists (`supervisor.BringUp → Plane{Gateway,Registry}`, `reconciler.Loop`, `deploymentruntime`, `Plane.Shutdown`); what's missing is **glue**: a `core/cmd/rat` entrypoint, a `plane.yaml` config loader, a **TCP listener** for the gateway (`RegisterCapabilityInvokeServiceServer` + `net.Listen`), signal-driven lifecycle, and wiring the reconcile loop.

Two runtime modes (same daemon): **launch** (the daemon launches+supervises plugins via the deployment-runtime — the `./rat serve` solo path) and **attach** (the daemon dials already-running plugins by `endpoint:` — what the compose stack uses, so **no docker-in-docker**; `gateway.New` already takes external providers).

- **✅ Phase A (MVP) — DONE 2026-06-02** (`phase-1-adr-019-rat-serve`): `rat serve --plane plane.yaml` boots the `stateplugin` via the local-process runtime, serves the gateway on TCP; a client routes a capability (C5 allow + audit), an undeclared one is denied (C5 deny + audit), SIGTERM drains. `core/cmd/rat/` + `make core-serve-smoke`. *First time the core runs as a server.*
  - **🆕 Finding (deferred here):** wire the **reconciler crash-restart loop** into the daemon. Blocked on a small additive core change: `gateway.New` fixes the provider-conn map at construction (no re-bind setter), and `supervisor.BringUp` already launched the desired set — so running the reconciler over the same set double-launches, and a reconciler-relaunched plugin lands on a new endpoint the gateway can't re-dial. Needs a `gateway.SetProvider`/adopt path (additive, concurrency-safe) before the daemon can self-heal a crashed plugin. Phase A is boot-once+serve+drain without it.
- **Phase B:** **containerize the Python data-dev plugins** (the launch contract execs `image` directly, no args) → a `data-dev-plane.yaml` → `rat serve --runtime podman` runs the real ML lakehouse under the **actual core gateway**; the UI's control path becomes the real gateway (TS SDK), the BFF shrinks to the F9 data-leg only.
- **Phase C (beginner compose stack):** a **daemon container image** + `deploy/data-dev-starter/compose.yaml` bringing up rat-serve (attach mode) + **base plugins** (engine/catalog/storage/strategy) + **MinIO + Postgres** → `compose up` gives a newcomer a working data-dev plane in one command. The on-ramp end of "same binary, solo → cloud" (ties to EC-1).

**Starting it = ratify ADR-019** (7 open questions: default runtime; Python-plugin launch incl. a possible additive `LaunchSpec.args` — frozen-wire check; auditor sink; binary location vs the `rat/2.0` seal; phase placement vs the user-pull gate; attach-mode supervision; the base-plugin set + compose-stack location). Then build Phase A against the existing test plugins.

---

## Board-review findings ([reviews/08](../reviews/08-post-freeze-board-review.md), 2026-06-01)

The 5-agent post-freeze review. Grouped by when to act.

> **⏩ Now ACTIVE (Phase 0 close-out, chosen 2026-06-01 — see [current.md](current.md)):** ~~**B1** catalog commit-linkage~~ **✅ DONE ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))** + ~~**E2** manifest freeze + per-kind schemas~~ **✅ DONE ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md))**; ~~the **doc tail** (E4 `overview.md` drift + E1 the 12 missing CONTRACT.md + E3/E7)~~ **✅ DONE 2026-06-01**; **NEXT** is the **`v1.1` cut** (item 4 — tag the sealed surface), then **Phase 1**. The items below stay queued: the rest of the `v1.1` additives land opportunistically with that cut, and the **enforcement-layer findings are Phase 1 acceptance criteria** (see phases.md Phase 1).

**NOW — the freeze is still local/unpushed (closing window):**
- ~~**A1 [V2-REGRET]** — `WriteResult.snapshot_id` `optional` + re-cut `rat/1`.~~ **✅ DONE 2026-06-01** (commit `0e81314`; `rat/1` re-cut from `b9dbe2d`). The one V2-regret is resolved, not carried to a v2.
- ~~**D5/E4 honesty banner** on `plugin.v1.json` + `CONTRACT.md`.~~ **✅ DONE 2026-06-01** (`0e81314`). *Residual:* the `overview.md` drift (`plane-manager-plugin`→`deployment-runtime`; tier-0 callout; "core never commands") is still TODO — tracked as E4 in the process list below.
- ~~*(Not absorbed — moved to `v1.1`:)* the additive crash-safety fields **C1** + **C2**~~ **✅ DONE 2026-06-01 (folded into the `rat/1.5` cut, [ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields.md))** — `idempotency_key` + `already_applied` on the write path, `expected_rows`/`expected_batches` on `ArrowStream`; demonstrated in the composition. Per-axis conformance vectors → Phase 1.

**`v1.1` additive (no break; prioritized):**
- ~~**B1** — catalog `RegisterTable` + commit-linkage RPC.~~ **✅ DONE 2026-06-01 ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))** — additive `RegisterTable` + `CommitTable` on `catalog/v1`; the create→write→register→merge loop now closes on-wire (composition no longer seeds tables out-of-band). 32/32 + composition green. Resolves R3.
- ~~**C1/C2**~~ **✅ DONE 2026-06-01 (ADR-012)** — effect-leg idempotency key + `ArrowStream` completeness, folded into `rat/1.5`. **C4** terminal audit record (`outcome` at stream close; `AUDIT_OUTCOME_ERROR` already exists) — remains for Phase 1.
- Enrichments: structured `IsolationAttestation` (D1), signed **conformance attestation** message so `conformed_capabilities` is derived not self-asserted (D4), `health/v1` liveness/readiness probe the reconciler drives (sre#4), `WriteResult` insert/update/delete breakdown + `TableRef` snapshot_id/as_of (F2), `bound_capability` on `SubjectAssertion` (F1).

**Enforcement-layer / conformance (some need the core — specify now):**
- **C3** core MUST bound the provider call by `min(channel, deadline_unix_ms)` + streaming idle-timeout. **~~D1~~ ✅ DONE 2026-06-01** — full-isolation-profile vector + a *real* enforcing deployment-runtime (podman, kernel-enforced; `make core-test-podman`) — see [done.md](done.md). **D2/D3** `ArrowStream`-ticket TTL/single-use/cross-tenant vector · real-scoped-cred storage integration vector. **D4** marketplace verifies the attestation. **sre#4** crash-loop backoff + jitter **(→ promoted to an explicit Phase-1 exit gate — [reviews/09](../reviews/09-phase-1-gate-review.md))**. **sre#8** pin a core SLI list + `/metrics` contract while the core is still paper.

**Process / spec (cheap):**
- ~~**E3** label round-1 refs~~ **✅ DONE 2026-06-01** (13 `inmemory-py` READMEs). **E5** `ERROR_MODEL.md`: add `CANCELLED`/`ABORTED` (streaming), pin `TableRef.branch` vs per-RPC `branch` precedence, pin BidiStream empty-first-frame abort. **E6** state engine output-type stability is the caller's responsibility (SUM→CAST). ~~**E7** add the temptation ledger~~ **✅ DONE 2026-06-01** (pinned in `done.md`, count 0). **E8** make C2/mTLS a deployment-conformance item + document the audit keyring trust-root/rotation + tail-drop detection. **F3** secret timing-equivalence note. *(E5/E6/E8/F3 remain — spec polish, fold into the `v1.1` cut or Phase 1.)*

**Accept as documented v1 residual:** F1 (R1 bounded confused-deputy — M4 tenant cross-check confirmed fixed), F2 enrichments, engine SQL portability is inherent.

---

## Q02 simulated dry-run findings ([reviews/Q02-tracker.md](../reviews/Q02-tracker.md) synthesis, 2026-06-02)

Deduped from the 5-agent simulated panel (`reviews/11-q02-*.md`). **Authoritative triage = the maintainer's net-new-vs-already-tracked split** in [11-q02-maintainer-defense-log.md](../reviews/11-q02-maintainer-defense-log.md). **⚠️ Simulated (AI personas) — a real external review is still owed; treat these as a pre-validated punch-list, not the last word.** None is a *hard* freeze-reopen; the wire held.

**① Pre-unfreeze punch-list — resolve BEFORE the freeze is ever published (cheap now, expensive after):** *(→ a single "pre-unfreeze punch-list" ADR)*
- **PU-1 (High; soft freeze-reopen; 2 lenses)** — add a normative conformance MUST to `data.proto` `ArrowStream`: a producer MUST verify the presenting channel's authenticated identity == the ticket-bound `{caller,tenant}` (transport/app headers insufficient) + a wrong-channel/right-header conformance vector. The bytes leg bypasses the core, so C2 can't reach it; the ref trusts `X-RAT-*` headers (`bulkleg_test.go:39`). *Starting it = write the MUST + vector; ship channel-auth as the SDK default.*
- **PU-2 (High; conformance debt)** — give the keystone `common/v1/context.proto` + gateway-stamping contract its own context-carriage conformance suite + a 2nd independent gateway cross-run (ADR-003 rigor it skipped). Today: one impl, no portable vector (ADR-007 §Neutral). *Qualifies ADR-015's "freeze validated" → narrow it to "validated on the data axes the spike exercised."*
- **PU-3 (Med; soft freeze-reopen)** — add `expires_at` + a revocation reference to the conformance attestation / marketplace shape; design revocation + scoped/threshold authorities (no single forge-oracle key). Today `Conforms` is static set-membership = "conformed forever."
- **PU-4 (Med-High; soft freeze-reopen / decide-now)** — **decide:** v1 tenancy is **isolation-only** (mark `DECISION_KIND_SHARING` advisory-not-enforced — cheap) *or* **sharing-capable** (add a delegation/grant primitive to `state`/`storage` in `rat/1` before publish — retrofitting cross-tenant semantics later is the expensive v2). `DECISION_KIND_SHARING` is currently decidable-but-un-actionable on flat-string keys.
- **Decide-the-additive-now seams** (the additive door closes at publish): semantic-field-skew negotiation — the ADR-012 crash-safety fields shipped as plain fields with no negotiation handle → silent double-apply on version skew; decide capability-URI-per-behavior *or* an additive `requires[].min_revision` (architect F2 / maintainer A1). · `Event` signing — mirror the signed `AuditRecord` on `Event` (unsigned identity in-body through a pluggable bus today) (architect F7). · split `vend-credentials` into read/write capability URIs (security F6).

**② Multi-tenant availability cluster** — core-impl (no wire change); gates any real multi-tenant use:
- **AV-1 (🔴 Critical; close FIRST — free now)** — `core/lease` `Store.Renew/Acquire` return `(ok, err)`; "renewal-error ≠ lease-lost" (hold leadership through transient backend errors until genuine local-TTL expiry; `state.PutOutcome.UNKNOWN` already models it on the wire). Add a test that injects an erroring `Renew`. A breaking refactor once a durable backend binds the `bool` interface.
- **AV-2 (High)** — map the already-frozen `LaunchSpec` `limits` → `--memory`/`--cpus`/`--pids-limit` in `podman.go` *and* `localprocess.go` (both drop them today); reject limit-less launches in multi-tenant mode; add a "limit exceeded → contained" vector.
- **AV-3 (High)** — bound the reconciler's runtime RPCs with per-call deadlines; give `Status()`/`Endpoint()` a read path that doesn't share the reconcile mutex (one hung `Healthcheck` pins all tenants + blinds Status today).
- **AV-4 (High)** — `Degraded` → capped-infinite-retry (cap the *interval*, not attempts) + a `Reset/Resume` path; **emit an event/metric on every state-transition edge**. Today it's a silent terminal black-hole (`reconciler_test.go:118` codifies it).
- **AV-5 (Med-High)** — add seccomp to `checkI9Minimum`: refuse `unconfined`/weaker-than-RuntimeDefault (the runtime should impose the max, not honor a caller-supplied weaker value).
- **AV-6 (High)** — Arrow ticket: per-producer key + `key_id`/rotation (mirror the conformance keyring) + a shared/durable single-use store (the lease's state-backend CAS) so restart/replica doesn't reopen replay; or bind tickets to a channel/cert fingerprint.
- **AV-7 (Low-Med)** — `noexec` on `/tmp`+`/data`; map `/data` to the plugin uid instead of `0o777`.

**③ Tier-0 / observability / selection / discipline:**
- **T-1 (High)** — design + document the state-backend **degraded mode** (serve last-known-good reads / refuse only writes when the backend is unreachable; pair with AV-1) and **build the real state-backend read path** (the spike reconciler reads a *fixed* slice → the "always re-read state" guarantee is unexercised); specify the bootstrap-seat **recovery** leg (seat crash/restart + re-attach to running plugins).
- **O-1 (Med-High; sharpens sre#8)** — emit a counter/event on every reconcile state-transition edge **now** (it's what AV-4's alert keys off), and pin the `/metrics` golden-signal list + an SLO doc while the core is still paper.
- **O-2 (Med; already-tracked, re-prioritized)** — pull **upgrade/version-skew** forward (partial upgrades are the *normal* case for a polyglot plugin mesh): a kubelet/apiserver-style N/N-1 policy + `rat preflight` orphan check + dual-advertise rollout; make desired-state git-first to bank half the DR win.
- **P-1 (Med)** — name the **plane/pipeline/binding desired-state language** (where provider *selection* happens) as a first-class contract artifact; document that capability negotiation resolves *eligibility* while plane bindings resolve *selection* (today the selection layer is unspecified + outside the contract triple).
- **K-2 (process)** — before each *real-backend* reference lands (Iceberg/Nessie/postgres), run an explicit **omission-audit** ("what loop can this backend complete that the in-memory refs faked?") — the freeze gate is structurally blind to omission (ADR-010/B1 is the existence proof, not the last case).
- **D-1 (discipline)** — add an **enforcement-layer obligation count** to the temptation ledger (the gateway already performs 6–8 enforcement jobs on one hop while "the core stays six" stays literally true; the metric can't see enforcement-layer accretion — the K8s apiserver lesson).

**④ Ecosystem on-ramp** — GTM/impl (no wire change); some are cold-start-critical and **cannot wait for Phase 4**:
- **EC-1 (🔴 Critical-cold-start; P1)** — co-locate a real `plugin.yaml` in every `examples/<axis>/<impl>/`, ship the ADR-006-D2-promised **`examples/README.md`** (currently missing — a doc-drift regression), and pull a thin `rat plugin validate <dir>` forward (the JSON Schema already exists). Today there's no walkable `git clone → running plugin` path.
- **EC-2 (High)** — document a `rat dev` localhost-attach inner loop; **decide whether launch metadata (`image`/`command`) is a manifest field** (additive — decide before authors hard-code the out-of-band convention) or operator-config; reconcile with ADR-016 ("builds a LaunchSpec from the manifest").
- **EC-3 (High; P1)** — build the conformance **issuance pipeline** (runner → signer → publish; `conformance.Sign` is test-only today) + a marketplace install-time attestation check; until then render marketplace `conformed_capabilities` as *unverified/self-declared*, and fix the `plugin.v1.json:6` honesty-banner-vs-`verified.go` drift. *(Core-load D4 enforcement IS built — do not re-flag it.)*
- **EC-4 (High-GTM)** — name **one wedge axis** (`format`/`catalog` on the Iceberg/Delta tailwind) and write the first 3 third-party plugins *with* design partners; treat that as the product, not the 19th axis.
- **EC-5 (Med)** — publish a **governance commitment** before recruiting external authors: who may propose a `rat://` axis/capability, how a community plugin contests a first-party one, the trust-root model for conformance authorities (plural/federated?), and a contract + reference-marketplace relicense pledge.
- **EC-6 (Low)** — a "versioning for authors" CONTRACT.md section (two version axes + `compatible_core`); consider a manifest sidecar resolvable without re-imaging (vs D6's integrity guarantee). Plus the architect F8 doc fix: regenerate `overview.md`'s manifest example from a real validating `contracts/examples/*.plugin.yaml`.

---

## Remaining Phase 0 tail (post-`rat/1`)

The data-plane is frozen ([ADR-009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md)). What's left to fully close Phase 0 — all loosely-coupled, none blocking the frozen surface:

- ~~**`strategy/v1` second reference**~~ → **✅ DONE (2026-05-31, `rat/1.1`)** — `examples/strategy/scd2-py` (SCD2) is the divergent second strategy; `strategy/v1` is frozen at `v1`. A *third* strategy (soft-delete) is optional hardening, not required.
- ~~**Control-plane axis references**~~ → **✅ DONE (2026-05-31, `rat/1.2`)** — 7 axes (identity/secret/scheduler/tenancy/billing/observability/audit-log) referenced + frozen; conformance 27/27.
- ~~**`deployment-runtime` reference**~~ → **✅ DONE (2026-05-31, `rat/1.3`)** — two divergent refs (local-process + k8s-dryrun) + freeze; conformance 29/29.
- ~~**Experience-axis references**~~ → **✅ DONE (2026-05-31, `rat/1.4`)** — ui/notifications/marketplace referenced + frozen. **🎉 ALL 18 axis contracts are now `v1`.**
- ~~**Manifest schema freeze** (`plugin/v1`)~~ → **✅ DONE 2026-06-01 ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md))** — `plugin.v1.json` frozen at `v1` + 18 per-kind schemas (`schema/kinds/`) + `make validate-manifests` gate.

## Post-`rat/1` residuals (accepted into `v1`, tracked for GA)

From the freeze review ([reviews/07](../reviews/07-freeze-review.md)); all additive or bounded, none wire-breaking:

- **R1** — `SubjectAssertion` bound to the operation (`correlation_id`), not hop/capability: bounded confused-deputy (blast radius = the operation's C5-declared capability set). Revisit if finer user-presence proof is needed.
- **R2** — storage `VendCredentials` tenant-scoping is a per-impl property (ADR-005 bearer exception; core can't inspect an STS blob).
- **R3** — additive niceties: ~~catalog create-table / commit-linkage RPC (the composition surfaced this — harness seeds tables out-of-band)~~ **✅ catalog commit-linkage RESOLVED 2026-06-01 ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))**; remaining: watch `caught-up` bookmark, `Event.schema_version`, `ArrowStream` termination signal, `MergeBranchResponse` no-op-vs-replay disambiguation, `TableRef.branch` vs per-RPC `branch` precedence. All additive post-freeze (new RPC/fields/enum values).

---

## ✅ DONE — `(rat.capability)` rolled across ALL 18 axes

**✅ DONE 2026-06-01 (with [ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md), as the enabling step for the per-kind schemas).** The 6 data-plane axes were annotated during 0d (2026-05-31); the remaining **12** control/experience axes (strategy, identity, tenancy, deployment-runtime, scheduler, secret, observability, audit-log, ui, notifications, marketplace, billing) were annotated 2026-06-01 — additive (`buf breaking` FILE clean), SDKs regenerated. Every axis method now carries `option (rat.common.v1.capability)`, so the C5 gateway + C6 conformance (Phase 1) have the machine-readable capability map they need. Source: reviews/06 I-4 (AUTH-9).

---

## 0d/0e round-2 — a technologically-divergent reference per data-plane axis (real backends)

The current 0d references are **inmemory twins** (`inmemory-go` + `inmemory-py`): two independent *code paths* in two languages that pass one shared golden-vector file. That validates the **wire contract** (proto shapes, RPC cardinalities, error model, the `rat-callmeta-bin` envelope, gateway mediation) and the cross-cutting machinery — and it's where ADR-007 + ADR-008 came from. But it is the **weak form** of ADR-003 "independence": both impls use the same underlying tech (a hashmap), so they cannot surface the **orthogonality-assumption / semantic-divergence** failures ADR-003 targets ("this only worked because both used the same Arrow dialect"; snapshot-isolation vs CAS; serializable vs eventual).

**Round 2 (before any data-plane axis → `v1`):** make one reference per axis a *real divergent backend* with a different consistency/semantic profile. Cheapest + highest-value first:
- **`state` = sqlite — ✅ DONE 2026-05-31** (`examples/state/sqlite-py/`): passes the shared vectors + adds round-2 tests for DURABILITY (survives reopen) and LINEARIZABLE CAS (16 threads → exactly one winner, enforced by `BEGIN IMMEDIATE`, not a mutex). Establishes the round-2 pattern: real backend + same vectors + a backend-specific semantic test.
- **`storage` = local-fs — ✅ DONE 2026-05-31** (`examples/storage/localfs-go/`): passes the shared vectors (now provider-neutral logical prefixes) + adds round-2 tests for PATH CONTAINMENT (escaping prefix → `PERMISSION_DENIED`, dir created on disk) and TENANT ISOLATION (two tenants → distinct paths). The cross-tenant boundary, enforced by `filepath` resolution rather than convention.
- **`catalog` = sqlite — ✅ DONE 2026-05-31** (`examples/catalog/sqlite-py/`): passes the shared vectors + adds round-2 tests for DURABILITY (branches + snapshots + idempotency ledger survive reopen) and CONCURRENT-MERGE SAFETY (16 threads → exactly one winner via `BEGIN IMMEDIATE`, the publish gate's lost-update prevention).
- **`runtime` = subprocess — ✅ DONE 2026-05-31** (`examples/runtime/subprocess-py/`): passes the shared vectors + adds round-2 tests for OS PROCESS ISOLATION (work runs in a child PID ≠ server; each unit gets its own process). The seed of the sandboxing story.
- **`engine` = REAL pair — ✅ DONE 2026-05-31** via option (b): **DuckDB + DataFusion** (`examples/engine/{duckdb-py,datafusion-py}`) pass a real-SQL conformance set (`engine-real-v1.json`) with REAL typed Arrow (Arrow IPC result leg). ADR-003's literal two-engine cross-run; retires the typed-Arrow gap for engine. The toy refs stay as the wire-contract validation.
- **`format` = REAL pair — ✅ DONE 2026-05-31** via option (b): **Parquet** (pyarrow) + **Delta** (`deltalake`) — `examples/format/{parquet-py,delta-py}` — write real Arrow data files, pass the shared `format-v1.json` (real Arrow data leg both directions via `streams.py`), + backend tests (parquet: real files on disk; delta: TIME TRAVEL). **🎉 ROUND 2 COMPLETE — 6/6.**

> **🎉 ROUND 2 COMPLETE (2026-05-31).** All six data-plane axes have a technologically-divergent real backend passing the shared vectors + a backend-specific semantic test. The typed-Arrow gap is retired for engine+format (real Arrow IPC; only the TRANSPORT remains in-process). The remaining "make it real" item toward freeze is a real **Arrow Flight transport** for the data legs (see current.md).

This also naturally subsumes the **typed-Arrow conformance pass** (a real format/engine backend forces the real Arrow Flight data leg, retiring the in-process stream-registry stand-in). Decision recorded 2026-05-31 (Tom: "finish wire contracts first, then round 2"). Until round 2 lands, the roadmap's per-axis "ADR-003 gate MET" means the wire-contract cross-run only.

---

## ADRs to write (from synthesis — 23 of 26 not yet written)

Numbered as proposed in [reviews/00-synthesis.md](../reviews/00-synthesis.md). Most are Phase 0 wire-breaking concerns that land *during* Phase 0 as the contracts get drafted, NOT before. They're listed here so they're not lost.

> **⚠️ These are *prospective* synthesis numbers — they do NOT match the real ADR sequence.** Real ADR numbers are assigned at write-time ([adrs/README.md](../docs/architecture/adrs/README.md)); the real ADR-005/006/007 already diverged from this table, and the real **[ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md) is catalog commit-linkage** (not "Tenancy"), and the real **[ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) is the Phase-1 spike / commitment-gate decision** (not "API gateway hardening" — that topic gets a later number). Treat the IDs below as topic placeholders, not reservations.

### Phase 0-blocking (must land during Phase 0)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-004 | Wire contract: trace context + correlation IDs | C1 |
| ADR-005 | Plugin-to-core authentication | C2 (Q13) |
| ADR-006 | State-gateway isolation + per-plugin namespacing | C3 |
| ADR-007 | Resource asks / limits in manifest contract | C4 |
| ADR-008 | Capability enforcement at runtime | C5 |
| ADR-009 | Conformance suite obligations per axis | C6 |
| ADR-010 | Tenancy as structural isolation | C7 |
| ADR-013 | API gateway hardening + listener split | C10 |
| ADR-007 ✅ DONE | **Identity transport across the core-mediated hop** — DECIDED + MIGRATED 2026-05-31 ([ADR-007](../docs/architecture/adrs/007-call-context-transport.md)). `RequestContext` moved from message field 1 → `rat-callmeta-bin` metadata header across all 37 control sites; SDKs regenerated; both `format` refs + stub gateway updated; golden vectors green. Carry-over (optional, not blocking): a real SDK metadata interceptor so plugin code gets the reconstructed context automatically. | C1/C2 keystone |
| ADR-008 ✅ | **Streaming capability invocation** — DECIDED 2026-05-31 ([ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md)): add `InvokeServerStream` + `InvokeBidiStream` to `core/v1 CapabilityInvokeService` (generic byte-relays, enforce-at-open, identity in `rat-callmeta-bin` per ADR-007). **Migration is now the in-flight next step** (see [current.md](current.md)): add the 2 RPCs to `invoke.proto`, regen SDKs, server-stream relay in the stub gateway, route `runtime.Execute` through `InvokeServerStream` + add runtime's deferred `(rat.capability)` annotation, re-run runtime vectors. Unblocks gateway mediation for `state.Watch`/`scheduler.WatchDue`/`observability.Ingest` too. | ARCH (streaming) |

### Pre-GA (land during Phase 1-4)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-011 | Plugin supply-chain trust (Sigstore + manifest signing) | C8 |
| ADR-014 | Core-native observability + SLOs | I1 |
| ADR-015 | Upgrade safety: version skew + preflight + reversible migrations | I2 |
| ADR-016 | Backup/restore + GitOps desired-state | I3 |
| ADR-017 | Plugin scaffolding + local dev loop | I4, I5 |
| ADR-018 | Event-bus authn/authz | I6 |
| ADR-019 | Desired-state RBAC + admission control | I7 |
| ADR-020 | Mandatory audit trail | I8 |
| ADR-021 | Deployment-runtime minimum isolation profile | I9 |
| ADR-022 | Reconciler robustness (backoff, jitter, fairness) | I10 |
| ADR-023 | Plugin publish + signing pipeline | I11 |
| ADR-024 | Plugin deprecation + compatibility governance | I12 |
| ADR-025 | Secret-handling contract | I13 |
| ADR-027 | Incumbent-stack migration path strategy | I16 |
| ADR-028 | First-five-minutes wow + front-door demo | I17 |

### Now (land before Phase 0 starts if real product launch is coming)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-026 | GTM positioning + message canon | I15 |

---

## Open questions parked in ideas/inbox.md (synthesis Q11-Q15)

These came up during ADR-002's locking session and are tracked in [ideas/inbox.md](../ideas/inbox.md). They surface as ADRs when relevant:

- **Q11** — Should v2's ADRs 025/026 still be implemented on v2 (D7 implication)?
- **Q12** — Exact default solo bundle composition (becomes ADR-003-bundle when Phase 0 produces references)
- **Q13** — Plugin auth model (resolved by ADR-005 above)
- **Q14** — Marketplace plugin UX shape (becomes a Phase 5 ADR)
- **Q15** — Plugin sandboxing model (resolved by ADR-011 + ADR-021)

---

## Engineering work queued (will activate when phases begin)

### Phase 0 work items
- Draft `plugin/v1.json` JSON Schema (sub-phase 0a)
- Draft 20 axis proto files (sub-phase 0b)
- Draft cross-cutting protos: `common/v1/context.proto`, `common/v1/descriptors.proto` (sub-phase 0c)
- Build 12 reference implementations (sub-phases 0d + 0e)
- Build `rat-conformance` test harness (sub-phase 0f)
- Write 20 `CONTRACT.md` author-facing docs (sub-phase 0g)
- Recruit external peer reviewers (OSGi / K8s / VSCode contributors) — *[reviews/09](../reviews/09-phase-1-gate-review.md) dissent + [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) Q02: zero external human review so far; fold into the full-commitment gate, post-spike.*
- Per-RPC latency benchmark (sub-phase 0f)

### Phase 1 work items
- **🔭 ACTIVE: the contract-de-risking spike** ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)) — minimal real registry + capability enforcer; C5 enforcement; a crash-mid-strategy case; exercise C3 + D2. Goal: break a frozen contract while the freeze is still local. See [current.md](current.md).
- **CI on `phase-1` from commit 1** ([reviews/09](../reviews/09-phase-1-gate-review.md) #7): `buf breaking` (no committed buf baseline exists today — the "additive-only" claim rests on source inspection) + `make {conformance,composition,validate-manifests}`. Keep the freeze **local/unpushed** (no remote/BSR) until C5 enforcement passes.
- Implement the 6 core things in Go (~12-15k LOC)
- Mock plugin set for integration tests
- The 11 weekly milestones from ADR-001 Phase 1 description

### Phase 2 work items
- Promote 12 reference plugins to production grade
- Build `rat plugin new` scaffolding
- Build `rat dev --plugin` local dev loop
- Port v2's `demo-loader` as the front-door wow

### Phase 4 work items
- All hardening items from Phase 4 section of [phases.md](phases.md)
- All GTM items (4 of 5 non-engineering)

---

## Claude Code config: deferred until first code file

One of the two original deferred items has been resolved; one remains.

**Trigger (remaining):** land when real proto authoring experience reveals style patterns.

| Item | What | Why deferred |
|---|---|---|
| Path-scoped proto/manifest rule | A new `.claude/rules/proto-contracts.md` with `paths: ["**/*.proto", "**/plugin.yaml", "**/plugin/v1.json"]` capturing proto-authoring conventions: field naming, message nesting, service naming for Go gRPC, `buf.yaml` layout, capability URI format per ADR-002 D4. | The always-load `plugin-architecture.md` already captures architectural invariants. The path-scoped rule earns its place only once real proto files reveal tool-specific style patterns that don't belong in the always-load rule. Write after the first proto pass (sub-phase 0b). |

**Resolved (not landed):** `PostToolUse` auto-format hook — rejected on latency grounds. See `done.md` 2026-05-30 entry for the decision record.

**How to land the remaining item:**
1. Draft the rule content from actual proto authoring experience first.
2. Add the `paths:`-frontmatter file at `.claude/rules/proto-contracts.md`.
3. Update this backlog entry → cut it + append to `done.md`.

Source: `claude-engineer` audit 2026-05-30. Doc citations: `https://code.claude.com/docs/en/hooks-guide.md` (PostToolUse pattern), `https://code.claude.com/docs/en/sub-agents.md` (rules path-scoping).

---

## GTM / non-engineering work (deferred until commitment)

These need the project commitment decision before they make sense to start:

- Reposition project message (anti-lock-in / cost-ownership canon)
- Hand-pick 3-5 design partners (year 1)
- Build a public reproducible benchmark (the Polars pattern)
- Plan a content/distribution motion (founder-led)
- Design dbt→RAT and Airflow→RAT migration UX
- Commercial-path planning (managed cloud later)

---

## Ideas that may or may not become work

In [ideas/inbox.md](../ideas/inbox.md) — naming, plugin distribution patterns, manifest-from-proto generation, etc. Promote to backlog if they sharpen into specific work.

---

## Maintenance

When new work is identified during a working session:

1. Capture it here (this file) with enough specificity to be actionable.
2. Note where it came from (review, idea, conversation, ADR).
3. Don't worry about ordering — that's done at promotion time (when item moves to [current.md](current.md)).

When an item starts:

1. Cut it from this file.
2. Add to [current.md](current.md) with the immediate next concrete step.
3. Update [phases.md](phases.md) status if it changes a phase boundary.

When an item is dropped (decided against, superseded):

1. Cut it from this file.
2. Note the decision in the relevant ADR or `ideas/inbox.md` archive.
3. Don't leave dead entries here.
