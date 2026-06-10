# rat-secret-vault — the production secret-backend (Vault KV v2)

The production shape of the secret axis (backlog **DX-6**). Where the demo `env-py`
store reads `RAT_SECRETS` once at boot (rotation = restart everything), this plugin
fetches from HashiCorp Vault **on every `Resolve`** — rotate the secret in Vault and
the next resolve sees it. **No plugin restart, no daemon restart.**

## Env contract

| var | meaning |
|---|---|
| `RAT_VAULT_ADDR` | e.g. `http://vault:8200` (required) |
| `RAT_VAULT_TOKEN` | the plugin's Vault token (required — in production this is the *one* secret the platform holds; everything else lives in Vault) |
| `RAT_VAULT_MOUNT` | KV v2 mount (default `secret`) |
| `RAT_VAULT_PREFIX` | path prefix under the mount (default `rat`) |

Path layout: `ref://lake/pg-dsn` resolved by tenant `acme` reads
`{mount}/data/{prefix}/acme/lake/pg-dsn`, value under the data key `value`. The tenant
comes from the `rat-callmeta-bin` envelope (ADR-007), never from the request — empty
tenant maps to `default/`.

Axis semantics honored (see [`CONTRACT.md`](../../../contracts/proto/rat/secret/v1/CONTRACT.md)):
unknown ref AND cross-tenant ref → `found=false` (anti-enumeration; Vault 403s — what
per-tenant policies produce for cross-tenant probes — map there too, logged for the
operator). Vault **unreachable** is NOT "not found": it surfaces as `UNAVAILABLE`.

Driven with stdlib `urllib` — deliberately no Vault client library (one fewer
supply-chain edge on the platform's most sensitive plugin).

## Verifying it live (the DX-6 proof, run as written)

```sh
# a dev Vault + one secret
podman run -d --name vault --cap-add=IPC_LOCK -p 18200:8200 \
  -e VAULT_DEV_ROOT_TOKEN_ID=root -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  docker.io/hashicorp/vault:latest server -dev
podman exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root vault \
  vault kv put -mount=secret rat/acme/lake/pg-dsn value='host=pg password=OLD'

# the plugin (see main.py for a local run; or build the image:)
podman build -f plugins/secret/vault-py/Dockerfile -t rat/secret-vault:dev .

# resolve as tenant acme → found=true, password=OLD
# resolve as tenant evil → found=false        (anti-enumeration)
# then ROTATE:
podman exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root vault \
  vault kv put -mount=secret rat/acme/lake/pg-dsn value='host=pg password=NEW'
# resolve again WITHOUT restarting anything → password=NEW
```

(Executed end-to-end on 2026-06-10 — resolve, cross-tenant denial, unknown-ref denial,
and the rotation, all against one uninterrupted plugin process.)

## Why no `harness_test.py`

The conformance suite auto-discovers harnesses and runs them in a plain python container
— no Vault available there. Like `state/postgres-py` (the same production-backend
precedent), this reference verifies against its real backend instead of joining the
in-process matrix. The wire-semantics references for this axis remain
`secret/inmemory-py` (+ the golden vectors).

## Using it in a plane

```yaml
  - name: rat-secret
    manifest: ./manifests/secret.plugin.yaml      # provides rat://secret/v1/resolve
    launch:
      image: rat/secret-vault:dev
      isolation: i9
      env:
        RAT_VAULT_ADDR: "${PLATFORM_VAULT_ADDR}"   # ADR-050: facts from your .env
        RAT_VAULT_TOKEN: "${PLATFORM_VAULT_TOKEN}"
```
