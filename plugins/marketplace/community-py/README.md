# rat-marketplace-community-py — `marketplace` reference

A community `kind: marketplace` reference for `marketplace/v1`. It passes the
[conformance](../../../contracts/conformance/marketplace-v1.json) golden vectors:
an implementation that drives the same axis suite any other marketplace must.

A marketplace is a **discovery surface** for plugins — multiple coexist
(community-open, curated, enterprise-internal); the solo bundle ships a community
one. The two RPCs under test are `Search` and `Get`.

## What's special about this axis

Its one hard job is the **COMPATIBILITY question — "does this plugin work on MY
deployment?"** On a pluggable-everything platform that question is answerable
*because of the capability model*: every listing advertises its required +
provided capabilities, so `Search` can compute fit against what the caller's
deployment provides. A marketplace that can't answer "works on your deployment?"
has failed its one hard job. So the proto makes the capability sets, conformance,
and signature **mandatory** listing fields, not optional metadata.

This axis is deployment-scoped, not tenant-scoped — the deployment's capabilities
arrive as an explicit `deployment_capabilities` request field, so there is **no**
`rat-callmeta-bin` / identity handling here.

## Capabilities

| Capability | RPC | What it does |
|---|---|---|
| `rat://marketplace/v1/search` | `Search` | capability-aware discovery: filter by kind + free-text + the "works on my deployment?" compatibility filter |
| `rat://marketplace/v1/get` | `Get` | full detail for one listing (`NOT_FOUND` if unknown) |

## Mandatory listing fields

Every `Listing` populates these — they are what make capability-aware filtering
possible:

| Field | Why mandatory |
|---|---|
| `provided_capabilities` | what the plugin offers; drives "what can I add?" |
| `required_capabilities` | what the plugin needs; drives the "works on my deployment?" subset filter |
| `conformed_capabilities` | which capabilities passed their axis golden-data suite (empty = unverified, UI must surface that) |
| `signed` / `signed_by` | supply-chain trust |

The seeded catalog holds three real-ish RAT plugins
(`rat-engine-duckdb-py`, `rat-format-parquet-py`, `rat-strategy-scd2-py`), each
signed by `rat-dev` with its conformed set equal to its provided set.

## Files

| File | Role |
|---|---|
| `store.py` | `Marketplace`: seeded catalog + `search` (kind + query + compatibility filter) and `get` (`NOT_FOUND` if unknown) |
| `server.py` | `MarketplaceServicer`: `Search` → `SearchResponse`; `Get` → `GetResponse` / `abort` on `MarketplaceError` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/marketplace-v1.json` and drives this impl over real gRPC; asserts the compatibility filter, the mandatory fields, and `NOT_FOUND` |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/marketplace/community-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-marketplace-community-py conformed to marketplace/v1 golden vectors`.
