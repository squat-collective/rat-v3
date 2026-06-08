"""The web-portal UI plugin's in-process state + behavior.

A `kind: ui` plugin is an experience surface (overview.md / Phase 5 multi-UI
story: each UI is a separate ui plugin). This contract is deliberately thin — a
UI mostly CONSUMES the API gateway like any other client. What the core needs is
(a) discovery of the surfaces/slots it exposes, and (b) the ability to resolve a
slot + contributing component to the render info (asset ref + props schema), so
other plugins can contribute components via `contributes.slots` in their manifest.

This module holds the pure logic; `server.py` is the gRPC adapter over it.
"""

import json

import grpc


class UiError(Exception):
    """A domain error carrying a gRPC status code; the servicer translates it
    into context.abort(...)."""

    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class WebPortalUi:
    DISPLAY_NAME = "Web Portal"

    # The slots this UI hosts: (slot id, description). Slot ids are
    # capability-shaped URIs — the same ones plugins name in
    # contributes.slots[].target in their plugin.v1.json.
    SLOTS = [
        ("rat://ui/v1/pipeline-detail", "Per-pipeline detail page"),
        ("rat://ui/v1/dataset-overview", "Dataset overview panel"),
    ]

    # Resolved contributions: (slot, component) -> (asset_ref, props_schema).
    # In production these are discovered from peer plugins' contributes.slots; the
    # reference hard-codes a representative set so the harness can resolve them.
    COMPONENTS = {
        ("rat://ui/v1/pipeline-detail", "lineage-graph"): (
            "oci://ghcr.io/rat-dev/lineage-graph:1.0.0",
            {"type": "object", "properties": {"pipeline_id": {"type": "string"}}},
        ),
        ("rat://ui/v1/dataset-overview", "schema-card"): (
            "oci://ghcr.io/rat-dev/schema-card:1.0.0",
            {"type": "object", "properties": {"dataset_id": {"type": "string"}}},
        ),
    }

    def describe(self):
        """Return (display_name, [(slot, description), ...])."""
        return self.DISPLAY_NAME, list(self.SLOTS)

    def render_slot(self, slot: str, component: str):
        """Resolve (slot, component) -> (asset_ref: str, props_schema_bytes: bytes).
        Unknown (slot, component) -> UiError(NOT_FOUND)."""
        entry = self.COMPONENTS.get((slot, component))
        if entry is None:
            raise UiError(
                grpc.StatusCode.NOT_FOUND,
                f"no component {component!r} contributed to slot {slot!r}",
            )
        asset_ref, props_schema = entry
        return asset_ref, json.dumps(props_schema).encode("utf-8")
