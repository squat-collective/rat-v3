"""Cross-axis COMPOSITION harness — the ADR-003 'run against each other on golden
data' gate (reviews/07 Part C).

For each of the four ADR-003 cross-combinations in contracts/conformance/
composition-v1.json (baseline + format/catalog/engine one-axis substitutions, storage
held at A), this:

  1. boots the chosen format + catalog + engine as REAL gRPC servers, each reusing the
     per-axis reference's REAL backend store (parquet/delta, sqlite/in-memory,
     duckdb/datafusion) — loaded under a unique module name so the same-named store.py
     files don't collide;
  2. wires them into a mediating Gateway purely by capability annotation (no plugin
     names);
  3. seeds the golden source via the format;
  4. runs the REAL strategy reference (examples/strategy/fullrefresh-py) over the
     gateway: catalog.get-table -> engine.query (which resolves its source via
     format.scan and streams the transform result over real Arrow Flight) ->
     format.overwrite;
  5. reads the target back and asserts it equals the single expected_target.

Every combination producing that identical target — across genuinely different engine
/format/catalog technologies, with the strategy code unchanged — is what proves the
data-plane contracts COMPOSE, not merely that each axis conforms alone. Exit 0 iff all
four combinations pass.
"""

import importlib.util
import json
import os
import sys
from concurrent import futures

import grpc
import pyarrow as pa

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc
from rat.format.v1 import format_pb2, format_pb2_grpc

from comp_engine import CompositionEngineServicer
from flight import FlightHost, flight_pull
from gateway import Gateway

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
VECTORS = os.path.join(ROOT, "contracts", "conformance", "composition-v1.json")

# Where each impl's backend store.py lives (loaded under a unique module name).
BACKENDS = {
    "format": {
        "parquet-py": ("examples/format/parquet-py/store.py", "Store"),
        "delta-py": ("examples/format/delta-py/store.py", "Store"),
    },
    "catalog": {
        "sqlite-py": ("examples/catalog/sqlite-py/store.py", "Catalog"),
        "inmemory-py": ("examples/catalog/inmemory-py/store.py", "Catalog"),
    },
    "engine": {
        "duckdb-py": ("examples/engine/duckdb-py/store.py", "Engine"),
        "datafusion-py": ("examples/engine/datafusion-py/store.py", "Engine"),
    },
}


def _load(path, modname):
    spec = importlib.util.spec_from_file_location(modname, os.path.join(ROOT, path))
    mod = importlib.util.module_from_spec(spec)
    sys.modules[modname] = mod
    spec.loader.exec_module(mod)
    return mod


def _service_desc(pb2_module, name):
    return pb2_module.DESCRIPTOR.services_by_name[name]


def _serve(add_servicer_fn, servicer):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    add_servicer_fn(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    return server, grpc.insecure_channel(f"127.0.0.1:{port}")


# ---- composition servicers (thin: reuse the real backend stores) ----------------

class CompFormatServicer(format_pb2_grpc.FormatServiceServicer):
    def __init__(self, store):
        self.store = store
        self.flight = FlightHost()

    def close(self):
        self.flight.stop()

    def Resolve(self, request, context):
        rows = self.store.scan(request.table.identifier)
        table = pa.Table.from_pylist(rows) if rows else pa.table({})
        return format_pb2.ResolveResponse(stream=self.flight.put(table))

    def Append(self, request, context):
        t = flight_pull(request.source)
        n = self.store.append(request.table.identifier, t.to_pylist() if t else [])
        return format_pb2.AppendResponse(result=data_pb2.WriteResult(rows_affected=n))

    def Overwrite(self, request, context):
        t = flight_pull(request.source)
        n = self.store.overwrite(request.table.identifier, t.to_pylist() if t else [])
        return format_pb2.OverwriteResponse(result=data_pb2.WriteResult(rows_affected=n))


class CompCatalogServicer(catalog_pb2_grpc.CatalogServiceServicer):
    def __init__(self, catalog):
        self.cat = catalog

    def GetTable(self, request, context):
        from grpc import StatusCode
        try:
            branch, uri = self.cat.get_table(request.identifier, request.branch)
        except Exception as e:
            context.abort(getattr(e, "code", StatusCode.NOT_FOUND), str(e))
        return catalog_pb2.GetTableResponse(
            table=data_pb2.TableRef(identifier=request.identifier, uri=uri, branch=branch)
        )


# ---- per-impl construction helpers ----------------------------------------------

def build_catalog(impl, tmp, table_ids):
    Catalog = getattr(_load(BACKENDS["catalog"][impl][0], f"catalog_{impl.replace('-', '_')}"),
                      "Catalog")
    if impl == "sqlite-py":
        cat = Catalog(os.path.join(tmp, "catalog.db"))
        for tid in table_ids:
            cat._conn().execute("INSERT OR IGNORE INTO tables(identifier) VALUES (?)", (tid,))
    else:  # inmemory-py
        cat = Catalog()
        for tid in table_ids:
            cat._tables.add(tid)
    return cat


def build_format(impl, tmp):
    Store = getattr(_load(BACKENDS["format"][impl][0], f"format_{impl.replace('-', '_')}"),
                    "Store")
    return Store(os.path.join(tmp, "data"))


def build_engine(impl, tmp):
    Engine = getattr(_load(BACKENDS["engine"][impl][0], f"engine_{impl.replace('-', '_')}"),
                     "Engine")
    backend = Engine()
    if impl == "duckdb-py":
        bind = lambda name, table: backend.con.register(name, table)
    else:  # datafusion-py
        bind = lambda name, table: backend.ctx.register_record_batches(name, [table.to_batches()])
    return backend, bind


# ---- one combination -------------------------------------------------------------

def run_combo(combo, v, tmp):
    src_id, tgt_id = v["source_table"], v["target_table"]
    sql = v["transform_sql"]

    fmt = CompFormatServicer(build_format(combo["format"], tmp))
    cat = CompCatalogServicer(build_catalog(combo["catalog"], tmp, [src_id, tgt_id]))
    fmt_srv, fmt_ch = _serve(format_pb2_grpc.add_FormatServiceServicer_to_server, fmt)
    cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server, cat)

    gw = Gateway()
    gw.register(format_pb2_grpc.FormatServiceStub(fmt_ch), _service_desc(format_pb2, "FormatService"))
    gw.register(catalog_pb2_grpc.CatalogServiceStub(cat_ch), _service_desc(catalog_pb2, "CatalogService"))

    backend, bind = build_engine(combo["engine"], tmp)
    eng = CompositionEngineServicer(backend, bind, gw.invoker_for(["rat://format/v1/scan"]))
    eng_srv, eng_ch = _serve(engine_pb2_grpc.add_EngineServiceServicer_to_server, eng)
    gw.register(engine_pb2_grpc.EngineServiceStub(eng_ch), _service_desc(engine_pb2, "EngineService"))

    # --- seed the golden source via the format (host rows on Flight, Overwrite) ---
    seed_host = FlightHost()
    try:
        seed_stream = seed_host.put(pa.Table.from_pylist(v["source_rows"]))
        fmt_ch_stub = format_pb2_grpc.FormatServiceStub(fmt_ch)
        fmt_ch_stub.Overwrite(format_pb2.OverwriteRequest(
            table=data_pb2.TableRef(identifier=src_id), source=seed_stream))

        # --- run the REAL strategy reference over the gateway ---
        from strategy_store import FullRefreshStrategy
        strat = FullRefreshStrategy(gw.invoker_for([
            "rat://catalog/v1/get-table", "rat://engine/v1/query", "rat://format/v1/overwrite"]))
        result = strat.apply(src_id, tgt_id, json.dumps({"sql": sql}).encode())

        # --- read the target back via the format ---
        resp = fmt_ch_stub.Resolve(format_pb2.ResolveRequest(
            table=data_pb2.TableRef(identifier=tgt_id)))
        out = flight_pull(resp.stream)
        got = sorted((out.to_pylist() if out else []), key=lambda r: r.get("region", ""))
        return got, int(result.rows_affected)
    finally:
        seed_host.stop()
        eng.close()
        fmt.close()
        for s in (eng_srv, fmt_srv, cat_srv):
            s.stop(None)


def main():
    with open(VECTORS, encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "composition/v1"
    expected = sorted(v["expected_target"], key=lambda r: r["region"])

    # Make the strategy reference's backend importable (its store.py, under a name).
    _load("examples/strategy/fullrefresh-py/store.py", "strategy_store")

    import tempfile
    rows = []
    ok = True
    for combo in v["combinations"]:
        with tempfile.TemporaryDirectory() as tmp:
            try:
                got, n = run_combo(combo, v, tmp)
                match = got == expected
                ok = ok and match
                detail = "OK" if match else f"MISMATCH got={got}"
            except Exception as e:  # noqa
                ok = False
                got, n, detail = None, 0, f"ERROR {type(e).__name__}: {e}"
        rows.append((combo["name"], combo["engine"], combo["format"], combo["catalog"], detail))

    w = [max(len(r[i]) for r in [("combo","engine","format","catalog","result")] + rows) for i in range(5)]
    line = "  " + "  ".join(h.ljust(w[i]) for i, h in enumerate(("combo","engine","format","catalog","result")))
    print(line)
    print("  " + "  ".join("-" * w[i] for i in range(5)))
    for r in rows:
        print("  " + "  ".join(str(r[i]).ljust(w[i]) for i in range(5)))
    print()
    if ok:
        print(f">> COMPOSITION CONFORMANT ✅ — all {len(rows)} ADR-003 cross-combinations produced the identical target")
        sys.exit(0)
    print(">> COMPOSITION FAILED ❌")
    sys.exit(1)


if __name__ == "__main__":
    main()
