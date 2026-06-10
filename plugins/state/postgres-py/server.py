"""A Postgres-backed StateService — the platform's state-backend (run/pipeline/schedule
metadata). EXPLORATORY (the data platform bundle, ADR-020 S4). Implements the frozen
state/v1 Get/Put/List with monotonic revisions + single-key compare-and-set (if_revision).

Keys follow the frozen KEY GRAMMAR (state.proto header): printable UTF-8, `/` as a
logical separator, no empty / control chars / `.`|`..` path segments. The gateway is the
namespace boundary; this backend stores plugin-relative keys verbatim.
"""

import os
import threading
import time

import grpc
import psycopg2

from rat.common.v1 import context_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.secret.v1 import secret_pb2
from rat.state.v1 import state_pb2, state_pb2_grpc


class PostgresState(state_pb2_grpc.StateServiceServicer):
    """Postgres-backed state-backend. The Postgres DSN is a SECRET: it is resolved from an
    opaque ref ($RAT_STATE_PG_REF, e.g. ref://state/pg-dsn) via the gateway's
    rat://secret/v1/resolve at first use — so this plugin's manifest/env carries no
    credential (ADR-022). $RAT_STATE_PG (a literal DSN) is still honored as a fallback for
    the no-secret-plugin path. Resolution is lazy so the plugin becomes Healthy immediately
    and doesn't race the secret plugin's wiring at boot."""

    def __init__(self) -> None:
        self._lock = threading.Lock()       # serialize CAS (a single-node linearizable shim)
        self._init_lock = threading.Lock()  # one-time lazy DSN-resolve + table create
        self._initialized = False
        self._dsn = None
        # DSN sources: a literal DSN (fallback) or a secret ref resolved via the gateway.
        self._direct_dsn = os.environ.get("RAT_STATE_PG")
        self._dsn_ref = os.environ.get("RAT_STATE_PG_REF")
        self._gateway = os.environ.get("RAT_GATEWAY", "127.0.0.1:7777")
        self._caller = os.environ.get("RAT_PLUGIN_NAME", "rat-state")
        self._tenant = os.environ.get("RAT_TENANT", "acme")

    def _resolve_dsn(self) -> str:
        """The DSN: a literal env value, or — preferred — a secret REF resolved through the
        gateway (C5-authorized via this plugin's `requires`, audited). Retries because the
        secret plugin may not be wired into the gateway yet at the very first call."""
        if self._direct_dsn:
            return self._direct_dsn
        if not self._dsn_ref:
            raise RuntimeError("set RAT_STATE_PG_REF (a secret ref) or RAT_STATE_PG (a literal DSN)")
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(traceparent="00-" + "e" * 32 + "-" + "f" * 16 + "-01", correlation_id="rat-state"),
            identity=context_pb2.Identity(caller_plugin=self._caller, tenant=self._tenant))
        md = [("rat-callmeta-bin", rc.SerializeToString())]
        stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(self._gateway))
        last = "unknown"
        for _ in range(60):
            try:
                r = stub.Invoke(invoke_pb2.InvokeRequest(
                    capability="rat://secret/v1/resolve",
                    payload=secret_pb2.ResolveRequest(secret_ref=self._dsn_ref).SerializeToString()), metadata=md)
                resp = secret_pb2.ResolveResponse()
                resp.ParseFromString(r.result)
                if resp.found:
                    return resp.value.decode("utf-8")
                last = f"secret {self._dsn_ref!r} not found (absent or not authorized)"
            except grpc.RpcError as e:
                last = f"{e.code()}: {e.details()}"
            time.sleep(1)
        raise RuntimeError(f"could not resolve DSN ref {self._dsn_ref!r} via {self._gateway}: {last}")

    def _ensure(self):
        """Lazily resolve the DSN and create the table — once, on first state operation."""
        if self._initialized:
            return
        with self._init_lock:
            if self._initialized:
                return
            self._dsn = self._resolve_dsn()
            with self._conn() as c, c.cursor() as cur:
                cur.execute("CREATE TABLE IF NOT EXISTS rat_state (k TEXT PRIMARY KEY, v BYTEA, revision BIGINT NOT NULL DEFAULT 1)")
                c.commit()
            self._initialized = True

    def _conn(self):
        return psycopg2.connect(self._dsn)

    def Get(self, req, context):
        self._ensure()
        with self._conn() as c, c.cursor() as cur:
            cur.execute("SELECT v, revision FROM rat_state WHERE k = %s", (req.key,))
            row = cur.fetchone()
        if not row:
            return state_pb2.GetResponse(found=False)
        return state_pb2.GetResponse(found=True, value=bytes(row[0]), revision=row[1])

    def Put(self, req, context):
        self._ensure()
        with self._lock, self._conn() as c, c.cursor() as cur:
            cur.execute("SELECT revision FROM rat_state WHERE k = %s", (req.key,))
            row = cur.fetchone()
            cur_rev = row[0] if row else 0
            if req.if_revision and req.if_revision != cur_rev:  # CAS precondition failed
                return state_pb2.PutResponse(outcome=state_pb2.PUT_OUTCOME_CONFLICT, revision=cur_rev)
            new_rev = cur_rev + 1
            cur.execute(
                "INSERT INTO rat_state (k, v, revision) VALUES (%s, %s, %s) "
                "ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v, revision = EXCLUDED.revision",
                (req.key, psycopg2.Binary(req.value), new_rev))
            c.commit()
        return state_pb2.PutResponse(outcome=state_pb2.PUT_OUTCOME_COMMITTED, revision=new_rev)

    def CreateIfAbsent(self, req, context):
        """Atomically create the key only if absent (ADR-049). Unlike Put (whose read→check→write is
        serialized by the in-process _lock — a single-node shim), this uses Postgres's native
        `INSERT … ON CONFLICT (k) DO NOTHING`, which is atomic at the DATABASE across ALL clients and
        replicas — so it's the correct multi-replica HA primitive (the lease bootstrap / ticket store
        backed by a shared Postgres). RETURNING tells us whether THIS insert won the row."""
        self._ensure()
        with self._conn() as c, c.cursor() as cur:
            cur.execute(
                "INSERT INTO rat_state (k, v, revision) VALUES (%s, %s, 1) "
                "ON CONFLICT (k) DO NOTHING RETURNING revision",
                (req.key, psycopg2.Binary(req.value)))
            row = cur.fetchone()
            if row is not None:  # we created it
                c.commit()
                return state_pb2.CreateIfAbsentResponse(outcome=state_pb2.PUT_OUTCOME_COMMITTED, revision=row[0])
            # already existed (a concurrent/earlier creator won) → report the existing revision
            cur.execute("SELECT revision FROM rat_state WHERE k = %s", (req.key,))
            existing = cur.fetchone()
            c.commit()
        return state_pb2.CreateIfAbsentResponse(
            outcome=state_pb2.PUT_OUTCOME_CONFLICT, revision=existing[0] if existing else 0)

    def List(self, req, context):
        self._ensure()
        with self._conn() as c, c.cursor() as cur:
            cur.execute("SELECT k FROM rat_state WHERE k LIKE %s ORDER BY k", (req.prefix + "%",))
            keys = [r[0] for r in cur.fetchall()]
        return state_pb2.ListResponse(keys=keys)
