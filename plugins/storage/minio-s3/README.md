# rat-storage-minio-s3 — the data-dev plane `storage` (remote S3/MinIO)

> 🛰️ **Exploratory** — part of the [data-dev plane experiment](../../../experiments/data-dev-plane/README.md).
> Additive: the frozen `storage/v1` surface is unchanged.

A `kind: storage` plugin that vends **short-TTL, tenant- + prefix-scoped** S3
credentials for a remote MinIO/S3 object store. The data plane (the DuckDB-ML engine)
uses those creds to read/write Parquet on S3 **directly** — the bytes never touch the
core (overview.md "data plane bypasses core"; the D3 cred-isolation point).

It is the **first reference to implement the Q02 5c read/write split** (ADR-017):

| capability | RPC | mode |
|---|---|---|
| `rat://storage/v1/vend-credentials` | `VendCredentials(prefix, mode)` | mode from the request (broad grant) |
| `rat://storage/v1/vend-credentials-read` | `VendReadCredentials(prefix)` | **READ**, fixed by the method (C5-authorizable) |
| `rat://storage/v1/vend-credentials-write` | `VendWriteCredentials(prefix)` | **WRITE**, fixed by the method |

Tenant comes from the `rat-callmeta-bin` metadata header (ADR-007) — never a request
field, so it can't be forged (the C7 enforcement property).

## Two minters (one plugin) — [`creds.py`](creds.py)

- **`ScopeReceiptMinter`** (default, no MinIO): echoes the granted scope
  `{tenant, prefix, mode, expires_unix_ms}` — what the
  [`storage-v1.json`](../../../contracts/conformance/storage-v1.json) golden vectors
  assert. This is what `make conformance` runs (offline, no boto3).
- **`MinioSTSMinter`** (real): calls MinIO STS **`AssumeRole`** with an **inline policy**
  scoped to `s3://<bucket>/<tenant>/<prefix>/*` for the mode, returning real temp creds.
  The blob also carries the S3 connection details so the engine can
  `CREATE SECRET … TYPE S3` directly. boto3 is imported lazily — only this path needs it.

**The scoping is real, not decorative.** The step-3 integration probe proved temp creds
for tenant `acme` can read `acme/*` and are **denied** `other/*` — a mis-scoped grant
cannot cross the tenant boundary.

## Configuration (real mode)

Set `MINIO_ENDPOINT` to switch from offline scope receipts to live STS:

| env | meaning |
|---|---|
| `MINIO_ENDPOINT` | MinIO S3 API `host:port` (e.g. `rat-minio:9000`) — presence enables STS mode |
| `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` | the admin creds the plugin mints scoped creds from |
| `RAT_S3_BUCKET` | bucket the lake lives in (default `rat`) |
| `MINIO_USE_SSL` | `true` for https (default `false`) · `MINIO_REGION` (default `us-east-1`) |
| `RAT_CRED_TTL_SECONDS` | short-TTL for vended creds (default `900`) |

## Run the conformance harness (containerized — no MinIO needed)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/storage/minio-s3 \
  python:3.12 bash -c 'pip install -q grpcio==1.80.0 protobuf==7.35.0 && python harness_test.py'
```

The real STS path is exercised end-to-end by the experiment's remote runner against the
live MinIO + Postgres compose stack (`make data-dev-remote`).
