#!/usr/bin/env bash
# run-socket-mount.sh — the FULLY containerized launch-mode platform (ADR-022 socket-mount).
#
# Unlike run with rat-on-the-host, here RAT ITSELF is a container. It drives the HOST's
# podman over a mounted socket (Docker-out-of-Docker) to launch each plugin as a SIBLING
# container on a shared network, and dials those siblings BY NAME via podman DNS — the same
# shape as k8s pod-to-pod-by-service-name (the prod target). The infra is still just
# Postgres + MinIO; adding a plugin is still one plugins.yaml entry + an image.
#
#   ./platform/run-socket-mount.sh up      # infra + rat-as-a-container + the 4 sibling plugins
#   ./platform/run-socket-mount.sh runs    # read the run history from the bff, by name, via a peer
#   ./platform/run-socket-mount.sh down     # tear it all down
#
# Requires a rootless podman with the user socket enabled:
#   systemctl --user enable --now podman.socket
set -euo pipefail
cd "$(dirname "$0")/.."                                   # repo root
NET=rat-net
RAT=rat-orchestrator
SOCK="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/podman/podman.sock"
INFRA="podman compose -f platform/compose.infra.yaml"

up() {
  [ -S "$SOCK" ] || { echo "no podman user socket at $SOCK — run: systemctl --user enable --now podman.socket"; exit 1; }
  echo ">> build images (rat + plugins)"; make rat-image plugin-images >/dev/null
  echo ">> infra: Postgres + MinIO"; $INFRA up -d
  for _ in $(seq 1 30); do
    [ "$(podman inspect -f '{{.State.Health.Status}}' rat-platform-infra-postgres-1 2>/dev/null)" = healthy ] && break
    sleep 1
  done
  podman network exists "$NET" || podman network create "$NET" >/dev/null
  echo ">> rat AS A CONTAINER (socket-mounted, on $NET) launches the plugin siblings"
  podman rm -f "$RAT" >/dev/null 2>&1 || true
  podman run -d --name "$RAT" \
    --network "$NET" \
    --user 0 \
    -v "$SOCK:/run/podman/podman.sock" \
    -e CONTAINER_HOST=unix:///run/podman/podman.sock \
    -e RAT_PODMAN_BIN=podman-remote \
    -e RAT_PODMAN_NETWORK="$NET" \
    --env-file "$PWD/platform/.env" \
    -v "$PWD/platform:/plane:ro" \
    -p 7777:7777 \
    rat/serve:dev serve --plane /plane/plugins.yaml
  for _ in $(seq 1 90); do podman logs "$RAT" 2>&1 | grep -q "gateway serving" && break; sleep 1; done
  podman logs "$RAT" 2>&1 | grep -E "injected RAT_GATEWAY|launching|wired|serving"
  echo ">> siblings on $NET:"; podman ps --format '   {{.Names}}  {{.Image}}' | grep -E 'rat-(pipeline|state|scheduler|bff)-[0-9]'
}

runs() {  # the bff has no host port (sibling mode) — reach it BY NAME from a peer on the net
  bff="$(podman ps --format '{{.Names}}' --filter ancestor=localhost/rat/bff:dev | head -1)"
  [ -n "$bff" ] || { echo "bff sibling not found — is the platform up?"; exit 1; }
  podman run --rm --network "$NET" docker.io/library/alpine:3.20 \
    sh -c "apk add -q curl >/dev/null 2>&1; curl -s $bff:50051/api/runs"
}

down() {
  podman rm -f "$RAT" >/dev/null 2>&1 || true
  for c in $(podman ps -aq --filter network="$NET" 2>/dev/null); do podman rm -f "$c" >/dev/null 2>&1 || true; done
  podman network rm -f "$NET" >/dev/null 2>&1 || true
  $INFRA down -v
}

case "${1:-up}" in
  up) up ;;
  runs) runs ;;
  down) down ;;
  *) echo "usage: $0 {up|runs|down}"; exit 2 ;;
esac
