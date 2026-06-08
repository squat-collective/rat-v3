"""run-local.py — the data-dev plane, end to end, LOCAL (no S3 yet).

Build-order step 2 (experiment README §11): boot the DuckLake catalog + the DuckDB-ML
engine together over real gRPC, both attached to the SAME local DuckLake, and run the
§6 composition on a small real corpus:

    create → register → transform/merge → embed() → snapshot/commit → 🔍 semantic search

Every control hop is a real `rat://{engine,catalog}/v1` capability call over gRPC (the
shape the sealed core mediates). Bulk Arrow results come back over the in-proc Flight
stand-in (the engine reference's StreamRegistry) — the same handoff every engine ref
uses. This is the "get a local end-to-end working first" milestone, before going remote
(MinIO) and adding the strategy + VS Code UI.

It is also an assertion-bearing test: it exits non-zero if the pipeline or the search
ranking is wrong, so `make data-dev-local` is a real gate.

Run (containerized — no host installs):
  podman run --rm -v "$PWD":/work:Z -e PYTHONPATH=/work/contracts/sdks/python \
    -w /work python:3.12 bash -c \
    'pip install -q grpcio==1.80.0 protobuf==7.35.0 duckdb==1.5.3 pyarrow==24.0.0 numpy==2.2.6 \
     && python experiments/data-dev-plane/run-local.py'
"""

import importlib
import os
import sys
import tempfile
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.engine.v1 import engine_pb2, engine_pb2_grpc

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))


def _load_plugin(reldir, bare_modules):
    """Import a reference plugin's real modules (server.py et al.) exactly as the plugin
    itself runs them — by their BARE sibling names (`from store import ...`). To do that
    without the same-named modules across plugins colliding, we prepend the plugin's dir
    to sys.path, import fresh, then pop the bare names so the next plugin re-resolves its
    own. Returns the loaded modules keyed by name."""
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

# Load the engine (embed → store → streams → server) and catalog (store → server) refs.
_eng = _load_plugin("plugins/engine/duckdb-ml-py", ["embed", "store", "streams", "server"])
eng_store, eng_server = _eng["store"], _eng["server"]
_cat = _load_plugin("plugins/catalog/ducklake-py", ["store", "server"])
cat_store, cat_server = _cat["store"], _cat["server"]


# A small REAL corpus (swap for any CSV/Parquet — the pipeline is column-driven:
# id/text/rating, README §9). Themes: battery, screen, camera, price, build.
REVIEWS = [
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
MODEL = "hash-256"  # deterministic default backend (golden-stable, zero-dep)
DIM = 256


def _serve(add_fn, servicer):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    add_fn(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    return server, grpc.insecure_channel(f"127.0.0.1:{port}")


def _q(s):  # SQL string literal
    return "'" + s.replace("'", "''") + "'"


def main():
    with tempfile.TemporaryDirectory() as tmp:
        meta = f"sqlite:{os.path.join(tmp, 'lakemeta.db')}"
        data = os.path.join(tmp, "lakedata") + "/"
        tracking = os.path.join(tmp, "catalog-tracking.db")

        # --- boot the two plugins as real gRPC servers, sharing ONE DuckLake ---------
        engine = eng_store.Engine(eng_store.DuckLakeConfig(meta, data))
        if "ducklake" not in engine.loaded or "vss" not in engine.loaded:
            print("ERROR: required DuckDB extensions did not load:", sorted(engine.loaded))
            sys.exit(2)
        eng_srv_obj = eng_server.EngineServicer(engine)
        eng_srv, eng_ch = _serve(engine_pb2_grpc.add_EngineServiceServicer_to_server, eng_srv_obj)
        engine_stub = engine_pb2_grpc.EngineServiceStub(eng_ch)

        catalog = cat_store.Catalog(tracking, meta, data)
        cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server,
                                 cat_server.CatalogServicer(catalog))
        catalog_stub = catalog_pb2_grpc.CatalogServiceStub(cat_ch)

        def execute(sql):
            return engine_stub.Execute(engine_pb2.ExecuteRequest(sql=sql)).result

        def query(sql):
            resp = engine_stub.Query(engine_pb2.QueryRequest(sql=sql))
            return eng_srv_obj.streams.pull(resp.stream)  # pyarrow.Table over the Flight leg

        ok = True
        try:
            print("🛰️  data-dev plane — local end-to-end (DuckLake + DuckDB-ML, all over gRPC)\n")

            # 1. CREATE the lake table via the ENGINE (DDL). embedding stored as FLOAT[]
            #    (variable list) — DuckLake rejects fixed FLOAT[N] (finding, README §10).
            execute("CREATE TABLE lake.reviews(id INTEGER, text VARCHAR, rating INTEGER, "
                    "embedding FLOAT[], _ingested_at TIMESTAMP DEFAULT now())")
            print("1. engine.Execute  → CREATE lake.reviews (embedding FLOAT[])")

            # 2. REGISTER the table on the RAT catalog axis (idempotent).
            catalog_stub.RegisterTable(catalog_pb2.RegisterTableRequest(identifier="reviews"))
            print("2. catalog.Register→ reviews tracked on the RAT axis")

            # 3. TRANSFORM/load the corpus (engine SQL).
            values = ",".join(f"({i},{_q(t)},{r},NULL,now())" for i, t, r in REVIEWS)
            n = execute(f"INSERT INTO lake.reviews(id,text,rating,embedding,_ingested_at) "
                        f"VALUES {values}").rows_affected
            print(f"3. engine.Execute  → loaded {n} reviews")

            # 4. EMBED only rows that need it (ML, the embed() UDF) — and snapshot.
            res = execute(f"UPDATE lake.reviews SET embedding = embed(text, {_q(MODEL)}) "
                          f"WHERE embedding IS NULL")
            snapshot = res.snapshot_id
            print(f"4. engine.Execute  → embed({MODEL}) on new rows; DuckLake snapshot = {snapshot!r}")

            # 5. COMMIT the snapshot to the catalog (the real commit-linkage, idempotent).
            commit = catalog_stub.CommitTable(catalog_pb2.CommitTableRequest(
                identifier="reviews", snapshot_id=snapshot, idempotency_key="local-run-1"))
            print(f"5. catalog.Commit  → recorded {commit.snapshot_id!r} (already_applied={commit.already_applied})")

            # 5b. GetTable resolves the SAME real snapshot the engine produced.
            tref = catalog_stub.GetTable(catalog_pb2.GetTableRequest(identifier="reviews")).table
            assert snapshot in tref.uri, f"catalog GetTable uri {tref.uri!r} lacks engine snapshot {snapshot}"
            print(f"   catalog.GetTable→ {tref.uri}")

            # 6. 🔍 SEMANTIC SEARCH — embed the query, rank by cosine distance (brute force
            #    over the lake; HNSW would need a derived fixed-array table, README §10).
            print("\n🔍 semantic search (brute-force cosine over the lake):")
            checks = {
                "battery life": {1, 2, 10},     # battery-themed reviews
                "screen display quality": {3, 4, 11},
                "photo camera quality": {5, 6},
            }
            for q, expected_top3 in checks.items():
                tbl = query(
                    f"SELECT id, rating, text, "
                    f"array_cosine_distance(embedding::FLOAT[{DIM}], embed({_q(q)},{_q(MODEL)})::FLOAT[{DIM}]) AS dist "
                    f"FROM lake.reviews ORDER BY dist LIMIT 3")
                rows = tbl.to_pylist()
                top_ids = {r["id"] for r in rows}
                hit = len(top_ids & expected_top3) >= 1
                ok = ok and hit
                print(f"\n  q={q!r}  {'✅' if hit else '❌ (expected overlap with '+str(expected_top3)+')'}")
                for r in rows:
                    print(f"     #{r['id']} dist={r['dist']:.3f} ★{r['rating']}  {r['text']}")

            # 7. idempotency: a re-run of the embed+commit with the same key is a no-op.
            commit2 = catalog_stub.CommitTable(catalog_pb2.CommitTableRequest(
                identifier="reviews", snapshot_id=snapshot, idempotency_key="local-run-1"))
            assert commit2.already_applied, "commit replay must be already_applied (C1)"
            print(f"\n7. catalog.Commit replay (same key) → already_applied={commit2.already_applied} ✅")

            print("\n" + ("✅ data-dev plane local end-to-end PASS — create→register→transform→embed→commit→search"
                          if ok else "❌ data-dev plane local end-to-end FAILED — search ranking off"))
        finally:
            catalog.close()
            eng_srv_obj.engine.con.close()
            for s in (eng_srv, cat_srv):
                s.stop(None)

        sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
