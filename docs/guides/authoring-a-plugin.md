# Authoring a plugin — from zero to a packed, verified image

This is the single linear walkthrough for writing a RAT v3 plugin. Everything here exists
and runs today: the `rat plugin` toolkit ([ADR-026](../architecture/adrs/026-plugin-authoring-and-packaging.md)),
the per-axis contract guides, the runtime SDK ([ADR-029](../architecture/adrs/029-plugin-runtime-sdk.md)),
and the conformance suite. By the end you have a **verified OCI image** — one proven to
launch under the I9 sandbox and serve every capability it declares — ready for
`rat add` or `rat plugin publish`. Where the tooling has gaps, the
[Current limitations](#current-limitations-honest) section says so plainly.

## The 10-minute path

1. **Pick your axis.** A plugin's `kind` is one of the 18 frozen axes:
   `engine` `runtime` `format` `strategy` `catalog` `storage` `deployment-runtime`
   `state-backend` `secret-backend` `scheduler-backend` `identity` `tenancy` `billing`
   `observability` `audit-log` `ui` `notifications` `marketplace`.
2. **Read the axis contract.** Every axis ships an author guide at
   `contracts/proto/rat/<axis>/v1/CONTRACT.md` — capability table, RPC semantics,
   conformance obligations, reference implementations. E.g.
   [`state/v1/CONTRACT.md`](../../contracts/proto/rat/state/v1/CONTRACT.md).
   Note the kind → axis naming: `state-backend` → `state`, `secret-backend` → `secret`,
   `scheduler-backend` → `scheduler`, `audit-log` → `auditlog`,
   `deployment-runtime` → `deploymentruntime`; every other kind is its own axis name.
3. **Scaffold.**
   ```sh
   rat plugin init my-state --kind state-backend --lang python   # also: go | typescript | rust
   cd my-state
   ```
   You get a folder that passes `check` on the first run: `manifest.yaml` (provides
   pre-filled for the kind, default `resources`), a kind-aware server stub, a
   `Dockerfile`, a `README.md`, and portable CI/CD (`ci.sh` +
   `.github/workflows/plugin.yml` — thin wrappers over the same `rat plugin` verbs).
   `--dir <path>` overrides the target directory.
4. **Implement.** The TODOs are in the server stub: Python scaffolds a real servicer
   class with one `raise NotImplementedError` method per capability (`server.py`); Go
   scaffolds a `ratplugin.Serve` closure with a `// TODO: register your servicer`
   (`main.go`). Fill in the methods against the CONTRACT.md semantics.
5. **`rat plugin check`** — the instant static gate. Validates the manifest (kind is a
   real axis, `metadata.version` present, `resources.requests` present), that every
   capability URI names something real in the axes this `rat` links, that `provides`
   stays on your own axis, and any contributed CLI commands. Run it after every
   manifest edit; it costs nothing.
6. **Unit-test locally** — your language's test runner against your servicer directly,
   no container. See [Iterating fast](#iterating-fast).
7. **`rat plugin test`** — the integration gate. Builds the image (podman), launches it
   under the real I9 profile (non-root, cap-drop ALL, read-only rootfs), waits for
   healthy, then smoke-invokes every capability in `provides` — `Unimplemented` fails
   the gate. `--image <ref>` tests an already-built image instead.
8. **`rat plugin pack`** — the artifact. Re-runs the gate and builds the **verified
   image** with your validated manifest stamped in as an OCI label
   (`dev.rat.manifest.v1.b64`), default tag `localhost/rat/<name>:<version>`.
   `rat add <ref>` reads the manifest straight from the image — no side-channel file.
9. **(Optional) `rat plugin publish --image localhost/rat/<name>:<version> --registry ghcr.io/<you>`**
   — re-verifies the image (never ship a broken plugin), then pushes
   `<registry>/<name>:<version>`. A local `registry:2` (`--registry localhost:5000`)
   works the same; `--latest` also pushes `:latest`.

## Discovering capabilities

Capability URIs (`rat://<axis>/v1/<verb>`) are the only coupling between plugins. The
fastest way to see them is the CLI — it renders the registry compiled into the binary
(the very annotations `rat plugin check` and the gateway enforce, so it cannot drift):

```sh
rat capabilities                  # everything, grouped by axis
rat capabilities state-backend    # one kind (or its axis: `rat capabilities state`)
```

Two slower places, always in agreement with it:

- **The CONTRACT.md table.** Each axis guide opens with a capability table — URI,
  RPC method, cardinality, semantics. This is the readable form.
- **The proto annotations.** Each RPC in `contracts/proto/rat/<axis>/v1/<axis>.proto`
  carries the authoritative annotation:
  ```proto
  rpc Get(GetRequest) returns (GetResponse) {
    option (rat.common.v1.capability) = "rat://state/v1/get";
  }
  ```
  `rat plugin check` resolves your declared URIs against these annotations — a typo'd
  capability is a hard failure, not a silent no-op.

Two shape rules:

- **Same-axis `provides`.** A `kind: state-backend` plugin may only provide
  `rat://state/...` capabilities. `requires` is legitimately cross-axis — that is
  capability composition (a format plugin requiring `rat://storage/v1/vend-credentials`).
- **The driver shape** ([ADR-039](../architecture/adrs/039-driver-plugins-and-the-authoring-gate.md)).
  A plugin with `provides: []` that only `requires` capabilities is a first-class
  *driver* — a scheduler firing pipelines, a BFF, an operator. The authoring floor is:
  **declare at least one of `provides` or `requires`**. A manifest with neither is
  rejected — a plugin must do something.

## The manifest

The minimal valid manifest (what the scaffold emits, plus labels):

```yaml
api_version: rat/1
kind: state-backend
metadata:
  name: my-state
  version: 0.1.0
  labels: { durability: disk }      # optional — see selection below
compatible_core: ["rat/1"]
provides:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/put
  - capability: rat://state/v1/list
resources:                          # MANDATORY (C4) — check/pack reject without it
  requests: { cpu: "50m", memory: "64Mi" }
  limits: { cpu: "500m", memory: "256Mi" }
```

Validated against the frozen envelope
[`contracts/schema/plugin.v1.json`](../../contracts/schema/plugin.v1.json) plus the
per-kind layer in [`contracts/schema/kinds/`](../../contracts/schema/kinds/) (a
*provider* of an axis must provide its minimal mandatory core; drivers are validated by
the envelope + the authoring gate only). Required: `api_version`, `kind`,
`metadata.name` + `metadata.version`, `provides` (may be empty for a driver), and
`resources` — `rat plugin check` enforces the load-bearing constraints;
`make validate-manifests` runs the exhaustive JSON-Schema pass.

Two optional fields worth knowing on day one:

- **`metadata.labels`** ([ADR-045](../architecture/adrs/045-provider-selection.md)) —
  open `key: value` self-description (`{compute: big, gpu: "true"}`). When several
  plugins provide the same capability, planes and calls choose a provider by matching a
  *selector* against these labels — never by plugin name. Ship honest descriptive
  labels and your plugin is selectable with zero operator config.
- **`contributes.commands`** ([ADR-041](../architecture/adrs/041-pluggable-cli-command-contributions.md)) —
  declare CLI commands the `rat` client surfaces as first-class verbs:
  ```yaml
  contributes:
    commands:
      - name: "branch create"
        capability: rat://catalog/v1/create-branch
        help: "Create a data branch off main"
        args:
          - { name: name, field: branch, positional: true, required: true }
  ```
  `rat plugin check` verifies the capability is real, each `field` exists on the
  request message, and the name doesn't shadow a built-in verb.

## Using the SDK

The `ratplugin` runtime SDK ([ADR-029](../architecture/adrs/029-plugin-runtime-sdk.md))
kills the two chunks of boilerplate every plugin repeats — serving and consuming —
and gets the cross-cutting envelope (identity + trace, ADR-007) right once.

**Go** (`github.com/squat-collective/rat-v3/contracts/sdks/go/ratplugin`, in [`contracts/sdks/go/ratplugin/`](../../contracts/sdks/go/ratplugin/plugin.go)):

```go
ratplugin.Serve(func(s grpc.ServiceRegistrar) {        // RAT_PLUGIN_ADDR + graceful SIGTERM drain
    statev1.RegisterStateServiceServer(s, &myStore{})  // one or many servicers
})

gw := ratplugin.Gateway()                              // dials RAT_GATEWAY once
var resp secretv1.ResolveResponse
gw.Call(ctx, "rat://secret/v1/resolve",                // stamps rat-callmeta-bin for you
    &secretv1.ResolveRequest{SecretRef: "ref://state/dsn"}, &resp)

tenant := ratplugin.CallerTenant(ctx)                  // C7 tenant-scoping, from incoming metadata
cfg := ratplugin.EnvMap("RAT_SECRETS")                 // the "k=v,k=v" env convention
```

**Python** (`rat.plugin`, in [`contracts/sdks/python/rat/plugin.py`](../../contracts/sdks/python/rat/plugin.py)):

```python
from rat import plugin

plugin.serve(register)                                  # register(server) adds your servicer(s)
gw = plugin.Gateway()
resp = gw.call("rat://secret/v1/resolve", req, secret_pb2.ResolveResponse)
tenant = plugin.caller_tenant(context)                  # inside a servicer method
cfg = plugin.env_map("RAT_SECRETS")
```

The Python `call` also forwards the C2 plugin token (`RAT_PLUGIN_TOKEN`) and a provider
selector — per-call `select="compute=big"` or the operator-set `RAT_SELECT` default
(ADR-045). UI plugins additionally get `rat.contrib.contribute_ui()` for publishing UI
component contributions. The SDK is a convenience, never a requirement: a plain gRPC
server against the generated stubs is always a valid plugin.

**Where the SDK comes from.** Three ways (ADR-051):

- **Go, as a module:** `go get github.com/squat-collective/rat-v3/contracts/sdks/go@latest`
  — the committed SDK is fetchable directly (the `ratplugin` runtime SDK rides in the
  same module; the path is long because Go requires a nested module's path to match its
  subdirectory).
- **Either language, via the published base images:** every release pushes
  `ghcr.io/squat-collective/rat-v3-plugin-base-{go,py}` — `FROM` those with no clone of this repo.
- **From a clone:** `make plugin-base-py` / `make plugin-base-go` build the same bases
  locally (`localhost/rat/plugin-base-{go,py}:dev`).

The scaffolded Dockerfiles `FROM` these. Go plugins compile the SDK in and ship a
~15 MB static binary on `scratch`; Python plugins ship their own code on the base.
TypeScript and Rust scaffolds serve raw gRPC (`@grpc/grpc-js` / `tonic`) — no
`ratplugin` for those languages yet.

## Conformance & golden vectors

Each axis has language-neutral golden vectors at
`contracts/conformance/<axis>-v1.json` — the single source of truth for "what a
conformant plugin of this axis does" ([ADR-003](../architecture/adrs/003-two-references-before-contract-freeze.md)).

**Who must pass them.** Reference implementations (the ones under
[`plugins/`](../../plugins/)) must — that's the freeze gate. For your own plugin they
are the bar for being *trusted with real duties*: e.g. a `state-backend` is only
eligible as the reconciler's lease backend if it passes the linearizable-CAS and
key-grammar vectors. Your CONTRACT.md lists the axis's specific obligations.

**How they run.** Each reference carries a small hand-written harness
(`harness_test.py` / `harness_test.go`) that boots its own service in-process on a
random port, loads the shared JSON, and drives the steps over real gRPC. Start from
**the canonical template**,
[`contracts/conformance/harness_template.py`](../../contracts/conformance/harness_template.py)
— it uses the `rat.vectors` SDK helpers (`load` · `serve_inprocess` · `run_expect`,
which **hard-fails on an expect key your harness doesn't handle** instead of silently
skipping it). For a filled-in real example of the same shape, see
[`plugins/state/sqlite-py/harness_test.py`](../../plugins/state/sqlite-py/harness_test.py).
The vectors themselves are gated by `make validate-vectors` (an envelope schema + a
per-file key registry in `contracts/schema/conformance-vector.v1.json`) — extending a
vector with a new key means registering it there in the same change.

```sh
make conformance      # every reference, containerized, one pass/fail matrix
                      # (auto-discovers plugins/<axis>/<impl>/ carrying a harness)

# one implementation directly (what the runner does inside its container):
PYTHONPATH=$PWD/contracts/sdks/python python plugins/state/sqlite-py/harness_test.py
```

Failures print the tail of the failing harness's output, so you see the assertion, not
just `FAIL`. Note the split honestly: `rat plugin test` proves *launches + serves*;
the golden vectors prove *behaves correctly* — today the vectors run through the
harness, not through `rat plugin test` (ADR-026 Q03 tracks unifying them).

## Iterating fast

Three loops, fastest first — spend your time in the first one:

1. **In-language unit tests (sub-second).** Test your servicer directly — the harness
   `Rig` pattern (in-process gRPC server on `127.0.0.1:0`) needs no container and no
   running core. This is where the actual logic gets right.
2. **`rat plugin check` (instant).** Static; run it on every manifest touch. It catches
   typo'd capabilities, axis mismatches, and missing mandatory fields before any build.
3. **`rat plugin test` / `pack` (~30 s+).** Each run is a full podman image build plus
   an I9 launch. Treat it as the integration gate before a commit or publish, not as
   the edit loop. If the image hasn't changed, `--image <ref>` skips the rebuild.

And the loop automates itself: **`rat plugin dev [<dir>]`** watches the directory and
re-runs `check` + `test` on every save (`--check-only` for just the instant static gate;
`--interval` to tune the poll). Failures print and keep watching — fix and save again.

## Current limitations (honest)

- **The Python SDK is not on PyPI** (the one DX-2 residual, ADR-051) — it ships inside
  `ghcr.io/squat-collective/rat-v3-plugin-base-py` instead; pip-only environments must wait for the
  packaging follow-up. (The Go SDK *is* `go get`-able.)
- **`rat plugin test` doesn't run the golden vectors.** It launch+smoke-verifies
  serving; behavioral conformance is the separate harness path (ADR-026 Q03).
- **The conformance harness is still hand-assembled.** No codegen — but there is now one
  canonical template (`contracts/conformance/harness_template.py`) + the `rat.vectors`
  helpers, instead of "copy whichever sibling you found first".
- **`rat plugin dev` polls (1s default), it doesn't fsnotify** — deliberate (no new core
  dependency). Every triggered `test` is still a full image rebuild; the sub-second loop
  remains your language's own unit tests.
- **`ratplugin` is Go + Python only.** TypeScript/Rust scaffolds work but hand-roll the
  serve/consume boilerplate (TS/Rust SDKs are next per ADR-029).

## Related

- [ADR-026](../architecture/adrs/026-plugin-authoring-and-packaging.md) — the `rat plugin` toolkit, verified-image gate, scaffolded CI/CD
- [ADR-029](../architecture/adrs/029-plugin-runtime-sdk.md) — the `ratplugin` runtime SDK (Serve · Call · CallerTenant)
- [ADR-039](../architecture/adrs/039-driver-plugins-and-the-authoring-gate.md) — driver plugins + the authoring validation floor
- [ADR-041](../architecture/adrs/041-pluggable-cli-command-contributions.md) — manifest-contributed CLI commands
- [ADR-045](../architecture/adrs/045-provider-selection.md) — labels + selectors for provider selection
- [`contracts/README.md`](../../contracts/README.md) — the contract triple (manifest + proto + capability URI)
- [`contracts/conformance/README.md`](../../contracts/conformance/README.md) — golden vectors + the suite runner
- `contracts/proto/rat/<axis>/v1/CONTRACT.md` — your axis's author guide ([state](../../contracts/proto/rat/state/v1/CONTRACT.md) is the worked example)
- [`.claude/rules/plugin-architecture.md`](../../.claude/rules/plugin-architecture.md) — the founding invariant every plugin lives under
