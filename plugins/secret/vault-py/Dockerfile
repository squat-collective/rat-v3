# Launchable rat-secret-vault plugin image (ADR-022). Build from the REPO ROOT:
#   podman build -f plugins/secret/vault-py/Dockerfile -t rat/secret-vault:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051
WORKDIR /plugin
COPY plugins/secret/vault-py/ /plugin/
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
