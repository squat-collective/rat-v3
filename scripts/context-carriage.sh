#!/usr/bin/env bash
# PU-2 (ADR-017) — cross-run the TWO context-carriage reference implementations (Go +
# Python) against the shared golden vectors. Both must pass; agreement is the ADR-003
# two-reference conformance signal for the KEYSTONE context-carriage contract
# (common/v1/context.proto + ADR-007 gateway stamping) — the most-irreversible frozen
# surface, which the data-axis conformance skipped (architect F1). Containerized per the
# project's container-only rule; nothing installed on the host.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="contracts/conformance/context-carriage"
VEC="/work/${DIR}/context-carriage-v1.json"

if command -v podman >/dev/null 2>&1; then
  RT=podman; RF=(--userns=keep-id)
elif command -v docker >/dev/null 2>&1; then
  RT=docker; RF=()
else
  echo "error: need podman or docker (container-only project)" >&2; exit 1
fi
GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.25}"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"

echo ">> context-carriage: Go reference (clean-room, stdlib only)"
"$RT" run --rm "${RF[@]}" -e HOME=/tmp -e GOTOOLCHAIN=local -e GOFLAGS=-mod=mod \
  -v "${REPO_ROOT}:/work:Z" -w "/work/${DIR}/go" "$GO_IMAGE" \
  go run . "$VEC"

echo ">> context-carriage: Python reference (technologically divergent)"
"$RT" run --rm "${RF[@]}" -e HOME=/tmp \
  -v "${REPO_ROOT}:/work:Z" -v rat-pipcache:/root/.cache/pip -w "/work/${DIR}/py" "$PY_IMAGE" \
  sh -c "pip install -q --root-user-action=ignore cryptography==44.0.0 >/dev/null 2>&1 && python check.py '${VEC}'"

echo ">> CONTEXT-CARRIAGE: both references conform — keystone is two-impl-conformed ✅"
