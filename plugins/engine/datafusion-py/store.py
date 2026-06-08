"""DataFusion-backed engine for rat-engine-datafusion-py — a ROUND-2 reference
(ADR-003).

The SECOND real engine (paired with duckdb-py): Apache DataFusion (a Rust query
engine, Arrow-native) executes the SAME typed SQL of engine-real-v1.json and returns
the SAME typed Arrow results. DuckDB and DataFusion agreeing on the golden data —
two genuinely different engine technologies — is ADR-003's literal two-engine
cross-run. Only this file differs from the duckdb reference; server/streams/harness
are identical.
"""

import grpc
import pyarrow as pa
from datafusion import SessionContext

ENGINE = "DataFusion"


class EngineError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class Engine:
    def __init__(self) -> None:
        self.ctx = SessionContext()

    def execute(self, sql: str) -> int:
        """Run a DDL/DML statement; return rows_affected (DataFusion returns an
        INSERT's row count in a `count` column; DDL returns no rows → 0)."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        try:
            batches = self.ctx.sql(sql).collect()
        except Exception as e:
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        if batches:
            tbl = pa.Table.from_batches(batches)
            if tbl.num_rows >= 1 and "count" in tbl.schema.names:
                return int(tbl.column("count")[0].as_py())
        return 0

    def query(self, sql: str, limit: int = 0) -> pa.Table:
        """Run a SELECT, returning a real Arrow table. `limit` (Preview) bounds rows."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        q = sql if limit <= 0 else f"SELECT * FROM ({sql}) LIMIT {limit}"
        try:
            return self.ctx.sql(q).to_arrow_table()
        except Exception as e:
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
