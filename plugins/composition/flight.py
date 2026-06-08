"""Real Arrow Flight transport for the composition's cross-axis data legs.

Identical in spirit to plugins/format/parquet-py/flight.py: a producer hosts a real
`pyarrow.flight` server on an ephemeral localhost port and returns a common.v1.
ArrowStream {endpoint, ticket, transport=FLIGHT, role=PRODUCER_HOSTED}; the consumer
dials it and DoGets. In the composition, BOTH the engine (result leg) and the format
(scan leg) host one of these, and the other axis dials it — so the engine<->format
Arrow handoff is a real Flight round-trip over a socket, not an in-process dict. That
is the handoff ADR-003 most wants to stress ("only worked because both used the same
Arrow dialect" — engine.proto:18).
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
        self._thread = threading.Thread(target=self.serve, daemon=True)
        self._thread.start()

    def put(self, table: pa.Table, declare_rows=None) -> data_pb2.ArrowStream:
        ticket = os.urandom(16)
        with self._lock:
            self._tables[ticket] = table
        # C2 (ADR-012): the producer DECLARES expected_rows so a truncated transfer is
        # detectable. `declare_rows` overrides the true count only for the truncation
        # negative test; production always declares the real count.
        return data_pb2.ArrowStream(
            endpoint=self._location,
            ticket=ticket,
            transport=data_pb2.ArrowTransport.ARROW_TRANSPORT_FLIGHT,
            role=data_pb2.ArrowStreamRole.ARROW_STREAM_ROLE_PRODUCER_HOSTED,
            ipc_schema=table.schema.serialize().to_pybytes(),
            expected_rows=(table.num_rows if declare_rows is None else declare_rows),
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
    if stream is None or not stream.ticket:
        return None
    client = fl.connect(stream.endpoint)
    try:
        table = client.do_get(fl.Ticket(stream.ticket)).read_all()
    finally:
        client.close()
    # C2 (ADR-012): if the producer declared expected_rows, the consumer MUST verify it
    # received exactly that many before treating the transfer as complete. A shortfall
    # means a TRUNCATED stream (producer died mid-send) — fail the write, never commit a
    # partial dataset (the silent SCD2-history-corruption path the board found).
    if stream.HasField("expected_rows") and table.num_rows != stream.expected_rows:
        raise ValueError(
            f"ArrowStream truncated: received {table.num_rows} rows, producer declared "
            f"{stream.expected_rows} (C2 — consumer MUST fail the write)"
        )
    return table
