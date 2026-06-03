// Connection-rooted tree providers for the two views in the RAT Data-Dev container:
//   - CatalogProvider: connection → DuckLake tables → snapshots  (browse N planes)
//   - HealthProvider:  connection → plugins (Healthy/Degraded)
//
// Each node carries its RatConnection so children fetch from the right endpoint and the
// per-connection commands (run pipeline / query / search / edit / remove) know their target.

import * as vscode from "vscode";
import { GatewayClient, TableInfo } from "./client";
import { RatConnection, getConnections } from "./connections";

export class ConnectionNode extends vscode.TreeItem {
  constructor(public readonly conn: RatConnection) {
    super(conn.name, vscode.TreeItemCollapsibleState.Expanded);
    this.description = conn.tenant ? `${conn.url} · ${conn.tenant}` : conn.url;
    this.contextValue = "connection";
    this.iconPath = new vscode.ThemeIcon("server-environment");
    this.tooltip = `RAT connection "${conn.name}" → ${conn.url}`;
  }
}

export class TableNode extends vscode.TreeItem {
  constructor(public readonly conn: RatConnection, public readonly table: TableInfo) {
    super(table.identifier, vscode.TreeItemCollapsibleState.Collapsed);
    this.description = `${table.rows} rows · ${table.snapshot || "—"}`;
    this.contextValue = "table";
    this.iconPath = new vscode.ThemeIcon("table");
    this.tooltip = `${table.identifier} @ ${table.branch} (${table.snapshot})`;
    this.command = {
      command: "ratDataDev.previewTable", title: "Preview Table",
      arguments: [{ url: conn.url, identifier: table.identifier }],
    };
  }
}

class LeafNode extends vscode.TreeItem {
  constructor(label: string, icon: string, contextValue?: string) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon(icon);
    if (contextValue) { this.contextValue = contextValue; }
  }
}

function unreachable(e: unknown): vscode.TreeItem {
  const item = new vscode.TreeItem("⚠ unreachable — is the gateway running?");
  item.iconPath = new vscode.ThemeIcon("warning");
  item.tooltip = String((e as any)?.message ?? e);
  return item;
}

function client(conn: RatConnection): GatewayClient { return new GatewayClient(conn.url); }

export class CatalogProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onDidChange = new vscode.EventEmitter<vscode.TreeItem | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  refresh(): void { this._onDidChange.fire(); }
  getTreeItem(e: vscode.TreeItem): vscode.TreeItem { return e; }

  async getChildren(element?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
    if (!element) {
      return getConnections().map((c) => new ConnectionNode(c));
    }
    if (element instanceof ConnectionNode) {
      try {
        const { tables } = await client(element.conn).tables();
        if (!tables.length) { return [new LeafNode("(no tables — run the pipeline)", "info")]; }
        return tables.map((t) => new TableNode(element.conn, t));
      } catch (e) { return [unreachable(e)]; }
    }
    if (element instanceof TableNode) {
      try {
        const { snapshots } = await client(element.conn).snapshots(element.table.identifier);
        return snapshots.map((s) => new LeafNode(s, "git-commit", "snapshot"));
      } catch (e) { return [unreachable(e)]; }
    }
    return [];
  }
}

export class HealthProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onDidChange = new vscode.EventEmitter<vscode.TreeItem | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  refresh(): void { this._onDidChange.fire(); }
  getTreeItem(e: vscode.TreeItem): vscode.TreeItem { return e; }

  async getChildren(element?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
    if (!element) {
      return getConnections().map((c) => new ConnectionNode(c));
    }
    if (element instanceof ConnectionNode) {
      try {
        const { plugins } = await client(element.conn).health();
        return plugins.map((p) => {
          const status = p.status;
          const icon = status === "Healthy" ? "pass" : status === "Degraded" ? "warning" : "circle-outline";
          const node = new LeafNode(`${p.kind}: ${p.name}`, icon);
          node.description = status;
          if (p.extensions?.length) { node.tooltip = `extensions: ${p.extensions.join(", ")}`; }
          return node;
        });
      } catch (e) { return [unreachable(e)]; }
    }
    return [];
  }
}
