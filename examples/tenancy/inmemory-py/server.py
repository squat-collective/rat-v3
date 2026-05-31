"""The TenancyService gRPC implementation (Python) — `tenancy` reference.

Implements Decide: read the caller's tenant FROM the rat-callmeta-bin metadata
header (ADR-007 — NEVER a request field, so a caller can't pose a decision as
another tenant), then delegate to the in-memory `TenancyPolicy` and return the
`(allowed, deny_code)` verdict. The core enforces the verdict; the plugin only
computes it (proto header: "The core enforces the verdict; the plugin only
computes it.").
"""

import grpc

from rat.common.v1 import context_pb2
from rat.tenancy.v1 import tenancy_pb2, tenancy_pb2_grpc

from store import TenancyPolicy


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


class TenancyServicer(tenancy_pb2_grpc.TenancyServiceServicer):
    def __init__(self, policy: TenancyPolicy | None = None) -> None:
        self._policy = policy or TenancyPolicy()

    def Decide(self, request, context):
        tenant = _tenant_from_context(context)  # from metadata, never a request field
        allowed, deny_code = self._policy.decide(
            tenant=tenant,
            kind=request.kind,
            subject_action=request.subject_action,
            counterparty_tenant=request.counterparty_tenant,
        )
        return tenancy_pb2.DecideResponse(allowed=allowed, deny_code=deny_code)
