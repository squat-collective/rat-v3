#!/usr/bin/env bash
#
# RAT cross-axis composition test (the ADR-003 "run against each other on golden
# data" gate — reviews/07 Part C). Boots the data-plane reference plugins together
# (catalog -> engine -> format, wired by capability, Arrow over real Flight) and runs
# the full-refresh strategy reference across the four ADR-003 cross-combinations on
# the shared golden data (contracts/conformance/composition-v1.json).
#
# Containerized (podman/docker, no host installs). Exit 0 iff every combination
# produces the identical expected target.
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 datafusion==53.0.0 deltalake==1.6.0"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

echo ">> cross-axis composition — booting catalog+engine+format together, running the strategy across 4 ADR-003 combos"
$RUNTIME run --rm -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
  -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE "$PY_IMAGE" bash -c '
  pip install -q --root-user-action=ignore '"$PY_DEPS"' >/dev/null 2>&1
  cd /work/examples/composition && python harness.py'
