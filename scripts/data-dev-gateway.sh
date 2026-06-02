#!/usr/bin/env bash
#
# Data-dev gateway — the backend the VS Code extension (examples/ui/vscode-rat) talks
# to (experiments/data-dev-plane, build-order step 6). Boots the in-proc engine +
# catalog + strategy stack on a local DuckLake, seeds a corpus + runs the
# incremental-embed strategy once, and serves a small JSON API over HTTP.
#
# Runs in the FOREGROUND (it's a dev server) and publishes the port so the editor on
# the host can reach it. Ctrl-C to stop. EXPLORATORY + additive.
#
#   make data-dev-gateway              # default port 8787
#   RAT_GATEWAY_PORT=9090 make data-dev-gateway
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 numpy==2.2.6"
PORT="${RAT_GATEWAY_PORT:-8787}"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

echo ">> data-dev gateway on http://localhost:${PORT}  (Ctrl-C to stop)"
echo ">> point the VS Code extension (examples/ui/vscode-rat) at this URL"
exec $RUNTIME run --rm -it -p "${PORT}:${PORT}" \
  -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
  -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE -e RAT_GATEWAY_PORT="${PORT}" \
  "$PY_IMAGE" bash -c "
  pip install -q --root-user-action=ignore $PY_DEPS >/dev/null 2>&1
  cd /work/examples/ui/vscode-rat/gateway && python app.py"
