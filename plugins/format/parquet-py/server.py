"""The FormatService gRPC implementation over a real Parquet store, with the data
leg carried by REAL Arrow Flight (flight.py) — not the in-process Arrow-IPC registry
the other references use.

Resolve hosts the table's rows on this plugin's Flight server and returns a
producer-hosted ArrowStream pointing at it (the caller DoGets it). Append/Merge/
Overwrite DoGet the source rows from the caller's Flight endpoint (named in the
source ArrowStream). Empty TableRef / missing merge_keys → INVALID_ARGUMENT.
RequestContext is NOT a field (ADR-007).
"""

import grpc
import pyarrow as pa

from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from flight import FlightHost, flight_pull
from store import Store


def _table_id(table, context):
    if not table or not table.identifier:
        context.abort(grpc.StatusCode.INVALID_ARGUMENT, "table.identifier is required")
    return table.identifier


def _rows(source):
    """Pull the source rows from the caller's Flight endpoint (real DoGet)."""
    table = flight_pull(source)
    return table.to_pylist() if table is not None else []


def _write_result(n):
    return data_pb2.WriteResult(rows_affected=n, snapshot_id="")


class FormatServicer(format_pb2_grpc.FormatServiceServicer):
    def __init__(self, store: Store) -> None:
        self.store = store
        self.flight = FlightHost()  # serves Resolve results over real Arrow Flight

    def close(self) -> None:
        self.flight.stop()

    def Resolve(self, request, context):
        ident = _table_id(request.table, context)
        rows = self.store.scan(ident)
        table = pa.Table.from_pylist(rows) if rows else pa.table({})
        return format_pb2.ResolveResponse(stream=self.flight.put(table))

    def Append(self, request, context):
        ident = _table_id(request.table, context)
        n = self.store.append(ident, _rows(request.source))
        return format_pb2.AppendResponse(result=_write_result(n))

    def Merge(self, request, context):
        ident = _table_id(request.table, context)
        if not request.merge_keys:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "merge_keys is required for Merge")
        n = self.store.merge(ident, list(request.merge_keys), _rows(request.source))
        return format_pb2.MergeResponse(result=_write_result(n))

    def Overwrite(self, request, context):
        ident = _table_id(request.table, context)
        n = self.store.overwrite(ident, _rows(request.source))
        return format_pb2.OverwriteResponse(result=_write_result(n))

    def Maintain(self, request, context):
        ident = _table_id(request.table, context)
        self.store.maintain(ident)
        return format_pb2.MaintainResponse(result=data_pb2.WriteResult(snapshot_id="maintained"))
