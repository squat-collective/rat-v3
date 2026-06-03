// Connection store — the extension manages MANY named RAT connections (like a DB
// explorer manages many servers). Each connection points at a RAT platform endpoint
// (a data-dev gateway today; a real core API gateway later): local / staging / prod /
// per-tenant / per-region. One editor, N planes — the "one UI, many planes" story.
//
// Connections persist in the `ratDataDev.connections` setting (user-global), so they're
// editable as JSON and ride VS Code Settings Sync. The model is intentionally small with
// room to grow (tenant, token/auth, TLS).

import * as vscode from "vscode";

export interface RatConnection {
  name: string;
  url: string;
  tenant?: string; // forward-room: stamped as identity once the core fronts the UI
}

const SECTION = "ratDataDev";

export function getConnections(): RatConnection[] {
  // Return exactly what the user configured — NO implicit default. An empty list is a
  // valid, respected state (the view shows an "Add Connection" welcome). Re-seeding a
  // default here is what made a sole `local` connection undeletable.
  const list = vscode.workspace.getConfiguration(SECTION).get<RatConnection[]>("connections", []);
  return (list || []).filter((c) => c && c.name && c.url);
}

export async function saveConnections(list: RatConnection[]): Promise<void> {
  await vscode.workspace.getConfiguration(SECTION).update(
    "connections", list, vscode.ConfigurationTarget.Global);
}

export async function addConnection(conn: RatConnection): Promise<void> {
  const list = getConnections();
  if (list.some((c) => c.name === conn.name)) {
    throw new Error(`a connection named "${conn.name}" already exists`);
  }
  await saveConnections([...list, conn]);
}

export async function removeConnection(name: string): Promise<void> {
  await saveConnections(getConnections().filter((c) => c.name !== name));
}

export async function updateConnection(name: string, next: RatConnection): Promise<void> {
  await saveConnections(getConnections().map((c) => (c.name === name ? next : c)));
}

/** Resolve which connection an action targets: an explicit one, the sole one, or a pick. */
export async function pickConnection(explicit?: RatConnection): Promise<RatConnection | undefined> {
  if (explicit) { return explicit; }
  const list = getConnections();
  if (list.length === 1) { return list[0]; }
  const choice = await vscode.window.showQuickPick(
    list.map((c) => ({ label: c.name, description: c.url, conn: c })),
    { placeHolder: "Which RAT connection?" });
  return choice?.conn;
}
