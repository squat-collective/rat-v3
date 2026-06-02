# ADR-020: The data platform bundle — `platform/`, medallion conventions, VS Code + CLI

## Status: Accepted (2026-06-02)

> The Phase-2 "solo bundle" made concrete: a single `platform/` folder that assembles the
> v3 plugin core + the data-dev reference plugins into a runnable, batteries-included data
> platform — a landing zone, DuckDB + DuckLake, editable SQL/Python models on a medallion
> architecture, data-quality tests, driven by `rat serve` and edited through the VS Code
> extension + `ratctl`. It is the v2 product (`ratatouille-v2`) rebuilt on the v3 architecture,
> with **VS Code + CLI replacing the web portal**.

## Context

[phases.md](../../../roadmap/phases.md) **Phase 2 (Solo deployment)** is "promote the reference
plugins to a usable solo bundle: `rat run` end-to-end + a one-command front door." [ADR-019](019-rat-serve-daemon.md)
delivered the runnable core (`rat serve`) + the containerized daemon, and the
[data-dev-plane experiment](../../../experiments/data-dev-plane/README.md) produced the reference
plugins (engine `duckdb-ml`, catalog `ducklake`, storage `minio-s3`, strategy `incremental-embed`,
ui `vscode-rat`). What's missing is the **product**: the thing a solo dev opens and *uses*.

**v2 lineage.** `ratatouille-v2` shipped this shape — landing zones (its ADR-013), merge
strategies (ADR-014), a query service (ADR-006), and a **web portal**. v3 rebuilds it on the
plugin core and **replaces the portal with the VS Code extension + the `ratctl` CLI** — clients
of the orchestrator's gateway, not a bespoke web app. The lesson carried over: a data platform
is mostly *conventions over a lake* (where raw lands, how layers are built, how quality is
checked) plus an *editor*. v3 keeps the conventions at the **project/plugin level**, not in the
six-thing core.

**Most of the plumbing already exists.** The bundle is largely *assembly*:

| Capability | Reuses | New? |
|---|---|---|
| docker compose stack | `core/Dockerfile` + ADR-019 Phase C | stack needs **attach mode** (the keystone) |
| config / plane | `plane.yaml` (ADR-019) | — |
| DuckDB engine | `examples/engine/duckdb-ml-py` | — |
| DuckLake catalog | `examples/catalog/ducklake-py` | — |
| landing zone (raw CSV) | — | **new** convention (v2 ADR-013) |
| edit SQL/Python/dbt | `incremental-embed` strategy | needs a **model/transform runner** |
| medallion orchestration | strategies + the gateway | **new** bronze/silver/gold conventions |
| data-quality tests | — | **new** (greenfield in v3) |
| VS Code editor | `vscode-rat` | repoint at the real gateway |
| CLI | `ratctl` (ADR-019) | — |

**Two hard constraints** found while scoping (they shape the build order, not the design):
1. **F9 — the engine's Arrow result leg is in-process only** (no network endpoint; `streams.py`
   uses `inproc://arrow`). A *containerized* engine cannot return query *rows* over the network
   without the (out-of-scope) Flight leg. Control hops (execute/commit) route through the gateway
   fine; bulk result display uses a co-located read or the BFF data-leg.
2. **Cross-container DuckLake sharing needs Postgres + MinIO** — the frozen `LaunchSpec` has no
   volume mount and podman gives each plugin its own netns + private `/data`, so two plugin
   *containers* can only share a lake over the network. Local-process plugins share the host FS
   and need neither.

These make the *containerized* stack the heavy step and the *local* bundle the cheap, reliable
first slice — hence the build order below.

## Decision

Ship a single self-contained bundle folder, `platform/`, and build it in **four working slices
(M1→M4)**, each independently runnable. The core stays six things; everything here is
project-level convention + existing plugins.

### The folder

```
platform/
├── compose.yaml          # the stack: rat serve + engine + catalog + Postgres + MinIO (attach mode)  [M2]
├── rat.yaml              # daemon config (addr/port, runtime)
├── plane.yaml            # the plugin set for this platform
├── landing/              # ← drop raw CSVs here (the ingestion source)
├── project/
│   ├── bronze/           # raw → typed (ingest models: read_csv → tables)
│   ├── silver/           # cleaned / conformed (SQL or Python models)
│   ├── gold/             # business marts
│   └── tests/            # data-quality assertions                                                    [M3]
├── pipelines/            # orchestration: which models run, in what order
└── README.md             # getting started → a working platform
```

### Conventions

- **Medallion.** `landing/` (raw files) → **bronze** (ingested as-is, typed) → **silver**
  (cleaned/conformed) → **gold** (business marts). Each layer is a set of tables in the lake,
  built by **models**.
- **Models are files.** A model is a `.sql` file (a `SELECT` materialized as a table) **or** a
  `.py` file (returns/writes a table). **Start with SQL + Python; `dbt` is a future model "kind"**
  added later (ADR open question Q01) — not a day-one dependency.
- **Pipelines are declared, executed via the gateway.** A pipeline names the models to run in
  order; the runner issues each as `rat://engine/v1/execute` (and `rat://catalog/v1/commit-table`)
  **through the real `rat serve` gateway** — so every layer build is C5-authorized + audited,
  exactly like any other command. The runner is a thin orchestrator (a client of the gateway,
  like `ratctl`), **not** new core surface.
- **Data-quality tests** live in `project/tests/` as assertions over tables (row counts, not-null,
  uniqueness, referential checks). A test runner evaluates them via the engine and reports
  pass/fail. Whether this is a dedicated plugin axis or a strategy/convention is Q03.
- **Edited in VS Code, driven by the CLI.** `vscode-rat` browses the lake + medallion layers,
  edits model files, runs pipelines, and shows test results; `ratctl` is the scriptable path.
  Together they replace the v2 portal.

### Build order (each a working slice)

- **M1 — scaffold + a *local* medallion demo.** The `platform/` folder with a sample CSV in
  `landing/`, a bronze→silver→gold pipeline run by `rat serve --plane plane.yaml` (local runtime)
  and a thin runner issuing the models through the gateway. Data verified by a co-located read of
  the shared local DuckLake (sidesteps F9 + needs no infra). *Makes the platform tangible.*
- **M2 — containerize it (keystone: attach mode).** Build the deferred `endpoint:`/attach path
  (`supervisor.Attach`) so `rat` runs in a container and connects to sibling plugin containers,
  then `compose.yaml` brings up rat + engine + catalog + **Postgres + MinIO** on one network.
  `compose up` → a working platform. The one genuinely risky piece (rootless-podman networking).
- **M3 — data-quality tests.** `project/tests/` + a runner that asserts against tables and reports.
- **M4 — VS Code.** Repoint `vscode-rat` at the running stack: browse layers, edit models, run
  pipelines, view test results.

## Consequences

- **Realizes Phase 2 — the product, not a demo.** A solo dev clones `platform/`, drops a CSV,
  edits models, runs the medallion pipeline, checks quality — through `rat serve` + VS Code/CLI.
- **Mostly assembly.** Engine, catalog, storage, UI, CLI, serve, daemon-image all exist; the new
  work is the *conventions* (landing/medallion/quality) + a thin transform runner + attach mode.
- **The core stays six things.** Landing zones, medallion layers, model files, quality tests, and
  the runner are all **project/plugin-level**. No new core responsibility (no temptation logged).
- **Cost / negatives (accepted):**
  - **Attach mode (M2) is a real, deferred build** and carries the rootless-podman networking risk
    (plugins↔infra, rat↔plugins). M1 deliberately doesn't depend on it.
  - **F9 stands:** bulk result *display* from a containerized engine still needs the BFF data-leg
    or a co-located read until a real Flight engine exists. The gateway remains the *control* plane.
  - **New conventions to maintain** — the medallion/model/test contracts are project surface that
    must stay documented as they evolve.
  - The transform-runner + quality-test shapes are **not yet contract-frozen** (Q02/Q03) — they
    firm up as M1/M3 land.

## Alternatives considered

1. **Keep a web portal (v2's shape).** Rejected: v3's UX bet is the editor + CLI as gateway
   *clients*; a bespoke web app is a heavier, separate surface. A web UI can return later as just
   another client of the same gateway (the `ui` axis), not a prerequisite.
2. **Big-bang the whole bundle.** Rejected: the containerized stack (attach mode + Postgres/MinIO)
   is the risky long pole; gating a *visible* result behind it is the wrong order. M1 (local) makes
   the platform real first; M2–M4 plug into the same folder.
3. **dbt from day one.** Deferred (Q01): SQL + Python models cover the medallion shape with zero
   extra dependency; `dbt` becomes a model "kind" once the model/runner contract is proven.
4. **A transform/quality engine in the core.** Rejected on the six-thing discipline: orchestration
   is a *client* of the gateway (like `ratctl`); quality is models + assertions. Neither is core.

## Open questions

- **Q01 — dbt timing.** RESOLVED: not day one. SQL + Python models first; `dbt` as a later model
  kind once the runner/model contract is stable.
- **Q02 — the transform runner's home.** A standalone client orchestrator (like `ratctl`/the
  run-*.py scripts) for M1, OR promoted to a `strategy` plugin so pipelines are themselves
  capability-invocable. Decide after M1 shows the shape.
- **Q03 — data-quality as a new axis vs a convention.** A dedicated quality/test plugin axis, or a
  `project/tests/` convention evaluated by the runner over the engine. Decide at M3.

## Related

- [ADR-019](019-rat-serve-daemon.md) — `rat serve` (the orchestrator) + `ratctl` + the daemon image: the runtime this bundle assembles.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime (local + podman) the stack launches/attaches through.
- [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the registry + invoke gateway every pipeline hop routes through (C5 + audit).
- [`experiments/data-dev-plane/README.md`](../../../experiments/data-dev-plane/README.md) — the reference plugins (engine/catalog/storage/strategy/ui) this bundle packages; findings F1–F9.
- v2 prior art: `ratatouille-v2` ADR-013 (landing zones), ADR-014 (merge strategies), ADR-006 (query service), the portal.
- [`ideas/inbox.md`](../../../ideas/inbox.md) — parked: runtime plugin self-registration (orthogonal; revisit at scale).
