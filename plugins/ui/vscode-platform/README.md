# vscode-platform — the `vscode` surface consumer (ADR-024 + ADR-025)

A VS Code extension that **hardcodes no view**. It is the **`vscode` surface consumer**
(ADR-025): on activation it fetches `GET /api/ui?surface=vscode` — the contributions plugins
targeted at *this* surface — and renders them. A plugin's `cli`/`webapp` interfaces never
appear here (surface scoping); a new plugin that contributes a *vscode* view/command/config
appears with **zero change to this extension**. The VSCode `contributes` model, scoped to one
surface. (Configure the surface via `ratPlatform.surface`, default `vscode`.)

## How it works

```
plugin manifest:  contributes.slots: [{ target: rat://ui/v1/<slot>, component: <name> }]
plugin at boot:   state/put ui/components/<plugin>/<id> = { slot, title, data|capability|schema, … }
        bff:      GET /api/ui   → aggregates ui/components/* by slot
   this shell:    renders each slot; actions → POST /api/invoke → gateway (C5 + audit)
```

| slot | rendered as | the contribution supplies |
|---|---|---|
| `explorer` | tree views (drill into tables/rows, run history) | a `data` bff route (+ `item` route to drill) |
| `command` | a VS Code command (palette + clickable) | a `capability` + default `args` |
| `config` | a settings panel (fields from the schema) | a JSON `schema` (+ a set `capability`) |

Everything routes through the bff's generic `/api/invoke`, so a contributed command is a
normal capability call — C5-authorized and audited like any other, never a UI backdoor.

## Run it

Point `ratPlatform.bff` at a running platform bff (`http://127.0.0.1:<bff-port>`), open the
**RAT Platform** view. The seeded platform contributions (Lake Tables, Run History, Run
pipeline) appear; publish a new `ui/components/<x>` and hit **RAT: Refresh UI** to see it.

> Build: `npm install && npm run compile`. Compile-verified strict; the rendering itself needs
> a running VS Code (it can't be exercised headlessly — only the bff aggregation can, and is).
