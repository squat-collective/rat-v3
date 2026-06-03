"""The SecretService gRPC implementation (Python) — env-backed platform secret-backend.

Implements Resolve: read the caller's tenant FROM the rat-callmeta-bin metadata header
(ADR-007), then resolve (tenant, secret_ref) against the env-backed store. Resolution is
tenant-scoped and anti-enumeration: an unknown ref AND a ref that exists only for a DIFFERENT
tenant both return found=false + empty value (NOT a PERMISSION_DENIED status). See the proto's
FOUND SEMANTICS comment — this is the whole point of the axis.
"""

import time

from rat.common.v1 import context_pb2
from rat.secret.v1 import secret_pb2, secret_pb2_grpc

from store import EnvSecretStore


def _tenant_from_context(context) -> str:
    """Read the caller's tenant out of the rat-callmeta-bin metadata envelope (ADR-007).
    Empty (== single-tenant/solo default) if absent. grpcio delivers `-bin` values as bytes."""
    for key, val in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(val)
            return rc.identity.tenant
    return ""


class SecretServicer(secret_pb2_grpc.SecretServiceServicer):
    def __init__(self, now_ms=None) -> None:
        self._now_ms = now_ms or (lambda: int(time.time() * 1000))
        self._store = EnvSecretStore()

    def Resolve(self, request, context):
        tenant = _tenant_from_context(context)  # from metadata, never a request field
        found, value, expires = self._store.resolve(tenant, request.secret_ref, self._now_ms())
        return secret_pb2.ResolveResponse(found=found, value=value, expires_unix_ms=expires)
