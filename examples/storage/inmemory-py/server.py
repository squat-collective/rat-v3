"""The StorageService gRPC implementation (Python) — second `storage` reference.

Implements VendCredentials: validate, read the caller's tenant FROM the
rat-callmeta-bin metadata header (ADR-007), and vend a short-TTL scope receipt
bound to (tenant, prefix, mode). Like the Go reference, the credential blob is a
CONFORMANCE scope receipt (JSON) standing in for a real opaque STS token, so the
harness can assert the C7 tenancy-scoping obligation.
"""

import json
import time

import grpc

from rat.common.v1 import context_pb2
from rat.storage.v1 import storage_pb2, storage_pb2_grpc

CREDENTIAL_TTL_MS = 900_000  # 15 minutes — the documented short-TTL

_MODE = {
    storage_pb2.AccessMode.ACCESS_MODE_READ: "READ",
    storage_pb2.AccessMode.ACCESS_MODE_WRITE: "WRITE",
    storage_pb2.AccessMode.ACCESS_MODE_READ_WRITE: "READ_WRITE",
}


def _tenant_from_context(context) -> str:
    """Read the caller's tenant out of the rat-callmeta-bin metadata envelope
    (ADR-007). Empty (== single-tenant/solo default) if absent. grpcio delivers
    `-bin` metadata values as bytes."""
    for key, val in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(val)
            return rc.identity.tenant
    return ""


class StorageServicer(storage_pb2_grpc.StorageServiceServicer):
    def __init__(self, now_ms=None) -> None:
        self._now_ms = now_ms or (lambda: int(time.time() * 1000))

    def VendCredentials(self, request, context):
        if not request.prefix:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "prefix is required")
        if request.mode == storage_pb2.AccessMode.ACCESS_MODE_UNSPECIFIED:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "mode must be specified")
        expires = self._now_ms() + CREDENTIAL_TTL_MS
        receipt = {
            "tenant": _tenant_from_context(context),  # from metadata, never a request field
            "prefix": request.prefix,
            "mode": _MODE.get(request.mode, "UNSPECIFIED"),
            "expires_unix_ms": expires,
        }
        return storage_pb2.VendCredentialsResponse(
            credentials=json.dumps(receipt).encode("utf-8"),
            expires_unix_ms=expires,
        )
