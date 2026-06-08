"""Env-backed secret store — the platform's secret-backend (ADR-022 §4 / Q4).

Loads its (tenant, secret_ref) -> value map from $RAT_SECRETS at boot: a JSON object
shaped { "<tenant>": { "<ref>": "<value>" } }. Same tenant-scoped, anti-enumeration
semantics as the conformance reference (plugins/secret/inmemory-py): an unknown ref AND a
cross-tenant ref BOTH resolve to (found=false, b"", 0) — never a distinguishable response
(reviews/06 API-1d). The secret VALUES live HERE, in one trust boundary, never in a consumer
plugin's manifest/env; consumers hold only an opaque ref and resolve it at point of use.

This env backend is the dev/solo source; production swaps it for Vault/KMS behind the SAME
frozen Resolve — consumers don't change. ($RAT_SECRETS still passes the value to THIS plugin
via its launch env; a file/Vault backend is the next step once LaunchSpec grows a mount/secret
channel — ADR-022 Q4. The win already banked: no consumer carries a raw credential.)
"""

import json
import os

# TTL applied to every resolved value (unix millis hint; callers re-resolve past it).
TTL_MS = 300_000  # 5 minutes


class EnvSecretStore:
    """Tenant-scoped, anti-enumeration secret resolver backed by $RAT_SECRETS."""

    def __init__(self, raw=None):
        raw = raw if raw is not None else os.environ.get("RAT_SECRETS", "{}")
        data = json.loads(raw or "{}")
        # (tenant, secret_ref) -> value. Tenant is part of the key, so cross-tenant
        # reads simply miss — not even reachable, let alone distinguishable.
        self._secrets = {}
        for tenant, refs in data.items():
            for ref, value in (refs or {}).items():
                self._secrets[(tenant, ref)] = value

    def resolve(self, tenant, secret_ref, now_ms):
        """Resolve (tenant, secret_ref) -> (found, value_bytes, expires_unix_ms).

        Anti-enumeration: a hit ONLY occurs when the (tenant, secret_ref) pair is present.
        A ref owned by another tenant, or an unknown ref, BOTH fall to (False, b"", 0).
        """
        value = self._secrets.get((tenant, secret_ref))
        if value is None:
            return (False, b"", 0)
        return (True, value.encode("utf-8"), now_ms + TTL_MS)
