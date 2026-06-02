"""Credential minters for rat-storage-minio-s3.

The storage axis vends short-TTL, **tenant- + prefix-scoped** credentials; the data
plane (the DuckDB-ML engine) then talks to object storage directly with them — the
control plane never sees bytes (overview.md "data plane bypasses core"). Tenant is
read from the request CONTEXT (ADR-007), never a request field, so it can't be forged
(the C7 enforcement point — reviews/01 Finding 3).

Two minters behind one interface so the same plugin serves both the offline
conformance harness and a real MinIO:

  * `ScopeReceiptMinter` — no MinIO needed. Returns the JSON "scope receipt"
    {tenant, prefix, mode, expires_unix_ms} the storage-v1 golden vectors assert. The
    default when no MINIO_ENDPOINT is configured (and what `make conformance` uses).
  * `MinioSTSMinter` — calls MinIO's STS `AssumeRole` with an **inline policy** scoped
    to `s3://<bucket>/<tenant>/<prefix>/*` for the requested mode, returning real
    short-TTL temp creds (key/secret/session-token). The blob ALSO carries the S3
    connection details so the engine can `CREATE SECRET … TYPE S3` directly. boto3 is
    imported lazily here so the offline path needs no AWS deps.

Both return `(blob: dict, expires_unix_ms: int)`; the server JSON-encodes the blob into
`VendCredentialsResponse.credentials` (opaque, `debug_redact` — never logged).
"""

import time

READ, WRITE, READ_WRITE = "READ", "WRITE", "READ_WRITE"
DEFAULT_TTL_SECONDS = 900  # 15 min — the documented short-TTL


def _now_ms():
    return int(time.time() * 1000)


class ScopeReceiptMinter:
    """Offline minter: echoes the granted scope. Stands in for an opaque STS token so
    the harness can assert the tenant/prefix/mode scoping obligation."""

    def __init__(self, ttl_seconds: int = DEFAULT_TTL_SECONDS, now_ms=_now_ms) -> None:
        self._ttl_ms = ttl_seconds * 1000
        self._now_ms = now_ms

    def mint(self, tenant: str, prefix: str, mode: str):
        expires = self._now_ms() + self._ttl_ms
        return {"tenant": tenant, "prefix": prefix, "mode": mode,
                "expires_unix_ms": expires}, expires


def _policy(bucket: str, scope_prefix: str, mode: str) -> dict:
    """An inline S3 policy scoped to bucket/<scope_prefix>/* for the mode. READ →
    GetObject+ListBucket; WRITE/READ_WRITE additionally Put/Delete (engines writing
    Parquet need list+get+put+delete)."""
    obj = f"arn:aws:s3:::{bucket}/{scope_prefix}/*"
    actions = ["s3:GetObject"]
    if mode in (WRITE, READ_WRITE):
        actions += ["s3:PutObject", "s3:DeleteObject"]
    return {
        "Version": "2012-10-17",
        "Statement": [
            {"Effect": "Allow", "Action": actions, "Resource": [obj]},
            {"Effect": "Allow", "Action": ["s3:ListBucket"],
             "Resource": [f"arn:aws:s3:::{bucket}"],
             "Condition": {"StringLike": {"s3:prefix": [f"{scope_prefix}/*"]}}},
        ],
    }


class MinioSTSMinter:
    """Real minter: MinIO STS AssumeRole with an inline tenant+prefix-scoped policy.

    `prefix` is the logical, provider-neutral path (e.g. "lake"); it resolves to
    `s3://<bucket>/<tenant>/<prefix>/*`, so a cred for tenant 'acme' physically cannot
    touch another tenant's data (proven in the step-3 probe: read acme/* OK, other/*
    denied)."""

    def __init__(self, *, endpoint: str, bucket: str, admin_key: str, admin_secret: str,
                 region: str = "us-east-1", use_ssl: bool = False,
                 ttl_seconds: int = DEFAULT_TTL_SECONDS) -> None:
        import boto3  # lazy — only the real path needs AWS deps
        self._bucket = bucket
        self._region = region
        self._use_ssl = use_ssl
        self._ttl = ttl_seconds
        scheme = "https" if use_ssl else "http"
        self._endpoint = endpoint
        self._endpoint_url = f"{scheme}://{endpoint}"
        self._sts = boto3.client("sts", endpoint_url=self._endpoint_url,
                                 aws_access_key_id=admin_key, aws_secret_access_key=admin_secret,
                                 region_name=region)

    def mint(self, tenant: str, prefix: str, mode: str):
        import json
        scope_prefix = "/".join(p for p in (tenant, prefix) if p)
        out = self._sts.assume_role(
            RoleArn="arn:rat:iam::scoped:role/data-plane",  # MinIO ignores the arn; required by the API
            RoleSessionName=f"rat-{tenant or 'solo'}",
            Policy=json.dumps(_policy(self._bucket, scope_prefix, mode)),
            DurationSeconds=self._ttl,
        )
        c = out["Credentials"]
        expires = int(c["Expiration"].timestamp() * 1000)
        blob = {
            "tenant": tenant, "prefix": prefix, "mode": mode, "expires_unix_ms": expires,
            # everything the engine needs for CREATE SECRET … TYPE S3:
            "s3": {
                "endpoint": self._endpoint, "region": self._region, "url_style": "path",
                "use_ssl": self._use_ssl, "bucket": self._bucket,
                "scope_prefix": scope_prefix,
                "key_id": c["AccessKeyId"], "secret": c["SecretAccessKey"],
                "session_token": c["SessionToken"],
            },
        }
        return blob, expires
