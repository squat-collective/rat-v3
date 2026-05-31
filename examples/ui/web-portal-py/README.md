# rat-ui-web-portal-py — the `ui` (experience) reference

The reference `kind: ui` plugin: a **web portal** experience surface.

A `kind: ui` plugin is an experience surface, not a data-plane component. The
platform's multi-UI story (overview.md / Phase 5) is **"each UI is a separate ui
plugin"** — web portal, CLI, Slack bot, VS Code extension are all distinct `ui`
plugins selected per deployment. So this contract is **deliberately thin**: a UI
plugin mostly **consumes the API gateway** like any other client. What the core
needs from it is only two things:

1. **Discovery** — what surface is this, and which **slots** does it host?
2. **Rendering** — given a slot + a contributing component, what does the UI need
   to render it?

## The slot mechanism

Slots are named extension points (capability-shaped URIs like
`rat://ui/v1/pipeline-detail`). Other plugins contribute components into them by
naming the slot in `contributes.slots[].target` in their `plugin.v1.json` (the
portal-slot mechanism — overview.md contract triple). `RenderSlot` resolves a
`(slot, component)` pair to the **render info**: an `asset_ref` (a JS bundle URL
or an OCI asset ref the UI loads) plus a serialized JSON-Schema `props_schema`
describing the props that component accepts. This is what lets the platform stay
extensible at the *experience* layer without the UI plugin knowing its peers — a
new plugin ships a component, declares the slot, and the portal renders it.

## Capabilities

| Capability | RPC | What it does |
|---|---|---|
| `rat://ui/v1/describe` | `Describe` | Enumerate the surface (`display_name`) + the slots this UI hosts |
| `rat://ui/v1/render-slot` | `RenderSlot` | Resolve a `(slot, component)` to render info (`asset_ref` + `props_schema`); unknown → `NOT_FOUND` |

This axis carries **no tenant/context** — Describe/RenderSlot are surface
metadata, not tenant-scoped operations — so (unlike the storage reference) there
is no `rat-callmeta-bin` handling.

## Files

| File | Role |
|---|---|
| `store.py` | `WebPortalUi` — the pure logic: hosted slots + the `(slot, component) → (asset_ref, props_schema)` map; unknown component → `UiError(NOT_FOUND)` |
| `server.py` | `UiServicer` — thin gRPC adapter; translates `UiError` into `context.abort` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/ui-v1.json` and drives this impl over real gRPC; asserts Describe (name + slot set), RenderSlot (asset ref + props schema present), and the unknown-component `NOT_FOUND` path |

## How it's tested

The harness boots `UiServicer` on a real in-process gRPC server and loads the
shared golden vectors (`contracts/conformance/ui-v1.json`):

- **Describe** — asserts `display_name == "Web Portal"` and the **set** of hosted
  slot ids matches the vector (order-independent).
- **RenderSlot** — for a known component, asserts a non-empty `asset_ref` and a
  non-empty `props_schema` that decodes to a JSON object.
- **Errors** — an unknown component aborts with `NOT_FOUND`.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/ui/web-portal-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-ui-web-portal-py conformed to ui/v1 golden vectors`.
