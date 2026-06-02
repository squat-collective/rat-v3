"""A Postgres-backed StateService — the platform's state-backend (run/pipeline/schedule
metadata). EXPLORATORY (the data platform bundle, ADR-020 S4). Implements the frozen
state/v1 Get/Put/List with monotonic revisions + single-key compare-and-set (if_revision).

Keys follow the frozen KEY GRAMMAR (state.proto header): printable UTF-8, `/` as a
logical separator, no empty / control chars / `.`|`..` path segments. The gateway is the
namespace boundary; this backend stores plugin-relative keys verbatim.
"""

import os
import threading

import psycopg2

from rat.state.v1 import state_pb2, state_pb2_grpc


class PostgresState(state_pb2_grpc.StateServiceServicer):
    def __init__(self) -> None:
        self._dsn = os.environ["RAT_STATE_PG"]
        self._lock = threading.Lock()  # serialize CAS (a single-node linearizable shim)
        with self._conn() as c, c.cursor() as cur:
            cur.execute("CREATE TABLE IF NOT EXISTS rat_state (k TEXT PRIMARY KEY, v BYTEA, revision BIGINT NOT NULL DEFAULT 1)")
            c.commit()

    def _conn(self):
        return psycopg2.connect(self._dsn)

    def Get(self, req, context):
        with self._conn() as c, c.cursor() as cur:
            cur.execute("SELECT v, revision FROM rat_state WHERE k = %s", (req.key,))
            row = cur.fetchone()
        if not row:
            return state_pb2.GetResponse(found=False)
        return state_pb2.GetResponse(found=True, value=bytes(row[0]), revision=row[1])

    def Put(self, req, context):
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

    def List(self, req, context):
        with self._conn() as c, c.cursor() as cur:
            cur.execute("SELECT k FROM rat_state WHERE k LIKE %s ORDER BY k", (req.prefix + "%",))
            keys = [r[0] for r in cur.fetchall()]
        return state_pb2.ListResponse(keys=keys)
