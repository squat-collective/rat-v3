"""In-memory catalog for rat-catalog-inmemory-py — the SECOND independent
`kind: catalog` reference (ADR-003).

A from-scratch Python code path mirroring the Go reference's MODEL (so the two stay
in lockstep on the shared golden vectors): branches are global (a branch is a named
snapshot of the catalog), one table is seeded on `main` at `snap-0`, CreateBranch
copies the source branch's snapshot, and MergeBranch mints `snap-<counter>` under
the optimistic-concurrency + idempotency contract (reviews/06 #8).
"""

import threading

import grpc

SEED_TABLE = "warehouse.sales.orders"


class CatalogError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class Catalog:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._branches = {"main": "snap-0"}  # branch -> snapshot id
        self._tables = {SEED_TABLE}          # known table identifiers
        self._merges: dict[str, str] = {}     # idempotency_key -> resulting snapshot
        self._counter = 0

    def get_table(self, identifier: str, branch: str):
        if not identifier:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "identifier is required")
        with self._lock:
            if identifier not in self._tables:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown table {identifier!r}")
            branch = branch or "main"
            if branch not in self._branches:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
            return branch, f"catalog://{identifier}@{branch}"

    def create_branch(self, branch: str, from_branch: str) -> None:
        if not branch:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "branch is required")
        from_branch = from_branch or "main"
        with self._lock:
            if from_branch not in self._branches:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown from_branch {from_branch!r}")
            self._branches[branch] = self._branches[from_branch]

    def merge_branch(self, branch: str, into: str, expected: str, key: str):
        """Returns (snapshot_id, already_applied)."""
        with self._lock:
            if key and key in self._merges:
                return self._merges[key], True
            if branch not in self._branches:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
            if into not in self._branches:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown into_branch {into!r}")
            cur = self._branches[into]
            if expected and expected != cur:
                raise CatalogError(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    f"into_branch {into!r} is at {cur!r}, not the expected {expected!r} (concurrent merge?)",
                )
            self._counter += 1
            snap = f"snap-{self._counter}"
            self._branches[into] = snap
            if key:
                self._merges[key] = snap
            return snap, False
