// RatFS — a GENERIC, contribution-driven FileSystemProvider that mounts a RAT workspace's
// filesystem-contributing plugin as a native VS Code folder, via the federation hub (ADR-033/034).
//
// CONTRIBUTION MODEL (the push, not the pull): a plugin DECLARES it is a filesystem in its manifest
// (`contributes.slots[].target: rat://ui/v1/filesystem` — see code-fs). The surface renders that
// contribution GENERICALLY — it holds NO knowledge of "code-fs" or its backend. It speaks whatever
// filesystem-capability FAMILY the contributor provides, selected by the connection's `fs.capability`
// descriptor (which mirrors the plugin's declaration). So code-fs (over state/v1, S3-backed) and a
// future fs-git / fs-local (over fs/v1, or any backend) mount identically — the surface is decoupled
// from both the plugin and its storage. code-fs's S3-ness is invisible here (it always was: the
// surface calls get/put/list; code-fs does S3 internally, over any S3-compatible storage plugin).
//
// OWED (the last mile): AUTO-discovery of which plugins contribute a filesystem. Today the descriptor
// is configured per-connection (sourced from the plugin's declaration). Auto-discovery needs the hub
// to forward `ListPlugins` AND `ListPlugins` to surface `contributes` (an additive amendment to the
// frozen control.proto) — ADR-pending; until then the connection names the contribution explicitly.
//
// TRANSPORT: shells the proven `rat call` path (TLS --cacert, auth --token, routing --workspace).

import * as vscode from "vscode";
import { execFile } from "child_process";
import { getConnections, RatConnection } from "./connections";

/** A read/write/list adapter for one filesystem-capability FAMILY (e.g. state/v1, later fs/v1). */
interface FsAdapter {
  get(key: string): { cap: string; data: unknown };
  parseGet(r: any): { found: boolean; valueB64: string; rev: number };
  put(key: string, valueB64: string): { cap: string; data: unknown };
  parsePut(r: any): string; // outcome ("" / PUT_OUTCOME_COMMITTED == ok)
  list(prefix: string): { cap: string; data: unknown };
  parseList(r: any): string[];
}

// The known filesystem-capability families. code-fs (and any state-backed fs) uses state/v1; a real
// `fs/v1` axis (ADR-032) is added here when it lands — RatFS itself does not change.
const ADAPTERS: Record<string, FsAdapter> = {
  "rat://state/v1": {
    get: (key) => ({ cap: "rat://state/v1/get", data: { key } }),
    parseGet: (r) => ({ found: !!r.found, valueB64: (r.value as string) ?? "", rev: Number(r.revision ?? 0) }),
    put: (key, valueB64) => ({ cap: "rat://state/v1/put", data: { key, value: valueB64 } }),
    parsePut: (r) => (r.outcome as string) ?? "PUT_OUTCOME_COMMITTED",
    list: (prefix) => ({ cap: "rat://state/v1/list", data: { prefix } }),
    parseList: (r) => ((r.keys as string[]) ?? []),
  },
  // "rat://fs/v1": { ... read/write/list/stat/delete ... }  ← add when ADR-032's fs axis lands
};

interface Mount { adapter: FsAdapter; prefix: string; conn: RatConnection; }

// A path→bytes namespace (state/v1) has no empty directories. To let "New Folder" persist, we write
// a zero-byte marker object; readDirectory hides it. (When `Delete` lands on the fs capability, the
// marker is deleted with the folder.) Standard object-store "folder marker" trick.
const DIR_MARKER = ".ratkeep";

/** Run one `rat call` against the connection's hub and return the parsed protojson response. */
function ratCall(conn: RatConnection, capability: string, data: unknown): Promise<any> {
  const hub = conn.hub ?? conn.url ?? "127.0.0.1:7700";
  const caller = conn.caller ?? "s3-storage"; // must `require` the fs capability (C5)
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

  // uri.authority = the connection name; uri.path = the path within the mount root.
  private mount(uri: vscode.Uri): Mount {
    const conn = getConnections().find((x) => x.name === uri.authority);
    if (!conn) { throw vscode.FileSystemError.FileNotFound(`no RAT connection "${uri.authority}"`); }
    const family = conn.fs?.capability ?? "rat://state/v1";
    const adapter = ADAPTERS[family];
    if (!adapter) {
      throw vscode.FileSystemError.Unavailable(
        `RatFS: no adapter for filesystem capability "${family}" (known: ${Object.keys(ADAPTERS).join(", ")})`);
    }
    return { adapter, prefix: conn.fs?.prefix ?? "", conn };
  }
  // map a mount-relative path to the contributor's namespace key (applying the mount prefix).
  private key(m: Mount, uri: vscode.Uri): string {
    const rel = uri.path.replace(/^\/+/, "");
    return m.prefix ? m.prefix.replace(/\/?$/, "/") + rel : rel;
  }

  async stat(uri: vscode.Uri): Promise<vscode.FileStat> {
    const m = this.mount(uri);
    if (uri.path.replace(/^\/+/, "") === "") { // the mount root is always a directory
      return { type: vscode.FileType.Directory, ctime: 0, mtime: 0, size: 0 };
    }
    const key = this.key(m, uri);
    const g = m.adapter.get(key);
    const got = m.adapter.parseGet(await ratCall(m.conn, g.cap, g.data));
    if (got.found) {
      const size = got.valueB64 ? Buffer.from(got.valueB64, "base64").length : 0;
      return { type: vscode.FileType.File, ctime: 0, mtime: got.rev, size };
    }
    const l = m.adapter.list(key + "/");
    if (m.adapter.parseList(await ratCall(m.conn, l.cap, l.data)).length > 0) {
      return { type: vscode.FileType.Directory, ctime: 0, mtime: 0, size: 0 };
    }
    throw vscode.FileSystemError.FileNotFound(uri);
  }

  async readDirectory(uri: vscode.Uri): Promise<[string, vscode.FileType][]> {
    const m = this.mount(uri);
    const base = this.key(m, uri);
    const prefix = base === "" ? (m.prefix ? m.prefix.replace(/\/?$/, "/") : "") : base.replace(/\/?$/, "/");
    const l = m.adapter.list(prefix);
    const keys = m.adapter.parseList(await ratCall(m.conn, l.cap, l.data));
    const children = new Map<string, vscode.FileType>();
    for (const full of keys) {
      if (!full.startsWith(prefix)) { continue; }
      const rest = full.slice(prefix.length);
      if (rest === "" || rest === DIR_MARKER) { continue; } // hide the empty-folder marker
      const slash = rest.indexOf("/");
      if (slash === -1) { children.set(rest, vscode.FileType.File); }
      else { children.set(rest.slice(0, slash), vscode.FileType.Directory); }
    }
    return [...children.entries()];
  }

  async readFile(uri: vscode.Uri): Promise<Uint8Array> {
    const m = this.mount(uri);
    const g = m.adapter.get(this.key(m, uri));
    const got = m.adapter.parseGet(await ratCall(m.conn, g.cap, g.data));
    if (!got.found) { throw vscode.FileSystemError.FileNotFound(uri); }
    return new Uint8Array(Buffer.from(got.valueB64, "base64"));
  }

  async writeFile(uri: vscode.Uri, content: Uint8Array, opts: { create: boolean; overwrite: boolean }): Promise<void> {
    const m = this.mount(uri);
    const key = this.key(m, uri);
    // Honor VS Code's create/overwrite contract (a normal save is create+overwrite → no extra read).
    if (!opts.create || !opts.overwrite) {
      const g = m.adapter.get(key);
      const exists = m.adapter.parseGet(await ratCall(m.conn, g.cap, g.data)).found;
      if (!exists && !opts.create) { throw vscode.FileSystemError.FileNotFound(uri); }
      if (exists && opts.create && !opts.overwrite) { throw vscode.FileSystemError.FileExists(uri); }
    }
    const p = m.adapter.put(key, Buffer.from(content).toString("base64"));
    const outcome = m.adapter.parsePut(await ratCall(m.conn, p.cap, p.data));
    if (outcome && outcome !== "PUT_OUTCOME_COMMITTED") {
      // CAS conflict = a concurrent edit landed first (collaborative safety, ADR-032).
      throw vscode.FileSystemError.Unavailable(`write rejected (${outcome}) — a concurrent edit landed first`);
    }
    this._emitter.fire([{ type: vscode.FileChangeType.Changed, uri }]);
  }

  // Empty folders: write a hidden marker so the directory persists (path→bytes has no empty dirs).
  async createDirectory(uri: vscode.Uri): Promise<void> {
    const m = this.mount(uri);
    const marker = this.key(m, uri).replace(/\/?$/, "/") + DIR_MARKER;
    const p = m.adapter.put(marker, "");
    await ratCall(m.conn, p.cap, p.data);
    this._emitter.fire([{ type: vscode.FileChangeType.Created, uri }]);
  }

  // delete / rename / move need a DELETE on the filesystem capability. state/v1 has none — so these
  // are honestly unsupported until `Delete` lands on state/v1 (or the fs axis, ADR-032). Clear error
  // (not a crash) so VS Code surfaces it as "operation not supported".
  async delete(_uri: vscode.Uri): Promise<void> {
    throw vscode.FileSystemError.NoPermissions(
      "Delete isn't supported yet: this filesystem is backed by the state axis (get/put/list), which has no delete. Pending a `Delete` on state/v1 (or the fs axis).");
  }
  async rename(_old: vscode.Uri, _new: vscode.Uri): Promise<void> {
    throw vscode.FileSystemError.NoPermissions(
      "Rename/move isn't supported yet: it needs delete (rename = copy + delete), and state/v1 has no delete. Pending a `Delete` on state/v1 (or the fs axis).");
  }
  watch(_uri: vscode.Uri): vscode.Disposable { return new vscode.Disposable(() => { /* no server push yet */ }); }
}
