#!/bin/sh
# install.sh — fetch the latest `rat` binary from GitHub Releases (Phase 4 distribution).
#
#   curl -fsSL https://github.com/squat-collective/rat-v3/releases/latest/download/install.sh | sh
#
# Downloads the right rat-<version>-<os>-<arch> for this machine, verifies its sha256
# against SHA256SUMS, and drops a `./rat` you can run immediately (the `chmod +x ./rat`
# vision). Override the repo with $RAT_REPO and the install dir with $RAT_BIN_DIR.
set -eu

REPO="${RAT_REPO:-squat-collective/rat-v3}"
BIN_DIR="${RAT_BIN_DIR:-.}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux | darwin) ;;
  *) echo "rat: unsupported OS '$os' (linux/darwin only)"; exit 1 ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "rat: unsupported arch '$arch'"; exit 1 ;;
esac

# Resolve the latest release tag (rat/N.M) → bare version (N.M).
tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
         | grep -o '"tag_name"[ ]*:[ ]*"[^"]*"' | head -1 | cut -d'"' -f4)"
[ -n "$tag" ] || { echo "rat: could not resolve the latest release of ${REPO}"; exit 1; }
ver="${tag##*/}"

asset="rat-${ver}-${os}-${arch}"
base="https://github.com/${REPO}/releases/download/${tag}"
echo "rat: downloading ${asset} (${tag}) …"
curl -fsSL "${base}/${asset}" -o "${BIN_DIR}/rat"
chmod +x "${BIN_DIR}/rat"

# Best-effort checksum verification.
if sums="$(curl -fsSL "${base}/SHA256SUMS" 2>/dev/null)"; then
  want="$(printf '%s\n' "$sums" | grep " ${asset}\$" | awk '{print $1}')"
  if [ -n "$want" ] && command -v sha256sum >/dev/null 2>&1; then
    got="$(sha256sum "${BIN_DIR}/rat" | awk '{print $1}')"
    [ "$want" = "$got" ] || { echo "rat: checksum mismatch — aborting"; rm -f "${BIN_DIR}/rat"; exit 1; }
    echo "rat: checksum ok"
  fi
fi

echo "rat: installed ${BIN_DIR}/rat — try:  ${BIN_DIR}/rat version  &&  ${BIN_DIR}/rat init"
