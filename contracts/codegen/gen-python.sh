#!/bin/sh
# ADR-018 — python codegen (protoc-35 hybrid). Runs INSIDE rat-codegen-python, from
# /workspace (= contracts/). Messages via standalone protoc 35 (protobuf 7.35.0 gencode,
# matching buf's protocolbuffers/python); gRPC service stubs via grpcio-tools. Proto paths
# are relative to the proto root so `-Iproto` resolves them; output preserves the
# rat/<axis>/v1 package dirs. Fully local/offline — no buf, no BSR.
set -e
protos=$(cd proto && find rat -name '*.proto' -type f | sort)
protoc -Iproto --python_out=sdks/python $protos
python -m grpc_tools.protoc -Iproto --grpc_python_out=sdks/python $protos
echo "OK: python SDK (protoc-35 messages + grpcio-tools grpc stubs)"
