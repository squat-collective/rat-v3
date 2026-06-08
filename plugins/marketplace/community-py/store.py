"""The in-process marketplace catalog — a community `kind: marketplace` plugin.

A marketplace is a discovery surface for plugins. Its one hard job on a
pluggable-everything platform is the COMPATIBILITY question — "does this plugin
work on MY deployment?" — which is answerable BECAUSE of the capability model:
each listing advertises its required + provided capabilities, so search can
compute fit against what the caller's deployment provides.

This reference seeds a tiny catalog of real-ish RAT plugins and implements the
two operations the proto mandates: `search` (capability-aware discovery) and
`get` (one listing's full detail).
"""

import grpc

from rat.marketplace.v1 import marketplace_pb2


class MarketplaceError(Exception):
    """Carries a gRPC status code + message for the servicer to surface."""

    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


def _seed_listings():
    """Three real-ish RAT plugins. Capability sets are MANDATORY (proto N2):
    provided/required drive the 'works on your deployment?' filter, conformed
    mirrors provided (these are conformance-passing references), and every
    listing is signed."""
    return [
        marketplace_pb2.Listing(
            plugin_id="rat-engine-duckdb-py",
            kind="engine",
            version="1.0.0",
            description="DuckDB SQL engine",
            provided_capabilities=[
                "rat://engine/v1/query",
                "rat://engine/v1/execute",
                "rat://engine/v1/preview",
            ],
            required_capabilities=["rat://format/v1/scan"],
            conformed_capabilities=[
                "rat://engine/v1/query",
                "rat://engine/v1/execute",
                "rat://engine/v1/preview",
            ],
            signed=True,
            signed_by="rat-dev",
            support_url="https://github.com/rat-dev/rat/issues",
        ),
        marketplace_pb2.Listing(
            plugin_id="rat-format-parquet-py",
            kind="format",
            version="1.0.0",
            description="Parquet format reader/writer",
            provided_capabilities=[
                "rat://format/v1/scan",
                "rat://format/v1/append",
                "rat://format/v1/merge",
                "rat://format/v1/overwrite",
            ],
            required_capabilities=["rat://storage/v1/vend-credentials"],
            conformed_capabilities=[
                "rat://format/v1/scan",
                "rat://format/v1/append",
                "rat://format/v1/merge",
                "rat://format/v1/overwrite",
            ],
            signed=True,
            signed_by="rat-dev",
            support_url="https://github.com/rat-dev/rat/issues",
        ),
        marketplace_pb2.Listing(
            plugin_id="rat-strategy-scd2-py",
            kind="strategy",
            version="1.0.0",
            description="SCD type-2 slowly-changing-dimension strategy",
            provided_capabilities=["rat://strategy/v1/apply"],
            required_capabilities=[
                "rat://catalog/v1/get-table",
                "rat://format/v1/scan",
                "rat://format/v1/merge",
            ],
            conformed_capabilities=["rat://strategy/v1/apply"],
            signed=True,
            signed_by="rat-dev",
            support_url="https://github.com/rat-dev/rat/issues",
        ),
    ]


class Marketplace:
    def __init__(self) -> None:
        self._listings = _seed_listings()

    def search(self, query: str, kind: str, deployment_capabilities):
        """Capability-aware discovery. Filter by kind (exact, if given), query
        (case-insensitive substring of plugin_id or description, if given), and
        COMPATIBILITY: when deployment_capabilities is non-empty, keep only
        listings whose required_capabilities are ALL provided by the deployment.
        That last clause is the 'works on my deployment?' filter — the axis's
        one hard job."""
        q = (query or "").lower()
        deploy = set(deployment_capabilities or [])
        out = []
        for listing in self._listings:
            if kind and listing.kind != kind:
                continue
            if q and q not in listing.plugin_id.lower() and q not in listing.description.lower():
                continue
            if deploy and not set(listing.required_capabilities).issubset(deploy):
                continue
            out.append(listing)
        return out

    def get(self, plugin_id: str):
        for listing in self._listings:
            if listing.plugin_id == plugin_id:
                return listing
        raise MarketplaceError(
            grpc.StatusCode.NOT_FOUND, f"no listing for plugin_id {plugin_id!r}"
        )
