# ADR-020: The data platform bundle — v2 rebuilt on v3 plugins, DuckLake catalog, VS Code + CLI

## Status: Accepted (2026-06-02)

> The Phase-2 product: **`ratatouille-v2` rebuilt on the v3 plugin core** — the *same* data
> platform (landing zones → medallion → quality-gated, scheduled refreshes), but every
> responsibility **decoupled into a v3 plugin microservice** behind the capability-invoke
> gateway, with **DuckLake as the catalog** (replacing v2's Nessie/Iceberg) and **VS Code +
> `ratctl` replacing the web portal**.
>
> **It is ALWAYS-ON and SELF-DRIVING.** `rat serve` runs 24/7 as the orchestrator; a
> **scheduler plugin** fires pipeline runs on a cron (hourly refreshes) on its own — no local
> stack to spin up, state lives remote (DuckLake-on-Postgres + data-on-S3). That is v2's
> behavior, now fully plugin-decoupled.

**Sharpened 2026-06-02** (same day, pre-implementation): the first framing built toward a
*local, on-demand* one-shot (run a pipeline by hand) — wrong. The platform must be **always-on +
scheduled + remote**, matching v2. This ADR is re-aimed: the build order is now **S1–S4** (the
always-on decoupled stack), and the **v2→v3 component mapping** below is the spine. The
bundle/medallion/DuckLake/VS-Code-CLI decision is unchanged; the *execution model* is corrected.

## Context

[phases.md](../../../roadmap/phases.md) **Phase 2 (Solo deployment)** wants the *product*: the
thing a solo dev deploys and uses. [ADR-019](019-rat-serve-daemon.md) delivered the always-on
orchestrator (`rat serve`), the containerized daemon, and a client (`ratctl`); the
[data-dev-plane experiment](../../../experiments/data-dev-plane/README.md) produced the reference
plugins (engine `duckdb-ml`, catalog `ducklake`, storage `minio-s3`, a strategy, ui `vscode-rat`).

### What v2 is (the target behavior)

`ratatouille-v2` is a 7-container platform: **`ratd`** (Go brain — REST API + a cron **scheduler**
+ plugin host + executor) drives sidecars: **runner** (Python — compile SQL/Jinja → DuckDB → write
**Iceberg**, run **quality tests**, with **Nessie git-branch isolation** per run), **ratq** (Python
read-only DuckDB query), **portal** (Next.js, the only UI), over **postgres** (pipeline/run/schedule
metadata), **minio** (S3: code + data + landing), **nessie** (Iceberg catalog). The user model:
**Namespace → Pipeline (`{ns}.{layer}.{name}`, SQL/Python, code-in-S3) → Run → Schedule (cron)**,
organized **bronze/silver/gold**, wired with **`ref('silver.orders')`**, **merge strategies**
(full_refresh/incremental/scd2/append/delete_insert/snapshot), **landing zones** for raw drops,
**quality tests** (zero violations = pass; `@severity: error` blocks the branch merge). The scheduler
fires refreshes hourly on its own.

### The v2 → v3 mapping (the spine)

Every v2 responsibility has a clean v3 home — *same behavior, decoupled into plugins, DuckLake catalog:*

| v2 component | did | → v3 |
|---|---|---|
| **ratd** (the brain) | API + scheduler + plugin host + executor | **`rat serve`** — the 6-thing core (registry + gateway + reconciler), the always-on orchestrator |
| ratd **scheduler** (cron tick → runs) | periodic refresh | a **scheduler plugin** (scheduler axis) — fires pipeline runs on cron, *through the gateway* |
| **runner** (compile → DuckDB → Iceberg, quality, branch/merge) | pipeline execution | the **engine plugin** (`duckdb-ml`) + a **pipeline strategy plugin** (medallion + merge-strategy + quality gate orchestration) |
| **ratq** (read-only query) | interactive query | the **engine plugin's** query capability (same engine) |
| **portal** (web IDE) | the only UI | **`vscode-rat` + `ratctl`** (VS Code + CLI) |
| **postgres** (platform metadata) | pipelines/runs/schedules/quality | a **state-backend plugin** |
| **minio** (S3) | code + data + landing | the **storage plugin** (`minio-s3`) |
| **nessie** (Iceberg catalog + git branching) | table catalog | **the DuckLake catalog plugin** (`ducklake`) — *replaces Nessie* **and** subsumes the Iceberg/format write (the engine writes the lake directly); DuckLake branching replaces Nessie branching |
| pipelines/config/tests in S3 | project-as-code | the **`platform/` folder** (models, configs, tests, schedules) |

**Already exist:** engine, DuckLake catalog, storage, `vscode-rat`, `ratctl`, `rat serve` + the
daemon image. **Genuinely new:** the **scheduler plugin**, a **state-backend plugin**, the
**pipeline/medallion strategy** (the runner's brain as a v3 strategy: merge strategies + quality
gate + branch-on-failure-discard), the **always-on compose stack** (attach mode), and the
landing/`ref()`/quality **conventions**.

### Two hard constraints (shape the build order, not the design)

1. **F9 — the engine's Arrow result leg is in-process only** (`streams.py` uses `inproc://arrow`,
   no network endpoint). A *containerized* engine cannot stream query *rows* over the network
   without the (out-of-scope) Flight leg. **Control** hops (execute/commit) route through the
   gateway fine; *bulk result display* uses a co-located read or the BFF data-leg.
2. **Cross-container DuckLake sharing needs Postgres + S3** — the frozen `LaunchSpec` has no volume
   mount, so two plugin *containers* share a lake only over the network: **DuckLake metadata on
   Postgres, data on S3/MinIO**. (This is exactly v2's remote posture — and it's required, not
   optional, for the always-on decoupled stack.)

## Decision

Ship a single self-contained bundle folder, `platform/`, that deploys as an **always-on,
self-driving** stack and is built in **four working slices (S1→S4)**. The core stays six things;
every platform responsibility is a **plugin** or **project-level convention**.

### The folder

```
platform/
├── compose.yaml          # the always-on stack: rat serve + scheduler + engine + catalog + storage + Postgres + MinIO (attach mode)
├── rat.yaml              # platform settings (project, lake, gateway)
├── plane.yaml            # the plugin set rat serve fronts
├── manifests/            # the plugins' manifests (provides/requires → C5)
├── landing/              # ← drop raw files here (the ingestion source)
├── project/
│   ├── bronze/           # raw → typed (ingest models)
│   ├── silver/           # cleaned / conformed
│   ├── gold/             # business marts
│   └── tests/            # data-quality assertions (zero-rows = pass)
├── pipelines/            # pipelines + their schedules (what runs, in what order, how often)
└── README.md
```

### Conventions (the v2 semantics, on v3)

- **Medallion + `ref()`.** `landing/` → bronze → silver → gold, each a set of lake tables built by
  **models**; `ref('silver.orders')` resolves to the lake table — same as v2.
- **Models are files** — `.sql` or `.py`. dbt is a future model kind (Q01), not day one.
- **A pipeline is a strategy.** The runner's brain becomes a **pipeline/medallion `strategy`
  plugin**: given a pipeline (its models + merge strategy + tests), it runs the models via
  `rat://engine/v1/execute`, applies the merge strategy, runs quality tests, and commits the
  snapshot to the DuckLake catalog — all **through the gateway** (C5 + audit), with
  branch-on-failure-discard (DuckLake branching). (Q02 RESOLVED: a strategy plugin, not an ad-hoc
  client — so a *scheduler* can invoke a pipeline as a capability.)
- **Merge strategies** (full_refresh / incremental / append / delete_insert / scd2 / snapshot) +
  **quality tests** (`project/tests/*.sql`, zero rows = pass, severity gates the merge) — v2's
  contract, re-expressed over DuckLake.
- **Scheduled + always-on.** Schedules (cron) live with the pipelines; the **scheduler plugin**
  fires them through the gateway. Deploy once, it refreshes on its own.
- **Edited in VS Code, driven by the CLI.** `vscode-rat` + `ratctl` replace the portal.

### The always-on "hourly refresh", end to end

```
scheduler plugin (cron: "medallion @ hourly")
   └─▶ rat serve gateway (C5 + audit) ──▶ pipeline strategy "run X"
          ├─▶ engine.execute  (compile ref()/Jinja → DuckDB, apply merge strategy)
          ├─▶ writes the lake directly  ──▶ DuckLake catalog (branch → snapshot)
          ├─▶ quality tests (zero rows = pass; failure discards the branch)
          └─▶ catalog.commit-table (the run's snapshot)
   state-backend records run/status/quality · vscode-rat + ratctl observe/edit
```

Same five phases as v2's runner — branch, compile, execute→lake, quality, merge/discard — but
DuckLake owns catalog+branching and every hop is a gateway capability call.

### Build order (each an independently provable slice)

- **S1 — the decoupled stack runs the medallion through `rat serve`, remote.** engine + DuckLake
  catalog + storage + **Postgres (DuckLake metadata) + MinIO (data)**; the medallion run by the
  **pipeline strategy** through the gateway. Proves "v2's pipeline, on v3 plugins, DuckLake catalog."
  Keystone: **attach mode** (`endpoint:` + `supervisor.Attach`) so `rat` connects to sibling
  containers — **no docker-in-docker**.
- **S2 — the scheduler plugin: self-driving refresh.** Fires the medallion on a cron (→ hourly)
  through the gateway. `compose up` → it refreshes on its own.
- **S3 — merge strategies + quality gates.** v2's transformation semantics + branch-on-failure-discard.
- **S4 — state-backend + VS Code/CLI.** Pipelines/runs/schedules metadata in a state-backend plugin;
  `vscode-rat` repointed at the running stack (browse layers, edit models, run/observe).

## Consequences

- **Realizes Phase 2 — v2's product on v3.** An always-on, self-driving data platform a solo dev
  deploys once; it ingests, transforms on a medallion, quality-gates, and refreshes on a schedule —
  fully decoupled into plugins, DuckLake as catalog, edited via VS Code + CLI.
- **Mostly assembly + a few real new plugins.** Engine, catalog, storage, UI, CLI, serve exist; new
  = scheduler plugin, state-backend plugin, the pipeline/medallion strategy, attach mode.
- **The core stays six things.** Scheduler, state, the pipeline strategy, quality, landing, medallion
  are all plugins/conventions — no new core responsibility (no temptation logged).
- **Cost / negatives (accepted):**
  - **Always-on ⇒ the containerized remote stack is mandatory**, not optional — attach mode +
    Postgres + MinIO, with the rootless-podman networking work that implies. There is no local
    shortcut for a *self-driving* stack.
  - **F9 stands:** bulk result *display* from a containerized engine needs a co-located read or the
    BFF data-leg until a real Flight engine exists. The gateway remains the *control* plane.
  - **New plugins to build + maintain** (scheduler, state-backend, pipeline strategy) and new
    conventions (medallion/`ref()`/quality/merge-strategy) that are project surface, not yet frozen.

## Alternatives considered

1. **Keep a web portal (v2's shape).** Rejected: v3's UX is editor + CLI as gateway *clients*; a web
   UI can return later as just another `ui`-axis client, not a prerequisite.
2. **Local, on-demand one-shot** (the initial framing). Rejected (see *Sharpened*): the platform must
   be always-on + scheduled + remote to match v2 — a hand-run local pipeline is the wrong target.
3. **Keep v2's `ratd` monolith** (API+scheduler+executor in one daemon). Rejected: the v3 bet is full
   decoupling — scheduler, state, execution are *plugins* behind the gateway, not core.
4. **Keep Nessie/Iceberg.** Rejected per the explicit goal: **DuckLake is the catalog** — it replaces
   Nessie *and* the format/Iceberg write (engine writes the lake), collapsing two v2 services into one.
5. **dbt from day one.** Deferred (Q01): SQL + Python models first; dbt as a later model kind.

## Open questions

- **Q01 — dbt timing.** RESOLVED: not day one; SQL + Python models first.
- **Q02 — the runner's home.** RESOLVED: a **pipeline `strategy` plugin** (capability-invocable, so
  the scheduler can fire it), not an ad-hoc client.
- **Q03 — data-quality as a new axis vs a convention.** Lean: a `project/tests/` convention the
  pipeline strategy evaluates over the engine (no new axis). Confirm at S3.
- **Q04 — state-backend choice.** Postgres (already in the stack for DuckLake) vs sqlite for the
  platform metadata. Decide at S4; likely reuse the stack's Postgres.
- **Q05 — scheduler plugin shape.** It must invoke a capability on a cron (call back into the
  gateway). Settle the trigger contract at S2 (a thin cron→`strategy.Apply` invoker first).

## Related

- [ADR-019](019-rat-serve-daemon.md) — `rat serve` (the always-on orchestrator) + `ratctl` + the daemon image: the runtime this bundle assembles.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime (local + podman) + attach mode (S1 keystone).
- [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the registry + invoke gateway every pipeline hop routes through (C5 + audit).
- [`experiments/data-dev-plane/README.md`](../../../experiments/data-dev-plane/README.md) — the reference plugins this bundle packages; findings F1–F9 (esp. F9, the in-proc Arrow leg).
- v2 prior art: `ratatouille-v2` — `ratd`/runner/ratq/portal, ADR-013 (landing zones), ADR-014 (merge strategies), ADR-006 (query service), ADR-007 (plugin system).
- [`ideas/inbox.md`](../../../ideas/inbox.md) — parked: runtime plugin self-registration (orthogonal; revisit at scale).
