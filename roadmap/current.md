# Current — what's in flight right now

> **Always read this first when opening a Claude session.** Concise by design — the full
> completion history lives in [`done.md`](done.md); the phase map in [`phases.md`](phases.md).
> Updated: 2026-06-10.

## ✅ Latest — `rat/6.18`: PUBLISHED 🐀 (DX-2 — the review is fully closed)

**`github.com/squat-collective/rat-v3`, public, Apache-2.0** ([ADR-051](../docs/architecture/adrs/051-publish-apache2-squat-collective.md)).
Module paths renamed `rat-dev`→`squat-collective/rat-v3` (go-get works; wire proven held — suite +
conformance green; one conscious FILE_SAME_GO_PACKAGE waiver). Both CI workflows repaired
*before* first run (contracts.yml auto-committed to main from CI; release.yml's image tag
contained a `/`), and release.yml now also ships `rat-plugin-base-{go,py}` to GHCR — the
SDK distribution. Verified from the outside: install.sh → `./rat version`, `go get`,
`podman pull`. **External plugin authors are now structurally possible.** Residual: PyPI
packaging (backlog). Details: [done.md](done.md).

## ✅ `rat/6.17`: the DX line is COMPLETE (DX-5…9; only DX-2 remains)

The last engineering batch of the frustration review: **[ADR-050](../docs/architecture/adrs/050-plane-file-env-interpolation.md)**
plane-file `${VAR}` interpolation + `platform/.env` (compose renders verified
byte-identical; undefined var fails loud by name) · **`plugins/secret/vault-py`** — the
production secret-backend, **rotation without restart proven live** against a dev Vault ·
**`rat plugin dev`** — the watch loop (verified live) · ADR-018 Q01 resolved (the protoc
hybrid is the design) · `rat call` takes flags in any order. Found + fixed en route:
`.dockerignore` had silently broken **every plugin-image build** since the ADR-038 move,
and the ADR index had stopped at 038. Gates: verify end-to-end, conformance 32/32, 9 new
tests. Backlog ⑤ is now **DX-2 only** (the publish decision). Details: [done.md](done.md).

## ✅ `rat/6.16`: `rat capabilities` + the vector lint gate (DX-3 + DX-4)

The capability registry is now a verb — `rat capabilities [<axis>|<kind>]` renders what
the binary links (URI · method · cardinality · messages · CONTRACT.md pointer; cannot
drift from enforcement). And the golden vectors can no longer lie: an envelope schema +
per-file key registry (`make validate-vectors`, now in `verify`) catches the typo'd keys
every harness silently skips, with `rat.vectors.run_expect` + `harness_template.py` as
the runtime half. Bonus: **`make gen-check`/`verify` had been silently broken on `main`**
(python codegen output dir + hand-written-SDK false-stales) — found, fixed, and `verify`
passes end-to-end again. Backlog **⑤ DX-3 + DX-4 cut**. Details: [done.md](done.md).

## ✅ `rat/6.15`: `rat validate`, the static preflight (DX-1)

Boot misconfig now surfaces **before** boot: `rat validate [--plane <file>]` statically
checks manifests · unique names · launch-xor-attach · capability URIs are real ·
**every `requires` has a provider** · **every launch image is launchable now** ·
resources declared — and `rat up/serve --strict` refuse to boot on any error (default
behavior unchanged). 7 new tests, core-test green. First real catch: the attach-mode
demo `plane.yaml` declares a secret `requires` it never satisfies (documented in
`platform/README.md`). Backlog **⑤ DX-1 cut**; DX-2…9 remain. Details: [done.md](done.md).

## ✅ `rat/6.14`: the DX sweep (docs-truth + guides + first-success)

A four-journey DX frustration review (author / operator / contract-evolution / onboarding)
found the entry docs systematically understating reality — README claimed "Phase 0+1
sealed, Q02 next"; all 18 `CONTRACT.md` banners said "the core is NOT built yet";
`platform/README.md` mapped deleted dirs — and the documented solo flow broken first-touch
(`rat call` dialed `:7777` while the project daemon listened on `.rat/daemon.sock`).
Fixed in one sweep: entry docs truth-synced; **[QUICKSTART.md](../QUICKSTART.md)**
(verified end-to-end: C5 allow + deny + the audit tail), **CONTRIBUTING.md**,
**[contracts/AMENDING.md](../contracts/AMENDING.md)**, **docs/guides/** (authoring +
platform topologies); `rat call`/`apply` default to the project daemon socket;
`make conformance` failures print their harness output; `make rat-build`; hook stdin-gating
+ allowlist 20→39. Engineering residue queued as backlog **⑤ DX-1…9**. All green:
core-test, conformance 32/32, 0 broken links. Details: [done.md](done.md).

## ✅ `rat/6.13`: the code-level review arc is COMPLETE

A from-scratch code-level review of `core/` + the protos (deliberately ignoring docs/roadmap) found
**7 structural gaps** between the contracts (which describe a security-complete platform) and the spike
core (candid in its comments about what was deferred). All 7 were fixed, sealed on the clean-room
(`clean-room/2.0`), **ported to `main`**, and then a frozen-contract amendment + its full adoption
followed — `main` advanced `rat/6.7` → `rat/6.13`, each step **additive, six-thing core held, no new
dependency, `-race`-green throughout** (incl. `composition` against the real Go providers).

**The 7 hardenings (ADRs 042–048, on `main` at `rat/6.7`):** #2 channel-authenticated plugin identity ·
#1 state-CAS HA lease (+ AV-1 transient-hold) · #4 bounded reconciler RPCs + decoupled status · #3
label/selector provider selection · #6 native `/metrics` + durable audit · #5 hub transparent proxy +
connection pooling · #7 arrow-ticket shared single-use store.

**The create-if-absent line ([ADR-049](../docs/architecture/adrs/049-state-v1-create-if-absent.md)):**
`state/v1` gained an additive, optional, capability-gated `CreateIfAbsent` RPC (`6.8`) → adopted by the
HA lease bootstrap, closing the 043 cold-start race (`6.9`) → fixed an arrow-ticket same-millisecond
replay bug the flaky test exposed (`6.10`) → adopted by the arrow-ticket store (`6.11`) → implemented in
both Python references + a cross-language golden vector (`6.12`) → implemented in `postgres-py` for
production HA, verified against a real Postgres incl. 16-connection concurrency (`6.13`). **Every state
backend on `main` — `inmemory-go`, `inmemory-py`, `sqlite-py`, `postgres-py` — now has it.**

**Nothing from the review remains open.** The only follow-ons are longer-horizon ADR items: mTLS +
`SubjectAssertion` signing (the full identity keystone) · OTel spans + signed/rotated audit · NATS-leaf
cross-machine federation · richer selector operators + plane `select:` ergonomics + load-balanced
replicas · fully-parallel per-plugin reconcile. See [done.md](done.md) for the per-tag log.

## Status one-liner

**Phases 0–9 are SEALED** (`rat/1.5` contracts → `rat/2.0` core → `rat/2.5`–`6.0`). Everything ≤
`rat/2.0` is the **frozen wire**; every tag since is additive. `main` is the sealed line at
**`rat/6.18`**: `rat/6.6` ported the clean-room DX improvements (ADR-039/040/041), `rat/6.7` the 7 core
hardenings (ADRs 042–048), `rat/6.8`–`6.13` the `state/v1` create-if-absent amendment (ADR-049) +
its full adoption, and the `rat/6.14`–`6.18` **DX line**: docs-truth + guides + first-success (6.14),
`rat validate` + `--strict` (6.15), `rat capabilities` + the vector lint gate + the gen-check repair
(6.16), plane `${VAR}` interpolation (ADR-050) + the vault-py secret-backend + `rat plugin dev` (6.17),
and the **publication** — Apache-2.0, `github.com/squat-collective/rat-v3`, GHCR distribution (ADR-051, 6.18). The from-scratch rebuild +
remote-dev-flow experiment the hardenings came from is sealed separately at **`clean-room/2.0`** (a
parallel line, not merged — its `plugins/`+`platform/` wipe would destroy this corpus). **ADR-042's
channel-authenticated identity also closes most of the Phase-10 "direct-gateway `--as` trust" debt
below** (the plugin door now authenticates by token; the wire `--as` is no longer trusted there).

## 🔵 Phase 10 — workspace federation + security (in-flight)

Built on `phase-10` (ADRs [029](../docs/architecture/adrs/029-plugin-runtime-sdk.md)/[031](../docs/architecture/adrs/031-durable-local-storage.md)/[033](../docs/architecture/adrs/033-workspace-federation-hub.md)/[034](../docs/architecture/adrs/034-security-responsibility-model.md)/[035](../docs/architecture/adrs/035-state-axis-delete.md); ADR-[036](../docs/architecture/adrs/036-reconciler-hosts-operators.md) is a Proposed sketch):

- **`rat hub`** — workspace federation, a gateway-of-gateways front door (ADR-033).
- **Identity at the edge** — an `identity-token` plugin (frozen `identity/v1`), hub TLS, and
  a secure-by-default binding guardrail: a public bind refuses without TLS + identity (ADR-034).
- **`ratplugin` runtime SDK** (ADR-029) · **durable `/data` mount** (ADR-031) ·
  **`state/v1 Delete`** (ADR-035, additive wire method) · **RatFS** — `rat://` as a native
  editable VS Code folder over the state axis through the hub.

**Still owed in Phase 10 (largely addressed on `main`):** plugin-to-plugin identity forgery is **closed**
by [ADR-042](../docs/architecture/adrs/042-channel-authenticated-plugin-identity.md) (`rat/6.7`) — the
plugin door authenticates by per-launch token, so the wire `--as` is no longer trusted there. What
remains is the *end-user* principal: `SubjectAssertion` signing + mTLS on the core↔plugin channel (the
ADR-042 follow-on). The `phase-10` integration branch itself was never formally "sealed"; its reusable
outputs reached `main` via the clean-room ports instead (`rat/6.6`–`6.13`).

## 🧹 Active: the professionalization restructure

Reducing the repo to the essential + a professional structure. Plan + audit:
[`docs/restructure/`](../docs/restructure/) (AUDIT.md = keep/remove analysis; TARGET.md =
the locked end-state tree). **Locked decisions:** `examples/`→`plugins/`,
`research/`→`docs/research/`, `reviews/` stays top-level with `reviews/archive/`, and the
**data-dev-plane experiment + 5 exploratory plugins** (incl. `vscode-rat`) **graduate to a
separate `rat-data-dev` repo**.

**Execution (steps 1–4 DONE on `phase-10`; 5 = extraction, paused for sign-off):**
1. ✅ Hygiene — `make clean` (reclaimed ~105M), ADR status sweep (021–026 → Accepted), roadmap refresh.
2. ✅ Cuts — dropped the unused TS + Rust SDKs ([ADR-037](../docs/architecture/adrs/037-trim-committed-sdks-to-consumed-languages.md), 89 files) + the superseded `sql-pipeline-py`/`platform/project/`/`pipelines/` (ADR-021).
3. ✅ Archive — the Q02 simulated-review kit + `reviews/board/` → `reviews/archive/` (12 files; links repointed).
4. ✅ Moves — `research/`→`docs/research/` + `examples/`→`plugins/` ([ADR-038](../docs/architecture/adrs/038-reference-plugins-live-under-plugins.md); 343 files, Go modules rebuilt clean, 0 broken links).
5. ✅ Extracted `rat-data-dev` (`~/sandbox/rat-data-dev`): the experiment + 5 exploratory plugins (incl. `vscode-rat`) + `data-dev-*` scripts. The platform's attach-mode engine/catalog services were vestigial (dbt-runner embeds them) → removed. 47→40 plugins.

**Bonus:** a repo-wide markdown link verifier surfaced + fixed **20 pre-existing broken links**; integrity now clean (0 broken across ~1400 links).

## Immediate next concrete step

**No pressing thread.** The code-level review arc (7 gaps + ADR-049) and the DX sweep are complete on
`main` through `rat/6.14`, all green. Genuinely-open work is optional / longer-horizon — pick by appetite:
- **DX:** the review's queue is **done except DX-2** — the publish/distribution decision
  (push the repo, release binaries, ghcr base images, PyPI SDK). It unblocks external plugin
  authors and is Tom's call, not an engineering task.
- **Security keystone (highest value):** mTLS on the core↔plugin channel + `SubjectAssertion` signing
  (the second half of ADR-042 — the end-user principal is still an unsigned passthrough).
- **Observability:** OTel spans + latency histograms; signed/rotated durable audit (`common/v1.AuditRecord`).
- **Selection v1.5/v2:** richer selector operators (`in`/`!=`/preference) + plane `select:` ergonomics;
  then load-balanced replicas (the reconciler-replica change).
- **Federation:** NATS-leaf cross-machine transport (ADR-033 Q01 / ADR-047 follow-on).
- **Housekeeping:** the `rat-data-dev` repo is local/unpushed — push when ready.

## What's NOT in flight

- **The user-pull gates still bind.** [phases.md](phases.md) **Gate B** (≥10 real solo users)
  before broad new product phases; **Gate C/D** beyond. **Q02** (external *human* peer review)
  is still owed but set aside as impractical for a solo dev — validated practically instead
  (the data-dev-plane experiment, principle #8).
- ~~The freeze stays local/unpushed~~ — **PUBLISHED** at `github.com/squat-collective/rat-v3`
  (Apache-2.0, [ADR-051](../docs/architecture/adrs/051-publish-apache2-squat-collective.md), `rat/6.18`).
  The real Q02 is now simply… public review.

## Branching (in force)

`main` is the sealed line (**`rat/6.18`**, pushed to `github.com/squat-collective/rat-v3`); additive increments
land via `--no-ff` merges of topic branches + an annotated `rat/N.M` tag. **Never commit directly to `main`** (a `PreToolUse` hook blocks
it) — work on a topic branch. Full rules: [`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When a session produces concrete output: update `done.md` → `current.md` → `phases.md` (if a
phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
