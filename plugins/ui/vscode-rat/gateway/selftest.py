"""Self-test for the data-dev gateway — boots it on an ephemeral port and exercises
every endpoint over real HTTP (the same surface the VS Code extension uses).

Run (containerized):
  podman run --rm -v "$PWD":/work:Z -e PYTHONPATH=/work/contracts/sdks/python \
    -w /work/plugins/ui/vscode-rat/gateway python:3.12 \
    bash -c 'pip install -q grpcio==1.80.0 protobuf==7.35.0 duckdb==1.5.3 pyarrow==24.0.0 numpy==2.2.6 \
             && python selftest.py'
"""

import json
import tempfile
import threading
import urllib.request
from http.server import ThreadingHTTPServer

import app as gw


def _get(base, path):
    with urllib.request.urlopen(base + path, timeout=30) as r:
        return json.loads(r.read())


def _post(base, path, body):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read())


def main():
    tmp = tempfile.mkdtemp(prefix="rat-gw-selftest-")
    stack = gw.Stack(tmp)
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), gw.make_handler(stack))
    port = httpd.server_address[1]
    base = f"http://127.0.0.1:{port}"
    t = threading.Thread(target=httpd.serve_forever, daemon=True)
    t.start()
    try:
        health = _get(base, "/api/health")
        assert any(p["kind"] == "engine" and p["status"] == "Healthy" for p in health["plugins"]), health
        print("health   :", [f"{p['kind']}={p['status']}" for p in health["plugins"]])

        tables = _get(base, "/api/tables")["tables"]
        names = {t["identifier"] for t in tables}
        assert {"reviews_raw", "reviews"} <= names, names
        reviews = next(t for t in tables if t["identifier"] == "reviews")
        assert reviews["rows"] == 12 and reviews["snapshot"].startswith("snap-"), reviews
        print("tables   :", [(t["identifier"], t["rows"], t["snapshot"]) for t in tables])

        snaps = _get(base, "/api/snapshots?table=reviews")["snapshots"]
        assert snaps and all(s.startswith("snap-") for s in snaps), snaps
        print("snapshots:", snaps)

        q = _post(base, "/api/query", {"sql": "SELECT id, rating, text FROM lake.reviews ORDER BY id", "limit": 3})
        assert q["columns"] == ["id", "rating", "text"] and len(q["rows"]) == 3, q
        print("query    :", q["columns"], "->", len(q["rows"]), "rows")

        s = _post(base, "/api/search", {"query": "battery life", "k": 3})
        top = {r["id"] for r in s["results"]}
        assert top & {1, 2, 10}, s
        print("search   : 'battery life' ->", [(r["id"], round(r["dist"], 3)) for r in s["results"]])

        # pipeline run lands the next batch -> incremental embed
        before = next(t for t in _get(base, "/api/tables")["tables"] if t["identifier"] == "reviews")["rows"]
        run = _post(base, "/api/pipeline/run", {})
        assert run["embedded"] == 3 and run["total"] == before + 3, run
        print("pipeline : embedded", run["embedded"], "->", run["total"], "rows (incremental)")

        print("PASS — data-dev gateway: health, tables, snapshots, query, search, pipeline/run")
    finally:
        httpd.shutdown()
        stack.close()


if __name__ == "__main__":
    main()
