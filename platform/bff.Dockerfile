# Launchable rat-bff (the UI control-path backend) plugin image (ADR-022). Build from the
# REPO ROOT:
#   podman build -f platform/bff.Dockerfile -t rat/bff:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 BFF_ADDR=0.0.0.0:8080
WORKDIR /plugin
COPY platform/bff.py /plugin/bff.py
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir grpcio==1.80.0 protobuf==7.35.0
CMD ["python", "bff.py"]
