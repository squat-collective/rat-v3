"""In-memory table store for rat-format-inmemory-py — the SECOND independent
`kind: format` reference (ADR-003).

Deliberately a from-scratch code path, not a port of the Go reference's internals:
its job is to prove the format/v1 wire contract is implementable by a different
implementation in a different language, and that it passes the SAME golden vectors
(contracts/conformance/format-v1.json). Tables are ordered lists of string-valued
rows; "bulk" data is carried by the in-process stream registry (streams.py), same
control-contract-only scope as the Go reference.
"""

import threading
from typing import Dict, List

Row = Dict[str, str]


class _Table:
    def __init__(self) -> None:
        self.rows: List[Row] = []
        self.snapshot: int = 0  # bumped on every mutation -> WriteResult.snapshot_id


class Store:
    """identifier -> _Table. Lock-guarded so the gRPC threadpool can serve in
    parallel."""

    def __init__(self) -> None:
        self._tables: Dict[str, _Table] = {}
        self._lock = threading.Lock()

    def _get(self, identifier: str) -> _Table:
        t = self._tables.get(identifier)
        if t is None:
            t = _Table()
            self._tables[identifier] = t
        return t

    def append(self, identifier: str, rows: List[Row]) -> tuple[int, int]:
        """Add rows unconditionally. Returns (rows_affected, new_snapshot)."""
        with self._lock:
            t = self._get(identifier)
            t.rows.extend(rows)
            t.snapshot += 1
            return len(rows), t.snapshot

    def overwrite(self, identifier: str, rows: List[Row]) -> tuple[int, int]:
        """Replace all rows. Returns (rows_affected, new_snapshot)."""
        with self._lock:
            t = self._get(identifier)
            t.rows = list(rows)
            t.snapshot += 1
            return len(rows), t.snapshot

    def merge(self, identifier: str, merge_keys: List[str], src: List[Row]) -> tuple[int, int]:
        """Upsert each source row onto the first existing row whose merge_keys all
        match, else append. Returns (rows_affected, new_snapshot)."""
        with self._lock:
            t = self._get(identifier)
            affected = 0
            for sr in src:
                idx = next((i for i, ex in enumerate(t.rows) if _keys_match(ex, sr, merge_keys)), -1)
                if idx >= 0:
                    t.rows[idx] = sr
                else:
                    t.rows.append(sr)
                affected += 1
            t.snapshot += 1
            return affected, t.snapshot

    def scan(self, identifier: str, columns: List[str]) -> List[Row]:
        """Return a copy of the rows in insertion order, optionally projecting
        columns (deterministic for golden vectors)."""
        with self._lock:
            t = self._get(identifier)
            if not columns:
                return [dict(r) for r in t.rows]
            return [{c: r[c] for c in columns if c in r} for r in t.rows]

    def maintain(self, identifier: str) -> int:
        """No-op upkeep (nothing to compact in memory); still bumps the snapshot so
        the contract's "returns a WriteResult" holds. Returns new_snapshot."""
        with self._lock:
            t = self._get(identifier)
            t.snapshot += 1
            return t.snapshot


def _keys_match(a: Row, b: Row, keys: List[str]) -> bool:
    return all(a.get(k) == b.get(k) for k in keys)
