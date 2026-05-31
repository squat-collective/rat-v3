"""REAL Arrow Flight transport for the data leg — replaces the in-process Arrow-IPC
registry stand-in the other references use.

The RAT contract says bulk rows move out-of-band as Arrow over a side channel the
plugins negotiate, described by a common.v1.ArrowStream {endpoint, ticket,
transport=FLIGHT, role}. This makes that literal: the party that PRODUCES data (the
format plugin for Resolve results; the caller for Append/Merge/Overwrite sources)
runs a real `pyarrow.flight` server on a real TCP port; the descriptor carries that
`grpc://host:port` + a ticket; the CONSUMER dials it and pulls the Arrow stream via
Flight DoGet — over the wire, not an in-process dict.

Both legs use PRODUCER_HOSTED (the data-holder hosts; the data-needer DoGets), which
matches the contract: Resolve → producer-hosted (caller DoGets); Append source →
caller-hosted (the format DoGets from the caller's endpoint). Single-use tickets
(SEC-14): a DoGet consumes the ticket.
"""

import os
import threading

import pyarrow as pa
import pyarrow.flight as fl

from rat.common.v1 import data_pb2


class FlightHost(fl.FlightServerBase):
    """A real Arrow Flight server holding tables by ticket, serving DoGet. Binds an
    ephemeral localhost port and serves on a background thread."""

    def __init__(self) -> None:
        super().__init__("grpc://127.0.0.1:0")
        self._tables = {}
        self._lock = threading.Lock()
        self._location = f"grpc://127.0.0.1:{self.port}"
        self._thread = threading.Thread(target=self.serve, daemon=True)
        self._thread.start()

    def put(self, table: pa.Table) -> data_pb2.ArrowStream:
        ticket = os.urandom(16)
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
            table = self._tables.pop(bytes(ticket.ticket), None)  # single-use
        if table is None:
            raise fl.FlightServerError("unknown or already-consumed ticket")
        return fl.RecordBatchStream(table)

    def stop(self) -> None:
        self.shutdown()


def flight_pull(stream: data_pb2.ArrowStream) -> pa.Table:
    """Dial the descriptor's Flight endpoint and DoGet its ticket — a real Flight
    round-trip over a TCP socket. None if the descriptor is empty."""
    if stream is None or not stream.ticket:
        return None
    client = fl.connect(stream.endpoint)
    try:
        return client.do_get(fl.Ticket(stream.ticket)).read_all()
    finally:
        client.close()
