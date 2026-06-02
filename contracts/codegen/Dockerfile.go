# ADR-018 — connectionless Go SDK codegen.
#
# buf + pinned LOCAL protoc-gen-go / protoc-gen-go-grpc, so `buf generate` runs the
# plugins locally (no BSR call at gen time — the cause of the ADR-017 rate-limit
# friction). The image BUILD needs the Go module proxy; the codegen RUN is offline.
# Versions are pinned to the committed SDK headers (protoc-gen-go v1.36.11,
# protoc-gen-go-grpc v1.6.2) so the remote->local swap is ZERO churn.
FROM docker.io/library/golang:1.25
ENV GOBIN=/usr/local/bin
RUN go install github.com/bufbuild/buf/cmd/buf@v1.47.2 \
 && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
WORKDIR /workspace
ENTRYPOINT ["buf"]
