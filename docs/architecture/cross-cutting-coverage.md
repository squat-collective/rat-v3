# Cross-cutting concern coverage (sub-phase 0c)

> Phase 0 sub-phase **0c** = "finalize the cross-cutting concern protos." This doc is
> the finalize artifact: it maps every cross-cutting concern (the synthesis C1–C10 +
> ARCH findings, and the [plugin-architecture.md](../../.claude/rules/plugin-architecture.md)
> cross-cutting list) to its **wire home** (a proto type) or its **non-wire mechanism**
> (transport / native / manifest), and confirms each is covered before the `rat/1`
> freeze. The principle: a cross-cutting concern enforced by the CORE must not live in
> an axis proto.

## The cross-cutting proto set (`common/v1/` + `core/v1/`)

Five shared types in `common/v1/` + the invoke service in `core/v1/` carry every
cross-cutting concern that has a wire shape:

| Proto | Types | Concern |
|---|---|---|
| [`common/v1/context.proto`](../../contracts/proto/rat/common/v1/context.proto) | `RequestContext`, `TraceContext`, `Identity`, `SubjectAssertion` | C1 trace, C2 identity, C3/C7 isolation principals |
| [`common/v1/data.proto`](../../contracts/proto/rat/common/v1/data.proto) | `ArrowStream`, `TableRef`, `WriteResult` | the out-of-band bulk-data leg (bytes bypass the core) |
| [`common/v1/annotations.proto`](../../contracts/proto/rat/common/v1/annotations.proto) | `(capability)` method option | C5 capability⇄method binding |
| [`common/v1/event.proto`](../../contracts/proto/rat/common/v1/event.proto) | `Event` | ARCH-1 async plane envelope |
| [`common/v1/audit.proto`](../../contracts/proto/rat/common/v1/audit.proto) | `AuditRecord`, `AuditOutcome` | C8 mandatory audit envelope **(moved here in 0c — see below)** |
| [`core/v1/invoke.proto`](../../contracts/proto/rat/core/v1/invoke.proto) | `CapabilityInvokeService` (`Invoke` + `InvokeServerStream` + `InvokeBidiStream`) | C5/C2/C7/C3/C8 enforcement point; the core-mediated call path |

## Coverage matrix

| # | Concern | Home | Mechanism |
|---|---|---|---|
| C1 | Trace-context propagation (`traceparent` mandatory) | `context.proto` `TraceContext` | carried in `rat-callmeta-bin` metadata (ADR-007) |
| C2 | Plugin-to-core authentication | `context.proto` `Identity` / `SubjectAssertion` (the *assertion*) | the credential itself is **transport** (mTLS / per-plugin token); not a message |
| C3 | State-gateway per-plugin isolation | `context.proto` `Identity.caller_plugin` + `state.proto` key namespacing | server-derived per hop; deny-by-default |
| C4 | Resource limits | [`schema/plugin.v1.json`](../../contracts/schema/plugin.v1.json) | **manifest** (JSON Schema), not a wire type (0a) |
| C5 | Capability enforcement at runtime | `annotations.proto` `(capability)` + `invoke.proto` | the gateway checks `requires`/`provides` per call (ADR-005) |
| C6 | Conformance (declared = conformed) | [`contracts/conformance/`](../../contracts/conformance/) + `make conformance` | golden vectors + the suite runner (0f), not a wire type |
| C7 | Tenancy as structural isolation | `context.proto` `Identity.tenant` | server-stamped, never caller-writable |
| C8 | Mandatory audit emission | **`audit.proto` `AuditRecord`** (core-authored/signed) + `auditlog.proto` (sink) | emitted even with no audit-log plugin |
| C9 | Two references before freeze | [ADR-003](adrs/003-two-references-before-contract-freeze.md) | process gate, not a wire type |
| C10 | API-gateway hardening | `invoke.proto` + the gateway impl | single entry point; the only coupling is by capability |
| ARCH-1 | Async event plane | `event.proto` `Event` | same `RequestContext` envelope as sync (in-body, per the async exception) |

**Every cross-cutting concern is covered** — either by a `common/v1` (or `core/v1`)
wire type, or by a deliberately non-wire mechanism (transport credential, manifest
schema, process gate, conformance suite). No concern is homeless, and no
core-enforced concern lives in an axis proto.

## The one 0c change: relocate the audit envelope

`AuditRecord` + `AuditOutcome` were in the **`rat.auditlog.v1` axis** proto — but the
audit record is **core-authored, core-signed, and emitted even when no audit-log
plugin is installed** (C8; the proto's own header says "this axis is only the export
sink"). A core concern living in an axis proto means the core's C8 emission would
import an axis contract — a layering inversion.

**0c moves `AuditRecord` + `AuditOutcome` to [`common/v1/audit.proto`](../../contracts/proto/rat/common/v1/audit.proto)**;
`auditlog.proto` now `import`s it and `AppendRequest.records` references
`common.v1.AuditRecord`. The move is **wire-compatible** — field numbers are
unchanged, so the canonical serialization + Ed25519 signatures + hash chain are
byte-identical; only the proto package (and the generated type name) changes
`rat.auditlog.v1` → `rat.common.v1`. (`buf breaking` flags it; allowed in
`v1-preview`. SDKs regenerated.)

## What "descriptors" meant (the other 0c ⬜ in the phase note)

The plan's 0c note flagged "audit envelope, descriptors ⬜". The **audit envelope** is
addressed above. **Descriptors** — a plugin's self-description — is the **manifest**
([`plugin.v1.json`](../../contracts/schema/plugin.v1.json), 0a) the registry indexes
by `(kind, name, version)`, plus the proto service descriptors the gateway already
reads for capability routing (the `(capability)` annotation). No additional descriptor
proto is needed; both are done.

## Conclusion

**Sub-phase 0c is complete.** The cross-cutting proto set is `common/v1/{context,
data, annotations, event, audit}` + `core/v1/invoke`, with `auditlog.proto`
demoted to a pure sink axis. Every C1–C10 + ARCH concern has a documented home.
Remaining toward freeze: **0h** (peer review + the `rat/1` freeze).
