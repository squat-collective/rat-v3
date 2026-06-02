// vscode-rat — a VS Code window into the RAT v3 data-dev plane, across MANY connections.
//
// EXPLORATORY (experiments/data-dev-plane, build-order step 6). The editor is a UI client
// of the platform — and it manages many named RAT connections (local / staging / prod /
// per-tenant), each pointing at a RAT endpoint (a data-dev gateway today, a core API
// gateway later). One editor, N planes. Every action maps to a data-dev capability:
// browse the DuckLake catalog, run the incremental-embed pipeline, query, 🔍 search.

import * as vscode from "vscode";
import { GatewayClient } from "./client";
import { CatalogProvider, HealthProvider, ConnectionNode, TableNode } from "./tree";
import { showQuery, showSearch } from "./panel";
import {
  RatConnection, getConnections, addConnection, removeConnection,
  updateConnection, pickConnection,
} from "./connections";

function clientFor(conn: RatConnection): GatewayClient { return new GatewayClient(conn.url); }

/** Resolve the connection a command targets: from a clicked tree node, or by picking. */
async function resolve(arg?: unknown): Promise<RatConnection | undefined> {
  if (arg instanceof ConnectionNode) { return arg.conn; }
  if (arg instanceof TableNode) { return arg.conn; }
  return pickConnection();
}

export function activate(context: vscode.ExtensionContext): void {
  const catalog = new CatalogProvider();
  const health = new HealthProvider();
  const refresh = () => { catalog.refresh(); health.refresh(); };

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("ratDataDev.catalog", catalog),
    vscode.window.registerTreeDataProvider("ratDataDev.health", health),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("ratDataDev.connections")) { refresh(); }
    }),

    vscode.commands.registerCommand("ratDataDev.refresh", refresh),

    // --- connection management ----------------------------------------------------
    vscode.commands.registerCommand("ratDataDev.addConnection", async () => {
      const name = await vscode.window.showInputBox({
        prompt: "Connection name", placeHolder: "staging", ignoreFocusOut: true,
        validateInput: (v) => (v && v.trim() ? undefined : "name is required"),
      });
      if (!name) { return; }
      const url = await vscode.window.showInputBox({
        prompt: `RAT gateway URL for "${name}"`, value: "http://localhost:8787",
        ignoreFocusOut: true,
        validateInput: (v) => (/^https?:\/\//.test(v || "") ? undefined : "must be an http(s) URL"),
      });
      if (!url) { return; }
      const tenant = await vscode.window.showInputBox({
        prompt: "Tenant (optional)", placeHolder: "leave blank for solo/default", ignoreFocusOut: true,
      });
      try {
        await addConnection({ name: name.trim(), url: url.trim(), ...(tenant ? { tenant: tenant.trim() } : {}) });
        refresh();
      } catch (e: any) {
        vscode.window.showErrorMessage(`Add connection failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.editConnection", async (node?: ConnectionNode) => {
      const conn = node?.conn ?? (await pickConnection());
      if (!conn) { return; }
      const url = await vscode.window.showInputBox({
        prompt: `URL for "${conn.name}"`, value: conn.url, ignoreFocusOut: true,
        validateInput: (v) => (/^https?:\/\//.test(v || "") ? undefined : "must be an http(s) URL"),
      });
      if (!url) { return; }
      await updateConnection(conn.name, { ...conn, url: url.trim() });
      refresh();
    }),

    vscode.commands.registerCommand("ratDataDev.removeConnection", async (node?: ConnectionNode) => {
      const conn = node?.conn ?? (await pickConnection());
      if (!conn) { return; }
      const ok = await vscode.window.showWarningMessage(
        `Remove RAT connection "${conn.name}"?`, { modal: true }, "Remove");
      if (ok !== "Remove") { return; }
      await removeConnection(conn.name);
      refresh();
    }),

    // --- per-connection actions ---------------------------------------------------
    vscode.commands.registerCommand("ratDataDev.runPipeline", async (arg?: unknown) => {
      const conn = await resolve(arg);
      if (!conn) { return; }
      try {
        const r = await vscode.window.withProgress(
          { location: vscode.ProgressLocation.Notification, title: `RAT [${conn.name}]: running pipeline…` },
          () => clientFor(conn).runPipeline());
        const how = r.already_applied ? "already applied (idempotent)" : `embedded ${r.embedded} new row(s)`;
        vscode.window.showInformationMessage(`[${conn.name}] pipeline: ${how} → ${r.total} rows · ${r.snapshot}`);
        refresh();
      } catch (e: any) {
        vscode.window.showErrorMessage(`[${conn.name}] pipeline failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.query", async (arg?: unknown) => {
      const conn = await resolve(arg);
      if (!conn) { return; }
      const sql = await vscode.window.showInputBox({
        prompt: `SQL on "${conn.name}" (DuckLake tables under \`lake.\`)`,
        value: "SELECT id, rating, text FROM lake.reviews ORDER BY id", ignoreFocusOut: true,
      });
      if (!sql) { return; }
      try {
        showQuery(conn.name, sql, await clientFor(conn).query(sql));
      } catch (e: any) {
        vscode.window.showErrorMessage(`[${conn.name}] query failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.search", async (arg?: unknown) => {
      const conn = await resolve(arg);
      if (!conn) { return; }
      const q = await vscode.window.showInputBox({
        prompt: `🔍 Semantic search on "${conn.name}" (lake.reviews)`,
        placeHolder: "how is the battery life", ignoreFocusOut: true,
      });
      if (!q) { return; }
      try {
        const { results } = await clientFor(conn).search(q, 10);
        showSearch(conn.name, q, results);
      } catch (e: any) {
        vscode.window.showErrorMessage(`[${conn.name}] search failed: ${e?.message ?? e}`);
      }
    }),

    vscode.commands.registerCommand("ratDataDev.previewTable", async (arg?: { url: string; identifier: string }) => {
      if (!arg?.identifier) { return; }
      const sql = `SELECT * FROM lake.${arg.identifier}`;
      const conn = getConnections().find((c) => c.url === arg.url);
      try {
        showQuery(conn?.name ?? arg.url, sql, await new GatewayClient(arg.url).query(sql, 100));
      } catch (e: any) {
        vscode.window.showErrorMessage(`Preview failed: ${e?.message ?? e}`);
      }
    }),
  );

  refresh();
}

export function deactivate(): void { /* connections are remote; nothing to clean up */ }
