"""rat.contrib — hand-written SDK helpers (NOT generated; codegen only writes rat/<axis>/v1).

contribute_ui(): a plugin publishes its UI contributions (ADR-024) in one call, so the
platform UI renders them — no hand-rolled state/put per plugin.

    from rat.contrib import contribute_ui
    contribute_ui(os.environ["RAT_GATEWAY"], os.environ["RAT_PLUGIN_NAME"], [
        {"slot": "explorer", "id": "tables", "title": "My Tables", "data": "/api/tables"},
        {"slot": "command",  "id": "go",     "title": "Run it", "capability": "rat://x/v1/y", "args": {}},
    ])

The plugin must `requires: rat://state/v1/put` (the contributions are stored in the
state-backend at ui/components/<plugin>/<id>) and declare its slot binding in
`contributes.slots`. Resolution/auth is the usual gateway path (C5-authorized + audited).
"""

import json
import time

import grpc

from rat.common.v1 import context_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.state.v1 import state_pb2


def contribute_ui(gateway, caller, components, tenant="acme", retries=1, traceparent=None, surface=None):
    """Publish a plugin's UI components to the state-backend via the gateway.

    gateway   — the RAT gateway address (e.g. $RAT_GATEWAY, injected by rat).
    caller    — this plugin's identity ($RAT_PLUGIN_NAME); also the ui/components/<caller>/ ns.
    components— a list of component-spec dicts, each with at least {slot, id, title}. A
                per-component "surface" (ADR-025: vscode|cli|webapp|generic) targets it at a
                surface; absent → the `surface` arg (else surface-agnostic).
    surface   — default surface stamped on components that don't set their own.
    retries   — attempts (>1 to ride out the state plugin still wiring at boot).

    Raises the last gRPC error if every attempt fails.
    """
    if surface is not None:
        components = [{"surface": surface, **c} for c in components]
    tp = traceparent or ("00-" + "a" * 32 + "-" + "b" * 16 + "-01")
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent=tp, correlation_id="ui-contrib"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant=tenant))
    md = [("rat-callmeta-bin", rc.SerializeToString())]
    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway))

    last = None
    for attempt in range(max(1, retries)):
        try:
            for c in components:
                # key by surface (ADR-025) so the same component id can target multiple
                # surfaces (vscode/cli/webapp) without colliding.
                key = f"ui/components/{caller}/{c.get('surface', 'generic')}/{c['id']}"
                stub.Invoke(invoke_pb2.InvokeRequest(
                    capability="rat://state/v1/put",
                    payload=state_pb2.PutRequest(key=key, value=json.dumps(c).encode()).SerializeToString()), metadata=md)
            return
        except grpc.RpcError as e:
            last = e
            if attempt + 1 < retries:
                time.sleep(2)
    if last is not None:
        raise last
