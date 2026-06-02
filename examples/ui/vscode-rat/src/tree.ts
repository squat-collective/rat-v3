// Tree data providers for the two views in the RAT Data-Dev container:
//   - CatalogProvider: DuckLake tables → their snapshots (browse the lakehouse)
//   - HealthProvider:  one row per plugin, Healthy/Degraded

import * as vscode from "vscode";
import { GatewayClient, TableInfo } from "./client";

type Node = TableNode | SnapshotNode;

export class TableNode extends vscode.TreeItem {
  constructor(public readonly table: TableInfo) {
    super(table.identifier, vscode.TreeItemCollapsibleState.Collapsed);
    this.description = `${table.rows} rows · ${table.snapshot || "—"}`;
    this.contextValue = "table";
    this.iconPath = new vscode.ThemeIcon("table");
    this.tooltip = `${table.identifier} @ ${table.branch} (${table.snapshot})`;
    this.command = {
      command: "ratDataDev.previewTable",
      title: "Preview Table",
      arguments: [table.identifier],
    };
  }
}

class SnapshotNode extends vscode.TreeItem {
  constructor(snapshot: string) {
    super(snapshot, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon("git-commit");
    this.contextValue = "snapshot";
  }
}

export class CatalogProvider implements vscode.TreeDataProvider<Node> {
  private _onDidChange = new vscode.EventEmitter<Node | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;

  constructor(private client: () => GatewayClient) {}

  refresh(): void { this._onDidChange.fire(); }
  getTreeItem(element: Node): vscode.TreeItem { return element; }

  async getChildren(element?: Node): Promise<Node[]> {
    try {
      if (!element) {
        const { tables } = await this.client().tables();
        return tables.map((t) => new TableNode(t));
      }
      if (element instanceof TableNode) {
        const { snapshots } = await this.client().snapshots(element.table.identifier);
        return snapshots.map((s) => new SnapshotNode(s));
      }
      return [];
    } catch (e: any) {
      const item = new vscode.TreeItem(`⚠ gateway unreachable — run \`make data-dev-gateway\``);
      item.tooltip = String(e?.message ?? e);
      return [item as Node];
    }
  }
}

class PluginNode extends vscode.TreeItem {
  constructor(kind: string, name: string, status: string, extensions?: string[]) {
    super(`${kind}: ${name}`, vscode.TreeItemCollapsibleState.None);
    this.description = status;
    const icon = status === "Healthy" ? "pass" : status === "Degraded" ? "warning" : "circle-outline";
    this.iconPath = new vscode.ThemeIcon(icon);
    if (extensions && extensions.length) { this.tooltip = `extensions: ${extensions.join(", ")}`; }
  }
}

export class HealthProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onDidChange = new vscode.EventEmitter<vscode.TreeItem | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;

  constructor(private client: () => GatewayClient) {}

  refresh(): void { this._onDidChange.fire(); }
  getTreeItem(element: vscode.TreeItem): vscode.TreeItem { return element; }

  async getChildren(): Promise<vscode.TreeItem[]> {
    try {
      const { plugins } = await this.client().health();
      return plugins.map((p) => new PluginNode(p.kind, p.name, p.status, p.extensions));
    } catch (e: any) {
      const item = new vscode.TreeItem("⚠ gateway unreachable");
      item.tooltip = String(e?.message ?? e);
      return [item];
    }
  }
}
