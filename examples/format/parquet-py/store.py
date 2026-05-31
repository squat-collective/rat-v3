"""Parquet-backed format store for rat-format-parquet-py — a ROUND-2 reference
(ADR-003). Half of the real `format` pair (with delta-py).

Where the in-memory format refs hold rows in a dict, this writes them as REAL
Parquet files on disk (one directory per table identifier) and reads them back with
pyarrow — the real data leg, not the toy string-row registry. It passes the SAME
shared format vectors (format-v1.json) — format's data is just rows, so the vectors
are provider-neutral — and adds a backend-specific test that real Parquet files land
on disk.
"""

import glob
import os

import grpc
import pyarrow as pa
import pyarrow.dataset as ds
import pyarrow.parquet as pq

FORMAT = "Parquet"


class FormatError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


def rows_to_table(rows):
    return pa.Table.from_pylist(rows)


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

    def table_dir(self, identifier: str) -> str:
        d = os.path.join(self.root, identifier.replace("/", "_"))
        os.makedirs(d, exist_ok=True)
        return d

    def _read(self, identifier):
        d = self.table_dir(identifier)
        if not glob.glob(os.path.join(d, "*.parquet")):
            return []
        return ds.dataset(d, format="parquet").to_table().to_pylist()

    def _write_all(self, identifier, rows):
        d = self.table_dir(identifier)
        for f in glob.glob(os.path.join(d, "*.parquet")):
            os.remove(f)
        if rows:
            pq.write_table(rows_to_table(rows), os.path.join(d, "data.parquet"))

    def append(self, identifier, rows):
        d = self.table_dir(identifier)
        if rows:
            n = len(glob.glob(os.path.join(d, "*.parquet")))
            pq.write_table(rows_to_table(rows), os.path.join(d, f"part-{n}.parquet"))
        return len(rows)

    def merge(self, identifier, keys, src):
        self._write_all(identifier, _upsert(self._read(identifier), src, keys))
        return len(src)

    def overwrite(self, identifier, rows):
        self._write_all(identifier, rows)
        return len(rows)

    def scan(self, identifier):
        return self._read(identifier)

    def maintain(self, identifier):
        # compaction: rewrite the table to a single file (a real maintenance op).
        self._write_all(identifier, self._read(identifier))
