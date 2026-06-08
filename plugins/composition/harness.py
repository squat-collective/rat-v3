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
  4. runs the REAL strategy reference (plugins/strategy/fullrefresh-py) over the
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
        "parquet-py": ("plugins/format/parquet-py/store.py", "Store"),
        "delta-py": ("plugins/format/delta-py/store.py", "Store"),
    },
    "catalog": {
        "sqlite-py": ("plugins/catalog/sqlite-py/store.py", "Catalog"),
        "inmemory-py": ("plugins/catalog/inmemory-py/store.py", "Catalog"),
    },
    "engine": {
        "duckdb-py": ("plugins/engine/duckdb-py/store.py", "Engine"),
        "datafusion-py": ("plugins/engine/datafusion-py/store.py", "Engine"),
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
        self._versions = {}    # identifier -> write counter; mints a per-write snapshot id
        self._committed = {}   # idempotency_key -> WriteResult (C1 effect-leg dedup ledger)

    def close(self):
        self.flight.stop()

    def _snapshot(self, identifier):
        # A real versioned format returns the snapshot a write produced in
        # WriteResult.snapshot_id; the strategy carries that value to
        # catalog.CommitTable (the commit-linkage, ADR-010). Mint a deterministic one.
        self._versions[identifier] = self._versions.get(identifier, 0) + 1
        return f"{identifier}@v{self._versions[identifier]}"

    def _idem(self, key):
        # C1 (ADR-012): a write with a key that already committed is a no-op returning
        # the ORIGINAL result with already_applied=true — never a second write.
        if key and key in self._committed:
            r = data_pb2.WriteResult()
            r.CopyFrom(self._committed[key])
            r.already_applied = True
            return r
        return None

    def _commit(self, key, result):
        if key:
            self._committed[key] = result
        return result

    def Resolve(self, request, context):
        rows = self.store.scan(request.table.identifier)
        table = pa.Table.from_pylist(rows) if rows else pa.table({})
        return format_pb2.ResolveResponse(stream=self.flight.put(table))

    def Append(self, request, context):
        dup = self._idem(request.idempotency_key)
        if dup is not None:
            return format_pb2.AppendResponse(result=dup)
        t = flight_pull(request.source)
        n = self.store.append(request.table.identifier, t.to_pylist() if t else [])
        return format_pb2.AppendResponse(result=self._commit(request.idempotency_key,
            data_pb2.WriteResult(rows_affected=n, snapshot_id=self._snapshot(request.table.identifier))))

    def Overwrite(self, request, context):
        dup = self._idem(request.idempotency_key)
        if dup is not None:
            return format_pb2.OverwriteResponse(result=dup)
        t = flight_pull(request.source)
        n = self.store.overwrite(request.table.identifier, t.to_pylist() if t else [])
        return format_pb2.OverwriteResponse(result=self._commit(request.idempotency_key,
            data_pb2.WriteResult(rows_affected=n, snapshot_id=self._snapshot(request.table.identifier))))

    def Merge(self, request, context):
        dup = self._idem(request.idempotency_key)
        if dup is not None:
            return format_pb2.MergeResponse(result=dup)
        t = flight_pull(request.source)
        n = self.store.merge(request.table.identifier, list(request.merge_keys),
                             t.to_pylist() if t else [])
        return format_pb2.MergeResponse(result=self._commit(request.idempotency_key,
            data_pb2.WriteResult(rows_affected=n, snapshot_id=self._snapshot(request.table.identifier))))


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

    def RegisterTable(self, request, context):
        from grpc import StatusCode
        try:
            branch, uri = self.cat.register_table(request.identifier, request.uri, request.branch)
        except Exception as e:
            context.abort(getattr(e, "code", StatusCode.INTERNAL), str(e))
        return catalog_pb2.RegisterTableResponse(
            table=data_pb2.TableRef(identifier=request.identifier, uri=uri, branch=branch)
        )

    def CommitTable(self, request, context):
        from grpc import StatusCode
        try:
            snap, already = self.cat.commit_table(
                request.identifier, request.branch, request.snapshot_id,
                request.expected_snapshot, request.idempotency_key)
        except Exception as e:
            context.abort(getattr(e, "code", StatusCode.INTERNAL), str(e))
        return catalog_pb2.CommitTableResponse(snapshot_id=snap, already_applied=already)


# ---- per-impl construction helpers ----------------------------------------------

def build_catalog(impl, tmp, admin_table_ids):
    """Construct the catalog backend and ADMIN-register the pre-existing (ingested)
    input tables via the catalog's PUBLIC api — no more private-store poking. The
    pipeline's OWN output table is NOT seeded here; the strategy registers it through
    the wire (catalog.RegisterTable) and records what it wrote (catalog.CommitTable),
    closing the create→write→register→merge loop on the frozen surface
    (ADR-010 / reviews/08 B1)."""
    Catalog = getattr(_load(BACKENDS["catalog"][impl][0], f"catalog_{impl.replace('-', '_')}"),
                      "Catalog")
    cat = Catalog(os.path.join(tmp, "catalog.db")) if impl == "sqlite-py" else Catalog()
    for tid in admin_table_ids:
        cat.register_table(tid, "", "")  # admin/ingestion registration of pre-existing inputs
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
        bind = lambda name, table: backend.con.register(name, table)  # register replaces
    else:  # datafusion-py — deregister-then-register so a re-bind (C1 retry) is idempotent
        def bind(name, table):
            try:
                backend.ctx.deregister_table(name)
            except Exception:  # noqa — not registered yet
                pass
            backend.ctx.register_record_batches(name, [table.to_batches()])
    return backend, bind


# ---- one combination -------------------------------------------------------------

def run_combo(combo, v, tmp):
    src_id, tgt_id = v["source_table"], v["target_table"]
    sql = v["transform_sql"]

    fmt = CompFormatServicer(build_format(combo["format"], tmp))
    cat = CompCatalogServicer(build_catalog(combo["catalog"], tmp, [src_id]))  # only the source is admin-registered
    fmt_srv, fmt_ch = _serve(format_pb2_grpc.add_FormatServiceServicer_to_server, fmt)
    cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server, cat)

    gw = Gateway()
    gw.register(format_pb2_grpc.FormatServiceStub(fmt_ch), _service_desc(format_pb2, "FormatService"))
    gw.register(catalog_pb2_grpc.CatalogServiceStub(cat_ch), _service_desc(catalog_pb2, "CatalogService"))

    backend, bind = build_engine(combo["engine"], tmp)  # bind is idempotent (C1 replay re-binds)
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

        # --- run the REAL strategy reference over the gateway. It REGISTERS its own
        #     output table + COMMITs the written snapshot through the wire (ADR-010) —
        #     the target was NOT pre-seeded, so the create→write→register→merge loop
        #     closes entirely on the frozen surface, no out-of-band poking. ---
        from strategy_store import FullRefreshStrategy, REQUIRES as FR_REQUIRES
        strat = FullRefreshStrategy(gw.invoker_for(list(FR_REQUIRES)))
        run_id = f"{combo['name']}-run-1"
        result = strat.apply(src_id, tgt_id, json.dumps({"sql": sql}).encode(), run_id=run_id)
        assert not result.already_applied, "first apply must not report already_applied"

        # --- C1 (ADR-012): a reconciler RETRY of the same run is a no-op. Re-apply with
        #     the SAME idempotency_key (run_id) and assert the write reports
        #     already_applied — the effect leg did not write twice. ---
        replay = strat.apply(src_id, tgt_id, json.dumps({"sql": sql}).encode(), run_id=run_id)
        assert replay.already_applied, "replay with the same run_id must be already_applied (C1)"

        # --- prove the loop closed ON THE WIRE: the catalog now resolves the target
        #     the strategy created (RegisterTable) — the harness never seeded it. ---
        cat_stub = catalog_pb2_grpc.CatalogServiceStub(cat_ch)
        tref = cat_stub.GetTable(catalog_pb2.GetTableRequest(identifier=tgt_id)).table
        assert tref.identifier == tgt_id and tref.branch == "main", \
            f"catalog did not learn the target on-wire: {tref}"

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


SCD2_VECTORS = os.path.join(ROOT, "contracts", "conformance", "strategy-scd2-v1.json")


def run_scd2(sv, tmp):
    """The SECOND strategy reference (scd2-py) over the real stack — proves strategy/v1
    serves a second, semantically-different strategy (ADR-003). Uses baseline backends
    (parquet format + sqlite catalog); runs two temporal loads and asserts the SCD2
    history."""
    scd2_mod = _load("plugins/strategy/scd2-py/store.py", "strategy_scd2_store")
    SCD2 = getattr(scd2_mod, "SCD2Strategy")
    src_id, tgt_id = sv["source_table"], sv["target_table"]

    fmt = CompFormatServicer(build_format("parquet-py", tmp))
    cat = CompCatalogServicer(build_catalog("sqlite-py", tmp, [src_id]))  # only the source is admin-registered
    fmt_srv, fmt_ch = _serve(format_pb2_grpc.add_FormatServiceServicer_to_server, fmt)
    cat_srv, cat_ch = _serve(catalog_pb2_grpc.add_CatalogServiceServicer_to_server, cat)
    fmt_stub = format_pb2_grpc.FormatServiceStub(fmt_ch)

    gw = Gateway()
    gw.register(fmt_stub, _service_desc(format_pb2, "FormatService"))
    gw.register(catalog_pb2_grpc.CatalogServiceStub(cat_ch), _service_desc(catalog_pb2, "CatalogService"))

    delta_host = FlightHost()  # the strategy hosts its synthesized version-delta here
    host_rows = lambda rows: delta_host.put(pa.Table.from_pylist(rows) if rows else pa.table({}))
    pull_rows = lambda s: (lambda t: t.to_pylist() if t is not None else [])(flight_pull(s))
    seed_host = FlightHost()
    try:
        scd2 = SCD2(gw.invoker_for(list(scd2_mod.REQUIRES)), host_rows, pull_rows)
        opts = {"natural_key": sv["natural_key"], "tracked": sv["tracked"]}
        for run in ("run1", "run2"):
            r = sv[run]
            fmt_stub.Overwrite(format_pb2.OverwriteRequest(
                table=data_pb2.TableRef(identifier=src_id), source=seed_host.put(pa.Table.from_pylist(r["source"]))))
            scd2.apply(src_id, tgt_id, json.dumps({**opts, "effective_from": r["effective_from"]}).encode())

        out = flight_pull(fmt_stub.Resolve(format_pb2.ResolveRequest(
            table=data_pb2.TableRef(identifier=tgt_id))).stream)
        got = out.to_pylist() if out is not None else []
        keyf = lambda r: (r.get("id"), r.get("effective_from"))
        return sorted(got, key=keyf), sorted(sv["expected_history"], key=keyf)
    finally:
        delta_host.stop(); seed_host.stop()
        for s in (fmt_srv, cat_srv):
            s.stop(None)


def check_c2_truncation():
    """C2 (ADR-012): a consumer MUST fail the write when an ArrowStream delivers fewer
    rows than the producer declared (a truncated / aborted transfer). Host 4 rows but
    DECLARE 9 — simulating a producer that died after 4 — and assert flight_pull raises
    instead of silently committing the partial dataset."""
    host = FlightHost()
    try:
        stream = host.put(pa.Table.from_pylist([{"id": i} for i in range(4)]), declare_rows=9)
        try:
            flight_pull(stream)
            return False, "consumer accepted a truncated stream (expected a failure)"
        except ValueError as e:
            return ("truncated" in str(e)), str(e)
    finally:
        host.stop()


def main():
    with open(VECTORS, encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "composition/v1"
    expected = sorted(v["expected_target"], key=lambda r: r["region"])

    # Make the strategy reference's backend importable (its store.py, under a name).
    _load("plugins/strategy/fullrefresh-py/store.py", "strategy_store")

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
        print(f">> cross-axis matrix ✅ — all {len(rows)} ADR-003 cross-combinations produced the identical target")
    else:
        print(">> cross-axis matrix ❌")

    # --- strategy axis: the SECOND reference (scd2-py) over the real stack ---
    with open(SCD2_VECTORS, encoding="utf-8") as f:
        sv = json.load(f)
    assert sv["axis"] == "strategy/scd2"
    print()
    try:
        with tempfile.TemporaryDirectory() as tmp:
            got, exp = run_scd2(sv, tmp)
        scd2_ok = got == exp
        print("  strategy reference 2 — scd2-py (parquet+sqlite, 2 temporal loads):",
              "OK ✅" if scd2_ok else "MISMATCH ❌")
        if not scd2_ok:
            print(f"    got={got}\n    exp={exp}")
    except Exception as e:  # noqa
        scd2_ok = False
        print(f"  strategy reference 2 — scd2-py: ERROR {type(e).__name__}: {e}")
    ok = ok and scd2_ok

    # --- C2 (ADR-012): truncated-stream detection ---
    c2_ok, c2_msg = check_c2_truncation()
    print()
    print("  C2 truncated-stream detection (declare 9 rows, deliver 4):",
          "OK ✅ — consumer failed the write" if c2_ok else f"FAILED ❌ — {c2_msg}")
    ok = ok and c2_ok

    print()
    if ok:
        print(">> COMPOSITION CONFORMANT ✅ — cross-axis matrix + both strategy references + "
              "C1 idempotent-replay + C2 truncation-detection pass")
        sys.exit(0)
    print(">> COMPOSITION FAILED ❌")
    sys.exit(1)


if __name__ == "__main__":
    main()
