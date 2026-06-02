"""The data-dev gateway — a thin backend-for-frontend the VS Code extension talks to.

> 🛰️ EXPLORATORY (experiments/data-dev-plane). Additive; no proto/axis change.

WHY a BFF and not direct gRPC from the extension (finding F9, README §10): the frozen
contracts keep bulk data OFF the control plane — `engine.Query` returns an `ArrowStream`
the consumer pulls out-of-band. The reference engine's Arrow leg is an IN-PROCESS
registry (a stand-in for Arrow Flight), so an external client (the editor) cannot pull
query rows over the wire. This gateway owns the in-proc stack, so it CAN pull that Arrow
— and it re-exposes results as plain JSON the editor renders. A production engine with a
real Flight endpoint would let a thin client (the generated Connect TS SDK, ADR-018) pull
directly; until then the BFF closes the data leg. The frozen CONTROL capabilities
(catalog browse, strategy apply) are exactly what the generated TS SDK would call — the
gateway just relays them so the demo runs from one endpoint.

It owns the same in-proc engine + catalog + strategy the runners use, on a local
DuckLake, seeds a corpus + runs the incremental-embed strategy once at boot, and serves:

  GET  /api/health                 plugin liveness/health
  GET  /api/tables                 catalog tables (+ current snapshot, row count)
  GET  /api/snapshots?table=<id>   the table's DuckLake snapshot ids
  POST /api/query    {sql}         run SQL → {columns, rows}
  POST /api/search   {query,k}     embed the query → vss cosine rank → rows
  POST /api/pipeline/run           land the next batch + re-run the strategy (incremental)

Run: `make data-dev-gateway` (publishes the port). Stdlib only on the HTTP side.
"""

import importlib
import json
import os
import sys
import tempfile
import threading
from concurrent import futures
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc
from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))


def _load_plugin(reldir, bare_modules):
    d = os.path.join(ROOT, reldir)
    sys.path.insert(0, d)
    try:
        for m in bare_modules:
            sys.modules.pop(m, None)
        return {m: importlib.import_module(m) for m in bare_modules}
    finally:
        sys.path.remove(d)
        for m in bare_modules:
            sys.modules.pop(m, None)

_eng = _load_plugin("examples/engine/duckdb-ml-py", ["embed", "store", "streams", "server"])
_cat = _load_plugin("examples/catalog/ducklake-py", ["store", "server"])
_str = _load_plugin("examples/strategy/incremental-embed-py", ["store", "server"])
_gw = _load_plugin("examples/composition", ["gateway"])
Gateway = _gw["gateway"].Gateway

MODEL, DIM = "hash-256", 256
OPTIONS = {"key": "id", "text_column": "text", "embed_model": MODEL,
           "watermark_column": "_ingested_at",
           "columns": ["id", "text", "rating", "_ingested_at"], "alias": "lake"}

BATCHES = [
    [(1, "the battery life is incredible lasts two full days", 5),
     (2, "battery drains way too fast by midday it is dead", 2),
     (3, "gorgeous bright screen great for watching video", 5),
     (4, "the screen cracked after a small drop poor build", 2),
     (5, "camera takes stunning photos in low light", 5),
     (6, "camera is grainy and slow to focus disappointing", 2),
     (7, "amazing value for the price highly recommend", 5),
     (8, "overpriced for what you get not worth the money", 1),
     (9, "solid build quality feels premium in the hand", 4),
     (10, "charging is slow but the battery capacity is huge", 4),
     (11, "the display colors are vivid and the screen is huge", 5),
     (12, "great phone but the price is a bit steep", 4)],
    [(13, "the fingerprint sensor is fast and reliable", 5),
     (14, "speaker volume is weak and tinny at high levels", 2),
     (15, "battery easily lasts a long weekend trip", 5)],
    [(16, "wireless charging is super convenient on the desk", 5),
     (17, "the phone overheats while gaming for long", 2),
     (18, "face unlock works even in the dark", 5)],
]


def _serve(add_fn, servicer):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    add_fn(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    return server, grpc.insecure_channel(f"127.0.0.1:{port}")


def _svc_desc(pb2, name):
    return pb2.DESCRIPTOR.services_by_name[name]


def _sql_str(s):
    return "'" + str(s).replace("'", "''") + "'"


def _display(value):
    """Render a cell for JSON. Embeddings (lists of float) collapse to a placeholder so
    payloads stay small — the UI shows '[256 dims]', not 256 floats. Datetimes (e.g. the
    `_ingested_at` watermark column) become ISO strings so SELECT * is JSON-safe."""
    import datetime
    if isinstance(value, list):
        return f"[{len(value)} dims]" if value and isinstance(value[0], float) else value
    if isinstance(value, (datetime.datetime, datetime.date, datetime.time)):
        return value.isoformat()
    return value


def _json_default(o):
    """Last-resort JSON encoder for types json.dumps can't handle natively (datetimes,
    Decimal, bytes, …) — so no endpoint can 500 on an exotic cell type."""
    import datetime
    from decimal import Decimal
    if isinstance(o, (datetime.datetime, datetime.date, datetime.time)):
        return o.isoformat()
    if isinstance(o, Decimal):
        return float(o)
    if isinstance(o, (bytes, bytearray)):
        return o.hex()
    return str(o)


class Stack:
    """The in-proc data-dev plane the gateway owns (engine + catalog + strategy)."""

    def __init__(self, tmp):
        meta = f"sqlite:{os.path.join(tmp, 'lakemeta.db')}"
        data = os.path.join(tmp, "lakedata") + "/"
        self.engine = _eng["store"].Engine(_eng["store"].DuckLakeConfig(meta, data))
        self.eng_obj = _eng["server"].EngineServicer(self.engine)
        eng_srv, eng_ch = _serve(engine_pb2_grpc.add_EngineServiceServicer_to_server, self.eng_obj)
        self.engine_stub = engine_pb2_grpc.EngineServiceStub(eng_ch)
        self.catalog = _cat["store"].Catalog(os.path.join(tmp, "tracking.db"), meta, data)
        cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server,
                                 _cat["server"].CatalogServicer(self.catalog))
        self.catalog_stub = catalog_pb2_grpc.CatalogServiceStub(cat_ch)
        gw = Gateway()
        gw.register(engine_pb2_grpc.EngineServiceStub(eng_ch), _svc_desc(engine_pb2, "EngineService"))
        gw.register(catalog_pb2_grpc.CatalogServiceStub(cat_ch), _svc_desc(catalog_pb2, "CatalogService"))
        invoke = gw.invoker_for(list(_str["store"].REQUIRES))
        strat_srv, strat_ch = _serve(strategy_pb2_grpc.add_StrategyServiceServicer_to_server,
                                     _str["server"].StrategyServicer(invoke))
        self.strategy_stub = strategy_pb2_grpc.StrategyServiceStub(strat_ch)
        self._servers = (eng_srv, cat_srv, strat_srv)
        self._lock = threading.Lock()
        self._batch = 0
        self.execute("CREATE TABLE lake.reviews_raw(id INTEGER, text VARCHAR, rating INTEGER, _ingested_at TIMESTAMP)")
        self.run_pipeline()  # land batch 0 + build lake.reviews so the UI has data at boot

    # --- engine helpers -----------------------------------------------------------
    def execute(self, sql):
        return self.engine_stub.Execute(engine_pb2.ExecuteRequest(sql=sql)).result

    def query(self, sql):
        return self.eng_obj.streams.pull(self.engine_stub.Query(engine_pb2.QueryRequest(sql=sql)).stream)

    # --- API operations -----------------------------------------------------------
    def health(self):
        try:
            self.query("SELECT 1")
            engine_ok = "Healthy"
        except Exception:
            engine_ok = "Degraded"
        return {"plugins": [
            {"name": "rat-engine-duckdb-ml", "kind": "engine", "status": engine_ok,
             "extensions": sorted(self.engine.loaded)},
            {"name": "rat-catalog-ducklake", "kind": "catalog", "status": "Healthy"},
            {"name": "rat-strategy-incremental-embed", "kind": "strategy", "status": "Healthy"},
        ]}

    def tables(self):
        rows = self.query(
            "SELECT table_name FROM information_schema.tables WHERE table_catalog='lake' ORDER BY table_name"
        ).to_pylist()
        out = []
        for r in rows:
            name = r["table_name"]
            n = self.query(f"SELECT count(*) AS n FROM lake.{name}").to_pylist()[0]["n"]
            snap = ""
            try:
                tref = self.catalog_stub.GetTable(catalog_pb2.GetTableRequest(identifier=name)).table
                snap = tref.uri.split("#")[-1] if "#" in tref.uri else ""
            except grpc.RpcError:
                pass
            out.append({"identifier": name, "rows": n, "snapshot": snap, "branch": "main"})
        return {"tables": out}

    def snapshots(self, table):
        snaps = self.query(f"SELECT snapshot_id FROM lake.snapshots() ORDER BY snapshot_id").to_pylist()
        return {"table": table, "snapshots": [f"snap-{s['snapshot_id']}" for s in snaps]}

    def run_query(self, sql, limit=200):
        tbl = self.query(f"SELECT * FROM ({sql}) LIMIT {int(limit)}" if "limit" not in sql.lower() else sql)
        cols = list(tbl.schema.names)
        rows = [[_display(rec.get(c)) for c in cols] for rec in tbl.to_pylist()]
        return {"columns": cols, "rows": rows}

    def search(self, q, k=10):
        sql = (f"SELECT id, rating, text, "
               f"array_cosine_distance(embedding::FLOAT[{DIM}], embed({_sql_str(q)},{_sql_str(MODEL)})::FLOAT[{DIM}]) AS dist "
               f"FROM lake.reviews ORDER BY dist LIMIT {int(k)}")
        return {"query": q, "results": self.query(sql).to_pylist()}

    def run_pipeline(self):
        """Land the next batch into reviews_raw + run the incremental-embed strategy. The
        first call lands batch 0 and builds lake.reviews; later calls show incrementality
        (only the newly-landed rows embed). Cycles through the batches."""
        with self._lock:
            batch = BATCHES[self._batch % len(BATCHES)]
            day = self._batch + 1
            vals = ",".join(f"({i},{_sql_str(t)},{r},TIMESTAMP '2026-01-{day:02d} 10:00:00')"
                            for i, t, r in batch)
            self.execute(f"INSERT INTO lake.reviews_raw(id,text,rating,_ingested_at) VALUES {vals}")
            run_id = f"ui-run-{self._batch}"
            res = self.strategy_stub.Apply(strategy_pb2.ApplyRequest(
                source=data_pb2.TableRef(identifier="reviews_raw"),
                target=data_pb2.TableRef(identifier="reviews"),
                options=json.dumps(OPTIONS).encode("utf-8"), idempotency_key=run_id)).result
            total = self.query("SELECT count(*) AS n FROM lake.reviews").to_pylist()[0]["n"]
            self._batch += 1
            return {"run_id": run_id, "embedded": res.rows_affected, "snapshot": res.snapshot_id,
                    "already_applied": res.already_applied, "total": total,
                    "landed_batch": (self._batch - 1)}

    def close(self):
        try:
            self.catalog.close(); self.engine.con.close()
        except Exception:
            pass
        for s in self._servers:
            s.stop(None)


def make_handler(stack: Stack):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):  # quiet
            pass

        def _send(self, code, payload):
            body = json.dumps(payload, default=_json_default).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Access-Control-Allow-Origin", "*")
            self.send_header("Access-Control-Allow-Headers", "Content-Type")
            self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def _body(self):
            n = int(self.headers.get("Content-Length", 0) or 0)
            return json.loads(self.rfile.read(n) or b"{}") if n else {}

        def do_OPTIONS(self):
            self._send(204, {})

        def do_GET(self):
            from urllib.parse import urlparse, parse_qs
            u = urlparse(self.path)
            try:
                if u.path == "/api/health":
                    self._send(200, stack.health())
                elif u.path == "/api/tables":
                    self._send(200, stack.tables())
                elif u.path == "/api/snapshots":
                    self._send(200, stack.snapshots(parse_qs(u.query).get("table", [""])[0]))
                else:
                    self._send(404, {"error": "not found"})
            except Exception as e:
                self._send(500, {"error": str(e)})

        def do_POST(self):
            try:
                body = self._body()
                if self.path == "/api/query":
                    self._send(200, stack.run_query(body.get("sql", ""), body.get("limit", 200)))
                elif self.path == "/api/search":
                    self._send(200, stack.search(body.get("query", ""), body.get("k", 10)))
                elif self.path == "/api/pipeline/run":
                    self._send(200, stack.run_pipeline())
                else:
                    self._send(404, {"error": "not found"})
            except Exception as e:
                self._send(500, {"error": str(e)})

    return Handler


def main():
    port = int(os.environ.get("RAT_GATEWAY_PORT", "8787"))
    tmp = tempfile.mkdtemp(prefix="rat-data-dev-gateway-")
    stack = Stack(tmp)
    httpd = ThreadingHTTPServer(("0.0.0.0", port), make_handler(stack))
    print(f"rat data-dev gateway listening on http://0.0.0.0:{port} "
          f"(tables: reviews_raw, reviews — {stack.tables()['tables']})", flush=True)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        stack.close()


if __name__ == "__main__":
    main()
