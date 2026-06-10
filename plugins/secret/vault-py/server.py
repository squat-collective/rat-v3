"""The SecretService gRPC implementation — HashiCorp-Vault-backed (KV v2).

The production shape of the secret axis (DX-6): every Resolve fetches from Vault AT CALL
TIME, so rotating a secret in Vault is visible on the very next resolve — no plugin or
daemon restart (the env-py demo store reads its env once at boot and can't do this).

Path layout (KV v2): {RAT_VAULT_MOUNT}/data/{RAT_VAULT_PREFIX}/{tenant}/{ref-path}
  ref "ref://lake/pg-dsn" + tenant "acme" → GET /v1/secret/data/rat/acme/lake/pg-dsn
The secret's data holds the value under the key "value".

FOUND SEMANTICS (the axis contract, see secret/v1/CONTRACT.md): an unknown ref and a
cross-tenant ref BOTH return found=false + empty value — never PERMISSION_DENIED (the
anti-enumeration property). Vault 404 AND 403 therefore map to found=false (a 403 is
also what per-tenant Vault policies produce for cross-tenant probes); the 403 is logged
for the operator, who needs to know if it's a policy misconfig. A Vault that is DOWN is
not "not found" — connection errors and 5xx surface as UNAVAILABLE so consumers can tell
"no such secret" from "the secret backend is unreachable".

Env contract:
  RAT_VAULT_ADDR    e.g. http://vault:8200          (required)
  RAT_VAULT_TOKEN   the plugin's Vault token         (required; in production this is
                                                      the ONE secret the platform holds)
  RAT_VAULT_MOUNT   KV v2 mount (default "secret")
  RAT_VAULT_PREFIX  path prefix under the mount (default "rat")
"""

import json
import os
import sys
import urllib.error
import urllib.request

import grpc

from rat.common.v1 import context_pb2
from rat.secret.v1 import secret_pb2, secret_pb2_grpc


def _tenant_from_context(context) -> str:
    """The caller's tenant, from the rat-callmeta-bin metadata envelope (ADR-007).
    Empty (== single-tenant/solo) maps to the "default" path segment."""
    for key, val in context.invocation_metadata():
        if key == "rat-callmeta-bin":
            rc = context_pb2.RequestContext()
            rc.ParseFromString(val)
            return rc.identity.tenant
    return ""


class VaultSecretServicer(secret_pb2_grpc.SecretServiceServicer):
    def __init__(self) -> None:
        self.addr = os.environ["RAT_VAULT_ADDR"].rstrip("/")
        self.token = os.environ["RAT_VAULT_TOKEN"]
        self.mount = os.environ.get("RAT_VAULT_MOUNT", "secret")
        self.prefix = os.environ.get("RAT_VAULT_PREFIX", "rat")

    def _vault_get(self, path: str):
        """GET one KV v2 secret; returns (status, parsed-json-or-None)."""
        req = urllib.request.Request(
            f"{self.addr}/v1/{self.mount}/data/{path}",
            headers={"X-Vault-Token": self.token},
        )
        try:
            with urllib.request.urlopen(req, timeout=5) as resp:
                return resp.status, json.loads(resp.read())
        except urllib.error.HTTPError as e:
            return e.code, None

    def Resolve(self, request, context):
        tenant = _tenant_from_context(context) or "default"  # from metadata, never the body
        ref = request.secret_ref
        if not ref.startswith("ref://"):
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, f"secret_ref must be ref://…, got {ref!r}")
        path = f"{self.prefix}/{tenant}/{ref[len('ref://'):]}"

        try:
            status, body = self._vault_get(path)
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            # Vault unreachable ≠ secret absent: consumers must be able to tell.
            context.abort(grpc.StatusCode.UNAVAILABLE, f"vault unreachable: {e}")

        if status == 403:
            # Cross-tenant probes under per-tenant policies land here → found=false
            # (anti-enumeration). Logged because it is ALSO what a misconfigured token
            # produces — the operator needs the breadcrumb.
            print(f"vault 403 on {path} (cross-tenant probe, or the token lacks policy)",
                  file=sys.stderr, flush=True)
            return secret_pb2.ResolveResponse(found=False)
        if status == 404 or body is None:
            return secret_pb2.ResolveResponse(found=False)
        if status != 200:
            context.abort(grpc.StatusCode.UNAVAILABLE, f"vault returned {status} for {path}")

        value = (body.get("data", {}).get("data", {}) or {}).get("value", "")
        if value == "":
            return secret_pb2.ResolveResponse(found=False)
        return secret_pb2.ResolveResponse(found=True, value=value.encode(), expires_unix_ms=0)
