# Package managers — the LOCAL catalog problem

> A cross-system entry (Docker, containerd, npm, pip, Cargo, apt, Homebrew) focused on **one
> question**: how does a package manager answer *"what do I have / what's available, locally"*
> quickly and offline? This is the prior art for RAT v3's **local marketplace** — the source
> that's derived from the plugin images present on a machine ([ADR-028](../../docs/architecture/adrs/028-local-marketplace-cached-index.md)).

## The claim

Almost none of these tools have a "local marketplace." They have a **local artifact store**
plus a **locally-cached index**, kept as two separate things — and *listing is fast because
something keeps the index*, instead of re-deriving it from the artifacts on every command. The
ones that feel instant (Docker) have a **daemon that owns the index**; the ones that work
offline (Cargo, apt, npm, pip) keep a **cached copy of the index** refreshed on a delta.

RAT's local marketplace today is the outlier: it re-runs `podman images` **and** inspects every
image's stamped manifest on *every* `rat search` / `rat add --with-deps`. That's the pattern
every mature manager moved away from.

## How they do it

| Tool | Local artifacts | Local index / metadata | "what do I have" reads… | Why it's fast / offline |
|---|---|---|---|---|
| **Docker** | daemon image store (overlay2 / containerd, CAS by digest) | the **daemon's metadata DB** (boltdb), labels included | `docker images` → queries the **daemon** | a long-running daemon owns + indexes the store; layers are never re-read |
| **containerd** | content store (CAS by digest) | boltdb metadata store; OCI annotations queryable | the metadata store | indexed, not re-scanned |
| **npm** | `node_modules/` (per project) | `~/.npm/_cacache` — content-addressable cache of tarballs **and** registry metadata, keyed by integrity hash | `npm ls` reads the `node_modules` tree | cacache is a CAS+index; `--offline` works straight from it |
| **pip** | `site-packages/` | `~/.cache/pip` (HTTP + wheel cache); per-package metadata in `.dist-info/METADATA` | `pip list` reads `.dist-info` | metadata sits next to the install; no network |
| **Cargo** | `~/.cargo/registry/cache/*.crate` | `~/.cargo/registry/index` — a **local copy of the registry index**, refreshed incrementally (git → sparse HTTP) | the lockfile + the cached index | the index is cached locally; only the delta is fetched |
| **apt** | `/var/cache/apt/archives/*.deb` | `/var/lib/apt/lists` (cached `Packages` indexes from `apt update`); installed db in `/var/lib/dpkg/status` | `dpkg -l` (installed) vs `apt list` (cached index) | "installed" and "available" are *separate, cached* queries |
| **Homebrew** | bottle cache (`~/Library/Caches/Homebrew`) | a **git clone** of the tap — formulae as files = the local index | reading the tap files | the index is just local files |

Three mechanisms recur:

1. **Read metadata from the config, not the artifact.** Docker/containerd read **labels /
   annotations** straight from the image *config JSON* — one cheap read, no container start.
   (RAT already does this: `dev.rat.manifest.v1.b64` is an OCI label.)
2. **Cache the index; refresh the delta.** Cargo/apt/npm never recompute the catalog from the
   artifacts — they keep a local index and update only what changed (`apt update`, Cargo's
   sparse index, cacache by hash).
3. **A daemon can own the index.** Docker's `images` is instant because the daemon holds the
   indexed store in a metadata DB; the CLI just queries it. Daemonless tools
   (`skopeo`/`crane`) prove you can *also* read an image's config with no daemon — the right
   fallback.

A fourth, smaller pattern: **"installed" vs "available" are different queries**, both cached —
`dpkg -l` vs `apt list`, `npm ls` vs the registry. RAT already mirrors this with `rat list`
(installed in this project) vs `rat search` (available, local + remote).

## What we adopt

- **A cached local index, refreshed on a cheap delta** (Cargo/apt/npm). RAT's local source
  becomes a cache keyed by the set of plugin-image digests; it is rebuilt only for images that
  appeared/disappeared, not on every command. The expensive per-image manifest read happens
  once per digest, then is reused.
- **Label-read, not artifact-run** (Docker) — keep reading the stamped manifest from the OCI
  label; that part is already right, just memoize it by digest.
- **Daemon-owned index with a daemonless fallback** (Docker ⊕ skopeo). When a `rat` daemon is
  running it can own the local-plugin index (and dovetail with the live `ListPlugins`, ADR-027);
  with no daemon, the CLI does a direct cached scan. Same data, two access paths.
- **Keep "installed vs available" crisp** (apt) — `rat list` vs `rat search` stay distinct.

## What we don't adopt

- **A separate background indexer daemon** (Docker's `dockerd` model in full). RAT's local
  marketplace must work with **no daemon running** (a solo dev who just `rat plugin pack`'d an
  image and hasn't started a plane). So the daemon-owned index is an *optimization*, not a
  requirement — the daemonless cached scan is the floor.
- **A content-addressable blob store of our own** (npm cacache, containerd content store). We
  don't store artifacts — the OCI registry / local podman store already does. RAT's index holds
  **pointers + derived metadata**, never the image bytes (this is the marketplace's whole point;
  see `marketplace/README.md`).
- **A git-clone index** (Homebrew/old Cargo). Overkill for a local scan; the local source has no
  upstream to clone — it's *derived* from the machine's images. (The *remote* marketplace server,
  parked for later, is where a fetched/served index belongs.)
- **A lockfile for the local marketplace.** Lockfiles pin *resolution*; `rat.lock` already owns
  that. The local catalog is discovery, not resolution.

## References

- Docker / OCI image config & labels: <https://github.com/opencontainers/image-spec/blob/main/config.md> (`config.Labels`)
- containerd content + metadata stores: <https://github.com/containerd/containerd/blob/main/docs/content-flow.md>
- npm cacache (content-addressable cache): <https://github.com/npm/cacache>
- Cargo registry index (git → sparse HTTP): <https://doc.rust-lang.org/cargo/reference/registry-index.html>
- apt lists / dpkg status: `/var/lib/apt/lists`, `/var/lib/dpkg/status` (Debian policy §)
- skopeo (daemonless inspect): <https://github.com/containers/skopeo>
- RAT marketplace format + sources: [`marketplace/README.md`](../../marketplace/README.md)
