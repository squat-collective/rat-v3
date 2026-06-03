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
