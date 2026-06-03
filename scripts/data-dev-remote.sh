#!/usr/bin/env bash
#
# Data-dev plane — REMOTE end-to-end runner (experiments/data-dev-plane, build-order
# step 3). Boots a MinIO (S3) + Postgres (DuckLake metadata) stack, then runs the SAME
# pipeline as the local demo but distributed: data on S3, metadata on Postgres, the
# engine's S3 creds vended by the minio-s3 storage plugin (short-TTL, tenant-scoped).
# Proves the data plane is unchanged when storage goes remote. EXPLORATORY + additive.
#
# Containerized (podman/docker, no host installs). Self-contained network + container
# names (rat-data-dev-*) so it doesn't clash with other local stacks. No host ports by
# default (the runner joins the network and reaches services by name).
#
#   scripts/data-dev-remote.sh           # up (idempotent) + run the pipeline
#   scripts/data-dev-remote.sh --down    # tear the stack down
#
# Exit 0 iff the remote pipeline + search ranking + D3 isolation pass (run-remote.py is
# assertion-bearing).
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
PG_IMAGE="${PG_IMAGE:-docker.io/library/postgres:16}"
MINIO_IMAGE="${MINIO_IMAGE:-quay.io/minio/minio:latest}"
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 numpy==2.2.6 boto3==1.40.0"

NET=rat-data-dev
PG=rat-data-dev-pg
MINIO=rat-data-dev-minio

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

down() {
  echo ">> tearing down the data-dev remote stack"
  $RUNTIME rm -f "$PG" "$MINIO" >/dev/null 2>&1 || true
  $RUNTIME network rm -f "$NET" >/dev/null 2>&1 || true
}

if [ "${1:-}" = "--down" ]; then down; exit 0; fi

# --- bring the stack up (idempotent) ---------------------------------------------
$RUNTIME network exists "$NET" >/dev/null 2>&1 || $RUNTIME network create "$NET" >/dev/null

running() { [ "$($RUNTIME inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]; }

if ! running "$PG"; then
  $RUNTIME rm -f "$PG" >/dev/null 2>&1 || true
  echo ">> starting Postgres ($PG) — DuckLake metadata DB"
  $RUNTIME run -d --name "$PG" --network "$NET" \
    -e POSTGRES_USER=ducklake -e POSTGRES_PASSWORD=ducklake -e POSTGRES_DB=ducklake \
    "$PG_IMAGE" >/dev/null
fi

if ! running "$MINIO"; then
  $RUNTIME rm -f "$MINIO" >/dev/null 2>&1 || true
  echo ">> starting MinIO ($MINIO) — S3 object store"
  $RUNTIME run -d --name "$MINIO" --network "$NET" \
    -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
    "$MINIO_IMAGE" server /data --console-address ":9001" >/dev/null
fi

# --- wait for Postgres to accept connections -------------------------------------
echo -n ">> waiting for Postgres"
for _ in $(seq 1 30); do
  if $RUNTIME exec "$PG" pg_isready -U ducklake -d ducklake >/dev/null 2>&1; then break; fi
  echo -n "."; sleep 1
done
echo

# --- reset the DuckLake metadata for a clean, deterministic run ------------------
$RUNTIME exec "$PG" psql -U ducklake -d ducklake \
  -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null 2>&1 || true

# --- run the remote pipeline (a python container on the same network) ------------
echo ">> running the remote pipeline (data→S3, metadata→Postgres, creds vended)"
$RUNTIME run --rm --network "$NET" -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
  -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE \
  -e MINIO_ENDPOINT="$MINIO:9000" -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  -e RAT_S3_BUCKET=rat -e PGHOST="$PG" \
  "$PY_IMAGE" bash -c "
  pip install -q --root-user-action=ignore $PY_DEPS >/dev/null 2>&1
  cd /work && python experiments/data-dev-plane/run-remote.py"
STATUS=$?

echo
echo ">> stack left running ($PG, $MINIO on network $NET). Tear down: scripts/data-dev-remote.sh --down"
exit "$STATUS"
