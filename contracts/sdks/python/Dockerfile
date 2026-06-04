# rat/plugin-base-py — the base image for Python rat plugins (ADR-026, SDK distribution).
#
# It bakes the generated rat Python SDK (all axes, importable as `rat.<axis>.v1`) + its gRPC
# runtime into site-packages. A plugin then `FROM`s this image and ships ONLY its own code
# (manifest + main.py + Dockerfile) — no vendored SDK tree in the plugin repo.
#
# Build:  podman build -t rat/plugin-base-py:dev contracts/sdks/python   (or `make plugin-base-py`)
FROM docker.io/library/python:3.12-slim

ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051

# The SDK's runtime deps, pinned to the codegen toolchain (ADR-018: protoc-35 / protobuf 7.35).
RUN pip install --no-cache-dir grpcio==1.80.0 protobuf==7.35.0

# The generated SDK package → on the default import path. `from rat.engine.v1 import …` just works.
COPY rat /usr/local/lib/python3.12/site-packages/rat
