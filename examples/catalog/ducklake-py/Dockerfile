# Launchable rat-catalog (DuckLake) plugin image (ADR-022). Build from the REPO ROOT:
#   podman build -f examples/catalog/ducklake-py/Dockerfile -t rat/catalog:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051
WORKDIR /plugin
COPY examples/catalog/ducklake-py/ /plugin/
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
