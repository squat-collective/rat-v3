"""The FormatService gRPC implementation over a real file-format store. Identical
for both real-format references; only store.py differs.

Resolve → a producer-hosted ArrowStream of the table's rows (read from real files);
Append/Merge/Overwrite pull rows from a caller-hosted source ArrowStream and write
them; Maintain compacts. Empty TableRef / missing merge_keys → INVALID_ARGUMENT.
RequestContext is NOT a field (ADR-007).
"""

import grpc

from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from store import Store
from streams import StreamRegistry


def _table_id(table, context):
    if not table or not table.identifier:
        context.abort(grpc.StatusCode.INVALID_ARGUMENT, "table.identifier is required")
    return table.identifier


def _rows(stream_registry, source):
    table = stream_registry.pull(source)
    return table.to_pylist() if table is not None else []


def _write_result(n):
    return data_pb2.WriteResult(rows_affected=n, snapshot_id="")


class FormatServicer(format_pb2_grpc.FormatServiceServicer):
    def __init__(self, store: Store) -> None:
        self.store = store
        self.streams = StreamRegistry()

    def Resolve(self, request, context):
        ident = _table_id(request.table, context)
        rows = self.store.scan(ident)
        # Even an empty result is a real (empty-schema-safe) Arrow table.
        import pyarrow as pa
        table = pa.Table.from_pylist(rows) if rows else pa.table({})
        return format_pb2.ResolveResponse(stream=self.streams.put(table))

    def Append(self, request, context):
        ident = _table_id(request.table, context)
        n = self.store.append(ident, _rows(self.streams, request.source))
        return format_pb2.AppendResponse(result=_write_result(n))

    def Merge(self, request, context):
        ident = _table_id(request.table, context)
        if not request.merge_keys:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "merge_keys is required for Merge")
        n = self.store.merge(ident, list(request.merge_keys), _rows(self.streams, request.source))
        return format_pb2.MergeResponse(result=_write_result(n))

    def Overwrite(self, request, context):
        ident = _table_id(request.table, context)
        n = self.store.overwrite(ident, _rows(self.streams, request.source))
        return format_pb2.OverwriteResponse(result=_write_result(n))

    def Maintain(self, request, context):
        ident = _table_id(request.table, context)
        self.store.maintain(ident)
        return format_pb2.MaintainResponse(result=data_pb2.WriteResult(snapshot_id="maintained"))
