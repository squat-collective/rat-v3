"""The MarketplaceService gRPC implementation (Python) — `marketplace` reference.

Implements the two proto operations: Search (capability-aware discovery) and Get
(one listing's full detail). This axis carries no tenant/context — discovery is
deployment-scoped, not tenant-scoped — so there is no rat-callmeta-bin handling
here; the deployment's capabilities arrive as an explicit request field.
"""

import grpc

from rat.marketplace.v1 import marketplace_pb2, marketplace_pb2_grpc

from store import Marketplace, MarketplaceError


class MarketplaceServicer(marketplace_pb2_grpc.MarketplaceServiceServicer):
    def __init__(self, marketplace: Marketplace = None) -> None:
        self._market = marketplace or Marketplace()

    def Search(self, request, context):
        listings = self._market.search(
            request.query,
            request.kind,
            list(request.deployment_capabilities),
        )
        return marketplace_pb2.SearchResponse(listings=listings)

    def Get(self, request, context):
        try:
            listing = self._market.get(request.plugin_id)
        except MarketplaceError as e:
            context.abort(e.code, e.message)
        return marketplace_pb2.GetResponse(listing=listing)
