// vscode-rat — a VS Code window into the RAT v3 data-dev plane.
//
// EXPLORATORY (experiments/data-dev-plane, build-order step 6). The cleanest expression
// of the multi-UI vision (CLI / web-portal / VS Code): the editor is a UI client of the
// platform. Every action maps to a data-dev capability — browse the DuckLake catalog,
// run the incremental-embed pipeline, query, 🔍 semantic-search — relayed by the
// gateway (examples/ui/vscode-rat/gateway). Start it with `make data-dev-gateway`.

import * as vscode from "vscode";
import { GatewayClient } from "./client";
import { CatalogProvider, HealthProvider } from "./tree";
import { showQuery, showSearch } from "./panel";

function clientFactory(): GatewayClient {
  const url = vscode.workspace.getConfiguration("ratDataDev").get<string>("gatewayUrl", "http://localhost:8787");
  return new GatewayClient(url);
}

export function activate(context: vscode.ExtensionContext): void {
  const client = clientFactory;
  const catalog = new CatalogProvider(client);
  const health = new HealthProvider(client);

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("ratDataDev.catalog", catalog),
    vscode.window.registerTreeDataProvider("ratDataDev.health", health),
  );

  const refresh = () => { catalog.refresh(); health.refresh(); };

  context.subscriptions.push(
    vscode.commands.registerCommand("ratDataDev.refresh", refresh),

    vscode.commands.registerCommand("ratDataDev.runPipeline", async () => {
      try {
        const r = await vscode.window.withProgress(
          { location: vscode.ProgressLocation.Notification, title: "RAT: running incremental-embed pipeline…" },
          () => client().runPipeline());
        const how = r.already_applied ? "already applied (idempotent)" : `embedded ${r.embedded} new row(s)`;
        vscode.window.showInformationMessage(`RAT pipeline: ${how} → ${r.total} rows total · ${r.snapshot}`);
        refresh();
      } catch (e: any) {
        vscode.window.showErrorMessage(`RAT pipeline failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.query", async () => {
      const sql = await vscode.window.showInputBox({
        prompt: "SQL to run against the engine (DuckLake tables are under `lake.`)",
        value: "SELECT id, rating, text FROM lake.reviews ORDER BY id",
        ignoreFocusOut: true,
      });
      if (!sql) { return; }
      try {
        showQuery(sql, await client().query(sql));
      } catch (e: any) {
        vscode.window.showErrorMessage(`Query failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.search", async () => {
      const q = await vscode.window.showInputBox({
        prompt: "🔍 Semantic search over lake.reviews (embed → vss cosine rank)",
        placeHolder: "how is the battery life",
        ignoreFocusOut: true,
      });
      if (!q) { return; }
      try {
        const { results } = await client().search(q, 10);
        showSearch(q, results);
      } catch (e: any) {
        vscode.window.showErrorMessage(`Search failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.previewTable", async (identifier?: string) => {
      if (!identifier) { return; }
      const sql = `SELECT * FROM lake.${identifier}`;
      try {
        showQuery(sql, await client().query(sql, 100));
      } catch (e: any) {
        vscode.window.showErrorMessage(`Preview failed: ${e?.message ?? e}`);
      }
    }),
  );

  refresh();
}

export function deactivate(): void { /* gateway is a separate process; nothing to clean up */ }
