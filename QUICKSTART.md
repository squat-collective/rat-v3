# 🐀 RAT in five minutes

A control plane, one plugin, one authorized call, one refused call, and the audit trail
that proves it. Every command below was run against this repo as written.

**Prereqs:** `podman` (or docker) + `make`. Nothing installs on the host — builds and
tests run in containers. (There are no public releases yet — the repo is the
distribution; `scripts/install.sh` activates when it publishes.)

## 0 · Build the CLI (~1 min)

```bash
make rat-build        # containerized go build → dist/rat
./dist/rat --help     # the verb map: PROJECT / DAEMON / AUTHOR / MARKETPLACE / CLIENT
```

## 1 · Build the demo plugin image (~1 min)

```bash
make stateplugin-image    # → rat/stateplugin:dev
```

This is the wire-stub state plugin the core uses in its own launch tests — it speaks
`state/v1` well enough to prove the control plane end-to-end. (Writing a *real* backend
is the first exercise in [docs/guides/authoring-a-plugin.md](docs/guides/authoring-a-plugin.md).)

## 2 · Create a project — a declared plugin set

```bash
mkdir rat-demo && cd rat-demo
/path/to/rat init --runtime podman      # writes rat.toml
```

Two manifests — the entire security model lives in these (C5: *declared = enforced*).
A **provider** declares `provides`; a **caller** (a register-only *driver*, no image)
declares `requires`:

```bash
cat > state.plugin.yaml <<'EOF'
api_version: rat/1
kind: state-backend
metadata: {name: state, version: 0.1.0}
compatible_core: ["rat/1"]
provides:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/put
  - capability: rat://state/v1/list
requires: []
resources:
  requests: {cpu: "50m", memory: "64Mi"}
EOF

cat > dev.plugin.yaml <<'EOF'
api_version: rat/1
kind: state-backend
metadata: {name: dev, version: 0.1.0}
compatible_core: ["rat/1"]
provides: []
requires:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/put
  - capability: rat://state/v1/list
EOF

rat add state --image rat/stateplugin:dev --manifest state.plugin.yaml
rat add dev --manifest dev.plugin.yaml       # driver: no image, just an identity
```

## 3 · Up

```bash
rat up -d     # launches the plugin container (I9 isolation), wires the gateway
rat status    # project — running · socket: .rat/daemon.sock · plugins (2)
```

## 4 · Call through the gateway

```bash
rat call rat://state/v1/get --as dev --data '{"key":"hello"}'
```
```json
{ "found": true, "value": "cGlkPTEga2V5PWhlbGxv" }
```

(The stub echoes `pid=… key=…` as the value — the round-trip is real: client → gateway →
C5 authz → plugin container → back.)

Now the refusal. `state` never declared any `requires`, so as a *caller* it may do
nothing:

```bash
rat call rat://state/v1/put --as state --data '{"key":"x","value":"eA=="}'
# rat: rpc error: code = PermissionDenied
#      desc = C5: caller "state" does not declare `requires` "rat://state/v1/put"
```

No policy file, no RBAC config — the manifests *are* the authorization.

## 5 · The audit trail

Every decision — allowed or denied — is durably logged:

```bash
tail -2 .rat/daemon.log
```
```json
{"kind":"decision","capability":"rat://state/v1/get","caller":"dev","provider":"state","allowed":true,"reason":"dev requires rat://state/v1/get; selected state"}
{"kind":"decision","capability":"rat://state/v1/put","caller":"state","allowed":false,"reason":"C5: caller \"state\" does not declare `requires` \"rat://state/v1/put\""}
```

## 6 · Down

```bash
rat down
```

`.rat/` keeps the daemon socket, the audit log, and `data/<plugin>/` durable mounts
(ADR-031) — it's your project's state, not cruft.

## Where next

| You want… | Go to |
|---|---|
| the full data-platform demo (dbt medallion, UI, scheduler) | [platform/README.md](platform/README.md) — `make platform-up` |
| to write a real plugin | [docs/guides/authoring-a-plugin.md](docs/guides/authoring-a-plugin.md) |
| to compose your own platform | [docs/guides/building-a-platform.md](docs/guides/building-a-platform.md) |
| the why and the architecture | [docs/vision.md](docs/vision.md) → [docs/architecture/overview.md](docs/architecture/overview.md) |
| where the project stands today | [roadmap/current.md](roadmap/current.md) |
