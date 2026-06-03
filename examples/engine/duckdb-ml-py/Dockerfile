# Launchable rat-engine (DuckDB-ML) plugin image (ADR-022). Build from the REPO ROOT:
#   podman build -f examples/engine/duckdb-ml-py/Dockerfile -t rat/engine:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051
WORKDIR /plugin
COPY examples/engine/duckdb-ml-py/ /plugin/
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
