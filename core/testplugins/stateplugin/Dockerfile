# A launchable state-backend plugin image (ADR-022): a tiny static Go binary rat can
# `podman run` as a container from a plane. Proves the decoupled launch-mode loop —
# rat launches SEPARATE plugin containers, the infra carries no per-plugin service.
#
# Build from the REPO ROOT (the core module's replace target is contracts/sdks/go):
#   podman build -f core/testplugins/stateplugin/Dockerfile -t rat/stateplugin:dev .

FROM docker.io/library/golang:1.25 AS build
WORKDIR /src
COPY core/ ./core/
COPY contracts/sdks/go/ ./contracts/sdks/go/
WORKDIR /src/core
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=mod GOSUMDB=off
RUN go build -trimpath -ldflags="-s -w" -o /out/stateplugin ./testplugins/stateplugin

# A minimal runtime. The podman deployment-runtime forces --user 1000:1000 + a read-only
# rootfs (the I9 profile); the static binary at a world-executable path runs fine under it.
FROM docker.io/library/alpine:3.20
COPY --from=build /out/stateplugin /usr/local/bin/stateplugin
ENTRYPOINT ["/usr/local/bin/stateplugin"]
