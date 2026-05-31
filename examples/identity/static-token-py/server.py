"""The IdentityService gRPC implementation (Python) — static-token reference.

Implements the two identity capabilities:
  - Authenticate: validate an opaque credential, resolve (subject, tenant).
  - Authorize:    coarse allow/deny for (subject, action, resource), where the
                  SUBJECT is read FROM the rat-callmeta-bin metadata header
                  (ADR-007) — the core stamps it on the hop; it is never a request
                  field. A deny is a successful rpc carrying a `deny_code`.
"""

from rat.common.v1 import context_pb2
from rat.identity.v1 import identity_pb2, identity_pb2_grpc

from store import StaticTokenIdentity


def _subject_from_context(context) -> str:
    """Read the caller's end-user subject out of the rat-callmeta-bin metadata
    envelope (ADR-007). Empty (== pre-auth/bootstrap) if absent. grpcio delivers
    `-bin` metadata values as bytes. The authoritative value is the core-signed
    SubjectAssertion; we read its mirrored `principal` for this coarse decision."""
    for key, val in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(val)
            return rc.identity.subject.principal
    return ""


class IdentityServicer(identity_pb2_grpc.IdentityServiceServicer):
    def __init__(self) -> None:
        self._backend = StaticTokenIdentity()

    def Authenticate(self, request, context):
        authenticated, subject, tenant = self._backend.authenticate(request.credential)
        return identity_pb2.AuthenticateResponse(
            authenticated=authenticated,
            subject=subject,
            tenant=tenant,
        )

    def Authorize(self, request, context):
        subject = _subject_from_context(context)  # from metadata, never a request field
        allowed, deny_code = self._backend.authorize(subject, request.action, request.resource)
        return identity_pb2.AuthorizeResponse(
            allowed=allowed,
            deny_code=deny_code,
        )
