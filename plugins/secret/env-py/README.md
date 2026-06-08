# rat-secret-env-py — the platform's secret-backend (env-backed)

A `kind: secret-backend` plugin implementing the frozen `secret/v1` contract
(`rat://secret/v1/resolve`). It holds the platform's secrets in **one trust boundary** and
hands them out, tenant-scoped, on demand — so no consumer plugin carries a raw credential.

Distinct from the conformance reference [`plugins/secret/inmemory-py`](../inmemory-py/) (a
hardcoded golden map, used to drive the conformance vectors): this one loads its map from
`$RAT_SECRETS` so a real deployment supplies its own secrets.

## Contract behaviour (unchanged from the axis)

- **Tenant-scoped:** the caller's tenant comes from the `rat-callmeta-bin` metadata header
  (ADR-007), never a request field. `(tenant, secret_ref)` is the lookup key.
- **Anti-enumeration:** an unknown ref and a cross-tenant ref are **indistinguishable** —
  both return `found=false` + empty value, never `PERMISSION_DENIED` (reviews/06 API-1d).
- **TTL + redaction:** values come back with a 5-min expiry hint; `value` is `debug_redact`
  so reflection/text-marshal omit it. Callers re-resolve rather than persist.

## Config

`$RAT_SECRETS` — a JSON object `{ "<tenant>": { "<ref>": "<value>" } }`, e.g.:

```json
{ "acme": { "ref://state/pg-dsn": "host=… port=… dbname=… user=… password=…" } }
```

`$RAT_PLUGIN_ADDR` — the gRPC listen address (launch mode injects `0.0.0.0:50051`).

## In the platform (ADR-022)

`platform/plugins.yaml` launches this as `rat-secret`, and `rat-state` resolves its Postgres
DSN from `ref://state/pg-dsn` at first use (via the gateway → C5-authorized + audited) instead
of carrying the DSN in its own env. The secret value lives only on this plugin.

> Dev backend: the value still reaches this plugin via its launch env (`$RAT_SECRETS`).
> Production swaps the env store for Vault/KMS behind the SAME `Resolve` — consumers don't
> change. A file/secret launch channel is ADR-022 Q4 (the frozen `LaunchSpec` has env only).
