#!/usr/bin/env bash
#
# Package the vscode-rat extension into an installable .vsix (experiments/data-dev-plane,
# build-order step 6). Runs npm install + @vscode/vsce package in a node container — no
# host node/npm install. The .vsix lands next to the extension (gitignored).
#
#   make data-dev-vsix
#   code --install-extension examples/ui/vscode-rat/vscode-rat-0.1.0.vsix
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
RUNTIME="$(command -v podman || command -v docker)"
NODE_IMAGE="${NODE_IMAGE:-docker.io/library/node:22}"
EXT="$ROOT/examples/ui/vscode-rat"

if [ -z "$RUNTIME" ]; then echo "no podman/docker found" >&2; exit 2; fi

echo ">> packaging vscode-rat into a .vsix (node container)"
$RUNTIME run --rm -v "$EXT":/ext:Z -w /ext "$NODE_IMAGE" bash -c '
  npm install --no-audit --no-fund >/dev/null 2>&1
  npx --yes @vscode/vsce@latest package --no-dependencies'

echo
VSIX="$(ls -t "$EXT"/*.vsix 2>/dev/null | head -1)"
if [ -n "$VSIX" ]; then
  echo ">> built: $VSIX"
  echo ">> install:  code --install-extension \"$VSIX\""
  echo ">> then run: make data-dev-gateway   (the extension's backend)"
else
  echo ">> no .vsix produced" >&2; exit 1
fi
