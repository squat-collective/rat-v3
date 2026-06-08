# ADR-001: Everything is a plugin

## Status: Accepted (2026-05-30)

## Context

This is the **founding ADR** for RAT v3. Every subsequent decision flows from it; if this one is wrong, the rest of the project is wrong.

RAT v2 (`~/sandbox/ratatouille-v2/ratatouille/`) has shipped a serious data platform with growing decoupling ambition — ADR-024 (decoupled storage/catalog/engine axes), ADR-025 (on-demand planes), ADR-026 (plugin capability manifests + dependency negotiation). Each of these is a step toward "more decoupled, more pluggable, more extensible." Reading them in sequence, a pattern emerges: *every direction we move, the answer is "make it a plugin."* But v2's core (`ratd` + `runner` + `ratq`) has baked-in assumptions — postgres as mandatory state, ratd as imperative orchestrator, portal as the only UI — that limit how far the pluginization can go.

The question this ADR resolves: **for v3, do we set a default rule for how the platform is structured?**

Two coherent answers were considered:

1. **"Plugins where useful, core where convenient."** A pragmatic platform with a substantial core that handles the common case directly; plugins for the long tail. This is how most platforms are structured (incl. dbt, Airflow, Spark, even K8s before CRDs).
2. **"Everything is a plugin; the core is a minimal coordinator."** A radical platform whose core handles only what *cannot* be a plugin. Everything else, including state backend, auth, scheduler, UI, deployment runtime, tenancy, billing — all plugins. This is closer to how Kubernetes works at its purest (operators + controllers + CRDs for everything), or NATS (a coordination kernel with everything around it).

We're choosing (2).

## Decision

**Every load-bearing concern in RAT v3 is a plugin, except for six irreducible core responsibilities.** The core's job is to coordinate plugins, not to provide functionality itself.

### The six irreducible core responsibilities

Each of these *cannot* be a plugin because of chicken-and-egg: it would have to exist before the plugin system to bootstrap the plugin system.

1. **Registry** — reads plugin manifests, indexes them by `(kind, name, version)`, answers capability lookups. The registry can't be a plugin because plugins are discovered through it.
2. **Identity gateway** — every request carries an identity. Validation delegates to an identity plugin (or returns anonymous-root if none installed). The gateway can't be a plugin because the core needs to know who's asking before it can route — but the validation logic *is* a plugin.
3. **State gateway** — `Get/Put/Watch/List` interface. The implementation is a state-backend plugin (postgres, sqlite, dynamodb, etcd, in-mem). The interface is in-core because the registry itself needs somewhere to persist — but the implementation *is* a plugin.
4. **Event bus** — async publish/subscribe for coordination + observability. Backed by a pluggable transport (NATS, Kafka, Redis Streams). The bus can't be a plugin because plugins coordinate through it.
5. **Reconciler loop** — reads desired state (manifests, planes, pipelines), compares to actual state, drives convergence. K8s controller pattern. The reconciler can't be a plugin because it's the thing that ensures plugins do what they're told.
6. **API gateway** — single entry point (gRPC + REST), authenticates via identity gateway, routes to handlers or proxies to plugins. The gateway can't be a plugin because it's how plugins are reached.

**Total core code budget: 5-10k LOC.** Anything more = scope creep.

### The 16-19 plugin axes (open-set; community can add more)

#### Data-plane (touch user data)
- `engine` — SQL → Arrow (DuckDB, ClickHouse, Spark, …)
- `runtime` — Arrow → Arrow ops (PyArrow, Polars, Datafusion)
- `format` — Arrow ↔ on-disk + metadata (Iceberg, Delta, Hudi)
- `strategy` — composes format + runtime ops (full_refresh, scd2, soft_delete)
- `catalog` — table identity, branches (Nessie, Lakekeeper, Unity, Glue, Hive)
- `storage` — credentials + bytes (S3, GCS, Azure, MinIO, IPFS)
- `deployment-runtime` — where plugins run (local-process, docker, podman, k8s, nomad, lambda)

#### Control-plane (backbone)
- `state-backend` — postgres, sqlite, dynamodb, etcd, in-memory
- `secret-backend` — env, Vault, AWS-SM, GCP-SM
- `scheduler-backend` — in-process cron, Temporal, Airflow-bridge, k8s-CronJob
- `identity` — anonymous, password, OAuth, OIDC, SAML, Keycloak
- `tenancy` — none, namespace, org, hierarchical
- `billing` — none, usage-metered, seat-based
- `observability` — stdout, prometheus, otel, datadog
- `audit-log` — file, postgres, splunk, kafka

#### Experience (human interface)
- `ui` — web-portal, cli, slack-bot, vim, vscode-ext
- `notifications` — slack, email, pagerduty, webhook
- `marketplace` — plugin distribution + discovery

### Why this specific decomposition

Three principles:

1. **An axis is a thing that has more than one credible implementation.** State backend has many (postgres, sqlite, dynamo, etcd). Auth has many (none, OIDC, SAML). If there's truly only ever one good answer, it lives in core.
2. **An axis is something operators have *opinions* about.** Solo devs want sqlite; enterprises want sharded postgres. Same data platform should serve both — that's what "axis" means.
3. **An axis is where the platform's identity should NOT prescribe.** RAT v3 picks no engine, no catalog, no UI. Those are operator choices.

## Consequences

### Positive

- **The platform truly has no lock-in.** Every choice an operator makes is undoable by swapping a plugin. Same OSS codebase serves solo devs, teams, and enterprise.
- **The core stays small.** 5-10k LOC means: one person can read it in a day. Bugs are concentrated. Security audits are tractable.
- **Adding new functionality never touches core.** New format = new plugin. New scheduler = new plugin. New UI = new plugin. Core releases are infrequent and small.
- **Multi-tenancy, billing, audit — all just plugins.** Same code path serves open-source single-tenant and SaaS multi-tenant. No fork.
- **Deployment topologies are configurations, not products.** Solo, team, hybrid, full-cloud — same binary, different plugin set.
- **Plugin authors can use any language.** Contract is proto + manifest. Implementation is whatever they want.

### Negative — accepted

1. **Plugin ecosystem chicken-and-egg.** With 16 axes, you need plugins for every axis to ship a working RAT. We'll bootstrap by writing first-party reference plugins for all 16. That's a lot of upfront code before the platform is "useful."
2. **The "deploy is a plugin combination" promise pushes complexity to operators.** They have to know what plugins they want. **Mitigation:** ship *bundles* (`rat-bundle-solo`, `rat-bundle-team`, `rat-bundle-cloud`) — curated default sets that hide the composition for new users.
3. **The reconciler model is harder to debug than imperative orchestration.** "Why didn't my pipeline run?" can have answers across many plugins + the core's reconciler logic. **Mitigation:** invest in `rat diagnose` tooling from day 1.
4. **Performance ceiling on the event bus.** If everything goes through it, the bus becomes the bottleneck. NATS/Kafka can handle massive throughput, but defaults need to scale to ~10k events/sec without operator tuning. **Mitigation:** ship a well-tuned default; document scaling.
5. **A serious rewrite, not an evolution.** RAT v2 can't incrementally become this — too many baked-in assumptions. **Mitigation:** v2 continues to ship in parallel; v3 grows from architecture → contracts → reference plugins → core over ~12-18 months.
6. **The marketing surface is harder.** "Most extensible data platform" is vague; "K8s for data" invites comparison to actual K8s. **Mitigation:** lead with deployment topologies (solo, team, cloud) and let the architecture be a quiet credibility moat.
7. **The temptation to add to the core never goes away.** Every quarter someone will want a seventh thing in the core. **Mitigation:** ADR-required for any core addition, with the burden of proof being "why this *cannot* be a plugin."

### Neutral

- The 16-axis taxonomy *will* evolve. Some axes will be merged, some will be split, some will be discovered. Expect 2-3 axis-level changes per year. This is fine if each change comes with an ADR.

## Open questions — RESOLVED in [ADR-002](002-founding-tech-stack.md)

The original open questions (Q01-Q06 below) were resolved in ADR-002 on 2026-05-30. Summary:

- **Q01 — Core language:** Go.
- **Q02 — Event bus default:** NATS JetStream, embedded for solo + external for team+.
- **Q03 — Reconciler durability:** Leader election + lease (K8s pattern).
- **Q04 — Plugin authentication to core:** Deferred; tracked as Q13 in ADR-002.
- **Q05 — Manifest source-of-truth:** In-image with operator override.
- **Q06 — Default bundle composition:** Deferred to a future ADR; tracked as Q12 in ADR-002.

See [ADR-002](002-founding-tech-stack.md) for the full reasoning + the rejected alternatives.

## Alternatives considered

1. **Status quo: evolve v2 toward maximum decoupling.** Continue ADRs 025+026 in v2. Don't start fresh. **Rejected because:** v2's core has assumptions that can't be fully removed without rewriting the core; the work to remove them approaches the cost of writing a new core. Better to take the lessons from v2 and build clean.
2. **"Pragmatic plugin platform" with thicker core.** Plugins for the long tail; the common case (postgres, web UI, in-process scheduler) hardcoded for simplicity. **Rejected because:** every other data platform has done this and ended up with a fork between "open-source community edition" and "enterprise edition." The thick core is where lock-in lives.
3. **Build on top of K8s directly.** Make every plugin a CRD; the reconciler is a K8s controller; deployment is k8s-native. **Rejected because:** RAT v3 should be deployable without K8s (solo devs especially). Use the K8s pattern (reconciliation) without depending on the runtime.
4. **Build on top of Temporal / Airflow.** Use one of these as the workflow substrate. **Rejected because:** they'd become the lock-in. Scheduler is a plugin axis; we can support them as backends.
5. **Modular monolith with build-time plugin selection.** Plugins compiled in at build time, not loaded at runtime. **Rejected because:** kills runtime extensibility (the whole point). Operators must be able to install/remove plugins without rebuilding the platform.

## Migration

This ADR doesn't migrate anything yet — it's the founding decision for v3. The implementation milestones below were **expanded post-synthesis** (see [reviews/00-synthesis.md](../../../reviews/00-synthesis.md) and [ADR-003](003-two-references-before-contract-freeze.md)) to bake the Critical cross-cutting concerns into the contracts before freeze — they are wire-breaking to retrofit.

- **Phase 0 (4-6 months) — Lock the contracts.** Spec the `plugin.yaml` JSON Schema + ~20 axis protos **with the Critical cross-cutting concerns baked in**: trace/correlation context (W3C `traceparent` mandatory on every RPC + every event), plugin-to-core authentication primitive (per-plugin token + mTLS-ready), resource asks/limits as mandatory manifest fields, state-gateway per-plugin namespacing, capability enforcement (declared = enforced at runtime), conformance suite obligations per axis, tenancy as a structural dimension (not advisory hook), API gateway listener split (internal vs public — port v2 ADR-019). **No data-plane contract may freeze without two independent reference implementations** ([ADR-003](003-two-references-before-contract-freeze.md)) — Phase 0 produces 12 reference plugins (6 critical axes × 2 impls) that stress-test the contracts on golden data. Per-RPC latency benchmark runs against references; if amplification is too high, contracts are revised before freeze. Author-facing prose (`CONTRACT.md`) alongside each proto. External peer review by plugin-system veterans before `rat/1` is tagged. Contracts ship as `v1-preview` during this phase. **See [docs/architecture/phase-0-detail.md](../../../roadmap/phases.md) and [roadmap/phases.md](../../../roadmap/phases.md).**
- **Phase 1 (3 months) — Build the core.** ~12-15k LOC of Go. The six things — registry, identity gateway, state gateway, event bus, reconciler, API gateway — implementing the Critical cross-cutting concerns from the contracts. Native observability (`/metrics`, OTel spans) independent of any observability plugin. Reconciler with backoff + jitter + per-pipeline fairness (the K8s lessons). Mandatory audit emission. **See [roadmap/phases.md](../../../roadmap/phases.md).**
- **Phase 2 (2 months) — Solo deployment reference plugins.** Promote the Phase 0 reference plugins to production quality for the solo bundle (sqlite state-backend, in-process scheduler, local-fs storage, embedded duckdb engine, embedded pyarrow runtime, embedded iceberg format, full-refresh strategy, web-portal UI, community-marketplace). Goal: `rat init && rat run my-pipeline.yaml` works end-to-end. Plugin scaffolding (`rat plugin new`) + local dev loop (`rat dev --plugin`) ship here.
- **Phase 3 (2 months) — Self-hosted team reference plugins.** Postgres state-backend, docker deployment-runtime, S3 storage, Nessie/Lakekeeper catalog, OIDC identity, Prometheus observability. Match the operational shape of v2's "self-hosted team" topology.
- **Phase 4 (3 months) — Hardening + GTM motion.** Upgrade safety (version skew + preflight + reversible state migrations), backup/restore with consistent backup set, plugin publish + Sigstore signing, deprecation governance, supply-chain trust gates. **In parallel:** the GTM work the synthesis flagged (anti-lock-in positioning, first-five-minutes wow via `demo-loader` port, design-partner program, dbt→RAT migration path). The 4-of-5 non-engineering GTM gaps land here, not as an afterthought.
- **Phase 5 (1-2 months) — Ecosystem moves.** Marketplace plugin UX, third deployment topology (hybrid/cloud), multi-UI story (CLI, Slack bot, VS Code extension), portal slot ecosystem.

## Related

- v2's ADR-024 (decoupled data architecture) — first principled decoupling step; format-as-capability decision is revised here (format is a full axis).
- v2's ADR-025 (on-demand decoupled planes) — pioneered the plane-runtime-proxy + plane-manager pattern; reused in v3 as `deployment-runtime` plugin + the reconciler's plane lifecycle.
- v2's ADR-026 (plugin capability manifest) — the manifest pattern adopted here is a direct evolution.
- v2's ADR-019 (internal listener split) — pattern for ratd hosting a privileged subsystem; v3 generalizes via the auth model.
- v2's ADR-009 (container executor) — closest existing analog of ratd spawning a container on demand; v3's `deployment-runtime` axis is its generalization.
- [docs/vision.md](../../vision.md) — the broader thesis this ADR formalizes.
- [docs/architecture/overview.md](../overview.md) — the integrated architecture.
- [docs/conversations/2026-05-30-the-vision-conversation.md](../../conversations/2026-05-30-the-vision-conversation.md) — the session where this decision emerged.
- Prior art summary in [research/prior-art/](../../../docs/research/prior-art/) — K8s, OSGi, VSCode, NATS, Temporal.
