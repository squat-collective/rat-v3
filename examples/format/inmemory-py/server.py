"""The FormatService gRPC implementation (Python) — second `format` reference.

Implements the five format/v1 RPCs against the in-memory store, honoring the wire
contract: each mutating RPC pulls rows from its caller-hosted source ArrowStream and
returns a WriteResult; Resolve returns a producer-hosted ArrowStream the caller pulls
from. RequestContext is accepted on every call (the reference does not forge/trust
identity — it just threads the context, as a real plugin would before the
core-mediated gateway stamps it).
"""

import grpc

from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from store import Store
from streams import StreamRegistry


def _snapshot_id(n: int) -> str:
    return f"snap-{n}"


def _table_id(table: data_pb2.TableRef, context: grpc.ServicerContext) -> str:
    """Extract identifier, aborting INVALID_ARGUMENT on an empty ref (a
    transport-level failure per the error-model convention)."""
    if not table or not table.identifier:
        context.abort(grpc.StatusCode.INVALID_ARGUMENT, "table.identifier is required")
    return table.identifier


def _write_result(rows: int, snapshot: int) -> data_pb2.WriteResult:
    return data_pb2.WriteResult(rows_affected=rows, snapshot_id=_snapshot_id(snapshot))


class FormatServicer(format_pb2_grpc.FormatServiceServicer):
    def __init__(self) -> None:
        self.store = Store()
        self.streams = StreamRegistry()

    # Resolve — rat://format/v1/scan. Returns a producer-hosted ArrowStream.
    def Resolve(self, request, context):
        ident = _table_id(request.table, context)
        rows = self.store.scan(ident, list(request.columns))
        return format_pb2.ResolveResponse(stream=self.streams.put(rows))

    # Append — rat://format/v1/append.
    def Append(self, request, context):
        ident = _table_id(request.table, context)
        rows = self.streams.pull(request.source)
        n, snap = self.store.append(ident, rows)
        return format_pb2.AppendResponse(result=_write_result(n, snap))

    # Merge — rat://format/v1/merge. merge_keys required (no upsert without a key).
    def Merge(self, request, context):
        ident = _table_id(request.table, context)
        if not request.merge_keys:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "merge_keys is required for Merge")
        rows = self.streams.pull(request.source)
        n, snap = self.store.merge(ident, list(request.merge_keys), rows)
        return format_pb2.MergeResponse(result=_write_result(n, snap))

    # Overwrite — rat://format/v1/overwrite.
    def Overwrite(self, request, context):
        ident = _table_id(request.table, context)
        rows = self.streams.pull(request.source)
        n, snap = self.store.overwrite(ident, rows)
        return format_pb2.OverwriteResponse(result=_write_result(n, snap))

    # Maintain — rat://format/v1/maintain. No-op upkeep; bumps the snapshot.
    # rows_affected is genuinely unknown for maintenance -> leave it absent
    # (proto3 optional), per WriteResult's documented semantics.
    def Maintain(self, request, context):
        ident = _table_id(request.table, context)
        snap = self.store.maintain(ident)
        return format_pb2.MaintainResponse(result=data_pb2.WriteResult(snapshot_id=_snapshot_id(snap)))
