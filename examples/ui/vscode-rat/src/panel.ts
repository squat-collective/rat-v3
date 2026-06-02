// A single reusable webview panel that renders query/search results as an HTML grid.

import * as vscode from "vscode";
import { QueryResult, SearchHit } from "./client";

let panel: vscode.WebviewPanel | undefined;

function ensurePanel(): vscode.WebviewPanel {
  if (!panel) {
    panel = vscode.window.createWebviewPanel(
      "ratDataDevResults", "RAT Results", vscode.ViewColumn.Active, { enableScripts: false });
    panel.onDidDispose(() => { panel = undefined; });
  }
  panel.reveal(vscode.ViewColumn.Active);
  return panel;
}

function esc(v: unknown): string {
  return String(v ?? "").replace(/[&<>"]/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c] as string));
}

function page(title: string, headerHtml: string, columns: string[], rows: unknown[][]): string {
  const head = columns.map((c) => `<th>${esc(c)}</th>`).join("");
  const body = rows.map((r) => `<tr>${r.map((c) => `<td>${esc(c)}</td>`).join("")}</tr>`).join("");
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
    body { font-family: var(--vscode-editor-font-family, monospace); padding: 8px 12px; color: var(--vscode-foreground); }
    h2 { font-size: 13px; margin: 4px 0 2px; }
    .sub { opacity: 0.7; font-size: 12px; margin-bottom: 10px; }
    table { border-collapse: collapse; width: 100%; font-size: 12px; }
    th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--vscode-panel-border, #333); vertical-align: top; }
    th { position: sticky; top: 0; background: var(--vscode-editor-background); }
    tr:hover td { background: var(--vscode-list-hoverBackground); }
    td:first-child, th:first-child { white-space: nowrap; }
  </style></head><body>
    <h2>${esc(title)}</h2><div class="sub">${headerHtml}</div>
    <table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>
  </body></html>`;
}

export function showQuery(connection: string, sql: string, result: QueryResult): void {
  const p = ensurePanel();
  p.title = `RAT Query · ${connection}`;
  p.webview.html = page("SQL result",
    `<b>${esc(connection)}</b> · <code>${esc(sql)}</code> — ${result.rows.length} row(s)`,
    result.columns, result.rows);
}

export function showSearch(connection: string, query: string, hits: SearchHit[]): void {
  const p = ensurePanel();
  p.title = `RAT Search · ${connection}`;
  const cols = ["rank", "id", "dist", "★", "text"];
  const rows = hits.map((h, i) => [i + 1, h.id, h.dist.toFixed(3), h.rating ?? "", h.text]);
  p.webview.html = page("🔍 semantic search",
    `<b>${esc(connection)}</b> · query: <code>${esc(query)}</code> · ${hits.length} hit(s)`, cols, rows);
}
