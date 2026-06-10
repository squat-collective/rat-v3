# 🐀 RAT data platform — the solo bundle

A self-contained, batteries-included data platform built on the RAT v3 plugin core
([ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md)). Drop raw files in a
**landing zone**, and a **dbt project** builds the medallion (bronze → silver → gold) into
a shared **DuckLake** (metadata on Postgres, data on MinIO/S3) — scheduled, quality-gated,
with run history and a web UI, every control hop routed through the `rat serve`
orchestrator. Since [ADR-021](../docs/architecture/adrs/021-orchestrator-pipelines-as-code.md),
**rat knows nothing about pipelines**: your data work is the dbt project in
[`dbt-project/`](dbt-project/), executed by the **dbt-runner plugin** (which embeds the
engine + catalog in-process via dbt-duckdb + DuckLake — the standalone `duckdb-ml`/`ducklake`
plugins graduated to the `rat-data-dev` showcase repo).

## The flow

```
 landing/orders.csv ──▶ dbt-runner: `dbt build` (bronze → silver → gold + dbt tests) ──▶ DuckLake
 (raw drops)            engine + catalog embedded (dbt-duckdb + DuckLake)                meta: Postgres
                                                                                         data: MinIO/S3
 scheduler ──(demo: every 20s)──▶ rat://strategy/v1/apply ◀──(manual)── run.py · bff POST /api/run

 every control hop goes through `rat serve` — C5-authorized + audited. A failing dbt test
 fails the build (FAILED_PRECONDITION): no successful run, the quality gate held.
```

## Folder map

```
platform/
├── plane.yaml             # ATTACH-mode plane: the plugin set rat serve dials (compose runs them)
├── plugins.yaml           # LAUNCH-mode plane: rat launches each plugin from its image (ADR-022)
├── compose.yaml           # the attach-mode demo stack: infra + every plugin + rat serve
├── compose.infra.yaml     # launch-mode infra: Postgres + MinIO ONLY — rat launches the rest
├── manifests/             # the plugins' manifests (provides/requires → C5 policy)
├── dbt-project/           # YOUR pipeline-as-code: dbt models + profiles.yml + rat.yaml (ADR-021)
│   └── models/            #   bronze_orders → silver_orders → gold_daily_revenue (+ schema.yml tests)
├── landing/               # ← drop raw CSVs here (orders.csv: a messy sample)
├── run.py                 # manual trigger: strategy/apply through the gateway, then verify
├── bff.py / bff.Dockerfile  # the UI's backend — JSON over HTTP, control calls via the gateway
├── run-socket-mount.sh    # rat-AS-a-container topology (driven by `make platform-socket`)
├── media/                 # screenshots
└── .rat/                  # runtime junk (daemon log/audit/data) — gitignored
```

**Choosing between these front doors** (attach vs launch vs socket-mount vs a `rat.toml`
project) is its own topic: see the
[building-a-platform guide](../docs/guides/building-a-platform.md).

## Run it — three modes, one platform

**1. Attach mode (the always-on compose stack):** compose runs *everything* — Postgres,
MinIO, and each plugin (dbt-runner, state, scheduler, bff) as sibling containers — and
**`rat serve` runs in its own container and ATTACHES to the plugins by service name**
(no docker-in-docker; see [`plane.yaml`](plane.yaml)).

```
make platform-up        # bring up the stack (Postgres+MinIO + dbt-runner/state/scheduler/bff + rat serve)
make platform-run       # trigger the medallion once through the gateway (then it self-refreshes anyway)
make platform-down      # tear it all down (and the volumes)
```

**2. Launch mode (rat on the host):** the infra compose carries **only Postgres + MinIO**;
rat launches every plugin container itself from [`plugins.yaml`](plugins.yaml) — adding a
plugin is one entry + an image, and the reconciler self-heals crashes
([ADR-022](../docs/architecture/adrs/022-plugins-are-launched-not-composed.md)).

```
make plugin-images                                      # build rat/{state,secret,scheduler,dbt-runner,bff}:dev
podman compose -f platform/compose.infra.yaml up -d     # Postgres + MinIO only
( cd platform && /path/to/bin/rat serve --plane plugins.yaml )
podman compose -f platform/compose.infra.yaml down -v   # teardown (Ctrl-C rat first)
```

This mode adds the **secret plugin**: plugin entries carry only `ref://` strings
(`RAT_STATE_PG_REF`, `RAT_LAKE_PG_REF`, …) and resolve them via `rat://secret/v1/resolve` —
no credentials in the plane file.

**3. Socket-mount (everything containerized):** rat itself runs as a container, drives the
**host's** podman over the mounted user socket, and launches the plugins as siblings on a
shared network, dialed by name — the k8s-shaped topology. Requires
`systemctl --user enable --now podman.socket`.

```
make platform-socket            # infra + rat-as-a-container + the sibling plugins
./platform/run-socket-mount.sh runs   # read the run history from the bff, by name, via a peer
make platform-socket-down       # tear it all down
```

## Watch it work

- **The UI:** in attach mode the bff is published on the host — `http://localhost:8088`
  (`GET /api/runs`, `/api/tables`, `/api/table/<name>`, `/api/ui?surface=vscode`;
  `POST /api/run`, `/api/invoke`). In launch/socket modes it serves on its launched plugin
  port (no fixed host port — `podman ps` to find it, or use `run-socket-mount.sh runs`).
- **The control hops** — every capability call is C5-authorized + audited by the core.
  Attach mode: `podman logs rat-platform-rat-serve-1 | grep capability`. Launch mode: the
  audit lines are on rat's own stdout.
- **The scheduler** fires `strategy/apply` every 20s in the demo
  (`podman logs rat-platform-scheduler-1` in attach mode); run history lands in the
  Postgres-backed state plugin and is read back through the gateway (`/api/runs`, `run.py`).

## Bring your own pipeline

The dbt project is **code you submit, not infrastructure** (ADR-021). In attach mode the
runner reads `dbt-project/` straight from the repo mount — edit a model and re-trigger. In
launch mode the runner executes the **applied** project from the state-backend
(`RAT_PROJECT_KEY=projects/medallion`; the image bakes this sample as a fallback) — submit
yours through the gateway:

```
rat apply --project platform/dbt-project --name medallion    # stored at projects/medallion
```

Add data the same way: drop a CSV in `landing/`, add a `bronze_*.sql` model that reads it,
chain `silver`/`gold` models with `ref()`, and declare tests in `models/schema.yml` —
`dbt build` runs models + tests, and a failing test blocks the run.

## Status

[ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md)'s slices S1–S4b are all
built (decoupled attach stack · self-driving scheduler · quality gates · state-backend run
history · bff UI control-path), then re-aimed twice: pipelines became **code you submit**
([ADR-021](../docs/architecture/adrs/021-orchestrator-pipelines-as-code.md) — the dbt-runner
axis replaced the bespoke SQL-medallion strategy), and plugins became **launched, not
composed** ([ADR-022](../docs/architecture/adrs/022-plugins-are-launched-not-composed.md) —
launch mode + socket-mount). The VS Code extension *UI itself* remains the follow-on; the
bff already assembles per-surface contributions (`/api/ui`,
[ADR-024](../docs/architecture/adrs/024-ui-assembled-from-plugin-contributions.md)).
