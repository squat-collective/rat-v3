"""The StateService gRPC implementation (Python) — second `state-backend` reference.

Get/Put/List unary; Watch a server-streaming generator. Every key/prefix is
validated against the KEY GRAMMAR (grammar.py) → INVALID_ARGUMENT. Put returns the
PutOutcome enum (COMMITTED / CONFLICT) — a CAS conflict is a NORMAL outcome, not a
gRPC error. RequestContext is NOT a field (ADR-007); this reference ignores identity.
"""

import grpc

from rat.state.v1 import state_pb2, state_pb2_grpc

from grammar import GrammarError, validate_key
from store import Store


class StateServicer(state_pb2_grpc.StateServiceServicer):
    def __init__(self) -> None:
        self.store = Store()

    def Get(self, request, context):
        try:
            validate_key(request.key, False)
        except GrammarError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        found, value, rev = self.store.get(request.key)
        return state_pb2.GetResponse(found=found, value=value, revision=rev)

    def Put(self, request, context):
        try:
            validate_key(request.key, False)
        except GrammarError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        committed, rev = self.store.put(request.key, request.value, request.if_revision)
        outcome = (
            state_pb2.PutOutcome.PUT_OUTCOME_COMMITTED if committed
            else state_pb2.PutOutcome.PUT_OUTCOME_CONFLICT
        )
        return state_pb2.PutResponse(outcome=outcome, revision=rev)

    def CreateIfAbsent(self, request, context):
        """Atomically create the key only if absent (ADR-049). COMMITTED == created; CONFLICT ==
        already existed (no write). In-memory never returns UNKNOWN."""
        try:
            validate_key(request.key, False)
        except GrammarError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        created, rev = self.store.create_if_absent(request.key, request.value)
        outcome = (
            state_pb2.PutOutcome.PUT_OUTCOME_COMMITTED if created
            else state_pb2.PutOutcome.PUT_OUTCOME_CONFLICT
        )
        return state_pb2.CreateIfAbsentResponse(outcome=outcome, revision=rev)

    def List(self, request, context):
        try:
            validate_key(request.prefix, True)
        except GrammarError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        return state_pb2.ListResponse(keys=self.store.list(request.prefix))

    def Watch(self, request, context):
        """Streams the backlog under the prefix in revision order, then ends. A real
        Watch would stay open + stream live changes; this reference bounds it for
        deterministic conformance (ordered replay is the property under test)."""
        try:
            validate_key(request.prefix, True)
        except GrammarError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        for key, value, rev in self.store.watch_backlog(request.prefix, request.from_revision):
            yield state_pb2.WatchResponse(
                type=state_pb2.WatchResponse.TYPE_PUT, key=key, value=value, revision=rev
            )
