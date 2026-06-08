"""Real Arrow IPC handoff for the engine result leg.

Unlike the toy in-memory engine (which stashed string rows), this serializes the
query result to REAL Arrow IPC bytes and carries them under a ticket — the typed-
Arrow data leg. The transport is still an in-process registry (a stand-in for Arrow
Flight, kept out of scope for a reference), but the DATA is genuine typed Arrow:
schema + columnar batches, serialized + deserialized via Arrow IPC. Shared by both
real-engine references (duckdb-py, datafusion-py).
"""

import os
import threading

import pyarrow as pa

from rat.common.v1 import data_pb2


class StreamRegistry:
    def __init__(self) -> None:
        self._batches = {}
        self._lock = threading.Lock()

    def put(self, table: pa.Table) -> data_pb2.ArrowStream:
        ticket = os.urandom(16)
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, table.schema) as writer:
            writer.write_table(table)
        with self._lock:
            self._batches[ticket] = sink.getvalue().to_pybytes()
        return data_pb2.ArrowStream(
            endpoint="inproc://arrow",
            ticket=ticket,
            transport=data_pb2.ArrowTransport.ARROW_TRANSPORT_FLIGHT,
            role=data_pb2.ArrowStreamRole.ARROW_STREAM_ROLE_PRODUCER_HOSTED,
            ipc_schema=table.schema.serialize().to_pybytes(),
        )

    def pull(self, stream: data_pb2.ArrowStream) -> pa.Table:
        with self._lock:
            ipc = self._batches.pop(stream.ticket, None)
        if ipc is None:
            return None
        return pa.ipc.open_stream(ipc).read_all()
