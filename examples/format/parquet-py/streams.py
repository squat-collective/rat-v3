"""Bidirectional real-Arrow data leg for the format references.

Format moves rows BOTH ways: a source ArrowStream feeds Append/Merge/Overwrite, and
Resolve returns a result ArrowStream. Both legs here carry REAL Arrow IPC (typed
schema + columnar batches), serialized + deserialized via Arrow IPC — the real data
leg, not the toy string-row registry. Transport is still an in-process registry (a
stand-in for Arrow Flight, out of scope for a reference). Shared by parquet-py +
delta-py.
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
        if stream is None or not stream.ticket:
            return None
        with self._lock:
            ipc = self._batches.pop(stream.ticket, None)
        if ipc is None:
            return None
        return pa.ipc.open_stream(ipc).read_all()
