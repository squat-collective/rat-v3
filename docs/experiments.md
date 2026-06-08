# Experiments

## data-dev plane — the ML lakehouse showcase → graduated to `rat-data-dev`

The end-to-end **DuckDB + embeddings + S3 lakehouse** experiment (which validated a set of
plugins against the frozen wire and surfaced findings F1–F9) has **graduated to its own repo**,
`rat-data-dev`, to keep this repo focused on the platform (restructure
[ADR-038](architecture/adrs/038-reference-plugins-live-under-plugins.md) / [docs/restructure/](restructure/)).

**What moved:** the `data-dev-plane` experiment (runners + compose) · 5 reference plugins —
`engine/duckdb-ml-py`, `catalog/ducklake-py`, `storage/minio-s3`, `strategy/incremental-embed-py`,
`ui/vscode-rat` · the `data-dev-*` scripts + Makefile targets.

**Why those plugins, and not others:** they were exploratory references for the experiment. The
medallion **platform** does not depend on them — its engine + catalog are embedded in the
dbt-runner (dbt-duckdb + DuckLake, in-process), so the standalone duckdb-ml/ducklake plugins were
vestigial here. The platform's reference plugins (state, secret, scheduler, dbt-runner, bff) stay.

**Note:** `ui/vscode-rat` carried the Phase-10 workspace-federation / RatFS demo (ADRs
033/034/035); that demo now lives in `rat-data-dev`. The main repo's UI references are
`plugins/ui/vscode-platform` + `plugins/ui/web-portal-py`.
