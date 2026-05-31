"""DuckDB-backed engine for rat-engine-duckdb-py — a ROUND-2 reference (ADR-003).

A REAL SQL engine (not the toy regex parser of the in-memory refs): DuckDB executes
the typed SQL of contracts/conformance/engine-real-v1.json and returns results as
real typed Arrow. Paired with the DataFusion reference, this is ADR-003's literal
'duckdb + datafusion' two-engine cross-run — and it retires the typed-Arrow gap for
engine (the data leg is real Arrow IPC, not the toy string-row stand-in).
"""

import duckdb
import grpc
import pyarrow as pa

ENGINE = "DuckDB"


class EngineError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class Engine:
    def __init__(self) -> None:
        self.con = duckdb.connect()  # in-memory

    def execute(self, sql: str) -> int:
        """Run a DDL/DML statement; return rows_affected (DuckDB returns the inserted
        count as the statement's result; DDL returns none → 0)."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        try:
            res = self.con.execute(sql)
        except Exception as e:  # parse/catalog error
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        try:
            rows = res.fetchall()
        except Exception:
            rows = []
        if rows and len(rows[0]) == 1 and isinstance(rows[0][0], int):
            return int(rows[0][0])
        return 0

    def query(self, sql: str, limit: int = 0) -> pa.Table:
        """Run a SELECT, returning a real Arrow table. `limit` (Preview) bounds rows."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        q = sql if limit <= 0 else f"SELECT * FROM ({sql}) LIMIT {limit}"
        try:
            return self.con.execute(q).to_arrow_table()
        except Exception as e:
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
