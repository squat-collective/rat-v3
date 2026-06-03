// The GENERIC RAT platform shell — the `vscode` SURFACE consumer (ADR-024 + ADR-025). It
// hardcodes NO platform view: on activation it fetches /api/ui?surface=vscode — the
// contributions every plugin targeted at the vscode surface — and renders them. A plugin's
// cli/webapp interfaces never appear here (surface scoping); a new plugin that contributes a
// vscode view/command/config appears with ZERO change to this extension. Actions route back
// through /api/invoke (→ the gateway, C5-authorized + audited). The VSCode `contributes`
// model, for a data platform, scoped to one surface.

import * as http from "http";
import { URL } from "url";
import * as vscode from "vscode";

function base(): string {
  return vscode.workspace.getConfiguration("ratPlatform").get<string>("bff", "http://127.0.0.1:8080");
}

// This extension IS the `vscode` surface consumer (ADR-025): it asks the bff only for the
// contributions targeted at its surface, so a plugin's cli/webapp interfaces never appear here.
function surface(): string {
  return vscode.workspace.getConfiguration("ratPlatform").get<string>("surface", "vscode");
}

function uiPath(): string {
  return `/api/ui?surface=${encodeURIComponent(surface())}`;
}

function req(method: string, path: string, body?: unknown): Promise<any> {
  return new Promise((resolve, reject) => {
    const u = new URL(path, base());
    const data = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    const r = http.request(
      u,
      { method, headers: data ? { "Content-Type": "application/json", "Content-Length": data.length } : {} },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c) => chunks.push(c as Buffer));
        res.on("end", () => {
          try {
            resolve(JSON.parse(Buffer.concat(chunks).toString() || "{}"));
          } catch (e) {
            reject(e);
          }
        });
      },
    );
    r.on("error", reject);
    if (data) {
      r.write(data);
    }
    r.end();
  });
}

// A contributed component (the runtime spec the bff aggregated from ui/components/*).
interface Component {
  slot: string;
  id: string;
  title: string;
  icon?: string;
  data?: string; // explorer: a bff route returning {tables:[...]} | {runs:[...]} | rows
  item?: string; // explorer: a per-item route prefix (e.g. /api/table/) to drill in
  capability?: string; // command/config: what to invoke
  args?: any; // command: default args
  schema?: any; // config: a JSON Schema
  _source?: string; // the contributing plugin
}

class Node extends vscode.TreeItem {
  constructor(
    label: string,
    collapsible: vscode.TreeItemCollapsibleState,
    public readonly comp?: Component,
    public readonly drill?: { route: string },
  ) {
    super(label, collapsible);
  }
}

// The tree is built ENTIRELY from /api/ui: top level = slots, then components, then (for
// explorer views) their fetched rows/items. Nothing platform-specific is hardcoded here.
class PlatformTree implements vscode.TreeDataProvider<Node> {
  private _onDidChange = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  private ui: { slots: Record<string, Component[]> } = { slots: {} };

  async refresh(): Promise<void> {
    this.ui = await req("GET", uiPath());
    this._onDidChange.fire();
  }

  getTreeItem(n: Node): vscode.TreeItem {
    return n;
  }

  async getChildren(n?: Node): Promise<Node[]> {
    if (!n) {
      // top level: one expandable node per slot present
      return Object.keys(this.ui.slots).map(
        (slot) => new Node(slot.toUpperCase(), vscode.TreeItemCollapsibleState.Expanded),
      );
    }
    // a slot node → its components
    if (!n.comp && !n.drill) {
      const slot = (n.label as string).toLowerCase();
      return (this.ui.slots[slot] || []).map((c) => {
        const expandable = c.slot === "explorer" && !!c.data;
        const node = new Node(
          `${c.title}${c._source ? `  ·  ${c._source}` : ""}`,
          expandable ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None,
          c,
        );
        if (c.icon) {
          node.iconPath = new vscode.ThemeIcon(c.icon);
        }
        if (c.slot === "command") {
          node.command = { command: `ratPlatform.cmd.${c.id}`, title: c.title };
          node.contextValue = "command";
        }
        if (c.slot === "config") {
          node.command = { command: "ratPlatform.openConfig", title: "Configure", arguments: [c] };
        }
        return node;
      });
    }
    // an explorer component → fetch its data and list items
    if (n.comp && n.comp.data) {
      const body = await req("GET", n.comp.data);
      if (Array.isArray(body.tables)) {
        return body.tables.map(
          (t: string) => new Node(t, vscode.TreeItemCollapsibleState.Collapsed, undefined, { route: `${n.comp!.item}${t}` }),
        );
      }
      if (Array.isArray(body.runs)) {
        return body.runs.map((r: any) => new Node(`${r.tick ?? r.key}: ${r.status ?? ""}`, vscode.TreeItemCollapsibleState.None));
      }
    }
    // drilling into one table → its rows
    if (n.drill) {
      const body = await req("GET", n.drill.route);
      const cols: string[] = body.columns || [];
      return (body.rows || []).map(
        (row: any[]) => new Node(cols.map((c, i) => `${c}=${row[i]}`).join("  "), vscode.TreeItemCollapsibleState.None),
      );
    }
    return [];
  }
}

export async function activate(ctx: vscode.ExtensionContext): Promise<void> {
  const tree = new PlatformTree();
  ctx.subscriptions.push(vscode.window.registerTreeDataProvider("ratPlatform.tree", tree));

  ctx.subscriptions.push(vscode.commands.registerCommand("ratPlatform.refresh", () => registerAndRefresh(ctx, tree)));
  ctx.subscriptions.push(
    vscode.commands.registerCommand("ratPlatform.openConfig", async (c: Component) => {
      // a config contribution: show its schema fields; a `capability` (if given) sets it.
      const props = Object.keys(c.schema?.properties || {});
      const pick = await vscode.window.showQuickPick(props.length ? props : ["(no fields)"], {
        title: `${c.title} — config (${c._source})`,
      });
      if (pick && c.capability) {
        const val = await vscode.window.showInputBox({ prompt: `Set ${pick}` });
        if (val !== undefined) {
          await req("POST", "/api/invoke", { capability: c.capability, data: { [pick]: val } });
        }
      }
    }),
  );

  await registerAndRefresh(ctx, tree);
}

// Re-fetch /api/ui and (re)register a VS Code command per command-slot contribution. Each
// fires its capability through the bff's generic /api/invoke. New commands appear on refresh.
const registered = new Set<string>();
async function registerAndRefresh(ctx: vscode.ExtensionContext, tree: PlatformTree): Promise<void> {
  let ui: { slots: Record<string, Component[]> };
  try {
    ui = await req("GET", uiPath());
  } catch (e) {
    vscode.window.showWarningMessage(`RAT platform: cannot reach the bff at ${base()} (${e})`);
    return;
  }
  for (const c of ui.slots.command || []) {
    const id = `ratPlatform.cmd.${c.id}`;
    if (registered.has(id)) {
      continue;
    }
    registered.add(id);
    ctx.subscriptions.push(
      vscode.commands.registerCommand(id, async () => {
        try {
          const res = await req("POST", "/api/invoke", { capability: c.capability, data: c.args || {} });
          vscode.window.showInformationMessage(`${c.title} → ${JSON.stringify(res)}`);
          await tree.refresh();
        } catch (e) {
          vscode.window.showErrorMessage(`${c.title} failed: ${e}`);
        }
      }),
    );
  }
  await tree.refresh();
}

export function deactivate(): void {}
