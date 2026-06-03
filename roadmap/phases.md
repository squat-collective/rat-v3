# Phases

The full-project plan, Phase 0 → Phase 5. Reflects the **post-synthesis** scope (see [reviews/00-synthesis.md](../reviews/00-synthesis.md)) — Phase 0 expanded to bake Critical concerns into contracts before freeze; Phase 4 adds the GTM work the original plan deferred.

> **Single source of truth on scope/timeline:** [ADR-001 Migration section](../docs/architecture/adrs/001-everything-is-a-plugin.md) + this file. If they conflict, [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md) wins; update this file.

## Phase status at a glance

| Phase | What | Status | Effort | Notes |
|---|---|---|---|---|
| **−1** | Architectural design + adversarial review | **done** (2026-05-30) | ~1 day | ADRs 001-003, vision, overview, 5-perspective review, synthesis |
| **0** | Lock the contracts (with Critical concerns baked in) | ✅ **DONE — SEALED `rat/1.5`** (2026-06-01) | 4-6 months | All 18 axes frozen + board-reviewed ([reviews/08](../reviews/08-post-freeze-board-review.md)); the close-out is complete — catalog commit-linkage (ADR-010), manifest freeze + 18 per-kind schemas (ADR-011), all 18 `CONTRACT.md` + doc tail (E1/E3/E4/E7), C1/C2 crash-safety (ADR-012) — and `rat/1.5` is cut. Phase 1 acceptance criteria = the deferred C3–C5/D1–D5 findings. |
| **1** | Build the core (~12-15k LOC) | ✅ **DONE — SEALED `rat/2.0`** (2026-06-02) | 3 months | All 9 board exit criteria met (C1/C3/C4/C5, D1–D4, sre#4) against real launched plugins. Six things + cross-cutting enforcement. |
| **2** | The v2-on-v3 **data platform** + the **per-project daemon UX** | ✅ **DONE — SEALED `rat/2.5`** (2026-06-03) | — | *Reframed from "solo reference plugins":* the data platform bundle (ADR-020/021) — landing→medallion as dbt code, self-driving, shared DuckLake — built as launch-mode plugins (ADR-022) under a poetry-style per-project daemon (ADR-023), secrets centralized, an extensible UI (ADR-024/025). Additive to `rat/2.0` (no wire change). ADRs 019–025. |
| **3** | **Surfaces & consumers** (ADR-024/025) — per-surface plugin interfaces; out-of-stack consumers | ✅ **DONE — SEALED `rat/3.0`** (2026-06-03) | — | All three surfaces demonstrated: CLI (`rat ui`, live), VS Code (scoped shell, compiles), webapp (bff SPA, rendered in a browser). UI assembled from contributions; `contribute_ui`. Additive. |
| **4** | **Distribution** — the GHCR release pipeline | ✅ **DONE — SEALED `rat/3.5`** (2026-06-03) | — | rat ships as a `ghcr.io` binary + image (`curl …/install.sh \| sh && ./rat version`). Reproducible make targets + `.github/workflows/release.yml` + `scripts/install.sh`. Hardening/GTM + GHCR plugin-image pull are the follow-ons. |
| **5** | **Plugin authoring & packaging** (ADR-026) — `rat plugin init/check/test/pack/publish` | ✅ **DONE — SEALED `rat/4.0`** (2026-06-03) | — | *Reframed from "ecosystem moves":* the authoring lifecycle (scaffold → check incl. dep coherence → I9 launch-verify → verified manifest-stamped image → publish to a registry). The third pillar (author) beside run + distribute. Marketplace/signing/conformance are follow-ons. |
| **6** | **Authoring ↔ runtime** (ADR-026 Q05) — manifest-from-image | ✅ **DONE — SEALED `rat/4.5`** (2026-06-03) | — | `rat add --image <ref>` reads the manifest stamped into a packed image (no `--manifest`; name derived). Closes the author→run handoff; the image is self-describing. |

**Total:** ~12-18 months from Phase 0 kick-off to v1 GA.

---

## Phase 0 — Lock the contracts (4-6 months)

**Goal:** the contract triple (manifest schema + protos + capability namespace) frozen as `rat/1` with Critical cross-cutting concerns baked in and 2 reference implementations stress-testing each critical axis.

**Why the scope grew (vs original 1-2 months):** the 5-perspective adversarial review surfaced 10 Critical findings that are wire-breaking to retrofit. Doing them in Phase 0 ≈ 4 weeks of extra work now; deferring them ≈ ecosystem flag-day later. See [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) and [reviews/00-synthesis.md](../reviews/00-synthesis.md).

**Sub-phases:**

| Sub-phase | Work | Effort |
|---|---|---|
| 0a | Manifest schema (`contracts/schema/plugin.v1.json`) — including resources, trust, capabilities blocks | 2-3 weeks — **envelope drafted ✅; per-kind schemas deferred to 0b** |
| 0b | ~20 axis protos (data + control + experience) | 4-6 weeks, parallelizable — **DONE ✅ 20 protos (18 axes + 2 common); buf lint+build+generate clean. Remaining 0b work: per-kind manifest schemas derived from protos** |
| 0c | Cross-cutting concern protos | 2-3 weeks — **✅ COMPLETE. Set: `common/v1/{context,data,annotations,event,audit}` + `core/v1/invoke`. Audit envelope moved auditlog.v1 → common.v1 (`audit.proto`, wire-compatible) so the core's C8 emission doesn't import an axis. Coverage doc maps every C1–C10/ARCH concern to its home; "descriptors" = the manifest (0a) + proto service descriptors. See `docs/architecture/cross-cutting-coverage.md`.** |
| 0d | Reference implementations round 1 — 1 per critical axis (6 plugins) | 6-8 weeks, parallelizable — **✅ COMPLETE: `format` `engine` `storage` `runtime` `catalog` `state` (`inmemory-go` each); all 6 data-plane axes. NOTE: "round 1/2" here = the two LANGUAGE twins (wire-contract gate); the technologically-divergent real-backend pair is a separate round, tracked in backlog.** |
| 0e | Reference implementations round 2 — 1 per critical axis (6 more) | 3-5 weeks — **✅ COMPLETE both senses. (a) Language twins (`inmemory-py`) → wire-contract gate; ADR-007 + ADR-008 decided AND migrated; gateway mediates unary + server-streaming. (b) 🎉 The technologically-divergent REAL-backend round is DONE for all six: state=sqlite, storage=local-fs, catalog=sqlite, runtime=subprocess, engine=duckdb+datafusion, format=parquet+delta — each passing the shared vectors + a backend-specific semantic test; typed-Arrow retired for engine+format. Remaining before `v1`: real Arrow Flight transport + 0f conformance suite + 0h freeze.** |
| 0f | Conformance suites + per-RPC latency benchmark | 3 weeks — **✅ COMPLETE. Conformance suite runner (`make conformance` → 20/20 references conform; auto-discovers refs, one pass/fail matrix, CI/freeze-gateable) + per-RPC latency benchmark (`make bench` → core-mediation hop ≈ +0.2ms/+270%, validates ADR-005). Real Arrow Flight transport ✅ (parquet-py).** |
| 0g | Author-facing prose (`CONTRACT.md` per axis) | ongoing; finalize 2 weeks — **✅ DONE for the 6 data-plane axes** (`contracts/proto/rat/{state,engine,format,storage,runtime,catalog}/v1/CONTRACT.md`): capabilities + RPCs + conformance obligations + writing-a-plugin + reference-impl table; indexed in the conformance README; all links verified, buf-clean. Control/experience axes when referenced. |
| 0h | Peer review + revisions + `rat/1` freeze | **✅ COMPLETE.** Freeze review ([reviews/07](../reviews/07-freeze-review.md)) → NO-GO punch-list (M1–M4 + S1–S4), all remediated. |
| 0i | Cross-axis composition (ADR-003 cross-combination gate) | **✅ COMPLETE.** First `strategy` ref + `make composition` — all 4 ADR-003 combos (DuckDB/DataFusion × Parquet/Delta × sqlite/in-memory) produce the identical target, strategy unchanged. |
| — | **`rat/1` FREEZE** | **🧊 DONE (ADR-009, tag `rat/1`).** 6 data-plane axes + cross-cutting types → `v1`. Control/experience + manifest stay `v1-preview`. |
| — | **`strategy/v1` FREEZE** | **🧊 DONE (tag `rat/1.1`).** Second strategy ref `scd2-py` (divergent: SCD2, `merge`-based) landed → `strategy/v1` → `v1`. `make composition` proves both strategy refs. |
| — | **Control-plane FREEZE** | **🧊 DONE (tag `rat/1.2`).** 7 axes (identity/secret/scheduler/tenancy/billing/observability/audit-log) — one ref + conformance each (ADR-003). `make conformance` 27/27. |
| — | **`deployment-runtime` FREEZE** | **🧊 DONE (tag `rat/1.3`).** Two divergent refs (local-process + k8s-dryrun) sharing the I9 isolation gate. `make conformance` 29/29. |
| — | **Experience FREEZE** | **🧊🎉 DONE (tag `rat/1.4`).** ui/notifications/marketplace — one ref each. **ALL 18 axis contracts now `v1`.** `make conformance` 32/32. Only `v1-preview` left: the manifest schema (`plugin/v1.json`). |

**Deliverables:**
- `plugin/v1.json` published at stable URL — **frozen `v1`** (ADR-011); kept local/unpushed pending the Phase-1 spike (ADR-013)
- **24** proto files + generated SDKs in Go, Python, Rust, TypeScript *(Java dropped)*
- **32** conformance-passing reference implementations in `examples/`
- Conformance test harness (`make conformance`)
- **18** CONTRACT.md docs (one per axis)
- Benchmark report
- Peer review notes — *adversarial only so far; external human review still owed (reviews/09 dissent, ADR-013 Q02)*

**Done when:**
- All critical axes have 2 reference implementations
- All 12 references pass their axis's conformance suite
- At least 4 cross-combinations of references work end-to-end on golden data
- Per-RPC benchmark numbers documented and acceptable
- External peer review feedback addressed
- `rat/1` contracts tagged + published

**Critical concerns to bake in** (see [reviews/00-synthesis.md](../reviews/00-synthesis.md) Critical findings):
- C1: Trace/correlation context in every RPC + event envelope
- C2: Plugin-to-core authentication primitive (per-plugin token + mTLS-ready)
- C3: State-gateway per-plugin namespacing
- C4: Resource asks/limits as mandatory manifest fields
- C5: Capability enforcement (declared = enforced at runtime)
- C6: Conformance suite obligations per axis
- C7: Tenancy as structural dimension (not advisory hook)
- C8: Plugin supply-chain trust (signing required for team+)
- C9: Two-reference rule (this ADR)
- C10: API gateway listener split (port v2 ADR-019)

**Phase 0 close-out (chosen 2026-06-01 — "complete & seal Phase 0"):** the contract surface is frozen + board-reviewed; four items remain before Phase 1, then cut contract **`v1.1`**:
1. ✅ **Catalog commit-linkage** — **DONE 2026-06-01 ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))**: additive `RegisterTable` + `CommitTable` RPCs so the branch-pipeline loop closes on the wire (reviews/08 B1 — the #1 functional gap). Proto + 4 SDKs + 3 refs + 6 golden steps + `examples/composition` rewired off out-of-band seeding; 32/32 + composition green. Resolves R3.
2. ✅ **Manifest schema freeze + 18 per-kind schemas** — **DONE 2026-06-01 ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md))**: `plugin.v1.json` frozen at `v1` (the last `v1-preview` artifact); `(rat.capability)` rolled across the 12 unannotated axes (additive); 18 per-kind schemas in `schema/kinds/` with minimal-mandatory-core `provides`; `make validate-manifests` gate (32/32).
3. ✅ **Doc tail** — **DONE 2026-06-01** (reviews/08 E1/E3/E4/E7): all 18 axes have a `CONTRACT.md` (12 authored via parallel subagents, caps verified vs the protos); `overview.md` drift fixed (declarative reconciler + tier-0 callout); temptation ledger started (count 0); 13 round-1 refs labeled `WIRE-CONTRACT REFERENCE`.
4. ✅ **Cut `v1.1`** — **DONE 2026-06-01 ([ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields.md))**: folded in the C1/C2 crash-safety additives (write-leg idempotency + ArrowStream completeness, demonstrated in the composition), then tagged `rat/1.5` over the sealed surface. 🎉 **Phase 0 complete.**

**Phase 0 detail:** lives in this file + ADR-001 + ADR-003 + the close-out in [current.md](current.md). (No separate `phase-0-detail.md` yet.)

---

## Phase 1 — Build the core (3 months)

> **Spike COMPLETE → gate CLEARED (2026-06-01).** Entered as a time-boxed contract-de-risking spike ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)); it stood up a real registry + capability enforcer and validated the frozen wire (C5/C1/C3/D2 green, no freeze-reopen — [reviews/10](../reviews/10-phase-1-spike-exit.md)). On that evidence the commitment gate is **CLEARED** ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md)) and the full ~3-month build below is **committed**. Next increment: **D1** (real process isolation).

**Goal:** `rat` binary that boots, accepts manifest installs (rejecting unsatisfied requires), runs the reconciler loop on a leader-elected single replica, emits `/metrics` + OTel, and exercises every Phase 0 contract via mock plugins. No functional plugins yet — the substrate.

**Six things implemented:**
1. Registry (~1500 LOC)
2. Identity gateway (~800 LOC) — port v2 ADR-020 platform token
3. State gateway (~1200 LOC) — per-plugin namespacing enforced
4. Event bus (~1000 LOC) — embedded NATS JetStream + subject-level authz
5. Reconciler loop (~2000 LOC) — leader-election + backoff/jitter + per-pipeline fairness
6. API gateway (~1500 LOC) — internal vs public listener split

**Cross-cutting (~2000 LOC):** trace context propagation, native observability, resource limits enforcement, capability enforcement, audit emission.

**Total:** ~12-15k LOC of Go.

**Milestones:** see ADR-001's expanded Phase 1 description for the 11 weekly milestones.

**Done when:**
- A mock plugin can register, get authenticated, make state-gateway calls scoped to its namespace
- A second mock plugin can't read the first plugin's state
- Lease handover works in <20s
- All Phase 0 contracts exercised by real code paths

**Acceptance criteria from the board review ([reviews/08](../reviews/08-post-freeze-board-review.md)) — the core isn't "done" until these *pass*** (the review converted "the core will enforce X" into testable exit conditions):
- **✅ C5** capability enforcement — a plugin invoking a capability not in its manifest `requires` is denied (`declared == provided` is *enforced*, not self-asserted). **Proven against REAL providers (2026-06-01):** the Go refs `examples/{catalog,format}/inmemory-go` (independent modules, local-process) and the SQLite catalog ref in a container under the podman full-I9 profile — declared caps route + return real results, undeclared caps the providers genuinely implement (merge / merge-branch) are denied + audited. (The complementary `declared == conformed` half is **D4**, still open.)
- **✅ C4** audit-on-every-decision — every enforcement decision (incl. DENIED) emits one record; streams emit a terminal close record. **DONE (2026-06-01):** the gateway records one decision record per call (allow/deny) and now a **terminal stream-close record** (Outcome success/error/canceled + frames + error, correlation-linked to the open); a denied stream gets only the deny record. The core **signature + hash chain** on the canonical `common/v1.AuditRecord` (the spike uses a simplified in-memory record) is the remaining GA item.
- **✅ C3** provider-call deadline — the core bounds the provider call by `min(channel, deadline_unix_ms)` + a streaming idle-timeout (a hung provider can't pin the gateway). **DONE (2026-06-01):** the deadline bound was real in the spike (unary + streams); the streaming idle-timeout backstop now cuts a silent provider even with NO deadline set (`Gateway.StreamIdleTimeout`, default 5m, reset per frame → `DeadlineExceeded` + a terminal `{timeout}` audit record). With this the gateway **C-series is complete** (C5/C4/C3/C1).
- **✅ D1** isolation conformance — a real *enforcing* deployment-runtime (podman, not dry-run) passes a full-profile vector. **MET (2026-06-01, [ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md)):** `core/deploymentruntime.Podman` enforces the full I9 profile at the kernel level (`--user`/`--cap-drop=ALL`/`--security-opt=no-new-privileges`/`--read-only`/seccomp + private netns); the live vector `make core-test-podman` proves it from inside the sandbox — uid≠0, CapEff=0, NoNewPrivs=1, read-only root (EROFS), metadata `169.254.169.254` unreachable — plus a structured isolation receipt. Closes the reviews/08 D1 honesty gap.
- **✅ D2/✅ D3** bytes-plane isolation — ArrowStream-ticket (TTL/single-use/binding) + storage-cred scoping are vector-tested, not honor-system. **Both DONE (2026-06-01).** **D2:** the `ArrowStream.ticket` (HMAC-signed, TTL'd, single-use, `{stream,caller,tenant}`-bound) is enforced as the sole gate on a real out-of-band bulk transfer — replay / cross-binding / expiry / tamper all refused at the boundary, no bytes leak (`core/arrowticket`). **D3:** the real `localfs-go` storage ref, launched behind the gateway, vends creds scoped to (tenant, prefix, mode, short TTL) with per-tenant root isolation + path containment (escape → `PERMISSION_DENIED`); tenant is read only from the gateway-re-stamped envelope.
- **✅ C1** crash-safety — the additive fields (`idempotency_key`, `already_applied`, `expected_rows/batches`) landed in `rat/1.5` (ADR-012); the Phase-1 AC is the *enforced* test: at-least-once re-runs don't double-apply. **DONE (2026-06-01):** proven against the fakes (composition) AND **against real backends** — a same-key retry against the real inmemory-go catalog is a no-op (original result returned even on payload drift), and a durable SQL ledger (sqlite-py under podman with a persistent volume) survives a real **backend crash+restart** (replay still `already_applied`). *Residual:* the write-leg idempotency is proven only against the fake (no real idempotent format ref exists yet) — a documented follow-on, not a freeze-reopen. (No strategy commit/abort wire shape was needed → no freeze-reopen, per [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md).)
- **✅ D4** conformance attestation — the core verifies `declared == conformed` (the marketplace/attestation is *derived*, not self-asserted). **DONE (2026-06-01):** `core/conformance` (ed25519-signed `Attestation` + an authority keyring) + `registry.NewVerified` trust a declared `provides` only when a signed attestation covers it — refusing missing / bad-signature / declared-but-not-conformed. The core's first real signature verification; the keyID model seeds the GA audit-signing (C4) + C8 supply-chain.
- **✅ sre#4** reconciler robustness — crash-loop backoff + jitter + lease-thrash guard. **Promoted from the backlog to an explicit Phase-1 exit gate** by [reviews/09](../reviews/09-phase-1-gate-review.md) (don't re-make the K8s CrashLoopBackoff mistake). **DONE (2026-06-01):** `core/reconciler` (level-triggered convergence; failing plugins restart with exponential backoff + jitter, capped to Degraded — never hammering the runtime) + `core/lease` (single-key CAS + an Elector whose TTL-margin/min-hold thrash guard keeps leadership stable under renewal-latency spikes; failover only on genuine expiry). Proven deterministically + end-to-end (real crash-looping plugin → Degraded; two-replica leader+failover). **🎉 With this, ALL Phase-1 acceptance criteria (C1, C3, C4, C5, D1, D2, D3, D4, sre#4) are MET — ready for the `rat/2.0` seal.**

---

## Phase 2 — Solo deployment reference plugins (2 months)

**Goal:** promote Phase 0 reference plugins to production quality for the solo bundle.

> **🛰️ Kickoff deliverable — [ADR-019](../docs/architecture/adrs/019-rat-serve-daemon.md) `rat serve` (Accepted, 2026-06-02).** The sealed core is a library, not a server; `rat serve` assembles it into a runnable daemon (the real API gateway over TCP) and a beginner `compose up` starter. **Phase A** (daemon vs the Go test plugins — the core first *runs*) → **Phase B** (containerize the data-dev Python plugins; the pipeline routes through the real gateway) → **Phase C** (`deploy/data-dev-starter/` compose stack: daemon + base plugins + MinIO/Postgres). This directly realizes Phase 2's "done when" (`rat run` end-to-end + a one-command front-door). The **data-dev-plane experiment** already produced the bundle's plugins (engine `duckdb-ml`, catalog `ducklake`, storage `minio-s3`, strategy `incremental-embed`, ui `vscode-rat`). Make-it-real infra, **not** behind Gate B.

> **🎯 The product — [ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md) the data platform bundle (Accepted, 2026-06-02; re-aimed same-day).** **`ratatouille-v2` rebuilt on the v3 plugin core**: the same platform (landing → medallion → quality-gated **scheduled** refreshes), every responsibility **decoupled into a v3 plugin** behind the gateway, **DuckLake as the catalog** (replacing Nessie/Iceberg), **VS Code + `ratctl`** replacing the portal. **Always-on + self-driving** — `rat serve` 24/7 + a **scheduler plugin** firing hourly refreshes; state remote (DuckLake-on-Postgres + S3). v2→v3 spine: `ratd`→`rat serve` · scheduler→**scheduler plugin** · runner→**engine + pipeline strategy** · ratq→engine query · portal→**vscode-rat + ratctl** · postgres→**state-backend** · minio→**storage** · **nessie→DuckLake**. Build order (each provable): **S1** decoupled stack runs the medallion via `rat serve`, remote (keystone: **attach mode**) · **S2** scheduler plugin (self-driving cron) · **S3** merge strategies + quality gates · **S4** state-backend + VS Code/CLI. New plugins: scheduler, state-backend, the pipeline/medallion strategy; + attach mode.

> **Status: Phase 2 IN-FLIGHT.** ✅ ADR-019 Phase A (`rat serve`, `make core-serve-smoke`) · ✅ containerized daemon (`core/Dockerfile`, `make rat-image`) · ✅ `ratctl` (`make ratctl-smoke`) · ✅ `platform/` project scaffold (medallion models/landing/tests — kept; execution re-aimed to scheduled+plugin-driven). **NOW: ADR-020 S1a — attach mode** (`supervisor.Attach` + `endpoint:` path), the keystone for the always-on `compose up` stack.

**Bundle:** sqlite state-backend, in-process scheduler, local-fs storage, embedded duckdb engine, embedded pyarrow runtime, embedded iceberg format, full-refresh strategy, web-portal UI, community-marketplace plugin.

**Also ships:**
- Plugin scaffolding (`rat plugin new --kind X --lang Y`)
- Local dev loop (`rat dev --plugin ./my-strategy` with manifest-on-disk override)
- Mock plugin set for isolated axis testing
- The **demo-loader** port from v2 — "zero → full warehouse in 60s" as the front-door wow

**Done when:** `rat init && rat run my-pipeline.yaml` works end-to-end. Front-door demo runs in <60s on a laptop.

---

## Phase 3 — Self-hosted team reference plugins (2 months)

**Goal:** match v2's "self-hosted team" operational shape.

**Bundle:** postgres state-backend, docker deployment-runtime, S3 storage, Nessie or Lakekeeper catalog, OIDC identity, Prometheus observability, audit-log to file, slack notifications.

**Done when:** a 5-person team can deploy via `docker compose up`, run pipelines with full quality testing + branch isolation, get observability dashboards out of the box.

---

## Phase 4 — Hardening + GTM motion (3 months)

**Goal:** production-grade ops + the non-engineering work the synthesis flagged.

**Engineering hardening:**
- Upgrade safety: version skew policy + `rat preflight upgrade` + reversible state-schema migrations
- Backup/restore: consistent backup set across state-backend + JetStream + plugin configs; RPO/RTO targets; GitOps desired state
- Plugin publish + Sigstore signing pipeline; supply-chain trust gates
- Plugin deprecation governance (`compatible_core` field, `rat plugin doctor`, N-1 skew)
- Crash-loop backoff + lease-thrash guards + reconcile fairness
- Secret rotation contract

**GTM work (in parallel — see [vision.md](../docs/vision.md) anti-goals):**
- Reposition message: anti-lock-in / cost-ownership, not "extensible"
- Migration paths off the incumbent stack (dbt → RAT, Airflow → RAT)
- Design-partner program — hand-pick 3-5
- Public reproducible benchmark (the Polars pattern)
- Founder-led distribution motion (content, community, hand-to-hand first-100-users work)

**Done when:** the product can be operated in production by a 50-person team AND has at least 3 design partners running real pipelines.

---

## Phase 5 — Ecosystem moves (1-2 months)

**Goal:** unlock the rest of the architecture's promise.

- Marketplace plugin UX (capability-aware "works on your deployment?" filter, signature display, trust badges)
- Third deployment topology (hybrid: on-prem control + cloud data)
- Multi-UI story (CLI, Slack bot, VS Code extension) — each as a separate `ui` plugin
- Portal slot ecosystem (third-party plugins contributing UI to the portal)
- Cross-engine federation (deferred from Phase 0 — see [reviews/00-synthesis.md](../reviews/00-synthesis.md) C9 alternative)

**Done when:** 10+ external plugin authors have shipped plugins for at least 5 different axes.

---

## Gates between phases

The phases aren't strict — work parallelizes. But four **hard gates** exist:

1. **Gate A (Phase 0 → Phase 1):** `rat/1` contracts tagged. No core code lands against unfrozen contracts.
2. **Gate B (Phase 2 → Phase 3):** the solo bundle is *actually used* by ≥10 real solo users running pipelines. If we can't get 10 solo users, the team bundle doesn't matter yet.
3. **Gate C (Phase 3 → Phase 4):** the self-hosted team bundle runs in at least 3 real teams' production for ≥30 days without ratd-team intervention. If we can't operate it at small scale, hardening is premature.
4. **Gate D (Phase 4 → Phase 5):** we have ≥100 daily-active users on the core (the [vision.md](../docs/vision.md) anti-goal). If not, ecosystem expansion is the wrong investment.

These gates exist because the synthesis flagged "shipping more architecture without users" as the modal failure mode. The gates force the project to be user-pulled, not architecture-pushed.

---

## Decision gates that BLOCK phase entry

Two decision gates the project must clear before phases proceed:

- **Before Phase 0:** Tom commits to 12-18 months of focused runway + the GTM work (not just the architecture work). Without this, the project should freeze at "great public design corpus" and not enter Phase 0. **Status (2026-06-01):** acknowledged → spike-deferred ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)) → **CLEARED ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md))**: the spike validated the contracts ([reviews/10](../reviews/10-phase-1-spike-exit.md)) and the full **core build** is committed (the Phase-0→1 gate). The GTM-specific commitment remains gated at Phase 4 + the user-pull Gates B/C/D below.
- **Before Phase 4:** Tom commits to the non-engineering work (design partners, content, distribution). Without this, hardening produces a beautifully-architected platform with no users — the unflattering scenario.

These aren't dates; they're commitments. The roadmap is honest about them because the synthesis was.
