"""The static-token identity backend (pure logic, no gRPC).

A `kind: identity` plugin backs the core's Identity Gateway. This reference is the
C2 DEFAULT — a static-token model, NOT anonymous-root (reviews/04). Tokens map to
subjects; subjects carry roles; actions require roles. Authentication is a
CONSTANT-TIME compare (hmac.compare_digest) so a bad token cannot be distinguished
from a good one by timing.

Authorization is COARSE (is this subject allowed this action at all). A deny is a
SUCCESSFUL Authorize that carries a machine-readable `deny_code` (the ERROR MODEL
in identity.proto), never a free-text reason that drives caller logic.
"""

import hmac

from rat.identity.v1 import identity_pb2

# token -> {subject, tenant, roles}. In a real plugin these are issued/rotated; here
# they are fixed conformance fixtures.
TOKENS = {
    "tok-acme-admin": {"subject": "alice", "tenant": "acme", "roles": ["admin", "runner"]},
    "tok-acme-viewer": {"subject": "bob", "tenant": "acme", "roles": ["viewer"]},
}

# action -> the role required to perform it.
ACTION_ROLES = {
    "pipeline.run": "runner",
    "plane.create": "admin",
}


class StaticTokenIdentity:
    def __init__(self) -> None:
        # subject -> roles, for the authorize path.
        self._subjects = {t["subject"]: t for t in TOKENS.values()}

    def authenticate(self, credential: bytes):
        """Constant-time-compare `credential` against every known token.

        Returns (authenticated, subject, tenant). The compare uses
        hmac.compare_digest so the timing does not leak which (if any) token
        matched. We iterate ALL tokens (no early return on first match) for the
        same reason.
        """
        matched = None
        for token, info in TOKENS.items():
            if hmac.compare_digest(credential, token.encode("utf-8")):
                matched = info
        if matched is None:
            return False, "", ""
        return True, matched["subject"], matched["tenant"]

    def authorize(self, subject: str, action: str, resource: str):
        """Coarse allow/deny. Returns (allowed, deny_code) where deny_code is an
        identity_pb2.DenyCode enum value."""
        if not subject or subject not in self._subjects:
            return False, identity_pb2.DenyCode.DENY_CODE_NOT_AUTHENTICATED
        required = ACTION_ROLES.get(action)
        if required is None:
            return False, identity_pb2.DenyCode.DENY_CODE_ACTION_FORBIDDEN
        if required in self._subjects[subject]["roles"]:
            return True, identity_pb2.DenyCode.DENY_CODE_UNSPECIFIED
        return False, identity_pb2.DenyCode.DENY_CODE_INSUFFICIENT_ROLE
