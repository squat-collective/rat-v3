#!/usr/bin/env bash
# Regenerate the per-language plugin SDKs from contracts/proto (ADR-006 D3).
#
# Source of truth: contracts/proto/**. Output: contracts/sdks/<lang>/ (committed,
# never hand-edited). Runs buf in a container per the project's container-only
# rule (root CLAUDE.md) — nothing is installed on the host.
#
# Usage:
#   scripts/gen-sdks.sh           # regenerate all wired languages
#   scripts/gen-sdks.sh --check   # regenerate to a temp dir + fail if it differs
#                                  # from the committed sdks/ (CI freshness gate)
#
# Currently wired: Go (ADR-006 allows Go-first while keeping the multi-language
# layout; Python/TS templates land when those reference plugins appear).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTRACTS="$REPO_ROOT/contracts"
BUF_IMAGE="${BUF_IMAGE:-docker.io/bufbuild/buf:1.47.2}"

# Container runtime: podman preferred (root CLAUDE.md), docker fallback.
if command -v podman >/dev/null 2>&1; then
  RUNTIME=podman
  RUNTIME_FLAGS=(--userns=keep-id)
elif command -v docker >/dev/null 2>&1; then
  RUNTIME=docker
  RUNTIME_FLAGS=()
else
  echo "error: need podman or docker (container-only project, nothing installed on host)" >&2
  exit 1
fi

# Languages wired today -> their buf.gen template + output subdir.
LANGS=(go python typescript rust)

run_buf_generate() {
  # $1 = workspace dir to mount as /workspace (buf templates write relative to it)
  local ws="$1" lang tmpl
  for lang in "${LANGS[@]}"; do
    tmpl="buf.gen.${lang}.yaml"
    echo ">> generating ${lang} SDK (${tmpl})"
    "$RUNTIME" run --rm "${RUNTIME_FLAGS[@]}" \
      -e HOME=/tmp -e XDG_CACHE_HOME=/tmp/.cache \
      -v "${ws}:/workspace:Z" -w /workspace \
      "$BUF_IMAGE" generate --template "$tmpl"
  done
}

if [[ "${1:-}" == "--check" ]]; then
  # Freshness gate: regenerate into a throwaway copy of contracts/ and diff the
  # produced sdks/ against the committed one. Non-empty diff => protos changed but
  # sdks/ wasn't regenerated. Used by CI + (optionally) a pre-commit check.
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  cp -r "$CONTRACTS/." "$TMP/"
  rm -rf "$TMP"/sdks
  run_buf_generate "$TMP"
  for lang in "${LANGS[@]}"; do
    # Ignore hand-added, non-generated module files (e.g. sdks/go/go.mod) — they
    # are not produced by buf and would otherwise read as a spurious diff.
    if ! diff -r \
        --exclude=go.mod --exclude=go.sum \
        "$CONTRACTS/sdks/$lang" "$TMP/sdks/$lang" >/dev/null 2>&1; then
      echo "error: contracts/sdks/$lang is stale — run 'make gen-sdks' and commit." >&2
      exit 1
    fi
  done
  echo "OK: all committed SDKs are up to date with contracts/proto."
else
  run_buf_generate "$CONTRACTS"
  echo "OK: regenerated SDKs into contracts/sdks/."
fi
