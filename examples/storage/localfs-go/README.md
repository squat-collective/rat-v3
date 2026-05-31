# rat-storage-localfs-go — ROUND-2 `storage` reference (real backend)

The **round-2** `storage` reference: a *technologically-divergent* backend, not
another in-memory scope-receipt echo. Where [`inmemory-go`](../inmemory-go) /
[`inmemory-py`](../inmemory-py) just echo the requested prefix into a JSON receipt,
this vends credentials scoped to a **real local filesystem path** under a per-tenant
root — and **enforces containment**.

This is the [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
*spirit*: a backend with a genuinely different behavior that still passes the same
wire contract. It earns that by doing what the in-memory echo **cannot**:

| Round-2 property | Test | Why the in-memory echo can't show it |
|---|---|---|
| **Path containment** | `TestLocalFS_PathContainment` | a normal prefix resolves under `<root>/<tenant>/` and the dir is created on disk; an **escaping** prefix (`../../escape`) → `PERMISSION_DENIED` | it never touches a filesystem, so it just echoes any prefix back |
| **Tenant isolation** | `TestLocalFS_TenantIsolation` | two tenants vending the same logical prefix get **distinct** paths, each under its own tenant root | the echo has no tenant-rooted paths to keep apart |

Path containment is the storage analog of what sqlite gave `state` (durability +
linearizable CAS): the cross-tenant security boundary `storage.proto` emphasizes
(reviews/01 Finding 3, reviews/04), enforced for real by `filepath` resolution
rather than asserted by convention.

And it **passes the SAME shared golden vectors** (`contracts/conformance/storage-v1.json`,
now using provider-neutral logical prefixes) — the scope binding (tenant + prefix +
mode + short TTL), through the stub core gateway. The receipt carries an extra
`resolved_path` the vectors ignore.

## Files

| File | Role |
|---|---|
| `store.go` | the local-fs store: resolve prefix under the tenant root, enforce containment, `MkdirAll`, build the receipt (+ `resolved_path`) |
| `server.go` | `VendCredentials`; reads tenant from `rat-callmeta-bin`; empty prefix / unspecified mode → `INVALID_ARGUMENT` |
| `main.go` | gRPC server entrypoint (`$RAT_STORAGE_ROOT`, `$RAT_PLUGIN_ADDR`) |
| `harness_test.go` | shared-vector cross-run (via the gateway) + the two round-2 filesystem tests |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -w /work/examples/storage/localfs-go \
  golang:1.25 go test ./...
```
