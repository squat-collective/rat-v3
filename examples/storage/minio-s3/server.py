"""The StorageService gRPC implementation for rat-storage-minio-s3.

The FIRST reference to implement the Q02 5c read/write split (ADR-017): besides the
broad `VendCredentials(prefix, mode)`, it serves `VendReadCredentials(prefix)` and
`VendWriteCredentials(prefix)` whose mode is FIXED by the METHOD — so C5 authorizes
read-vending and write-vending distinctly and a least-privilege grant can give read
without write.

Tenant is read from the rat-callmeta-bin metadata header (ADR-007), never a request
field — the C7 anti-forgery property. The actual scoped creds come from the configured
minter (creds.py): a real MinIO STS token set in production, a scope receipt offline.
"""

import json

import grpc

from rat.common.v1 import context_pb2
from rat.storage.v1 import storage_pb2, storage_pb2_grpc

from creds import READ, WRITE, READ_WRITE

_MODE = {
    storage_pb2.AccessMode.ACCESS_MODE_READ: READ,
    storage_pb2.AccessMode.ACCESS_MODE_WRITE: WRITE,
    storage_pb2.AccessMode.ACCESS_MODE_READ_WRITE: READ_WRITE,
}


def _tenant_from_context(context) -> str:
    """Read the caller's tenant out of the rat-callmeta-bin metadata envelope (ADR-007).
    Empty (== single-tenant/solo default) if absent."""
    for key, val in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(val)
            return rc.identity.tenant
    return ""


class StorageServicer(storage_pb2_grpc.StorageServiceServicer):
    def __init__(self, minter) -> None:
        self._minter = minter

    def _vend(self, context, prefix: str, mode: str):
        if not prefix:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "prefix is required")
        tenant = _tenant_from_context(context)
        try:
            blob, expires = self._minter.mint(tenant, prefix, mode)
        except Exception as e:  # STS / config failure
            context.abort(grpc.StatusCode.INTERNAL, f"vend failed: {e}")
        return json.dumps(blob).encode("utf-8"), expires

    def VendCredentials(self, request, context):
        if request.mode == storage_pb2.AccessMode.ACCESS_MODE_UNSPECIFIED:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "mode must be specified")
        creds, expires = self._vend(context, request.prefix, _MODE[request.mode])
        return storage_pb2.VendCredentialsResponse(credentials=creds, expires_unix_ms=expires)

    def VendReadCredentials(self, request, context):
        # mode FIXED to READ by the method (5c) — not chosen by the caller.
        creds, expires = self._vend(context, request.prefix, READ)
        return storage_pb2.VendReadCredentialsResponse(credentials=creds, expires_unix_ms=expires)

    def VendWriteCredentials(self, request, context):
        # mode FIXED to WRITE-capable by the method (5c).
        creds, expires = self._vend(context, request.prefix, WRITE)
        return storage_pb2.VendWriteCredentialsResponse(credentials=creds, expires_unix_ms=expires)
