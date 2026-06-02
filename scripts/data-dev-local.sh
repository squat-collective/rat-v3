#!/usr/bin/env bash
#
# Data-dev plane — LOCAL end-to-end runner (experiments/data-dev-plane, build-order
# step 2). Boots the DuckLake catalog + the DuckDB-ML engine together over real gRPC,
# both attached to one local DuckLake, and runs the §6 composition on a small real
# corpus: create → register → transform → embed() → snapshot/commit → 🔍 semantic
# search → idempotent replay. EXPLORATORY + additive — no S3 yet, no proto change.
#
# Containerized (podman/docker, no host installs). Exit 0 iff the pipeline + the search
# ranking are correct (run-local.py is assertion-bearing).
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
# duckdb (+ vss/ducklake/httpfs extensions at runtime), pyarrow + numpy (list UDFs).
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 numpy==2.2.6"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

echo ">> data-dev plane — local end-to-end (DuckLake + DuckDB-ML, over gRPC)"
$RUNTIME run --rm -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
  -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE "$PY_IMAGE" bash -c '
  pip install -q --root-user-action=ignore '"$PY_DEPS"' >/dev/null 2>&1
  cd /work && python experiments/data-dev-plane/run-local.py'
