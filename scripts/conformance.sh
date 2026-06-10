#!/usr/bin/env bash
#
# RAT conformance suite (sub-phase 0f).
#
# Runs EVERY reference plugin under plugins/<axis>/<impl>/ against its shared golden
# vectors (contracts/conformance/<axis>-v1.json) and prints a unified pass/fail
# matrix. The per-axis golden vectors are the authoritative conformance set; this is
# the single runner that proves "implementation X conforms to axis Y" across all of
# them. Containerized (podman/docker, no host installs); refs are auto-discovered, so
# a new reference joins the suite the moment it lands.
#
# Exit 0 iff every reference conforms.
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.25}"
PY_IMAGE="${PY_IMAGE:-docker.io/library/python:3.12}"
# Union of every Python reference's runtime deps (real backends included).
PY_DEPS="grpcio==1.80.0 protobuf==7.35.0 pyarrow==24.0.0 duckdb==1.5.3 datafusion==53.0.0 deltalake==1.6.0 cryptography==44.0.0 numpy==2.2.6"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

# --- discover references ---------------------------------------------------------
GO_REFS=(); PY_REFS=()
for d in plugins/*/*/; do
  d="${d%/}"
  # A conformance reference is a dir with a harness (harness_test.{go,py}). Dirs
  # with a go.mod but no harness (e.g. the latency benchmark) are NOT references.
  if   [ -f "$d/go.mod" ] && [ -f "$d/harness_test.go" ]; then GO_REFS+=("$d")
  elif [ -f "$d/harness_test.py" ];                       then PY_REFS+=("$d")
  fi
done

RESULTS="$(mktemp)"
trap 'rm -f "$RESULTS"' EXIT

echo ">> conformance suite — ${#GO_REFS[@]} Go + ${#PY_REFS[@]} Python references"

# --- Go references (each ref's harness_test.go drives gRPC against its vectors) ---
# A FAIL prints the harness output tail to stderr (stdout stays clean for the matrix) —
# a bare FAIL with the diagnostics swallowed was a top DX frustration.
if [ "${#GO_REFS[@]}" -gt 0 ]; then
  echo ">> running Go references (golang container)…"
  $RUNTIME run --rm -v "$ROOT":/work:Z -v rat-gocache:/go/pkg/mod -w /work "$GO_IMAGE" bash -c '
    for ref in '"${GO_REFS[*]}"'; do
      out="$(cd "/work/$ref" && go test ./... 2>&1)"
      if [ $? -eq 0 ]; then s=PASS; else
        s=FAIL
        { echo; echo "── FAIL: $ref — harness output (last 40 lines) ──"
          printf "%s\n" "$out" | tail -40
          echo "─────────────────────────────────────────────────"; } >&2
      fi
      echo "RESULT|$ref|go|$s"
    done' >> "$RESULTS"
fi

# --- Python references (one container; union of deps installed once) -------------
if [ "${#PY_REFS[@]}" -gt 0 ]; then
  echo ">> running Python references (python container)…"
  $RUNTIME run --rm -v "$ROOT":/work:Z -v rat-pipcache:/root/.cache/pip \
    -e PYTHONPATH=/work/contracts/sdks/python -e GRPC_VERBOSITY=NONE "$PY_IMAGE" bash -c '
    pip install -q --root-user-action=ignore '"$PY_DEPS"' >/dev/null 2>&1 \
      || echo ">> pip install failed — FAILs below are likely import errors" >&2
    for ref in '"${PY_REFS[*]}"'; do
      out="$(cd "/work/$ref" && python harness_test.py 2>&1)"
      if [ $? -eq 0 ]; then s=PASS; else
        s=FAIL
        { echo; echo "── FAIL: $ref — harness output (last 40 lines) ──"
          printf "%s\n" "$out" | tail -40
          echo "─────────────────────────────────────────────────"; } >&2
      fi
      echo "RESULT|$ref|py|$s"
    done' >> "$RESULTS"
fi

# --- render the matrix (sort on host; plain awk so mawk/gawk both work) ----------
echo
sort -t'|' -k2 "$RESULTS" | awk -F'|' '
  BEGIN {
    print "  axis      impl             lang vectors              result"
    print "  --------- ---------------- ---- -------------------- ------"
  }
  /^RESULT/ {
    split($2, p, "/"); axis = p[2]; impl = p[3]; lang = $3; status = $4
    vectors = axis "-v1.json"
    if (axis == "engine" && (impl ~ /duckdb/ || impl ~ /datafusion/)) vectors = "engine-real-v1.json"
    printf "  %-9s %-16s %-3s %-20s %s\n", axis, impl, lang, vectors, status
    total++; if (status == "PASS") passed++; else fails++
  }
  END {
    print "  --------- ---------------- ---- -------------------- ------"
    printf "  %d/%d references conform\n", passed, total
    exit (fails > 0)
  }
'
STATUS=$?

if [ "$STATUS" -eq 0 ]; then echo ">> CONFORMANT ✅"; else echo ">> NON-CONFORMANT ❌"; fi
exit "$STATUS"
