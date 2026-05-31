"""In-memory secret store — the data layer behind the `secret` reference.

Secrets are keyed by (tenant, secret_ref) so resolution is intrinsically
tenant-scoped. The CRITICAL property is anti-enumeration: a ref that belongs to
a DIFFERENT tenant, and a ref that does not exist at all, are INDISTINGUISHABLE
— both resolve to found=false with an empty value. See the proto's FOUND
SEMANTICS comment (reviews/06 API-1d / freeze-blocker #9): collapsing
"does-not-exist" with "exists-but-forbidden" is the whole point of the axis. A
distinguishable "exists-but-forbidden" would leak which refs are real.
"""

# TTL applied to every resolved value (unix millis hint; callers re-resolve past it).
TTL_MS = 300_000  # 5 minutes

# (tenant, secret_ref) -> value. Tenant is part of the key, so cross-tenant reads
# simply miss — they are not even reachable, let alone distinguishable.
SECRETS = {
    ("acme", "ref://db/password"): "s3cr3t",
    ("acme", "ref://api/key"): "key-123",
    ("globex", "ref://db/password"): "globex-pw",
}


class SecretStore:
    """Tenant-scoped, anti-enumeration in-memory secret resolver."""

    def resolve(self, tenant: str, secret_ref: str, now_ms: int):
        """Resolve (tenant, secret_ref) -> (found, value_bytes, expires_unix_ms).

        Anti-enumeration: a hit ONLY occurs when the (tenant, secret_ref) pair is
        present. A ref owned by another tenant, or an unknown ref, BOTH fall to
        the same (False, b"", 0) branch — no PERMISSION_DENIED, no distinguishable
        response.
        """
        value = SECRETS.get((tenant, secret_ref))
        if value is None:
            return (False, b"", 0)
        return (True, value.encode("utf-8"), now_ms + TTL_MS)
