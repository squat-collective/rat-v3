# 🐀 RAT data platform — the solo bundle

A self-contained, batteries-included data platform built on the RAT v3 plugin core
([ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md)). Drop raw files in a
**landing zone**, build a **medallion** (bronze → silver → gold) of editable SQL/Python
models, check **data quality**, and drive it all through the `rat serve` orchestrator —
edited in VS Code, scripted with `ratctl`. It's the v2 product rebuilt on v3, with
**VS Code + CLI replacing the web portal**.

## The flow

```
 landing/*.csv ──▶ bronze ──▶ silver ──▶ gold ──▶ project/tests/ (quality)
 (raw drops)      (ingested   (cleaned/   (business
                   as-is)      conformed)  marts)

 every layer built by issuing rat://engine/v1/execute through `rat serve`
 (C5-authorized + audited) — the pipeline is just commands to the orchestrator.
```

## Folder map

```
platform/
├── rat.yaml              # platform settings (read by the runner): gateway, project, lake
├── plane.yaml            # the plugin set `rat serve` brings up (engine + catalog + runner)
├── manifests/            # the plugins' manifests (provides/requires → C5)
├── landing/              # ← drop raw CSVs here
│   └── orders.csv        #   a messy sample (mixed-case status, nulls, a duplicate)
├── project/
│   ├── bronze/orders.sql        # raw → typed (read_csv from the landing zone)
│   ├── silver/orders.sql        # clean: lowercase status, drop bad rows, dedupe, sales only
│   ├── gold/daily_revenue.sql   # business mart: revenue per day
│   └── tests/                   # data-quality assertions (each must return ZERO rows)
├── pipelines/
│   └── medallion.yaml    # the models to run, in order
├── run.py                # the runner: drives the pipeline through the gateway + verifies
└── README.md
```

## Run it (the always-on stack)

```
make platform-up        # bring up the stack (Postgres + MinIO + engine + catalog + rat serve) — stays up
make platform-run       # run the medallion once through the gateway (bronze→silver→gold)
make platform-down      # tear it all down
```

`compose.yaml` brings up every plugin as a sibling container; **`rat serve` runs in its
own container and ATTACHES to the plugins by service name** (no docker-in-docker). The
DuckLake's **metadata lives on Postgres** and its **data on MinIO/S3**, so the engine and
catalog share one lake over the network. `make platform-run` connects to the gateway as
`platform-runner` and issues each model as a `rat://engine/v1/execute` command — so every
layer build is **C5-authorized + audited** by the core, just like any other command — then
commits the gold snapshot to the **DuckLake catalog** (also through the gateway) and reads
the mart back to show it.

Watch the control hops in the orchestrator's audit log:
```
podman logs rat-platform-rat-serve-1 | grep capability
```

Add your own data: drop a CSV in `landing/`, add a `bronze/<name>.sql` that `read_csv`s
`${LANDING}/<file>`, then `silver`/`gold` models, and list them in `pipelines/medallion.yaml`.

## Status

This bundle is built in slices ([ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md)):

- ✅ **S1 — the decoupled stack runs the medallion through `rat serve`, remote** (DuckLake on
  Postgres + data on MinIO; attach mode, no DinD).
- ✅ **S2 — self-driving** — the medallion is a `strategy.apply` capability, and a
  `scheduler-backend` driver fires it on a cron (the demo: every 20s). `make platform-up` and it
  refreshes on its own — nobody runs `platform-run`. Watch: `podman logs rat-platform-scheduler-1`.
- 🟡 **S3 — quality gates** ← *done*: `project/tests/*.sql` run after the layers build; a violation
  raises `FAILED_PRECONDITION` and **blocks the commit** (the catalog doesn't advance). Merge
  strategies (incremental) + read-isolation (DuckLake branching) remain.
- **S4 — state-backend + VS Code** — pipelines/runs/schedules metadata; `vscode-rat` on the stack.
