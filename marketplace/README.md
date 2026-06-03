# The RAT marketplace

A **marketplace** is a *source of plugin entries* — the `kind: marketplace` axis
([ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md)). `rat` reads several at
once and uses them to power `rat search`, `rat list`, and `rat add`'s dependency
auto-suggest / `--with-deps` auto-resolve.

## The two source kinds

| Kind | Where it comes from | How `rat` finds it |
|---|---|---|
| **local** | plugin images on this machine | their **stamped manifest** (ADR-026 OCI label `dev.rat.manifest.v1.b64`) — a `rat plugin pack`'d image *is* an entry, no index file needed |
| **added** | an index file **or an http(s) URL** | `rat marketplace add <name> <path-or-url>` → recorded in `~/.config/rat/marketplaces.json` |

## The official (remote) index

`official.json` in this directory is the reference index. It is meant to be **published as a
static file** (e.g. GitHub Pages for the `rat-dev` org) and added with no URL:

```bash
rat marketplace add official        # registers the built-in canonical URL
rat search                          # lists the official plugins (+ any local images)
```

The canonical URL is baked in as `officialIndexURL` in
[`core/cmd/rat/marketplace.go`](../core/cmd/rat/marketplace.go)
(`https://rat-dev.github.io/marketplace/official.json` — placeholder until the org's Pages
site is live). Adding your own remote index is the same command with an explicit URL:

```bash
rat marketplace add acme https://plugins.acme.example/index.json
```

### Remote behaviour

- **Timeout** — fetches are bounded (10 s) so an unreachable host can't wedge `rat search`.
- **Offline cache** — each fetched index is cached under `~/.cache/rat/marketplaces/<name>.json`;
  if a later fetch fails, `rat` uses the cached copy and prints a `⚠ … using cached copy` note.
- **Degraded sources are surfaced, not silently dropped** — a bad URL / malformed index warns
  on `rat search` rather than vanishing.

## Signing & provenance

A marketplace index can be **signed** so consumers verify it came from the publisher and
wasn't tampered with in transit (or in a compromised CDN/cache). RAT uses detached **ed25519**
signatures (the house signature algo) over the raw index bytes.

**Publisher** — sign the index, publish `index.json` *and* `index.json.sig` side by side:

```bash
rat marketplace keygen --out maintainer        # → maintainer.key (secret) + maintainer.pub
rat marketplace sign official.json --key maintainer.key   # → official.json.sig
# publish official.json + official.json.sig; share maintainer.pub
```

**Consumer** — pin the publisher's public key when adding the source; every fetch is then
verified (including the cached copy on offline fallback):

```bash
rat marketplace add official https://…/official.json --pubkey maintainer.pub
rat marketplace verify official      # on-demand re-check
rat search                           # TRUST column shows `signed✓`
```

- A pinned key with a **missing or invalid** signature is a **hard error** — the index is
  rejected, not used (a tampered index disappears from `search`, `verify` exits non-zero).
- `rat add --with-deps --require-signed` will **only** auto-pull providers from
  signature-verified sources; an unsigned provider is skipped and the dependency is reported
  unsatisfied.
- Sources are tagged in `rat marketplace list` (`🔑 signature-enforced`) and per-entry in
  `rat search` (`TRUST`: `signed✓` / `unsigned` / `local`).

> The built-in `official` source is **unsigned for now** (the canonical URL is a placeholder
> until the `rat-dev` org publishes it). Once published, its index will be signed and its
> public key pinned by default.

## Index format

```json
{
  "name": "rat-official",
  "description": "…",
  "plugins": [
    {
      "name": "rat-state",
      "kind": "state-backend",
      "image": "ghcr.io/rat-dev/state-postgres:1.0",
      "version": "1.0",
      "provides": ["rat://state/v1/get", "rat://state/v1/put", "rat://state/v1/list"],
      "requires": ["rat://secret/v1/resolve"],
      "description": "Postgres-backed state-backend (run history, project store)"
    }
  ]
}
```

`provides`/`requires` are capability URIs (`rat://<axis>/v<major>/<capability>`). They double
as a **dependency declaration**: `rat add --with-deps` synthesizes a plugin manifest straight
from an entry, so the index is enough to resolve a whole plane without pulling any image until
`rat up`.
