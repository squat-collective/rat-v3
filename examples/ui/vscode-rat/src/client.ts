// A tiny HTTP client for the data-dev gateway (examples/ui/vscode-rat/gateway).
//
// Uses node:http (always present in the VS Code extension host) — no fetch/typing
// concerns. The gateway is the BFF that owns the in-proc engine+catalog+strategy and
// re-exposes them as JSON, because the reference engine's Arrow result leg is in-proc
// only (finding F9). The frozen CONTROL capabilities (catalog browse, strategy apply)
// are what the generated Connect TS SDK (ADR-018) would call directly against a
// production core; here they are relayed through the one gateway so the demo is a
// single endpoint.

import * as http from "http";
import { URL } from "url";

export interface TableInfo { identifier: string; rows: number; snapshot: string; branch: string; }
export interface PluginHealth { name: string; kind: string; status: string; extensions?: string[]; }
export interface QueryResult { columns: string[]; rows: any[][]; }
export interface SearchHit { id: number; rating?: number; text: string; dist: number; }
export interface PipelineResult { run_id: string; embedded: number; snapshot: string; already_applied: boolean; total: number; landed_batch: number; }

export class GatewayClient {
  constructor(private baseUrl: string) {}

  private request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const url = new URL(path, this.baseUrl);
    const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    const opts: http.RequestOptions = {
      method,
      hostname: url.hostname,
      port: url.port,
      path: url.pathname + url.search,
      headers: { "Content-Type": "application/json", ...(payload ? { "Content-Length": payload.length } : {}) },
    };
    return new Promise<T>((resolve, reject) => {
      const req = http.request(opts, (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c) => chunks.push(c as Buffer));
        res.on("end", () => {
          const text = Buffer.concat(chunks).toString("utf8");
          let parsed: any = {};
          try { parsed = text ? JSON.parse(text) : {}; } catch { /* leave {} */ }
          if (res.statusCode && res.statusCode >= 400) {
            reject(new Error(parsed.error || `HTTP ${res.statusCode}`));
          } else {
            resolve(parsed as T);
          }
        });
      });
      req.on("error", reject);
      req.setTimeout(60000, () => req.destroy(new Error("gateway request timed out")));
      if (payload) { req.write(payload); }
      req.end();
    });
  }

  health(): Promise<{ plugins: PluginHealth[] }> { return this.request("GET", "/api/health"); }
  tables(): Promise<{ tables: TableInfo[] }> { return this.request("GET", "/api/tables"); }
  snapshots(table: string): Promise<{ table: string; snapshots: string[] }> {
    return this.request("GET", `/api/snapshots?table=${encodeURIComponent(table)}`);
  }
  query(sql: string, limit = 200): Promise<QueryResult> { return this.request("POST", "/api/query", { sql, limit }); }
  search(query: string, k = 10): Promise<{ query: string; results: SearchHit[] }> {
    return this.request("POST", "/api/search", { query, k });
  }
  runPipeline(): Promise<PipelineResult> { return this.request("POST", "/api/pipeline/run", {}); }
}
