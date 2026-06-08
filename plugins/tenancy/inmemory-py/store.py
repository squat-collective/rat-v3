"""In-memory tenancy POLICY engine — the pure decision logic, no gRPC.

This is policy ON TOP of the core's structural C7 tenant isolation (see the
tenancy.proto header): the core already namespaces state, threads
`identity.tenant` through every RPC, and vends tenant-scoped storage creds. This
plugin does NOT re-implement isolation. It only answers the POLICY questions the
core can't hardcode — "may tenant A share with tenant B?", "is tenant A over its
quota?" — and the core enforces the verdict.

`decide()` returns a `(allowed, deny_code)` pair using the real
`tenancy_pb2.DenyCode` enum values. QUOTA is stateful: a per-tenant counter lives
on the instance and is incremented on every QUOTA decision.
"""

from rat.tenancy.v1 import tenancy_pb2

# acme may share with "partner" only — everyone else is denied cross-tenant.
SHARING_ALLOWLIST = {"acme": {"partner"}}

# Per-(tenant) ceiling for QUOTA decisions: the 1st and 2nd pass, the 3rd+ fails.
QUOTA_LIMIT = 2


class TenancyPolicy:
    def __init__(self) -> None:
        # Per-tenant QUOTA counter. Incremented on each QUOTA decide; the
        # reference keeps it in-process (a real plugin would consult a backend).
        self._quota_used: dict[str, int] = {}

    def decide(self, tenant, kind, subject_action, counterparty_tenant):
        """Return (allowed: bool, deny_code: tenancy_pb2.DenyCode)."""
        if kind == tenancy_pb2.DecisionKind.DECISION_KIND_PERMISSION:
            # In-tenant permission is always allowed in this reference: the core
            # already scoped the request to the tenant; finer RBAC is identity's job.
            return True, tenancy_pb2.DenyCode.DENY_CODE_UNSPECIFIED

        if kind == tenancy_pb2.DecisionKind.DECISION_KIND_SHARING:
            if counterparty_tenant in SHARING_ALLOWLIST.get(tenant, set()):
                return True, tenancy_pb2.DenyCode.DENY_CODE_UNSPECIFIED
            return False, tenancy_pb2.DenyCode.DENY_CODE_CROSS_TENANT_DENIED

        if kind == tenancy_pb2.DecisionKind.DECISION_KIND_QUOTA:
            used = self._quota_used.get(tenant, 0) + 1
            self._quota_used[tenant] = used
            if used <= QUOTA_LIMIT:
                return True, tenancy_pb2.DenyCode.DENY_CODE_UNSPECIFIED
            return False, tenancy_pb2.DenyCode.DENY_CODE_QUOTA_EXCEEDED

        # DECISION_KIND_UNSPECIFIED (or any unknown kind): no policy applies → deny.
        return False, tenancy_pb2.DenyCode.DENY_CODE_POLICY_FORBIDDEN
