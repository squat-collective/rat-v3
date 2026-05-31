"""The CatalogService gRPC implementation (Python) — second `catalog` reference.

GetTable / CreateBranch / MergeBranch against the in-memory catalog. RequestContext
is NOT a field (ADR-007); this reference ignores identity. CatalogError → the
matching gRPC status via context.abort.
"""

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.common.v1 import data_pb2

from store import Catalog, CatalogError


class CatalogServicer(catalog_pb2_grpc.CatalogServiceServicer):
    def __init__(self) -> None:
        self.cat = Catalog()

    def GetTable(self, request, context):
        try:
            branch, uri = self.cat.get_table(request.identifier, request.branch)
        except CatalogError as e:
            context.abort(e.code, e.message)
        return catalog_pb2.GetTableResponse(
            table=data_pb2.TableRef(identifier=request.identifier, uri=uri, branch=branch)
        )

    def CreateBranch(self, request, context):
        try:
            self.cat.create_branch(request.branch, request.from_branch)
        except CatalogError as e:
            context.abort(e.code, e.message)
        return catalog_pb2.CreateBranchResponse(branch=request.branch)

    def MergeBranch(self, request, context):
        try:
            snap, already = self.cat.merge_branch(
                request.branch, request.into_branch, request.expected_into_snapshot, request.idempotency_key
            )
        except CatalogError as e:
            context.abort(e.code, e.message)
        return catalog_pb2.MergeBranchResponse(snapshot_id=snap, already_applied=already)
