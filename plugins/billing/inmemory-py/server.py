"""The BillingService gRPC implementation (Python) — first `billing` reference.

Implements Record: read the caller's tenant FROM the rat-callmeta-bin metadata
header (ADR-007) and record each usage event into a per-tenant ledger. The meter
boundary is ALWAYS the metadata tenant — never a request field (C7) — so a caller
cannot bill another tenant's account.
"""

from rat.billing.v1 import billing_pb2, billing_pb2_grpc
from rat.common.v1 import context_pb2

from store import BillingLedger


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


class BillingServicer(billing_pb2_grpc.BillingServiceServicer):
    def __init__(self, ledger: BillingLedger = None) -> None:
        self._ledger = ledger if ledger is not None else BillingLedger()

    def Record(self, request, context):
        tenant = _tenant_from_context(context)  # from metadata, never a request field (C7)
        count = self._ledger.record(tenant, request.events)
        return billing_pb2.RecordResponse(recorded=count)
