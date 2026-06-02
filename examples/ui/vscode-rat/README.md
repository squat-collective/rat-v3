# vscode-rat — a VS Code window into the data-dev plane (`kind: ui`, in spirit)

> 🛰️ **Exploratory** — part of the [data-dev plane experiment](../../../experiments/data-dev-plane/README.md).
> Additive: no proto/axis change. Build-order step 6.

The cleanest demonstration of the multi-UI vision (CLI / web-portal / **VS Code**): the
editor is a **UI client of the platform** — and it manages **many RAT connections** (like
a database explorer manages many servers). One editor, N planes: `local`, `staging`,
`prod`, per-tenant, per-region. Every action maps to a data-dev capability.

| surface | what it does |
|---|---|
| **Connections** | add many named RAT connections (▶ *Add Connection*); each points at a RAT gateway/core endpoint — local or **remote** |
| **DuckLake Catalog** tree | per connection → tables → snapshots (click a table to **preview** it) |
| **Run Pipeline** (on a connection) | runs the incremental-embed strategy on that plane; shows `embedded N → total` |
| **Run SQL Query** / **🔍 Semantic Search** | run against a chosen connection; results in a grid (titled with the connection) |
| **Plugin Health** tree | per connection → engine / catalog / strategy, Healthy/Degraded + loaded extensions |

### Connections (many environments)

Each connection is `{ name, url, tenant? }`, stored in the `ratDataDev.connections`
setting (so it's editable as JSON and rides Settings Sync). Add one from the catalog
view's **＋ Add Connection** button (name → URL → optional tenant), or right-click a
connection to **Run Pipeline / Query / Search / Edit / Remove** it. A connection's URL is
all the extension needs — point it at `http://localhost:8787` for a local gateway, or at
a **remote** environment running its own gateway/core. Connections that can't be reached
degrade gracefully (the tree shows *⚠ unreachable*), so a down plane never breaks the rest.

## Architecture — why a gateway (finding F9)

```
VS Code extension  ──HTTP/JSON──▶  data-dev gateway  ──gRPC──▶  engine + catalog + strategy
   (this folder, TS)               (gateway/, Python)           (the data-dev plugins)
```

The frozen contracts keep **bulk data off the control plane** — `engine.Query` returns
an `ArrowStream` the consumer pulls **out-of-band**. The reference engine's Arrow leg is
an **in-process** registry (a stand-in for Arrow Flight), so an external client (this
editor) can't pull query rows over the wire. The [`gateway/`](gateway/) owns the in-proc
stack, so it **can** pull that Arrow, and re-exposes results as JSON.

The **control** capabilities (catalog browse, `strategy.Apply`, health) are exactly what
the generated **Connect TypeScript SDK** (`contracts/sdks/typescript`, ADR-018's
connectionless codegen) would call directly against a production core. A production
engine with a real Flight endpoint would let a thin client pull the data leg too; until
then the gateway BFF closes it so the whole demo runs from one URL. The extension talks
to that one URL — swap it for a real core gateway and the editor code is unchanged.

## Install it (as a real extension)

Package it into a `.vsix` and install it into your VS Code — no host node/npm needed:

```bash
make data-dev-vsix                                          # builds examples/ui/vscode-rat/vscode-rat-0.1.0.vsix
code --install-extension examples/ui/vscode-rat/vscode-rat-0.1.0.vsix
```

(Or in VS Code: **Extensions** view → `…` menu → **Install from VSIX…** → pick the file.)

Then **start the backend** and reload:

```bash
make data-dev-gateway          # serves http://localhost:8787 (Ctrl-C to stop)
```

Click the **RAT Data-Dev** icon in the activity bar. The default `local` connection
points at `http://localhost:8787`; expand it to browse the catalog, then right-click it
to **Run Pipeline / Query / 🔍 Semantic Search**. Use **＋ Add Connection** to point at
more environments (remote gateways included) — each appears as its own root in the tree.

## Develop it (F5 debug)

1. `make data-dev-gateway` (the backend).
2. `cd examples/ui/vscode-rat && npm install && npm run compile`.
3. Open this folder in VS Code and press **`F5`** → an Extension Development Host launches
   with the extension loaded.

The gateway URL is configurable via the `ratDataDev.gatewayUrl` setting (default
`http://localhost:8787`) — point it at a remote gateway if you run one.

## Layout

```
vscode-rat/
├── package.json          # extension manifest (views, commands, config)
├── tsconfig.json
├── src/
│   ├── extension.ts      # activate(): register views + commands
│   ├── client.ts         # node:http client for the gateway
│   ├── tree.ts           # Catalog + Health tree data providers
│   └── panel.ts          # results webview (query / search grid)
├── media/rat.svg         # activity-bar icon
└── gateway/              # the Python BFF the extension talks to
    ├── app.py            # owns the in-proc stack; serves the JSON API
    └── selftest.py       # boots it + exercises every endpoint over HTTP
```

`npm run compile` type-checks + builds to `out/`. The gateway has its own self-test
(`gateway/selftest.py`) covering health/tables/snapshots/query/search/pipeline.
