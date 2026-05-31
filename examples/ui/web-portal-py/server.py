"""The UiService gRPC implementation (Python) — the `ui` reference.

Implements Describe (enumerate this UI's surfaces + hosted slots) and RenderSlot
(resolve a slot + contributing component to render info: asset ref + props schema).
The behavior lives in `store.py`; this is the thin gRPC adapter over it.

This axis carries no tenant/context: a UI plugin's Describe/RenderSlot are surface
metadata, not tenant-scoped operations — so there is no rat-callmeta-bin handling.
"""

from rat.ui.v1 import ui_pb2, ui_pb2_grpc

from store import UiError, WebPortalUi


class UiServicer(ui_pb2_grpc.UiServiceServicer):
    def __init__(self, ui: WebPortalUi | None = None) -> None:
        self._ui = ui or WebPortalUi()

    def Describe(self, request, context):
        display_name, slots = self._ui.describe()
        return ui_pb2.DescribeResponse(
            display_name=display_name,
            slots=[
                ui_pb2.HostedSlot(slot=slot, description=description)
                for slot, description in slots
            ],
        )

    def RenderSlot(self, request, context):
        try:
            asset_ref, props_schema = self._ui.render_slot(request.slot, request.component)
        except UiError as e:
            context.abort(e.code, e.message)
        return ui_pb2.RenderSlotResponse(asset_ref=asset_ref, props_schema=props_schema)
