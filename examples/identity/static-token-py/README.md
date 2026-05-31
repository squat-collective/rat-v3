# rat-identity-static-token-py — `identity` reference (static-token, the C2 default)

A `kind: identity` reference. It backs the core's **Identity Gateway** (one of the
six core things): every request the API gateway accepts is authenticated here
before routing, and coarse-authorized here before the action proceeds.

This reference is the **C2 default** — a *static-token* model, deliberately **NOT
anonymous-root** (reviews/04). Tokens map to subjects; subjects carry roles;
actions require roles.

## Capabilities

| Capability | RPC | Behavior |
|---|---|---|
| `rat://identity/v1/authenticate` | `Authenticate` | validate an opaque credential → `(authenticated, subject, tenant)`. Constant-time compare (`hmac.compare_digest`) so a bad token is timing-indistinguishable from a good one. |
| `rat://identity/v1/authorize` | `Authorize` | coarse allow/deny for `(subject, action, resource)`. A deny is a **successful** rpc carrying a machine-readable `deny_code`. |

## The static-token model

```
tok-acme-admin  → alice @ acme, roles: admin, runner
tok-acme-viewer → bob   @ acme, roles: viewer

pipeline.run requires: runner
plane.create requires: admin
```

`Authenticate` takes the opaque credential bytes (`debug_redact` in the proto — never
logged) and resolves the subject + tenant. `Authorize` reads the **subject from the
`rat-callmeta-bin` metadata header** ([ADR-007](../../../docs/architecture/adrs/007-call-context-transport.md))
— the core stamps the core-signed `SubjectAssertion` on the hop; it is **never** a
request field — and decides on the subject's roles. Outcomes:

| outcome | `deny_code` |
|---|---|
| allowed | `DENY_CODE_UNSPECIFIED` |
| empty / unknown subject | `DENY_CODE_NOT_AUTHENTICATED` |
| unknown action | `DENY_CODE_ACTION_FORBIDDEN` |
| known action, missing role | `DENY_CODE_INSUFFICIENT_ROLE` |

Callers branch on `deny_code`, never on the free-text `reason` (anti-enumeration-oracle,
per the proto ERROR MODEL).

## Files

| File | Role |
|---|---|
| `store.py` | `StaticTokenIdentity`: constant-time `authenticate` + role-based `authorize` (pure logic, no gRPC) |
| `server.py` | `IdentityServicer`; `Authorize` reads the subject from `rat-callmeta-bin` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/identity-v1.json` and drives this impl over real gRPC; asserts authenticate + authorize outcomes |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/identity/static-token-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-identity-static-token-py conformed to identity/v1 golden vectors`.
