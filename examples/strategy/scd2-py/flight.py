"""Arrow Flight host/pull for the SCD2 strategy's data-plane legs.

SCD2 is both an Arrow CONSUMER (it pulls the source + target scans to compute the
delta) and an Arrow PRODUCER (it hosts the computed delta for `format.merge` to pull).
Same real-Flight pattern as the format references — a strategy that touches bulk data
hosts/dials it like any data-plane plugin. (The composition test injects its own
host/pull; this file is for the standalone server.)
"""

import os
import threading

import pyarrow as pa
import pyarrow.flight as fl

from rat.common.v1 import data_pb2


class FlightHost(fl.FlightServerBase):
    def __init__(self) -> None:
        super().__init__("grpc://127.0.0.1:0")
        self._tables = {}
        self._lock = threading.Lock()
        self._location = f"grpc://127.0.0.1:{self.port}"
        threading.Thread(target=self.serve, daemon=True).start()

    def host_rows(self, rows) -> data_pb2.ArrowStream:
        ticket = os.urandom(16)
        table = pa.Table.from_pylist(rows) if rows else pa.table({})
        with self._lock:
            self._tables[ticket] = table
        return data_pb2.ArrowStream(
            endpoint=self._location,
            ticket=ticket,
            transport=data_pb2.ArrowTransport.ARROW_TRANSPORT_FLIGHT,
            role=data_pb2.ArrowStreamRole.ARROW_STREAM_ROLE_PRODUCER_HOSTED,
            ipc_schema=table.schema.serialize().to_pybytes(),
        )

    def do_get(self, context, ticket):
        with self._lock:
            table = self._tables.pop(bytes(ticket.ticket), None)
        if table is None:
            raise fl.FlightServerError("unknown or already-consumed ticket")
        return fl.RecordBatchStream(table)

    def stop(self) -> None:
        self.shutdown()


def pull(stream: data_pb2.ArrowStream):
    if stream is None or not stream.ticket:
        return []
    client = fl.connect(stream.endpoint)
    try:
        return client.do_get(fl.Ticket(stream.ticket)).read_all().to_pylist()
    finally:
        client.close()
