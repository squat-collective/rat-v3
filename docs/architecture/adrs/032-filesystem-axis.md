# ADR-032: The filesystem axis (`rat://fs/v1/…`) — a pluggable remote filesystem for collaborative code

## Status: Deferred (2026-06-04)

> **Deferral note (2026-06-04).** Started, then reverted. Adding a *new axis* today requires a new
> proto **and recompiling the core** (the gateway's `routableDescriptors()` is hardcoded) — which
> contradicts "axes are plugins, the core doesn't change." For the immediate need (a collaborative
> remote code store), we use a **pure-plugin** approach instead: a plugin that **reuses the existing
> `state` axis** (`get`/`put`/`list` = read/write/list a path→bytes namespace) and **requires the
> `storage` axis** (so any storage backend — minio, S3, … — plugs in). No proto, no core change. See
> [`ideas/inbox.md`](../../../ideas/inbox.md) → "code-fs (plugin-only)". **Revisit this fs axis** when
> richer semantics (stat/delete/real dirs, git-backing) are wanted — and ideally *after* the
> **dynamic-descriptor gap** is closed (so a new axis is a pure plugin, not a core recompile).

## ~~Status: Accepted — built~~ (reverted; see the deferral note above)

## Context

A platform that hosts *code* (pipeline definitions, dbt projects, notebooks) needs a place to
**store and share files** — the v2 portal's file editor + landing zones, reborn. The need:
**a remote, collaborative filesystem** — multiple people/plugins read and write the same paths,
durably, over shared storage.

There is no `fs` axis among the 18, but the axis set is **open** ([ADR-001](001-everything-is-a-plugin.md):
"community may add more `kind:` values without core changes"). A filesystem is a *distinct,
reusable* concern — and modeling it as an **axis** (not a one-off plugin capability) means the
*implementation* is pluggable: an **S3-backed** fs (shared, durable), a **state-backed** fs, or a
**git-backed** fs (real branches/history/merge — the strongest collaboration story) — all behind
the same contract, swappable like every other axis. The user chose this over storing code as flat
`state` KV precisely for that reusability + the git-backed upside.

## Decision

Add the **filesystem axis** (the 19th): a `kind: fs` plugin serving
`rat://fs/v1/{read,write,list,stat,delete}` over a **path → bytes** namespace.

### 1. The contract (`contracts/proto/rat/fs/v1/fs.proto`)

```proto
service FilesystemService {
  rpc Read(ReadRequest)     returns (ReadResponse)   { capability "rat://fs/v1/read";   }
  rpc Write(WriteRequest)   returns (WriteResponse)  { capability "rat://fs/v1/write";  }
  rpc List(ListRequest)     returns (ListResponse)   { capability "rat://fs/v1/list";   }
  rpc Stat(StatRequest)     returns (StatResponse)   { capability "rat://fs/v1/stat";   }
  rpc Delete(DeleteRequest) returns (DeleteResponse) { capability "rat://fs/v1/delete"; }
}
```

`Read`/`Write` carry the **content in the message** (unary). That fits *code files* (small text);
**large blobs** should use the **storage** axis directly (the data plane bypasses the core) — a
streaming `fs` variant is Q01. `List` is prefix-based (a flat path namespace with a `/` convention,
not real inodes); directories/move/copy are Q02.

### 2. The reference: `fs-s3` — built ON the storage axis

The first reference plugin **requires** `rat://storage/v1/vend-credentials-{read,write}` and
reads/writes **S3 objects** at the requested path with the vended creds. So the chain is
`consumer → fs/read → fs-s3 → storage/vend → s3-storage → secret/resolve → keyring|vault` — the
filesystem **composes on top of** the storage + secret axes, exactly "based on storage." It's
collaborative because the objects live in **shared S3** (CAS via S3 ETags is Q03).

### 3. Routing + tooling

The gateway routes `fs/*` once `fsv1` is in `routableDescriptors()` (it learns the capability↔
method binding from the proto annotations). `rat plugin init --kind fs` scaffolds it (added to
`knownKinds`/`kindProvides`), and `rat plugin check` validates `fs` capabilities like any axis.

### 4. Maturity

`fs/v1` is **additive + provisional**, not part of the `rat/2.0` freeze. As a **data-plane** axis,
[ADR-003](003-two-references-before-contract-freeze.md)'s **two-reference rule** applies *before it
is declared frozen*: `fs-s3` now; a second (`fs-state` or `fs-git`) before the freeze (Q04).

## Consequences

**Positive.**
- A real **remote, collaborative code store** — multiple editors/plugins on shared S3 paths.
- The filesystem is **pluggable**: S3 today; git-backed later = branches/history/merge (real VCS
  for collaboration); state-backed for small/embedded — all behind one contract.
- **Reusable** beyond code: the v2 portal file browser, landing zones, applied-project storage all
  become `fs` consumers.
- Composes cleanly on the **storage + secret** axes (no new credential machinery).

**Negative — accepted.**
- A 19th axis = more contract surface (sanctioned by the open-set, but real).
- **Content-in-message** caps practical file size (code is fine; large blobs → storage directly).
  Streaming is Q01.
- The **two-reference rule** (ADR-003) is *owed* before `fs/v1` freezes — one ref (`fs-s3`) now.
- The full read path is a **3-axis consume chain** (`fs → storage → secret`) — more hops; fine for
  control-plane code ops, not for bulk data (which bypasses this entirely).

**Neutral.** Flat path namespace (prefix `List`), not inodes; tenancy-scoping is the plugin's job
(prefix by `identity.tenant`), same rule as storage/secret.

## Open questions

- **Q01 — streaming `Read`/`Write`** for large files (the server-stream Invoke variant, ADR-008).
- **Q02 — directories / move / copy / rename** (v1 is a flat path namespace + prefix `List`).
- **Q03 — concurrency/versioning:** S3 ETag CAS on `Write` (collaborative-edit conflict detection);
  git-backed fs for real history.
- **Q04 — the second reference** (`fs-state` or `fs-git`) to satisfy ADR-003 before `fs/v1` freezes.

## Alternatives considered

- **Store code as flat `state` KV** (no new axis). Rejected by the user for *this*: it's a KV, not a
  filesystem, and not reusable as "the filesystem." (Still valid as an *implementation* — `fs-state`
  is a candidate second reference.)
- **A one-off custom capability** (not an axis). Rejected: a custom capability needs the same gateway
  descriptor wiring as an axis but isn't reusable/swappable — an axis is strictly better.
- **A git-over-S3 service** as the primary model. Deferred: `fs-git` is an *implementation* of this
  axis (Q03), not a separate model.

## Migration

Additive; **Phase 10**: this ADR → `fs/v1` proto + `make gen-sdks` → `fsv1` into
`routableDescriptors()` + `fs` into the scaffold's `knownKinds`/`kindProvides` → the `fs-s3`
reference plugin → prove the `fs → storage → secret` chain. `make breaking` clean (new service).

## Related

- [ADR-001](001-everything-is-a-plugin.md) — the open axis set this extends.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule `fs/v1` owes.
- [storage axis](../../../contracts/proto/rat/storage/v1/storage.proto) — what `fs-s3` builds on.
- [ADR-005](005-capability-invocation-model.md) — the Invoke gateway `fs/*` routes through.
