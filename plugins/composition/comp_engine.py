"""Composition EngineService — the cross-axis engine the per-axis refs don't yet do.

The per-axis engine references (plugins/engine/{duckdb,datafusion}-py) run self-
contained SQL against their own connection and IGNORE QueryRequest.tables, and carry
their result on an in-process Arrow stand-in (streams.py). That is sufficient for the
engine's OWN golden vectors but does NOT compose — engine.proto:51 intends `tables`
to be bound by resolving each through a format `scan` capability, and the result must
travel on the real ArrowStream transport a downstream format can dial.

This servicer closes both gaps, reusing the REAL engine backend (DuckDB / DataFusion
`Engine`, imported unchanged) but:
  - for each QueryRequest.tables ref, INVOKE rat://format/v1/scan via the gateway,
    DoGet the real Arrow over Flight, and BIND it into the backend under the ref's
    identifier (so `SELECT ... FROM <identifier>` resolves);
  - run the transform SQL → a real Arrow result;
  - HOST that result on a real Flight server and return its descriptor.

Binding (the one engine-specific seam) is supplied by the harness as `bind(name,
table)` — DuckDB `con.register`, DataFusion `ctx.register_record_batches` — so this
file stays engine-agnostic.
"""

import grpc
import pyarrow as pa

from rat.engine.v1 import engine_pb2, engine_pb2_grpc
from rat.format.v1 import format_pb2

from flight import FlightHost, flight_pull

CAP_SCAN = "rat://format/v1/scan"


class CompositionEngineServicer(engine_pb2_grpc.EngineServiceServicer):
    def __init__(self, backend, bind, invoke) -> None:
        self._backend = backend          # the real Engine (duckdb/datafusion)
        self._bind = bind                # bind(name, pa.Table) -> registers a source
        self._invoke = invoke            # gateway invoker (requires rat://format/v1/scan)
        self._flight = FlightHost()      # hosts the transformed result for the format

    def close(self) -> None:
        self._flight.stop()

    def Query(self, request, context):
        # Resolve + bind every source ref through the format `scan` capability —
        # a real cross-axis Flight round-trip, by capability, never by name.
        for ref in request.tables:
            scan = self._invoke(CAP_SCAN, format_pb2.ResolveRequest(table=ref))
            src = flight_pull(scan.stream)
            if src is None:
                src = pa.table({})
            self._bind(ref.identifier, src)
        try:
            result = self._backend.query(request.sql)  # real Arrow table
        except Exception as e:  # EngineError or backend parse/catalog error
            code = getattr(e, "code", grpc.StatusCode.INVALID_ARGUMENT)
            context.abort(code, str(e))
        return engine_pb2.QueryResponse(stream=self._flight.put(result))
