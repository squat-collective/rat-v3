"""In-memory mini-SQL store for rat-engine-inmemory-py — the SECOND independent
`kind: engine` reference (ADR-003).

A from-scratch Python code path (not a port of the Go internals): its job is to
prove the engine/v1 wire contract is implementable by a different implementation in
a different language, passing the SAME golden vectors
(contracts/conformance/engine-v1.json). Self-contained in-memory tables; the real
engine↔format handoff is separate integration work.
"""

import threading
from typing import Dict, List

from sql import SQLError, Stmt

Row = Dict[str, str]


class _Table:
    def __init__(self, cols: List[str]) -> None:
        self.cols = list(cols)
        self.rows: List[Row] = []


class Store:
    """name -> _Table, plus a monotonic snapshot bumped on every mutation."""

    def __init__(self) -> None:
        self._tables: Dict[str, _Table] = {}
        self._snapshot = 0
        self._lock = threading.Lock()

    def create(self, name: str, cols: List[str]) -> int:
        with self._lock:
            self._tables[name] = _Table(cols)
            self._snapshot += 1
            return self._snapshot

    def insert(self, name: str, vals: List[str]) -> tuple[int, int]:
        """Bind values positionally to the table's columns + append. Returns
        (rows_affected=1, snapshot). Raises SQLError on unknown table."""
        with self._lock:
            t = self._tables.get(name)
            if t is None:
                raise SQLError(f"unknown table {name!r}")
            r = {c: vals[i] for i, c in enumerate(t.cols) if i < len(vals)}
            t.rows.append(r)
            self._snapshot += 1
            return 1, self._snapshot

    def select_rows(self, st: Stmt) -> List[Row]:
        """Apply an optional WHERE equality filter + projection, in insertion
        order. Raises SQLError on unknown table."""
        with self._lock:
            t = self._tables.get(st.table)
            if t is None:
                raise SQLError(f"unknown table {st.table!r}")
            out: List[Row] = []
            for r in t.rows:
                if st.has_where and r.get(st.where_col) != st.where_val:
                    continue
                out.append(_project(r, st.cols, t.cols))
            return out


def _project(r: Row, proj: List[str], all_cols: List[str]) -> Row:
    cols = all_cols if proj == ["*"] else proj
    return {c: r[c] for c in cols if c in r}
