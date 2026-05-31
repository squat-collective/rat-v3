# Phases

The full-project plan, Phase 0 → Phase 5. Reflects the **post-synthesis** scope (see [reviews/00-synthesis.md](../reviews/00-synthesis.md)) — Phase 0 expanded to bake Critical concerns into contracts before freeze; Phase 4 adds the GTM work the original plan deferred.

> **Single source of truth on scope/timeline:** [ADR-001 Migration section](../docs/architecture/adrs/001-everything-is-a-plugin.md) + this file. If they conflict, [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md) wins; update this file.

## Phase status at a glance

| Phase | What | Status | Effort | Notes |
|---|---|---|---|---|
| **−1** | Architectural design + adversarial review | **done** (2026-05-30) | ~1 day | ADRs 001-003, vision, overview, 5-perspective review, synthesis |
| **0** | Lock the contracts (with Critical concerns baked in) | **in-flight** — 🧊 **data-plane FROZEN `rat/1`** (2026-05-31, ADR-009) | 4-6 months | Headline deliverable DONE; remaining = control-plane refs + `strategy` 2nd ref + manifest freeze |
| **1** | Build the core (~12-15k LOC) | not-started | 3 months | Six things + cross-cutting enforcement |
| **2** | Solo deployment reference plugins (production-grade) | not-started | 2 months | `chmod +x ./rat` works end-to-end |
| **3** | Self-hosted team reference plugins | not-started | 2 months | Match v2's operational shape |
| **4** | Hardening + GTM motion | not-started | 3 months | 4-of-5 non-engineering GTM gaps land here |
| **5** | Ecosystem moves | not-started | 1-2 months | Marketplace UX, multi-UI, hybrid topology |

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
- `plugin/v1.json` published at stable URL
- 20 proto files + generated SDKs in Go, Python, Rust, TS, Java
- 12 reference plugins in `examples/`
- Conformance test harness (`rat-conformance test`)
- 20 CONTRACT.md docs
- Benchmark report
- Peer review notes

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

**Phase 0 detail:** lives in this file + ADR-001 + ADR-003. (No separate `phase-0-detail.md` yet — referenced from ADR-001 but write only if scope grows beyond what this section captures.)

---

## Phase 1 — Build the core (3 months)

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

---

## Phase 2 — Solo deployment reference plugins (2 months)

**Goal:** promote Phase 0 reference plugins to production quality for the solo bundle.

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

- **Before Phase 0:** Tom commits to 12-18 months of focused runway + the GTM work (not just the architecture work). Without this, the project should freeze at "great public design corpus" and not enter Phase 0.
- **Before Phase 4:** Tom commits to the non-engineering work (design partners, content, distribution). Without this, hardening produces a beautifully-architected platform with no users — the unflattering scenario.

These aren't dates; they're commitments. The roadmap is honest about them because the synthesis was.
