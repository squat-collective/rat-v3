# Launchable rat-bff (the UI control-path backend) plugin image (ADR-022). Build from the
# REPO ROOT:
#   podman build -f platform/bff.Dockerfile -t rat/bff:dev .
FROM docker.io/library/python:3.12-slim
# HOME=/tmp so DuckDB writes its ~/.duckdb (extensions/secrets) to the I9 tmpfs (the
# read-only rootfs would otherwise reject it) — the bulk data leg (Q2).
ENV PYTHONUNBUFFERED=1 BFF_ADDR=0.0.0.0:8080 HOME=/tmp
WORKDIR /plugin
COPY platform/bff.py /plugin/bff.py
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
# duckdb for the bulk DATA leg (Q2): the bff reads the medallion tables from the shared lake.
RUN pip install --no-cache-dir grpcio==1.80.0 protobuf==7.35.0 duckdb==1.5.3
CMD ["python", "bff.py"]
