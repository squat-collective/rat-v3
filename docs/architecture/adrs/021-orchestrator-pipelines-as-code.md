# ADR-021: rat is a pure orchestrator — pipelines are code, the pipeline-runner axis, infra declares only plugins

## Status: Accepted (2026-06-02) — built + proven (see roadmap/done.md)

> **The product re-thought.** [ADR-020](020-data-platform-bundle.md)'s first build (S1–S4) proved the
> v3 *plumbing* — plugins routed through the gateway, self-driving, quality-gated, with run history.
> But it **baked the pipeline into the infra** (a hardcoded medallion, the model list in a compose env
> var, one global interval). That is not the code-driven platform v2 was. This ADR redirects the
> *pipeline/project model*: **rat orchestrates capabilities and never knows what a "pipeline" is; your
> data work is a dbt project (code) you submit at runtime; the infra declares only plugins.** It keeps
> ADR-020's decoupled stack, scheduler, state-backend, and gateway; it replaces the bespoke
> "model-list strategy" (ADR-020 Q02) with a **pipeline-runner plugin axis**.

## Context

**What v2 actually was — a *code-driven runtime*.** Your project *was* the source of truth: a pipeline
= a model file (`pipeline.sql`/`.py`) + `config.yaml` (merge strategy, schedule, materialization) +
`tests/`, addressed `namespace.layer.name`, linked by `ref()`. The runner discovered the project,
built the DAG, compiled (`ref()`/Jinja), ran each model with its config, ran tests, committed — on
each pipeline's own cron. The portal was just an *editor over the code*. **You edited files; the
platform ran them.** (Essentially **dbt + an orchestrator + an editor**.)

**What ADR-020's first build got wrong.** It conflated "the plugins talk through the gateway" with
"the platform works." The *pipeline* became infrastructure: a fixed set of SQL files, a model list in
`compose.yaml`, a single 20s interval. There is no "project as code that the platform runs" — the very
thing that made v2 usable.

**Tom's requirements for the rethink (this ADR's frame):**
- Fully **decoupled**: rat is an orchestrator *between plugins and interfaces* (and other axes).
- **Plugins can depend on other plugins** (a real dependency graph).
- **dbt** as the pipeline/code language (also Python, others) — with a metadata/Jinja helper.
- **The infra should declare only plugins — nothing else.** Pipelines are not infrastructure.
- **KISS.**

The deep realization: a pipeline is a *workload*, not infrastructure. You `apply` it to the platform
(like `kubectl apply` a Deployment), you don't bake it into the cluster.

## Decision

### The principle

> **rat orchestrates *capabilities*. The platform is a set of *plugins*. Your pipelines are a *dbt
> project* (code) you submit. The infra declares plugins and nothing else.**

rat parses no model, knows no `ref()`, has never heard of "bronze". It routes capability calls,
schedules, records, enforces, and wires plugin dependencies. Everything data-shaped lives in plugins
and in *your code*.

### The layers

```
 interfaces      CLI · VS Code · web          ── connect to rat: submit / trigger / observe
     │
   ┌─▼──────────────────────────────────────┐
   │  rat  (orchestrator — the six things)    │  knows nothing about dbt or data.
   │  route capabilities · schedule · record  │  route · schedule · record · enforce · wire deps.
   │  · enforce · wire requires→provides      │
   └─▲───────────────┬────────────────────────┘
     │ capabilities  │ deps resolved by rat (requires → a peer's provides)
   ┌─┴────────┐ ┌────┴─────┐ ┌─────────┐ ┌──────────┐ ┌────────┐
   │dbt-runner│ │ catalog  │ │ storage │ │scheduler │ │ state  │   ← PLUGINS (the only thing
   │(language)│ │(ducklake)│ │ (minio) │ │  (cron)  │ │  (pg)  │      the infra declares)
   └────▲─────┘ └──────────┘ └─────────┘ └──────────┘ └────────┘
        │ runs
   ┌────┴────────────────┐
   │  YOUR dbt project    │  ← CODE. `rat apply ./project`. NOT infra.
   │  models/ ref() tests │
   └──────────────────────┘
```

### The unlock: the pipeline *language* is a plugin (the `pipeline-runner` axis)

rat does not know dbt — the **`dbt-runner` plugin** does. A `pipeline-runner` plugin `provides
rat://pipeline/v1/run` and, given a project, executes it. Because **dbt brings its own DAG, `ref()`,
Jinja, tests and materializations, rat reinvents none of it** — the single biggest lesson from the
failed first build (which hand-rolled a worse DAG-less runner).

The language is therefore **pluggable**: `dbt-runner` first; a `python-runner` (with a small `rat`
metadata SDK — `ref()`, config, the lake connection) later; SQL-only, Spark, etc. as further runners.
A project picks its runner; rat routes to it. Adding a language = adding a plugin, never touching rat.

### Plugin dependencies = capability composition (no new core magic)

`requires:`/`provides:` (already in the v3 manifest) **is** the plugin dependency graph. The
`dbt-runner` declares `requires: [catalog, storage]`; it does **not** hardcode "Postgres is here, S3 is
there." rat's registry resolves each `requires` to a peer's `provides`, and the runner **composes them
by capability call** at run time — e.g. `storage/vend-credentials` for short-TTL S3 creds, a
`catalog`/`lake` capability for the metadata connection. This is exactly how today's strategy already
calls engine/catalog through the gateway — **dependency injection is just capability composition**, so
it adds no seventh core responsibility (ADR-001 discipline holds).

### Three schemas (KISS)

**1. The plane — infrastructure. ONLY plugins (+ each plugin's own backend wiring). No pipelines, ever.**
```yaml
# plane.yaml — the plugins rat orchestrates. Nothing about your data work appears here.
plugins:
  - { name: dbt-runner, image: rat/dbt-duckdb, requires: [catalog, storage] }
  - { name: catalog,    image: rat/ducklake,   config: { metadata: pg://… } }
  - { name: storage,    image: rat/minio,      config: { endpoint: minio:9000 } }
  - { name: scheduler,  image: rat/cron }
  - { name: state,      image: rat/pg-state }
```

**2. A project — your code. Standard dbt + one rat file. Submitted, never in the plane.**
```
my-analytics/                  # your repo
├── rat.yaml                   # ← the ONLY rat-specific file
├── dbt_project.yml            # standard dbt
├── models/  bronze/ silver/ gold/   ref(), {{ config(...) }}, Jinja
└── models/schema.yml          # standard dbt tests
```
```yaml
# rat.yaml — turns a dbt project into a rat pipeline
kind: pipeline
runner: dbt                    # which runner capability (→ the dbt-runner plugin)
schedule: "0 * * * *"          # rat schedules the refresh (per-project cron)
```
```
rat apply ./my-analytics       # submit the CODE; rat stores it + schedules it
```

**3. The plugin manifest — capabilities + dependencies (already the v3 shape).**
```yaml
kind: pipeline-runner
provides: [ rat://pipeline/v1/run ]
requires: [ rat://catalog/v1/*, rat://storage/v1/vend-credentials ]
```

### How a run happens

1. `rat apply ./my-analytics` → rat records the project + its schedule as **desired state** (in the
   state-backend; the code itself stored via the storage plugin).
2. The **scheduler** plugin fires it on its cron → invokes `rat://pipeline/v1/run(project=my-analytics)`.
3. rat routes that to the **dbt-runner**, having resolved its `requires`; the runner **composes its
   deps**: `storage/vend` for S3 creds, the catalog for the lake connection.
4. The dbt-runner runs `dbt build` — **dbt** does the DAG, `ref()`, Jinja, tests, materializations —
   against the lake.
5. rat records the run in **state**; interfaces (CLI/VS Code) read it, trigger ad-hoc runs, and edit
   the project code.

rat orchestrated *capabilities and a schedule*. It never parsed a model. Swap dbt→python-runner,
ducklake→iceberg, cron→Temporal: **same rat, different plugins.**

### What is reused vs. new

- **Reused (ADR-019/020):** `rat serve` + the gateway (capability routing, C5, audit), the manifest
  `provides`/`requires` model, attach-mode + the compose stack, the scheduler-backend axis, the
  state-backend plugin, DuckLake catalog + MinIO storage.
- **New / redirected:** a **`pipeline-runner` axis** (`rat://pipeline/v1/run` over a *project*); a
  **`dbt-runner` reference plugin** (dbt-core + dbt-duckdb, composing catalog/storage); a **project
  model** (`rat.yaml` + a standard dbt project) submitted via **`rat apply`** (code at runtime, not
  infra); rat resolving `requires`→`provides` as **dependency injection**; **per-project cron** via the
  scheduler axis. ADR-020's bespoke "model-list strategy" (Q02) is **replaced** by the runner axis.

## Consequences

- **rat becomes a true orchestrator** — language-agnostic, data-agnostic. Its value is routing,
  scheduling, recording, enforcing, and wiring deps; nothing data-shaped leaks into it.
- **No reinvented data engine.** dbt is the DAG/`ref()`/Jinja/test/materialization engine; the failed
  hand-rolled runner is deleted, not patched.
- **The infra is tiny and stable** — a list of plugins. Pipelines come and go as `rat apply`'d code;
  the platform never redeploys to gain a pipeline.
- **The language is pluggable** — dbt today, Python/Spark/SQL as further runner plugins; users pick per
  project.
- **It is the purest expression of ADR-001** — even the *pipeline language* is a plugin.
- **Cost / negatives (accepted):**
  - **A new axis + capability** (`pipeline-runner` / `pipeline/v1/run`) and a **project-submission
    mechanism** (`rat apply`, where the code lives, how the runner fetches it) — real new surface.
  - **dbt-duckdb ↔ DuckLake integration** must be made to work (the runner's adapter against
    Postgres-metadata + S3-data); a real engineering task.
  - **Dependency injection by capability** needs a "vend the lake connection" capability so the runner
    can compose catalog+storage (DuckLake fuses them) — a small contract gap (Open question Q2).
  - **Per-project scheduling** finally requires the *full* scheduler-backend axis
    (`Schedule`/`Cancel`/`WatchDue`), not the interval-driver shim.
  - **Redirects part of ADR-020** — its S3/S4 "bespoke pipeline strategy + quality-in-strategy" is
    superseded by the runner axis (dbt owns tests + materialization). S1–S2's decoupled stack +
    self-driving scaffolding remain valid foundations.

## Alternatives considered

1. **The hand-rolled model-list runner (ADR-020's first build).** Rejected: reinvents dbt's DAG/`ref()`
   /Jinja/tests poorly, and bakes the pipeline into the infra. It was the wrong product even though the
   plumbing was right.
2. **dbt inside rat (the core knows dbt).** Rejected hard: couples the orchestrator to one language and
   to data semantics — the opposite of the six-thing discipline. The language must be a plugin.
3. **Pipelines declared in the plane (declarative infra).** Rejected: this *is* the mistake — pipelines
   are workloads (code you `apply`), not infrastructure. The plane lists plugins only.
4. **Project delivery by git-watch** (the runner polls a repo) **vs. `rat apply` upload.** Lean to
   `rat apply` (upload the code into the platform, the runner fetches it) for KISS; git-watch is a
   later source plugin (Open question Q1).
5. **Catalog + storage as two separate deps vs. one fused "lake" capability.** DuckLake fuses catalog +
   storage; a single `lake/vend-connection` capability may be cleaner than composing two. Deferred
   (Q2).

## Open questions

- **Q1 — project delivery.** `rat apply` *uploads* the project (stored via the storage plugin; the
  runner fetches it) vs. a **source plugin** that watches git. Lean: upload first, git-watch as a later
  source-axis plugin.
- **Q2 — the lake-connection capability.** How the dbt-runner gets the DuckLake metadata DSN + data
  path + S3 creds: a `storage/vend-credentials` (exists) + a new `catalog`/`lake` "describe/vend
  connection" capability. Decide whether catalog+storage compose, or a fused `lake` capability vends
  the whole connection.
- **Q3 — the `pipeline/v1/run` contract.** Inputs (project ref, run mode: full/`select`, vars),
  outputs (status, models built, test results, snapshot). Streaming logs vs. unary result.
- **Q4 — Python runner + the metadata SDK.** The `rat` python helper (`ref()`, config, lake
  connection) for a `python-runner` — the ergonomics layer per non-dbt language.
- **Q5 — the project as desired state.** Does `rat apply` write to the state-backend and let the
  reconciler keep it scheduled (K8s-style convergence), and how does `rat delete` retire it?

## Related

- [ADR-020](020-data-platform-bundle.md) — the data platform bundle. This ADR **redirects its
  pipeline/project model** (Q02 bespoke strategy → the pipeline-runner axis) while keeping its
  decoupled stack, scheduler, state-backend, and gateway.
- [ADR-001](001-everything-is-a-plugin.md) — everything is a plugin. This is its purest form: the
  *pipeline language itself* is a plugin.
- [ADR-019](019-rat-serve-daemon.md) — `rat serve` (the orchestrator) + attach mode + the compose
  stack this runs on.
- [ADR-005](005-capability-invocation-model.md) / [ADR-014](014-spike-core-registry-and-invoke-gateway.md)
  — the capability-invoke gateway the runner composes its dependencies through.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime that launches/attaches the plugins.
- `ratatouille-v2` — the code-driven platform (dbt + orchestrator + portal) this re-creates, decoupled.
- [`ideas/inbox.md`](../../../ideas/inbox.md) — runtime plugin self-registration (kin to "projects submitted at runtime").
