// RatFS — a VS Code FileSystemProvider that mounts a RAT workspace's code-fs as a native folder.
//
// Maps the `rat://<connection>/<path>` URI scheme onto the FROZEN state axis through the federation
// hub (ADR-033) — get/put/list = read/write/list a path→bytes namespace (code-fs, the collaborative
// remote code filesystem). Once registered, the VS Code Explorer, editor, save, and search all work
// natively on `rat://…` because VS Code treats it as a real filesystem.
//
// TRANSPORT: it shells out to the `rat` binary (`rat call …`) — the exact, already-proven hub path
// (TLS via --cacert, auth via --token, routing via --workspace; ADR-034). This reuses the verified
// CLI rather than re-implementing gRPC + the binary call-context metadata in Node; a native
// Connect-ES transport (no shell-out) is a clean future refinement. Pattern: like the built-in Git
// extension shelling `git`. Requires `rat` on PATH (or set `ratDataDev.ratPath`).

import * as vscode from "vscode";
import { execFile } from "child_process";
import { getConnections, RatConnection } from "./connections";

/** Run one `rat call` against the connection's hub and return the parsed protojson response. */
function ratCall(conn: RatConnection, capability: string, data: unknown): Promise<any> {
  const hub = conn.hub ?? conn.url ?? "127.0.0.1:7700";
  const caller = conn.caller ?? "s3-storage"; // must `require` state/* (C5); s3-storage does
  const args = ["call", capability, "--as", caller, "--addr", hub, "--data", JSON.stringify(data ?? {})];
  if (conn.workspace) { args.push("--workspace", conn.workspace); }
  if (conn.token) { args.push("--token", conn.token); }
  if (conn.cacert) { args.push("--cacert", conn.cacert); }
  const bin = vscode.workspace.getConfiguration("ratDataDev").get<string>("ratPath", "rat");
  return new Promise((resolve, reject) => {
    execFile(bin, args, { maxBuffer: 64 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) { reject(new Error((stderr || (err as Error).message).trim())); return; }
      try { resolve(stdout.trim() ? JSON.parse(stdout) : {}); }
      catch { reject(new Error(`unparseable rat output: ${stdout.slice(0, 200)}`)); }
    });
  });
}

export class RatFS implements vscode.FileSystemProvider {
  private readonly _emitter = new vscode.EventEmitter<vscode.FileChangeEvent[]>();
  readonly onDidChangeFile = this._emitter.event;

  // uri.authority = the connection name; uri.path = the code-fs key (a path→bytes namespace).
  private conn(uri: vscode.Uri): RatConnection {
    const c = getConnections().find((x) => x.name === uri.authority);
    if (!c) { throw vscode.FileSystemError.FileNotFound(`no RAT connection "${uri.authority}"`); }
    return c;
  }
  private key(uri: vscode.Uri): string { return uri.path.replace(/^\/+/, ""); }

  async stat(uri: vscode.Uri): Promise<vscode.FileStat> {
    const key = this.key(uri);
    if (key === "") { return { type: vscode.FileType.Directory, ctime: 0, mtime: 0, size: 0 }; }
    const got = await ratCall(this.conn(uri), "rat://state/v1/get", { key });
    if (got.found) {
      const size = got.value ? Buffer.from(got.value, "base64").length : 0;
      return { type: vscode.FileType.File, ctime: 0, mtime: Number(got.revision ?? 0), size };
    }
    // not a file — a directory if any key lives under "<key>/"
    const listed = await ratCall(this.conn(uri), "rat://state/v1/list", { prefix: key + "/" });
    if (((listed.keys as string[]) ?? []).length > 0) {
      return { type: vscode.FileType.Directory, ctime: 0, mtime: 0, size: 0 };
    }
    throw vscode.FileSystemError.FileNotFound(uri);
  }

  async readDirectory(uri: vscode.Uri): Promise<[string, vscode.FileType][]> {
    const base = this.key(uri);
    const prefix = base === "" ? "" : base.replace(/\/?$/, "/");
    const listed = await ratCall(this.conn(uri), "rat://state/v1/list", { prefix });
    // collapse full keys into immediate children: an exact key is a File, a deeper one a Directory.
    const children = new Map<string, vscode.FileType>();
    for (const full of ((listed.keys as string[]) ?? [])) {
      if (!full.startsWith(prefix)) { continue; }
      const rest = full.slice(prefix.length);
      if (rest === "") { continue; }
      const slash = rest.indexOf("/");
      if (slash === -1) { children.set(rest, vscode.FileType.File); }
      else { children.set(rest.slice(0, slash), vscode.FileType.Directory); }
    }
    return [...children.entries()];
  }

  async readFile(uri: vscode.Uri): Promise<Uint8Array> {
    const got = await ratCall(this.conn(uri), "rat://state/v1/get", { key: this.key(uri) });
    if (!got.found) { throw vscode.FileSystemError.FileNotFound(uri); }
    return new Uint8Array(Buffer.from((got.value as string) ?? "", "base64"));
  }

  async writeFile(uri: vscode.Uri, content: Uint8Array, _opts: { create: boolean; overwrite: boolean }): Promise<void> {
    const value = Buffer.from(content).toString("base64");
    const res = await ratCall(this.conn(uri), "rat://state/v1/put", { key: this.key(uri), value });
    if (res.outcome && res.outcome !== "PUT_OUTCOME_COMMITTED") {
      // CAS conflict = a concurrent edit landed first (collaborative safety, ADR-032).
      throw vscode.FileSystemError.Unavailable(`write rejected (${res.outcome}) — code-fs detected a concurrent edit`);
    }
    this._emitter.fire([{ type: vscode.FileChangeType.Changed, uri }]);
  }

  // code-fs is the frozen state axis: get/put/list only. No delete/rename in the contract.
  async delete(): Promise<void> {
    throw vscode.FileSystemError.NoPermissions("code-fs has no delete capability (state/v1 is get/put/list)");
  }
  async rename(): Promise<void> {
    throw vscode.FileSystemError.NoPermissions("code-fs has no rename capability (copy via read+write instead)");
  }
  createDirectory(_uri: vscode.Uri): void { /* directories are implicit in the path namespace */ }
  watch(_uri: vscode.Uri): vscode.Disposable { return new vscode.Disposable(() => { /* no server push yet */ }); }
}
