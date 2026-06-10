"""rat.plugin — the rat plugin runtime SDK (ADR-029). Kills the serve + consume boilerplate.

Hand-written (NOT generated); ships in plugin-base-py alongside rat.contrib.

    from rat import plugin
    from rat.secret.v1 import secret_pb2, secret_pb2_grpc

    plugin.serve(lambda s: secret_pb2_grpc.add_SecretServiceServicer_to_server(Keyring(), s))

    gw = plugin.Gateway()
    resp = gw.call("rat://secret/v1/resolve",
                   secret_pb2.ResolveRequest(secret_ref=ref), secret_pb2.ResolveResponse)
"""

import os
import secrets as _rand
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc


def serve(register, max_workers: int = 8) -> None:
    """Run the plugin's gRPC server until terminated. `register(server)` adds your servicer(s)."""
    addr = os.environ.get("RAT_PLUGIN_ADDR", "0.0.0.0:50051")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
    register(server)
    server.add_insecure_port(addr)
    server.start()
    print(f"ratplugin: {os.environ.get('RAT_PLUGIN_NAME', '')} serving on {addr}", flush=True)
    server.wait_for_termination()


def env_map(name: str) -> dict:
    """Parse a "k=v,k=v" env var (the RAT_SECRETS / config convention) into a dict."""
    out = {}
    for kv in os.environ.get(name, "").split(","):
        if "=" in kv:
            k, v = kv.split("=", 1)
            out[k.strip()] = v
    return out


def _traceparent() -> str:
    return f"00-{_rand.token_hex(16)}-{_rand.token_hex(8)}-01"


class Gateway:
    """Calls capabilities through the gateway (RAT_GATEWAY). Build once, reuse."""

    def __init__(self):
        self.addr = os.environ.get("RAT_GATEWAY", "127.0.0.1:7777")
        self.name = os.environ.get("RAT_PLUGIN_NAME", "")
        # C2: the per-launch bearer token rat injected. The gateway derives caller_plugin from
        # it on the plugin door (the wire identity below is no longer trusted for authz).
        self.token = os.environ.get("RAT_PLUGIN_TOKEN", "")
        self._chan = grpc.insecure_channel(self.addr)
        self._stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(self._chan)

    def call(self, capability: str, req, resp_type, tenant: str = "default"):
        """Stamp the rat-callmeta-bin envelope (identity + trace), Invoke, parse into resp_type."""
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(traceparent=_traceparent(), correlation_id=self.name),
            identity=context_pb2.Identity(caller_plugin=self.name, tenant=tenant),
        )
        md = [("rat-callmeta-bin", rc.SerializeToString())]
        if self.token:
            md.append(("rat-plugin-token", self.token))
        out = self._stub.Invoke(
            invoke_pb2.InvokeRequest(capability=capability, payload=req.SerializeToString()),
            metadata=md,
        )
        resp = resp_type()
        resp.ParseFromString(out.result)
        return resp


def caller_tenant(context) -> str:
    """Read the calling tenant out of the incoming rat-callmeta-bin metadata (C7 scoping)."""
    for key, value in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(value)
            return rc.identity.tenant
    return ""
