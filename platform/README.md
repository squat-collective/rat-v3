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

## Run it (M1 — local)

```
make platform-demo        # builds the bundle image, runs the medallion end to end
```

Under the hood: `rat serve --plane plane.yaml` (local runtime) launches the **DuckDB
engine** + **DuckLake catalog** plugins; `run.py` connects to the gateway as
`platform-runner` and issues each model as a `rat://engine/v1/execute` command (so every
layer build is authorized + audited like any other command); then it reads the gold mart
straight from the shared DuckLake to show the result.

Add your own data: drop a CSV in `landing/`, add a `bronze/<name>.sql` that
`read_csv`s it, then `silver`/`gold` models, and list them in `pipelines/medallion.yaml`.

## Status

This bundle is built in slices ([ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md)):

- **M1 — local medallion demo** ← *here*. Scaffold + a runnable local pipeline through the gateway.
- **M2 — `compose up`** — containerize via attach mode: rat + plugins + Postgres + MinIO on one network.
- **M3 — data-quality tests** — `project/tests/` run + reported.
- **M4 — VS Code** — `vscode-rat` repointed at the running stack: browse layers, edit models, run pipelines.
