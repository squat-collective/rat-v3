"""The mediating capability-invoke gateway for the composition (ADR-005, in Python).

This is the stand-in for the core's CapabilityInvokeService. The architectural point
of the composition is that NO plugin reaches another by name — every cross-axis call
goes `caller -> gateway -> provider`, resolved by capability URI. The gateway:

  1. RESOLVES the capability URI to a concrete provider method via a registry
     populated from each provider's (rat.common.v1.capability) method annotations —
     never a hard-coded plugin name (this is the same registry the real core builds).
  2. ENFORCES C5: the caller may invoke only capabilities its manifest `requires`.
  3. INVOKES the provider's gRPC method and relays the response.

It deliberately routes TYPED requests (not the opaque byte-relay the Go stub gateway
proved): the composition's subject under test is cross-AXIS data flow + capability
decoupling, and the byte-relay mechanics (passthrough codec, rat-callmeta-bin) are
already proven by plugins/state/inmemory-go/gateway_test.go + ADR-005/007/008.
"""

import grpc
from google.protobuf import descriptor_pb2

from rat.common.v1 import annotations_pb2  # noqa: F401 — registers the (capability) ext


def _capability_methods(service_descriptor):
    """Read capability_uri -> method_name from a service's (capability) annotations."""
    out = {}
    for method in service_descriptor.methods:
        opts = method.GetOptions()
        for field, value in opts.ListFields():
            if field.full_name == "rat.common.v1.capability":
                out[value] = method.name
    return out


class Gateway:
    def __init__(self) -> None:
        # capability_uri -> (stub, method_name)
        self._routes = {}

    def register(self, stub, service_descriptor) -> None:
        """Wire a provider in by reading its capability annotations (no plugin name)."""
        for cap, method_name in _capability_methods(service_descriptor).items():
            self._routes[cap] = (stub, method_name)

    def invoker_for(self, requires):
        """Return an invoke(capability, request) bound to a caller's `requires` set —
        the seam a plugin (e.g. the strategy) is handed. Enforces C5 deny-by-default."""
        allowed = set(requires)

        def invoke(capability, request):
            if capability not in allowed:
                raise PermissionError(
                    f"C5: caller not authorized for capability {capability!r}"
                )
            route = self._routes.get(capability)
            if route is None:
                raise grpc.RpcError(f"no provider for capability {capability!r}")
            stub, method_name = route
            return getattr(stub, method_name)(request)

        return invoke
