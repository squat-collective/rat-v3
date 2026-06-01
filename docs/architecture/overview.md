# Architecture Overview — RAT v3

The full architecture in one document. ADRs in `docs/architecture/adrs/` resolve specific decisions; this doc gives the integrated picture.

## The shape

```
┌──────────────────────────────────────────────────────────────────────┐
│                         CORE (5-10k LOC)                              │
│                                                                       │
│   ┌─────────────┐  ┌──────────────────┐  ┌─────────────────────┐    │
│   │ Registry    │  │ Identity gateway │  │ Reconciler loop     │    │
│   │ (manifests, │  │ (delegates to    │  │ (desired → actual)  │    │
│   │  capabilities)  │  auth plugin)    │  │                     │    │
│   └─────────────┘  └──────────────────┘  └─────────────────────┘    │
│                                                                       │
│   ┌─────────────┐  ┌──────────────────┐  ┌─────────────────────┐    │
│   │ State       │  │ Event bus        │  │ API gateway         │    │
│   │ gateway     │  │ (sync + async)   │  │ (gRPC + REST)       │    │
│   │ (delegates) │  │                  │  │                     │    │
│   └─────────────┘  └──────────────────┘  └─────────────────────┘    │
└──────────────────────────────────────┬───────────────────────────────┘
                                       │
        ┌──────────────────────────────┼──────────────────────────────┐
        │                              │                              │
        ▼                              ▼                              ▼
┌────────────────────┐    ┌────────────────────┐    ┌────────────────────┐
│  DATA-PLANE        │    │  CONTROL-PLANE     │    │  EXPERIENCE        │
│  PLUGINS           │    │  PLUGINS           │    │  PLUGINS           │
│                    │    │                    │    │                    │
│  engine            │    │  state-backend     │    │  ui (web/cli/      │
│  runtime           │    │  secret-backend    │    │      slack/vim)    │
│  format            │    │  scheduler-backend │    │  notifications     │
│  strategy          │    │  identity          │    │  ide-extension     │
│  catalog           │    │  tenancy           │    │  marketplace       │
│  storage           │    │  billing           │    │                    │
│  deployment-runtime│    │  observability     │    │                    │
│                    │    │  audit-log         │    │                    │
└────────┬───────────┘    └────────────────────┘    └────────────────────┘
         │ direct point-to-point gRPC
         │ (bytes never flow through core)
         ▼
┌──────────────────────────────────────────────────────────────────────┐
│   physical infra: S3, postgres, compute, message queues, etc.        │
└──────────────────────────────────────────────────────────────────────┘
```

## The six core things

Each is irreducible — i.e., it can't itself be a plugin because of chicken-and-egg.

1. **Registry** — reads manifests, indexes by `(kind, name, version)`, answers capability lookups. Backed by state-backend plugin for persistence but the lookup primitive is in-core.
2. **Identity gateway** — every request carries an identity. Validation delegates to identity plugin (or anonymous-root if none). Auth-check decision logic is plugin-side; gateway routing is core.
3. **State gateway** — `Get/Put/Watch/List` interface. Implementation is state-backend plugin (postgres, sqlite, dynamodb, etcd, in-mem). Interface is core because the registry needs it.
4. **Event bus** — publish/subscribe for async coordination + observability. Backed by NATS/Kafka/Redis-Streams plugin. Protocol is fixed; transport is pluggable.
5. **Reconciler** — reads desired state (manifests, planes, pipelines), compares to actual, drives convergence. Kubernetes controller pattern.
6. **API gateway** — gRPC + REST entry point. Authenticates via identity gateway; routes to internal handlers or proxies to plugins.

Total LOC budget: **5-10k**. Probably Rust (for performance + memory safety) or Go (for ecosystem). Comparable in size to etcd, NATS, Temporal's core. (Core language locked to **Go** — [ADR-004](adrs/004-core-language-go.md).)

> **Tier 0 — the bootstrap-critical plugins.** Three of the plugins the six things delegate to are load-bearing for the core to *start*, so they get a documented exception: the **state-backend** (the state gateway's implementation — the registry can't index without it), the **deployment-runtime** (which launches every *other* plugin process), and the **event-bus transport**. These are still plugins — different implementations are possible — but they are selected **at boot**, not hot-swapped at runtime like the rest, and they get extra rigor. Do not market them as "swap while running." This is named honestly in [`.claude/rules/plugin-architecture.md`](../../.claude/rules/plugin-architecture.md) ("the hidden tier 0") and [reviews/01](../../reviews/01-adversarial-architect.md) Finding 6 — and now here in the front-door doc too.

## The plugin axes

Open-set `kind:` in the manifest. Ship-day v1 has these (community can add more):

### Data-plane plugins (touch user data)
- **engine** — SQL → Arrow (DuckDB, ClickHouse, Spark, Trino, BigQuery)
- **runtime** — Arrow → Arrow ops (PyArrow, Polars, Datafusion, Pandas)
- **format** — Arrow ↔ on-disk + metadata (Iceberg, Delta, Hudi, raw Parquet, raw CSV)
- **strategy** — composes format + runtime to write a snapshot (full_refresh, scd2, soft_delete, incremental)
- **catalog** — table identity, branches, snapshot indexing (Nessie, Lakekeeper, Unity, Glue, Hive, HMS)
- **storage** — credentials + bytes (S3, GCS, Azure, MinIO, local-FS, IPFS, R2)
- **deployment-runtime** — where plugins actually run (local-process, docker, podman, k8s, nomad, lambda, fargate)

### Control-plane plugins (backbone for orchestration)
- **state-backend** — postgres, sqlite, dynamodb, etcd, in-memory
- **secret-backend** — env, Vault, AWS-SM, GCP-SM, sealed-secrets
- **scheduler-backend** — in-process cron, Temporal, Airflow-bridge, k8s-CronJob
- **identity** — anonymous, password, OAuth, OIDC, SAML, Keycloak
- **tenancy** — none, namespace, org, hierarchical
- **billing** — none, usage-metered, seat-based, custom
- **observability** — stdout, prometheus, otel, datadog, cloudwatch
- **audit-log** — file, postgres, splunk, kafka, none

### Experience plugins (human interface)
- **ui** — web-portal, cli, slack-bot, vim, vscode-ext
- **notifications** — slack, email, pagerduty, webhook
- **ide-extension** — language-server-protocol bindings
- **marketplace** — plugin distribution + discovery (github-actions, internal registry, enterprise vendor)

**16-19 axes at v1.** Open-set means more can land without core changes.

## The contract triple

Three orthogonal contracts; together they define the platform's wire shape.

### 1. Proto files (gRPC service contracts)

Generated SDKs in Go, Python, Rust, TypeScript, Java published by the core team. Plugins in any language.

Examples:
- `engine/v1/engine.proto` — Execute, Query, Preview
- `format/v1/format.proto` — Write, Resolve, Maintain
- `catalog/v1/catalog.proto` — CreateBranch, GetTable, MergeBranch
- `storage/v1/storage.proto` — VendCredentials
- `identity/v1/identity.proto` — Authenticate, Authorize
- `state/v1/state.proto` — Get, Put, Watch, List
- `tenancy/v1/tenancy.proto` — DecisionHook
- `ui/v1/ui.proto` — what an experience plugin exposes
- … (one per axis + the decision-hook protos for permission/sharing/audit)

### 2. plugin.yaml manifest (`plugin/v1` schema)

```yaml
plugin:
  id:          rat-strategy-scd2
  version:     0.3.0
  api_version: rat/1
  kind:        strategy

provides:
  - kind: strategy
    name: scd2
    version: v1
    schema: { options: {...} }

requires:
  - kind: format
    capabilities: [scan, merge, append]
    version: v1
  - kind: runtime
    version: ">=v1"

suggests:
  - kind: format
    names: [iceberg, delta, hudi]

contributes:
  - kind: portal-slot
    slot: pipeline-strategy-configurator
    component: SCD2Configurator
```

JSON-Schema-validated. One schema for every plugin kind. Plugin authors edit one file.

### 3. URI-shaped capability strings

`rat://strategy/v1/scd2`, `rat://format/v1/merge`, `rat://com.example/their-capability/v1/...`

Naturally namespaced (community avoids collisions), versioned, lookup-able. Open-set — never enumerated centrally.

## Communication model

**Sync gRPC** for request-response with strong typing: `format.Resolve(ref)`, `catalog.GetTable(ref)`, `storage.VendCredentials(prefix)`. Use when you need an answer now.

**Async event bus** for coordination + observability: `pipeline_run_started`, `plugin_installed`, `plane_warmed`. Use when N consumers should react without coupling.

Both are pluggable transports:
- gRPC backend = HTTP/2; can be swapped (e.g., Connect over HTTP/1.1 for browsers).
- Event bus backend = NATS / Kafka / Redis Streams — chosen by deployment topology.

**Data plane bypasses core for bytes.** Engine ↔ Storage is direct S3 traffic. Engine ↔ Format is small RPCs (file lists, metadata). Runner never sees data. Core never sees data. The core is a coordination point, not a chokepoint.

## The reconciliation model

Kubernetes controller pattern, applied to data:

```
Operator declares (via UI or API):
  - planes:        [name, axis-bindings]
  - pipelines:     [name, plane, source, strategy, schedule]
  - subscriptions: [event, action]

Reconciler loop (every N seconds):
  - For each declared pipeline:
      - If scheduled to run now: check the plane is healthy.
        If not: record the missing axes as desired-running in plane state.
        Emit `pipeline_run_requested` event.
  - For each subscription:
      - If the triggering event happened: emit the corresponding action event.
  - For each plugin process:
      - Verify healthcheck. If failed, record it not-ready in desired state.

Plugins react to desired state + events:
  - deployment-runtime converges actual → desired plane: it launches the missing
    axes and restarts the unhealthy ones (it owns process lifecycle via its
    Launch/Terminate/Healthcheck capabilities — the core records what SHOULD run,
    it never spawns a process itself).
  - Engine subscribes to `pipeline_run_requested` for its plane → runs work.
  - Observability subscribes to `*_completed` → pushes metrics.
  - Notifications subscribes to `*_failed` → alerts Slack.
```

**The core never tells anyone to do anything.** It maintains the truth of "what should be true" and lets plugins react — even process lifecycle: the reconciler records the desired plane and the **deployment-runtime** plugin converges to it (the core never spawns a process itself). This is profoundly more scalable than imperative orchestration because plugins can be added/removed/replaced without core changes.

## Deployment topologies (same core, different plugin sets)

| Topology | State | Auth | Deploy-runtime | Storage | Engine |
|---|---|---|---|---|---|
| **Solo** (single binary, laptop) | sqlite | anonymous | local-process | local-FS | duckdb-embedded |
| **Self-hosted team** | postgres | oidc-keycloak | docker | minio/s3 | duckdb-container |
| **Hybrid (on-prem control, cloud data)** | postgres | oidc-okta | k8s on-prem + eks data | s3-aws | spark-on-eks |
| **Full cloud (SaaS-style)** | dynamodb¹ | auth0 | aws-fargate | s3 | snowflake or bigquery |

¹ DynamoDB backs a **multi-replica** control plane only in **strongly-consistent read mode** — its default eventually-consistent reads would let two replicas both win the leader-election CAS (split-brain). The state-backend conformance suite gates on single-key linearizable CAS + ordered Watch; an eventually-consistent backend must run strongly-consistent or declare itself solo-only. See `contracts/proto/rat/state/v1/state.proto` (reviews/06 C-4 / freeze-blocker #3).

Same `rat` binary; different `plugin.yaml` set. **No fork between freemium-open-source and multi-tenant-SaaS.**

## Scalability

- **Solo:** single binary, in-process plugins, sqlite state. 30MB, boots <1s.
- **Team (10-100 users, single-tenant):** core + postgres + plugin containers. GB-TB tables, tens of pipelines/sec.
- **Enterprise (1000+ users, multi-tenant, hybrid):** core replicated behind LB; state in dynamodb or sharded postgres; plugins span on-prem + cloud. PB tables, thousands of concurrent pipelines.

The core itself doesn't change between scales. It's *plugin composition* that changes.

**The core stays stateless** (state in state-backend plugin). Stateless = horizontally scalable. N core replicas behind a load balancer; the reconciler loop uses optimistic concurrency on shared state (the K8s controller pattern).

## What stays the same as RAT v2

- **The pipeline model** (SQL/Python pipelines, branch-isolated runs, quality tests pre-merge) is sound; we keep it.
- **Descriptors as the glue** (TableDescriptor, CatalogDescriptor, StorageDescriptor) — same idea, slightly cleaner protos.
- **Trust model** for second-party Python pipelines (ADR-017 in v2) — applies equally.
- **Brutalist-but-functional UI philosophy** — bundle.js, slot system, no build step.

## What changes from RAT v2

- **State is a plugin axis.** Postgres becomes the default backend, not the mandatory one.
- **The portal is a plugin.** One of many `ui` plugins.
- **The scheduler is a plugin axis.** In-process cron is default; Temporal/Airflow/k8s-CronJob can swap in.
- **Auth is a plugin axis.** Anonymous-root is default; OIDC/OAuth/SAML/Keycloak swap in.
- **Multi-tenancy is a plugin axis.** None is default; org/team/hierarchical swap in.
- **Deployment runtime is a plugin axis.** Docker is default; k8s/nomad/lambda swap in.
- **Manifests replace ratd's bespoke proto + 5 entry_points groups + broker JSON.** Single contract.
- **Event bus is first-class.** Today's ratd:8090 internal listener becomes a NATS-style stream.
- **No language-specific SDKs.** Contract is `.proto` + `plugin.yaml`. SDKs are generated.

## Known unknowns — RESOLVED in [ADR-002](adrs/002-founding-tech-stack.md)

All ten Q01-Q10 below were resolved on 2026-05-30. Summary:

- **Q01 — Core language:** Go.
- **Q02 — Event bus default:** NATS JetStream, embedded.
- **Q03 — Manifest validation:** Standalone JSON Schema, proto referenced by URI.
- **Q04 — Capability versioning:** Major version only (K8s-style).
- **Q05 — Reconciler durability:** Leader election + lease.
- **Q06 — Manifest source:** In-image with operator override.
- **Q07 — v2 migration:** No plan; build a tool reactively if a real production user surfaces.
- **Q08 — Cross-engine queries:** Core raises typed errors; federation is plugin territory.
- **Q09 — Marketplace:** Plugin axis; community marketplace plugin in default solo bundle.
- **Q10 — License:** Apache 2.0.

See [ADR-002](adrs/002-founding-tech-stack.md) for full reasoning + rejected alternatives. New questions raised by these decisions (Q11-Q15, e.g. plugin sandboxing, default bundle composition) are deferred to future ADRs; tracked in [ideas/inbox.md](../../ideas/inbox.md).
