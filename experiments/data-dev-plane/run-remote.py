"""run-remote.py — the data-dev plane, end to end, REMOTE (S3 + Postgres).

Build-order step 3 (experiment README §11): the SAME pipeline as run-local.py, but now
distributed — three plugins over real gRPC, data on **S3/MinIO**, DuckLake metadata on
**Postgres**, and the engine's S3 creds **vended by the storage plugin** (short-TTL,
tenant+prefix-scoped). The whole point: the data plane is *unchanged* when storage goes
remote — only configuration moves. That is the "swap a plugin, the rest holds" thesis.

Flow (every control hop a real rat://{storage,engine,catalog}/v1 capability over gRPC):

  storage.VendWriteCredentials(tenant=acme, prefix=lake)  →  short-TTL STS creds
        → engine CREATE SECRET S3 + ATTACH ducklake:postgres (DATA_PATH s3://…)
  create → register → transform → embed() → flush(Parquet→S3) → snapshot → commit
        → 🔍 semantic search → idempotent replay
  + D3 showcase: read creds scoped to acme cannot touch another tenant's prefix.

Bytes (Parquet) move engine↔S3 directly; the catalog touches only Postgres METADATA —
it never sees bytes. Resolves findings F3 (Postgres metadata = real multi-writer) and
F4 (explicit flush forces inlined data out to Parquet on S3).

Run it via `make data-dev-remote` (boots MinIO + Postgres, then this). Requires env:
  MINIO_ENDPOINT MINIO_ROOT_USER MINIO_ROOT_PASSWORD RAT_S3_BUCKET PGHOST
"""

import importlib
import json
import os
import sys
import time
from concurrent import futures

import boto3
import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.common.v1 import context_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc
from rat.storage.v1 import storage_pb2, storage_pb2_grpc

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

_eng = _load_plugin("examples/engine/duckdb-ml-py", ["embed", "store", "streams", "server"])
eng_store, eng_server = _eng["store"], _eng["server"]
_cat = _load_plugin("examples/catalog/ducklake-py", ["store", "server"])
cat_store, cat_server = _cat["store"], _cat["server"]
_sto = _load_plugin("examples/storage/minio-s3", ["creds", "server"])
sto_creds, sto_server = _sto["creds"], _sto["server"]

# config
ENDPOINT = os.environ["MINIO_ENDPOINT"]
ADMIN_KEY = os.environ["MINIO_ROOT_USER"]
ADMIN_SECRET = os.environ["MINIO_ROOT_PASSWORD"]
BUCKET = os.environ.get("RAT_S3_BUCKET", "rat")
PGHOST = os.environ.get("PGHOST", "rat-pg")
TENANT, PREFIX = "acme", "lake"
PG_META = f"postgres:host={PGHOST} port=5432 dbname=ducklake user=ducklake password=ducklake"
S3_DATA = f"s3://{BUCKET}/{TENANT}/{PREFIX}/"
MODEL, DIM = "hash-256", 256

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


def _serve(add_fn, servicer):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    add_fn(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    return server, grpc.insecure_channel(f"127.0.0.1:{port}")


def _callmeta(tenant):
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(
            traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
            correlation_id="remote-run-1"),
        identity=context_pb2.Identity(tenant=tenant))
    return [("rat-callmeta-bin", rc.SerializeToString())]


def _q(s):
    return "'" + s.replace("'", "''") + "'"


def _secret_sql(s3):
    return (f"CREATE OR REPLACE SECRET s3 (TYPE S3, KEY_ID {_q(s3['key_id'])}, "
            f"SECRET {_q(s3['secret'])}, SESSION_TOKEN {_q(s3['session_token'])}, "
            f"ENDPOINT {_q(s3['endpoint'])}, URL_STYLE 'path', "
            f"USE_SSL {'true' if s3['use_ssl'] else 'false'}, REGION {_q(s3['region'])})")


def main():
    s3admin = boto3.client("s3", endpoint_url=f"http://{ENDPOINT}",
                           aws_access_key_id=ADMIN_KEY, aws_secret_access_key=ADMIN_SECRET,
                           region_name="us-east-1")
    try:
        s3admin.create_bucket(Bucket=BUCKET)
    except Exception:
        pass

    print("🛰️  data-dev plane — REMOTE end-to-end (S3/MinIO + Postgres, over gRPC)\n")

    # --- boot the storage plugin; vend WRITE creds for the engine -------------------
    sto_srv, sto_ch = _serve(storage_pb2_grpc.add_StorageServiceServicer_to_server,
                             sto_server.StorageServicer(sto_creds.MinioSTSMinter(
                                 endpoint=ENDPOINT, bucket=BUCKET,
                                 admin_key=ADMIN_KEY, admin_secret=ADMIN_SECRET)))
    storage_stub = storage_pb2_grpc.StorageServiceStub(sto_ch)
    wblob = json.loads(storage_stub.VendWriteCredentials(
        storage_pb2.VendWriteCredentialsRequest(prefix=PREFIX),
        metadata=_callmeta(TENANT)).credentials.decode("utf-8"))
    print(f"0. storage.VendWrite→ STS creds for {TENANT}/{PREFIX} "
          f"(key {wblob['s3']['key_id'][:8]}…, mode {wblob['mode']})")

    # --- boot the engine (S3 secret from the vended creds + Postgres/S3 lake) -------
    engine = eng_store.Engine(eng_store.DuckLakeConfig(PG_META, S3_DATA),
                              secret_sql=_secret_sql(wblob["s3"]))
    eng_obj = eng_server.EngineServicer(engine)
    eng_srv, eng_ch = _serve(engine_pb2_grpc.add_EngineServiceServicer_to_server, eng_obj)
    engine_stub = engine_pb2_grpc.EngineServiceStub(eng_ch)

    # --- boot the catalog (Postgres metadata; reads metadata only, never bytes) -----
    catalog = cat_store.Catalog(f"/tmp/rat-remote-tracking-{int(time.time())}.db",
                                PG_META, S3_DATA,
                                extensions=("httpfs", "postgres", "ducklake"))
    cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server,
                             cat_server.CatalogServicer(catalog))
    catalog_stub = catalog_pb2_grpc.CatalogServiceStub(cat_ch)

    def execute(sql):
        return engine_stub.Execute(engine_pb2.ExecuteRequest(sql=sql)).result

    def query(sql):
        return eng_obj.streams.pull(engine_stub.Query(engine_pb2.QueryRequest(sql=sql)).stream)

    ok = True
    try:
        # fresh table for a clean run
        execute("DROP TABLE IF EXISTS lake.reviews")
        execute("CREATE TABLE lake.reviews(id INTEGER, text VARCHAR, rating INTEGER, embedding FLOAT[])")
        print("1. engine.Execute  → CREATE lake.reviews (metadata→Postgres, data→S3)")
        catalog_stub.RegisterTable(catalog_pb2.RegisterTableRequest(identifier="reviews"),
                                   metadata=_callmeta(TENANT))
        print("2. catalog.Register→ reviews tracked")
        vals = ",".join(f"({i},{_q(t)},{r},NULL)" for i, t, r in REVIEWS)
        n = execute(f"INSERT INTO lake.reviews(id,text,rating,embedding) VALUES {vals}").rows_affected
        print(f"3. engine.Execute  → loaded {n} reviews")
        execute(f"UPDATE lake.reviews SET embedding = embed(text, {_q(MODEL)}) WHERE embedding IS NULL")
        print(f"4. engine.Execute  → embed({MODEL}) on new rows")
        snapshot = execute("CALL ducklake_flush_inlined_data('lake')").snapshot_id
        print(f"5. engine.Execute  → flush_inlined_data → Parquet on S3; snapshot {snapshot!r}")

        # prove the bytes really landed on S3 (the engine↔S3 leg, core never saw them)
        objs = s3admin.list_objects_v2(Bucket=BUCKET, Prefix=f"{TENANT}/{PREFIX}/").get("Contents", [])
        pq = [o["Key"] for o in objs if o["Key"].endswith(".parquet")]
        assert pq, "no Parquet landed on S3"
        print(f"   S3 now holds   → {len(pq)} Parquet file(s) under s3://{BUCKET}/{TENANT}/{PREFIX}/")

        commit = catalog_stub.CommitTable(catalog_pb2.CommitTableRequest(
            identifier="reviews", snapshot_id=snapshot, idempotency_key="remote-run-1"),
            metadata=_callmeta(TENANT))
        tref = catalog_stub.GetTable(catalog_pb2.GetTableRequest(identifier="reviews"),
                                     metadata=_callmeta(TENANT)).table
        assert snapshot in tref.uri, f"catalog uri {tref.uri!r} lacks snapshot {snapshot}"
        print(f"6. catalog.Commit  → {commit.snapshot_id!r}; GetTable resolves {tref.uri} (from Postgres)")

        print("\n🔍 semantic search (brute-force cosine over the S3-backed lake):")
        checks = {"battery life": {1, 2, 10}, "screen display quality": {3, 4, 11},
                  "photo camera quality": {5, 6}}
        for q, expect in checks.items():
            rows = query(
                f"SELECT id, rating, text, "
                f"array_cosine_distance(embedding::FLOAT[{DIM}], embed({_q(q)},{_q(MODEL)})::FLOAT[{DIM}]) AS dist "
                f"FROM lake.reviews ORDER BY dist LIMIT 3").to_pylist()
            hit = bool({r["id"] for r in rows} & expect)
            ok = ok and hit
            print(f"\n  q={q!r}  {'✅' if hit else '❌'}")
            for r in rows:
                print(f"     #{r['id']} dist={r['dist']:.3f} ★{r['rating']}  {r['text']}")

        # idempotent replay (C1)
        replay = catalog_stub.CommitTable(catalog_pb2.CommitTableRequest(
            identifier="reviews", snapshot_id=snapshot, idempotency_key="remote-run-1"),
            metadata=_callmeta(TENANT))
        assert replay.already_applied, "commit replay must be already_applied (C1)"
        print("\n7. catalog.Commit replay (same key) → already_applied=True ✅")

        # --- D3 showcase: read creds scoped to acme cannot cross the tenant boundary ---
        rblob = json.loads(storage_stub.VendReadCredentials(
            storage_pb2.VendReadCredentialsRequest(prefix=PREFIX),
            metadata=_callmeta(TENANT)).credentials.decode("utf-8"))
        s3admin.put_object(Bucket=BUCKET, Key="globex/lake/secret.txt", Body=b"other tenant")
        scoped = boto3.client("s3", endpoint_url=f"http://{ENDPOINT}",
                              aws_access_key_id=rblob["s3"]["key_id"],
                              aws_secret_access_key=rblob["s3"]["secret"],
                              aws_session_token=rblob["s3"]["session_token"], region_name="us-east-1")
        denied = False
        try:
            scoped.get_object(Bucket=BUCKET, Key="globex/lake/secret.txt")
        except Exception:
            denied = True
        ok = ok and denied
        print(f"8. D3 isolation    → {TENANT} read creds DENIED globex/lake/secret.txt: "
              f"{'✅' if denied else '❌ LEAK'}")

        print("\n" + ("✅ data-dev plane REMOTE end-to-end PASS — S3 data + Postgres metadata + "
                      "vended creds; data plane unchanged from local" if ok
                      else "❌ data-dev plane REMOTE end-to-end FAILED"))
    finally:
        catalog.close()
        engine.con.close()
        for s in (eng_srv, cat_srv, sto_srv):
            s.stop(None)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
