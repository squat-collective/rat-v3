# Done — completed work log

Reverse chronological. Each entry: date, what was accomplished, links to artifacts (commits, files, ADRs).

---

## 📿 Six-thing-core temptation ledger (standing — not a chronological entry)

> **CLAUDE.md #2** / [`.claude/rules/plugin-architecture.md`](../.claude/rules/plugin-architecture.md): every time we're tempted to add a **7th core responsibility**, log it here with the verdict. **Three in a quarter = an early warning** the premise needs revisiting. Started 2026-06-01 (reviews/08 **E7** — the discipline was held ad hoc but never *recorded*; the ledger now exists even at count 0).

**Temptations logged since 2026-06-01: `0`** (two *examined* and resolved as *not* 7th things — ADR-027 live control, 2026-06-03, and ADR-036 reconciler-hosts-operators, 2026-06-04; see the table rows). The post-freeze board review ([reviews/08](../reviews/08-post-freeze-board-review.md) architect #1, #6) independently confirmed the six-thing discipline **held** through the entire Phase-0 contract surface. The recurring "cross-cutting concerns" (trace propagation, plugin↔core auth, state-gateway isolation, mandatory audit, native observability) were resolved as **correctness conditions of the existing six**, not as new core responsibilities (see plugin-architecture.md "Cross-plugin concerns" + reviews/00 C1–C10). Items 1–2 of the Phase-0 close-out (catalog commit-linkage, manifest freeze) added *plugin-axis* + *manifest* surface only — no core temptation.

| date | the thing we wanted to put in core | chicken-and-egg proof attempted? | verdict |
|---|---|---|---|
| 2026-06-03 | a **live control plane** (register/deregister plugins at runtime, [ADR-027](../docs/architecture/adrs/027-live-plugin-control-rpc.md)) | n/a — examined whether it's a 7th responsibility | **Not a temptation.** It *extends two existing* responsibilities — mutating the **Registry** + an admin RPC on the **API gateway** — and reuses the **Reconciler**'s unchanged convergence (only its desired *input* became mutable). No new core responsibility; the count stays **6**. |
| 2026-06-04 | the **reconciler hosting domain operators** for declarative pipeline/data automation ([ADR-036](../docs/architecture/adrs/036-reconciler-hosts-operators.md), Proposed) | **yes** — would automation require the core to know what a pipeline/table is? | **Not a temptation.** It *generalizes the Reconciler's existing job* (drive convergence) from one kind (plugins) to many, using **only existing core pieces** (registry · state-watch · event bus · API gateway · state-CAS lease). All domain logic lives in **operator plugins** (`operator/v1` axis); the plugin-set loop becomes the built-in tier-0 "deployment operator". Chicken-and-egg confirms the reconciler can't itself be a plugin, while domain operators can. Count stays **6**. |

---

## 2026-06-08 — Professionalization restructure (steps 1–4) + Phase-10 consolidation

On `phase-10` (11 commits, `49d08bd`→roadmap). **Goal:** reduce the repo to the essential + a
professional structure (audit + locked target in [`docs/restructure/`](../docs/restructure/)).

- **Phase 10 consolidated** — merged `phase-10-workspace-federation` (33 commits, ADRs 029–036)
  + `phase-10-adr-028-local-catalog` into the `phase-10` integration branch (`main` untouched).
- **Step 1 — hygiene.** `make clean` target (reclaimed ~105M of gitignored build output: 180M→75M);
  ADR **status sweep** (021–026 Proposed→Accepted — code shipped; left 017/028/036/032 honestly
  open); **roadmap refresh** (current.md was 2 phases stale; phases.md table extended to Phase 10).
- **Step 2 — cuts.** [ADR-037](../docs/architecture/adrs/037-trim-committed-sdks-to-consumed-languages.md): dropped the **TS + Rust SDKs** (zero consumers) + codegen wiring + dead
  `buf.gen.python.yaml` (89 files / ~17.2k lines). Removed the **superseded** `sql-pipeline-py` +
  `platform/project/` + `pipelines/` (ADR-021's dbt-runner replaced them).
- **Step 3 — archive.** The Q02 *simulated*-review kit + `reviews/board/` raw outputs → `reviews/archive/` (12 files); every inbound link repointed.
- **Step 4 — moves.** `research/`→`docs/research/`; **`examples/`→`plugins/`** ([ADR-038](../docs/architecture/adrs/038-reference-plugins-live-under-plugins.md), 343 files,
  `git mv` history-preserved, 8 Go module paths `rat/examples/`→`rat/plugins/`). The separate
  `contracts/examples/` manifest-vector dir was preserved (reverted over-eager rewrites).
- **Verified:** 8/8 Go plugin modules go-build clean under new module paths; `core/` build+vet clean;
  buf lint clean; a new repo-wide markdown link verifier (~1400 links) found + fixed **20
  pre-existing broken links** and now reports **0 broken**.
- **Paused for sign-off:** step 5 — graduate `experiments/data-dev-plane/` + 5 exploratory plugins
  (incl. `vscode-rat`) + `data-dev-*` scripts to a separate `rat-data-dev` repo.

The top-level tree now reads as the architecture: `contracts/` · `core/` · `plugins/` · `platform/`
· `marketplace/` · `docs/` · `reviews/` · `roadmap/` · `ideas/` · `scripts/`.

---

## 2026-06-04 — RatFS: code-fs as a native VS Code folder (`plugins/ui/vscode-rat`)

Commit `1c66b17` on `phase-10-workspace-federation`. A `vscode.FileSystemProvider` for the `rat://<connection>/<path>` scheme, backed by the frozen **state axis through the federation hub** ([ADR-033](../docs/architecture/adrs/033-workspace-federation-hub.md)/[034](../docs/architecture/adrs/034-security-responsibility-model.md)): `readFile→state/get`, `writeFile→state/put` (CAS conflict surfaced as a write error), `readDirectory/stat→state/list`. Registered, the **Explorer/editor/save/search work natively on `rat://` URIs** — code-fs becomes an editable remote folder, **authenticated + multi-workspace + collaborative**. Transport: shells the proven `rat call` path (TLS `--cacert` · auth `--token` · routing `--workspace`) — reuses the verified CLI rather than re-implementing gRPC + binary call-context metadata in Node (like the Git extension shelling `git`); native Connect-ES transport is a future refinement. Extended `RatConnection` (hub/workspace/token/cacert/caller), added the *Open code-fs Folder* command + `onFileSystem:rat` activation. **Compiles (tsc) clean; backend verified live** — `readDirectory`/`readFile` against the secured hub (kitchen) return the code-fs tree + file bytes. v0.2.1→0.3.0.

---

## 2026-06-04 — Identity at the edge: `identity-token` plugin + hub TLS + secure-by-default guardrail ([ADR-034](../docs/architecture/adrs/034-security-responsibility-model.md))

Built ADR-034 Q01/Q02 + [ADR-033](../docs/architecture/adrs/033-workspace-federation-hub.md) Q03 on `phase-10-workspace-federation` (commit `2ece0c4`):

- **`identity-token` reference plugin** (in `~/sandbox/kitchen`) — the ADR-034 Q01 reference: implements the **frozen** `rat/identity/v1 Authenticate`, mapping a bearer token → `{subject, tenant}` (env-backed `RAT_TOKENS`; swap for OIDC/SPIFFE behind the same method). Packed `localhost/rat/identity-token:0.1.0`.
- **`rat hub` secure-by-default binding guardrail** (ADR-034 §4): a **public** (non-loopback) bind **refuses** unless `--tls-cert/--tls-key` **and** `--identity` are set — or an explicit `--insecure` (loud-warned); a **localhost** bind stays open (loopback trust). `isPublicAddr()`.
- **TLS** serving on the hub + **edge authentication**: with `--identity <addr>`, every `Invoke` must carry a valid `rat-token`; the hub calls the identity plugin's `Authenticate` and rejects `Unauthenticated` **before forwarding** — closing the trust-asserted `--as` gap *at the edge*.
- **client**: `rat call --token` (bearer), `--cacert` / `--tls-skip-verify` (reach a TLS hub). **descriptors**: the frozen identity axis wired into `routableDescriptors`.
- **Proven:** public bind refused w/o TLS+identity; the secure hub authenticates `subject=alice tenant=acme` + forwards to kitchen; **bad/missing token → `Unauthenticated`**; plaintext→TLS-hub → handshake fails; localhost bind stays plaintext/open. No proto change, `breaking` + vet clean.
- **Still owed** (follow-on): **gateway-level** identity enforcement for *direct* (non-hub) access — today the per-plane gateway still trusts the wire `--as`; the hub closes it at the federated edge. Plus subject-stamping onto the forwarded envelope.

---

## 2026-06-04 — Workspace federation: the `rat hub` ([ADR-033](../docs/architecture/adrs/033-workspace-federation-hub.md)) + remote-access architecture

Dogfooding session ("build a rat platform from scratch") that surfaced + built real work, on `phase-10-workspace-federation`:

- **`rat hub` — workspace federation front door** ([ADR-033](../docs/architecture/adrs/033-workspace-federation-hub.md), Accepted; commits `1baeec8` ADR, `56e9aa0` impl). A *gateway-of-gateways*: serves `CapabilityInvokeService` as a generic relay, reads a `rat-workspace` selector from metadata, forwards the opaque `InvokeRequest` to the named workspace's daemon **preserving the `rat-callmeta-bin` envelope** (each workspace runs its own C5). Auto-discovers running workspaces from the instance registry (same source as `rat ls`); `rat call --workspace <name> --addr <hub>`. **No proto change** (reuses the frozen invoke contract), `make breaking` + vet clean. **Proven:** one hub federating **kitchen** (code-fs/minio) + **pantry** (state-sqlite) — distinct backends + data + the NOT_FOUND path. First cut = unary Invoke + local discovery; **NATS-leaf cross-machine, transparent stream/control proxy, identity-at-hub+TLS are Q01–Q03**.
- **The security responsibility model** the hub sits on — now **[ADR-034](../docs/architecture/adrs/034-security-responsibility-model.md)** (Accepted, commit `8d98a56`): the three-ring shared-responsibility split (🌍 environment = perimeter · 🔌 plugins = identity/secret/audit/tenancy/connectivity · ⚙️ core = identity-required + C5 + audit), the irreducible core minimum, the secure-by-default binding posture, defense-in-depth. *Authentication is delegated, authorization is owned, the perimeter is environmental.* The `connectivity` axis (reverse-tunnel/mesh/**nats-leaf**) was demoted from a security mechanism to an optional convenience.
- **Kitchen dogfooding (in `~/sandbox/kitchen`, the user's platform — not this repo):** built **`code-fs`**, a pure-plugin collaborative remote code filesystem (PROVIDES the frozen `state/v1` caps, REQUIRES `storage/v1/vend` → any storage backend; no new proto, no core change) — and **deferred the `fs` axis** ([ADR-032](../docs/architecture/adrs/032-filesystem-axis.md) → Deferred, commit `ae9e2a3`) because a new axis needs a proto **and** a core recompile (the hardcoded `routableDescriptors()` — see the **dynamic-descriptor gap**, [`ideas/inbox.md`](../ideas/inbox.md)). Proven: put/get/list round-trip, real S3 objects in minio, CAS conflict detection, and a live **keyring→secret-vault** secret-backend swap (Vault audit-confirmed) with zero change to consumers.

---

## 2026-06-04 — 🎉 PHASE 9 SEALED — `rat/6.0` (live plugin control)

Phase 9 — **live plugin control** ([ADR-027](../docs/architecture/adrs/027-live-plugin-control-rpc.md)) — is **sealed at `rat/6.0`** (`phase-9` merged to `main`, annotated tag). The first **major** bump since `rat/2.0`: the contract surface grew a new core↔operator service (`ControlService`) — additive, but a genuine new wire. `rat add`/`remove` now materialize against a **running** daemon (no restart); the registry + reconciler desired-set became mutable while the six-thing core held (the live control plane *extends* the Registry + API gateway, it is not a 7th thing — see the temptation ledger). The line: `rat/2.0` core · `rat/2.5`–`5.9` (platform · surfaces · distribution · authoring · authoring↔runtime · dependency resolution · marketplace ×4 · project lifecycle) · **`rat/6.0` live control**. Detail of the 6-slice build below.

---

## 2026-06-03 — Phase 9 BUILT: live plugin control — the `ControlService` RPC ⚡ ([ADR-027](../docs/architecture/adrs/027-live-plugin-control-rpc.md))

The big one: `rat add`/`remove` now materialize against a **running** daemon — a plugin joins/leaves the live plane with **no restart** — the reconcile-to-desired-state premise finally exercised at runtime. [ADR-027](../docs/architecture/adrs/027-live-plugin-control-rpc.md) (**Accepted**), chosen via a 3-way design fork (dedicated `ControlService` gRPC over control-as-capability / SIGHUP-reload). Built in 6 slices on `phase-9-adr-027-live-control`:

1. **ADR-027** — the decision (operator-action control, *separate* from C5 capability-invocation; stays within the six-thing core — extends Registry + API gateway, reconciler convergence unchanged; no temptation logged).
2. **`contracts/proto/rat/core/v1/control.proto`** — `RegisterPlugin`/`DeregisterPlugin`/`ListPlugins`; manifest rides as raw YAML bytes (no proto schema dup) + a `LaunchSpec`. Additive (`buf lint`/`breaking` clean); SDKs regenerated for all four languages, Go SDK compiles.
3. **Mutable core** — `registry.Register`/`Deregister` (RWMutex, atomic insert keeping the no-dup invariants at runtime) + `reconciler.AddDesired`/`RemoveDesired` (the convergence path unchanged; only the desired *input* is now mutable). `-race` clean.
4. **`controlService`** (`core/cmd/rat/control.go`) — served on the control listener alongside the invoke gateway (launch mode only). RegisterPlugin validates → registers → adds desired → waits (bounded) for Healthy → **rolls back** on failure (no partial state). Idempotent re-register; driver (no-image) path; ListPlugins joins registry + reconcile state.
5. **Daemon-aware CLI** — `rat add`/`remove` dial the control socket and materialize live when a daemon is up (`projectDaemonAddr`/`dialControl`/`materializeAdd`/`materializeRemove`), else stay declarative; `--no-live` escape. `rat.toml` stays the source of truth (written first; the RPC applies the diff).
6. **Proofs** — deterministic `control_test.go` (incl. the **real `reconciler.Loop` + `lease.Elector`**, fake runtime): register→launch→Healthy→authorizable→listed, idempotent, rollback-on-never-healthy; `serve_control_test.go`: a **real `rat serve` daemon** driven over the actual socket — a live RegisterPlugin **flips the gateway's C5 decision** (PermissionDenied → routed → PermissionDenied after Deregister) with no restart; ListPlugins reflects the live plane. `make breaking` clean throughout.

*Note:* the real-process-LAUNCH smoke tests (`TestServe`, `TestReconcilerDrivesRealRuntime`) are starved/flaky under this sandbox's host load — **confirmed failing on the clean `rat/5.9` tree, independent of ADR-027**; the launch path is proven via the deterministic real-loop test instead. **Open (ADR-027 Q01–Q04):** operator identity + mTLS for TCP control; atomic multi-plugin `ApplyPlane`; Deregister cascade-on-dependents; hot `UpdatePlugin`. 🎉 **SEALED `rat/6.0`** (see the seal entry above).

---

## 2026-06-03 — 🎉 PHASE 8 RE-SEALED — `rat/5.9` (project lifecycle: `rat remove`)

The marketplace/project phase iterates to **`rat/5.9`** (`phase-8` merged to `main` again, annotated tag) — slice 5, **`rat remove`** (alias `rm`): the symmetric inverse of `rat add` — text-level block strip (comments + siblings preserved), rat-managed manifest cleanup (`--keep-manifest` to skip), and the resolver re-run that surfaces a now-unsatisfied `requires`. The sealed line is now `… rat/5.5` (discover) · `rat/5.6` (auto-resolve) · `rat/5.7` (remote index) · `rat/5.8` (signed entries) · `rat/5.9` (`rat remove`). Additive — `make breaking` clean, no proto/axis change. **Open follow-ons:** `rat add`/`remove` *materializing* into a live daemon (the RegisterPlugin/DeregisterPlugin RPC, ADR-023 — today both are declarative, `rat up` applies); publish + sign `official.json` on the `rat-dev` Pages site (URL + key are placeholders).

---

## 2026-06-03 — Phase 8 slice 5: `rat remove` — the inverse of `rat add` ➖

Closed the project lifecycle's missing half. `rat remove <name>` (alias `rat rm`) strips the named `[[plugin]]` block from `rat.toml` at the **text level** (so the file's header comments + the sibling blocks — incl. a `[plugin.env]` sub-table — survive verbatim; the inverse of `rat add`'s append), deletes the **rat-managed** manifest under `manifests/` (only files rat wrote — a user-supplied `--manifest` elsewhere is left alone; `--keep-manifest` skips even the managed one), and — symmetry with add — re-runs the resolver so a now-unsatisfied `requires` surfaces. **Proven live**: build a 4-plugin plane via `--with-deps`, `rat remove rat-state` (a *provider*) → block + managed manifest gone, resolver re-warns that `my-scheduler`/`dbt-runner` now lack `state/put`/`state/get` (with a re-add suggestion), `rat list` drops to 3; `--keep-manifest` keeps the file; header comments preserved; removing an absent plugin errors (exit 1). Unit test `TestRemovePluginBlock` (sibling + `[plugin.env]` + comments preserved, re-parses, absent→error). New: `runRemove`/`removePluginBlock`/`managedManifest` (project.go), `remove`/`rm` dispatch (main.go), `project_remove_test.go`. Additive — `make breaking` clean, `make core-test` green, no proto change.

---

## 2026-06-03 — 🎉 PHASE 8 RE-SEALED — `rat/5.8` (marketplace iteration 4: signed entries)

The marketplace phase iterates to **`rat/5.8`** (`phase-8` merged to `main` again, annotated tag) — slice 4, **signed entries**: detached ed25519 signatures over the index bytes, pinned per source (`keygen`/`sign`/`--pubkey`/`verify`), a tampered index rejected as a hard error, `--require-signed` strict auto-resolve, and trust surfaced in `list`/`search`. The sealed line is now `… rat/5.5` (discover) · `rat/5.6` (auto-resolve) · `rat/5.7` (remote index) · `rat/5.8` (signed entries). Additive — `make breaking` clean, no proto/axis change. **Open marketplace follow-ons:** publish + sign `official.json` on the `rat-dev` Pages site (URL + key are placeholders); `rat remove` symmetry; `rat add` *launching* into a live daemon (RegisterPlugin RPC, ADR-023).

---

## 2026-06-03 — Phase 8 slice 4: signed marketplace entries (ed25519 provenance) 🔑

Gave the marketplace **provenance** — the point of `--with-deps` auto-pulling is trusting what you pull. Detached **ed25519** signatures (the house algo, seeded by D4) over the raw index bytes: a publisher signs, a consumer pins the publisher's public key per source, and rat verifies every fetch (including the cached copy on offline fallback). New verbs: `rat marketplace keygen` (ed25519 keypair), `rat marketplace sign <index> --key` (writes detached `<index>.sig`), `rat marketplace add … --pubkey <key-or-path>` (pins + enforces), `rat marketplace verify <name>` (on-demand re-check). A pinned key with a missing/invalid signature is a **hard error** — the index is rejected, not used. Trust is surfaced: `rat marketplace list` tags `🔑 signature-enforced`; `rat search` gains a `TRUST` column (`signed✓` / `unsigned` / `local`); the `--with-deps` add lines + suggestions carry the label. New strict knob `rat add --with-deps --require-signed` only auto-pulls from verified sources (an unsigned provider is skipped + reported). **Proven live**: keygen → sign → pin → `verify`/`search` show `signed✓`; a one-byte tamper after signing is rejected on `search` (drops the source) and `verify` exits 1; `--require-signed` refuses an unsigned-only provider with `✗ … is unsigned`. Unit test `TestSignVerifyRoundTrip` (happy + tamper + wrong-key + garbage-sig). New files `core/cmd/rat/signing.go` + `signing_test.go`; README signing section. Additive — `make breaking` clean, `make core-test` green (12/12), no proto change.

---

## 2026-06-03 — 🎉 PHASE 8 RE-SEALED — `rat/5.7` (marketplace iteration 3: remote index)

The marketplace phase iterates to **`rat/5.7`** (`phase-8` merged to `main` again, annotated tag) — slice 3, the **remote/HTTP-hosted official index**: a built-in `official` shorthand, a bounded fetch timeout, an offline cache with graceful fallback, surfaced warnings, and the publishable `marketplace/official.json` + README. The sealed line is now `… rat/5.5` (discover) · `rat/5.6` (auto-resolve) · `rat/5.7` (remote index). Additive — `make breaking` clean, no proto/axis change. **Open marketplace follow-ons:** actually publish `official.json` to the `rat-dev` Pages site (the URL is a placeholder); signed marketplace entries; `rat remove` symmetry; `rat add` *launching* into a live daemon (RegisterPlugin RPC, ADR-023).

---

## 2026-06-03 — Phase 8 slice 3: the remote/HTTP-hosted official index 🌐

Made the **added** marketplace source production-grade for *remote* indexes (it already accepted http(s) URLs, but only as a bare `http.Get` — untested live, silent-skip on failure). Now: a **built-in `official` shorthand** (`rat marketplace add official` registers the canonical `officialIndexURL` — no URL to type; `marketplace list` advertises built-ins not yet added), a **bounded 10 s fetch timeout** (a hung host can't wedge `rat search`), an **offline cache** (`~/.cache/rat/marketplaces/<name>.json` — a failed fetch falls back to the last-good copy with a `⚠ … using cached copy` note), and **surfaced warnings** (a bad URL / malformed index warns instead of vanishing). The reference index is now also published-shaped as `marketplace/official.json` + a `marketplace/README.md` documenting the format, hosting, and remote behaviour. **Proven live** against a containerized HTTP server: `marketplace add` a remote URL → `search` over HTTP → cache written → **stop the server** → `search` + `add --with-deps` both keep working from cache (warning shown), resolving a full transitive plane offline. New: `officialIndexURL`/`wellKnownMarketplaces`, `fetchSource`/`marketCacheDir`/`marketHTTP`, warnings threaded through `addedEntries`/`allMarketEntries`. Additive — `make breaking` clean, `make core-test` green, no proto change.

---

## 2026-06-03 — 🎉 PHASE 8 RE-SEALED — `rat/5.6` (marketplace iteration 2: `--with-deps`)

The marketplace phase iterates to **`rat/5.6`** (`phase-8` merged to `main` again, annotated tag) — slice 2, **`rat add --with-deps`**, turns the auto-*suggest* into auto-*resolve*: it transitively pulls the marketplace provider for every unsatisfied `requires` (synthesizing each provider's manifest from its entry; no pull at declare-time). The sealed line is now `… rat/5.5` (marketplace: discover) · `rat/5.6` (marketplace: auto-resolve). Additive — `make breaking` clean, no proto/axis change. **Open marketplace follow-ons:** a remote/HTTP-hosted official index; signed marketplace entries; `rat remove` symmetry; `rat add` *launching* into a live daemon (the RegisterPlugin RPC, ADR-023).

---

## 2026-06-03 — Phase 8 slice 2: `rat add --with-deps` — auto-run the suggested add 🔗

Turned the auto-*suggest* into auto-*resolve*. `rat add <name> … --with-deps` now, after the primary add, **loops**: compute the project's unsatisfied `requires` → find each one's marketplace provider (`providerFor`) → add it → repeat until everything has a provider or none can be supplied. **Transitive** — a provider added this round has its OWN `requires` resolved next round. The added provider's manifest is **synthesized from the marketplace entry** (`synthManifest`: the entry's kind/provides/requires *are* the manifest), so no image pull at declare-time — the image is fetched at `rat up`/`serve`. Proven live: `add my-scheduler --with-deps` → `+ rat-state + dbt-runner` (round 1) → `+ rat-secret` (round 2, rat-state's own `secret/resolve`) → `✓ all dependencies satisfied`; correctly saw dbt-runner's `state/get` already covered (no duplicate); a `requires` no marketplace provides (`catalog/register`) pulls what it can and reports the remainder with the axis hint. New: `--with-deps` flag, `resolveWithDeps`/`manifestsOf` (project.go), `addMarketEntry`/`synthManifest` (marketplace.go). Additive — `make breaking` clean, `make core-test` green, no proto change.

---

## 2026-06-03 — 🎉 PHASE 8 SEALED — `rat/5.5` (the marketplace)

Phase 8 — **the marketplace** — is **sealed at `rat/5.5`** (`phase-8` merged to `main`, annotated tag). It closes the Phase-7 follow-on and completes the **discover** leg of the ecosystem story: alongside **author** (`rat plugin …`), **run** (`rat serve`/`add`), **distribute** (GHCR), you can now **find** a plugin.

The `kind: marketplace` axis (ADR-001): a marketplace is a **source of plugin entries**, read in aggregate — **local** (images on this machine, by their ADR-026 stamped manifest) + **added** (index files/URLs, `~/.config/rat/marketplaces.json`). Verbs `rat search` / `rat list` / `rat marketplace add|list`, and the headline **`rat add` auto-suggest**: each unsatisfied `requires` names the exact provider + a ready-to-run `rat add --image <ref>` line. Reference index `marketplace/rat-official.json` (5 plugins).

The sealed line: `rat/2.0` core · `rat/2.5` platform+daemon UX · `rat/3.0` multi-surface UI · `rat/3.5` distribution · `rat/4.0` authoring · `rat/4.5` authoring↔runtime · `rat/5.0` dependency resolution · `rat/5.5` the marketplace. Additive throughout — `make breaking` clean, no proto/axis change since `rat/2.0`. **Open follow-ons:** `rat add` *running* the suggested add (pull+add in one); a remote/HTTP-hosted official index; signed marketplace entries; the remaining ADR-026 (launch-time manifest resolution, golden-vector conformance in `test`, signing, build-backend/template axes) + ADR-025 surface follow-ons.

---

## 2026-06-03 — Phase 8 slice 1: the marketplace — discovery + `rat add` auto-suggest 🛒

Built the **marketplace axis** (`kind: marketplace`, ADR-001) — closes the Phase-7 follow-on "`rat add` auto-suggesting the *exact* plugin." A marketplace is a **source of plugin entries**; rat reads several at once:

- **local** — plugin images on this machine, discovered by their stamped manifest (ADR-026 OCI label `dev.rat.manifest.v1.b64` → name/kind/provides/requires). No index file needed; a `rat plugin pack`'d image *is* a marketplace entry.
- **added** — index files / URLs the operator registers (`marketplace/rat-official.json` is the reference: 5 plugins, capability→image). Config at `~/.config/rat/marketplaces.json` (XDG_CONFIG_HOME).

New verbs (`core/cmd/rat/marketplace.go`): `rat search [query]` (name/kind/description **and capability** match, across local+added), `rat list` (plugins installed in this project's `rat.toml`), `rat marketplace add <name> <src> | list`. And the headline: the `rat add` satisfiability resolver now **auto-suggests** — each unsatisfied `requires` prints the exact provider + a ready-to-run `rat add --image <ref>  (<name>, from <source>)` line (`reportUnsatisfiedSuggesting`), falling back to the axis hint when no marketplace has a provider. Proven live: `marketplace add official → search (capability `state` surfaces the scheduler+dbt-runner that *require* it) → add my-scheduler ⚠2 unsatisfied → each names the exact ghcr.io image + source → list`. Additive — `make breaking` clean, no proto change.

---

## 2026-06-03 — 🎉 PHASE 7 SEALED — `rat/5.0` (dependency resolution)

Phase 7 — **dependency resolution** — is **sealed at `rat/5.0`** (`phase-7` merged to `main`, annotated tag). It completes both halves of "does rat check plugin deps?":

- **coherence** (already, `rat plugin check`, ADR-026) — are a *single* plugin's `provides`/`requires` **real** capabilities, and does the kind match its axis?
- **satisfiability** (this phase, `rat add`/`rat up`, ADR-023 #6) — across the project's plugins, does every `requires` have a **provider**? The poetry-resolver: `unsatisfiedRequires` collects all provides, flags every requires nobody provides, and warns (with a suggested axis) as the project fills in — proven `add rat-pipeline → ⚠3 → add rat-state → ⚠2 → add rat-secret → clean`.

The sealed line: `rat/2.0` core · `rat/2.5` platform+daemon UX · `rat/3.0` multi-surface UI · `rat/3.5` distribution · `rat/4.0` authoring · `rat/4.5` authoring↔runtime · `rat/5.0` dependency resolution. Additive throughout — `make breaking` clean, no proto/axis change since `rat/2.0`. **Open follow-ons:** `rat add` auto-suggesting the *exact* plugin (a capability→plugin index — the marketplace); the remaining ADR-026 (launch-time manifest resolution, golden-vector conformance in `test`, signing, build-backend/template axes) + ADR-025 surface follow-ons.

---

## 2026-06-03 — Phase 7 slice 1: the satisfiability resolver — `requires` need a provider 🧩

The deploy-time complement to `rat plugin check` (which validates a *single* plugin's deps are real): a **plane-level** check that every `requires` across the project's plugins is actually **provided** by some plugin in the set (ADR-023 #6, the poetry-resolver). `core/cmd/rat/resolver.go`:
- `unsatisfiedRequires(manifests)` — collect all `provides`, return every `requires` no manifest provides;
- `reportUnsatisfied` / `logUnsatisfied` — poetry-style warnings with a suggestion (the axis a provider belongs to);
- wired into **`rat add`** (warns after each add, as the project fills in) and **`rat up`** (`launchPlane` logs it before launching).

**Proven live:** in a fresh project, `add rat-pipeline` → `⚠ 3 unsatisfied: requires state/get, secret/resolve, state/put (add a state-/secret-axis plugin)`; `add rat-state` → `⚠ 2` (pipeline + state both still need secret/resolve); `add rat-secret` → clean. `TestUnsatisfiedRequires` covers the graph check; `make core-test` + `breaking` green; additive (no proto/axis).

So the two halves of dependency checking are both real now: **coherence** (`rat plugin check` — are a plugin's deps *real* capabilities?) and **satisfiability** (`rat add`/`up` — is there a *provider* for each?). The resolver warns; it doesn't block (a `requires` may be intentionally external, and the gateway errors at invoke time if it's actually called). Follow-on: `rat add` could auto-suggest the exact plugin to add (needs a capability→plugin index, the marketplace).

---

## 2026-06-03 — 🎉 PHASE 6 SEALED — `rat/4.5` (authoring ↔ runtime integration)

Phase 6 — closing the **authoring→runtime handoff** — is **sealed at `rat/4.5`** (`phase-6` merged to `main`, annotated tag). The single, load-bearing slice: **`rat add` reads the stamped manifest** (ADR-026 Q05), so a packed plugin image is genuinely **self-describing** —

- `rat plugin pack` stamps the validated manifest into the image (`dev.rat.manifest.v1.b64`);
- `rat add --image <ref>` (no `--manifest`) pulls if needed, reads the manifest back, **derives the name** from it, materializes `manifests/<name>.plugin.yaml`, and records it in `rat.toml`.

So the full ecosystem loop is closed end to end: **author** (`rat plugin init→check→test→pack→publish`, where pack puts the manifest *in* the image) → **run** (`rat add <ref>` reads it *out* → `rat up`) → **distribute** (`curl … | sh`). Additive throughout — `make breaking` clean, no proto/axis change since `rat/2.0`. The sealed line: `rat/2.0` core · `rat/2.5` platform+daemon UX · `rat/3.0` multi-surface UI · `rat/3.5` distribution · `rat/4.0` authoring · `rat/4.5` authoring↔runtime. **Open follow-ons (ADR-026):** launch-time manifest resolution without materializing a file; the deploy-time satisfiability resolver; golden-vector conformance in `test`; the build-backend/template axes; signing + the marketplace index.

---

## 2026-06-03 — Phase 6 slice 1: `rat add` reads the stamped manifest (manifest-from-image) 🏷️

Closed ADR-026 Q05: `rat add --image <packed-ref>` no longer needs `--manifest`. With `--image` and no `--manifest`, `rat add` (`core/cmd/rat/project.go`):
- ensures the image is present (`podman pull` if not — so a GHCR ref works);
- reads the manifest from the image's `dev.rat.manifest.v1.b64` label (`readStampedManifest`, now returning the raw bytes too) — **the name is derived from it** when not given;
- materializes the manifest into `manifests/<name>.plugin.yaml` (original bytes, comments preserved) and references it in `rat.toml`.

**Proven live:** `rat plugin pack` → a stamped `localhost/rat/secret:packed`; then `rat add --image localhost/rat/secret:packed` (no `--manifest`, no name) → `read manifest from … → manifests/rat-secret.plugin.yaml`, recorded `name = "rat-secret"` (derived) + image + manifest path in `rat.toml`; `rat plugin check` on the materialized manifest passes — full circle. `make core-test` + `breaking` green; additive (no proto/axis).

So the authoring→runtime handoff is seamless: `rat plugin pack` stamps the manifest in, `rat add <ref>` reads it back. The image is now genuinely self-describing. (Follow-ons: `rat add` resolving the manifest at *launch* without materializing a file; the satisfiability resolver; signing.)

---

## 2026-06-03 — 🎉 PHASE 5 SEALED — `rat/4.0` (plugin authoring & packaging)

Phase 5 — the **plugin authoring toolkit** (ADR-026) — is **sealed at `rat/4.0`** (`phase-5` merged to `main`, annotated tag). rat now has the third pillar an ecosystem needs (**author**, beside **run** and **distribute**): the complete lifecycle **`rat plugin init → check → test → pack → publish`**, all proven live —

- **`init`** — scaffolds a ready-to-build folder per kind (manifest + SDK stub + Dockerfile + README + **portable CI/CD**), poetry-init style;
- **`check`** — the static gate: manifest schema + per-kind + **dependency coherence** (provides/requires name *real* capabilities; kind matches its axis; cross-axis requires allowed);
- **`test`** — launches the image under the **real I9 profile** + verifies it **serves** every declared capability (fails on `Unimplemented`);
- **`pack`** — builds a **verified, manifest-stamped** image (manifest-from-image; `rat add <ref>` can read it);
- **`publish`** — re-verifies + pushes to a registry (`ghcr.io` or a local `registry:2`), **never shipping a broken plugin**.

Builds on the sealed line: **`rat/2.0`** core · **`rat/2.5`** platform + per-project daemon UX · **`rat/3.0`** multi-surface UI · **`rat/3.5`** distribution · **`rat/4.0`** authoring. Additive throughout — `make breaking` clean, no proto/axis change since `rat/2.0`. **Open follow-ons (ADR-026):** golden-vector conformance in `test` (Q03), `rat add` reading the stamped manifest, the deploy-time satisfiability resolver, the build-backend + template axes, signing + the marketplace index.

---

## 2026-06-03 — Phase 5 slice 5: `rat plugin check` now validates DEPENDENCIES 🔗

Closed the ADR-026 §3 gap — `check` was syntax-only on deps; now it validates them. `check` verifies (`core/cmd/rat/plugin.go`):
- **real capabilities** — every `provides`/`requires` must `resolveMethod` against the linked axis descriptors. A made-up capability in a **linked** axis hard-fails (typo); a capability whose axis isn't compiled into this rat is **noted unverified** (honest — not a false negative);
- **kind coherence** — a plugin's `provides` must be its own axis (`kindAxis`); `requires` are legitimately **cross-axis** (capability composition), so only their reality is checked, never their axis.

Helpers: `capAxisOf`, `kindAxis`, `linkedAxes`. **Proven live:** `rat-pipeline` passes (1 provides, **3 cross-axis requires all confirmed real**); a made-up `requires` → `✗ not a real capability of axis "state" (typo?)`; a `state-backend` providing a `strategy` cap → `✗ expected "state"-axis capabilities`. `TestPluginCheckDeps` covers it; `make core-test` + `breaking` green; additive. *(Satisfiability — is a provider actually available — stays the deploy-time `rat add`/`up` resolver, the poetry-resolver from ADR-023, not a per-plugin check.)*

So `rat plugin check` answers "are the plugin's deps real + coherent?" — yes, now. The authoring loop **`init → check → test → pack → publish`** is complete and the static gate has teeth on dependencies.

---

## 2026-06-03 — Phase 5 slice 4: `rat plugin publish` — the lifecycle is complete 🚀

The last verb (ADR-026): ship a verified plugin image to a registry (the team diff). `rat plugin publish` (`core/cmd/rat/plugin.go`):

- resolves the manifest from `--manifest` **or the image's stamped label** (`readStampedManifest` — manifest-from-image, ADR-026 Q05);
- **RE-verifies** the image being shipped (`launchAndProbe` — never publish a broken plugin);
- tags + `podman push`es to `<registry>/<name>:<version>` (+ `:latest`). Registry = `ghcr.io/<owner>` (prod) or a local **`registry:2`** (`localhost:5000`, the "local packaging service") — same mechanism; push handles TLS/auth (auto-insecure for localhost).

**Proven live end-to-end:** a local `registry:2` came up; `rat plugin pack` made a verified, stamped `localhost/rat/secret:packed`; `rat plugin publish --image … --registry localhost:5000` read the stamped manifest, re-verified (`✓ launches under I9 … serves rat://secret/v1/resolve`), pushed → `🚀 published localhost:5000/rat-secret:0.1.0`; the registry confirmed `{"name":"rat-secret","tags":["0.1.0"]}`. `make core-test` (cmd/rat) + `breaking` green; additive (no proto/axis). *(arrowticket's `TestBulkLegTicket` flaked once on a timing race, passed on rerun — unrelated.)*

🎉 **The plugin authoring lifecycle is COMPLETE:** **`rat plugin init → check → test → pack → publish`** — scaffold (poetry-init) → static gate → launch-verify under I9 → a verified, self-describing image → shipped to a registry, never publishing a broken plugin. Together with the runtime model (launch/route) and the distribution pipeline (rat's own GHCR release), rat now has the three things an ecosystem needs: **author, run, distribute.** Remaining ADR-026 follow-ons: full golden-vector conformance in `test` (Q03), `rat add` reading the stamped manifest (drop `--manifest`), the build-backend + template axes, signing + the marketplace index.

---

## 2026-06-03 — Phase 5 slice 3: `rat plugin pack` — the verified, manifest-stamped image 📦

The verb that ties authoring together (ADR-026): run the gate AND produce the artifact. `rat plugin pack` (`core/cmd/rat/plugin.go`):

- **stamps the validated manifest into the image** as the OCI label `dev.rat.manifest.v1.b64` (base64 YAML) — either building the dir's Dockerfile `--label` or, with `--image`, deriving `FROM <existing> + LABEL` in a temp context;
- runs the **gate** on the FINAL tag (`launchAndProbe`, extracted + shared with `test`: launch under I9 → healthy → serves every declared capability);
- **reads the manifest back** from the image to confirm it's recoverable, then tags the verified image (default `rat/<name>:<version>`).

This lands the **manifest-from-image** path (ADR-026 Q05): a packed image carries its own manifest, so `rat add <ref>` can read it (dropping `--manifest`). **Proven live:** `rat plugin pack --image rat/secret:dev --manifest …/secret.plugin.yaml --tag localhost/rat/secret:packed` → stamped, `✓ launches under I9 … serves rat://secret/v1/resolve`, `📦 packed … — verified + manifest stamped`; `podman inspect … | base64 -d` returned the full manifest. `make core-test` + `breaking` green; additive.

The authoring loop is now **`rat plugin init → check → test → pack`** — scaffold → static gate → launch-verify → a verified, self-describing image. Only **`publish`** (push the verified image → GHCR, the team diff) remains; then full golden-vector conformance (ADR-026 Q03) and `rat add` reading the stamped manifest.

---

## 2026-06-03 — Phase 5 slice 2: `rat plugin test` — the verified-plugin gate (launch + serves) 🔬

The strong gate (ADR-026): a plugin isn't "verified" because it built — it's verified because it **launches under I9 and actually serves what it declares**. `rat plugin test` (`core/cmd/rat/plugin.go`):

- builds the image (or `--image` an existing one), **launches it under the real I9 profile** via the deployment-runtime (non-root · cap-drop ALL · read-only rootfs), waits healthy;
- then **smoke-invokes each `provides` capability** directly on the launched plugin — capability → gRPC method via the linked descriptors (`resolveMethod`), a `dynamicpb` empty-request `conn.Invoke` — and **fails if any is `Unimplemented`** ("declares it, doesn't serve it"). `--manifest` overrides the manifest path.

**Proven live:** `rat plugin test --image rat/secret:dev --manifest …/secret.plugin.yaml` → `✓ launches under I9 … + healthy`, `✓ serves rat://secret/v1/resolve`, `✓ rat-secret PASSED`. A **lying** manifest (claims `rat://state/v1/get`) → `✗ declares … but does NOT serve it (Unimplemented)`. The deferred Terminate cleans the container. `TestResolveMethod` covers the capability→method resolution; `make core-test` + `breaking` green; additive (no proto/axis).

So the authoring loop now has its teeth: `rat plugin init → check → test`. A plugin that passes `test` is proven to run hardened + honor its contract surface. Full golden-vector conformance (ADR-026 Q03) + `pack`/`publish` (verified image → GHCR) are the remaining slices.

---

## 2026-06-03 — Phase 5 slice 1: `rat plugin init` + `check` — the authoring toolkit (ADR-026) 🧰

The build-time complement to the runtime model: **ADR-026** (Proposed) defines the `rat plugin` toolkit (init/check/test/pack/publish) — scaffold, the verified-plugin gate, scaffolded portable CI/CD, local-vs-GHCR tiers. This slice lands the two provable, immediately-useful verbs.

- **`rat plugin init <name> --kind <kind>`** (`core/cmd/rat/plugin.go`): scaffolds a ready-to-build folder — `manifest.yaml` (provides pre-filled from a per-kind map), a `server.py` SDK stub, `Dockerfile`, `requirements.txt`, `README.md`, `.gitignore`, and **portable CI/CD** (`ci.sh` + `.github/workflows/plugin.yml`, whose logic is just `rat plugin check && test && pack`, bootstrapped by the Phase-4 one-line install — works on GH + others; CI on PR, CD-publish on a `v*` tag). Refuses an unknown kind (must be one of the 18 axes). poetry-init for plugins.
- **`rat plugin check`** (the fast STATIC gate): `manifest.Load` (schema-subset: kind, name, well-formed capability URIs) + coherence (kind is a known axis; version present). Pass/fail.

**Proven live:** `rat plugin init my-secrets --kind secret-backend` scaffolded **9 files** with `provides: rat://secret/v1/resolve` pre-filled; `rat plugin check` passed (`✓ my-secrets (secret-backend) — manifest valid: 1 provides`); corrupting a capability made check **refuse** it (`malformed provides capability URI`). `TestPluginInitCheck` covers the loop (scaffold passes, unknown kind rejected, broken manifest fails). `make core-test` + `breaking` green; additive (no proto/axis). `test`/`pack`/`publish` (launch+conformance → verified image → GHCR) are the next slices.

So authoring a plugin is now `rat plugin init → check` (then the gate fills in) — `poetry`/`cargo` for plugins, with CI/CD baked in from `init`. Follow-ons (ADR-026 open questions): `test`/`pack`/`publish`; the build-backend + template axes; manifest-in-image; signing + the marketplace index.

---

## 2026-06-03 — 🎉 PHASE 4 SEALED — `rat/3.5` (distribution: the GHCR release pipeline)

Phase 4 — **distribution** — is **sealed at `rat/3.5`** (`phase-4` merged to `main`, annotated tag). rat now ships as a **`ghcr.io` binary + image**, so getting started is the founding `chmod +x ./rat` vision — no clone, no make:

- **`rat version`** (build-time `-ldflags -X main.version=<tag>`); **`make release-build`** → a static, versioned binary; **`make release-image`** → `ghcr.io/rat-dev/rat:<ver>` (version baked); **`make release-checksums`** → `SHA256SUMS` — all reproducible locally.
- **`.github/workflows/release.yml`** wraps them on a `rat/*` tag → a GitHub Release (binaries + SHA256SUMS + install.sh) + a multi-arch image on ghcr.io.
- **`scripts/install.sh`** → `curl … | sh` (detect os/arch, download, sha256-verify, drop `./rat`).

Proven via the reproducible build (`make release-build VERSION=3.0` → a statically-linked binary that runs `rat version`/`rat init`; the image's `rat version` reports the tag too). The CI run + `curl|sh` download need a real GitHub/remote (out of sandbox); the artifact-defining build logic is verified. Additive — `make breaking` clean; no proto/axis change since `rat/2.0`. **Next (Phase 4 continues / Phase 5): wire `rat add` to pull plugin images from GHCR (so `rat.toml` refs resolve end to end), signed releases + SBOM, then the ADR-025 surface follow-ons.**

---

## 2026-06-03 — Phase 4 slice 1: the GHCR release pipeline — `curl … && chmod +x ./rat` 📦

The distribution front door (the inbox idea, ADR-023): ship rat as a **`ghcr.io` binary + image** so a user never clones or `make`s. Built as **reproducible `make` targets** with a thin CI wrapper, so a release is exactly what builds locally.

- **`rat version`** (+ `--version`/`-v`): a `var version` injected at build time via `-ldflags "-X main.version=<tag>"`; defaults to `dev`.
- **`make release-build`** → a **static, CGO-free, versioned** `rat` binary into `dist/rat-<ver>-<os>-<arch>` (override `RELEASE_OS`/`RELEASE_ARCH` to cross-compile). **`make release-image`** → the daemon image `ghcr.io/rat-dev/rat:<ver>` (+ `:latest`), version baked via `ARG VERSION` in `core/Dockerfile`. **`make release-checksums`** → `SHA256SUMS`.
- **`.github/workflows/release.yml`**: on a `rat/*` tag → matrix-build the static binaries (linux/darwin × amd64/arm64) → a **GitHub Release** (binaries + SHA256SUMS + install.sh); + buildx the **multi-arch image** → `ghcr.io`. A thin wrapper over the make targets.
- **`scripts/install.sh`**: `curl -fsSL …/install.sh | sh` — detects os/arch, resolves the latest release, downloads the right `rat-<ver>-<os>-<arch>`, verifies its sha256, drops a `./rat`.

**Proven live (the reproducible build):** `make release-build VERSION=3.0` → a **statically-linked** ELF that runs (`rat version` → `rat 3.0`; `rat init` creates a `rat.toml`); `make release-image VERSION=3.0` built `ghcr.io/rat-dev/rat:3.0` and **the image's `rat version` reports `3.0`** too; `SHA256SUMS` generated. The CI workflow + the `curl|sh` download can't run in-sandbox (no GitHub/remote), but the build logic — the part that defines the artifacts — is fully verified. `make core-test` + `breaking` green; additive (no proto/axis).

So getting started becomes: `curl …/install.sh | sh && ./rat version` (binary) or `podman run ghcr.io/rat-dev/rat:<ver>` (daemon image) — the founding `chmod +x ./rat` vision, real. Follow-ons: wire `rat add` to pull plugin images from GHCR (so `plugins.yaml`/`rat.toml` refs resolve); signed releases + SBOM; a `get.rat.dev` short URL.

---

## 2026-06-03 — 🎉 PHASE 3 SEALED — `rat/3.0` (surfaces & consumers)

Phase 3 — the **surfaces & consumers** model (ADR-024/025) — is **sealed at `rat/3.0`** (`phase-3` merged to `main`, annotated tag). The UI is assembled from plugin contributions, and a plugin presents **per-surface** interfaces that **out-of-stack consumers** render — demonstrated across all three surfaces:

- **CLI** (`rat ui`) — proven live (text);
- **VS Code** (the generic shell, `?surface=vscode`) — surface-scoped + compiles strict;
- **webapp** (the bff's SPA at `/`) — **rendered in a real browser** ([screenshot](../platform/media/webapp-surface.png)).

One contribution registry (`ui/components/<plugin>/<surface>/<id>` in the state-backend); `contribute_ui` lets a plugin add UI in one call; actions route through the gateway (C5 + audit). Additive to `rat/2.5` — no proto/axis change; `make breaking` clean throughout. **Next: the GHCR release pipeline (Phase 4 / distribution) — ship rat as a `ghcr.io` binary + image so getting started is `curl … && chmod +x ./rat`, no clone/make.**

---

## 2026-06-03 — Phase 3 slice 3: the WEBAPP surface — a browser consumer, visually proven 🌐

The third surface (ADR-025) — and the one we can prove **visually**. The bff now serves a self-contained **SPA at `/`** (`WEBAPP_HTML` in `platform/bff.py`): the browser loads it, it fetches `/api/ui?surface=webapp`, and renders the webapp-targeted contributions — views (with drill-into-table), command buttons (→ `/api/invoke`). It runs in the **browser**, not the daemon — an out-of-stack consumer, exactly like the CLI/VS Code ones; it hardcodes no view.

- **Surface-keyed contributions:** `contribute_ui` now keys by surface (`ui/components/<plugin>/<surface>/<id>`) so the same component id targets multiple surfaces without colliding (the bff seed too). `rat-pipeline` now contributes a **webapp** interface (Lake Tables view + Run pipeline button) alongside its vscode + cli ones; the bff adds a webapp Run-History view.
- **Proven LIVE + VISUALLY:** the bff served the SPA at `/`; `/api/ui?surface=webapp` returned `{explorer:[run-history, lake-tables], command:[run-pipeline]}` (webapp-scoped — cli `build` absent); and a **headless Chromium screenshot** ([`platform/media/webapp-surface.png`](../platform/media/webapp-surface.png)) shows the rendered page: *EXPLORER* → "Run History" (platform-bff) = **9 runs recorded**, "Lake Tables" (rat-pipeline) = **bronze_orders / gold_daily_revenue / silver_orders** (the real lake tables); *COMMAND* → a **Run pipeline** button. Two plugins' webapp contributions, in one browser, scoped to the surface. `make core-test` + `breaking` green; additive (no proto/axis).

**🎉 ALL THREE surfaces now consume the same contribution registry independently:** **CLI** (`rat ui`, proven live), **VS Code** (surface-scoped shell, compiles), **webapp** (SPA, **rendered in a real browser**). ADR-025's surfaces-&-consumers model is real and demonstrated end to end. *(Visual proof needed a headless-chromium-via-podman screenshot — the Playwright MCP was blocked by a container-permission issue in this env.)* Remaining ADR-025 follow-ons: per-consumer identity/connection; the webview rich-content protocol; view-data capabilities (so a consumer fetches view data without the bff).

---

## 2026-06-03 — Phase 3 slice 2: the VS Code shell becomes the `vscode` surface consumer 🪟

Pointed the generic shell (`plugins/ui/vscode-platform/`) at its surface: it now fetches `GET /api/ui?surface=vscode` (a `surface()` helper + `uiPath()`; a `ratPlatform.surface` setting, default `vscode`) instead of the unscoped `/api/ui`. So the shell renders only the **vscode-targeted** contributions — a plugin's `cli`/`webapp` interfaces never leak in. Compile-verified strict (`tsc`).

The rendering still needs a running VS Code (unprovable headlessly), but the data side is proven: the bff already returns `?surface=vscode` → `{explorer:[run-history,lake-tables], command:[run-pipeline]}` (and `?surface=cli` → `{command:[build]}`), so the shell receives exactly its surface's set, `build` excluded. Two surfaces now consume the same registry independently: the **CLI** (`rat ui`, proven live) and the **VS Code** shell (surface-scoped + compiling). Additive (TS only). Next: a webapp consumer; the ADR-025 follow-ons (per-consumer identity/connection, the webview content protocol, view-data capabilities).

---

## 2026-06-03 — Phase 3 slice 1: the CLI surface — `rat ui` (surfaces & consumers, ADR-025) 🖥️

First buildable + provable slice of ADR-025 (the surface the CLI lets us prove headlessly): a plugin contributes a **per-surface** interface, and an **out-of-stack consumer** renders only its surface.

- **Surface dimension.** Component specs carry a `surface` (`vscode`|`cli`|`webapp`|agnostic); `contribute_ui` gained a `surface=` default; the bff's `/api/ui?surface=X` filters; the CLI consumer filters `surface == cli` (+ agnostic). `matchesSurface` unit-tested.
- **`rat ui` — the CLI surface consumer** (`core/client/ui.go`, a new `rat` verb): a **direct-to-gateway** client (nothing of it in the daemon) that reads `ui/components/*` from the state-backend, renders the cli-targeted contributions, and `rat ui run <id>` invokes a command's capability through the gateway. Identity via `--as` (C5 bounds it — ADR-025 #6).
- **`rat-pipeline` now contributes per-surface** (`main.py`): a **vscode** interface (Lake Tables view + Run pipeline command) AND a **cli** interface (a `build` command) — same capabilities, two surfaces.

**Proven live:** `rat ui` (cli) showed only `build` (from rat-pipeline); `rat ui run build` fired `strategy/apply` → `{snapshotId: cli-build}`; `rat ui --surface vscode` showed the *same registry's* vscode interface (`run-pipeline` + `lake-tables`) with `build` invisible; the bff filtered identically (`?surface=cli` → `{command:[build]}`, `?surface=vscode` → the vscode set); audit shows the cli consumer's command routed through the gateway (C5). `make core-test` + `breaking` green; additive (no proto/axis).

So ADR-025 is real for the CLI surface: one plugin, multiple surface-tailored interfaces; consumers are out-of-stack renderers that pull only their surface; absence of a surface is inert. Next on Phase 3: the vscode shell reads `?surface=vscode` (the generic shell already exists — point it at the surface); a webapp consumer; ADR-025 follow-ons (consumer identity/connection, the webview content protocol, view-data capabilities).

---

## 2026-06-03 — 🎉 PHASE 2 SEALED — `rat/2.5` (the v2-on-v3 platform + the daemon UX)

Phase 2 — the data platform bundle (ADR-020) reimagined as the orchestrator + plugin model — is **sealed at `rat/2.5`** (`phase-2` merged to `main`, annotated tag). What landed across the phase, all proven running:

- **The platform (v2 on v3):** landing → medallion (bronze/silver/gold) as **your dbt code** (`rat apply`'d, ADR-021) → quality-gated tests → **self-driving** scheduled refresh → **run history** → output in a **shared DuckLake** (Postgres catalog + S3 Parquet) → the bff serves it. Six plugins behind one gateway (scheduler · pipeline/dbt · state/Postgres · secret · bff · runner), every hop C5-authorized + audited, **DuckLake as the catalog** — the v2→v3 mapping, complete.
- **Launch, not compose (ADR-022):** rat launches plugins from a plane; the infra is just Postgres + MinIO; **socket-mount** lets rat run as a container launching siblings by name (k8s-shaped).
- **The daemon UX (ADR-023):** rat is a **per-project, poetry-style daemon** — `rat init`/`add`/`up -d`/`down`/`ls`/`status`/`call`/`apply`, one binary, many coexisting per machine (per-project unix socket + auto-port callback companion + instance-namespaced runtime).
- **Secrets centralized:** every credential (state DSN, lake DSN, S3) lives only in the secret plugin; consumers hold refs and resolve at use (C5 + audit + redaction).
- **An extensible UI (ADR-024/025):** the UI is **assembled from plugin contributions** — a generic shell renders `/api/ui`; `contribute_ui` lets a plugin add a view/command/config in one call; ADR-025 captures the **surfaces & consumers** model (per-surface interfaces; vscode/cli/webapp as out-of-stack consumers).

ADRs 019–025 land the thinking; `make breaking` held clean across the phase (additive only — no proto/axis change since `rat/2.0`). The frozen contract surface is untouched; Phase 2 is all core-daemon + reference plugins + platform + UX on top of the sealed `rat/2.0` wire. **Next: Phase 3 — build the surfaces & consumers model (ADR-025), starting with the CLI surface (the one provable headlessly).**

---

## 2026-06-03 — `contribute_ui` SDK helper: a plugin adds UI in one call 🧩

Made ADR-024's "a plugin contributes UI" a **one-liner**. New hand-written SDK module **`contracts/sdks/python/rat/contrib.py`** (`rat/contrib`, not generated — codegen only writes `rat/<axis>/v1`; named `contrib` to avoid the `rat/ui/` axis package): `contribute_ui(gateway, caller, components, retries=…)` publishes a plugin's UI components to `ui/components/<caller>/<id>` in the state-backend (via the gateway — C5-authorized + audited), riding out the state plugin's boot wiring with retries.

Migrated the platform to the proper pattern — **each plugin owns its UI**:
- **`rat-pipeline`** (dbt-runner) now self-contributes its `Lake Tables` view + `Run pipeline` command (`main.py` spawns a background `contribute_ui` once at startup); its manifest gains `requires: state/v1/put` + the declarative `contributes.slots` binding (`rat://ui/v1/explorer`→lake-tables, `rat://ui/v1/command`→run-pipeline).
- **the bff** drops those from its seed and keeps only its OWN `Run History`.

**Proven live:** the dbt-runner logged `contributed 2 UI components`; `/api/ui` shows `lake-tables` + `run-pipeline` **sourced from `rat-pipeline`** and `run-history` from `platform-bff` — the contributions come from the owning plugins, not a central seed. `make breaking` green; `contrib.py` parses clean; additive (no proto/axis/Go). So a plugin adds UI by: declare `contributes.slots`, `requires state/put`, call `contribute_ui(...)` once. Follow-ons unchanged: registry-introspection (read `contributes` from manifests), contribution trust.

---

## 2026-06-03 — the UI is assembled from plugin contributions (ADR-024) — plugins extend the UI 🧩

Made the VS Code UI **scalable by plugins**: it hardcodes no view; plugins *contribute* views/commands/config and the UI renders them — the VSCode `contributes` model, which the **frozen manifest already carries** (`contributes.slots: [{target, component}]`). **[ADR-024](../docs/architecture/adrs/024-ui-assembled-from-plugin-contributions.md) (Proposed)** defines the runtime mechanism; no contract change (uses `contributes.slots` + `state/v1`).

- **Contribution = manifest binding + runtime spec.** The manifest declares the slot binding; the rich component spec (`title`, `data` endpoint / `capability` / `schema`) is published to the **state-backend** at `ui/components/<plugin>/<id>` — the state-backend is the contribution registry (as it is the project store, ADR-021). No core change.
- **bff aggregates** (`platform/bff.py`): `GET /api/ui` does `state/list ui/components/` + get-each → a slot-grouped descriptor (`explorer`/`command`/`config`); it **hardcodes no view** (seeds only the platform's *own* components once). `POST /api/invoke {capability, data}` is the **generic action path** — any contributed command resolves its capability via the protobuf descriptor pool (json↔proto) and routes through the gateway (C5 + audit). bff manifest gains `state/v1/put`.
- **Generic shell** (`plugins/ui/vscode-platform/`): a VS Code extension that fetches `/api/ui`, renders each slot (explorer→tree with table/row drill-in, command→VS Code command, config→form), and fires actions via `/api/invoke`. **Compile-verified strict** (`tsc`, reusing vscode-rat's toolchain); the rendering needs a running VS Code (only the aggregation is headlessly provable — and is).

**Proven live:** the platform's seeded contributions showed in `/api/ui` (explorer: Lake Tables, Run History; command: Run pipeline); then a **brand-new "cooltool" plugin** published a **config form + a command** to `ui/components/cooltool/*` (via `state/put`) and **both appeared in `/api/ui` with the bff + shell unchanged** (`from cooltool`); `/api/invoke` fired a contributed command generically → `{rowsAffected:0, snapshotId:ui-test}`, audited as `platform-bff → strategy/apply` through the gateway. `make breaking` green; additive (no proto/axis/Go).

So **adding a plugin can add UI** — a view, a command, a config form — by publishing a contribution; the shell + bff never change. The last visible mile (a human opening the VS Code shell) is the only unprovable-here part; the extensibility mechanism beneath it is real + tested. Follow-ons: a `contribute_ui` SDK helper (one-call publish); a registry-introspection capability so the bff reads `contributes` from manifests directly; contribution trust (ties to the marketplace idea).

---

## 2026-06-03 — lake creds → the secret plugin: all platform secrets in ONE place 🔐

Closed the cred gap Q2 left: the lake DSN + S3 creds no longer sit in `plugins.yaml` — every consumer resolves them from `rat-secret` at point of use (the same pattern `rat-state` already used for its DSN). Now **the only place a credential appears is `rat-secret`'s `RAT_SECRETS`**.

- **Generic `*_REF` resolution.** A consumer carries `<NAME>_REF=ref://…` instead of `<NAME>=<secret>`. The dbt-runner (`server.py`) resolves **every** `*_REF` env var via `rat://secret/v1/resolve` (C5-authorized, audited, retry-on-boot) and injects the plain `<NAME>` into the dbt subprocess env — so the dbt profile's `env_var('RAT_LAKE_PG')` reads a resolved value while the credential lives only on the secret plugin. The bff (`bff.py`) does the same via a `_cfg(name)` helper (literal env, else resolve `<name>_REF`). Resolution is cached.
- **Wiring:** `rat-pipeline` + `platform-bff` manifests gain `requires: rat://secret/v1/resolve`; `rat-secret`'s `RAT_SECRETS` gains `ref://lake/pg-dsn` + `ref://lake/s3-key` + `ref://lake/s3-secret`; `plugins.yaml` swaps the consumers' `RAT_LAKE_PG`/`RAT_S3_KEY`/`RAT_S3_SECRET` for `*_REF` (non-secret `RAT_LAKE_DATA`/`RAT_S3_ENDPOINT` stay literal).

**Proven live:** `grep password platform/plugins.yaml` matches **only** `rat-secret`'s line; the platform self-drove → the medallion built into the shared lake (PASS=7, runner resolved its DSN from the secret plugin); the bff served `/api/table/gold_daily_revenue` (resolved its own creds); audit shows the new hops — `rat-pipeline → secret/resolve → rat-secret` (×3) + `platform-bff → secret/resolve → rat-secret` (×3) + `rat-state` (×1). `make breaking` green; additive (no proto/axis/Go — plugin code + manifests + plane).

So the platform's whole secret surface is centralized + resolved-at-use: `state DSN`, `lake DSN`, `S3 creds` — one trust boundary, every read C5-authorized + audited + redacted. Remaining secret follow-on: ADR-022 Q4 (a `LaunchSpec` secret channel so even `rat-secret` loads from Vault/a file, not `$RAT_SECRETS`).

---

## 2026-06-03 — Q2: the medallion writes to a SHARED DuckLake — the UI sees pipeline output 🪣

The data-side gap closed (ADR-021 Q2): the pipeline no longer writes a local DuckDB trapped in the runner's tmpfs — it materializes into a **shared DuckLake** (DuckLake catalog on **Postgres**, data as Parquet on **S3/MinIO**), so any plugin/client/UI can read the tables.

**The blocker was always dbt** (the engine plugin already did remote DuckLake). Root cause + fix found empirically: **dbt-duckdb 1.9.4 attaches DuckLake natively** — its `Attachment` has an `options` dict, so `attach: [{path: "ducklake:postgres:…", alias: lake, options: {data_path: "s3://…"}}]` emits exactly `ATTACH 'ducklake:postgres:…' AS lake (DATA_PATH 's3://…')`, plus a `TYPE S3` secret for MinIO. Models materialize with `+database: lake`.

- **Write side** (`platform/dbt-project/`): `profiles.yml` attaches the shared lake via env vars rat injects (`RAT_LAKE_PG` / `RAT_LAKE_DATA` / `RAT_S3_*` — no creds committed); `dbt_project.yml` sets `+database: lake`. `plugins.yaml` gives `rat-pipeline` the lake connection. The dbt-runner image gained `HOME=/tmp` so DuckDB's `~/.duckdb` lands on the I9 tmpfs (read-only rootfs otherwise rejected `/plugin/.duckdb`).
- **Read side / the UI's view** (`platform/bff.py` + `bff.Dockerfile`): the bff gained the **bulk DATA leg** — `GET /api/tables` and `GET /api/table/<name>` attach the same lake **read-only** and return JSON. This is the honest **F9 split**: control (trigger / run history) through the gateway, the bulk data leg **direct** (the lake read never appears in the audit). `+duckdb` in the image, `HOME=/tmp`.

**Proven live:** the full platform self-drove → `dbt build` created `lake.main.{bronze_orders, silver_orders, gold_daily_revenue}` (PASS=7) **in the shared lake**; a **separate** client (a throwaway DuckDB attaching the same lake) read `gold_daily_revenue` → `2026-05-01: 2 orders $59.98`, `2026-05-03: 2 orders $179.49`; and the **bff served it** — `/api/tables` → `[bronze_orders, gold_daily_revenue, silver_orders]`, `/api/table/gold_daily_revenue` → the rows as JSON. Control still flows through the gateway (run history, 15 runs; audit shows `platform-bff → state/get/list`, `rat-scheduler → apply/put`, `rat-state → secret/resolve`) — the lake read is **not** in the audit, confirming the data leg is direct. `make breaking` green; additive (no proto/axis/Go — config + images + bff.py).

So the v2 picture is whole on v3: **landing → medallion (bronze/silver/gold) → quality-gated → self-driving refresh → run history → and now the OUTPUT is in a shared DuckLake the UI reads.** Follow-ons: resolve `RAT_LAKE_PG` via the secret plugin (creds out of `plugins.yaml`); DuckLake snapshots/time-travel for read-isolation; a real VS Code UI pointed at these bff endpoints.

---

## 2026-06-03 — slice 2c: the daemon lifecycle — `rat up -d` / `down` / `ls` / `status` 🔌

The daemon-lifecycle ergonomics (ADR-023 slice 2c) — and the fix for the backgrounded-kill papercut. `core/cmd/rat/lifecycle.go`:
- **`rat up -d`** — spawns a **detached** background daemon (its own process group, output → `.rat/daemon.log`), waits until it logs `gateway serving`, prints its pid. Foreground `rat up` is unchanged. Both **refuse** to start a second daemon for a project that already has one (the unix socket would otherwise be silently hijacked).
- **`rat down`** — SIGTERMs this project's daemon and waits for the drain — so teardown is **owned**, not a stray `kill` that races (the papercut, gone).
- **`rat status`** — this project's state (running+pid / stopped) + socket + declared plugins.
- **`rat ls`** — every rat daemon on the machine (the `docker ps` of daemons), from a global registry at `~/.local/state/rat/instances.json`.

The daemon now **registers itself**: on serve it writes `.rat/daemon.pid` + a registry entry (keyed by project dir); on drain it retracts both (`registerDaemon`/`deregisterDaemon`, gated on a project context — no-op for a raw `rat serve --plane`). The registry is *status* (pruned of dead pids on read), never the spec — consistent with ADR-023.

**Proven live:** `rat init`→`add`→**`rat up -d`** (pid 4163550) → **`rat ls`** showed `lifecycle 4163550 /tmp/rat-2c` → **`rat status`** = running + socket + 2 plugins → **`rat call`** routed to the backgrounded daemon (`pid=4163558 key=2c`) → **`rat down`** stopped it cleanly → `rat ls` empty, socket cleaned, **no stray process**. `TestRegistryAndPid` covers pid-liveness + upsert/prune/remove. `make core-test` + `breaking` green; additive (no proto/axis).

The daemon track is now a complete tool: **`rat init` → `rat add` → `rat up -d` → `rat status`/`ls` → `rat apply`/`call` → `rat down`**, many coexisting per machine. Remaining on the track: **2d** (`rat lock` + capability resolver at `add` time + image-embedded manifests to drop `--manifest`) and the **GHCR release pipeline** (`curl … chmod +x ./rat`).

---

## 2026-06-03 — slice 3: one `rat` binary — `ratctl` folded in as `rat call` / `rat apply` 🧬

Unified the artifact (ADR-023 §7): the client logic moved into a shared **`core/client`** package (exported `Run`), and `rat` grew `call`/`apply` verbs over it. So one `rat` binary now does **`serve` · `up` · `init` · `add` · `call` · `apply`**. The ADR-019 orchestrator/client *boundary* stays real (the client still reaches the daemon only over the gateway, no in-process shortcut) — only the *packaging* unified. `ratctl` becomes a **thin alias** (`main` → `client.Run`) for the transition; its test moved to `core/client` (`build.Dir` + `run→Run` adjusted), `make ratctl-smoke` now targets `./client`.

**Proven live:** built one `bin/rat`; `rat init`→`rat add`→`rat up` brought a project daemon up, then **`rat call` (the same binary)** routed to the launched plugin (`pid=4125480 key=one-binary`), the **`ratctl` alias** hit the same daemon (same pid, `key=via-alias`), and `rat call` of an undeclared `put` was `PermissionDenied` (C5). `make core-test` + `breaking` green; additive (no proto/axis; client logic relocated, behavior identical — `core/client` carries the moved `TestRatctlCallsThroughGateway` + `TestTarProject`).

The daemon track now reads end-to-end as one tool: **`rat init` → `rat add` → `rat up` → `rat apply` → `rat call`**, all `rat`. Remaining: **2c** (`rat up -d` background + `rat down` + `rat ls` + `rat status`), `rat lock` + capability resolver, image-embedded manifests (drop `--manifest`); then the GHCR release pipeline makes `curl … chmod +x ./rat` real.

---

## 2026-06-03 — slice 2: the poetry verbs — `rat init` / `add` / `up` over `rat.toml` 📜

The ergonomics layer of ADR-023: the platform is now a **command-written `rat.toml`** (the committed spec, never hand-edited — poetry's `pyproject.toml` model) driven by poetry-shaped verbs. `rat` became a **multi-call binary** (`serve` | `up` | `init` | `add`).

- **`rat.toml`** (TOML, via `github.com/pelletier/go-toml/v2`): `name` + `runtime` + `addr` + `[[plugin]]` tables. Reduces to the same internal `Plane` as a YAML plane (`planeFromRaw` is now shared by `LoadPlane`/`LoadProject`), so the daemon's bring-up path is unchanged. Default `addr` = this project's **per-project unix socket** (`.rat/daemon.sock`).
- **`rat init`** — writes a commented `rat.toml` SHELL (name + runtime, no plugins) + a `.rat/.gitignore` (runtime junk is ignored, like `.venv`); refuses to clobber.
- **`rat add <name> --image <ref> --manifest <path> [--env K=V]`** — **appends** a `[[plugin]]` (preserves the file's comments/order), with a duplicate-name check; no `--image` ⇒ a register-only driver. (Live hot-register lands with the `RegisterPlugin` RPC; for now `rat up` materializes the spec.)
- **`rat up`** — walks up from cwd to find `rat.toml` (git/poetry/cargo-style), loads it, and runs the daemon (`serveResolved`, shared with `rat serve`). Foreground for now.

**Proven live:** in `/tmp/rat-poetry-demo`, `rat init --name demo` → `rat add rat-state --image … --manifest …` + `rat add rat-caller` wrote a clean `rat.toml`; `rat up` discovered it and served on `/tmp/rat-poetry-demo/.rat/daemon.sock` (+ companion `[::]:37135`); `ratctl … --addr unix:…/.rat/daemon.sock` routed to the launched plugin (`pid=4069897 key=poetry`). `TestProjectInitAddLoad` covers the init→add→load round-trip (incl. dup-add + clobber refusal). `make core-test` + `breaking` green; additive (no proto/axis; +`go-toml/v2` dep).

So the real flow is now `rat init` → `rat add` → `rat up`, over a committed `rat.toml`. Remaining on the daemon track: **2c** (`rat up -d` background + `rat down` + `rat ls` global registry + `rat status`), `rat lock` + the capability resolver at add-time, image-embedded manifests (drop the `--manifest` path), and **slice 3** (fold `ratctl` into the one `rat` binary so it's `rat call`/`rat apply`). *(Papercut still open: a backgrounded daemon's kill occasionally misses in the test harness — the daemon drains fine on real SIGTERM.)*

---

## 2026-06-03 — slice 1.5: the dual-listener daemon — unix control socket + auto-port TCP callback companion 🔁

Closed the gap slice 1 deferred: a unix-socket-only gateway can't be dialed *back* by launched **driver** plugins (scheduler/bff) that need a network endpoint. The daemon now serves the **same gateway on two listeners** (`core/cmd/rat`):
- **control** = `pl.Addr` — a per-project **unix socket** (ADR-023, collision-free) or TCP;
- **plugin-callbacks** = when control is a unix socket, an **auto-port TCP companion** (`0.0.0.0:0` → a free port, so it never collides across instances either); when control is already TCP, that doubles as the callback endpoint.

Listeners open **before** assembly so the companion's actual port is known in time to inject `RAT_GATEWAY` (now `host.containers.internal:<companion-port>` / `<rat-hostname>:<port>` / loopback, per topology — `gatewayCallbackAddr(pl, port)`). `GracefulStop` closes both; the socket unlinks on drain.

**Proven live — the FULL platform under the per-project model:** `rat serve` with `addr: unix:/tmp/rat-platform.sock` (podman, host mode) logged `control /tmp/rat-platform.sock · plugin-callbacks [::]:38673`; the **scheduler self-drove through the companion** (`firing … → host.containers.internal:38673`, ticks refreshed) and recorded **durable** run history to Postgres; **`ratctl` read that history over the UNIX SOCKET** simultaneously (`platform-runner → state/list`); audit shows both paths through **one gateway** (drivers via companion: `rat-scheduler → apply/put`, `rat-state → resolve`; CLI via socket: `→ list`); socket cleaned on drain. `make core-test` + `breaking` green; additive (no proto/axis). The full self-driving platform now runs per-project, collision-free on both the control and callback channels — two such platforms can coexist (distinct sockets + distinct auto-ports). Next: the poetry verbs (`init`/`add`/`install`/`lock`) + `.rat/` layout + `rat ls`, then fold `ratctl` into one `rat`.

---

## 2026-06-03 — ADR-023 + slice 1: rat is a per-project daemon — two rats coexist on one machine 🏠

Decided the *shape of the daemon* (the question Phase 2 left open) and built the load-bearing half. **[ADR-023](../docs/architecture/adrs/023-rat-as-a-per-project-daemon.md) (Proposed):** rat is a small, **disposable per-project daemon** (a venv with a heartbeat) whose desired state lives in an **external, git-committed spec** (`rat.toml`+`rat.lock`); control is **hybrid, poetry-style** (imperative `rat add` writes the spec *then* reconciles; declarative `rat install` rebuilds from it); the running registry is **status, never the source of truth** (k8s spec/status — sidesteps the snowflake-server failure mode of a living broker). Ships as a GHCR binary (`chmod +x ./rat`) **and** image; one multi-call `rat` (refines ADR-019's two-binary split — boundary kept, artifact unified). Many rats per machine ⇒ per-instance isolation is mandatory. Promotes the parked runtime-self-registration idea (its `SetProvider` keystone is now built).

**Slice 1 built + proven** (`phase-2-rat-per-project-daemon`) — the two pieces that touch existing code and make coexistence real:
- **Per-project unix socket.** The daemon's `listen()` now binds a `unix:<path>` address (parent dir created, stale socket removed, unlinked on drain) instead of only `tcp` — so many daemons coexist with **no port war** (the old fixed `:7777` made the 2nd daemon fail). `ratctl --addr unix:<path>` dials it (gRPC's native `unix:` target).
- **Instance-namespaced deployment-runtime.** A plane `name:` (or the plane-dir basename) becomes the **instance id**; the podman runtime (`NewPodmanInstanced`) prefixes SIBLING-mode container names with it (`<instance>-rat-state-1`), so two daemons never collide on a name even if they share a network. `TestContainerName` covers it.

**Proven live:** two daemons (`alpha`, `beta`) started on `/tmp/rat-A/daemon.sock` and `/tmp/rat-B/daemon.sock` — both `gateway serving` at once; `ratctl` against each socket routed to a **distinct** stateplugin process (`pid=3924413` vs `pid=3924406`); C5 still enforced per-instance (undeclared `put` → `PermissionDenied`); both sockets cleaned on drain. `make core-test` + `breaking` green; additive (no proto/axis).

**Deferred within slice 1 (noted):** a unix-only gateway can't be dialed *back* by launched **driver** plugins (scheduler/bff) that need a network endpoint — so the proof used serve-only plugins. The driver-callback endpoint (an auto-port TCP companion, or mounting the socket into plugin containers) is the next sub-step, before the poetry verbs (`init`/`add`/`install`/`lock`) + `.rat/` layout + `rat ls`.

---

## 2026-06-03 — `rat apply`: your pipeline is code you submit (not baked) 📦

ADR-021's headline made real: the dbt project is no longer baked into the runner image — you **submit** it to the running orchestrator, and the next run executes YOUR code. Crucially, this needed **no new axis and no proto change**: the **state-backend IS the project store**.

- **`ratctl apply --project <dir> --name <name>`** (`core/cmd/ratctl`): tar.gz's the project client-side (generated/VCS noise excluded — `tarProject` + `TestTarProject`), then ships it to `projects/<name>` via `rat://state/v1/put` — the same C5-authorized, audited gateway path as any command. `ratctl` grew a subcommand dispatcher (`call` | `apply`); `apply` builds a `state.PutRequest` directly (binary tarball, not protojson). Default caller `--as platform-runner`.
- **The dbt-runner fetches the applied project** (`plugins/runner/dbt-duckdb/server.py`): on each `strategy/apply` it `rat://state/v1/get`s `projects/<name>`, extracts it (py3.12 `filter="data"` safe untar), and runs `dbt build` on it — re-extracting **only when the stored revision changed** (revision-cached). The baked sample project is the fallback until something is applied.
- **Wiring:** `rat-pipeline` manifest `requires rat://state/v1/get`; `platform-runner` `requires rat://state/v1/put` (the operator identity `ratctl apply` uses); rat now **injects `RAT_PLUGIN_NAME`** (each plugin's manifest name → its caller identity) alongside `RAT_GATEWAY` in `launchPlane`, so the runner can identify itself when it calls `state/get`; `plugins.yaml` sets `RAT_PROJECT_KEY: projects/medallion`.

**Proven live** (host mode): the **baked** run was `PASS=7` (no `applied_marker`); `ratctl apply` of a modified project returned `applied … → projects/medallion (8 files, revision 1)`; the runner logged `extracted applied project 'projects/medallion' rev 1` and the next run built `1 of 8 OK created sql table model main.applied_marker` → `PASS=8`, with DuckDB ground-truth `('applied-via-rat-apply', 42)`. **Re-apply** of a further-changed project bumped to **rev 2**, re-extracted, and the value propagated `42 → 99`. Audit shows the full path: `platform-runner → state/put → rat-state` (the apply) + `rat-pipeline → state/get → rat-state` (the fetch). `make core-test` + `breaking` + `ratctl-smoke` green; additive (no proto/axis).

So adding/updating a pipeline is now `ratctl apply` — your code, submitted to the always-on orchestrator, picked up on the next run. (Known nit: the host-mode SIGTERM drain occasionally races and leaves plugin containers up — a teardown-robustness follow-on; manual cleanup is one line.) Follow-ons: Q2 (dbt→shared-DuckLake so the UI sees the tables), per-project cron, the dedicated `pipeline/v1/run` axis.

---

## 2026-06-03 — secret plugin: creds out of consumer plugins, resolved through the gateway 🔐

Tom's "store one or 2 secrets on a communicating secret plugin" made real, on the **frozen `secret/v1`** contract (no proto change). A `kind: secret-backend` plugin holds the platform's secrets in **one trust boundary**; consumer plugins hold only an opaque **ref** and resolve it at point of use through the gateway (C5-authorized, audited, tenant-scoped, redacted).

- **`plugins/secret/env-py`** — an env-backed secret-backend (`store.py` loads a `{tenant: {ref: value}}` map from `$RAT_SECRETS`; `server.py`/`main.py` serve the frozen `SecretService.Resolve`). Keeps the conformance reference (`inmemory-py`, hardcoded golden map) untouched. Same tenant-scoped anti-enumeration: unknown ref AND cross-tenant ref both → `found=false` (never `PERMISSION_DENIED`); 5-min TTL; `value` is `debug_redact`.
- **`rat-state` resolves its DSN, lazily** (`plugins/state/postgres-py/server.py`): on first state op it dials the gateway (`RAT_GATEWAY`, injected) and calls `rat://secret/v1/resolve` for `$RAT_STATE_PG_REF` (`ref://state/pg-dsn`), retrying until `rat-secret` is wired, then connects Postgres. A literal `$RAT_STATE_PG` is still honored as a fallback. Lazy ⇒ rat-state is Healthy immediately and doesn't race the secret plugin's boot.
- **Wiring:** `secret/v1` added to the gateway's routable descriptors (`cmd/rat/descriptors.go`); `rat-state` manifest gains `requires: rat://secret/v1/resolve`; `platform/manifests/secret.plugin.yaml`; `plugins.yaml` adds `rat-secret` (the DSN lives in its `$RAT_SECRETS`, one place) and `rat-state`'s env drops the DSN for `RAT_STATE_PG_REF`. `make plugin-images` builds `rat/secret:dev`.

**Proven live** (host mode, `rat serve --plane plugins.yaml`, 5 launched plugins): startup injected `RAT_GATEWAY` + wired all 5 + served; the new audit hop **`rat-state → rat://secret/v1/resolve → rat-secret`** fired (lazy, once, then cached); `rat-state`'s container env carried **no credential** (only `RAT_STATE_PG_REF=ref://state/pg-dsn`, no `RAT_SECRETS`, no password); the platform **self-drove** (ticks 2+ refreshed) and recorded **durable** run history to Postgres (so the DSN resolved + connected); the raw DSN password appeared **0×** in rat's audit log. `make core-test` + `breaking` green; additive (no proto/axis).

So adding the secret plugin is — as Tom asked — one `plugins.yaml` entry + an image, and consumers stop carrying raw credentials. Follow-on: ADR-022 Q4 (a `LaunchSpec` secret channel so even `rat-secret` loads from Vault/a file instead of `$RAT_SECRETS`).

---

## 2026-06-03 — 2c COMPLETE: socket-mount — rat is ITSELF a container, launching plugins as siblings by name 🔌

The final 2c refinement, and ADR-022's "socket-mount local" made real: **rat runs as a container** that drives the **host's rootless podman** over a mounted socket (Docker-out-of-Docker) and launches each plugin as a **sibling container** on a shared `rat-net`, dialing them **by name** via podman DNS — the exact k8s pod-to-pod-by-service-name shape (the prod target), no host-port publishing.

**Core change — the podman runtime's SIBLING mode** (`core/deploymentruntime/podman.go`): `NewPodmanNetworked(net)` (selected by `$RAT_PODMAN_NETWORK`) launches each plugin with `--network=<net> --name <plugin>-<seq> --replace`, returns `<name>:50051` as the endpoint (no `-p` publish; a containerized rat's own `127.0.0.1` can't reach a sibling's host port, but a name on the shared net resolves), and `--add-host=host.containers.internal:host-gateway` so plugins still reach host-published backends (Postgres). Empty network ⇒ the original host-publish mode, unchanged. **I9 holds in both** — a user bridge is still a private netns that drops the 169.254 metadata route. `TestPodmanSiblingNetwork` proves a peer container resolves the sibling by name and connects (`make core-test-podman` green); `TestContainerName` covers the naming. `$RAT_PODMAN_BIN` lets the runtime use `podman-remote`.

**Same plane, both topologies** — rat now **injects `RAT_GATEWAY`** into each launched plugin per mode (`host.containers.internal` on the host · rat's own shared-net name when socket-mounted · loopback for local processes), so `plugins.yaml` runs host-mode AND socket-mounted **unchanged** (the hardcoded `RAT_GATEWAY` came out of the file). `core/Dockerfile` now installs the podman client + a `podman-remote` wrapper, and splits ENTRYPOINT/CMD so the plane path is overridable.

**Proven end-to-end** (`make platform-socket` / `platform/run-socket-mount.sh`): `rat AS A CONTAINER` (`--user 0` for the rootless host socket, on `rat-net`, socket mounted) → `launching 4 plugin(s) via podman` → `wired rat-pipeline-1 / rat-state-2 / rat-scheduler-3 / platform-bff-4` (all siblings, by name) → serving on `:7777`. The platform **self-drove**: ticks 2–10 each `refreshed` (real `dbt build` inside `rat-pipeline-1`), recorded durable run history to the **host** Postgres via `host.containers.internal` (verified in `rat_state`), the scheduler dialed the gateway at rat's injected shared-net name, and the **bff served `/api/runs` reached BY NAME from a peer** (no host port). Audit shows every hop through the *containerized* gateway: `rat-scheduler → strategy/apply → rat-pipeline`, `→ state/put → rat-state`, `platform-bff → state/list+get → rat-state`. Drain + `run-socket-mount.sh down` tore down rat + every sibling + `rat-net`; Tom's kinora/kinori stacks untouched.

**ADR-022 Q3 (sibling networking) + Q5 (gateway re-bind) RESOLVED; Q2 partially** (RAT_GATEWAY injection). `make core-test` + `core-test-podman` + `breaking` green; additive (no proto/axis). **The launch-mode arc is complete:** rat launches the whole self-driving platform, whether on the host or as a container itself; adding a plugin is one `plugins.yaml` entry + an image; the infra is two backends. Orthogonal follow-ons remain: secret plugin (creds out of env), Q2 (dbt→shared-DuckLake), `rat apply` (project upload), and a `k8s` deployment-runtime for the prod profile.

---

## 2026-06-03 — 2c: the FULL launch-mode platform self-drives — rat launches all 4 plugins, infra is just Postgres+MinIO 🚀

The payoff in full: **rat-on-host launched the entire platform — 4 plugin containers from one `plugins.yaml` — and it ran itself.** `platform/plugins.yaml` now declares `rat-pipeline` (dbt-runner), `rat-state` (Postgres-backed run history), `rat-scheduler` (self-driving clock), `platform-bff` (UI control-path), plus a register-only `platform-runner` driver. The infra shrank to `platform/compose.infra.yaml` = **just Postgres + MinIO** (no per-plugin service — adding a plugin never touches it). The two *drivers* (scheduler, bff — they call the gateway, serve no capability) got a trivial TCP health-port so the reconciler can launch + supervise them like any plugin (`_serve_health` in `cron-py/main.py`; the bff binds `RAT_PLUGIN_ADDR`). Plugins reach the host (Postgres at `:55440`, the gateway at `:7777`) via `host.containers.internal`. **Proven live** (`rat serve --plane plugins.yaml`, podman runtime):
- `launching 4 plugin(s) via podman` → `wired rat-pipeline/rat-state/rat-scheduler/platform-bff` → `gateway serving on :7777 — 5 plugin(s) up`; 4 `rat/{dbt-runner,state,scheduler,bff}:dev` containers running, all launched by rat;
- the platform **self-drove**: ticks 2–10 each `refreshed` (a real `dbt build` in the dbt-runner container) **and** recorded to run-history via `rat-state → Postgres` (tick 1 lost the cold-start race to the gateway bind, retried next tick);
- **durable**: 9 `success` rows verified directly in Postgres (`rat_state` table, `runs/000002…000010`);
- the **launched bff** served `/api/runs` reading that history back through the gateway;
- audit confirms the launched plugins talk *only* through the gateway: `rat-scheduler → strategy/apply → rat-pipeline`, `rat-scheduler → state/put → rat-state`;
- drain tore down every launched container (reconciler-managed); Tom's kinora/kinori stacks untouched.

So **the whole self-driving, run-history, UI-backed platform runs entirely rat-launched; the infra is two backends; adding a plugin = one `plugins.yaml` entry + an image.** ADR-022's headline, end to end. `make breaking` clean; additive (no proto/axis change). **Remaining in 2c:** **socket-mount** (containerize rat: podman CLI + host socket + in-network endpoint) as the final refinement. (Project delivery via `rat apply` and dbt→shared-DuckLake (Q2) remain orthogonal follow-ons.)

---

## 2026-06-03 — 2c: the slim launch-mode platform — the medallion runs through a rat-LAUNCHED stack 🎛️

The payoff: **rat launches the dbt-runner as its own container and the medallion runs inside it** — no per-plugin compose service. `platform/plugins.yaml` (a `runtime: podman` launch plane — one entry per plugin → its image) + the dbt-runner image now bakes a demo dbt project + landing data (a DEMO shortcut; real projects arrive via `rat apply`, ADR-021 Q1) and is read-only-rootfs-safe (`DBT_SEND_ANONYMOUS_USAGE_STATS=0`, dbt writes only to the I9 `/tmp` tmpfs). **Proven live** (rat on the host + the proven podman runtime): `rat serve --plane plugins.yaml` →
- `launching 1 plugin(s) via podman` → `wired rat-pipeline -> 127.0.0.1:44587` → serving;
- a `rat/dbt-runner:dev` **container** is running, launched by rat;
- `ratctl call rat://strategy/v1/apply` → routes to it → it runs `dbt build` on the baked project → `bronze_orders → silver_orders → gold_daily_revenue` + tests → **`Completed successfully, PASS=7 ERROR=0`** (the medallion ran *inside the launched container*);
- SIGTERM → `unwired rat-pipeline` → drained.

So **the medallion runs through a rat-launched stack; the infra carries no per-plugin service; adding a plugin = one `plugins.yaml` entry + an image** — ADR-022's headline, real. `make breaking` clean; additive. **Remaining in 2c:** add the other plugins to `plugins.yaml` (state → Postgres via `host.containers.internal`; scheduler/bff are drivers that need a trivial health port so the reconciler can launch+supervise them) with a slim `compose` = **rat + Postgres + MinIO**; then **socket-mount** (containerize rat: podman CLI + host socket + the in-network endpoint tweak) as the final refinement. (Project delivery via `rat apply` and the dbt→shared-DuckLake (Q2) remain orthogonal follow-ons.)

---

## 2026-06-03 — 2c: the Python plugin images are baked 🐳

The launchable images for every platform plugin (ADR-022) — so rat can `podman run` each as its own container (no per-plugin compose service). A `Dockerfile` per plugin (build context = repo root; `.dockerignore` loosened to allow `plugins/` + `platform/`, junk excluded) + `make plugin-images`:
- `rat/state:dev` (161M) · `rat/catalog:dev` (214M) · `rat/engine:dev` (443M) · `rat/scheduler:dev` (153M) · `rat/dbt-runner:dev` (324M) · `rat/bff:dev` (153M) — each `FROM python:3.12-slim`, copies the python SDK into site-packages + the plugin code, pip-installs its `requirements.txt`, `CMD python main.py`.
- The **dbt-runner** is special: dbt lives in its **own venv** (`/opt/dbtvenv`, `$RAT_DBT_BIN`) because dbt-core pins an older protobuf than the RAT SDK's 7.35 — verified live: the gRPC side runs **protobuf 7.35.0** + imports the SDK, AND **dbt 1.11.11** runs from the venv. The Go `rat/stateplugin:dev` (19M) from the prior step rounds out the set.

Verified **functional** (not just built): `rat/engine:dev` imports duckdb/pyarrow/numpy + the SDK + its plugin code; `rat/dbt-runner:dev` runs both sides. `make breaking` clean; additive (Dockerfiles + `.dockerignore` + a make target). **Next:** the slim launch-mode platform — a `plugins.yaml` (one entry per plugin → these images) + a compose that drops to **rat + Postgres + MinIO** (rat launches the rest); the scheduler/bff (drivers, no served port) get a trivial health port so the reconciler can launch+supervise them; prove the medallion runs through the launched stack. Then **socket-mount** (containerize rat) as the refinement.

---

## 2026-06-02 — 2c (first step): rat launches a SEPARATE plugin container — the decoupled loop, proven 📦

The reliable, decoupled-launch proof (the path chosen over a blind socket-mount): **rat launches a plugin as its own container** from a plane. `core/testplugins/stateplugin/Dockerfile` bakes a launchable `rat/stateplugin:dev` image (a static Go binary, alpine, runs under the podman runtime's I9 profile); `core/cmd/rat/plane.podman.yaml` is a `runtime: podman` plane that launches it. Proven live (rat on the host, the proven podman runtime — no socket-in-container yet): `rat serve --plane plane.podman.yaml` →
- `launching 1 plugin(s) via the "podman" runtime` → `wired rat-state -> 127.0.0.1:45043` → `gateway serving`;
- a **separate container** (`localhost/rat/stateplugin:dev`) is running, launched by rat;
- `ratctl call rat://state/v1/get` → routes to it; the value decodes to `pid=1 key=k1` — **pid 1 proves it ran inside the container**, not as a host process;
- `put` → `PermissionDenied` (C5); SIGTERM → drained → the container terminated.

So **adding a plugin = one plane entry + an image; rat launches it as its own container; the infra carries no per-plugin service** — the ADR-022 model, decoupled + reconciler-driven (self-healing), reached *reliably* (uses the already-proven podman runtime; `make stateplugin-image`). `make breaking` clean; additive (no proto/axis). **Next:** bake the Python plugin images (engine/catalog/dbt-runner/…) + a slim `plugins.yaml`/compose so the *platform* stack drops to rat + Postgres + MinIO; then **socket-mount** (containerize rat: podman CLI + the host socket + the in-network endpoint tweak) as the focused final refinement.

---

## 2026-06-02 — 2b: the launch-mode daemon — `rat serve` launches + supervises + self-heals 🚀

`rat serve`'s launch path is now **reconciler-driven** (ADR-022): rat is the **sole launcher**, not a static one-shot. `core/cmd/rat`:
- **`rewire.go`** — `gatewayRewire` implements `reconciler.Rewire`: `Bind` dials the plugin's endpoint + `gateway.SetProvider` (closing any prior conn); `Unbind` → `RemoveProvider` + close; `Close` for shutdown.
- **`main.go`** — `assemble` now returns a mode-agnostic `runningPlane` (gateway + teardown). The launch branch is **`launchPlane`**: build the registry + an **empty** gateway → run the **reconciler** over the desired set with the `gatewayRewire` → the reconciler launches each plugin and, on Healthy, dials + wires it; on crash it relaunches + re-wires (self-healing). It waits for the initial set to be Healthy so the gateway is wired before serving (same "ready when serving" semantics). `BringUp`'s static launch is **replaced** (no double-launch); attach mode is untouched.

**Proven:** `make core-serve-smoke` — the daemon boots the stateplugin **via the reconciler**, routes a C5-authorized call, denies an undeclared one, and SIGTERM drains (stop loop → terminate instance → GracefulStop). Full `make core-test` green (cmd/rat, ratctl, gateway, reconciler, supervisor) + `breaking` clean; gofmt clean; additive (no proto/axis). Self-heal-on-crash is unit-proven (`TestRewireOnRelaunch`) + now wired into the live daemon.

So `rat serve` launches plugins, keeps them healthy, and re-wires routing when one relaunches — a real orchestrator. **Next:** apply this to the *platform* — a `plugins.yaml` + the **socket-mount** deployment-runtime so the compose stack drops the per-plugin services to just **rat + Postgres + MinIO** (ADR-022's "adding a plugin = one line"), then the **secret plugin**.

---

## 2026-06-02 — reconciler re-wire hook — a relaunched plugin self-heals routing 🔁

The launch-mode wiring's core mechanism: the reconciler now drives the gateway re-bind across a crash. `core/reconciler`: a `Rewire` interface (`Bind(name, endpoint)` / `Unbind(name)`) + an optional `Config.Rewire`; the reconciler calls **`Bind` when a plugin goes Healthy** (initial launch OR a crash-relaunch on a *new* endpoint) and **`Unbind` when a Healthy plugin is lost** — keeping the reconciler decoupled from the gateway (the daemon wires `Rewire` → `gateway.SetProvider`/`RemoveProvider`). Test `TestRewireOnRelaunch`: a healthy plugin is Bound at ep1 → crashes → Unbound → relaunches → Bound at ep2 (ep1 ≠ ep2) — routing self-heals automatically. `make core-test` (reconciler + gateway green; an unrelated `arrowticket` timing flake passes on re-run) + `breaking` clean; gofmt clean; additive (no proto/axis).

With `gateway.SetProvider` (done) + this hook, the **self-healing re-wire path is complete + tested**. **Next (the launch-mode daemon assembly):** wire `cmd/rat` to run the reconciler with a `gatewayRewire` adapter (Bind = dial + `SetProvider`; Unbind = `RemoveProvider` + close) as the **sole launcher** (replacing `BringUp`'s static launch — avoiding the Phase-A double-launch), so `rat serve` launches plugins, keeps them healthy, and re-wires on crash — then a `plugins.yaml` + the socket-mount runtime ([ADR-022](../docs/architecture/adrs/022-plugins-are-launched-not-composed.md)).

---

## 2026-06-02 — `gateway.SetProvider` re-bind DONE — the keystone three threads waited on 🔑

The provider-connection gap I first flagged in Phase A (and parked twice) is closed. `core/gateway/gateway.go`: the `providers` map is now guarded by a `sync.RWMutex`; `New` **owns** (copies) the map; new **`SetProvider(name, conn)`** (bind/re-bind, returns the previous conn to close) + **`RemoveProvider(name)`**; `openCall` reads via a read-locked `provider()` accessor. So the gateway can **re-wire a provider's live connection at runtime** — concurrency-safe against in-flight Invoke/relay. Test `TestSetProviderRebind` (core/gateway): a call routes to conn A → `SetProvider` swaps to conn B → the same call routes to B → `RemoveProvider` → Unavailable. `make core-test` + **`go test -race ./gateway`** + `breaking` green; gofmt clean; additive (no proto/axis, `rat/2.0` untouched).

This single change **unblocks three threads at once**: (1) the reconciler **hot-restart re-wire** (Phase-A sre#-adjacent finding — a relaunched plugin's new endpoint), (2) **launch-with-lifecycle** ([ADR-022](../docs/architecture/adrs/022-plugins-are-launched-not-composed.md) Q5), and (3) **runtime plugin self-registration** ([ideas/inbox.md](../ideas/inbox.md) — add a provider while serving). The gateway is now mutable; wiring the supervisor/reconciler to call `SetProvider` on (re)launch is the next step toward the launch-mode platform.

---

## 2026-06-02 — ADR-022 PROPOSED: plugins are launched, not composed 🔌

The second architectural trigger from Tom: *adding a plugin should be almost nothing* — no compose service per plugin. [ADR-022](../docs/architecture/adrs/022-plugins-are-launched-not-composed.md) (Proposed): the ADR-019/020 platform was built in **attach mode** (compose starts every plugin, rat connects), so the **infra grows one ~15-line compose service per plugin** — backwards. Fix: rat **launches** plugins (it already can — [ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md): "the core launches"). **Adding a plugin = one entry in `plugins.yaml`** (`name`, `image`, `needs`, `secrets`, `config`); rat does launch → inject config → fetch secrets (from a **secret plugin**) → wire deps → healthcheck → connect → register. The deployment-runtime is **socket-mount locally** (rat-in-a-container drives the host container socket — the docker/k8s-daemon model Tom pointed at) and **Kubernetes in prod** (rat → the API; no socket, no DinD). The infra shrinks to a fixed bootstrap (**rat + Postgres + MinIO + secret plugin**) that does *not* grow per plugin; secrets live in the secret plugin, never in the infra. **Surfaces a load-bearing dependency (Q5):** launch-with-lifecycle needs a concurrency-safe `gateway.SetProvider` re-bind (the Phase-A reconciler-rewire finding + the parked runtime self-registration idea). Design only. **Next: ratify ADR-022 (+021), then rebuild the platform to launch mode + the secret plugin + the gateway re-bind.**

---

## 2026-06-02 — ADR-021 PROVEN (experiment): real dbt, orchestrated by rat 🧪✅

First working slice of the [ADR-021](../docs/architecture/adrs/021-orchestrator-pipelines-as-code.md) vision — **rat orchestrates a real dbt project**, on `phase-2-dbt-runner`:
- **`plugins/runner/dbt-duckdb`** — a **dbt-runner plugin** (reuses the frozen `strategy/v1/apply` axis for the experiment). On Apply it runs `dbt build` on a project — **dbt owns the DAG, `ref()`, Jinja, materializations AND tests; rat reinvents none of it.** dbt runs as a subprocess from its **own venv** (dbt-core pins an older protobuf than the RAT SDK's 7.35 — isolated behind a binary boundary).
- **`platform/dbt-project/`** — a standard dbt project (the user's *code*): `dbt_project.yml`, `models/{bronze,silver,gold}.sql` with `{{ ref() }}`, `models/schema.yml` native tests (not_null/unique), and one `rat.yaml` (`kind: pipeline, runner: dbt, schedule`).
- Wired into the stack (the `pipeline` service → the dbt-runner; its manifest now requires nothing — dbt-duckdb + DuckLake are the engine+catalog, in-proc).
- **Proven live:** the scheduler fired `strategy/apply` into the dbt-runner; `dbt build` ran the medallion — `bronze_orders → silver_orders → gold_daily_revenue` + 4 tests — **`PASS=7 ERROR=0, Completed successfully`**. Every fire audited (`rat-scheduler → strategy/apply → rat-pipeline`); run history recorded the failed early runs (the "lake" errors → `status: failed` — the quality/error path) and the successful ones (`status: success`).

This validates the core ADR-021 model: **rat orchestrates capabilities and a schedule; the pipeline is a dbt project (code); the language is a plugin.** `make breaking` clean (the dbt-runner reuses the strategy axis — no proto change). **Known limit (Q2):** the experiment materializes to a *local* DuckDB (`dbt-duckdb`'s `attach` can't pass DuckLake's `DATA_PATH`, and `on-run-start` runs after relation-binding) — wiring dbt's output into the *shared remote DuckLake* (so other plugins/UI see the tables, via a lake-connection capability) is the next step. Other follow-ons: the dedicated `pipeline/v1/run` axis; `rat apply` (project upload) instead of a mount; per-project cron from `rat.yaml`.

---

## 2026-06-02 — ADR-021 PROPOSED: rat as a pure orchestrator — pipelines as code (dbt) 🧭

A fundamental rethink with Tom after the ADR-020 first build felt "shitty vs. v2." The diagnosis: ADR-020 S1–S4 proved the v3 *plumbing* (plugins through the gateway, self-driving, quality-gated, run history) but **baked the pipeline into the infra** (a hardcoded medallion, the model list in a compose env, one global interval) — not the *code-driven* platform v2 was (project-as-code: dbt-shaped models + config + tests + per-pipeline cron; you edit files, the platform runs them). [ADR-021](../docs/architecture/adrs/021-orchestrator-pipelines-as-code.md) (Proposed) redirects the pipeline/project model: **rat orchestrates *capabilities* and never knows what a "pipeline" is; your data work is a dbt project (code) you `rat apply`; the infra declares only plugins.** Key moves: the **pipeline *language* is a plugin** (a `pipeline-runner` axis — `dbt-runner` first, `python-runner` later — so rat reinvents no DAG/`ref()`/Jinja/tests; dbt does); **plugin deps = capability composition** (`requires`→`provides`, no new core magic); three KISS schemas (plane = plugins only · a project = standard dbt + one `rat.yaml` · the manifest's provides/requires). Keeps ADR-020's decoupled stack/scheduler/state/gateway; **replaces** its bespoke "model-list strategy" (Q02). Design only — no build yet; open questions Q1–Q5 (project delivery via `rat apply` vs git-watch; a lake-connection capability; the `pipeline/v1/run` contract; the python metadata SDK; project-as-desired-state). **Next: ratify ADR-021, then build the `dbt-runner` reference.**

---

## 2026-06-02 — ADR-020 S4b (UI control-path) DONE: the UI's backend routes through the real gateway 🖥️

The portal-replacement's backend is now the **real orchestrator**. New [`platform/bff.py`](../platform/bff.py): a thin JSON-over-HTTP backend (a `kind: ui` driver, `platform-bff`) that a VS Code / web UI talks to, and that issues every **control** call to `rat serve` as a capability invocation (C5 + audit) — the honest minimum of the F9 split (control through the gateway; the bulk data-leg/row-preview would attach its own engine, out of scope here):
- `GET /api/health` → `{ ok, gateway }` · `GET /api/runs` → the run history (`rat://state/v1/list` + `get`) · `POST /api/run` → trigger a refresh (`rat://strategy/v1/apply`).
- Wired into the compose stack (`bff` service on host `:8088`). **Proven live via curl:** `/api/runs` returned `runs/000001/2` and `/api/run` triggered a refresh (`snap-12`) — every hop audited as caller `platform-bff` (`→ state/get`, `→ state/list`, `→ strategy/apply`).

`make breaking` clean; no Go/proto change. **What this IS:** the UI's control path now flows through the real gateway (the ADR-019 Phase-B-step-4 / ADR-020 S4 intent), proven without the VS Code UI (which can't run headlessly). **What remains (follow-on):** the **VS Code extension UI itself** — the existing `plugins/ui/vscode-rat` is experiment-shaped (semantic search over reviews), so a platform UX (medallion layers + run history) pointed at this BFF, run interactively, is the next step; the bulk data-leg (table/row preview) is the F9 follow-on.

**🎉 ADR-020 S1–S4 complete (core + backends):** v2's platform, rebuilt on the v3 plugin core with DuckLake as catalog — **decoupled stack · self-driving scheduled refresh · quality-gated commits · run history · a UI control-path through the gateway** — every hop authorized + audited, CLI/BFF not a portal. From a sealed library to a running, self-driving, quality-gated data platform.

---

## 2026-06-02 — ADR-020 S4 (state-backend) DONE: the platform has run history 📋

The platform now records + serves its own metadata — v2's `runs` table, as a **state-backend plugin** behind the gateway (on `phase-2-state`). New [`plugins/state/postgres-py`](../plugins/state/postgres-py/): a Postgres-backed `kind: state-backend` plugin implementing the frozen state/v1 **Get/Put/List** (monotonic revisions + single-key CAS via `if_revision`), reusing the stack's Postgres (a `rat_state` KV table). Wired into the platform:
- the **scheduler** records a run record per fire — `rat://state/v1/put` `runs/<tick>` = `{tick, status, snapshot, error}` (Q04 resolved: reuse the stack's Postgres);
- the **runner** reads the history back through the gateway — `rat://state/v1/list` (prefix `runs/`) + `get`.
- **Proven live:** `make platform-up` → the scheduler self-drives and records runs; `make platform-run` lists them: `3 run(s) recorded; runs/000001 {"status":"success","snapshot":"snap-4"} …`. Every state hop audited (`rat-scheduler → state/put`, `platform-runner → state/list/get → rat-state`).

`make breaking` clean; no Go/proto change (S4 is a plugin + wiring). **Remaining in S4 (S4b):** repoint **`vscode-rat`** at the live `rat serve` gateway (browse the medallion layers + run history, edit models, run/observe) — the bigger TS effort (the control path via the gateway's Connect SDK; the F9 data-leg/row-preview stays on the BFF). With S1–S4 the platform is **v2's core, on v3 plugins, DuckLake catalog**: decoupled stack · self-driving scheduled refresh · quality-gated commits · run history — all through the gateway, audited.

---

## 2026-06-02 — ADR-020 S3 (quality gates) DONE: tests block the commit ✅🚦

The pipeline strategy now runs **data-quality tests** that gate the catalog commit — v2's "tests block the merge", on DuckLake (on `phase-2-quality`). After building the layers + flushing, `sql-pipeline-py` (since removed — superseded by the dbt-runner, ADR-021) runs each `project/tests/*.sql`; a test that returns rows is a violation, and **any violation blocks the commit** (the strategy raises `FAILED_PRECONDITION` *before* `catalog.commit-table`, so the published snapshot pointer stays at the last good one).
- **The F9 dodge:** each test runs as `CREATE OR REPLACE TEMP TABLE _rat_qt AS <test>` — so `rows_affected` **is** the violation count, needing no Arrow row-pull (the in-proc data leg, F9, never enters the picture).
- **Proven live:** the self-driving stack passes both tests each tick and commits (`quality …: pass (0 violation(s))`); injecting a deliberately-failing test gated the very next tick — `rat-pipeline: quality _demo_failing.sql: FAIL (2 violation(s))` → `scheduler tick → error: FAILED_PRECONDITION quality gate failed`, **no commit**. Demo test removed after.

`make breaking` clean; no Go/proto change. **Remaining in S3 (follow-ons):** (a) **merge strategies** beyond full_refresh — incremental on a `unique_key`+watermark (the incremental-embed strategy already shows the shape); (b) **read-isolation** — v2's Nessie branch-on-failure-discard so readers never see un-passed data; DuckLake's model is snapshots/time-travel (not git branches), so this needs a DuckLake-branching investigation. S3 today delivers quality-GATED COMMITS; full read-isolation is the richer form. **Next: those S3 follow-ons, or S4 — state-backend + VS Code.**

---

## 2026-06-02 — ADR-020 S2 DONE: the platform is SELF-DRIVING — scheduled refresh through rat ⏰

The always-on stack now refreshes **on its own**, no command needed — v2's `ratd` scheduler→runner, decoupled into v3 plugins behind the gateway. Two pieces (both proven live on `phase-2-scheduler`):
- **S2a — the pipeline as a capability** (`plugins/strategy/sql-pipeline-py` (removed — superseded by the dbt-runner, ADR-021)): the medallion runner promoted to a `strategy` plugin (Q02). On `rat://strategy/v1/apply` it runs bronze→silver→gold via `rat://engine/v1/execute` and commits the gold snapshot via `rat://catalog/v1/{register,commit}-table` — **all back through the gateway** (it dials `RAT_GATEWAY`, names no concrete plugin). The audited chain: `platform-runner → strategy/apply → rat-pipeline → engine/catalog` — exactly v2's `ratd → runner → engine`, now per-hop C5-enforced. `run.py` is now just the manual trigger (one `strategy.apply`).
- **S2b — the self-driving clock** ([`plugins/scheduler/cron-py`](../plugins/scheduler/cron-py/)): a `kind: scheduler-backend` driver that fires `rat://strategy/v1/apply` on an interval (demo: every 20s; a real plane: hourly). Proven: `make platform-up` → the scheduler fires on its own — tick 1 → snap-4, tick 2 → snap-8, tick 3 → snap-12 (a fresh DuckLake snapshot each refresh, 3 gold Parquet snapshots on S3), every fire audited as caller `rat-scheduler`. *(A minimal active trigger; the full scheduler-backend axis — `Schedule`/`Cancel`/`WatchDue`, a clock the orchestrator watches — is the richer form, noted as a follow-on.)*

Plus a `minio-setup` one-shot in `compose.yaml` (provisions the lake bucket at stack-up, so the pipeline writes whether triggered by the scheduler or the manual runner). `make breaking` clean; no Go/proto change (S2 is all plugins + compose). **Next: S3 — merge strategies + quality gates (branch-on-failure-discard).**

---

## 2026-06-02 — ADR-020 S1 DONE: the decoupled platform runs the medallion through rat serve 🥉🥈🥇

The always-on stack runs **for real**. `make platform-up` brings up the data platform via `podman compose`: **Postgres** (DuckLake metadata) + **MinIO** (S3 data) + the **DuckDB engine** + the **DuckLake catalog** (each a sibling container) + **`rat serve`** — which runs in its own container and **attaches** to the plugins by service name (the S1a attach mode; **no docker-in-docker**). `make platform-run` then runs the medallion through the **real gateway**:
- `bronze/orders.sql` (read_csv the landing zone) → 9 rows · `silver/orders.sql` (lowercase status, drop null-key rows, dedupe, completed-sales only) → 4 rows · `gold/daily_revenue.sql` → 2 rows (2026-05-01 = 59.98, 2026-05-03 = 179.49). All correct.
- Every layer issued as `rat://engine/v1/execute` **through the gateway** (C5-authorized + audited); the gold snapshot committed to the **DuckLake catalog** via `rat://catalog/v1/{register,commit}-table` — **6 audited control hops** in `rat serve`'s log.
- **Parquet for all three layers landed on MinIO/S3** (`/data/rat/platform/main/{bronze,silver,gold}_*`); metadata on Postgres. Verified by reading the gold mart back from the lake.

This is **v2's pipeline, rebuilt on v3 plugins, with DuckLake as the catalog** — exactly the ADR-020 S1 proof. New: `platform/compose.yaml` (the always-on stack), the attach-mode `platform/plane.yaml`, env-driven `platform/run.py` (the medallion runner over the gateway), small additive entrypoint tweaks to the engine (S3 secret from `RAT_S3_*` env, before lake attach) + catalog (`RAT_DUCKLAKE_EXTENSIONS` for the remote httpfs/postgres/ducklake set), `make platform-{up,run,down}`. `make core-test` + `breaking` green; the proto/axis surface is untouched. **Next: S2 — the scheduler plugin (self-driving cron refresh).**

---

## 2026-06-02 — ADR-020 RE-AIMED: v2 rebuilt on v3 — always-on, scheduled, DuckLake catalog 🎯

After studying `ratatouille-v2` carefully with Tom, ADR-020 was sharpened (same-day, pre-implementation) from the initial *local one-shot* framing to the correct one: **rebuild v2's data platform on the v3 plugin core** — same behavior (landing → medallion → quality-gated **scheduled** refreshes), every responsibility **decoupled into a v3 plugin** behind the gateway, **DuckLake as the catalog** (replacing v2's Nessie/Iceberg), **VS Code + `ratctl`** replacing the portal. **Always-on + self-driving:** `rat serve` 24/7 + a **scheduler plugin** firing hourly refreshes; state remote (DuckLake-on-Postgres + S3). The ADR now carries the **v2→v3 component mapping** as its spine (`ratd`→`rat serve`, scheduler→scheduler plugin, runner→engine+pipeline-strategy, ratq→engine query, portal→vscode-rat+ratctl, postgres→state-backend, minio→storage, **nessie→DuckLake**) and a re-aimed **S1–S4** build order (S1 decoupled remote stack via attach mode · S2 scheduler · S3 merge strategies + quality · S4 state-backend + VS Code). Q02 resolved (the runner becomes a **pipeline strategy plugin**, capability-invocable so the scheduler can fire it). Roadmap synced. **Next: S1a — attach mode** (`supervisor.Attach` + the `endpoint:` path), the keystone for the always-on stack.

---

## 2026-06-02 — ADR-020 ACCEPTED: the data platform bundle — Phase 2 starts 🎯

[ADR-020](../docs/architecture/adrs/020-data-platform-bundle.md) (Accepted). Tom set the Phase-2 vision: a single `platform/` folder = a generic, batteries-included data platform — a **landing zone** (raw CSV) → **medallion** (bronze→silver→gold) of editable SQL/Python models → **data-quality tests**, run through `rat serve`, edited via `vscode-rat` + `ratctl`. The v2 product (`ratatouille-v2`: portal + landing-zones/merge-strategies/query-service) rebuilt on the v3 plugin core, **web portal replaced by VS Code + CLI**. Decision: the folder + conventions (medallion, models-as-files, gateway-executed pipelines, `project/tests` quality), built in four working slices — **M1** scaffold + local medallion demo → **M2** containerize (attach-mode `compose up` + Postgres/MinIO) → **M3** data-quality → **M4** VS Code. Core stays six things (all conventions are project/plugin-level — no temptation logged). Recorded the F9 (in-proc Arrow leg) + cross-container-sharing constraints that order the build, and Q01–Q03 (dbt timing, runner home, quality-as-axis-vs-convention). Branches: `phase-2` (integration, off `phase-1`) + `phase-2-platform-bundle` (topic). **Next: build M1.**

---

## 2026-06-02 — `ratctl` — a client connects to the orchestrator (the kubectl to `rat serve`) 🐀🎛️

On `phase-1-adr-019-phase-b` (off `phase-1`). A conversation with Tom reframed the goal: `rat` is an **orchestrator service** that many UIs (CLI, VS Code, webapp) connect to and drive — and a client connecting is **orthogonal** to how plugins got registered, so it needs no plugin-pipeline work. Built the first real client, as a **separate binary** (clients are not subcommands of `rat` — the orchestrator is one thing, clients another):
- **`core/cmd/ratctl/main.go`** — `ratctl call <capability> --as <caller> [--data '<protojson>'] [--addr host:port]`. Fully generic: resolves capability→method+request/response types from the linked axis descriptors (`protoregistry.GlobalFiles`), builds the request from protojson, dials the gateway and issues the command with the call-context envelope (traceparent C1 + caller identity C5), prints the response as protojson. Surfaces a C5 deny as a `PermissionDenied` status.
- **`core/cmd/ratctl/ratctl_test.go`** (`make ratctl-smoke`) — brings up a state plane in-process, serves the gateway over TCP, drives it with `ratctl`'s `run()`: authorized `get` routes to the launched plugin (response decodes, value pid-tagged); undeclared `put` → `PermissionDenied`. The **client→orchestrator** path proven end to end.

`make core-test` + `core-serve-smoke` + `ratctl-smoke` + `breaking` green; gofmt clean; additive, no proto/axis. **Decision recorded:** kept the declarative `rat serve --plane` model (not the runtime self-register model Tom floated) — parked self-registration in [`ideas/inbox.md`](../ideas/inbox.md) (needs an ADR + the same mutable-provider core change as the Phase-A reconciler-rewire gap; a scale feature, premature pre–Gate-B).

---

## 2026-06-02 — ADR-019: `rat` runs in a container — the control-plane daemon image 🐀📦

On `phase-1-adr-019-rat-serve`. Tom's steer: the control plane should run **in a containerized environment** (the same `rat` binary runs bare-metal *or* in a container — the k8s/docker-daemon shape). So the ADR-019 **Phase-C daemon-image** piece was pulled forward (architecture unchanged — just packaging):
- **`core/Dockerfile`** — multi-stage: a `golang:1.25` builder produces a **static, CGO-free** `rat` binary (+ the Phase-A `stateplugin`), copied into a minimal **non-root** `alpine:3.20` runtime (non-root is mandatory — the local-process runtime refuses root per I9). Builds from the repo root (the core module's `replace` target is `contracts/sdks/go`); `.dockerignore` scopes the context to `core` + `contracts` (excludes the ~59M `plugins/`).
- **`core/cmd/rat/plane.container.yaml`** — the Phase-A demo plane baked at `/etc/rat/plane.yaml`, so `podman run -p 7777:7777 rat/serve:dev` serves a working gateway out of the box; override by mounting your own plane.
- **`make rat-image`** target.

**Proven now:** `make rat-image` builds; `podman run` boots the daemon in-container (the launched stateplugin comes up via local-process *inside* the container, non-root, the gateway serves on `:7777`), and `podman stop` (SIGTERM) drains cleanly ("signal received — draining" → "drained"). `make core-test` + `core-serve-smoke` + `breaking` still green; additive, no proto/axis. **Note:** this is the *daemon-image* slice; the rest of Phase B (the data-dev Python plugins mediated by the gateway) and the full Phase-C compose stack (attach mode + MinIO/Postgres) remain — deferred per Tom's "keep current architecture, just containerize rat" steer.

---

## 2026-06-02 — ADR-019 Phase A BUILT: `rat serve` — the core runs as a server 🐀🛰️

On `phase-1-adr-019-rat-serve` (off `phase-1-data-dev-plane`). **The first time the sealed Phase-1 core runs as a daemon a client can connect to** — not a library wired up in a test. New `core/cmd/rat/` (a `main` package, **additive** — touches no sealed/tested package, doesn't move `rat/2.0`):
- **`main.go`** — `rat serve --plane plane.yaml`: parse the plane → pick the deployment-runtime (`local` default / `podman`) → `supervisor.BringUp` (launch + healthcheck + dial + register + gateway, the blessed one-call assembly) → `grpc.NewServer` + `corev1.RegisterCapabilityInvokeServiceServer` + `net.Listen("tcp", addr)` + `Serve` → block on SIGINT/SIGTERM → `GracefulStop` (drain in-flight) → `plane.Shutdown` (close conns + kill instances).
- **`plane.go`** — the `plane.yaml` schema + `LoadPlane`: `addr`/`runtime`/`health_timeout`/`plugins[]`; per-plugin **launch** (→ `LaunchSpec` with the full I9 profile) | **endpoint** (attach — accepted in schema, fails loudly as "Phase C") | **neither** (a register-only driver, the `Launch==nil` path, so C5 can authorize the calls it makes). Name must equal `manifest.metadata.name`; manifest/image paths resolve relative to the plane file.
- **`auditor.go`** — `StdoutAuditor` implements `gateway.Auditor`: one mutex-serialized JSON line per decision (allow/deny) + per stream-close → the ADR-001 mandatory-audit invariant holds with no audit-log plugin installed.
- **`descriptors.go`** — the union of axis `File_*` descriptors the gateway routes (state/catalog/engine/format/storage/strategy).
- **manifests + `plane.example.yaml`** — `rat-state` (the `stateplugin` as a launched provider, get+put) + `rat-caller` (register-only, requires get only).

**Exit criteria PROVEN** by `core/cmd/rat/serve_test.go` (`make core-serve-smoke`) — builds the daemon + plugin, runs the **real binary** over TCP, drives it with a real gRPC client: ✅ authorized `rat://state/v1/get` routes to the launched plugin (C5 allow + audit line) · ✅ undeclared `rat://state/v1/put` → `PERMISSION_DENIED` (C5 deny + audit line) · ✅ **SIGTERM drains cleanly** (exit 0, "drained" logged, no leak). `make core-test` + `make core-serve-smoke` + `make breaking` all green; gofmt clean; no proto/axis touched.

**Finding (Phase-A surfaced, deferred to backlog):** the ADR's step-5 reconciler crash-restart loop is **not** wired into the daemon. `supervisor.BringUp` constructs the gateway with a **fixed** provider-conn map (`gateway.New` has no provider re-bind setter), so a reconciler restart would relaunch a plugin on a *new* endpoint the gateway can't re-dial — and running the reconciler over the same desired set would **double-launch** (BringUp already brought them up). Phase A is therefore boot-once + serve + drain (exactly what the exit criteria test); hot crash-restart needs a small additive `gateway`/supervisor change (a `SetProvider`/adopt path) — captured in backlog, out of Phase-A scope (sealed package). **Next: ADR-019 Phase B** (containerize the data-dev Python plugins; run them through the real gateway).

---

## 2026-06-02 — ADR-019 ACCEPTED: `rat serve` daemon + beginner compose stack (Phase 2 kickoff)

[ADR-019](../docs/architecture/adrs/019-rat-serve-daemon.md) finalized **Accepted** and rewritten to be **executed cold by a fresh session** — Implementation map (exact APIs: `supervisor.BringUp`, `manifest.Load`, `gateway.Auditor`, the `File_rat_*` descriptors, `corev1.RegisterCapabilityInvokeServiceServer`), a per-phase runbook, and a kickoff checklist. Closes the gap the data-dev experiment surfaced (F9 / "why not the core gateway?"): the sealed core is a **library, not a server**. Resolves all 7 prior open questions into firm decisions (local→podman; containerize Python plugins **image-only, no proto change**; stdout auditor; binary at `core/cmd/rat/`; build now as **Phase 2 kickoff**, not Gate-B-blocked; attach-mode health-checks-not-restarts; compose stack at `deploy/data-dev-starter/`). Two runtime modes — **launch** (solo) vs **attach** (compose orchestrates → no docker-in-docker). Build order **A** (daemon vs Go test plugins — core first runs) → **B** (data-dev plugins via the real gateway) → **C** (`compose up` beginner stack). Roadmap threaded: phases.md (Phase 2 kickoff), current.md (active next = Phase A), backlog promoted. **Next: build Phase A.**

---

## 2026-06-02 — vscode-rat v0.2.0: multi-environment RAT explorer (many connections)

On `phase-1-data-dev-plane`. The VS Code extension now manages **many named RAT connections** (like a DB explorer manages many servers) — `{name, url, tenant?}` persisted in the `ratDataDev.connections` setting, the tree **connection-rooted** (connection → tables → snapshots; health → plugins), with per-connection Run Pipeline / Query / Search and Add/Edit/Remove. One editor, N planes (local / staging / prod / per-tenant / remote); unreachable planes degrade gracefully. Each connection is just a URL → point it at a **remote** gateway/core. The "one UI, many planes" scalability story made concrete. New `src/connections.ts`; compiles clean; repackaged → `vscode-rat-0.2.0.vsix` (`make data-dev-vsix`). Idea + follow-ons (gateway *remote mode* to target a real remote S3+Postgres plane; per-connection auth/tenant identity) captured in [`ideas/inbox.md`](../ideas/inbox.md).

---

## 2026-06-02 — Data-dev plane build step 6 DONE: the VS Code UI — the experiment is END-TO-END

On `phase-1-data-dev-plane`. Build-order §11 step 6 — a VS Code extension as a UI client of the data-dev plane, closing the multi-UI vision (CLI / web-portal / **VS Code**). With this the experiment spans **storage → catalog → engine+ML → strategy → UI**, local AND remote. EXPLORATORY + **ADDITIVE**: `make breaking` clean, conformance unchanged (34/34), sealed `rat/2.0` surface untouched.

- **`plugins/ui/vscode-rat`** — TypeScript VS Code extension: DuckLake catalog tree (tables→snapshots, click-to-preview), **Run Pipeline** (incremental-embed strategy), SQL query grid, **🔍 semantic search**, plugin-health view. Compiles clean under strict `tsc` (verified in a node:22 container → `out/*.js`).
- **`gateway/app.py`** + **`make data-dev-gateway`** (`scripts/data-dev-gateway.sh`) — a stdlib-only Python BFF that owns the in-proc engine+catalog+strategy, seeds + runs the strategy at boot, and serves a JSON API (`/api/{health,tables,snapshots,query,search,pipeline/run}`). Its `selftest.py` exercises every endpoint over HTTP; verified host-facing over the published port (curl: health/tables/search/pipeline all correct, incremental 12→15).
- **Finding F9 (README §10):** the bytes/control split means a UI needs a data-leg helper — `engine.Query` returns an out-of-band `ArrowStream` and the reference engine's leg is in-proc (a Flight stand-in), so an external client can't pull rows over the wire. Hence the gateway BFF. The frozen **control** capabilities are exactly what the connectionless Connect TS SDK (ADR-018) calls directly; a real Flight engine would retire the BFF. Honest deployment reality, not a contract gap.
- **🎉 The data-dev plane experiment is now end-to-end** — 5 new plugins (`minio-s3`, `ducklake-py`, `duckdb-ml-py`, `incremental-embed-py`, `vscode-rat`) + a gateway, composing a real scalable ML lakehouse on the sealed `rat/2.0` core **without changing one byte of the frozen wire**. Steps 2/3/4/6 done; step 5 (full compose) is covered by the `make data-dev-{local,remote,strategy,gateway}` targets. The practical Q02 substitute (principle #8) has produced its findings (F1–F9). **Next: a synthesis writeup of what held / what bent, and decide which findings feed back into the contracts or a future ADR.**

---

## 2026-06-02 — Data-dev plane build step 4 DONE: a real incremental-embed ELT strategy

On `phase-1-data-dev-plane`. Build-order §11 step 4 — a genuine incremental ELT as a `kind: strategy` plugin, composing capabilities through the invoke gateway (names no concrete plugin). EXPLORATORY + **ADDITIVE**: `make breaking` clean, conformance unchanged (34/34 — the strategy, like fullrefresh/scd2, has no `harness_test.py`; it's exercised by its runner), sealed `rat/2.0` surface untouched.

- **`plugins/strategy/incremental-embed-py`** — the §5.4 pattern: register/own target → CTAS schema-from-source → **server-side watermark** stage (only-new rows, no Arrow round-trip) → **MERGE** upsert → **embed only `embedding IS NULL`** → `ducklake_flush_inlined_data` → `commit-table` (idempotency_key = run id). `REQUIRES = (get-table, register-table, engine.execute, commit-table)` — **no `format` capability** (the engine writes the lake directly).
- **`run-strategy.py`** + **`make data-dev-strategy`** (`scripts/data-dev-strategy.sh`) — strategy→gateway→engine+catalog over gRPC, 3 runs: **run 1 embeds 12** (full load), **run 2 embeds 3** (only the newly-landed delta — incrementality), **run 2 replay embeds 0 / already_applied** (C1 idempotency). New batch-2 rows rank top in search (#15 "weekend trip", #13 "fingerprint sensor"), confirming the incremental embed landed. Assertion-bearing.
- **Finding F8 (README §10):** a strategy in a DuckLake world writes through the **engine** (not a format plugin) and addresses tables by lake-qualified name — plugin-agnostic in *binding*, DuckLake-aware in *addressing*. The watermark is server-side, so the strategy is pure `execute` + a final snapshot. **Next: §11 step 6 — `vscode-rat` (the VS Code UI via the connectionless TS SDK).** (Step 5, the full compose/`make data-dev-plane`, is largely covered by the local/remote/strategy runners + their make targets.)

---

## 2026-06-02 — Data-dev plane build step 3 DONE: the pipeline goes REMOTE (S3 + Postgres)

On `phase-1-data-dev-plane`. Build-order §11 step 3 — data moves to **S3/MinIO**, DuckLake metadata to **Postgres**, and the engine's S3 creds are **vended by a storage plugin**. The same pipeline runs distributed with **search distances byte-identical to local** — the data plane is unchanged when storage goes remote (the "swap a plugin, the rest holds" thesis). EXPLORATORY + **ADDITIVE**: conformance **34/34** (minio-s3 joined), `make breaking` clean, sealed `rat/2.0` surface untouched.

- **`plugins/storage/minio-s3`** (`ca13589`) — `kind: storage` plugin, **first impl of the Q02 5c read/write split**. Two minters (`creds.py`): `ScopeReceiptMinter` (offline, passes `storage-v1` golden vectors) + `MinioSTSMinter` (real `AssumeRole` with an inline policy scoped to `s3://bucket/<tenant>/<prefix>/*`). Tenant from `rat-callmeta-bin` (ADR-007, C7 anti-forgery). Verified against live MinIO: read creds read `acme/*`, denied cross-tenant `globex` + denied writes (least-privilege).
- **`run-remote.py`** + **`make data-dev-remote`** (`scripts/data-dev-remote.sh`, `compose/compose.yaml`) — boots MinIO + Postgres, vends WRITE creds → engine `CREATE SECRET S3` + `ATTACH ducklake:postgres (DATA_PATH s3://…)` → create→register→transform→embed→**flush(Parquet→S3)**→snapshot→commit→🔍search→idempotent-replay→D3-isolation. Assertion-bearing; Parquet verified on S3; D3 cross-tenant denial verified.
- **Enabling edits (additive, defaults unchanged):** engine `_EXTENSIONS` += `postgres`; engine `Engine(secret_sql=…)` runs `CREATE SECRET` before ATTACH; catalog `Catalog(extensions=…, secret_sql=…)` for the remote lake.
- **Findings (README §10):** F3 ✅ resolved by Postgres (real concurrent writers); F4 ✅ resolved by `ducklake_flush_inlined_data`; **F6** the catalog needs **no S3 creds** (metadata-only — bytes/metadata split falls out cleanly, sharper least-privilege); **F7** STS isolation is real object-store policy, not just the RAT capability layer. **Next: §11 step 4 — `incremental-embed-py` strategy (watermark→merge→embed-only-new→index→snapshot).**

---

## 2026-06-02 — Data-dev plane build step 2 DONE: the DuckDB heart runs local end-to-end

On `phase-1-data-dev-plane`. Build-order §11 step 2 complete — the DuckLake catalog + DuckDB-ML engine, with a **local end-to-end transform→embed→search running green over real gRPC**. EXPLORATORY + **ADDITIVE**: `make breaking` clean, conformance **33/33** (was 32; the new engine joined), the sealed `rat/2.0` surface untouched — **no proto, no new axis** (the "ML is an engine extension" thesis, README §3, proven in code).

- **`plugins/engine/duckdb-ml-py`** — the `duckdb-py` engine extended with `vss`/`ducklake`/`httpfs` (best-effort load) + an **`embed(text, model) → FLOAT[]`** UDF (`embed.py`: pluggable `hash-256` default / `minilm` / `ollama:*` seam). `Execute` now surfaces the DuckLake snapshot in `WriteResult.snapshot_id`. Still a conformant engine: passes **engine-real-v1** AND a new **[`engine-embed-v1.json`](../contracts/conformance/engine-embed-v1.json)** deterministic embed golden (dim 256 + exact nonzero buckets + L2-norm).
- **`plugins/catalog/ducklake-py`** — a DuckLake-backed `catalog/v1`: `GetTable`/`CommitTable` resolve+record the **real** lake snapshot; branches are a thin tracker (the §10 Q2 spike). On a `selftest.py` (frozen catalog-v1 parity deferred), not yet in the auto-conformance matrix.
- **`experiments/data-dev-plane/run-local.py`** / **`make data-dev-local`** — boots both plugins over gRPC sharing one DuckLake; runs create→register→transform→`embed()`→snapshot→commit→🔍 semantic-search→idempotent-replay on a 12-row real corpus; **assertion-bearing** (search ranking checked). Resolves the **§4/§10(b) catalog/engine-boundary tension** (engine writes, catalog records the snapshot).
- **Findings folded into README §10:** F1 DuckLake rejects fixed `FLOAT[N]` → embeddings as `FLOAT[]`, HNSW needs a derived non-lake table (brute-force cosine on the lake); F2 list UDFs need numpy; F3 DuckLake sqlite metadata is single-writer → catalog uses short-lived read connections (Postgres at scale); F4 DuckLake inlines small writes (flush for Parquet); F5 `snapshot_time` pulls pytz (avoided). **Next: §11 step 3 — `minio-s3` + S3 wiring (data goes remote).**

---

## 2026-06-02 — Data-dev plane experiment KICKED OFF (exploratory) — design doc

`experiments/data-dev-plane/README.md` on `phase-1-data-dev-plane` (`5d55371`). The **practical substitute** for the (impractical-for-a-solo-dev) Q02 external review: prove the platform by composing a real, scalable, **end-to-end ML lakehouse** workflow from plugins (principle #8 — "test the deployment topology"). EXPLORATORY + changeable; **ADDITIVE** (no new axis, no contract change, `make breaking` untouched). Stack: `minio-s3` (remote S3) · `ducklake-py` ([DuckLake](https://ducklake.select/docs/stable/) catalog, subsumes format) · `duckdb-ml-py` (engine **+ ML as DuckDB extensions** — `embed()` UDF + `vss`, **NO new proto**) · `incremental-embed-py` (a real ELT strategy) · `vscode-rat` (VS Code UI via the connectionless TS SDK). The doc documents every plugin + manifests + schemas + the exact SQL composition + the pluggable embed backend (hash-256 / minilm / ollama-on-HAL-9000) + scalability + the catalog/engine-boundary tension & open questions + the build order. **Next (fresh session): build the DuckLake catalog + DuckDB-ML engine heart (build order in the doc §11).**

---

## 2026-06-02 — PU-2 DONE: keystone context-carriage two-reference conformance → punch-list COMPLETE

`c0508a6` on `phase-1-pu2-keystone-conformance` — the last pre-unfreeze gate item (ADR-017 PU-2). The keystone context-carriage contract (`common/v1/context.proto` + ADR-007 gateway stamping — the carrier for C1/C2/C3/C5/C7/C8, the most-irreversible frozen surface) had the **weakest** conformance of the freeze: one impl (the spike Go gateway); the ADR-003 two-reference rule never reached it (architect F1, maintainer-conceded). PU-2 applies that forcing function:
- **[`contracts/conformance/context-carriage/context-carriage-v1.json`](../contracts/conformance/context-carriage/context-carriage-v1.json)** — 12 portable golden vectors: C1 (missing/ill-formed traceparent, missing correlation → reject); `caller_plugin` **re-derived** not propagated (the C3 namespace guarantee); trace **verbatim**; `SubjectAssertion` verification (bad signature / unknown key_id / wrong `bound_correlation_id` / expired); and the **M4 bare-mirror cross-check** (tenant + principal mismatch → reject, by reconstructing the signed bytes from the bare mirrors).
- **`go/` + `py/`** — two clean-room, technologically-divergent reference impls (no shared code; neither shares code with `core/gateway`). **`make context-carriage`** cross-runs both → **12/12 each, identical accept/reject + reason on every vector.** The keystone is now two-impl-conformed; the contract is implementable from the prose alone, in two languages.

🎉 **With PU-2, the ENTIRE ADR-017 pre-unfreeze punch-list is COMPLETE** (PU-1 + PU-2 + PU-3 + PU-4 + the 5a/5b/5c seams — all landed + verified, `make breaking` clean throughout). The **sole remaining condition** before the freeze can leave local/unpushed is the **real Q02 external human review** (ADR-013 Q02). ADR-017 stays Proposed pending that.

---

## 2026-06-02 — ADR-018 COMPLETE: Python connectionless (protoc-35 hybrid) — all 4 SDKs BSR-free

Resolved the python blocker (Tom chose the protoc-35 hybrid) — `2ee749e` on `phase-1-adr-018-python`. `contracts/codegen/Dockerfile.python` pairs **standalone protoc v35.0** (the MESSAGES → `ValidateProtobufRuntimeVersion(7,35,0)`, matching buf's `protocolbuffers/python` and the refs' `protobuf==7.35.0` — **no downgrade**) with **grpcio-tools 1.80.0** (the gRPC stubs → `GRPC_GENERATED_VERSION 1.80.0`, matching the refs' `grpcio==1.80.0`). `gen-python.sh` runs both; `gen-sdks.sh` special-cases python (no standalone `protoc-gen-python` — messages are a protoc builtin).

The one-time migration (48 files) is benign — protoc-35 omits default `json_name`s buf serialized explicitly (protobuf computes the same defaults at runtime; `json_name` only affects JSON, not the binary wire) + the grpc stubs gain a version guard. **VERIFIED: `make conformance` 32/32 references conform** — every python ref runs on the hybrid SDK.

🎉 **ADR-018 rollout COMPLETE: Go + TypeScript + Rust + Python all generate connectionless — codegen is fully BSR-free.** The rate-limit friction that bit the ADR-017 cut is gone for every language.

---

## 2026-06-02 — ADR-018 rollout: Rust connectionless + 5c closed; Python blocked on version-skew

Continued the ADR-018 rollout on `phase-1-adr-018-rust-python`:
- **Rust ✅** (`9eeb014`) — `contracts/codegen/Dockerfile.rust` (rust:1-bookworm + buf + cargo-built `protoc-gen-prost`/`protoc-gen-tonic`). The latest protoc-gen-prost defaults to the **nested layout** (matching the committed structure) and keeps the **selective `Eq,Hash` derives**, so the one-time migration churn is just cosmetic attribute formatting (`x="y"` → `x = "y"`) — and the regen **closes the pending ADR-017 5c rust-storage gap** (`VendReadCredentials`/`VendWriteCredentials` now present). Rust has no Cargo project / no reference plugins (an unused artifact), so zero-risk. Rust codegen is now BSR-free.
- **Python ⏸️ BLOCKED (decision needed)** — the `grpc_tools.protoc` path (ADR-018 Alternative #3) works offline, BUT the latest **grpcio-tools 1.81.0 bundles protobuf 6.33.5**, while buf's `protocolbuffers/python` (the committed gencode), all **13 python refs** (`requirements.txt: protobuf==7.35.0`), and `scripts/conformance.sh` are pinned to **protobuf 7.35.0** — a *major*-version skew (6 vs 7). So `grpc_tools.protoc` produces a refs-INCOMPATIBLE SDK (a downgraded `ValidateProtobufRuntimeVersion(6,33,5)` guard). Connectionless python needs a **tradeoff**: (a) downgrade the whole pinned python stack (13 refs + conformance) to 6.33.5 + re-verify conformance, (b) a **protoc-35 + grpc-plugin hybrid** to match 7.35.0 connectionless, or (c) keep python on remote until grpcio-tools catches up to protoc 35. The attempt was reverted; python stays on **remote**.

Net: **Go + TypeScript + Rust connectionless** (3/4, all BSR-free); python is the one remaining, blocked on a real grpcio-tools-vs-buf version skew that's a tradeoff call.

---

## 2026-06-02 — ADR-018 connectionless codegen: Go + TypeScript landed (Rust/Python staged)

[ADR-018](../docs/architecture/adrs/018-connectionless-codegen-local-plugins.md) on `phase-1-adr-018-connectionless-codegen` — switch SDK codegen from **remote BSR plugins** (the ADR-017 rate-limit friction) to **LOCAL plugins** in pinned per-language toolchain images. `scripts/gen-sdks.sh` now dispatches per language (a local `rat-codegen-<lang>` image if `contracts/codegen/Dockerfile.<lang>` exists, else the stock buf image + remote plugins); `make gen-images` pre-builds them.
- **Go ✅** (`6e32223`) — `Dockerfile.go` (buf + protoc-gen-go v1.36.11 + protoc-gen-go-grpc v1.6.2, pinned to the committed headers). A connectionless `buf generate` reproduces the Go SDK **byte-for-byte — ZERO churn**.
- **TypeScript ✅** (`ec947ef`) — `Dockerfile.typescript` (node + protoc-gen-es v2.12.0 + protoc-gen-connect-es v1.6.1). **Zero churn**.
- **Rust ⏸️ staged** — the cargo plugins build fine, but bare `protoc-gen-prost@0.4.0` differs from the committed output in BOTH **layout** (flat `rat.<axis>.v1.rs` vs the committed nested `rat/<axis>/v1/…`) AND **content** (the committed adds `Eq, Hash` derives) → neoeinstein's buf-plugin config must be replicated before it's a clean swap. Rust has **no reference plugins** (unused), so deferred rather than forcing a messy diff. Follow-on: match the neoeinstein prost/tonic config (derives + nested layout), flip, and complete the pending 5c rust-storage regen.
- **Python ⏸️ staged** — ADR-018 **Open Q01**: no standalone `protoc-gen-python` (it's a protoc builtin); the `grpc_tools.protoc` fallback (Alternative #3) is the path.

Go + TS are now **BSR-free**; rust + python stay on remote plugins until their follow-ons. ADR-006's remote-plugin *mechanism* is superseded (layout unchanged).

---

## 2026-06-02 — Additive pre-publish cut LANDED (ADR-017 §Migration step 2)

Executed the ADR-017 additive cut on `phase-1-q02-additive-cut` (3 commits), **all verified additive** (`make breaking` clean vs sealed `main`; `make lint` / `compile-sdks` / `validate-manifests` 32/32; `make core-test` green for the demos; generation deterministic):
- **Cut 1 (`51234e6`)** — PU-1 (`ArrowStream.ticket` producer-channel-auth MUST), PU-4 (tenancy ISOLATION-ONLY; `DECISION_KIND_SHARING` advisory-not-enforced), 5b (`Event.signature`+`key_id`), PU-3 (`Listing.conformance_expires_unix_ms`+`revoked_capabilities`), 5a (`capabilityRef.revision`/`min_revision`). All SDKs regenerated.
- **Enforcement demos (`360cef1`, `make core-test` green, no ripple)** — PU-3 core: `Attestation.ExpiresAtUnixMs` (signed/tamper-evident) + `Authority.Revoke/IsRevoked` + `NewVerified` refuses revoked/expired conformance without rotating the key. PU-1 core: an **mTLS channel-auth conformance vector** proving a leaked ticket presented over the wrong authenticated channel with spoofed `X-RAT-*` headers is REFUSED (403, no bytes), + a contrast test characterizing the header-trusting stand-in being fooled.
- **5c (`a764155`)** — storage `VendReadCredentials`/`VendWriteCredentials` (mode-scoped capability URIs `…/vend-credentials-read|write`) so C5 authorizes read-vs-write; refs auto-compile via the `Unimplemented` embed. Additive.

**Known transient gap: the RUST storage SDK regen for 5c is PENDING** — buf.build's anonymous BSR rate-limit (the toolchain runs *remote* codegen plugins → 8 BSR calls per `gen-sdks` run) is exhausted, and the rust community plugins are remote-only. Go/Python/TypeScript regenerated; **rust has no reference plugins (an unused artifact)**. Complete with one `make gen-sdks` (or a `buf login`) when the window resets — also fixes the pre-existing python `class X(object)`→`class X` cosmetic drift Cut 1 folded in.

**Remaining for the pre-unfreeze gate:** finish the rust regen · **PU-2** (keystone context-envelope two-reference conformance — the separate larger piece) · then the **real Q02** external review → re-seal `rat/2.x`. (`ADR-017` stays Proposed until the real Q02.)

---

## 2026-06-02 — PU-4 ratified: v1 tenancy is isolation-only (ADR-017 Q01 resolved)

Tom's call on the one open fork in [ADR-017](../docs/architecture/adrs/017-pre-unfreeze-contract-amendment-gate.md): **v1 tenancy = isolation-only.** `DECISION_KIND_SHARING` becomes *advisory-not-enforced* in v1 (the axis stops advertising an un-actionable verb); actioned cross-tenant sharing + hierarchical tenancy defer to a future `v2` delegation primitive (its own ADR, only if a user pulls for it). The sharing-capable alternative (a pre-publish delegation primitive in `rat/1`) was rejected for v1 — Gate B unmet, no user pulling. **With Q01 resolved, the punch-list has no open forks left — only an all-additive pre-publish cut + the real Q02.** ADR-017 stays **Proposed** (its one remaining condition for Accepted = the real Q02 external review confirms/extends the gate).

---

## 2026-06-02 — ADR-017 (Proposed): pre-unfreeze contract-amendment gate

[ADR-017](../docs/architecture/adrs/017-pre-unfreeze-contract-amendment-gate.md), on `phase-1-q02-dryrun` — operationalizes the Q02 dry-run synthesis into the explicit gate the freeze must pass **before it ever leaves local/unpushed**: publish only after **(a)** the punch-list resolves **AND (b)** the real Q02 external review runs. Punch-list: **PU-1** bytes-leg producer channel-auth MUST (+ vector), **PU-2** keystone context-envelope two-reference conformance (*qualifies ADR-015's "freeze validated" claim*), **PU-3** attestation expiry/revocation, **PU-4** tenancy isolation-only-vs-sharing (**a DECISION for Tom — Q01**), + 3 decide-now additive seams (semantic-skew negotiation, `Event` signing, vend read/write split). Status **Proposed** → Accepted once PU-4 is ratified + the real Q02 confirms/extends it. Explicitly scopes **OUT** the availability cluster (AV-*) + ecosystem (EC-*) — those gate multi-tenant-production / adoption, not freeze-publish.

---

## 2026-06-02 — Q02 SIMULATED dry-run: 5-agent deliberating panel → synthesis + pre-publish punch-list

Ran the Q02 review brief end-to-end as a **simulated** panel using the Claude Code agent-team feature (a `q02-panel` team of named teammates with `SendMessage` cross-talk) — 4 lens-reviewers (architect/security/sre/ecosystem) **+ a defending maintainer**; AI personas, *not* humans, on `phase-1`. Reviewers verified claims against the real `core/`+`contracts/` code (`file:line` cites), cross-examined each other and the maintainer live, then each filed `reviews/11-q02-<lens>.md`; the maintainer filed a defense log. Chaired the synthesis into [`reviews/Q02-tracker.md`](../reviews/Q02-tracker.md) (new "Synthesis — SIMULATED dry-run" section).

- **30 raw findings → ~26 deduped.** Tallies: architect 9, security 7, sre 7, ecosystem 7; maintainer **12/13 conceded, 1 mixed, 0 bluffs** ([defense log](../reviews/11-q02-maintainer-defense-log.md) — incl. an explicit net-new-vs-already-tracked triage).
- **Freeze-reopen verdict: 0 hard · 3 soft** (all additive, all fixable in the still-local window) — **PU-1** bytes-leg producer-channel-auth MUST (filed by security **and** architect, 2 lenses), **PU-3** attestation expiry/revocation, **PU-4** tenancy sharing scope-or-delegate. Plus **PU-2** (the keystone context-envelope has the *weakest* conformance of the frozen surface → qualifies ADR-015's "freeze validated" claim) + 3 decide-the-additive-now seams (semantic-skew negotiation, Event signing, vend read/write split).
- **Strongest positives:** the **security** lens *validated the sealed enforcement spine* (C5/C4/D4/D1 "real, not theater"); the **ecosystem** lens retired reviews/02's core fear ("the contracts don't exist") — "most author-respectful surface in the space." The **SRE** headline — *"the wire is right; the run-lifecycle code around it is where the 3am risk lives"* — re-confirms the reviews/09 dissent ("green certifies shapes, not obligations") with line-level evidence (incl. a 🔴 Critical: `core/lease` has no error channel → backend-blip step-down storm; free to fix now).
- **Net read (feeds Q01): GO / adjust-before-unfreeze** — no reviewer demanded a hard wire break or a reconsider-the-bet.
- **HONESTY:** every artifact carries a `SIMULATED` banner. This does **NOT** discharge Q02 — real external humans are still owed before the freeze leaves local/unpushed; the dry-run is a *baseline for them to falsify* + a *pre-publish punch-list*, weighted like reviews/00–08. The recruitment table in the tracker stays "not started."

Findings grouped into the backlog ([backlog.md](backlog.md) → "Q02 simulated dry-run findings"); the maintainer's net-new list is the authoritative triage. Next concrete artifact: a **pre-unfreeze punch-list ADR** (PU-1..4 + the decide-now seams).

---

## 2026-06-02 — front-door refresh: README + CLAUDE.md now reflect the sealed core

`README.md` + `CLAUDE.md`, on `phase-1-frontdoor-refresh`. Both still said *"architecture-only / not yet any product code"* — false since the Phase-1 seal. The entry point now states the real status (Phase 0 + 1 sealed, `rat/1.5` / `rat/2.0`; what the core enforces; Q02 the next gate), adds a "what's here" map (core/contracts/examples/…), and puts [`roadmap/current.md`](current.md) first in the reading order. A project is only as well-structured as its front door is accurate; the internals were already disciplined (ADRs, fresh roadmap, sealed+tagged git) — this fixes the one piece that lied. (No new structure added — the standing risk is meta-process accumulation, not under-structure.)

---

## 2026-06-02 — Q02 recruiting prep — shortlist + cover-note variants + findings tracker

Everything around running Q02 except the human step (recruiting), on `phase-1-q02-recruiting`:
- **[`reviews/Q02-reviewer-shortlist.md`](../reviews/archive/Q02-reviewer-shortlist.md)** — by-lens **profiles + sourcing pools** (not a contacts list), a selection checklist ("scars, not enthusiasm"; willing to disagree; no conflict), and how many/which (minimum viable = architect + security; + SRE comfortable; ecosystem only if adoption is the worry).
- **Per-lens cover-note variants** appended to [`Q02-outreach-note.md`](../reviews/archive/Q02-outreach-note.md) — a tuned "try to break X" opener per lens, each pointing at the matching brief.
- **[`reviews/Q02-tracker.md`](../reviews/Q02-tracker.md)** — a reviewer status table + a findings-doc template (→ `reviews/11-q02-<name>.md`) + a synthesis section that feeds the **Q01** v2-vs-v3 call (incl. a freeze-reopen-trigger check).

**Q02 is now fully teed up; the only remaining step is human — recruit the reviewer(s) + run it.** Freeze stays local/unpushed until the synthesis lands.

---

## 2026-06-02 — Q02 kit COMPLETE: tailored SRE + ecosystem + architect briefs (all 5 internal lenses covered)

Three more lens-tailored companions (parallel to the security one), each front-loading a real-vs-paper / settled-vs-open section + a lens-specific question set so the reviewer models the right system. With these the kit covers **all five internal review lenses** (security, SRE, ecosystem, architect/contracts) plus the general brief + outreach note.
- **[`reviews/Q02-brief-sre.md`](../reviews/archive/Q02-brief-sre.md)** (`phase-1-q02-sre`) — SRE/operability: the tier-0 **state-backend SPOF**, **diagnosability** across polyglot plugins (`rat diagnose`), **native `/metrics` + SLOs** (still paper — sre#8), single-leader **reconcile-loop capacity** + fairness, **upgrade/version-skew**, **DR/backup**, **resource-limit enforcement**, and a failure-mode catalog. Real-vs-paper: sre#4's crash-loop backoff + lease-thrash guard are DONE; most of [reviews/03](../reviews/03-operations-sre.md) remains open.
- **[`reviews/Q02-brief-ecosystem.md`](../reviews/archive/Q02-brief-ecosystem.md)** (`phase-1-q02-ecosystem`) — ecosystem/plugin-author: the existential **cold-start** problem (zero third-party plugins), **author DX** (the contract triple + conformance bar), **capability-negotiation as the differentiator**, **marketplace** as compatibility oracle + supply-chain trust, **versioning/skew** + **governance** of the `rat://` namespace, and author incentives. Real-vs-paper: contracts frozen + 30+ refs + D4-enforced conformance are real; the ecosystem itself + marketplace + signing + DX tooling + governance are paper. Don't re-flag what ADR-003 + D4 settled.
- **[`reviews/Q02-brief-architect.md`](../reviews/archive/Q02-brief-architect.md)** (`phase-1-q02-architect`) — architect/contracts: the **premise** soundness, six-thing-core **minimality + completeness**, **tier-0 honesty**, the **contract triple** + capability as the unit of composition, **frozen-wire regret** (which message/field forces a v2 — ArrowStream / RequestContext-in-metadata / the additive commit-linkage seam / the error model), capability-model **algebra** (provider selection, composition, granularity), and the cross-cutting **enforcement-layer** layering. Settled-vs-open: the wire is frozen (regret = a v2 break) + the premise is committed (Q02 is the gate to challenge it); ADR-003's two-refs + reviews/06–08 caught the obvious freeze-blockers — find the premise flaw + the *subtle* regret.

**Q02 kit COMPLETE (5 briefs + outreach):** [general](../reviews/archive/Q02-external-review-brief.md) · [outreach note](../reviews/archive/Q02-outreach-note.md) · tailored [security](../reviews/archive/Q02-brief-security.md) / [SRE](../reviews/archive/Q02-brief-sre.md) / [ecosystem](../reviews/archive/Q02-brief-ecosystem.md) / [architect](../reviews/archive/Q02-brief-architect.md). All five internal review lenses now have a front-loaded variant. **The only remaining Q02 step is the human one: recruit the reviewer(s) + run it** (freeze stays local/unpushed until then).

---

## 2026-06-01 — Q02 external-review kit drafted (brief + outreach note + security-focused brief)

[`reviews/Q02-external-review-brief.md`](../reviews/archive/Q02-external-review-brief.md) + [`reviews/Q02-outreach-note.md`](../reviews/archive/Q02-outreach-note.md) + [`reviews/Q02-brief-security.md`](../reviews/archive/Q02-brief-security.md), on `phase-1-q02-{brief,outreach,security}`. The recruiting kit for the owed **Q02 external peer review** ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) / [reviews/09](../reviews/09-phase-1-gate-review.md) dissent: zero external human review so far). The **brief** frames the premise, states what internal review already covered (so reviewers don't re-derive it), lists the load-bearing questions we most want challenged (premise / contracts-freeze / data-plane / operability / ecosystem / prior-art), the already-acknowledged residuals, a reading order, and a findings template + severity scale. The **outreach note** is the short, personalize-and-send recruiting message. The **security-focused brief** is a tailored companion that front-loads the trust model + a threat-model question set (the C2 channel-auth gap, I9 sandbox containment, the core-bypassing bytes-leg ticket, credential vending/tenancy, supply-chain + audit-signing) for a security reviewer. **Next on Q02: recruit reviewers** (OSGi/K8s/VSCode/Temporal-class practitioners) and run it; freeze stays local/unpushed until then.

---

## 2026-06-01 — 🎉🎉 PHASE 1 SEALED — `rat/2.0`

`phase-1` → `main`, tagged `rat/2.0` (annotated). All 9 board exit criteria met (C1, C3, C4, C5, D1, D2, D3, D4, sre#4 — see the entries below), each proven **against real launched plugins**, with the frozen wire intact (`make breaking` green throughout). The spike core grew into a real control plane: registry (+ conformance-verified `NewVerified`) · capability-invoke gateway (C5 authz + C4 audit + C3 deadline/idle) · two deployment-runtimes (local-process + podman full-I9) · supervisor · reconciler + leader-election lease · arrow-ticket bulk-leg gate · storage-cred isolation.

- **Seal mechanics:** `git merge --no-ff phase-1` into `main` + `git tag -a rat/2.0` (merge+tag, not commit — the `main`-guard hook permits it). Tags: `rat/1.5` = Phase 0, `rat/2.0` = Phase 1.
- **Freeze stays LOCAL/unpushed.** Owed before broad commitment / a push: **Q02 external peer review** (only internal adversarial review so far). Phase 2+ are **user-pull-gated** (phases.md Gate B: ≥10 real solo users) — not started.
- **Non-blocking residuals** (backlog): write-leg idempotency vs a real idempotent format ref (C1 residual); explicit cloud metadata-egress drop + structured `IsolationAttestation` (D-series GA); core audit signing + hash chain (C4/C8 GA, seeded by D4's ed25519).

---

## 2026-06-01 — 🎉 sre#4 — the reconciler (crash-loop backoff/jitter + leader election): PHASE-1 DoD COMPLETE (9/9)

`core/reconciler` + `core/lease` ([reviews/03](../reviews/03-operations-sre.md) §incident-runbooks → [reviews/09](../reviews/09-phase-1-gate-review.md) exit gate), on `phase-1-sre4-reconciler`. The 5th of the six core things, built greenfield with the sre#4 robustness baked in: **don't re-make the K8s CrashLoopBackoff mistake.** Level-triggered convergence (events are hints; each pass re-observes), one active replica via a lease.

- **`core/lease`:** a single-key linearizable CAS `Store` (models the state-backend's CAS, overview D5) + an `Elector` with the **lease-thrash guard** — a TTL margin keeps leadership across renewal-latency spikes (a delayed-but-in-margin renewal retains it), and a follower acquires only after genuine expiry (minimum-hold). Tests: two-contender mutual exclusion, thrash guard under a latency spike (no ping-pong), failover after a leader stops, continuous-term min-hold.
- **`core/reconciler`:** converges a desired plugin set via the deployment-runtime; a crashed/unhealthy plugin is restarted with **exponential backoff** (base·2ⁿ, capped) + injectable **jitter** + a **crash-loop cap** (→ Degraded after N, so it stops hammering the runtime); success resets the counter; a launch error crash-loops through the same path. `Loop` ties Elector + Reconciler on a jittered tick (only the leader converges). `testplugins/crashplugin` exits immediately (the real crash target).
- **Tests:** deterministic backoff schedule (1s,2s,4s,4s capped) + cap + no-hammer-after-Degraded + readiness + recovery-reset (fake runtime + injectable clock); a deterministic two-replica **leader + failover** (leader converges, follower idle, thrash guard, failover → new leader resumes); a **REAL end-to-end** (Loop + local-process): a healthy plugin converges while a genuinely crash-looping one is capped at Degraded. `go test -race` clean; `make core-test` + `make breaking` green (no wire change). Commit `5a350ce`.
- **🎉 MILESTONE — all 9 Phase-1 exit criteria met** (C5, C4, C3, C1, D1, D2, D3, D4, sre#4). The Phase-1 definition-of-done ([reviews/10](../reviews/10-phase-1-spike-exit.md)) is complete → the `phase-1` → `main` **seal (`rat/2.0`)** is ready to cut ([git-branching.md](../.claude/rules/git-branching.md)). Still owed before/around the seal: **Q02** external peer review.

---

## 2026-06-01 — C1 against real backends — idempotency survives a real backend crash

`core/composition` + `core/deploymentruntime` ([reviews/10](../reviews/10-phase-1-spike-exit.md) C1 exit), on `phase-1-c1-real-backends`. The crash-mid-strategy at-least-once idempotency was proven against the in-repo fakes (composition_test.go); C1-real re-proves it against the **real catalog refs**, whose commit-key ledgers are genuine (an in-memory map in inmemory-go; a **durable SQL table** in sqlite-py). The format leg can't be re-proven — the real inmemory-go format deliberately ignores `idempotency_key` — so C1-real rides on the catalog's `CommitTable`/`MergeBranch` (a documented gap: no real *idempotent format* ref exists yet).

- **Proof A** (`TestC1AgainstRealCatalogRetry`, core-test): the real inmemory-go catalog launched behind the gateway. A retry with the same `idempotency_key` is a no-op — `CommitTable` replay returns `already_applied` with the ORIGINAL snapshot **even when the retry's payload DRIFTED** (the key, not the payload, anchors the result); `MergeBranch` is idempotent under the same key too.
- **Proof B** (`TestC1DurableLedgerSurvivesRestartViaPodman`, podman-gated): the **gold-standard crash-safety proof** — the ledger survives a real BACKEND crash. The sqlite catalog runs under the podman runtime with a **persistent data volume**; commit under key K, then tear the catalog container DOWN (`Shutdown`) and relaunch a fresh one on the SAME durable db — a replay still returns `already_applied=true` (`snap-durable`). The durable SQL ledger outlived the crash, which an in-memory backend / our fakes fundamentally cannot.
- **Podman runtime:** added `Podman.DataRoot` — each launched plugin gets a persistent host dir (`<DataRoot>/<plugin_id>`) mounted at `/data` (`-v dir:/data:Z`, 0777 forced past umask so the non-root container uid can write), surviving `Terminate`+relaunch. Empty == ephemeral only (unchanged). The persistent peer to the `/tmp` tmpfs. (`go vet` caught a proto-by-value copy in a test helper → fixed to return pointers.) `make core-test` + `make core-test-podman` + `make breaking` green. Commit `583d799`.
- **Milestone:** 8 of 9 Phase-1 exit criteria cleared (C5/C4/C3/C1/D1/D2/D3/D4). **Remaining:** **sre#4** — reconciler crash-loop backoff + jitter + lease-thrash guard. Then the Phase-1 acceptance criteria are met → the `phase-1` → `main` seal (`rat/2.0`).

---

## 2026-06-01 — D2 — the ArrowStream ticket is the only gate on a real bulk leg

`core/arrowticket` ([reviews/10](../reviews/10-phase-1-spike-exit.md) D2 exit), on `phase-1-d2-bulk-leg`. The `Minter` (HMAC-signed, TTL'd, single-use, `{stream,caller,tenant}`-bound tickets) was proven at the unit level (reviews/10 "field sufficient"); D2's remaining half is **wiring it into a real out-of-band transfer**. The Arrow bytes leg **bypasses the core**, so unlike the control plane (gateway/C5) there is no mediator — the `ArrowStream.ticket` is the *sole* authorization.

- **The proof** (`bulkleg_test.go`): a Flight-shaped (DoGet) stand-in — a real `httptest` endpoint that streams the payload ONLY when the presented `ArrowStream.ticket` validates (via the `Minter`) against the presenting identity (caller/tenant — the spike's stand-in for the authenticated Flight channel; C2 tightens the source at GA) + this endpoint's stream. The frozen `commonv1.ArrowStream` carries endpoint+ticket. Vectors, **through the real transfer**: happy (exact payload received) · replay (single-use → 403, no bytes) · cross-binding (a leaked ticket from another tenant → 403 and NOT consumed, so the rightful holder still succeeds) · expired (past-TTL → 403) · tamper (mutated ticket → 403). On every rejection, no bytes leak.
- **Flake hunt:** the test failed once under concurrent `make core-test` load (HTTP keep-alive connection reuse — a classic `httptest`+`DefaultClient` flake class), so it now uses a dedicated keep-alives-disabled client (fresh connection per fetch, never the global client) — verified 5× under full concurrent load + 40× isolated + `-race`. `make core-test` + `make breaking` green (no wire change — exercises the frozen `ArrowStream` + the existing reference; `contracts/` untouched). Commit `af6e55c`.
- **Milestone:** 7 of 9 Phase-1 exit criteria cleared (C5/C4/C3/D1/D2/D3/D4). **Remaining:** **C1** *against real backends* (the crash-mid-strategy idempotency re-proven against a real idempotent backend, e.g. the sqlite catalog — so far only proven against fakes) · **sre#4** (reconciler crash-loop backoff/jitter).

---

## 2026-06-01 — D4 conformance attestation — the core verifies `declared == conformed` (ed25519)

`core/conformance` + `core/registry` ([reviews/10](../reviews/10-phase-1-spike-exit.md) D4 exit), on `phase-1-d4-conformance-attestation`. A plugin's manifest `provides` was **self-asserted** (plugin.v1.json: *"no enforcer exists yet"*). D4 makes it **derived**: the core trusts a declared capability only if a **signed conformance attestation** proves the plugin conformed it (marketplace.proto `conformed_capabilities`; format/v1 CONTRACT C6 — "capability declared is meaningless without capability conformed").

- **`core/conformance`:** `Attestation{PluginName, Conformed[], KeyID, Signature}`, signed by a conformance authority over a canonical form (plugin + **sorted** conformed caps + keyID, so the signature commits to the key id — key-substitution defense). `Authority` is the core's keyring (key id → ed25519 public key); `Verify` rejects unknown key ids + bad signatures. **The core's first real signature verification** — the unsigned audit record (C4) + isolation receipt are the GA-signing seeds; the keyID model mirrors `common/v1.AuditRecord.key_id` (rotation/agility via new key ids).
- **`registry.NewVerified(manifests, attestations, authority)`:** for every manifest that provides any capability, require an attestation that **verifies** AND **covers every provided capability**; refuse on missing / bad-signature / declared-but-not-conformed. A pure caller/driver (no `provides`) needs none. On success it delegates to `New`, so the gateway's C5 path is unchanged — it just can no longer be fed a self-asserted provider. (The full bring-up adopts D4 by building its registry via `NewVerified`.)
- **Tests:** genuine verifies; wrong-key / tampered-set / unknown-key-id rejected; `NewVerified` accepts a fully-conformed provider (and the registry then authorizes the cap) and refuses declared-but-not-conformed / forged / missing. `make core-test` + `make breaking` green (no wire change — the attestation is a core type, `contracts/` untouched). Commit `9e7edca`.
- **Milestone:** 6 of 9 Phase-1 exit criteria cleared (C5/C4/C3/D1/D3/D4). **Remaining:** D2 (real Arrow bulk leg — ticket TTL/single-use/binding) · C1 *against real backends* (so far only proven against fakes) · sre#4 (reconciler crash-loop backoff/jitter). *(Corrected count: C1-against-real-backends is still open — an earlier draft miscounted it as cleared.)*

---

## 2026-06-01 — D3 storage-cred isolation — scoped, tenant-isolated, contained (real local-fs ref)

`core/composition` ([reviews/10](../reviews/10-phase-1-spike-exit.md) D3 exit), on `phase-1-d3-storage-creds`. The storage axis's C7 obligation — *vended creds are scoped to the caller's tenant + prefix + mode, short-TTL, and a prefix can't escape the tenant root* — is now **vector-tested through the real launched plugin behind the gateway**, not honor-system.

- **The proof** (`composition_storagecreds_test.go`): the **round-2 real** `plugins/storage/localfs-go` ref (independent module) is launched via local-process (`RAT_STORAGE_ROOT=tempdir`) behind the gateway; `vend-credentials` flows through the C5 gateway and returns the JSON scope receipt. Asserted: **(1) scoping** — bound to (tenant, prefix, mode) + a TTL; **(2) tenant isolation** — `acme` and `globex` vend the SAME logical prefix but resolve to DISTINCT per-tenant roots (`…/acme/warehouse/orders` vs `…/globex/warehouse/orders`); **(3) containment** — `../globex/secrets` from `acme` → `PERMISSION_DENIED`; **(4)** empty prefix → `INVALID_ARGUMENT`; **(5) C5** — an undeclared caller is denied. The tenant comes ONLY from the gateway-re-stamped metadata envelope (not a request field).
- **Defense in depth, surfaced in the audit:** C5 authorizes the `vend-credentials` *capability*, then the storage plugin enforces tenancy *containment* — so the containment/validation refusals are the **provider's** (C5-allowed in the audit); only the undeclared caller is a C5 denial (the audit shows exactly 1). `make core-test` + `make breaking` green. Commit `7a8b386`.
- **C2 caveat (deferred):** the spike trusts the tenant claimed in the inbound envelope; the full core re-derives it from the authenticated channel — the scoping mechanism proven here is unchanged, only the source of the trusted tenant tightens. **Next DoD:** D4 conformance-attestation enforced · D2 real bulk leg · C1 against real backends · sre#4.

---

## 2026-06-01 — C3 streaming idle-timeout backstop — a hung provider can't pin a stream (gateway C-series complete)

`core/gateway` ([reviews/10](../reviews/10-phase-1-spike-exit.md) C3 exit), on `phase-1-c3-idle-timeout`. The deadline bound `min(channel, deadline_unix_ms)` already covered the deadline-SET case (unary + streams). The deferred gap (reviews/10 line 37) was a server-stream with **no** deadline: a provider that sends no frame, no EOF, and no error blocks `RecvMsg` forever and pins the stream. C3 adds the **idle backstop**.

- **The backstop:** `relayServerStream` runs the downstream stream under a cancelable `streamCtx` (child of `oc.ctx`, so the deadline bound still applies) with a `time.AfterFunc` idle watchdog reset on each frame. If no frame arrives within the idle window the watchdog cancels → `RecvMsg` returns → the cause is attributed: parent deadline/cancel (the C3 bound), the idle watchdog (→ `DeadlineExceeded` "stream idle timeout"), or a genuine provider error. `Gateway.StreamIdleTimeout` (default **5m**; generous because a legitimately quiet `watch` is normal — such providers should keepalive, or a deployment tunes it). `streamOutcome` gains a **"timeout"** label so an idle/deadline cut is legible in the audit trail (distinct from a provider error).
- **Tests:** a hung provider (N frames then blocks on `srv.Context().Done()`) is cut **promptly** with `DeadlineExceeded` + a terminal `{timeout, Frames:N}` record — by the idle watchdog when no deadline is set, and by the soft deadline when one is (< idle). `go test -race` clean (watchdog concurrency). `make core-test` + `make breaking` green (no wire change — C3 is an implementation backstop, not a contract). Commit `b9f22f1`.
- **Milestone:** with C3 done the **gateway C-series is complete** — C5 (capability enforcement, real providers) · C4 (audit every decision + terminal stream-close) · C3 (deadline bound + idle backstop) · C1 (crash-safety idempotency). **Next DoD:** D3 storage-cred isolation · D4 conformance-attestation enforced · D2 real bulk leg · C1 against real backends · sre#4.

---

## 2026-06-01 — C4 terminal stream-close audit record — the stream audit trail closes

`core/gateway` ([reviews/10](../reviews/10-phase-1-spike-exit.md) C4 exit), on `phase-1-c4-terminal-audit`. Per-decision audit + audit-on-deny were already real (the gateway records exactly one decision record per call, allow or deny). The missing half — the deferred C4 item — was the **terminal stream-close record**: ADR-008 enforces stream authz at OPEN, so a stream's *decision* is audited there, but nothing recorded how the stream **ended**. Now it does.

- **The terminal record:** when a server-stream closes, the gateway emits one terminal `AuditRecord` — `Outcome` ∈ {success, error, canceled}, `Frames` relayed, and the `Error` if any — so a stream that errors or is cut mid-flight (incl. by the C3 soft deadline) is never a silent gap. A stream **denied at open never opens**, so it gets only the deny decision record (no terminal). `AuditRecord` gained `Correlation` (the envelope's correlation_id) so a stream's open + close records link; `Terminal`/`Outcome`/`Frames`/`Error` carry the close. `Outcome` maps to the frozen `common/v1.AuditOutcome` at GA.
- **Refactor:** `openCall` now returns an `*openedCall` struct (ctx/method/conn/cancel + caller/provider/correlation) so the terminal record can correlate; `Invoke` is behaviour-unchanged; `InvokeServerStream` relays via `relayServerStream` (counts frames) then emits the terminal record.
- **Tests:** a streaming Watch provider drives both outcomes — clean stream → `[open allow, terminal success Frames=3]` sharing a correlation id; erroring stream → `[open allow, terminal error Frames=1, Error set]`; the deny-at-open test now also asserts *no* terminal record. `make core-test` + `make breaking` green. Commit `1ba9f18`.
- **Deferred (GA, not C4-blocking):** core signing + the hash chain on the canonical `common/v1.AuditRecord` (the spike uses a simplified in-memory record). **Next DoD:** C3 idle-timeout backstop · D3 storage-cred isolation · D4 conformance-attestation enforced · D2 real bulk leg · C1 real backends · sre#4.

---

## 2026-06-01 — C5 against REAL providers — enforcement holds beyond our fakes (Go refs + a SQLite container)

`core/composition` + `core/deploymentruntime` ([reviews/10](../reviews/10-phase-1-spike-exit.md) C5 exit), on `phase-1-c5-real-providers`. The spike enforced C5 against our in-repo fakes; this **extends the proof to genuine reference plugins** behind the supervisor + gateway. The manifest-derived authorization holds identically: declared caps route + return **real results**; a capability the real provider genuinely implements but the caller never declared is **denied + audited**.

- **Proof 1 — Go refs via local-process** (`composition_realproviders_test.go`): the full get-table → register → overwrite → commit-table pipeline runs through the canonical ADR-003 refs `plugins/{catalog,format}/inmemory-go` — built as **independent modules** (own `go.mod`), launched as isolated processes. Real results (the real catalog returns `catalog://warehouse.sales.orders@main`; the real format returns `snap-1`; commit-linkage holds). C5 then denies `format/merge` + `catalog/merge-branch` — caps the refs implement but the strategy never declared. 4 allow + 2 deny audited (C4).
- **Proof 2 — SQLite catalog via podman** (`composition_realpodman_test.go`): C5 against a **real-backend plugin in a real container** — the SQLite catalog ref `plugins/catalog/sqlite-py`, built into a `python:3.12-slim` image and launched by the **podman runtime under the full I9 profile**, behind the gateway. `get-table` + `commit-table` (declared) hit real SQLite and return real results; `merge-branch` (undeclared) is denied. Ties C5 + supervisor + the podman runtime together end-to-end. Gated by `RAT_PODMAN_TEST` → `make core-test-podman`.
- **podman runtime hardening:** add a writable `/tmp` tmpfs (read-only root + tmpfs is the canonical hardened pattern — lets a stateful plugin keep scratch, e.g. SQLite's WAL db, without weakening the read-only root) + `rm -f -t 0` on Terminate (no 10s SIGTERM grace). `make core-test` + `make core-test-podman` + `make breaking` green. Commit `6e66a24`.
- **Next:** remaining Phase-1 DoD — C4 terminal audit incl. denials, C3 idle-timeout backstop, D2 real bulk leg, D3 storage-cred isolation, D4 conformance-attestation enforced, C1 against real backends, sre#4.

---

## 2026-06-01 — 🎉 D1 COMPLETE: the podman deployment-runtime — full I9 profile, kernel-enforced

`core/deploymentruntime` + `core/testplugins/probeplugin` ([ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md) §4), on `phase-1-podman-runtime`. The second deployment-runtime reference and the one that **closes D1**: where `local-process` honors only the process-level I9 subset, **`Podman` ENFORCES the full profile at the kernel level** — closing the [reviews/08](../reviews/08-post-freeze-board-review.md) D1 honesty gap (the v1 refs *self-attest* `read_only_root_fs` while enforcing nothing). The board's literal exit criterion — "a real *enforcing* deployment-runtime (podman, not dry-run) passes a full-profile vector" — is met.

- **`podman.go`:** `Launch` maps the `IsolationProfile` 1:1 onto podman's real enforcement surface — `--user` (non-root), `--cap-drop=ALL`, `--security-opt=no-new-privileges`, `--read-only`, default/named seccomp, and `--network=bridge` to force a **private netns** (never inherit a host-network default — which would defeat metadata isolation *and* break port publishing; learned by dogfooding the nested env). Publishes the in-container port to an ephemeral host port; `Healthcheck` = running + endpoint-accepts + a **structured JSON isolation receipt** (CONTRACT.md shape — the receipt the honesty note wanted, not a free-form string); `Terminate` = `podman rm -f`.
- **`isolation.go`:** extracted the shared I9 trust gate (`checkI9Minimum`, the Go twin of the Python refs' `check_spec`) + the receipt types; `localprocess.go` now calls it.
- **`testplugins/probeplugin`:** an in-container prober that self-reports its sandbox (uid, CapEff, NoNewPrivs, root-writable, metadata-reachable), so the test proves the **kernel** enforced the profile — not merely that the runtime requested it. Static (CGO_ENABLED=0), runs `FROM scratch`.
- **`testimage/Dockerfile` + `make core-test-podman`:** a privileged go+podman image driving a **real nested `podman run`** under the full profile. Kept OUT of `core-test` (no podman in the plain go image → the live test SKIPs there).
- **Live proof** (`make core-test-podman` → `TestPodmanFullProfile` PASS): `uid=1000`, `CapEff=0000000000000000`, `NoNewPrivs=1`, root not writable (EROFS), `169.254.169.254` unreachable, `seccomp=RuntimeDefault`. `make core-test` green (live test skips; I9-gate + empty-image tests run); `make breaking` green (contracts/ untouched). Commit `4f3854e`.
- **Next:** the real process boundary now unblocks **C5 against real providers** + **D3** storage-cred isolation; the structured receipt seeds **D4** (conformance attestation). Remaining Phase-1 DoD: C4 terminal audit, C3 idle-timeout, D2 real bulk leg, C1 real backends, sre#4.

---

## 2026-06-01 — D1 steps 3–4: composition through launched providers — the cross-axis pipeline over isolated processes

`core/composition` + `core/testplugins` ([ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md)), on `phase-1-composition-launched`. The in-test `fakeCatalog`/`fakeFormat` are **promoted to standalone binaries**, and the full cross-axis pipeline is **re-run through the supervisor** — so catalog + format now serve from **launched, isolated child processes**, not in-process bufconn fakes.

- **Promotion (one impl, two topologies):** `testplugins/catalogsvc` + `testplugins/formatsvc` hold the fakes as importable packages (frozen RPCs + C1 idempotency + ADR-010 commit-linkage). The SAME impl backs both the in-process composition test (bufconn) and the launched `catalogplugin`/`formatplugin` binaries — no in-process-vs-binary divergence. Each tags a free-form response field with `os.Getpid()` (catalog→`TableRef.uri`, format→`WriteResult.snapshot_id`), mirroring `stateplugin`, so work is attributable to a distinct OS process. `runPipeline` refactored to drive the gateway client + return a response-only `runResult`, shared by both topologies.
- **Test:** `composition_launched_test.go` brings catalog+format up through the `local-process` runtime behind the gateway (`supervisor.BringUp`), then drives get-table → register → overwrite → commit-table through the LAUNCHED processes. **Distinct PIDs** (test/catalog/format all different, e.g. `4588/4689/4695`); **commit-linkage** holds across the boundary; **C5** still denies an undeclared `merge` (audited); **C1** crash-mid-strategy recovery is idempotent (replayed overwrite `already_applied`, written once, committed once). Commit `c37ce7b`; `make core-test` + `make breaking` green.
- **Next:** the **podman** runtime for the full I9 profile (read-only-fs / metadata-egress / seccomp) = **D1 complete**.

---

## 2026-06-01 — D1 step 2: the supervisor — the core brings plugins up as launched processes behind the gateway

`core/supervisor` ([ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md)), on `phase-1-supervisor`. `BringUp(runtime, specs, …)` Launches each provider via the deployment-runtime → waits healthy → dials the endpoint → registers; caller/driver specs (no `Launch`) are registered for their `requires` only; it then builds the registry + gateway over the launched providers. `Plane.Shutdown` terminates every instance + closes conns; a failed launch tears down what already came up. **Replaces the spike's dial-pre-running** — provider conns now come from isolated processes the core launched.

- **Test:** `BringUp` launches a real `stateplugin` via the local-process runtime; the gateway routes a C5-authorized `Get` to the **launched child** (distinct PID); an undeclared `put` → `PERMISSION_DENIED`; a below-I9 plugin aborts `BringUp`. Commit `61be935`; `make core-test` green.
- **Next:** promote the catalog/format fakes to standalone binaries → re-run composition-on-Go through launched providers; then a podman runtime for the full I9 profile = **D1 complete**.

---

## 2026-06-01 — D1 step 1: the `local-process` deployment-runtime — real child-process isolation, I9-enforced

First code of the committed full build's D1 ([ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md)), on `phase-1-local-process-runtime`. `core/deploymentruntime.LocalProcess` implements the frozen `DeploymentRuntimeService`:

- **Launch** execs `LaunchSpec.image` (a plugin binary) as a child OS process bound to a runtime-allocated loopback endpoint; **enforces the I9 minimum** — below `run_as_non_root + drop_all_capabilities + no_new_privileges` (or running as root, which can't honor non-root) → `FAILED_PRECONDITION`; empty image → `INVALID_ARGUMENT`.
- **Healthcheck** = PID liveness + endpoint readiness (HEALTHY / UNKNOWN / UNHEALTHY); **Terminate** kills the child's process group + reaps it.
- `core/testplugins/stateplugin` — a minimal standalone StateService binary the runtime launches (Get tags its own PID).
- **Test** (`go test ./core/...`): build the plugin → Launch → Healthcheck-until-HEALTHY → dial + Get **ran in a distinct child PID** → Terminate (then NotFound); + the I9-refusal + empty-image gates. Commit `c638202`; `make core-test` green.
- **Next:** the supervisor (manifests → Launch → dial → register) + composition-through-launched providers; then a podman runtime for the full profile = **D1 complete**.

---

## 2026-06-01 — ADR-016: plugin provisioning via the deployment-runtime axis (D1 opened)

First decision of the committed full build ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md)). [ADR-016](../docs/architecture/adrs/016-plugin-provisioning-via-deployment-runtime.md): the core **launches** plugins through the frozen `deployment-runtime/v1` axis (`Launch` → `{instance_id, endpoint}` → `Healthcheck` → dial → register → `Terminate`) instead of the spike's dial-pre-running shortcut. The deployment-runtime is **tier-0** (bootstrapped in-core; everything else launched through it — no 7th core thing). The D1 increment = a Go `local-process` runtime enforcing the process-level I9 subset (refuse below non-root / cap-drop / no-new-privs) + the in-test fakes promoted to standalone binaries + composition re-run through launched (distinct-PID) providers; the **podman** runtime (full profile: read-only-fs / metadata-egress) is the follow-on that **completes D1**. Registry/gateway interfaces (ADR-014) unchanged; frozen contracts untouched. Next: build the `local-process` runtime + the supervisor.

---

## 2026-06-01 — 🎯 Phase-1 commitment gate CLEARED — full core build committed ([ADR-015](../docs/architecture/adrs/015-phase-1-commitment-gate-cleared.md))

The decision [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) deferred to the spike's report. The spike validated the frozen contracts ([reviews/10](../reviews/10-phase-1-spike-exit.md)) — C5/C1/C3/D2 green via a real enforcer, `make breaking` clean, **no freeze-reopen** — and on that evidence Tom cleared the gate: **commit to the full Phase-1 core build.** The exploratory posture (held since pre-Phase-0) ends.

- **Scope:** clears the **Phase-0 → Phase-1** gate (full core build). The later user-pull gates stay hard — phases.md **Gate B** (≥10 solo users), **Gate C/D** — and **Q02** (external peer review) is still owed (schedule *during* the build).
- **Rationale (Q01):** the founding premise — v2's baked-in assumptions (postgres-mandatory, ratd-as-orchestrator, portal-as-only-UI) can't evolve into the everything-is-a-plugin thesis; v3 is the from-scratch design, now evidence-backed by the spike. Recorded in ADR-015.
- **Definition of done = the full Phase-1 acceptance criteria:** C5 (real providers), C4-terminal, C3 (idle-timeout backstop), D1 real isolation, D2 (real bulk leg), D3, D4-enforced, C1 (real backends), sre#4.
- **Next:** D1 — a real process-isolating deployment-runtime (the spike used in-process providers).

---

## 2026-06-01 — Spike CLOSED: C3 deadline + D2 ticket + CI + exit report — frozen wire HELD, no freeze-reopen

Closed the Phase-1 contract-de-risking spike ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) / [ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md)), on `phase-1-spike-closeout`.

- **C3 (provider deadline)** — `core/gateway` bounds the downstream call by `min(channel, deadline_unix_ms)`; a 2s-slow provider returns `DeadlineExceeded` in ~150ms (a hung provider can't pin the gateway). Test green.
- **D2 (ArrowStream ticket)** — `core/arrowticket`: an HMAC-signed, TTL'd, single-use, `{stream,caller,tenant}`-bound credential carried in `bytes ticket`; replay / expiry / cross-binding / tamper all rejected. Proves the frozen field suffices (producer-side; an SDK helper eventually). Tests green.
- **CI** — `make core-test` (build+vet+test `./core/...`, folded into `verify`) + `make breaking` (buf-breaking `contracts` vs `main`). Both run green; **`make breaking` confirms the spike touched no frozen contract.**
- **Exit report** — [reviews/10](../reviews/10-phase-1-spike-exit.md): C5/C1/C3/D2 all validated by a real enforcer; **no freeze-reopen triggered**; the board's "shapes-not-obligations" risk is materially reduced. The recommendation feeds Tom's deferred commitment-gate decision (ADR-013): **commit** / **continue-exploratory** both well-supported; the strategic v2-vs-v3 call (Q01) + external review (Q02) remain his.
- **NOT proven (= the full build, not freeze risks):** D1 real process isolation, D3 storage-cred, D4 attestation-enforcement, C4 terminal audit, sre#4 backoff.

---

## 2026-06-01 — Spike core: cross-axis composition-on-Go — C5 + crash recovery validated; the frozen wire SUFFICES

The spike's centerpiece, end-to-end ([ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md) §5), on `phase-1-composition`. `core/composition` drives the real pipeline (catalog `get-table` → format `overwrite` → catalog `commit-table`) through the Go enforcing gateway, a manifest per plugin, against Go providers honoring the frozen RPCs + idempotency contract.

- **`TestCompositionPipeline`** — the multi-axis pipeline runs; the catalog records exactly the snapshot the format produced (commit-linkage, ADR-010); 4 hops authorized + audited (C4).
- **`TestCrashMidStrategyRecovers`** (C1) — a strategy that crashes after the write but before `commit-table` recovers on an at-least-once re-run with the same run id: the replayed `overwrite` is a no-op (`already_applied`) → **no double-write**, exactly-once commit.
- **`TestCompositionDeniesUndeclaredMidPipeline`** (C5) — `merge` (undeclared) is denied mid-pipeline though the format provides it. `go build` + `vet` + `test ./core/...` PASS (`golang:1.25`). Commit `dfd6587`.
- **🔑 FINDING (de-risking — the spike's whole purpose):** the frozen wire **suffices** for crash-between-write-and-commit recovery via the existing `idempotency_key`/`already_applied` fields (ADR-012); the strategy axis did **not** need a commit/abort wire shape. **No freeze-reopen.** (Multi-output all-or-nothing atomicity stays the branch+merge primitive's job — a follow-on probe, not a strategy-level gap.)
- **Next:** lighter spike probes (C3 deadline, D2 ticket) + CI (`make core-test`) + the spike exit report → the deferred commitment-gate decision (ADR-013).

---

## 2026-06-01 — Spike core: the capability-invoke gateway — C5 enforced end-to-end at the wire

Second spike increment ([ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md)), on `phase-1-invoke-gateway`. `core/gateway` implements the `core/v1` `CapabilityInvokeService` (`Invoke` + `InvokeServerStream`), seeded from the faithful non-test `plugins/bench/latency-go/gateway.go` — but its **C5 decision is `registry.Authorize` (derived from declared manifests), audited per decision (C4)**, not the stubs' hardcoded allowlist. Routes `capability→method` from the `(rat.common.v1.capability)` annotation; relays opaque frames (passthrough codec); re-stamps identity + propagates traceparent (ADR-007); rejects a missing/ill-formed traceparent (C1).

- **Real gRPC enforcement test** (state axis, bufconn): an allowed `Get` relayed intact; an undeclared `put` + an unknown caller → `PERMISSION_DENIED`; a server-stream `watch` denied at open (ADR-008 enforce-at-open); a missing envelope → `InvalidArgument` before the decision. `go vet` + `go test ./core/...` **PASS** (`golang:1.25`). Commit `de34989`.
- **C5 is now real end-to-end** — the self-asserted stub is replaced by a decision derived from what plugins declare. Next: composition-on-Go (the full pipeline through this gateway) + the C1/C2 cases + CI.

---

## 2026-06-01 — Spike core: the registry foundation (C5 derived from real manifests) — `go test` green

First real Phase-1 spike code (ADR-014), on `phase-1-registry-core`. New Go module `github.com/rat-dev/rat/core`:

- **`core/manifest`** — loads the frozen `plugin.v1.json` manifest shape (the real `contracts/examples/*.plugin.yaml`) into Go structs + validates the `rat://<axis>/v<major>/<cap>` URI grammar.
- **`core/registry`** — indexes manifests by name + provided capability; **`Authorize(caller, cap)` allows iff `caller.requires ∋ cap ∧ provider.provides ∋ cap`** — the C5 decision *derived from declared manifests*, replacing the throwaway stubs' hardcoded allowlist. Rejects duplicate providers (no selection policy yet).
- **Tested green** (containerized `golang:1.25`, `go vet` + `go test ./...`, `GOSUMDB=off`): the allow path (`scd2→format/merge`) + 3 deny modes (undeclared-require / no-provider / unknown-caller) + duplicate-provider + malformed-URI, all against the 2 real manifests. Commit `fdcf780`.
- **Next:** `core/gateway` (`CapabilityInvokeService` seeded from `plugins/bench/latency-go/gateway.go`, C5 wired to `registry.Authorize` + an audit record per decision), then composition-on-Go + the C5-negative / C1 / C2 exit tests.

---

## 2026-06-01 — ADR-014: the spike-core shape pinned (registry + capability-invoke gateway)

Contracts-before-code for the Phase-1 spike. [ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md) scopes the minimum real core that makes **C5 real**: a Go **registry** (loads the real `plugin.yaml` manifests → indexes `(kind,name,version)` + a capability map; builds the `capability→(service,method)` route table from the `(rat.common.v1.capability)` annotation) + a **capability-invoke gateway** (seeded from the faithful non-test `plugins/bench/latency-go/gateway.go`) whose **C5 decision is *derived from the manifests*** — `X allowed iff X ∈ caller.requires ∧ X ∈ provider.provides` — not the test stubs' hardcoded allowlist. Reconciler/bus/identity/state-gateway/process-launch deferred; plugins run as local gRPC servers. Exit tests: composition-on-Go + C5-negative (`PERMISSION_DENIED` + audit) + C1 crash-mid-strategy + C2 truncation; a frozen-wire insufficiency = a freeze-reopen while still local. Lives in a new `core/` module (`replace` → the SDK). Next: build `phase-1-registry-core`.

---

## 2026-06-01 — Phase-1 commitment gate RE-CONFIRMED (13-agent board) → **time-boxed spike** ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) · [reviews/09](../reviews/09-phase-1-gate-review.md))

Before committing to the full Phase 1 core build, re-confirmed readiness + "did we miss anything?" via a 13-agent board workflow: an 8-area completeness audit → a 4-lens board → chair synthesis (audit on Sonnet, board+chair on Opus).

- **Verdict: proceed-with-conditions (strong-majority).** Engineering readiness independently re-verified *this session* (not trusted from the roadmap): `rat/1.5` verified, `make conformance` 32/32 + `make composition` + `make validate-manifests` 32/32 ran live, ADR-003's two-reference bar genuinely met on all 6 data-plane axes over real Arrow Flight, the one true v2-regret (`snapshot_id`) found+fixed pre-publish, the biggest gap (B1) absorbed additively (ADR-010).
- **"Did we miss anything?" — no.** All 8 audit areas `minor-gaps`; **nothing was dropped from [reviews/08](../reviews/08-post-freeze-board-review.md)**. Two items elevated: **sre#4** (reconciler crash-loop backoff) promoted backlog → explicit Phase-1 AC; the **commitment gate** (governance).
- **Decision (Tom): a time-boxed 2–4 week contract-de-risking spike** ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)) — stand up a minimal real registry + capability enforcer and actively try to break a frozen contract (C5 + crash-mid-strategy + C3/D2), freeze kept local so any regret is cheap. The 12–18mo runway commitment is **deferred to the spike's exit report**.
- **Dissent preserved** (business lens WAIT, high): 3-day/112-commit project, zero soak, self-asserted conformance, no external review — green certifies *shapes*, not *obligations*. The spike buys evidence before the bet.
- **Roadmap reconciled** (board condition #8): C1 clarified (fields done `rat/1.5`; enforcement is Phase 1), "D1–D5"→"D1–D4" (D5 done), deliverable counts corrected (24 protos / 32 refs / 18 CONTRACT.md / SDKs Go·Py·Rust·TS, Java dropped), stale "(Staged; commit pending.)" notes cleared.

---

## 2026-06-01 — Branching discipline landed — `main` / `phase-N` / `phase-N-<slug>` + a `main`-guard hook

As Phase 1 begins, codified "always work on a nice branch" (Tom's ask). Planned by the `claude-engineer` agent; built-in-first (a rule + a hook guard, no new agent/skill).

- **[`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md)** (always-load) — topology (`main` = sealed line tagged `rat/N.M`; `phase-N` = long-lived integration; `phase-N-<slug>` = short-lived topic branches merged back `--no-ff`), naming, merge rules, tag convention.
- **`main`-guard** added to `.claude/hooks/contracts-check.sh` — blocks direct `git commit` on `main` (exit 2); the phase-seal `git merge`/`git tag` path is unaffected. **Verified both ways** (blocks on `main`, passes on a working branch).
- **Mechanics:** renamed `master` → `main` (local-only; no remote configured); forked `phase-1` from the `rat/1.5` sealed commit.
- **Two bugs caught by dogfooding the model on contact** (CLAUDE.md #8 — test the topology, not the feature): (1) the guard was first committed only on `phase-1`, so it was absent on `main` where it's needed → landed the infra on `main` as the baseline (FF); (2) git can't create `phase-1/<slug>` while a branch `phase-1` exists (ref directory/file conflict) → switched sub-branches to a **hyphen** (`phase-1-<slug>`). Both fixes documented in the rule.

---

## 2026-06-01 — Phase 0 close-out (4/4): **`rat/1.5` cut — 🎉 PHASE 0 SEALED** (C1/C2 crash-safety folded in, [ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields.md))

The final close-out item. Folded the two cheap additive crash-safety fields into the seal (while the surface is local/unpublished), then cut `rat/1.5` over the complete Phase-0 contract surface.

- **[ADR-012](../docs/architecture/adrs/012-crash-safety-additive-fields.md) — additive crash-safety fields.** **C1** (write-leg idempotency): `idempotency_key` on `format` Append/Overwrite/Merge + `strategy.ApplyRequest`, `already_applied` on `WriteResult` — the data plane now has **one** idempotency model across the commit leg (ADR-010) and the write leg. **C2** (stream completeness): `optional expected_rows`/`expected_batches` on `ArrowStream` — a truncated transfer is detectable; the consumer MUST fail the write, closing the silent SCD2-history-corruption path. Additive (`buf breaking` FILE clean); SDKs regenerated.
- **Demonstrated end-to-end** in [plugins/composition](../plugins/composition): the full-refresh strategy threads `idempotency_key` → a reconciler **retry** of every combo is a no-op (`already_applied=true`, no double-write — verified across all 4 combos, incl. the datafusion engine whose bind was made idempotent); producers declare `expected_rows` + consumers verify; a truncation negative (declare 9, deliver 4) fails the write. **`make composition` ✅.** Obligations documented in `format` + `strategy` CONTRACT.md. Per-axis conformance vectors deferred to Phase 1 (the enforcement bucket).
- **`rat/1.5` cut** over the sealed surface: 18 axis protos + cross-cutting types frozen, catalog commit-linkage (ADR-010), manifest envelope + 18 per-kind schemas (ADR-011), all 18 `CONTRACT.md`, C1/C2 crash-safety (ADR-012). **`make conformance` 32/32 · `make composition` ✅ · `make validate-manifests` 32/32.**
- **🎉 PHASE 0 COMPLETE.** Next: **Phase 1 (the core)** — the registry + reconciler + event bus + identity/state/API gateways — with the board's remaining crash-safety + enforcement findings (reviews/08 **C3–C5, D1–D5**) as its acceptance criteria.

---

## 2026-06-01 — Phase 0 close-out (3/4): **the doc tail** (reviews/08 E1/E3/E4/E7)

Cleared the four documentation findings from the board review; the contract surface is now fully documented + internally consistent.

- **E1 — all 18 axes now have a `CONTRACT.md`.** Authored the **12 missing** control/experience author guides (strategy, identity, tenancy, deployment-runtime, scheduler-backend, secret-backend, observability, audit-log, ui, notifications, marketplace, billing) via **12 parallel subagents** on the [`catalog/v1`](../contracts/proto/rat/catalog/v1/CONTRACT.md) template. Each: honesty banner · capabilities table · RPC semantics · conformance obligations · cross-cutting · writing guide · reference table. **Verified programmatically:** 18/18 exist, all required sections present, every documented capability URI matches the proto's `(rat.capability)` annotations exactly, all relative links resolve.
- **E4 — `overview.md` drift fixed.** The reconciler pseudocode no longer commands a phantom `plane-manager-plugin`; reframed declaratively (the reconciler **records desired plane state**, the **deployment-runtime** plugin converges — the core never spawns a process), so the "core never tells anyone to do anything" thesis holds. Added a **tier-0 callout** (state-backend / deployment-runtime / event-bus are bootstrap-critical, selected at boot, not hot-swapped) to the front-door doc; noted the core language is locked to Go (ADR-004).
- **E7 — the temptation ledger now exists** (CLAUDE.md #2), pinned at the top of this file. **Count: 0** — the board independently confirmed the six-thing discipline held; cross-cutting concerns were resolved as *correctness conditions of the existing six*, not new core responsibilities.
- **E3 — round-1 reference toys labeled** `WIRE-CONTRACT REFERENCE` across **13** `inmemory-py` READMEs: the 6 data-plane ones point at their round-2 real backend ("NOT A STARTER TEMPLATE"); the 7 control/experience sole-refs note they are not production-hardened.
- **Status:** staged, verified; commit pending.

---

## 2026-06-01 — Phase 0 close-out (2/4): **manifest schema FROZEN `v1` + 18 per-kind schemas** ([ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md))

Closed reviews/08 **E2** + the **last `v1-preview` artifact**. The one author-hand-written contract is now frozen, and a per-kind layer catches the wrong/missing-required-capability mistake the envelope can't.

- **[ADR-011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md)** — freeze `plugin.v1.json` at `v1` (additive-only within `rat/1`; breaking → `plugin.v2.json`) + the per-kind schema layer + **minimal-mandatory-core** strictness (the per-axis core table, e.g. `format`→`scan`, `catalog`→`get-table`, `state-backend`→`get`+`put`) + the enabling annotation roll.
- **Annotation roll** — added `(rat.common.v1.capability)` to all **12** previously-unannotated axes (strategy/identity/tenancy/deployment-runtime/scheduler/secret/observability/audit-log/ui/notifications/marketplace/billing); URIs from each proto's header comment. **Additive** (`buf breaking` FILE clean vs HEAD; lint+build clean). SDKs regenerated — 12 axes × {Go,Py,TS} descriptors (Rust + `_grpc` stubs unchanged, as expected: a method *option* lives only in the embedded FileDescriptor). All 18 axes now machine-readable; also unblocks the Phase-1 C5 gateway + C6 conformance for the control/experience tail.
- **Envelope frozen** — `plugin.v1.json` → `v1`; `schema/README.md` + `contracts/README.md` flipped to frozen; fixed an inaccurate illustrative example (`kind:engine`/`scan` → `kind:format`/`scan`).
- **18 per-kind schemas** — `contracts/schema/kinds/<kind>.v1.json`, each `allOf` the envelope + `kind` const + a `provides`-MUST-contain rule for the mandatory core. kind↔axis-segment mapping handled (`state-backend`→`state`, `secret-backend`→`secret`, `scheduler-backend`→`scheduler`, `audit-log`→`auditlog`).
- **Validation** — new [`scripts/validate-manifests.py`](../scripts/validate-manifests.py) + **`make validate-manifests`** gate (the static half of `rat plugin validate`): **32/32** checks — examples pass envelope+per-kind, the *new* per-kind rejections fire (missing core, wrong-axis, kind mismatch), INVALID #1–3/#5 rejected, #4 (semantic impl-naming) documented as the remaining lint gap. `INVALID-examples.md` +#6 (wrong-cap-for-kind); fixed 2 stale example capability refs (catalog `register`→`register-table`, storage `read`/`write`→`vend-credentials`). **`make conformance` still 32/32.**
- **Status:** staged, verified green; commit pending. *(gen-check BSR-throttled locally; freshness proven by the successful `make gen-sdks` + scoped 12-axis diff + the live conformance run.)*

---

## 2026-06-01 — Phase 0 close-out (1/4): **catalog commit-linkage** — the headline-feature hole closed on-wire ([ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md))

Closed reviews/08 **B1** (3 agents' top concern) — the first `v1.1` additive. The branch-pipeline headline feature now completes its **create → write → register → merge** loop entirely on the frozen wire; the composition no longer fakes table registration out-of-band.

- **[ADR-010](../docs/architecture/adrs/010-catalog-commit-linkage.md)** — two additive RPCs on the frozen `catalog/v1`: `RegisterTable` (`rat://catalog/v1/register-table`, idempotent create of a new output table) + `CommitTable` (`rat://catalog/v1/commit-table`, records the writer-supplied `WriteResult.snapshot_id` — the commit-linkage). `CommitTable` carries `MergeBranch`'s safety model (`expected_snapshot` CAS + `idempotency_key` → `already_applied`), giving the previously-unguarded **write/publish leg** the idempotency the B1 architect→sre cross-consult flagged. Two RPCs (not one) so create-vs-commit are method-level capabilities (the format `Write`-split precedent). **Resolves ADR-009 residual R3.**
- **Wire:** `catalog.proto` +2 RPCs +4 messages — **additive** (`buf breaking` FILE clean vs HEAD; lint + build clean). All 4 SDKs regenerated (Go/Py/TS/Rust — only the 8 catalog files changed).
- **References:** all 3 catalogs (`inmemory-go`, `inmemory-py`, `sqlite-py` — store + server) implement register/commit; sqlite uses `BEGIN IMMEDIATE` for the same durable + concurrent-safe semantics as merge.
- **Golden vectors:** `catalog-v1.json` +6 lifecycle steps (register new/idempotent · commit new/idempotent-retry/CAS-reject/CAS-ok) +3 error steps; all 3 harnesses extended (+ the 2 caps in the Go stub gateway's C5 allowlist). **`make conformance` 32/32.**
- **Composition:** `build_catalog` no longer pokes the catalog's private store — only the pre-existing source is admin-registered via the public api; the **full-refresh + SCD2 strategies register their output + commit the written snapshot through the gateway**, and the harness asserts `GetTable(target)` succeeds *after* the run (the catalog learned the output on-wire). `CompFormatServicer` now returns a real `snapshot_id`. **`make composition` ✅** (4/4 combos + both strategies).
- **Status:** staged in the working tree, verified green; commit pending. *(gen-check freshness gate is BSR-rate-limited locally; SDK freshness confirmed by the successful `make gen-sdks` + catalog-only diff + the live SDK exercise in conformance/composition.)*

---

## 2026-06-01 — Absorbed the board's two "NOW" items + **re-cut `rat/1`** (pre-publish correction)

Actioned the two reviews/08 items that were only possible while the freeze is local/unpushed, and re-cut the `rat/1` tag.

- **A1 [V2-REGRET fixed]** — `WriteResult.snapshot_id` `string` → **`optional`** (`data.proto`). Kills the empty-sentinel that conflated "no version" vs "cannot report" — the API-13 bug fixed on the sibling `rows_affected` but left on this field. `string`→`optional` is breaking under `buf` FILE rules, so it was free now / impossible after publication. Go refs' `snapshotID()` → `*string` (reads via `GetSnapshotId()` unchanged; Python proto-optional transparent). All 4 SDKs regenerated; **`make conformance` 32/32**; buf clean.
- **D5/E4 honesty banner** — added "the orchestrating core is NOT built yet (Phase 1); enforcement here is the contract it MUST implement, it does not run today; conformance tests references not a live deployment; `provides`/`conformed_capabilities` are self-asserted (no enforcer)" to `plugin.v1.json` (`$comment`) + the 6 `CONTRACT.md` author guides.
- **`rat/1` re-cut** (commit `0e81314`, was `b9dbe2d`) — supersedes the original; the annotation records why. `rat/1.5`–`rat/1.4` remain valid and layer on top.
- Commit `0e81314` (`fix(contract)!`). The single true V2-regret the board found is now **resolved**, not carried to a v2.

---

## 2026-06-01 — 5-agent post-freeze board review (communicating team) → [reviews/08]

Ran the first adversarial review *after* the freeze, as a **communicating agent team**: 5 specialists (`architect`, `security`, `ecosystem`, `sre`, `contracts`) reviewed the frozen surface (rat/1..rat/1.4 + 32 refs + composition) in parallel and **cross-consulted each other via direct messages** — several findings changed as a result (the terminal-audit finding came from `sre`→`security`; `architect` cross-corrected `sre` on the health contract; `security`↔`contracts` confirmed the ArrowStream-ticket gap).

- **Artifacts:** [`reviews/08-post-freeze-board-review.md`](../reviews/08-post-freeze-board-review.md) (synthesis) + [`reviews/board/`](../reviews/archive/board/) (5 full reports). Commit `b4c0526`.
- **Verdict:** the frozen WIRE is sound — **only ONE true V2-regret** across 18 axes (`WriteResult.snapshot_id` empty-sentinel) — but the freeze + "32/32 conformance" badge **overstates the guarantee**: enforcement (I9 isolation, ArrowStream ticket, storage cred scoping), crash-safety (no effect-leg idempotency key, no stream terminator, no provider deadline), and the **core itself** are deferred/unbuilt, and frozen artifacts describe the unbuilt core's enforcement in the present tense.
- **Strongest signal:** 3 agents independently nominated the **catalog commit-linkage/CreateTable gap** (the headline branch-pipeline feature can't close its loop on the frozen wire).
- **Actionable now (freeze is still local/unpushed):** make `snapshot_id` `optional` + re-cut `rat/1`; add a "core not built" honesty banner to `plugin.v1.json` + every `CONTRACT.md`. Full prioritized action list in reviews/08 → queued in [backlog](backlog.md).

---

## 2026-05-31 — 🧊🎉 **Experience axes FROZEN** (`rat/1.4`) — ALL 18 AXIS CONTRACTS NOW `v1`

Built one reference per experience axis and froze them — **completing the entire axis-contract surface**. `make conformance` **32/32** (commits `5ce7b30` refs, `030d406` freeze, tag **`rat/1.4`**).

- **`plugins/notifications/inmemory-py`** — Send delivery sink (captures messages); rejects empty title (`INVALID_ARGUMENT`).
- **`plugins/marketplace/community-py`** — Search/Get over seeded listings; the load-bearing **capability-aware "works on my deployment?" filter** (only listings whose `required_capabilities` are satisfied by the caller's `deployment_capabilities` are returned — e.g. scd2 is filtered until `format/merge` is present). Mandatory listing fields (provided/required/conformed + signed) exercised; Get unknown → `NOT_FOUND`.
- **`plugins/ui/web-portal-py`** — Describe (display name + hosted slots) + RenderSlot (resolve a contributed component → asset_ref + props_schema); unknown → `NOT_FOUND`.
- **Build method:** all 3 via **parallel subagents** on the storage template (omitting the tenant/context handling these stateless axes don't need).
- **Freeze:** flipped ui/notifications/marketplace DRAFT → `v1` (`rat/1.4`); buf clean.

**🎉 Milestone: every one of the 18 axis contracts is now frozen at `v1`** — 7 data-plane (engine/format/catalog/storage/runtime/state/strategy) + 1 tier-0 (deployment-runtime) + 7 control-plane (identity/secret/scheduler/tenancy/billing/observability/audit-log) + 3 experience (ui/notifications/marketplace), plus the cross-cutting types. **The only remaining `v1-preview` artifact is the manifest schema (`plugin/v1.json`).**

---

## 2026-05-31 — 🧊 **`deployment-runtime/v1` FROZEN** (`rat/1.3`) — two divergent references

Built two technologically-divergent references for the tier-0 `deployment-runtime` axis (the I9 trust boundary) and froze it. `make conformance` **29/29** (commits `119a1a0` refs, `50f21ee` freeze, tag **`rat/1.3`**).

- **`plugins/deploymentruntime/local-process-py`** — runs each plugin instance as a real child OS process (the `chmod +x ./rat` runtime); real Launch → Healthcheck (PID liveness) → Terminate lifecycle.
- **`plugins/deploymentruntime/k8s-dryrun-py`** — models a managed/declarative runtime: maps the `LaunchSpec` + I9 `IsolationProfile` → a Kubernetes Pod `securityContext` and admits the manifest (dry-run, no cluster). Where the isolation profile gets a real enforcement target.
- **Shared I9 gate** (the load-bearing obligation): both refuse to launch below the I9 minimum (`run_as_non_root` + `drop_all_capabilities` + `no_new_privileges`) → `FAILED_PRECONDITION`; empty image → `INVALID_ARGUMENT`. Both expose an isolation-honored receipt in `Healthcheck.detail`. Both pass the shared [`deploymentruntime-v1.json`](../contracts/conformance/deploymentruntime-v1.json) — local fork vs container proving the contract composes across runtime technologies.
- **Freeze:** flipped the proto Status DRAFT → `v1` (`rat/1.3`). Like the 6 ADR-003-listed data-plane axes, it got the full two-reference rigor (it's outside ADR-003's explicit list, like strategy, but it's the trust boundary the 3rd-party-plugin bet leans on).

**Still `v1-preview`:** the experience axes (`ui`, `notifications`, `marketplace`) + the manifest schema — the last of the Phase 0 tail.

---

## 2026-05-31 — 🧊 **Control-plane axes FROZEN** (`rat/1.2`) — 7 references + freeze

Built one reference per control-plane axis (ADR-003 requires only one for control-plane, vs two for data-plane) and froze them. `make conformance` now **27/27** (commits `5bcedf9` refs, `ba9269b` freeze, tag **`rat/1.2`**).

- **`plugins/identity/static-token-py`** — Authenticate (constant-time token compare; the C2 default, not anon-root) + Authorize (coarse role-based `deny_code`).
- **`plugins/secret/inmemory-py`** — Resolve with **anti-enumeration**: unknown ref AND cross-tenant ref both return `found=false` (never `PERMISSION_DENIED`).
- **`plugins/scheduler/inmemory-py`** — Schedule/Cancel + server-streaming WatchDue (one-shots; at-least-once delivery).
- **`plugins/tenancy/inmemory-py`** — Decide (permission/sharing/quota → `allowed` + `deny_code`); policy *on top of* the core's structural C7 isolation.
- **`plugins/billing/inmemory-py`** — Record usage events, per-tenant by construction (C7) + aggregation/isolation tests.
- **`plugins/observability/inmemory-py`** — bidi Ingest with cumulative per-batch acks.
- **`plugins/auditlog/inmemory-py`** — Append sink enforcing all 4 freeze-blocker-#4 properties: **Ed25519 signature verify** over the pinned canonical serialization, `prev_hash` chain check, prefix-only commit, idempotent DUPLICATE (adds `cryptography`; harness plays the signing core).
- **Build method:** the 4 simple unary axes (identity/secret/tenancy/billing) via **parallel subagents** on the storage template; the 3 streaming/crypto axes (scheduler/observability/auditlog) built directly.
- **Freeze:** flipped the 7 axis Status markers DRAFT → `v1` (frozen, `rat/1.2`); buf clean. Executes ADR-009's stated plan.

**Still `v1-preview`:** `deployment-runtime` (data-plane, no ref yet) + experience axes (`ui`, `notifications`, `marketplace`) + the manifest schema.

---

## 2026-05-31 — 🧊 **`strategy/v1` FROZEN** (`rat/1.1`) — scd2 second reference landed

The ADR-009-anticipated follow-on: with a second, semantically-different strategy reference, `strategy/v1` advances `v1-preview` → `v1` (commit `cd8fcac`, tagged **`rat/1.1`**).

- **`plugins/strategy/scd2-py/`** — Slowly Changing Dimension Type 2: stateful + temporal, the deliberate ADR-003 divergence from full-refresh. Reads source snapshot + existing target history; closes changed versions (`is_current=false`, `effective_to=run-ts`) + inserts new current versions; written via one `format.merge` keyed on `(natural_key…, effective_from)`. **Different capability mix** (`get-table` + `scan`×2 + `merge`, no engine) over the same `Apply` contract.
- **`contracts/conformance/strategy-scd2-v1.json`** — two-run temporal golden scenario (initial load → snapshot with changed + unchanged + new key → expected history).
- **`make composition`** extended — added `FormatService.Merge` + an SCD2 phase; now proves the cross-axis matrix **AND both strategy references** over the real stack (gateway + parquet + sqlite + Flight). Green.
- **`strategy.proto` → v1** (frozen, `rat/1.1`).
- **Contract observations** (ADR-003 payoff, none requiring a change): a strategy can read target state (`scan`), can be a data **producer** (hosts the synthesized delta) and **consumer** (pulls scans) — full-refresh was a pure router, so this stayed hidden until the second reference. Per-run params ride in `options`.

**`strategy/v1` is now `v1`.** Remaining `v1-preview`: control/experience axes + the manifest schema.

---

## 2026-05-31 — 🧊 **`rat/1` FROZEN** — data-plane contracts advanced to `v1` (ADR-009 + tag)

The Phase 0 contract-freeze milestone. With both gates met (0h-remediation + 0i cross-axis composition), the data-plane axis contracts advance `v1-preview` → `v1`.

- **[ADR-009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md)** (`6ca3ed2`) — the freeze decision: the six ADR-003 data-plane axes (engine/format/catalog/storage/runtime/state) + the cross-cutting types they depend on (`common/v1/{context,data,annotations,event,audit}`, `core/v1/invoke`, `ERROR_MODEL.md`) freeze at `v1`; breaking changes now require `v2`. `strategy/v1` (one ref) + control/experience axes + the manifest schema stay `v1-preview`. Residuals R1–R3 accepted as documented.
- **Freeze applied** (`b9dbe2d`, **tagged `rat/1`**) — flipped the Status markers on all 17 frozen files (11 protos + 6 `CONTRACT.md`) DRAFT/v1-preview → "v1 (frozen — rat/1, ADR-009)"; comment-only, buf lint+build clean, SDKs unaffected. `reviews/07` carries the RESOLUTION banner (the 0h NO-GO is closed).
- The `rat/1` annotated tag marks the frozen surface (local; reversible until external consumers pin to it).

**Phase 0's headline deliverable — a frozen, independently-validated data-plane contract — is DONE.** What remains in Phase 0 is the loosely-coupled tail: `strategy/v1` second reference, the control-plane axes' single references, and the manifest-schema freeze.

---

## 2026-05-31 — Sub-phase 0i COMPLETE: cross-axis composition (ADR-003 cross-combination gate MET)

Built the ADR-003 "run against each other on golden data" gate the freeze review flagged as the one unmet clause (reviews/07 Part C).

- **`plugins/strategy/fullrefresh-py/`** (`abd1228`) — the FIRST `kind: strategy` reference (the axis had zero). Pure capability orchestration over a single `invoke` seam: `catalog.get-table → engine.query → format.overwrite`, coupled to nothing by name. Its conformance IS the composition test.
- **`plugins/composition/`** + **`make composition`** — boots catalog+engine+format as real gRPC servers wired by capability through a mediating gateway, Arrow over **real pyarrow.flight** between axes, and runs the strategy across the 4 ADR-003 combos on shared golden data ([`composition-v1.json`](../contracts/conformance/composition-v1.json)). `comp_engine.py` closes the gap the per-axis engine refs left (resolve `QueryRequest.tables` via `format.scan`, bind, stream results over Flight).
- **Result:** all 4 combos — DuckDB/DataFusion × Parquet/Delta × sqlite/in-memory, storage held at local-fs — produce the **identical** target with the strategy code unchanged. **Gate MET.**
- **Findings surfaced** (the payoff): engine `SUM` type diverges DuckDB(hugeint)/DataFusion(int64) → golden SQL pins it with `CAST AS BIGINT`; the engine `tables`-binding + real Arrow transport weren't exercised per-axis; catalog has no create-table RPC (GA commit-linkage, R3). None wire-breaking. Conformance still 20/20.

---

## 2026-05-31 — 0h-remediation COMPLETE: the freeze punch-list (M1–M4 + S1–S4) cleared

Cleared the entire 0h freeze-review punch-list ([reviews/07](../reviews/07-freeze-review.md)). User chose **strict ADR-003** for the cross-axis gate, so remediation lands first, then the strategy reference + composition test, then the freeze. Conformance held **20/20** after every change.

- **M1+M2** (`16d9c37`) — pinned the canonical error model: new [`contracts/proto/rat/common/v1/ERROR_MODEL.md`](../contracts/proto/rat/common/v1/ERROR_MODEL.md) (two-layer rule: domain-outcome-field vs gRPC-status; the full status-code table; the not-found rule + secret anti-enumeration exception). Fixed the dangling cite in `invoke.proto`; documented catalog's deliberate no-`found` choice in `catalog.proto`; pointed all 6 axis `CONTRACT.md` at the model.
- **M3+M4** (`7e169e1`) — hardened the signed envelope: `key_id` on `AuditRecord` (field 11) + `SubjectAssertion` (field 5), each resolving in the core's published keyring to {key, algorithm} (rotation + agility = new key_id, no on-wire `alg`); covered by the signature. Added VERIFICATION CONTRACT step 4 (bare `Identity.tenant`/`principal` MUST equal the signature-covered values) + the transport-trust basis note (caller_plugin/tenant rest on authenticated transport C2). Additive fields, buf-clean; 4 SDKs regenerated.
- **S1–S4** (`df07ff9`) — comment cluster: `WriteResult.snapshot_id` reworded (not format-only); bidi non-first-frame `capability` → ABORT not "ignore"; audit-on-deny pinned as a C8 conformance obligation; stale `runtime-v1.json` comment corrected (ADR-008 closed the streaming-mediation gap).

**All 4 MUST-FIX + 4 SHOULD-FIX done; 3 residuals (R1–R3) tracked in backlog.** Next (strict-ADR-003 path): build the first **strategy** reference + the **cross-axis composition test** (the ADR-003 cross-combination gate), then tag `rat/1`.

---

## 2026-05-31 — Sub-phase 0h: freeze review COMPLETE — verdict **NO-GO** for unconditional `rat/1`

Ran the final adversarial pass before tagging the data-plane contracts `v1`. Three independent reviewers (contract-coherence, security/enforcement, freeze-readiness/integration) swept the now-complete surface; every blocker was ground-truthed against the actual proto/vector/reference files before being accepted or downgraded. Evidence base: `make conformance` **20/20 PASS**, `make lint`+`make build` clean.

- **Result:** [`reviews/07-freeze-review.md`](../reviews/07-freeze-review.md). The 15 prior freeze-blockers (reviews/06) are confirmed resolved and the keystone holds. But the pass found a **new punch-list** the earlier review couldn't reach (the references + cross-cutting protos + CONTRACT.md docs that surface these didn't exist then):
  - **4 MUST-FIX** (wire-shape / un-retrofittable): **M1** error-model convention referenced (invoke.proto:99) but pinned in no frozen artifact; **M2** "resource absent" modeled 3 ways (secret/state `found` bool vs catalog `NOT_FOUND`) with no governing rule; **M3** signatures (`AuditRecord`, `SubjectAssertion`) carry no `key_id`/alg → rotation pain; **M4** `SubjectAssertion` verification contract omits the bare-mirror cross-check (`Identity.tenant` == signed tenant) + doesn't state the transport-trust dependency.
  - **4 SHOULD-FIX** (cheap text): **S1** engine `snapshot_id` vector vs `WriteResult` comment mismatch; **S2** bidi non-first-frame `capability` "ignored" not "rejected"; **S3** audit-on-deny intended but unpinned (stub omits it); **S4** stale `runtime-v1.json` comment.
  - **3 ACCEPTED RESIDUALS** (defensible, documented): R1 assertion bound to operation not hop (bounded by C5 `requires`); R2 storage cred-scoping honour-system (ADR-005 bearer exception); R3 additive niceties → backlog.
- **The real gate (ADR-003):** per-axis is MET (two refs + divergent real backend + golden vectors, all 6 axes), but the **cross-axis composition** clause is **NOT met** — conformance is per-axis only, and the **strategy axis (which composes engine+format+catalog+storage) has zero references**. Risk a composition test finds a flaw: low (coupling types `TableRef`/`ArrowStream` partly exercised via real Arrow Flight). But low ≠ satisfied.
- **Decision:** do NOT tag `rat/1` yet. Path: (1) 0h-remediation clears M1–M4 + S1–S4; (2) cross-axis fork — **(a)** strict ADR-003 (build strategy ref + composition test first) or **(b)** conditional freeze (tag after step 1, cross-axis as the one documented residual gate). The fork is rigor-vs-velocity → user's call. (3) advance the 6 axes `v1-preview`→`v1`.

**Files:** `reviews/07-freeze-review.md` (new). New backlog work surfaced (M1–M4, S1–S4, R1–R3, cross-axis gate).

---

## 2026-05-31 — Sub-phase 0c COMPLETE: cross-cutting protos finalized (audit envelope relocated + coverage audit)

Finalized the cross-cutting concern protos. An audit of every C1–C10 + ARCH concern against its wire home surfaced **one real layering inversion**, which 0c fixes; everything else was already covered (the freeze-blocker remediation had filled context/data/annotations/event/invoke).

- **The finding:** `AuditRecord` + `AuditOutcome` lived in the **`rat.auditlog.v1` axis** proto — but the audit record is **core-authored, core-signed, and emitted even when no audit-log plugin is installed** (C8; the proto's own header says "this axis is only the export sink"). A core-enforced cross-cutting type living in an axis proto would force the core's C8 emission to import an axis contract.
- **The fix:** created **`contracts/proto/rat/common/v1/audit.proto`** with `AuditRecord` + `AuditOutcome` (the cross-cutting C8 envelope, next to context/data/annotations/event); `auditlog.proto` now imports it and `AppendRequest.records` references `common.v1.AuditRecord`. **Wire-compatible** — field numbers unchanged, so the canonical serialization + Ed25519 signatures + hash chain are byte-identical; only the proto package (and generated type name) moves `auditlog.v1` → `common.v1`. `buf lint`/`build` clean; `buf breaking` flags the move (3 expected findings, allowed in `v1-preview`); all 4 SDKs regenerated.
- **Coverage doc:** [`docs/architecture/cross-cutting-coverage.md`](../docs/architecture/cross-cutting-coverage.md) — the finalize artifact: a matrix mapping every C1–C10 + ARCH concern to its wire home (`common/v1/{context,data,annotations,event,audit}` + `core/v1/invoke`) or its deliberately non-wire mechanism (transport credential / manifest schema / process gate / conformance suite). Confirms NO concern is homeless and NO core-enforced concern lives in an axis proto. Also resolves the plan's "descriptors ⬜" note (descriptors = the manifest `plugin.v1.json` + the proto service descriptors the gateway already reads — both done).

**Sub-phase 0c is COMPLETE.** The cross-cutting proto set is final: `common/v1/{context, data, annotations, event, audit}` + `core/v1/invoke`, with `auditlog.proto` demoted to a pure sink axis. Remaining toward `rat/1` freeze: **0h** (peer review + freeze).

**Files:** `contracts/proto/rat/common/v1/audit.proto` (new), `contracts/proto/rat/auditlog/v1/auditlog.proto` (imports it), `contracts/sdks/**` (regenerated), `docs/architecture/cross-cutting-coverage.md`.

---

## 2026-05-31 — Sub-phase 0g: per-axis `CONTRACT.md` author guides (6 data-plane axes)

Wrote the author-facing contract guide for every data-plane axis — the canonical "how do I implement a `kind: <axis>` plugin" doc, grounded in the now-existing protos, golden vectors, and both reference rounds.

- **6 files, one per axis**, placed **next to the proto** (`contracts/proto/rat/<axis>/v1/CONTRACT.md`) so an author reads the wire contract + the guide together: `state`, `engine`, `format`, `storage`, `runtime`, `catalog`.
- Each covers: what the axis is, the **capabilities + method/cardinality table**, the **RPCs** (request/response + semantics), the **conformance obligations** (the axis-specific ones spelled out — state's key grammar + linearizable CAS, catalog's merge-safety, storage's C7 tenant-scoping, format's bidirectional Arrow leg, engine's typed-Arrow, runtime's streaming framing), the **cross-cutting rules** (context-in-metadata ADR-007, core-mediation ADR-005/008, bulk-bypasses-core), a **"writing a plugin"** checklist, and a **reference-implementations** table (round-1 wire + round-2 real backend, with what each demonstrates).
- **All cross-links verified** (proto, conformance vectors, ADRs, reviews, cross-axis docs, reference dirs — every relative path resolves); `buf lint` ignores the `.md` files in the proto module (clean).
- Index added to [`contracts/conformance/README.md`](../contracts/conformance/README.md) ("Per-axis contract docs"). Control/experience axes get their `CONTRACT.md` when referenced.

This is the 0g deliverable for the axes that have references (the grounded, non-speculative ones). Remaining toward freeze: 0c (finalize cross-cutting protos) + 0h (peer review + `rat/1` freeze).

**Files:** `contracts/proto/rat/{state,engine,format,storage,runtime,catalog}/v1/CONTRACT.md`, `contracts/conformance/README.md` (index).

---

## 2026-05-31 — Sub-phase 0f COMPLETE: per-RPC latency benchmark — the ADR-005 mediation hop, measured

The second + final 0f sub-item: a benchmark that quantifies the one perf number the architecture trades on — the **core-mediated gateway's overhead vs a direct call** (ADR-005 accepted "a latency hop per control call", with a direct-dial fast-path *only if a profiling pass shows it's needed*; ADR-008 added a streaming relay). This IS that profiling pass.

- **`plugins/bench/latency-go/`** + **`make bench`** — measures the SAME plugin RPC two ways (direct `caller→plugin` vs mediated `caller→gateway→plugin`) for a unary RPC (`state.Get`) and a server-streaming one (`runtime.Execute`); reports p50/p99/mean + the delta. The plugin RPCs are trivial (fixed response / a few fixed frames) so the measurement isolates transport + mediation cost. The mediated path includes the client-side marshal/unmarshal + the `rat-callmeta-bin` envelope stamp (the SDK's real cost) + the gateway's traceparent-validate + identity-restamp + passthrough relay (a faithful non-test gateway in `gateway.go`).
- **Result (localhost TCP, single goroutine):** unary direct p50 ~62µs vs mediated ~228µs → **+166µs (+266%)**; streaming direct ~66µs vs mediated ~249µs → **+183µs (+277%)**. Mediation roughly TRIPLES a control RPC's latency (a full extra gRPC hop + serialization) but the **absolute cost is ~0.2ms**.
- **Validates the ADR-005 bet honestly:** cheap enough for control traffic (a pipeline run makes a handful of control calls → ~ms total, negligible vs the data work), and the hot path doesn't pay it at all — bulk DATA bypasses the gateway entirely via `ArrowStream`. If a future hot control path ever needs sub-mediation latency, the direct-dial fast-path ADR-005 left open can be added — but the number shows it isn't needed for v1.
- The benchmark dir has a `go.mod` but no `harness_test.go`, so `scripts/conformance.sh` discovery was tightened to require a harness — the bench is correctly excluded from `make conformance` (still 20/20).

**🎉 Sub-phase 0f is COMPLETE** — conformance suite runner (`make conformance`, 20/20) + latency benchmark (`make bench`). Plus the real Arrow Flight transport landed. The data-plane reference + conformance + perf arc of Phase 0 is done; remaining is freeze prep (0c/0g/0h).

**Files:** `plugins/bench/latency-go/**`, `Makefile` (bench target), `scripts/conformance.sh` (harness-required discovery).

---

## 2026-05-31 — Sub-phase 0f: the conformance suite runner — one command, one pass/fail matrix

Formalized the conformance suite (the operational form of ADR-003's "both pass the axis's conformance suite"). The per-axis golden vectors were already authoritative; what was missing was a single runner across all references.

- **`scripts/conformance.sh`** + **`make conformance`** — **auto-discovers every reference** under `plugins/<axis>/<impl>/` (Go = has `go.mod`; Python = has `harness_test.py`), runs each one's harness against its golden vectors (Go via `go test`, Python via `python harness_test.py`), and prints a single **axis × impl × lang × vectors × result** matrix. Containerized (podman/docker, no host installs); one golang container for all Go refs, one python container (union of deps installed once) for all Python refs. **Exit 0 iff every reference conforms** — so CI / the freeze gate can hang on it. A new reference joins the suite the moment it lands (no registration).
- Portable rendering (host `sort` + plain `awk`, works under mawk/gawk); real-engine refs correctly mapped to `engine-real-v1.json`, the rest to `<axis>-v1.json`.
- **Verified: 20/20 references conform** — all 6 axes' round-1 language twins + the round-2 real backends (sqlite/local-fs/subprocess/duckdb/datafusion/parquet/delta), green in one run.
- `contracts/conformance/README.md` documents `make conformance` + a sample matrix.

This is the 0f deliverable's core (the suite runner). A per-RPC latency benchmark is the remaining 0f sub-item (deferred — lighter, optional).

**Files:** `scripts/conformance.sh`, `Makefile` (conformance target), `contracts/conformance/README.md`.

---

## 2026-05-31 — Real Arrow Flight transport — the last in-process data-leg stand-in retired

Replaced the in-process Arrow-IPC registry with a REAL `pyarrow.flight` transport in `plugins/format/parquet-py` — the only reference where the bulk-data leg is now *fully* real (real Parquet files + real Flight wire).

- **`plugins/format/parquet-py/flight.py`** — a real `FlightServerBase` on an ephemeral localhost port. `put(table)` hosts the table + returns `ArrowStream{endpoint=grpc://host:port, ticket}`; `flight_pull(stream)` dials the descriptor's endpoint and `DoGet`s the ticket — a real Flight round-trip over a TCP socket. Single-use tickets (DoGet consumes — SEC-14).
- **Both directions are real:** the PLUGIN hosts a Flight server for Resolve results (the harness DoGets); the CALLER (harness) hosts a Flight server for Append/Merge/Overwrite sources (the plugin DoGets). Matches the contract's "Resolve → producer-hosted; the format pulls from a caller-hosted source" — both `PRODUCER_HOSTED` (data-holder hosts, data-needer DoGets).
- **Zero contract change:** the `common.v1.ArrowStream {endpoint, ticket, transport=FLIGHT, role}` descriptor was always real-Flight-shaped; only the implementation swapped (in-process dict → real Flight server). `streams.py` deleted from parquet-py; `server.py` + `harness_test.py` use `flight.py`. Still passes the SAME shared `format-v1.json` + the real-Parquet-files test. Green in `python:3.12`.
- This proves the in-process registry was always a transport CHOICE, not a contract limitation. The other refs keep it for simplicity; parquet-py is the canonical real-Flight demonstration.

**Significance:** the last "stand-in" in the data plane is retired (in this reference). Across rounds 1+2 the DATA was already real typed Arrow (engine/format); now the TRANSPORT is real Arrow Flight too. The data-plane contract is validated end-to-end with real backends AND a real wire.

**Files:** `plugins/format/parquet-py/{flight.py,server.py,harness_test.py,README.md}` (−`streams.py`). No proto/SDK/vector change.

---

## 2026-05-31 — 🎉 ROUND 2 COMPLETE: `format` = REAL pair (Parquet + Delta) — real Arrow files + time travel

Sixth + final round-2 axis, via option (b) two REAL backends. **Round 2 is now complete — all six data-plane axes have a technologically-divergent real backend.**

- **Real Arrow data leg, BOTH directions:** unlike the toy refs (string-row registry), the source rows for Append/Merge/Overwrite are staged as real Arrow (Arrow IPC) and Resolve results pulled back as real Arrow — `streams.py` (shared with the engine pair). This is the full typed-Arrow data leg for format, retiring the last in-process-stand-in for these refs.
- **`plugins/format/parquet-py`** (pyarrow): writes real `.parquet` files per table; full Append→scan→Merge(upsert)→Overwrite→Maintain(compact) lifecycle on real files; backend test asserts real Parquet files land on disk + readable.
- **`plugins/format/delta-py`** (`deltalake`): backs the table with a real **Delta Lake** table (transaction log over Parquet). Earns **time travel** (`test_delta_time_travel`: two appends → versions 0/1; read v0 back → prior state) — the versioned-snapshot substrate the `catalog` axis's branches sit on. Only `store.py` differs from parquet; `server.py`/`streams.py` identical. (deltalake's Rust runtime aborts at interpreter teardown after all logic ran → `os._exit(0)` after PASS.)
- **Both pass the SAME shared `format-v1.json`** the in-memory + Parquet refs use (format data is provider-neutral rows). All FOUR format refs green (inmemory-go, inmemory-py, parquet-py, delta-py). Verified in `python:3.12` / `golang:1.25`.

**🎉 ROUND 2 COMPLETE — 6/6 data-plane axes with a real divergent backend, each passing its shared golden vectors + a backend-specific semantic test:**
- `state`=sqlite (durability + linearizable CAS)
- `storage`=local-fs (path containment + tenant isolation)
- `catalog`=sqlite (durable branches/ledger + concurrent-merge safety)
- `runtime`=subprocess (OS process isolation)
- `engine`=duckdb+datafusion (real SQL + typed Arrow)
- `format`=parquet+delta (real Arrow files + time travel)

This is the full ADR-003 rigor: every data-plane contract is now validated by running code in two languages (round 1, wire contract) AND a technologically-divergent real backend (round 2, semantic). The typed-Arrow gap is retired for engine + format. **The remaining gap before `v1`** is just the real Arrow Flight transport (all data legs still use an in-process IPC registry stand-in) + 0f conformance-suite formalization + 0h peer review/freeze.

**Files:** `plugins/format/{parquet-py,delta-py}/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2 (option b): `engine` = REAL pair — DuckDB + DataFusion on real SQL + typed Arrow

The first round-2 axis done via **option (b): two REAL backends** (ADR-003's literal "duckdb + datafusion" example), not toy + real. Two genuinely different SQL engine technologies agree on one shared golden-vector file.

- **`contracts/conformance/engine-real-v1.json`** — REAL typed SQL (`CREATE TABLE orders (id INTEGER, region VARCHAR, amount INTEGER)`, `INSERT`, `SELECT … WHERE … / LIMIT`) with typed-Arrow result assertions (row_count + projected columns + rows_contain with TYPED values). Distinct from the round-1 toy `engine-v1.json` (which validates the wire contract via the in-memory mini-SQL refs).
- **`plugins/engine/duckdb-py`** (DuckDB 1.5.3) + **`plugins/engine/datafusion-py`** (Apache DataFusion 53.0.0) — both execute the same SQL, both return results as **real typed Arrow**. Only `store.py` differs between them; `server.py`/`streams.py`/`harness_test.py` are identical (the contract is the same, only the engine changes). Both green in `python:3.12`.
- **Retires the typed-Arrow gap for engine:** the result leg is now **real Arrow IPC** (typed schema + columnar batches, serialized + read back with pyarrow via `streams.py`), not the toy string-row stand-in. The transport is still an in-process registry (Flight deferred), but the DATA is genuine typed Arrow.
- Deps install cleanly + fast in-container (duckdb/datafusion/pyarrow, ~8s). The toy `inmemory-go`/`inmemory-py` engine refs remain as the round-1 wire-contract validation.

**Round 2 progress: 5 of 6 axes.** `state`=sqlite, `storage`=local-fs, `catalog`=sqlite, `runtime`=subprocess, **`engine`=duckdb+datafusion**. Remaining: **`format`** (parquet + delta/iceberg — real Arrow files; the last + heaviest).

**Files:** `contracts/conformance/engine-real-v1.json` + README, `plugins/engine/duckdb-py/**`, `plugins/engine/datafusion-py/**`. No proto/SDK change.

---

## 2026-05-31 — Round 2: `runtime` = subprocess (real backend) — OS process isolation

Fourth round-2 real backend. `plugins/runtime/subprocess-py/` — each `Execute` runs the work unit in a real CHILD OS PROCESS (`worker.py`) instead of in-thread. Runtime is the "where does the code run" axis; this one actually runs it elsewhere.

- **Passes the SAME shared vectors** (`contracts/conformance/runtime-v1.json`) — the toy work_spec (`{steps, rows, indeterminate, fail}`) is abstract enough a child-process runtime interprets it identically (emit `steps` progress events ± fraction, then a completion). All three runtime refs (inmemory-go, inmemory-py, subprocess-py) green on one shared file.
- **Two isolation properties the in-thread runtime CANNOT show:** `test_work_runs_in_a_separate_process` (work unit PID ≠ server's) and `test_each_work_unit_gets_its_own_process` (two Execute calls → two DISTINCT child PIDs).
- Process isolation is the seed of the real runtime/deployment-runtime sandboxing story (a crashing unit can't take the runtime down; a container/WASM runtime is the step up). Python stdlib `subprocess`; direct streaming harness. Green in `python:3.12`.

**Round 2 progress: 4 of 6 axes** (`state`, `storage`, `catalog`, `runtime`). Remaining: **`format` + `engine`** — the genuinely heavy ones (real Arrow Flight + Parquet / DuckDB) that need conformance-vector REWORK first (engine vectors are toy-mini-SQL-specific; format carries the bulk leg as an in-process stand-in). Not drop-in like the other four — surfaced for a decision (see [current.md](current.md)).

**Files:** `plugins/runtime/subprocess-py/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2: `catalog` = sqlite (real backend) — durable branches/ledger + concurrent-merge safety

Third round-2 real backend. `plugins/catalog/sqlite-py/` — branches, their snapshots, and the idempotency ledger live in sqlite (real transactional SQL DB, file, WAL) rather than an in-memory dict.

- **Passes the SAME shared vectors** (`contracts/conformance/catalog-v1.json`) — same model + deterministic snapshot scheme. All three catalog refs (inmemory-go, inmemory-py, sqlite-py) green on one shared file.
- **Two properties the in-memory catalog CANNOT show:**
  - **Durability** (`test_durability_branches_and_ledger_survive_reopen`): create branch + merge → close → reopen the same db file → the branch, the moved snapshot, AND the idempotency ledger persist (a re-merge with the same key is still a no-op returning already_applied). A dict dies with the process.
  - **Concurrent-merge safety** (`test_concurrent_merge_one_winner`): 16 threads race a MergeBranch into `main` from the same expected snapshot → exactly one COMMITs, the rest FAILED_PRECONDITION. Durable, cross-connection lost-update prevention via `BEGIN IMMEDIATE` (+ idempotency-key PK), not an in-process mutex.
- Concurrent-merge safety is the publish gate of the v2 pipeline model (reviews/06 #8 — `MergeBranch` is reconciler-retried, must be safe under retry AND concurrency), enforced for real. Python stdlib sqlite3; direct harness. Green in `python:3.12`.

**Round 2 progress: 3 of 6 axes** (`state`=sqlite, `storage`=local-fs, `catalog`=sqlite). Remaining: `format`, `engine`, `runtime` (the Arrow-heavy / execution ones).

**Files:** `plugins/catalog/sqlite-py/**`. No proto/SDK/vector change.

---

## 2026-05-31 — Round 2: `storage` = local-fs (real backend) — path containment + tenant isolation

Second round-2 real backend. `plugins/storage/localfs-go/` — a `storage` plugin that vends credentials scoped to a REAL local filesystem path under a per-tenant root, where the in-memory refs just echo the requested prefix into a JSON scope receipt.

- **Provider-neutral vectors:** `storage`'s `prefix` is provider-specific (the in-memory refs used `s3://…` URIs). Changed `contracts/conformance/storage-v1.json` to scheme-less LOGICAL prefixes (`warehouse/orders`, …) so every backend can resolve them per its own scheme; the in-memory refs (which echo) re-pass unchanged (verified). The receipt MAY carry extra provider-specific fields (local-fs adds `resolved_path`) the vectors ignore.
- **Passes the SAME shared vectors** through the stub gateway (scope = tenant + logical prefix + mode + short TTL). All three `storage` refs (inmemory-go, inmemory-py, localfs-go) green on one shared file.
- **Two filesystem properties the in-memory echo CANNOT show, now tested:**
  - **Path containment** (`TestLocalFS_PathContainment`): a normal prefix resolves under `<root>/<tenant>/` and the dir is created on disk; an escaping prefix (`../../escape`) → `PERMISSION_DENIED` (via `filepath.Rel` containment). The in-memory echo would accept it.
  - **Tenant isolation** (`TestLocalFS_TenantIsolation`): two tenants vending the same logical prefix get distinct paths under their own roots.
- Path containment is the storage analog of sqlite's durability/CAS — the cross-tenant security boundary `storage.proto` emphasizes (reviews/01 F3, reviews/04), enforced for real rather than by convention. Go (routes through the gateway, unlike the Python sqlite ref). Green in `golang:1.25`.

**Round 2 progress: 2 of 6 axes** have a divergent real backend (`state`=sqlite, `storage`=local-fs). Pattern holds: real backend + same shared vectors + a backend-specific semantic test.

**Files:** `contracts/conformance/storage-v1.json` (logical prefixes), `plugins/storage/localfs-go/**`. No proto/SDK change.

---

## 2026-05-31 — ROUND 2 begins: `state` = sqlite (real backend) — durability + linearizable CAS

The first **technologically-divergent** reference (ADR-003's *spirit*, not just letter): a third `state` implementation backed by **sqlite** (real embedded transactional SQL DB, file-on-disk, WAL) rather than an in-memory hashmap. `plugins/state/sqlite-py/`.

- **Passes the SAME shared golden vectors** (`contracts/conformance/state-v1.json`) the in-memory twins pass — a real backend conforming to the identical wire contract is the actual round-2 ADR-003 evidence. All three `state` refs (inmemory-go, inmemory-py, sqlite-py) green on one shared file.
- **Two properties the in-memory twins CANNOT show, now actually tested:**
  - **Durability** (`test_durability_survives_reopen`): write → close store → reopen the same db file → state persists. A hashmap dies with the process.
  - **Linearizable CAS** (`test_linearizable_cas_one_winner`): 16 threads race a compare-and-set from the same revision → **exactly one** COMMITs. Serialization enforced by sqlite's `BEGIN IMMEDIATE` (durable, cross-connection), not an in-process mutex — the real lease primitive (reviews/06 C-4) the in-memory twin could only fake.
- CAS is read→check→write inside a `BEGIN IMMEDIATE` transaction; global monotonic revision via a `meta` table; change log table for Watch. Same MODEL as the in-memory refs (matching revisions) so the shared vectors pass. Python stdlib `sqlite3` (zero new deps; GIL released during sqlite calls so the concurrency test is real).
- Verified in `python:3.12` (sqlite 3.46.1): `PASS … + durability + linearizable CAS`.

**Significance:** this is the first axis where the round-2 SEMANTIC gate (not just the wire-contract gate) is exercised on a divergent backend. The in-memory `state` CAS is serialized by a mutex (also linearizable, but in-process + non-durable); sqlite proves the contract holds on a backend with a genuinely different consistency/durability profile — exactly the "orthogonality assumption" rigor ADR-003 exists for.

**Files:** `plugins/state/sqlite-py/**`. No proto/SDK change.

---

## 2026-05-31 — 0d: `state` axis (Go + Python) → `state/v1` wire-contract gate MET → 🎉 ROUND 1 COMPLETE (all 6 data-plane axes)

Sixth + final data-plane axis through the 0d wire-contract two-reference gate — and the **capstone**: a tier-0 plugin with 4 RPCs (Get/Put/List unary + Watch server-streaming) and the axis's two pointed obligations.

- **contracts/conformance/state-v1.json** — STATEFUL lifecycle: Put(create) → Get(found) → Put CAS-MATCH (COMMITTED) → Put CAS-CONFLICT (the `PutOutcome.CONFLICT` enum, NOT a gRPC error, with the conflicting revision) → Get(unchanged, proving the conflict didn't write) → Get(missing) → two more Puts → List(all)/List(prefix) → ordered Watch replay of the change log. + 6 KEY-GRAMMAR error vectors (empty / oversize / NUL / control-char / traversal / dot-component → INVALID_ARGUMENT). Deterministic revision scheme keeps the impls in lockstep; control-char keys are built from `key_len`/`key_inject` so the vector file stays pure-ASCII.
- **First reference to use BOTH gateway relays:** unary `Invoke` (Get/Put/List) AND the ADR-008 `InvokeServerStream` (Watch) — a shared `openCall` does enforce/route/stamp/audit once for both.
- **state.proto:** added `(rat.common.v1.capability)` to all 4 RPCs + annotations import (was comment-only). SDKs regenerated (Go/Python/TS; Rust emits no method options).
- **inmemory-go** (grammar/store/server/main + dual-relay stub gateway + harness): green in `golang:1.25`. **inmemory-py** (mirrored grammar + model): green in `python:3.12`.

**🎉 0d ROUND 1 COMPLETE.** All six data-plane axes — format, engine, storage, runtime, catalog, state — now have two independently-written references (Go + Python) passing one shared golden-vector file. **Verified: all TWELVE references green together.** Cross-cutting work that fell out of round 1: ADR-007 (call-context transport) + ADR-008 (streaming invocation), both decided AND migrated; the SDK-vendoring fix; the round-1/round-2 framing correction.

**What round 1 is NOT (per the scope caveat):** all twelve are in-memory twins — the WIRE-contract gate. The technologically-divergent real-backend pair (round 2: state=sqlite, storage=local-fs, …) + the typed-Arrow pass are still required before any axis → `v1`. See [backlog.md](backlog.md).

**Files:** `contracts/conformance/state-v1.json`, `contracts/proto/rat/state/v1/state.proto`, `contracts/sdks/**`, `plugins/state/inmemory-go/**`, `plugins/state/inmemory-py/**`.

---

## 2026-05-31 — 0d: `catalog` axis — two references (Go + Python) + shared golden vectors → `catalog/v1` wire-contract gate MET

Fifth data-plane axis through the 0d two-reference (wire-contract) gate. The richest axis so far — git-like branch/version semantics with a real safety contract.

- **contracts/conformance/catalog-v1.json** — a STATEFUL lifecycle: GetTable(main) → CreateBranch(run-42 from main) → GetTable(on branch) → MergeBranch with optimistic-concurrency ACCEPT (`expected_into_snapshot` matches) + idempotency_key → idempotent retry (`already_applied=true`) → MergeBranch REJECT (`FAILED_PRECONDITION`, target moved) ; + stateless errors (unknown table `NOT_FOUND`, empty id `INVALID_ARGUMENT`). Exercises the MERGE-SAFETY contract (reviews/06 #8) for real. Deterministic snapshot scheme (seed main@snap-0; merge → snap-<counter>) keeps the two impls in lockstep; the harness gained per-step `expect.code` so an error can be asserted mid-sequence.
- **catalog.proto:** added `(rat.common.v1.capability)` to all 3 RPCs (get-table/create-branch/merge-branch) + annotations import (was comment-only) so the gateway routes them. SDKs regenerated.
- **inmemory-go** (`plugins/catalog/inmemory-go/`): store(branches/merges ledger)/server/main + the unary stub gateway re-pointed at CatalogService + harness. Green in `golang:1.25`.
- **inmemory-py** (`plugins/catalog/inmemory-py/`): from-scratch second reference mirroring the snapshot model. Green in `python:3.12`.

**Verified (containers):** all TEN references (format+engine+storage+runtime+catalog, Go+Python) green together.

**Scope (per the round-1/round-2 split):** in-memory twins — wire-contract gate. A real divergent backend (e.g. sqlite-catalog) is round-2.

**Files:** `contracts/conformance/catalog-v1.json`, `contracts/proto/rat/catalog/v1/catalog.proto`, `contracts/sdks/**` (regenerated), `plugins/catalog/inmemory-go/**`, `plugins/catalog/inmemory-py/**`.

---

## 2026-05-31 — ADR-008 migration executed: `InvokeServerStream` + `InvokeBidiStream`; runtime now gateway-mediated

Implemented [ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md) (decided in the prior commit; this is the implementation, kept separate per one-ADR-per-commit).

- **`invoke.proto`:** added `InvokeServerStream(InvokeServerStreamRequest) returns (stream InvokeServerStreamResponse)` + `InvokeBidiStream(stream InvokeBidiStreamRequest) returns (stream InvokeBidiStreamResponse)` to `CapabilityInvokeService`, with 4 new distinct message types. **Refinement vs the ADR's first draft:** buf STANDARD's `RPC_REQUEST_RESPONSE_UNIQUE` forbids sharing `InvokeRequest`/`InvokeResponse` across RPCs, so each variant got its own request/response (also the more evolvable choice). ADR-008 §2 + Migration amended to record this (same-day). `buf lint`/`build` clean; `buf format` applied; the added methods + messages are non-breaking (`buf breaking` FILE).
- **`runtime.proto`:** added the deferred `(rat.common.v1.capability) = "rat://runtime/v1/execute"` method option (+ annotations import) so the gateway can route it.
- **SDKs:** regenerated all 4 (Go/Python/TS/Rust) — the Go SDK now exposes `InvokeServerStream` client/server + the 4 new types.
- **Stub gateway (runtime example):** added the **server-stream relay** — enforce C5 + validate traceparent + stamp the downstream `rat-callmeta-bin` envelope (ADR-007) + one C8 audit ALL once at stream-open, then open a downstream server-streaming call (`ClientConn.NewStream` + `StreamDesc{ServerStreams:true}` + passthrough codec) and relay each `ExecuteResponse` frame's opaque bytes upstream — never deserializing.
- **Runtime harness:** rewired from direct-dial to route `Execute` through `gw.InvokeServerStream` (replacing the direct path + updating the header note; the Python harness stays direct like the other Python refs). Added the C8 one-audit-per-stream assertion.

**Behavior-preserving — verified:** the **unchanged** runtime golden vectors still pass, now over the mediated streaming path (Go `golang:1.25`); INVALID_ARGUMENT relays through the streaming gateway verbatim. All EIGHT references (format+engine+storage+runtime, Go+Python) green together after the invoke.proto + SDK changes.

**Files:** `contracts/proto/rat/core/v1/invoke.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `contracts/sdks/**` (regenerated), `docs/architecture/adrs/008-*.md` (§2 + Migration amended), `plugins/runtime/inmemory-go/{gateway_test.go,harness_test.go}`, `plugins/runtime/inmemory-py/README.md`.

---

## 2026-05-31 — ADR-008: streaming capability invocation (per-cardinality Invoke variants)

Resolved the streaming-mediation finding the `runtime` 0d reference surfaced. **[ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md) (Accepted):** add `InvokeServerStream(InvokeRequest) returns (stream InvokeResponse)` + `InvokeBidiStream(stream InvokeRequest) returns (stream InvokeResponse)` to `core/v1 CapabilityInvokeService`. Streaming capabilities stay core-mediated — the gateway enforces C2/C5/C7/C8 + traceparent **once at stream-open**, stamps the downstream `rat-callmeta-bin` envelope for the stream's lifetime (ADR-007), and relays each frame's opaque bytes via the passthrough codec (never deserializing). One C8 audit per stream.

**Why:** ADR-005's `Invoke` is unary; the contract has 4 streaming methods (`runtime.Execute`, `state.Watch`, `scheduler.WatchDue` server-streaming; `observability.Ingest` bidi) with no mediation path. Extends ADR-005 (upholds its central-enforcement thesis — streaming is "unary with N frames", gateway stays axis-generic) + reuses ADR-007's enforce-at-open + identity-in-metadata. `InvokeBidiStream` subsumes client-streaming, so only 2 new RPCs. Rejected direct-dial-with-token (reintroduces the honor-system ADR-005 refused), progress-to-event-bus (mutilates axis contracts, doesn't generalize to bidi), and leave-unmediated (permanent enforcement hole).

**Process:** ADR-only commit. The implementation (add the 2 RPCs to `invoke.proto`, regen SDKs, server-stream relay in the gateway, route `runtime.Execute` through `InvokeServerStream` + add runtime's deferred capability annotation, re-run the unchanged runtime vectors) is queued as the next step.

**Files:** `docs/architecture/adrs/008-streaming-capability-invocation.md`, `docs/architecture/adrs/README.md` (index), `ideas/inbox.md` (finding promoted), `roadmap/**`.

---

## 2026-05-31 — 0d: `runtime` axis — two references (Go + Python) + shared golden vectors → `runtime/v1` ADR-003 gate MET (+ streaming-mediation finding)

Fourth data-plane axis through the 0d two-reference gate, and the **first streaming axis**: `Execute(ExecuteRequest) returns (stream ExecuteResponse)` — interim `ExecuteProgress` + terminal `ExecuteCompleted` (a oneof).

- **contracts/conformance/runtime-v1.json** — drives a tiny work_spec (`{steps, rows, indeterminate, fail}`): emit `steps` progress messages (fraction `(i+1)/steps`, or **absent** when indeterminate — exercising the proto3 optional double presence) then a terminal completion (success + `WriteResult.rows_affected`, or `success=false`+error). Vectors: determinate / indeterminate / zero-progress / failure + an empty-work_spec error.
- **inmemory-go** (`plugins/runtime/inmemory-go/`): `spec`/`server`/`main` + streaming harness. Green in `golang:1.25`.
- **inmemory-py** (`plugins/runtime/inmemory-py/`): from-scratch second reference (server-streaming generator). Green in `python:3.12`.

**⚠️ Contract finding surfaced (the 0d forcing function working as designed, like ADR-007):** ADR-005's `core/v1 CapabilityInvokeService.Invoke` is **unary**, but `runtime.Execute` is **server-streaming** — so the stub gateway **cannot mediate a streaming capability**. Runtime is therefore driven **directly** (bypassing the gateway), meaning its C2/C5/C7/C8 + traceparent seams are currently unenforced. Freeze-relevant (`invoke.proto` is in `rat/1`, and any future streaming axis hits this). Captured in [ideas/inbox.md](../ideas/inbox.md) with three resolutions (add `InvokeStream` / direct-dial-with-token / progress-to-event-bus); leaning toward `InvokeStream`. Candidate follow-up ADR queued in [backlog.md](backlog.md). The runtime capability annotation was **deferred** (only needed for gateway routing, which is blocked) — so NO proto change, NO SDK regen this commit.

**Verified (containers):** all EIGHT references (format + engine + storage + runtime, Go + Python) green together.

**Files:** `contracts/conformance/runtime-v1.json` + README, `plugins/runtime/inmemory-go/**`, `plugins/runtime/inmemory-py/**`, `ideas/inbox.md`.

---

## 2026-05-31 — Fix: vendor the Go + Python SDKs (un-ignore them) — makes ADR-006 D1 true

Resolved the repo defect surfaced during storage 0d. The root `.gitignore` had `*.pb.go` + `*_pb2.py` under "Generated artefacts" (alongside the retired `gen/`), which silently excluded the **entire Go SDK** and **all Python message classes** from version control — contradicting [ADR-006](../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md) D1 ("vendored `contracts/sdks/<lang>/` … ARE committed").

- Removed the two patterns from the root `.gitignore` (kept `gen/`); added a note pointing to ADR-006 D1 so it isn't re-added. The only `*.pb.go`/`*_pb2.py` in the repo are the SDKs (examples import the SDK, don't generate), so the un-ignore is safe + targeted. `contracts/.gitignore` still handles the one intended exclusion (`sdks/go/go.sum`).
- Committed the now-trackable **42 Go `*.pb.go`** + **23 Python `*_pb2.py`** files (freshly regenerated; reflect ADR-007's no-context-field + the storage capability annotation). Tom chose "fix now: commit the SDKs."
- Result: a fresh `git clone` can `go build` a reference + `import rat.*` in Python without first running `make gen-sdks`. All four languages' SDKs are now genuinely vendored.

**Files:** `.gitignore`, `contracts/sdks/go/**` (42 `.pb.go`), `contracts/sdks/python/**` (23 `_pb2.py`).

---

## 2026-05-31 — 0d: `storage` axis — two references (Go + Python) + shared golden vectors → `storage/v1` ADR-003 gate MET

Third data-plane axis through the 0d two-reference gate. Storage owns credential vending (no Arrow leg) — one RPC, `VendCredentials` — and is the **C7 tenancy enforcement point**.

- **First reference that CONSUMES identity from the metadata envelope.** Tenant-scoping is storage's whole job, so both impls read `context.identity.tenant` from the `rat-callmeta-bin` metadata header (ADR-007) — never a request field, so a caller can't request another tenant's creds. This exercises the ADR-007 **provider-side read** that format/engine didn't.
- **Capability annotation rolled out to storage.** `storage.proto`'s `VendCredentials` had the capability only in a comment (freeze-blocker #5 only annotated format+engine); added `option (rat.common.v1.capability) = "rat://storage/v1/vend-credentials"` (+ the annotations import) so the gateway can route it. Additive/wire-compatible. Partial progress on the backlog "roll `(rat.capability)` across remaining axes" item. SDK delta: one TS file (`storage_pb.ts`); Rust codegen doesn't emit method options (no diff); Go/Python generated files are gitignored (see finding below).
- **Conformance via a scope receipt.** The credential blob is opaque/provider-specific in production; the references encode the granted scope as JSON `{tenant, prefix, mode, expires_unix_ms}` so the harness can assert the C7 obligation (scope.tenant == caller tenant + requested prefix + mode + short TTL). Added a `TestStorage_TenantComesFromMetadataNotRequest` / `test_tenant_from_metadata_not_request` structural check (vend under a different caller tenant → creds scoped to THAT tenant).
- **inmemory-go** (`plugins/storage/inmemory-go/`): creds/server/main + the axis-generic stub gateway re-pointed at `StorageService` + harness. Green in `golang:1.25`.
- **inmemory-py** (`plugins/storage/inmemory-py/`): from-scratch second reference. Green in `python:3.12`.

**Verified (containers):** all SIX references (format + engine + storage, Go + Python) green together.

**⚠️ Pre-existing repo finding surfaced (not introduced here):** the root `.gitignore` ignores `*.pb.go` (line 28) and `*_pb2.py` (line 29) — so the **Go SDK and the Python message classes are NOT committed** (only TS/Rust + Python grpc-stubs are tracked). This contradicts ADR-006 D1's "vendored SDKs are committed." References build fine against locally-regenerated SDKs (and CI regenerates), but a fresh clone can't import the Go/Python SDK without `make gen-sdks`. Logged in [backlog.md](backlog.md) for a decision.

**Files:** `contracts/conformance/storage-v1.json` + README, `contracts/proto/rat/storage/v1/storage.proto` (annotation), `contracts/sdks/typescript/rat/storage/v1/storage_pb.ts`, `plugins/storage/inmemory-go/**`, `plugins/storage/inmemory-py/**`.

---

## 2026-05-31 — 0d: `engine` axis — two references (Go + Python) + shared golden vectors → `engine/v1` ADR-003 gate MET

Second data-plane axis through the 0d two-reference gate, reusing the format pattern (shared conformance JSON + two independent impls + the stub ADR-005/007 gateway).

- **Shared golden vectors** — `contracts/conformance/engine-v1.json` (+ README grammar note): CREATE/INSERT via Execute (rows_affected 0 vs 1), Query (SELECT, WHERE, projection), Preview (bounded by `limit`), + `rows_exclude_keys` to assert projection drops columns; 2 error vectors (unknown table, empty SQL).
- **Mini-SQL** — a deliberately tiny, fully-specified grammar (`CREATE TABLE` / `INSERT … VALUES` / `SELECT … [WHERE] [LIMIT]`) so two independent parsers stay in lockstep: the SAME three regexes in Go (`sql.go`) and Python (`sql.py`). The point under test is the engine WIRE contract, not SQL fidelity. Self-contained in-memory tables (the engine↔format handoff is separate integration work, noted).
- **inmemory-go** (`plugins/engine/inmemory-go/`) — first reference: store/sql/stream/server/main + a stub gateway (`gateway_test.go`, the axis-generic ADR-005/007 stub re-pointed at `EngineService`) + harness routing Execute/Query/Preview through the gateway (C5 + C8 + traceparent gate). Green in `golang:1.25`.
- **inmemory-py** (`plugins/engine/inmemory-py/`) — second, from-scratch reference; imports the vendored Python SDK; loads the same JSON. Green in `python:3.12`.
- Context rides in `rat-callmeta-bin` metadata throughout (ADR-007) — these references are built natively on the post-migration contract.

**Verified (containers):** all FOUR references (format + engine, Go + Python) green together — `go test ./...` (both Go) and `python harness_test.py` (both Python).

**Files:** `contracts/conformance/engine-v1.json` + README, `plugins/engine/inmemory-go/**`, `plugins/engine/inmemory-py/**`.

---

## 2026-05-31 — ADR-007 migration executed: `RequestContext` field → `rat-callmeta-bin` metadata across the contract

Implemented [ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (the decision landed in commit `9ff3cac`; this is the implementation, kept separate per one-ADR-per-commit).

- **Protos:** stripped `RequestContext context = 1` from **37 messages** (36 request messages across the 18 axis services + `core/v1 InvokeRequest`), each replaced with `reserved 1;`; removed the now-unused `context.proto` import from those 19 files. `context.proto` prose rewritten to specify `rat-callmeta-bin` carriage + the "why metadata not field 1" rationale (messages unchanged). `event.proto` keeps its in-body `RequestContext` (async exception — core-stamped once at emit, no per-hop metadata channel) with the carriage distinction documented. `strategy.proto` Apply comment corrected (providers reached via the core invoke gateway, not "via RequestContext"). `buf lint`/`build` clean; `buf format` applied.
- **`buf breaking` confirms exactly 37 findings, all "field 1 `context` deleted"** — nothing collateral, exactly as the ADR predicted; allowed in `v1-preview`.
- **SDKs:** regenerated all 4 (Go/Python/TS/Rust) via `make gen-sdks`; the generated request types no longer carry `context`.
- **References + gateway updated to the metadata model:**
  - Stub gateway (`inmemory-go/gateway_test.go`) now reads the inbound `rat-callmeta-bin` envelope, **validates traceparent** (new C1 gate — possible now that trace is in metadata, not the opaque payload; rejects missing/ill-formed with `INVALID_ARGUMENT`), and constructs the downstream envelope (trace verbatim, identity re-stamped) as outbound metadata — still never deserializing the payload. New test `TestGateway_RejectsMissingTraceparent`.
  - Both harnesses (`inmemory-go`, `inmemory-py`) carry context via `rat-callmeta-bin` metadata instead of a request field.
- **Behavior-preserving — verified:** the **unchanged** shared golden vectors still pass on both impls (Go in `golang:1.25`, Python in `python:3.12`), the strongest evidence the migration changed carriage, not semantics. The ADR-003 `format/v1` two-reference cross-run remains green.

**Caveat (recorded, non-blocking):** `make gen-check` hit the known BSR rate-limit (429) on its *temp* regen (the done.md 2026-05-31 multi-SDK caveat) → false "python stale." The committed SDKs are correct — proven by both harnesses passing against them. Network-bound check, not a content defect.

**Files:** `contracts/proto/**` (20 files), `contracts/sdks/**` (regenerated), `plugins/format/inmemory-go/{gateway_test.go,harness_test.go}`, `plugins/format/inmemory-py/harness_test.py`, `roadmap/**`.

---

## 2026-05-31 — ADR-007: call-context transport (cross-cutting context → metadata, not payload)

Resolved the freeze-blocking finding the 0d stub gateway surfaced. **[ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (Accepted):** the cross-cutting envelope (`RequestContext` = trace + identity + deadline) moves out of message field 1 into a single binary transport-metadata header `rat-callmeta-bin`. The keystone's message *shape* is kept verbatim; only the *carrier* changes.

**Why:** ADR-005 committed the gateway to being a generic proxy that forwards the payload *without interpreting it* — but the gateway must validate `traceparent` and re-stamp `identity` per hop, both impossible on an opaque payload while the envelope lives inside it. Moving the envelope to metadata makes ADR-005's generic-proxy guarantee literally true, lets the gateway do its stated job, and eliminates the forgeable in-body identity footgun by construction. Refines ADR-005 (upholds it); relocates — does not discard — the keystone identity model. Rejected the splice-field-1, keep-as-mirror, and identity-only-in-metadata alternatives (reasons in the ADR).

**Process:** ADR-only commit (per the one-ADR-per-commit rule). The proto migration (strip `RequestContext context = 1` from 18 axis services + `InvokeRequest`; regen 4 SDKs; SDK metadata interceptor; update both `format` refs + the stub gateway; re-run the unchanged golden vectors) is queued as the next implementation step — **not** in this commit.

**Files:** `docs/architecture/adrs/007-call-context-transport.md`, `docs/architecture/adrs/README.md` (index), `ideas/inbox.md` (finding marked promoted), `roadmap/**`.

---

## 2026-05-31 — 0d: second `format` reference (inmemory-py) + shared golden vectors + stub ADR-005 gateway → `format/v1` ADR-003 gate MET

The [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) two-reference gate for `format/v1` is satisfied: a **second, independently-written** reference passes the **same golden vectors** as the first, both loading one shared artifact.

- **Shared golden vectors** — `contracts/conformance/format-v1.json` (+ README). Language-neutral, executable: the *single source of truth* for format/v1 behavior (lifecycle append→scan→merge→overwrite→maintain + 2 error vectors). This is how "run against each other on golden data" is met *literally* (one file, two impls) rather than by hand-copied-but-equal vectors.
- **Go harness refactored** — `inmemory-go/harness_test.go` now loads the shared JSON (was inline vectors) and drives everything through a generic vector executor.
- **Stub ADR-005 invoke-gateway** — `inmemory-go/gateway_test.go` (~150 LOC, test-only, throwaway). The Go harness no longer dials FormatService directly; it goes harness → `core/v1 CapabilityInvokeService.Invoke` → format plugin. The gateway is a **faithful generic byte-relay**: a passthrough codec (`Name()=="proto"`) forwards the serialized payload without deserializing it, capability→method routing is read from the `(rat.common.v1.capability)` method annotation (freeze-blocker #5 machinery, not a hand map), and it enforces C5 (capability allowlist) + emits C8 audit (asserted: one record per mediated call). Validates the mediation seams, not just the plugin-to-plugin data contract.
- **Second reference, `inmemory-py`** — `plugins/format/inmemory-py/` (store.py / streams.py / server.py / main.py / harness_test.py + README + pinned requirements). From-scratch Python code path (not a Go port), imports the vendored Python SDK via `PYTHONPATH`. Its harness loads the SAME shared JSON.

**Verified (containers, no host installs):**
- Go (`golang:1.25`): `go test ./...` green — full lifecycle + error vectors, all mediated through the stub gateway. `go.mod` cleanly promotes `google.golang.org/protobuf` to a direct dep (`go mod tidy`).
- Python (`python:3.12`, grpcio 1.80.0 / protobuf 7.35.0 — matches the gencode runtime pin exactly): `python harness_test.py` → `PASS`.

**Finding surfaced (captured in [ideas/inbox.md](../ideas/inbox.md)):** building the generic proxy exposed a real contract tension — the gateway must re-stamp `identity.caller_plugin` per hop and never trust wire identity, but `RequestContext` (with identity) lives *inside* the payload a generic proxy won't deserialize. So re-stamped identity has to ride in gRPC metadata, which contradicts "RequestContext travels as field 1 of every request." Needs a resolution (metadata-only / splice-field-1 / two-channel) before any axis freezes. Exactly the ADR-003-predicted "second implementation reveals the contract flaw" outcome.

**Still open before `format/v1` advances `v1-preview`→`v1`:** the identity-transport decision above; a typed-Arrow conformance pass (the bulk leg is still an in-process registry stand-in in both refs).

**Files:** `contracts/conformance/**`, `plugins/format/inmemory-go/{harness_test.go,gateway_test.go,go.mod,go.sum}`, `plugins/format/inmemory-py/**`, `ideas/inbox.md`.

---

## 2026-05-31 — 0d started: first reference plugin (rat-format-inmemory-go) — commit `c472620`

First real RAT v3 *code*: a reference `kind: format` plugin under `plugins/format/inmemory-go/` (ADR-006 D2 layout), implementing the full `format/v1` wire contract to validate it by building against it (ADR-003 forcing function), not as production storage.

- `store.go` — in-memory ordered-row tables: append / merge(upsert on keys) / overwrite / scan / maintain; per-mutation snapshot.
- `stream.go` — in-process stand-in for the out-of-band Arrow leg: single-use ticket registry; `Resolve` returns producer-hosted `ArrowStream{transport=FLIGHT}`; mutating RPCs pull a caller-hosted source. (Real Arrow Flight deferred to a production reference.)
- `server.go` — the 5 `FormatService` RPCs over gRPC; empty `TableRef`/missing `merge_keys` → `INVALID_ARGUMENT`; `Maintain` leaves `rows_affected` absent (proto3 optional = unknown).
- `main.go` — gRPC server entrypoint.
- `harness_test.go` — golden-data conformance harness: append→scan→merge→overwrite→maintain + 2 error cases over REAL in-process gRPC (bufconn). The vectors a second independent impl must also pass.

**Supporting:** committed the Go SDK `go.mod`+`go.sum` (reproducible builds; `go mod tidy` resolved grpc v1.81.1 + protobuf v1.36.11); dropped the go.sum gitignore; `.gitignore` for the compiled binary. Plugin depends on the SDK via a local `replace`.

**Verified (golang:1.25 container):** `go build` / `go vet` / `go test` all green — 3 tests PASS over real gRPC.

**Process note:** a long cancelled tool-batch mid-session left a stale compiled 15MB binary + a broken `server.go` (dead `errUnused` + stray brace) uncommitted, and the first plugin commit + roadmap edits never landed. Terminal stdout was also corrupting. Recovered by checking real git/file state (not terminal output), rewriting `server.go` clean, removing the binary, re-verifying green in-container, then committing fresh as `c472620`.

**Next (ADR-003 gate):** a SECOND independent `format` impl — e.g. `plugins/format/inmemory-py` — running the SAME golden vectors, before `format/v1` can freeze. (The sequencing panel — see chat — recommends also routing the harness's control RPCs through a ~200-LOC throwaway stub invoke-gateway so the freeze also validates the ADR-005 mediation seams, not just the plugin-to-plugin data contract.)

**Files:** `plugins/format/inmemory-go/**`, `contracts/sdks/go/{go.mod,go.sum}`, `contracts/.gitignore`.

---

## 2026-05-31 — Multi-language SDKs: Python, TypeScript, Rust (+Go) — commit `78be8b4`

**What:** Extended codegen from Go-only to all four target languages (Tom: "python, ts and ruff[=Rust]"), realizing the any-language promise (ADR-001 / vision #3). Each is a committed, peer `contracts/sdks/<lang>/` with its own `buf.gen.<lang>.yaml`:
- **Go** — protocolbuffers/go + grpc/go (43 files + go.mod; compiles under golang:1.25)
- **Python** — protocolbuffers/python + grpc/python (46)
- **TypeScript** — bufbuild/es + connectrpc/es (42)
- **Rust** — community neoeinstein-prost + neoeinstein-tonic (39)

`scripts/gen-sdks.sh` LANGS=(go python typescript rust); `--check` loops all four (excludes hand-added go.mod/go.sum). CI (`contracts.yml`) regenerates all four (was Go-only). ADR-006 amended (diagram + stacks + BSR-rate-limit note).

**Each language's codegen empirically verified in-container** (buf generate exit 0, file counts above). `make check` (buf lint) green.

**⚠️ Operational caveat (real, recorded):** codegen uses **remote buf.build plugins** → regenerating all four in quick succession hits **BSR rate limits** (429); `make compile-sdks` also flaked on `go get` (network) during this session. Neither is a content defect — the committed SDKs are correct, Go compiled clean earlier — but it means `make gen-check`/`compile-sdks` are network-bound and can transiently fail locally. Future hardening: retry/backoff on 429, or local (non-remote) codegen plugin images. Not blocking.

---

## 2026-05-31 — Codegen pipeline: make targets + gen script + CI + per-commit hook

**What:** Built the SDK-codegen + verification toolchain that ADR-006 D3 calls for. Three pieces (commits `654c3f1` pipeline, `4abffe7` Claude hook):

1. **`scripts/gen-sdks.sh` + `Makefile`** — containerized (podman/docker, no host installs). `make check` = FAST per-commit gate (buf lint, ~5s); `make verify` = FULL (lint + build + SDK freshness + Go SDK compile, slow/network); `make gen-sdks` / `gen-check` / `compile-sdks`. Vendored Go SDK landed at `contracts/sdks/go/` (42 files + go.mod), compiles clean under `golang:1.25` (resolves the ADR-006 D3 Go≥1.25 floor). `buf.gen.yaml` → `buf.gen.go.yaml` (per-language, outputs to `sdks/go`); `.gitignore` drops the retired `/gen/`.
2. **`.github/workflows/contracts.yml`** — `validate` job (buf lint+build, +`buf breaking` on PRs) and `sdks` job (regen + Go compile; PRs **fail on stale committed SDK**; push to main **auto-commits regenerated SDKs back** — the "autogenerate on GitHub" ask).
3. **Per-commit Claude hook** (`.claude/`, via claude-engineer) — PreToolUse/Bash with `if:"Bash(git commit *)"` → `.claude/hooks/contracts-check.sh`. The hook process only spawns on `git commit`; the script then self-guards on staged `contracts/proto/` files (pure shell, no container if none staged) before running `make check`; exit 2 blocks on lint failure. **Never per-edit, never `make verify`.** Verified all 4 paths against the real script: nothing-staged 11ms/exit0, non-proto-staged 6ms/exit0, clean-proto exit0 (ran make check), broken-proto exit2/blocked with the lint error fed back. Caveat: only gates Claude-run commits (CI is the real boundary; human-commit gating would need a repo git pre-commit hook).

**Verified:** `make check` / `gen-check` / `compile-sdks` all exit 0 locally; hook tested across all 4 paths; settings.json valid + `$schema`/`env`/`permissions` preserved.

**Files:** `Makefile`, `scripts/gen-sdks.sh`, `.github/workflows/contracts.yml`, `contracts/buf.gen.go.yaml`, `contracts/.gitignore`, `contracts/sdks/go/**`, `.claude/settings.json`, `.claude/hooks/contracts-check.sh`.

---

## 2026-05-31 — ADR-006: SDK distribution + reference-plugin layout + codegen toolchain

**What:** Before scaffolding 0d (first reference implementations — the first code), pinned three project-shaping decisions in [ADR-006](../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md), prompted by Tom's point that plugins must be authorable in *any* language (ADR-001 / vision #3), not just Go.

- **D1 SDK distribution:** vendored `contracts/sdks/<lang>/` (Go/Python/TS as peer committed dirs, none privileged), regenerated-not-hand-edited; **BSR publication deferred** as the later external-distribution channel. Mirrors Kubernetes (vendor for the monorepo, publish for outsiders). Chosen over BSR-now (needs network + org; sandbox blocks) and protos-only/local-codegen (multi-step build in a fiddly-toolchain env).
- **D2 layout:** reference plugins under `plugins/<axis>/<impl>-<lang>/`; ADR-003's two-reference rule satisfied per critical axis by two impls in *different* languages running shared golden-data vectors — cross-language interop is the strongest form of the rule.
- **D3 codegen:** containerized `buf generate` driven by a committed `scripts/gen-sdks.sh`. Captured two gotchas already hit: the generated Go gRPC stubs need Go ≥ 1.25 (base `golang:1.23` image failed to build the SDK this session — pin the image or pin grpc/protobuf), and `buf generate` uses remote buf.build plugins (network) so the script must handle local-plugin fallback.

**Process note / correction:** earlier this session I claimed the Go SDK "compiles clean" — it does NOT yet; codegen *produces* 42 Go files but compiling them failed on the Go-version floor above. ADR-006 D3 records the real situation; resolving it is the first 0d task.

**Next:** scaffold per D1/D2/D3 — `buf.gen.<lang>.yaml` + `scripts/gen-sdks.sh` (settle the Go-version/grpc-pin), generate+commit `sdks/`, drop the transient `gen/` path, then `plugins/format/inmemory-go/`.

**Files:** `docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md` (new), `docs/architecture/adrs/README.md`.

---

## 2026-05-30 — Freeze-blocker #10a: debug_redact on sensitive bytes fields

reviews/06 SEC-8 (part of #10): "never logged" was a comment; `[debug_redact = true]` makes redaction structural (reflection/text-marshal omit the field). Applied to the four sensitive bytes fields: `secret.ResolveResponse.value`, `identity.AuthenticateRequest.credential`, `storage.VendCredentialsResponse.credentials`, `common.ArrowStream.ticket`. Confirmed buf 1.47.2 accepts the option via an isolated test first.

**Verified:** buf lint 0 / build 0 / generate 42 Go files. Commit `(10a)`.

### #10 remaining — `artifact`/digest manifest block (AUTH-14⊕SEC-15) — NOT YET DONE

The other half of #10 (add a top-level `runtime` discriminator + `artifact` {ref, digest} block to `plugin.v1.json`, required for out-of-process plugins; update examples; tie `trust.signature` to `artifact.digest` + the authz envelope) is **deliberately deferred**. Rationale: per reviews/06 this is **additive/GA-safe** — adding a property to a schema we own does not break any plugin's wire contract (unlike the structural #1–9 changes), so it can land after the `rat/1` freeze without a flag-day. Only the "what the signature covers" *decision* carried a freeze rider, and that decision is recorded (sign artifact.digest + provides/requires/resources). Pairs with the two #9f doc-pins (pagination default, timestamp ratification) as the additive tail.

---

## 2026-05-30 — Freeze-blocker #9c/9d/9e: data-plane shapes + schema/proto slivers

Continued the #9 small-wire-fix cluster (reviews/06). All buf-verified (lint 0 / build 0 / generate 42); each its own commit.

**9c (`9c25c26`) — data-plane shapes.** `data.proto` ArrowStream: pinned the wire protocol (new `ArrowTransport` enum = FLIGHT + `transport` field — "gRPC/Flight-style" was not a spec) and encoded host-vs-dial (new `ArrowStreamRole` enum PRODUCER_HOSTED/CONSUMER_HOSTED + `role` field — same type used in opposite directions); ticket-security note (short-TTL/single-use/bound, SEC-14; detailed spec is GA). `observability.proto` Ingest: client-streaming → **bidi-streaming** (the old single terminal ack gave no backpressure/partial-failure feedback; bidi acks per batch).

**9d (`f290601`) — schema shape.** `plugin.v1.json` `contributes.slots[].target`: bare `capabilityUri` → `capabilityRef` (API-17, consistency with provides/requires; string↔object is breaking). scd2 example updated to the wrapped shape; both manifests re-validate.

**9e (`277a09f`) — proto slivers.** API-13 sentinel → proto3 `optional` presence: `WriteResult.rows_affected` (absent==unknown) + `ExecuteProgress.fraction` (absent==indeterminate). API-12: `strategy.Apply.options` encoding pinned (UTF-8 JSON vs metadata_schema). API-11: `scheduler.WatchDue` delivery pinned at-least-once (reconciler dedupes by trigger_id+fired_at).

**Remaining in #9 (low-value doc-pins, optional):** pagination-default note on `state.List` / `marketplace.Search` (v1 returns unbounded; a future `page_size` default must preserve that) and the timestamp-type ratification note (int64 unix-ms is the deliberate, final rat/1 choice). Both are comments, not wire changes — arguably addable post-freeze; deferred.

**Files:** `contracts/proto/rat/common/v1/data.proto`, `contracts/proto/rat/observability/v1/observability.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `contracts/proto/rat/strategy/v1/strategy.proto`, `contracts/proto/rat/scheduler/v1/scheduler.proto`, `contracts/schema/plugin.v1.json`, `contracts/examples/rat-strategy-scd2.plugin.yaml`.

---

## 2026-05-30 — Freeze-blocker #9a+9b: secret found semantics + decision error model

Freeze-blocker #9 (the small-wire-fix cluster, reviews/06 C-5 + API-1d) is being landed in sub-commits. First two done:

**9a (`22b76e2`) — `secret.Resolve.found` semantics.** Pinned at freeze: `found=false` deliberately conflates "ref does not exist" with "ref exists but unauthorized" (anti-enumeration). Auth failures return `found=false` + empty value, NOT `PERMISSION_DENIED`. Comment-only but freeze-gated (pins the meaning of the existing `found` field).

**9b (`fcbe8bb`) — decision-RPC error model.** A deny on a *successful* decision rpc can't be carried by a gRPC status code, so `identity.Authorize` + `tenancy.Decide` get an in-band enumerated `deny_code` alongside `allowed`; free-text `reason` demoted to log/audit-only (field 3), MUST NOT drive caller logic (enumeration-oracle, reviews/04). Per-package `DenyCode` enums. Header ERROR MODEL note on both: transport failures → gRPC status; decisions → `allowed` + `deny_code`.

**Process note:** an earlier attempt committed only the secret change while claiming all three (a linter re-applied my reverted identity/tenancy edits asynchronously, and my re-edits failed on the stale-file guard). Caught on verification: amended the 9a commit message to match its actual content (secret only), then landed identity+tenancy cleanly as 9b with fresh reads. No false claim remains in history.

**Verified:** buf lint 0 / build 0 / generate 42 Go files; dup-free.

**Remaining for #9:** 9c (ArrowStream protocol+role, Ingest shape) + 9d (slots.target wrap, options encoding, timestamp ratification, pagination default, scheduler delivery doc, optional-presence).

**Files:** `contracts/proto/rat/secret/v1/secret.proto`, `contracts/proto/rat/identity/v1/identity.proto`, `contracts/proto/rat/tenancy/v1/tenancy.proto`.

---

## 2026-05-30 — Freeze-blocker #8: catalog.MergeBranch idempotency + concurrency

**What:** reviews/06 #8 (ARCH-4 / I-18) — `MergeBranch` is the publish gate of the pipeline model and the reconciler retries it, but it took only branch names: a retried merge could double-apply and concurrent merges into main could lose updates. Added two request fields + one response field.

**`MergeBranchRequest` gains:**
- `expected_into_snapshot` (4) — optimistic-concurrency guard; the merge applies only if `into_branch` is still at this snapshot, else it fails and the caller re-reads/re-tests. Closes the lost-update window between concurrent merges. Empty == unconditional (sole-writer only).
- `idempotency_key` (5) — stable id for the logical merge (e.g. run id); a retry with a key that already committed is a no-op returning the original result. Closes the double-apply window under reconciler retry.

**`MergeBranchResponse` gains:** `already_applied` (2) — true when the response reflects a previously-committed merge (idempotent retry) rather than a fresh apply.

**Scope:** only the request-shape change is freeze-gated. The separate I-18 gap — how the catalog learns what `format.Write` wrote to a branch (a new commit-linkage RPC) — is additive and stays GA-deferred.

**Verified:** buf lint 0 / build 0 / generate 42 Go files (fields compile into existing catalog package files); dup-free.

**Next:** freeze-blocker #9 — the smaller-wire-fix cluster (error convention, `secret.Resolve.found`, Arrow role field, `Ingest` shape, timestamp type, `slots.target` wrap + freeze-slivers).

**Files:** `contracts/proto/rat/catalog/v1/catalog.proto`.

---

## 2026-05-30 — Freeze-blocker #7: common/v1/event.proto (async event-bus envelope)

**What:** reviews/06 ARCH-1 — the async plane (event bus, one of the six core things) had NO wire envelope, so distributed tracing broke across the async boundary and multi-tenant event routing was undefined, while every sync RPC carried `RequestContext`. Added `common/v1/event.proto` defining the `Event` envelope.

**Shape:** `Event` = `{ RequestContext context, string event_id, string type, int64 timestamp_unix_ms, string source, bytes payload, string partition_key }`:
- `context` — the SAME trace+identity+tenant envelope sync RPCs carry, so a `pipeline_run_failed` traces back through its `pipeline_run_requested` within one `correlation_id`, across every reacting plugin; identity is core-stamped at emit time (non-forgeable, keystone rules).
- `event_id` — idempotent redelivery (at-least-once transports redeliver; a subscriber that saw this id no-ops). Distinct from `correlation_id` (shared across an operation's events).
- `type` — subscription match key (overview.md: subscriptions = [event, action]); open-set, lower_snake_case.
- `source` — emitting component (core reconciler or core-mediated plugin id); async analogue of `identity.caller_plugin`.
- `payload` — serialized type-specific message, opaque to the transport (routes by type+tenant without interpreting it, like invoke.proto's payload).
- `partition_key` — optional ordered-delivery key (e.g. per-run-id), where the transport supports it.

Protocol fixed, transport pluggable (ADR-002 D2/D4: NATS JetStream default / Kafka / Redis Streams).

**Verified:** buf lint 0 / build 0 / generate 42 Go files (`event.pb.go`; message-only, no service); dup-free.

**Next:** freeze-blocker #8 — `catalog.MergeBranch`: add `expected_snapshot` + `idempotency_key`.

**Files:** `contracts/proto/rat/common/v1/event.proto` (new).

---

## 2026-05-30 — Freeze-blocker #6: core/v1/invoke.proto (capability-invoke service)

**What:** Added the wire artifact ADR-005 requires and reviews/06 C-6 (AUTH-2 ⊕ ARCH-2) flagged as missing — the mechanism by which a plugin actually CALLS a capability it `requires`. Before this, "the core wires providers via the registry" was comment-deep with no wire mechanism; the headline call-by-capability feature was unbuildable.

**Shape (core-mediated per ADR-005):** new `CapabilityInvokeService.Invoke(InvokeRequest) → InvokeResponse`:
- `InvokeRequest` = `{ RequestContext context, string capability, bytes payload }` — the capability URI (e.g. `rat://format/v1/merge`) + the serialized request message for the target axis method.
- `InvokeResponse` = `{ bytes result }` — the serialized provider response.

**How it works:** a generic proxy. The plugin calls `Invoke` on the core API gateway instead of dialing the provider. The core resolves capability→provider (registry + the `(rat.common.v1.capability)` method annotation from #5), enforces C2/C5/C7/C3, re-derives `identity.caller_plugin` for the downstream hop, stamps C1 trace, emits the C8 audit record, then forwards `payload` to the provider's method without interpreting it (no per-axis core knowledge → no 7th core thing). Bulk Arrow data still bypasses the core via `ArrowStream`; `Invoke` carries only control RPCs. Enforcement failures surface as gRPC status, not a response field.

**Verified:** buf lint 0 / build 0 / generate 41 Go files (`invoke.pb.go` + `invoke_grpc.pb.go`); dup-free.

**Next:** freeze-blocker #7 — async event-bus envelope (`common/v1/event.proto`) OR explicitly carve the async plane out of the `rat/1` freeze.

**Files:** `contracts/proto/rat/core/v1/invoke.proto` (new).

---

## 2026-05-30 — Freeze-blocker #5: capability annotations + format.Write split

**What:** reviews/06 I-3 + I-4 (AUTH-8 + AUTH-9). Freeze-critical parts done; cross-axis annotation rollout deferred as additive.

1. **`common/v1/annotations.proto` (new):** `extend google.protobuf.MethodOptions { string capability = 70001; }` — the machine-readable capability⇄method binding. The mapping from capability URI → `(service, method)` previously lived only in comments, unreadable by the C5 enforcement gateway (ADR-005) and C6 conformance harness. Must be in the frozen `rat/1` surface (freeze-dependency). buf accepts the custom option; `annotations.pb.go` generates.

2. **Split `format.Write` → `Append`/`Merge`/`Overwrite` (breaking → freeze):** the single `Write` RPC keyed by a `WriteMode` enum meant a plugin that `provides` only `append` couldn't be enforced at method level. Now each is a distinct RPC 1:1 with a capability; `overwrite` gets the `rat://format/v1/overwrite` URI it previously lacked. `WriteMode` removed; per-RPC request+response messages (`{Append,Merge,Overwrite}Request/Response` — buf STANDARD requires a unique response type per RPC, so no shared `WriteResponse`); `merge_keys` only on Merge.

3. **Annotated format (5 methods) + engine (3).** **Engine needed NO split** — execute/query/preview were already 3 distinct RPCs 1:1 with capabilities; the blocker's "split engine.Execute" wording didn't apply, so engine just got the annotation. Noted in-proto.

**Deferred (additive, NOT freeze-blocking):** roll `(rat.capability)` across the other 14 axis services — adding a method option is wire-compatible (`buf breaking` FILE doesn't flag it). Tracked in backlog; land before the C5 gateway / C6 harness.

**Verified:** buf lint 0 / build 0 / generate 39 Go files (annotations.pb.go + the split format messages); both example manifests re-validate (deltalake's scan/merge/append capabilities all survive the split); dup-free. (Caught + fixed a buf STANDARD failure pre-commit — initial shared `WriteResponse` violated "unique response type per RPC".)

**Next:** freeze-blocker #6 — add `core/v1/invoke.proto` (the ADR-005 core-mediated capability-invoke service).

**Files:** `contracts/proto/rat/common/v1/annotations.proto` (new), `contracts/proto/rat/format/v1/format.proto` (split + annotate), `contracts/proto/rat/engine/v1/engine.proto` (annotate).

---

## 2026-05-30 — Freeze-blocker #4: auditlog.proto tamper-evidence + completeness

**What:** reviews/06 C-3 (SEC-5 ⊕ API-5) — the audit trail was "tamper-evident" in name only and couldn't report partial failure. Four coupled fixes to `auditlog.proto`:

1. **Core authors the chain, not the sink/caller:** `id` + `prev_hash` are core-assigned; `Append` is **core-only** (capability not grantable to ordinary plugins) → no plugin can inject records or fork the chain, no `prev_hash` races.
2. **Each record core-signed:** added `AuditRecord.signature` (Ed25519 over the canonical bytes) → a third-party sink can *verify* the chain but can't forge/reorder/drop without detection.
3. **Canonical serialization pinned in-contract:** specified the deterministic proto3 form the signature/hash cover (ascending field order, each field once, minimal varints, defaults omitted, no unknowns) → cross-impl verification is well-defined. Un-retrofittable once chains exist → pre-freeze.
4. **Per-record response, prefix-only commit:** replaced `AppendResponse.appended:int64` with `repeated RecordAck` (`AppendStatus` COMMITTED/DUPLICATE/REJECTED + `RejectCode` BAD_SIGNATURE/CHAIN_BREAK/STORAGE_ERROR); commit is a contiguous prefix (a REJECTED entry ⇒ all later uncommitted, so no fork on partial failure); `last_committed_id`/`last_committed_hash` watermark lets a reconnecting emitter resume with no gap. `APPEND_STATUS_DUPLICATE` is the idempotent-retry valve. Two prose invariants captured: a dropped/rejected record is itself a meta-audit event; duplicate-on-retry must not double-append.

**Verified:** buf lint 0 / build 0 / generate 38 Go files (RecordAck + 2 enums compile into the existing auditlog package files — no new file count); dup-free; no stale `appended` refs.

**Next:** freeze-blocker #5 — `annotations.proto` + `(rat.capability)` method option + split `Write`/`engine.Execute` per-mode (do together).

**Files:** `contracts/proto/rat/auditlog/v1/auditlog.proto`.

---

## 2026-05-30 — Freeze-blocker #3: state.proto (key grammar + Put tri-state + CAS conformance)

**What:** reviews/06 #3 — three coupled fixes to `state.proto` (the tier-0 state backend the reconciler depends on):

1. **Key/prefix grammar (SEC-2):** `key`/`prefix` were unconstrained strings → naive namespace-prefix concat could be escaped. Now a stated conformance rule: reject empty key / >512B / non-UTF-8 / NUL+control chars / `.`+`..` traversal segments → `INVALID_ARGUMENT`. Makes C3 isolation a real boundary, not comment-deep.
2. **Put outcome tri-state (C-4 / API-1 reconciler axis):** replaced `PutResponse.committed:bool` with a `PutOutcome` enum — `COMMITTED` / `CONFLICT` (lost CAS race, deterministic, didn't write) / `UNKNOWN` (timeout/partition, may-or-may-not have committed). `committed:bool` couldn't express UNKNOWN, which is fatal for lease fencing (a "maybe-committed" renewal can't be relied on).
3. **CAS conformance + DynamoDB (C-4 / ARCH-3):** turned "MUST be linearizable" from prose into a stated conformance obligation (single-key linearizable CAS + ordered Watch, gated by the 0f suite); resolved the contradiction where overview.md advertised DynamoDB (eventually-consistent default) as a cloud state backend → split-brain leader election. DynamoDB now annotated as strongly-consistent-mode-or-solo-only in the topology table; removed it from the proto's plugin-examples list.

**Verified:** buf lint 0 / build 0 / generate 38. No remaining references to the old `committed` field.

**Next:** freeze-blocker #4 — audit `AppendResponse` → per-record `RecordAck` (prefix-only) + canonical-serialization pin + core-assigned id/prev_hash.

**Files:** `contracts/proto/rat/state/v1/state.proto`, `docs/architecture/overview.md` (topology footnote).

---

## 2026-05-30 — Freeze-blocker #2: format capability URI rename

**What:** reviews/06 C-2 (API-7 ⊕ AUTH-1) — `format` was the one axis whose capability URI (`rat://format-capability/v1/*`) didn't match its proto package (`rat.format.v1`), breaking the contract-triple's "URI mirrors the package coordinate" invariant for the most-referenced axis. Renamed `rat://format-capability/v1/*` → `rat://format/v1/*`.

**Changed (live contract + architecture doc):** `format.proto` (capability map + RPC comments), `strategy.proto` (prose), `rat-format-deltalake.plugin.yaml` + `rat-strategy-scd2.plugin.yaml` (the `provides`/`requires` URIs), `INVALID-examples.md`, `schema/README.md`, and `docs/architecture/overview.md` (`kind: format-capability` → `kind: format` + the URI string).

**Deliberately NOT changed:** historical records — `reviews/00,02,06` and `docs/conversations/*` keep `format-capability` (reviews/06 literally documents it as the bug; rewriting history would be dishonest).

**Verified:** buf lint 0 / build 0 / generate 38; both example manifests re-validate against the schema (containerized).

**Next:** freeze-blocker #3 — state.proto (key/prefix grammar + Put tri-state + CAS conformance/DynamoDB).

**Files:** `contracts/proto/rat/format/v1/format.proto`, `contracts/proto/rat/strategy/v1/strategy.proto`, `contracts/examples/{rat-format-deltalake,rat-strategy-scd2}.plugin.yaml`, `contracts/examples/INVALID-examples.md`, `contracts/schema/README.md`, `docs/architecture/overview.md`.

---

## 2026-05-30 — Freeze-blocker #1: context.proto keystone rewrite (3-principal identity)

**What:** Applied the first + highest-leverage freeze-blocker from [reviews/06](../reviews/06-proto-contract-review.md) C-1 (SEC-1 ⊕ AUTH-12, the keystone). Rewrote `contracts/proto/rat/common/v1/context.proto`, replacing the single forgeable, semantically-ambiguous `subject` string with three distinct principals + structural trace/identity separation. Commit `322126c`.

**New `RequestContext` shape:**
- `TraceContext` (traceparent/tracestate/correlation_id) — caller-supplied, propagated verbatim, diagnostic-only.
- `Identity` — all fields CORE-stamped, never trusted from the wire:
  - `caller_plugin` — immediate caller, server-derived from the hop's channel credential, **re-derived every hop, never propagated**. C3 state namespace = `(caller_plugin, tenant)`.
  - `subject` — a `SubjectAssertion` (core signature + `bound_correlation_id` + `expires_unix_ms`), not a bare string. Verification contract: every consuming hop verifies the signature, checks `bound_correlation_id == inbound correlation_id` (anti-replay/confused-deputy), and checks TTL. Propagated.
  - `tenant` — server-stamped, propagated, never caller-writable (C7 structural).
- `deadline_unix_ms` — unchanged hint.

**Downstream coherence (other half of AUTH-12):** `state.proto` C3 namespace now keys on `identity.caller_plugin` (was the contradictory `subject (the calling plugin)`); comment refs → `context.identity.{tenant,subject}` in storage/secret/billing/tenancy/identity. Composes with ADR-005 (the core-mediated gateway stamps `caller_plugin` per hop).

**Verified (containerized):** buf lint 0, build 0, generate 38 Go files; dup-scan clean (python-verified across all 6 touched files).

**Next:** freeze-blocker #2 — rename `format` capability URIs `rat://format-capability/v1/*` → `rat://format/v1/*`.

**Files:** `contracts/proto/rat/common/v1/context.proto` (rewrite), `state/v1/state.proto`, `storage/v1/storage.proto`, `secret/v1/secret.proto`, `billing/v1/billing.proto`, `tenancy/v1/tenancy.proto`.

---

## 2026-05-30 — ADR-005: capability invocation model — core-mediated

**What:** Resolved the one open design decision from [reviews/06](../reviews/06-proto-contract-review.md) C-6 (AUTH-2 ⊕ ARCH-2) — how a plugin actually *calls* a capability it `requires`, which the protos never expressed on the wire. Wrote [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md).

**Decision:** **core-mediated** (systems-architect's position) over direct-dial (plugin-author's). Control-plane capability calls go through a new core capability-invoke service (`core/v1/invoke.proto`); the core resolves capability→provider via the registry, enforces C2/C3/C5/C7/C8 + stamps C1 trace, and proxies. The calling plugin still orchestrates the *sequence* (core is a switchboard, not an orchestrator). Bulk Arrow bytes still bypass the core (the data-plane exception is preserved).

**Why not direct-dial:** scoped-token direct-dial distributes enforcement to every callee → re-introduces the honor-system the security review condemned (the first plugin that validates loosely or skips audit silently breaks a platform invariant, with nothing central to catch it). Latency — the only thing direct-dial wins — is the dimension a control plane cares least about, and bytes already bypass the core. A direct-dial fast-path stays available as a future superseding ADR *if* profiling proves a need.

**Freeze impact:** the freeze artifact is the new `core/v1/invoke.proto` (now freeze-blocker item #6 in current.md); `RequestContext` does NOT gain provider-routing fields. reviews/06 C-6 updated open → resolved.

**Files:** `docs/architecture/adrs/005-capability-invocation-model.md` (new), `docs/architecture/adrs/README.md`, `reviews/06-proto-contract-review.md`, `roadmap/*`.

---

## 2026-05-30 — Proto contract review (adversarial agent-team) → reviews/06

**What:** Ran a 4-expert agent-team peer review of the 20 sub-phase-0b proto files +
`schema/plugin.v1.json`, pre-freeze (per ADR-003). Lenses: api-designer (proto/gRPC),
plugin-author (implementability), security-eng (wire-vs-comment enforcement),
systems-architect (composition/failure). Reviewers worked cold (not given the prior
architecture reviews' answers), cross-challenged each other, and classified every finding on
**severity × freeze-gate**. Output: [`reviews/06-proto-contract-review.md`](../reviews/06-proto-contract-review.md).

**Headline:** the protos are clean as individual services, but the cross-plugin properties that
are the RAT thesis (call-by-capability invocation, per-plugin/tenant isolation, tamper-evident
audit) are asserted in comments but **not enforced by the fields** — comment-deep. **Contract
is NOT ready to freeze** — **15 freeze-blockers + 1 open design decision (AUTH-2 invocation
model)**; ~28 further findings are GA-deferrable.

**15 freeze-blockers (cannot fix post-freeze)** — top: the identity keystone (forgeable +
contradictorily-defined `subject` → C3 unbuildable); format capability URI naming breaks the
triple; state key grammar + `state.Put` outcome tri-state + CAS-linearizability-conformance (+
DynamoDB eventual-consistency → split-brain leader election, a NEW critical); audit
AppendResponse shape; async event-bus envelope (no `event.proto`); `MergeBranch`
idempotency/expected-snapshot; `secret.Resolve.found` semantics; Arrow protocol+role; split
`Write` per-mode; `rat.capability` annotation; `Ingest` streaming shape; timestamp type;
`slots.target` wrap.

**Method notes:** keystone hit independently by 3/4 lenses; the sharpest find (confused-deputy
assertion-replay → per-hop `correlation_id` enforcement) only emerged from the team's converged
fix; one finding conceded down (API-8), one reviewer self-discarded 4 unverified findings.

**⚠️ Correction (committed `0201892`, after first version `b9be88b`):** systems-architect's
ballot was lost in transit (tool acked, message never landed). The first report version was
written without it and **wrongly recorded AUTH-2 as direct-dial-by-consensus** plus three items
as GA. When the ballot arrived, the report was corrected — all changes toward *more* severe:
AUTH-2 is now a documented **open disagreement** (systems-architect: core-mediated /
plugin-author: direct-dial; needs an ADR), and `state.Put` tri-state, the async event envelope,
and `MergeBranch` request-shape were upgraded to freeze-blockers (12 → 15). Provenance noted in
the report appendix.

**Next:** resolve the AUTH-2 model (ADR) + apply the 15 freeze-blocker fixes (start with the
`context.proto` keystone — everything keys off it), re-running buf each step.

**Files:** `reviews/06-proto-contract-review.md` (commits `b9be88b` + correction `0201892`), `roadmap/*`.

---

## 2026-05-30 — Agent-teams flag pinned into project settings

Declared `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` in the project-committed `.claude/settings.json` `env` block so the repo self-documents its reliance on the experimental agent-teams feature (previously set only in user-global `~/.claude/settings.json`). Flag is experimental/unofficial — may change on product update. Doc: `https://code.claude.com/docs/en/settings.md` (`env` block pattern).

**Files:** `.claude/settings.json`, `roadmap/done.md`.

---

## 2026-05-30 — PostToolUse auto-format hook: evaluated and rejected

**What:** Evaluated the deferred `PostToolUse` auto-format hook from `backlog.md` (the
trigger condition — 20 `.proto` files committed — was met). Decision: **do not land the
hook; adopt manual-batch formatting instead.**

**Decision rationale:** Containerized formatter in a synchronous `PostToolUse` hook is a
latency antipattern for this project. Each `Edit|Write` call would spin up a Podman
container (500ms–2s startup) even though `gofmt` itself runs in <50ms and `buf format`
in <200ms. The container overhead is 10–40x the tool cost, paid after every single file
edit, blocking Claude from proceeding to the next tool call. At this project's
development pace (heavy multi-file sessions), that latency accumulates into real friction.

**Alternative landed:** none needed in `.claude/`. The `permissions.allow` array already
permits `buf format *` and `go fmt *` via Bash tool. The model is expected to run a
batch format (`buf format -w` via the established Podman invocation) before committing,
consistent with how every other containerized tool is used in this project. A
`scripts/fmt.sh` helper can be added when Go code is actively being written — a Bash
script, not a hook.

**Doc citation:** `https://code.claude.com/docs/en/hooks-guide.md` — PostToolUse +
`Edit|Write` matcher pattern confirmed; latency tradeoff is Claude Code engineer judgment,
not a doc constraint.

**Files:** `roadmap/backlog.md` (hook row cut, decision noted), `roadmap/done.md` (this
entry).

---

## 2026-05-30 — Phase 0 sub-phase 0b complete: long tail (experience + billing)

**What:** Drafted the final four axes — all v1 axes now have a proto.

**New protos (`contracts/proto/rat/`):**
- `ui/v1` — Describe/RenderSlot. Deliberately thin (a UI mostly consumes the API
  gateway); the contract is about exposing surfaces + hosting `contributes.slots`
  portal contributions (overview.md contract triple).
- `notifications/v1` — Send (severity + target + attributes; secrets-redaction
  obligation noted, I13).
- `marketplace/v1` — Search/Get. **reviews/02 N2**: provided/required capabilities,
  conformance results, and signature are MANDATORY listing fields so any
  marketplace can answer the "works on MY deployment?" capability-fit question —
  the one hard job of a marketplace on a pluggable-everything platform.
- `billing/v1` — Record (per-tenant metering by construction via context.tenant, C7).

**Verified (containerized):** `buf lint` 0 findings (exit 0), `buf build` 0
errors, `buf generate` **38 Go files**, dup-scan clean.

**Milestone:** **sub-phase 0b axis protos COMPLETE — 20 proto files total** (18
axis services + 2 common). Every v1 plugin axis from ADR-001 now has a wire
contract. Critical concerns with a wire home: C1, C2, C3, C5, C7 + I8/I9/I13.

**What 0b still needs before it's fully done:** per-kind manifest schemas derived
from these protos (the 0a→0b handoff in `schema/README.md`). Then 0c: the
event-bus envelope (C1 trace in async events, not just RPCs).

**Files:** `contracts/proto/rat/{ui,notifications,marketplace,billing}/v1/*.proto`,
`contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b cont'd (batch 3): 5 control-plane axes

**What:** Added the remaining bootstrap/ops control-plane axes. Data plane was
already complete; this finishes most of the control plane.

**New protos (`contracts/proto/rat/`):**
- `deploymentruntime/v1` — Launch/Terminate/Healthcheck; **tier-0** (where plugins
  run); **I9 minimum isolation profile** is wire-level contract (non-root,
  cap_drop ALL, no-new-privileges, read-only FS, blocked metadata egress) — the
  trust boundary the "install many 3rd-party plugins" bet leans on.
- `scheduler/v1` — Schedule/Cancel/WatchDue (a clock, not an orchestrator).
- `secret/v1` — Resolve; **I13 secret contract**: refs in manifests/events/logs,
  values resolved on demand with TTL, never persisted, redaction a core duty.
- `observability/v1` — Ingest (client-stream). **Export sink only** — the core's
  own `/metrics` + OTel are NATIVE and do not depend on this plugin (reviews/03
  I1); "observability: none" still leaves the core self-observable.
- `auditlog/v1` — Append; **I8 mandatory audit**: append-only, hash-chained
  (prev_hash) tamper-evident records. Audit ALWAYS emits (core-local fallback);
  this axis decides WHERE the trail goes, never WHETHER it exists.

**Verified (containerized):** `buf lint` 0 findings (exit 0), `buf build` 0
errors, `buf generate` 30 Go files, dup-scan clean. No streaming-name issues this
batch (watched the *Response rule proactively).

**Phase status:** 0b now has **14 of ~20 axis protos** — data plane complete (6),
control plane nearly complete (8: state, identity, tenancy, deployment-runtime,
scheduler, secret, observability, audit-log). Remaining: experience axes (ui,
notifications, marketplace) + billing. Critical concerns now wired: C1, C2, C3,
C5, C7, plus I8/I9/I13 isolation/audit/secret contracts.

**Files:** `contracts/proto/rat/{deploymentruntime,scheduler,secret,observability,auditlog}/v1/*.proto`,
`contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b cont'd: 6 more axis protos + lint fix

**What:** Added six more axis service contracts (rest of the data plane + the
three Critical-carrying control-plane axes) and **corrected a lint failure that
slipped into the prior commit** (`e79910c`).

**New protos (`contracts/proto/rat/`):**
- `engine/v1/engine.proto` — Execute/Query/Preview ⇒ `rat://engine/v1/{execute,query,preview}`.
- `catalog/v1/catalog.proto` — GetTable/CreateBranch/MergeBranch (branch-isolated runs).
- `storage/v1/storage.proto` — VendCredentials; **C7 enforcement point** (creds
  scoped to `context.tenant` + prefix, short-TTL — the mis-scope that reviews/01
  Finding 3 warned defeats tenancy).
- `state/v1/state.proto` — Get/Put/List/Watch; **tier-0** (bootstrap-critical),
  **C3** (per-plugin + per-tenant namespacing, deny-by-default cross-plugin), CAS
  `Put` backs the leader-election lease (ADR-002 D5).
- `identity/v1/identity.proto` — Authenticate/Authorize; **C2** (per-plugin token,
  constant-time compare — inherits v2 ADR-020; default is NOT anonymous-root).
- `tenancy/v1/tenancy.proto` — Decide; **C7** as *structural* (core enforces
  isolation; plugin only computes policy — explicitly rejects the "isolation is
  4 plugins agreeing" reading from reviews/01).

**Lint correction:** renamed streaming response types to satisfy buf STANDARD —
`runtime.ExecuteEvent` → `ExecuteResponse` (this is the finding that was wrongly
reported as passing in `e79910c`), and pre-empted the same on the new
`state.WatchEvent` → `WatchResponse`.

**Verified (containerized):** `buf lint` **0 findings** (genuinely, exit 0 this
time), `buf build` **0 errors**, `buf generate` **20 Go files**, dup-scan clean.

**Phase status:** 0b now has **9 of ~20 axis protos** (format, runtime, strategy,
engine, catalog, storage, state, identity, tenancy) + 2 common protos. Critical
concerns C1/C2/C3/C5/C7 now have a wire home.

**Files:** `contracts/proto/rat/{engine,catalog,storage,state,identity,tenancy}/v1/*.proto`,
`contracts/proto/rat/runtime/v1/runtime.proto` (fix),
`contracts/proto/rat/state/v1/state.proto`, `contracts/README.md`, `roadmap/*`.

---

## 2026-05-30 — Phase 0 sub-phase 0b started: first axis protos + buf toolchain

**What:** Drafted the first three axis service contracts + the cross-cutting
request context, and stood up the `buf` proto toolchain (containerized).

**Protos (`contracts/proto/rat/`):**
- `common/v1/context.proto` — **C1 bake-in**: `RequestContext` (traceparent +
  tracestate + correlation_id mandatory; subject for C2/C5; tenant for C7;
  deadline hint). Travels as field 1 of every RPC. Pulled forward from 0c
  because every axis proto imports it.
- `common/v1/data.proto` — shared data-plane handoff types (`TableRef`,
  `ArrowStream`, `WriteResult`). Encodes the "control plane carries refs, bulk
  bytes go out-of-band as Arrow" invariant from overview.md.
- `format/v1/format.proto` — `Resolve`/`Write`/`Maintain` ⇒
  `rat://format-capability/v1/{scan,merge,append,maintain}`.
- `runtime/v1/runtime.proto` — `Execute` (server-streaming) ⇒
  `rat://runtime/v1/execute`.
- `strategy/v1/strategy.proto` — `Apply` ⇒ `rat://strategy/v1/apply`.

These three axes were chosen first because the example manifests (0a) already
reference their capability URIs — so the manifest↔wire loop now closes.

**Toolchain:** `contracts/buf.yaml` (lint STANDARD, breaking FILE),
`contracts/buf.gen.yaml` (Go SDK wired; other SDKs in 0d/0e),
`contracts/.gitignore` (generated `gen/` excluded as build artifacts).

**Verified (containerized, per container-only rule):** `buf build` and
`buf generate` passed (`docker.io/bufbuild/buf:1.47.2`, run with `--userns=keep-id`
+ writable HOME). `buf generate` produced 8 Go files (git-ignored artifacts).

**⚠️ Correction (recorded in the next entry's commit):** this commit was
described at the time as "buf lint 0 findings" — that was WRONG. `runtime.proto`
still returned `stream ExecuteEvent`, which buf STANDARD flags (response type must
be `*Response`-named). Lint was actually FAILING (1 finding) at the time of
`e79910c`; build/generate passed (lint findings don't block them) and that was
misread as lint passing. Fixed in the following commit (`ExecuteEvent` →
`ExecuteResponse`).

**Note:** several Write calls glitched mid-session (duplicated lines / wrong
paths); caught via dup-scan + buf, all files rewritten clean and re-verified.

**Files:** `contracts/proto/**` (5 protos), `contracts/buf.yaml`,
`contracts/buf.gen.yaml`, `contracts/.gitignore`, `contracts/README.md`,
`roadmap/*`.

---

## 2026-05-30 — Phase 0 entered: sub-phase 0a manifest schema drafted

**What:** Entered Phase 0 (Lock the contracts) and produced the first contract artifact — the manifest envelope schema. Created the `contracts/` workspace.

**Artifacts (all in `contracts/`):**
- `schema/plugin.v1.json` — manifest **envelope** schema, JSON Schema 2020-12 (per ADR-002 D3). Validates the structure common to every axis: `api_version`/`kind`/`metadata`/`provides`/`requires`/`suggests`/`contributes`/`metadata_schema`, plus the capability-URI grammar `rat://<axis>/v<major>/<capability>`.
- `schema/README.md` — design notes; records the **per-kind schema decision** (envelope-first now, per-kind schemas layered in 0b as each axis proto lands) and the documented gaps (semantic capability validity needs `rat plugin validate`, 0f).
- `examples/rat-strategy-scd2.plugin.yaml` — canonical valid manifest (from overview.md, extended with Critical fields).
- `examples/rat-format-deltalake.plugin.yaml` — second valid manifest (signed/team+ trust block).
- `examples/INVALID-examples.md` — negative test vectors (future 0f corpus).
- `README.md` — contracts workspace entry point + status table.

**Critical concerns baked in (synthesis):** C4 resource asks/limits (`resources`, **mandatory**), C5 capability enforcement (`provides` is the enforced declaration, minItems 1), C8 supply-chain trust (`trust` block, optional@solo / required@team+).

**Verified:** ran a containerized validator (Podman, `python:3.12-slim` + `jsonschema`) — schema is meta-valid, both examples pass, all 4 negative vectors correctly rejected. ALL GREEN.

**Phase status:** Phase 0 moved not-started → in-flight; sub-phase 0a substantially drafted (schema + examples done; per-kind schemas deferred to 0b).

**Note on the commitment gate:** `phases.md` flags a 12–18mo runway + GTM commitment as a pre-Phase-0 gate. Tom chose to proceed (sandbox/exploratory). Gate noted, not formally cleared.

**Files:** `contracts/` (new tree, 6 files), `roadmap/current.md`, `roadmap/phases.md`, `roadmap/done.md`.

---

## 2026-05-30 — Core language locked: Go (ADR-004)

Wrote [ADR-004](../docs/architecture/adrs/004-core-language-go.md) to **ratify and lock** the Go decision that [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D1 had already made. The decision itself wasn't new — D1 said "Core language: Go" all along — but the project *prose* (CLAUDE.md "Rust or Go") and the just-landed allowlist (both toolchains) were still treating it as open. ADR-004 closes that gap before code starts.

### Why an ADR if D1 already said Go
The gap between "decided in the ADR" and "treated as open in prose/tooling" is exactly the kind of drift the ADR-first discipline exists to catch. ADR-004 also records something D1 left implicit: Go is the **committed default**, with the door to re-language kept open as a Phase-0 validation checkpoint per D1's own re-language meta-principle (not via a prototype spike — ADR-002 specifies none).

### Changes
- **`docs/architecture/adrs/004-core-language-go.md`** — new ADR (Accepted).
- **`docs/architecture/adrs/002-founding-tech-stack.md`** — D1 cell annotated "Locked & ratified by ADR-004" (cross-link only; decision unchanged).
- **`docs/architecture/adrs/README.md`** — index row for ADR-004.
- **`.claude/settings.json`** — **trimmed to the Go toolchain**: dropped the 7 Cargo/Rust allow-rules (`cargo build/test/check/clippy/run/fmt/doc`) added in the prior entry. The "both toolchains until locked" rationale no longer holds. 26 → 19 rules.
- **`roadmap/current.md`** — updated.

### Rationale source
Grounded in ADR-002 D1: ecosystem alignment (etcd/NATS/K8s/Temporal/Crossplane all Go), mature gRPC tooling (`grpc-go`), faster MVP, larger cloud-native contributor pool, plus plugin-SDK ergonomics (no borrow-checker barrier for plugin authors — the ADR-001 bet). Contracts remain language-neutral, so third-party plugins stay any-language.

---

## 2026-05-30 — `.claude/settings.json` coding-phase allowlist

> **Superseded in part (same day):** the Cargo/Rust rules added here were removed once the language locked to Go — see the ADR-004 entry above.


Pre-coding permissions audit (via `claude-engineer` agent). Expanded the `permissions.allow` array to cover both candidate toolchains — the Go vs Rust language decision from ADR-002 is still open ("Both / undecided"), so both are pre-allowed so Phase 0 can start in either direction without permission-prompt friction.

### Changes

- **`.claude/settings.json`** — added `$schema` key (IDE autocomplete); populated `permissions.allow` with 26 command rules:
  - **Go:** `go build/test/vet/mod/generate/run/fmt`
  - **Rust/Cargo:** `cargo build/test/check/clippy/run/fmt/doc`
  - **Protobuf (buf):** `buf generate/lint/breaking/format`
  - **Podman:** `podman build/run/compose` (per Tom's container-only rule in root CLAUDE.md)
  - **Git (safe):** `git commit/add/diff/log/status`

### Deliberately omitted (keep prompting)

`git push`, `git reset`, `git rebase`, `podman rm`, `podman rmi` — destructive or remote-affecting; prompt behavior preserved.

### Two deferred items queued in backlog

(See `backlog.md` — "Claude Code config: deferred until first code file" section.)

### Rationale

`go test ./...`, `cargo clippy`, `buf generate` etc. are not in Claude Code's built-in read-only set and would prompt on every invocation. Listing them in `permissions.allow` removes the friction without relaxing any destructive-command guardrails. Cite: `https://code.claude.com/docs/en/permissions.md`.

---

## 2026-05-30 — `.claude/` configuration landed

Project-specific Claude Code setup created. Same minimalist discipline as the architecture: built-ins first, custom additions justified, docs cited.

### Files added
- `.claude/README.md` — entry point + structure
- `.claude/settings.json` — `permissions.allow` empty (honest: every common command in transcripts was either auto-allowed or mutating; nothing meaningful to allowlist)
- `.claude/rules/plugin-architecture.md` — founding constitutional invariant from ADR-001 (always-load, no `paths:` frontmatter). Codifies the 6-thing core + 16+ axes; the "tier 0" callout from synthesis Finding 6; cross-cutting concerns owned by the core.
- `.claude/rules/claude-environment.md` — meta-discipline for `.claude/` itself. Built-in first, cite official docs, minimal surface, quarterly audit. Names the built-in agents + skills explicitly so future sessions can't drift.
- `.claude/agents/claude-engineer.md` — specialized custom agent (model: sonnet; tools: Read/Edit/Write/Bash/WebFetch/Grep/Glob) for Claude Code config work. Reads `https://code.claude.com/docs/` before recommending changes; distinct from built-in `claude-code-guide` (research-only) — `claude-engineer` can make changes.

### Files updated
- Root `CLAUDE.md` — new principle #10 "Maintain the Claude Code environment as architecture"; directory map extended with `.claude/` tree
- `.gitignore` — excludes `.claude/settings.local.json` (per-user overrides not committed)

### What was NOT added
- ❌ Hooks — the synthesis warned against premature automation; CLAUDE.md rules are enough for now
- ❌ Custom skills — built-in skills (deep-research, code-review, etc.) cover the needs
- ❌ MCP servers — nothing project-specific yet
- ❌ Other custom agents — built-ins (claude, Explore, Plan, general-purpose, claude-code-guide) cover everything except Claude-Code-config-itself, which is what claude-engineer is for

### Rationale
Tom asked for the setup as part of "before anything of this could you tell me the claude setup for this new sandbox." The audit surfaced that the project had only CLAUDE.md rules — no agents, hooks, settings. Rather than adding a wide surface, we mirrored the architecture's discipline: a minimal `.claude/` core (rules + one custom agent) that future additions must justify against built-ins.

The `claude-engineer` agent is the operational equivalent of ADR-003's "two reference implementations before contract freeze" rule — it forces every Claude Code config change to go through a docs-citing, built-in-first filter, instead of accumulating ad-hoc custom additions over time.

---

## 2026-05-30 — Phase −1 complete

The full architectural-design + adversarial-review phase, landed in one day with Claude.

### Roadmap structure + ADR-003 + post-synthesis doc updates
- Created `roadmap/` directory with CLAUDE.md (maintenance rules), README, phases.md, current.md, done.md, backlog.md
- Wrote **ADR-003** — "Two independent reference implementations before any contract freezes" (the C9 forcing function from synthesis)
- Updated **ADR-001 Migration section** — Phase 0 timeline shifted to 4-6 months; Critical cross-cutting concerns baked in; all 5 phases expanded with post-synthesis scope
- Updated **vision.md** — added "Anti-goals" section: (1) no new plugin axis in year 1 until 100 daily users on core, (2) effort ratio must invert from 95/5 architecture-to-GTM toward 60/40
- Updated **ADRs index** with ADR-003
- Updated **root CLAUDE.md** with roadmap reference + maintenance rule

### 5-perspective adversarial team review
- Spawned `rat-v3-review` team with 5 teammates (adversarial-architect, plugin-ecosystem-builder, operations-sre, security-reviewer, product-gtm) running in parallel via the experimental agent-teams feature
- Each produced a deep critique against the founding ADRs
- Wrote **synthesis** (`reviews/00-synthesis.md`) — 5 cross-cutting themes converged across all 5 reviewers, 10 Critical findings, 17 Important findings, 26 prospective ADRs, 2 roadmap shifts
- Headline finding: *the architecture is sound; the cross-cutting concerns that genuinely have to span plugins (trust, conformance, observability, distribution) have no owner; the proposed mitigations for the documented failure modes don't escape them*
- 5 review files: `01-adversarial-architect.md`, `02-plugin-ecosystem-builder.md`, `03-operations-sre.md`, `04-security-reviewer.md`, `05-product-gtm.md`
- Team gracefully shut down post-synthesis

### Founding ADRs landed
- **ADR-001** — "Everything is a plugin" (the founding principle: 6-thing core + 16+ plugin axes)
- **ADR-002** — "Founding tech stack + strategy decisions" (Go + NATS + JSON Schema + Apache 2.0 + K8s patterns + 7 other decisions across 10 questions; meta-principle: AI-rewrite escape hatch lowers tech-choice stakes)
- Both ADRs include rejected-alternatives tables, structured Consequences sections, and links to the conversations that produced them
- Conversation distillations committed: `docs/conversations/2026-05-30-the-vision-conversation.md` + `docs/conversations/2026-05-30-tech-stack-decisions.md`

### Initial scaffold
- Created `~/sandbox/rat/` project directory + git init
- Project CLAUDE.md with working principles (contracts before code, six-thing-core discipline, ADR-first, honest tradeoffs, capture-ideas-where-they're-born, save load-bearing conversations)
- README + .gitignore
- Vision document (the thesis) — minimal core + everything pluggable
- Architecture overview document — the integrated picture
- ADRs README with template + numbering/discipline rules
- Sub-CLAUDE.md files for `docs/architecture/adrs/`, `ideas/`, `docs/conversations/`
- ideas/inbox.md with 6 seed entries (later expanded to 11)
- research/prior-art/README.md (K8s, OSGi, VSCode, NATS, Temporal, etc.)
- research/competitors.md (Snowflake, Databricks, dbt, Airflow, Iceberg, MotherDuck — the landscape)
- 14 files, ~1276 lines, 1 commit (`7d57eab`)

### Git commits this session

```
536c9c1 docs(review): synthesis + remaining 2 reviews — 5-perspective adversarial audit
4d2af82 docs(review): security threat model (STRIDE) for RAT v3
778e79d docs(review): 3rd-party plugin author ecosystem review
dbdcde5 docs(review): adversarial architect review
33c1ec0 docs(adr): lock founding tech stack — Go, NATS, Apache 2.0, K8s patterns (ADR-002)
7d57eab chore: initial scaffold for RAT v3
```

(This entry's own commit lands separately — see git log for `docs(roadmap+adr): ...`.)

### What's true at end of day 2026-05-30

- Project lives at `~/sandbox/rat/`, git-initialized, ~3000 lines of architecture + thinking
- 3 Accepted ADRs (001, 002, 003)
- 2 conversation distillations
- 5 adversarial reviews + 1 synthesis
- 11 ideas captured in `ideas/inbox.md`
- Research scaffold present (prior art + competitors)
- Roadmap structure operational; this file is the proof
- **No code yet.** Awaiting Tom's commitment decision before Phase 0 starts.

## 2026-06-04 — state/v1 `Delete` ([ADR-035](../docs/architecture/adrs/035-state-axis-delete.md)) — RatFS delete/rename unblocked

A VS Code filesystem-op audit of RatFS found delete-file, delete-folder, rename and move all blocked by **one gap: the state axis had no `Delete`** (a KV store with Get/Put/List/Watch but no delete). Fixed the contract + the full chain:
- **[ADR-035](../docs/architecture/adrs/035-state-axis-delete.md)** (Accepted) + `rat://state/v1/delete` added to `state.proto` — additive (`buf breaking` clean), idempotent, CAS-aware, **optional per backend** (UNIMPLEMENTED allowed). SDKs regenerated; `rat` rebuilt (gateway routes it).
- **code-fs** implements `Delete` (S3 `RemoveObject`) + declares `provides: state/v1/delete`; **s3-storage** declares `requires: state/v1/delete` (it prunes its connection registry) so it's a valid C5 caller. Both repacked; plugin-base-go rebuilt with the fresh SDK.
- **RatFS** (vscode-rat v0.5.0): `delete` (file / recursive dir) + `rename`/move (copy + delete). Also fixed in the audit: `createDirectory` (empty folders via a `.ratkeep` marker) + `writeFile` create/overwrite flags.
- **Proven live** through the secure hub: put→delete→`found:true`, idempotent re-delete, markers cleaned, 0 leftovers, C5-gated.
