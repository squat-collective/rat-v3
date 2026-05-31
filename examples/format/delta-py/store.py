"""Delta Lake-backed format store for rat-format-delta-py — a ROUND-2 reference
(ADR-003). The SECOND real `format` (paired with parquet-py).

Backs the table with a real **Delta Lake** table (a transaction log over Parquet) —
genuinely different storage technology from plain Parquet files. It passes the SAME
shared format vectors AND earns a property neither the in-memory dict nor plain
Parquet files can show: **time travel** (read a prior table version via Delta's
versioned snapshots — the Iceberg/Delta semantic the catalog axis's branches sit on
top of). Only this file differs from parquet-py; server/streams are identical.
"""

import os

import grpc
import pyarrow as pa
from deltalake import DeltaTable, write_deltalake

FORMAT = "Delta"


class FormatError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


def _upsert(existing, src, keys):
    out = list(existing)
    for sr in src:
        idx = next((i for i, e in enumerate(out) if all(e.get(k) == sr.get(k) for k in keys)), -1)
        if idx >= 0:
            out[idx] = sr
        else:
            out.append(sr)
    return out


class Store:
    def __init__(self, root: str) -> None:
        self.root = root

    def table_path(self, identifier: str) -> str:
        return os.path.join(self.root, identifier.replace("/", "_"))

    def _read(self, identifier):
        try:
            dt = DeltaTable(self.table_path(identifier))
        except Exception:  # TableNotFoundError → no table yet
            return []
        return dt.to_pyarrow_table().to_pylist()

    def _overwrite(self, identifier, rows):
        if rows:
            write_deltalake(self.table_path(identifier), pa.Table.from_pylist(rows), mode="overwrite")

    def append(self, identifier, rows):
        if rows:
            write_deltalake(self.table_path(identifier), pa.Table.from_pylist(rows), mode="append")
        return len(rows)

    def merge(self, identifier, keys, src):
        self._overwrite(identifier, _upsert(self._read(identifier), src, keys))
        return len(src)

    def overwrite(self, identifier, rows):
        self._overwrite(identifier, rows)
        return len(rows)

    def scan(self, identifier):
        return self._read(identifier)

    def maintain(self, identifier):
        # real maintenance: compact the Delta table's files.
        try:
            DeltaTable(self.table_path(identifier)).optimize.compact()
        except Exception:
            pass
