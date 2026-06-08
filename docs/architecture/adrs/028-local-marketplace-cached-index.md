# ADR-028: The local marketplace as a cached, digest-keyed index

## Status: Proposed (2026-06-04)

## Context

The marketplace ([Phase 8](../../../roadmap/done.md), `marketplace.go`) reads from two source
kinds: **added** (index files / remote URLs, already cached under `~/.cache/rat/marketplaces/`
and signature-verified) and **local** — the plugin images present on this machine, discovered
by their stamped manifest (the OCI label `dev.rat.manifest.v1.b64`, ADR-026).

The local source is the outlier. `localEntries()` does this on **every** `rat search`, every
`rat add --with-deps`, and every `reportUnsatisfiedSuggesting`:

```
podman images --filter label=dev.rat.manifest.v1.b64 ...   # list the candidate images
for each image:  readStampedManifest(ref)                  # a podman inspect / skopeo PER image
```

So the expensive per-image manifest read is repeated on every command, scaling with the number
of local plugin images. With a handful it's fine; with many it's sluggish, and it is pure
recomputation — the images haven't changed between two `rat search`es a second apart.

The prior art ([research/prior-art/package-manager-local-catalogs.md](../../research/prior-art/package-manager-local-catalogs.md))
is unanimous: package managers keep the **local artifact store** and a **cached index** as two
separate things, and listing is fast because *something keeps the index* — a daemon owns it
(Docker), or it is cached and delta-refreshed (Cargo / apt / npm). RAT's remote source already
does this; the local source must too.

Two RAT-specific facts shape the fix:
- We already read metadata from the **OCI label**, not by running the image (the Docker-correct
  approach). The waste is only that we re-read it every command.
- The local catalog must work with **no daemon running** — a solo dev who just `rat plugin
  pack`'d an image and hasn't started a plane (`rat serve`/`rat up`). So a background indexer
  daemon cannot be the *floor*.

## Decision

Make the local marketplace a **cached index keyed by image content digest**, refreshed on a
cheap delta, with the daemonless CLI scan as the baseline and a daemon-owned variant deferred.

### 1. A digest-keyed local cache

Cache file `~/.cache/rat/local/index.json` (mirroring the remote `~/.cache/rat/marketplaces/`),
holding `{ imageID → marketEntry }`. `imageID` is the image's **content ID** (the config
digest podman/docker report as `{{.ID}}`), which changes whenever the image content changes —
including a moving tag like `:dev` rebuilt to new bytes. So the cache is **content-exact**:
no time-based staleness window (the reason the *remote* cache uses a TTL — it can't cheaply
know if the upstream changed — does not apply locally, where listing digests is cheap).

### 2. Delta refresh — read each manifest at most once per content

`localEntries()` becomes:

```
1. CHEAP list:  podman images --filter label=… --format '{{.ID}} {{.Repository}}:{{.Tag}}'
                → the current set of (imageID, ref)
2. for each imageID already in the cache:   reuse the cached entry
   (refresh only its display ref if the tag moved — no manifest read)
3. for each NEW imageID:   readStampedManifest(ref) ONCE → cache it
4. DROP cache entries whose imageID is no longer present
5. atomic write-back (temp + rename — concurrent `rat` invocations are safe)
```

The one expensive operation (`readStampedManifest`) runs only for images whose *content* the
cache hasn't seen. Steady state (no new images) = one cheap `podman images` call, zero inspects.

### 3. Daemonless floor + a force-refresh escape hatch

The CLI does the cached scan itself, so the local marketplace works with **no daemon**. A
`--refresh` flag on `rat search` (and an honored `RAT_NO_CACHE`) forces a full rescan, for the
rare case the cache is suspected wrong.

### 4. podman → docker fallback

The cheap list uses podman, falling back to docker (the project's standard runtime fallback),
so the local marketplace works on a docker-only host. (Only the *list* call needs the fork; the
label is read the same way from either.)

### 5. Correctness is non-negotiable

The cache may never make `search` / `--with-deps` show a stale or wrong entry. Digest-keying
guarantees this: a changed image ⇒ a new `imageID` ⇒ a cache miss ⇒ a fresh read; a removed
image ⇒ its entry is dropped. There is no "entry for content we no longer have."

## Consequences

**Positive.**
- Repeated `rat search` / `add --with-deps` / auto-suggest go from *O(N images) inspects per
  command* to *one cheap list call* in steady state. The authoring↔discovery loop (pack →
  search) stays snappy as a developer accumulates plugins.
- Offline-friendly and daemon-free — the floor still works for a solo dev with no plane.
- Reuses the existing label-read; symmetric with the remote cache (`~/.cache/rat/...`).

**Negative — accepted.**
- A cache to invalidate — but digest-keying removes the classic staleness risk (content-exact),
  and `--refresh` is the escape hatch.
- One more per-user cache file under `~/.cache/rat/`. Acceptable; it's the same shape as the
  remote-index cache already there.
- Still forks `podman`/`docker` for the cheap list (unavoidable without a daemon) — but it's a
  single `images` call, not N `inspect`s.

**Neutral.**
- Introduces a local cache alongside the remote one; the two are deliberately symmetric in
  location + atomic-write discipline.

## Open questions

- **Q01 — Daemon-owned local index.** When a `rat` daemon is running it could own the
  local-plugin index in memory (dovetailing with the live `ListPlugins`, ADR-027) and serve it
  over a control RPC, so the CLI skips even the cheap list. Deferred: the cached file already
  makes the common path fast; the daemon-owned path is an optimization, not the floor.
- **Q02 — Surface "near-miss" images.** Images you *built* but didn't `rat plugin pack` (so
  they're unstamped) are invisible. A future `rat marketplace local` could nudge "3 local images
  look like plugins but aren't packed." Separate feature; not this ADR.
- **Q03 — Watch vs scan-on-demand.** Inotify on the image store to invalidate the cache the
  instant an image changes, vs the scan-on-demand here. Scan-on-demand is sufficient (the cheap
  list *is* the invalidation check); watching is a later optimization.
- **Q04 — A curated personal local catalog.** Letting a dev maintain their own local file-backed
  set beyond the auto-scan. Separate feature.

## Alternatives considered

- **Status quo (rescan every command).** Rejected — it is the problem.
- **TTL-based local cache** (time-expiry, like the remote HTTP cache). Rejected: locally we can
  *cheaply* list digests, so we get content-exact invalidation with no staleness window — strictly
  better than a TTL guess. (The remote cache uses a TTL only because it cannot cheaply tell if the
  upstream changed.)
- **Batch the inspect** (one `podman inspect` over all images instead of N). A partial win, but
  still O(N) work every command and still no reuse across commands. Rejected as insufficient.
- **An always-on background indexer daemon** (full Docker model). Rejected: the local marketplace
  must work with no daemon (the solo-dev floor); a mandatory indexer over-engineers it. The
  daemon-owned index is kept as an *optional* layer (Q01), never the baseline.

## Migration

Additive; **Phase 10**, CLI-side only (no proto change, no daemon change for v1). Step 1 is this
ADR. Then: **(2)** digest-keyed cache in `localEntries` (delta refresh + atomic write + `--refresh`
/ `RAT_NO_CACHE` + docker fallback); **(3)** tests — cache-hit reuse (no re-read), digest-change
invalidation, dropped-on-removal, atomic concurrent write; **(4)** prove — timing before/after on
a multi-image machine + a correctness check that a rebuilt image is re-read. `make breaking` stays
clean (no contract touched).

## Related

- [ADR-026](026-plugin-authoring-and-packaging.md) — the stamped-manifest OCI label the local
  source reads.
- [ADR-027](027-live-plugin-control-rpc.md) — the live daemon + `ListPlugins`; the home of the
  deferred daemon-owned local index (Q01).
- [research/prior-art/package-manager-local-catalogs.md](../../research/prior-art/package-manager-local-catalogs.md)
  — the Docker / Cargo / apt / npm prior art this applies.
- [`marketplace/README.md`](../../../marketplace/README.md) — the marketplace format + the local vs
  added source distinction.
