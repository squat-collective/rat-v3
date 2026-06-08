"""run-strategy.py — the incremental-embed ELT driving the data-dev plane.

Build-order step 4 (experiment README §11): the real strategy (plugins/strategy/
incremental-embed-py) over a live stack — strategy → gateway → engine + catalog, all
gRPC, the strategy naming no concrete plugin. It proves the §5.4 ELT pattern AND C1
idempotency on a genuine incremental workload:

  run 1 (initial load)   →  embeds the whole corpus
  run 2 (new rows landed) →  embeds ONLY the delta   (incrementality)
  run 2 replay (no new)   →  embeds 0, commit already_applied   (idempotency, C1)

The strategy reaches engine/catalog ONLY through the capability-invoke gateway
(plugins/composition/gateway.py) bound to its `requires` set — C5 deny-by-default. The
runner just (a) provides that gateway, (b) lands raw rows in lake.reviews_raw (the
ingestion an upstream would do), and (c) calls strategy.Apply over the wire.

Run: `make data-dev-strategy`.
"""

import importlib
import json
import os
import sys
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.engine.v1 import engine_pb2, engine_pb2_grpc
from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc
from rat.common.v1 import data_pb2

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))


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

_eng = _load_plugin("plugins/engine/duckdb-ml-py", ["embed", "store", "streams", "server"])
eng_store, eng_server = _eng["store"], _eng["server"]
_cat = _load_plugin("plugins/catalog/ducklake-py", ["store", "server"])
cat_store, cat_server = _cat["store"], _cat["server"]
_str = _load_plugin("plugins/strategy/incremental-embed-py", ["store", "server"])
strat_store, strat_server = _str["store"], _str["server"]
_gw = _load_plugin("plugins/composition", ["gateway"])
Gateway = _gw["gateway"].Gateway

MODEL, DIM = "hash-256", 256
OPTIONS = {"key": "id", "text_column": "text", "embed_model": MODEL,
           "watermark_column": "_ingested_at",
           "columns": ["id", "text", "rating", "_ingested_at"], "alias": "lake"}

# batch 1 — landed on day 1; batch 2 — landed on day 2 (later watermark).
BATCH1 = [
    (1, "the battery life is incredible lasts two full days", 5),
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
    (12, "great phone but the price is a bit steep", 4),
]
BATCH2 = [
    (13, "the fingerprint sensor is fast and reliable", 5),
    (14, "speaker volume is weak and tinny at high levels", 2),
    (15, "battery easily lasts a long weekend trip", 5),
]


def _serve(add_fn, servicer):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    add_fn(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    return server, grpc.insecure_channel(f"127.0.0.1:{port}")


def _svc_desc(pb2, name):
    return pb2.DESCRIPTOR.services_by_name[name]


def _q(s):
    return "'" + s.replace("'", "''") + "'"


def main():
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        meta = f"sqlite:{os.path.join(tmp, 'lakemeta.db')}"
        data = os.path.join(tmp, "lakedata") + "/"

        engine = eng_store.Engine(eng_store.DuckLakeConfig(meta, data))
        if "ducklake" not in engine.loaded:
            print("ERROR: ducklake extension not loaded:", sorted(engine.loaded)); sys.exit(2)
        eng_obj = eng_server.EngineServicer(engine)
        eng_srv, eng_ch = _serve(engine_pb2_grpc.add_EngineServiceServicer_to_server, eng_obj)
        engine_stub = engine_pb2_grpc.EngineServiceStub(eng_ch)

        catalog = cat_store.Catalog(os.path.join(tmp, "tracking.db"), meta, data)
        cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server,
                                 cat_server.CatalogServicer(catalog))

        # --- the capability-invoke gateway the strategy is handed (C5-bound) ----------
        gw = Gateway()
        gw.register(engine_pb2_grpc.EngineServiceStub(eng_ch), _svc_desc(engine_pb2, "EngineService"))
        gw.register(catalog_pb2_grpc.CatalogServiceStub(cat_ch), _svc_desc(catalog_pb2, "CatalogService"))
        invoke = gw.invoker_for(list(strat_store.REQUIRES))

        strat_srv, strat_ch = _serve(strategy_pb2_grpc.add_StrategyServiceServicer_to_server,
                                     strat_server.StrategyServicer(invoke))
        strategy_stub = strategy_pb2_grpc.StrategyServiceStub(strat_ch)

        def execute(sql):
            return engine_stub.Execute(engine_pb2.ExecuteRequest(sql=sql)).result

        def query(sql):
            return eng_obj.streams.pull(engine_stub.Query(engine_pb2.QueryRequest(sql=sql)).stream)

        def land(rows, day):
            vals = ",".join(f"({i},{_q(t)},{r},TIMESTAMP '2026-01-{day:02d} 10:00:00')" for i, t, r in rows)
            execute(f"INSERT INTO lake.reviews_raw(id,text,rating,_ingested_at) VALUES {vals}")

        def apply(run_id):
            return strategy_stub.Apply(strategy_pb2.ApplyRequest(
                source=data_pb2.TableRef(identifier="reviews_raw"),
                target=data_pb2.TableRef(identifier="reviews"),
                options=json.dumps(OPTIONS).encode("utf-8"),
                idempotency_key=run_id)).result

        ok = True
        try:
            print("🛰️  data-dev plane — incremental-embed STRATEGY (strategy→gateway→engine+catalog)\n")
            # ingestion: raw landing table the strategy reads from
            execute("CREATE TABLE lake.reviews_raw(id INTEGER, text VARCHAR, rating INTEGER, _ingested_at TIMESTAMP)")

            land(BATCH1, day=1)
            r1 = apply("run-1")
            print(f"run 1 (initial)     → embedded={r1.rows_affected}  snapshot={r1.snapshot_id!r}  "
                  f"already_applied={r1.already_applied}")
            ok = ok and r1.rows_affected == len(BATCH1) and not r1.already_applied

            land(BATCH2, day=2)
            r2 = apply("run-2")
            print(f"run 2 (incremental) → embedded={r2.rows_affected}  snapshot={r2.snapshot_id!r}  "
                  f"(only the {len(BATCH2)} newly-landed rows)")
            ok = ok and r2.rows_affected == len(BATCH2)

            r2b = apply("run-2")
            print(f"run 2 replay        → embedded={r2b.rows_affected}  already_applied={r2b.already_applied}  (C1)")
            ok = ok and r2b.rows_affected == 0 and r2b.already_applied

            total = query("SELECT count(*) AS n FROM lake.reviews").to_pylist()[0]["n"]
            print(f"\ntarget now holds {total} rows (12 + 3 incremental)")
            ok = ok and total == len(BATCH1) + len(BATCH2)

            print("\n🔍 semantic search over the strategy-built table:")
            for q, expect in {"battery life trip": {1, 2, 10, 15}, "fingerprint sensor": {13}}.items():
                rows = query(
                    f"SELECT id, text, array_cosine_distance(embedding::FLOAT[{DIM}], "
                    f"embed({_q(q)},{_q(MODEL)})::FLOAT[{DIM}]) AS dist "
                    f"FROM lake.reviews ORDER BY dist LIMIT 3").to_pylist()
                hit = bool({r["id"] for r in rows} & expect)
                ok = ok and hit
                print(f"\n  q={q!r}  {'✅' if hit else '❌'}")
                for r in rows:
                    print(f"     #{r['id']} dist={r['dist']:.3f}  {r['text']}")

            print("\n" + ("✅ incremental-embed strategy PASS — incremental load + embed-only-new + "
                          "idempotent replay (C1), composed purely by capability"
                          if ok else "❌ incremental-embed strategy FAILED"))
        finally:
            catalog.close()
            engine.con.close()
            for s in (strat_srv, eng_srv, cat_srv):
                s.stop(None)
        sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
