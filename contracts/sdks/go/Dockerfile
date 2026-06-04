# rat/plugin-base-go — the BUILD base for Go rat plugins (ADR-026, SDK distribution).
#
# It carries the generated rat Go SDK module at /sdk + a warm module cache. A Go plugin's
# go.mod does `replace github.com/rat-dev/rat/gen => /sdk`, so it compiles the SDK IN and ships
# a tiny static binary on scratch — no SDK at runtime (unlike Python, which needs it installed).
#
# Build:  podman build -t rat/plugin-base-go:dev contracts/sdks/go   (or `make plugin-base-go`)
FROM docker.io/library/golang:1.25
ENV GOTOOLCHAIN=local

# the generated SDK module → a stable path plugins `replace` onto.
COPY . /sdk

# pre-warm the SDK's deps (grpc, protobuf, …) into the module cache so plugin builds are fast.
RUN cd /sdk && go mod download all
