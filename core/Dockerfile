# Build + run the RAT control-plane daemon (`rat serve`) in a container (ADR-019).
#
# `rat` is the control plane: the SAME static binary runs bare-metal (`./rat serve`)
# or, as here, inside a container — the k8s/docker-daemon shape. This image bundles the
# Phase-A demo plane (the stateplugin) so `docker run` boots a working plane out of the
# box; mount your own plane at /etc/rat/plane.yaml to override it.
#
# Build from the REPO ROOT (the core module's replace target lives in contracts/sdks/go):
#   podman build -f core/Dockerfile -t rat/serve:dev .      # or: make rat-image
# Run (the gateway listens on :7777):
#   podman run --rm -p 7777:7777 rat/serve:dev
#   podman stop <id>     # SIGTERM → the daemon drains (in-flight calls finish, plugin killed)

# ---- builder: a static, CGO-free binary (no libc dependency at runtime) -------------
FROM docker.io/library/golang:1.25 AS build
WORKDIR /src
# only the core module + its local replace target are needed to build
COPY core/ ./core/
COPY contracts/sdks/go/ ./contracts/sdks/go/
WORKDIR /src/core
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=mod GOSUMDB=off
# VERSION is injected at release time (the GHCR pipeline passes --build-arg VERSION=<tag>),
# so the daemon image's `rat version` reports the release tag. Defaults to "dev".
ARG VERSION=dev
# the daemon + the Phase-A demo state plugin (so the image runs without extra mounts)
RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/rat ./cmd/rat \
 && go build -trimpath -ldflags="-s -w" -o /out/stateplugin ./testplugins/stateplugin

# ---- runtime: minimal alpine, NON-ROOT -----------------------------------------------
# Non-root is the default: the local-process deployment-runtime refuses to run as root
# (it cannot honor run_as_non_root with privileges — I9). uid 10001 is an arbitrary
# unprivileged user. The SIBLING/socket-mount mode (ADR-022) overrides this at run time
# with `--user 0` so rat can reach the host's rootless podman socket (which maps to the
# host user) — there rat uses the PODMAN runtime, so the local-process root-refusal is moot.
#
# `podman` is installed so a CONTAINERIZED rat can drive the HOST's podman over a mounted
# socket (Docker-out-of-Docker). The `podman-remote` wrapper forces remote mode; point it
# at the socket with $CONTAINER_HOST and select it with $RAT_PODMAN_BIN=podman-remote.
FROM docker.io/library/alpine:3.20
RUN adduser -D -u 10001 rat \
 && apk add --no-cache podman \
 && printf '#!/bin/sh\nexec podman --remote "$@"\n' > /usr/local/bin/podman-remote \
 && chmod +x /usr/local/bin/podman-remote
COPY --from=build /out/rat /usr/local/bin/rat
COPY --from=build /out/stateplugin /usr/local/bin/stateplugin
COPY core/cmd/rat/manifests/ /etc/rat/manifests/
COPY core/cmd/rat/plane.container.yaml /etc/rat/plane.yaml
USER rat
EXPOSE 7777
# ENTRYPOINT is just the binary; CMD carries the default plane so it can be overridden
# (`podman run rat/serve:dev serve --plane /plane/plugins.yaml`) for the socket-mount stack.
ENTRYPOINT ["/usr/local/bin/rat"]
CMD ["serve", "--plane", "/etc/rat/plane.yaml"]
