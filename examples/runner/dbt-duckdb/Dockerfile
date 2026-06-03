# Launchable rat-dbt-runner plugin image (ADR-022). dbt lives in its OWN venv because
# dbt-core pins an older protobuf than the RAT gRPC SDK (7.35) — the gRPC server uses the
# system env, dbt is shelled out via $RAT_DBT_BIN. Build from the REPO ROOT:
#   podman build -f examples/runner/dbt-duckdb/Dockerfile -t rat/dbt-runner:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051 RAT_DBT_BIN=/opt/dbtvenv/bin/dbt \
    DBT_SEND_ANONYMOUS_USAGE_STATS=0 \
    RAT_DBT_PROJECT=/project RAT_DBT_PROFILES=/project RAT_LANDING=/landing \
    HOME=/tmp
# HOME=/tmp so DuckDB writes its ~/.duckdb (extensions, secrets) to the I9 tmpfs — the
# read-only rootfs would otherwise reject /plugin/.duckdb (Q2 DuckLake materialization).
WORKDIR /plugin
COPY examples/runner/dbt-duckdb/ /plugin/
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
# DEMO shortcut: bake a sample dbt project + landing data so a launched runner has
# something to run. Real projects arrive via `rat apply` (ADR-021 Q1 — upload/mount), not
# baked into the runner. dbt writes only to /tmp (the I9 tmpfs); rootfs stays read-only.
COPY platform/dbt-project/ /project/
COPY platform/landing/ /landing/
RUN pip install --no-cache-dir grpcio==1.80.0 protobuf==7.35.0 \
 && python -m venv /opt/dbtvenv \
 && /opt/dbtvenv/bin/pip install --no-cache-dir dbt-duckdb==1.9.4 duckdb==1.5.3
CMD ["python", "main.py"]
