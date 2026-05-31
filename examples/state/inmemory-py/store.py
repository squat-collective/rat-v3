"""In-memory key/value state for rat-state-inmemory-py — mirrors the Go reference's
store.go MODEL (global monotonic revision, compare-and-set, append-only change log
for Watch) so the two impls stay in lockstep.
"""

import threading
from typing import Dict, List, Tuple


class Store:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._data: Dict[str, Tuple[bytes, int]] = {}  # key -> (value, revision)
        self._log: List[Tuple[str, bytes, int]] = []    # append-only (key, value, revision)
        self._rev = 0

    def get(self, key: str):
        with self._lock:
            if key not in self._data:
                return False, b"", 0
            value, rev = self._data[key]
            return True, value, rev

    def put(self, key: str, value: bytes, if_rev: int):
        """Compare-and-set. Returns (committed, revision): on commit, revision is the
        new rev; on conflict, revision is the current (conflicting) rev (no write).
        In-memory always knows the outcome, so UNKNOWN never arises."""
        with self._lock:
            cur_rev = self._data[key][1] if key in self._data else 0
            if if_rev > 0 and cur_rev != if_rev:
                return False, cur_rev
            self._rev += 1
            self._data[key] = (value, self._rev)
            self._log.append((key, value, self._rev))
            return True, self._rev

    def list(self, prefix: str) -> List[str]:
        with self._lock:
            return sorted(k for k in self._data if k.startswith(prefix))

    def watch_backlog(self, prefix: str, from_rev: int):
        """Change-log events with revision >= from_rev whose key has the prefix, in
        revision order. from_rev == 0 means 'from now' (no backlog) per the contract;
        the vectors use from_rev=1 to replay from the beginning."""
        with self._lock:
            if from_rev == 0:
                return []
            return [(k, v, r) for (k, v, r) in self._log if r >= from_rev and k.startswith(prefix)]
