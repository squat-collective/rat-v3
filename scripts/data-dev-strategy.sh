#!/usr/bin/env bash
#
# Data-dev plane — incremental-embed STRATEGY runner (experiments/data-dev-plane,
# build-order step 4). Drives the real incremental ELT strategy (examples/strategy/
# incremental-embed-py) over a live engine + catalog via the capability-invoke gateway:
# run 1 (initial load) → run 2 (only the delta embeds) → run 2 replay (idempotent, C1).
# Local DuckLake (no S3 needed). EXPLORATORY + additive.
#
# Containerized (podman/docker, no host installs). Exit 0 iff the incremental counts +
# idempotent replay + search assertions hold (run-strategy.py is assertion-bearing).
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 numpy==2.2.6"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

echo ">> data-dev plane — incremental-embed strategy (strategy→gateway→engine+catalog)"
$RUNTIME run --rm -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
  -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE "$PY_IMAGE" bash -c '
  pip install -q --root-user-action=ignore '"$PY_DEPS"' >/dev/null 2>&1
  cd /work && python experiments/data-dev-plane/run-strategy.py'
