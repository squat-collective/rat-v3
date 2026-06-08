# Q02 — reviewer brief (SECURITY focus)

> A security-tailored companion to the [main reviewer brief](Q02-external-review-brief.md). It front-loads the **trust model + threat-model questions**; the main brief has the full architecture, the non-security questions, and the logistics. Read this if your lens is security.
> **Confidentiality:** RAT v3 is unpublished and the contract freeze is **local/unpushed**. Please treat everything as confidential and don't redistribute.

## The ask, as a security person

RAT v3's whole bet is *"install many self-describing plugins, including third-party ones."* That makes the platform's security a property of its **trust boundaries and isolation**, not of any single plugin. **Be the adversary:** model a malicious or compromised plugin, a malicious tenant, a leaked ticket/credential, a supply-chain-poisoned plugin image, and a network attacker on the data plane — and tell us where a boundary breaks. All review so far has been internal ([reviews/04-security-reviewer.md](../04-security-reviewer.md), [reviews/board/security.md](board/security.md)), so it shares our blind spots; we want you to find the ones we structurally can't.

## RAT v3 in one security-relevant paragraph

A ~6-thing core **mediates the control plane** — every RPC is capability-authorized + audited and routed by the core. **Bulk data bypasses the core** entirely (Apache Arrow Flight, out-of-band; the core never sees the bytes). Plugins are independent, polyglot, possibly third-party processes the core launches through a **deployment-runtime** under a mandatory isolation profile. Identity (`caller_plugin`, `tenant`) rides every RPC envelope; tenancy + credential-scoping are the multi-tenant boundary. (Full architecture: the main brief + `docs/architecture/overview.md`.)

## What's REAL vs DEFERRED (read before you threat-model)

Phase 1 is sealed (`rat/2.0`) with these **enforced**: capability authz (**C5**), audit-on-every-decision incl. denials + terminal stream-close (**C4**), full-profile container isolation via podman (**D1/I9**), the Arrow bytes-leg ticket (**D2**), storage credential scoping + path containment (**D3**), ed25519 conformance attestation (**D4**).

**Deferred — and load-bearing for your model (please assume these are NOT yet real):**
- **C2 — channel authentication.** Today the core derives `caller_plugin` + `tenant` from the `rat-callmeta-bin` *call envelope* (effectively self-asserted). The planned core re-derives them from the **authenticated channel** (per-plugin token + mTLS-ready). **In the current spike a plugin could forge its caller/tenant.** This is the single biggest thing we want your read on (below).
- **Audit signing + hash chain.** The canonical `AuditRecord` (`common/v1/audit.proto`) has `signature`/`prev_hash`/`key_id` fields, but the spike's gateway records are **unsigned** — tamper-evidence isn't wired yet (the ed25519 in D4 is the seed).
- **Explicit metadata-egress drop** (today it's the container netns) and a **structured `IsolationAttestation`** message (today a JSON receipt).

## The trust model & boundaries

| # | Boundary | Enforced by | What's real | Your job |
|---|---|---|---|---|
| 1 | caller → core (control plane) | gateway: C5 authz + C4 audit + C3 deadline | identity from the **envelope** (C2 deferred) | can a plugin escalate / forge identity / confuse the deputy? |
| 2 | core → provider (downstream hop) | gateway re-stamps identity, propagates `traceparent`, relays **opaque** frames (passthrough codec, never deserializes) | yes | does opaque relay close or hide attack surface (method/payload mismatch)? |
| 3 | plugin process sandbox | deployment-runtime / I9 (podman: non-root, cap-drop ALL, no-new-privs, read-only-fs, seccomp, private netns) | yes (kernel-verified) | is the I9 minimum enough to contain a hostile plugin? |
| 4 | the bytes leg (data plane) | the `ArrowStream` ticket (HMAC, TTL, single-use, `{stream,caller,tenant}`-bound) — the **only** gate; core bypassed | yes (`core/arrowticket`) | forge / replay / leak / confused-deputy / cross-tenant? |
| 5 | credential vending (storage) | short-TTL, tenant+prefix-scoped creds + path containment | yes (`core/.../composition_storagecreds_test.go`) | scope escape / over-vend / blast radius? |
| 6 | tenancy (multi-tenant) | tenant in envelope; per-plugin state-namespacing (deny-by-default) | partial (rides on C2) | cross-tenant data / state / bus / cred leaks? |
| 7 | supply chain | conformance attestation (ed25519, `declared==conformed`) + manifest signing (C8) | attestation real; manifest-sign + audit-sign deferred | forge an attestation / poison an image / compromise the signing authority? |

**Adversaries we care most about, in order:** (1) a malicious/compromised **plugin** (the core threat of the whole bet); (2) a malicious **tenant** on a shared plane; (3) a **leaked ticket/credential**; (4) a **supply-chain-poisoned** plugin image; (5) a **network attacker** on the data plane.

## Threat-model questions (the heart)

**A — Identity & capability enforcement (C5 + the C2 gap).**
- Validate the *planned* C2 design: identity re-derived from an authenticated channel (per-plugin token, mTLS-ready) instead of the envelope. Is that the right model? Does anything in the current design **secretly assume unforgeable identity** in a way that C2 won't cleanly retrofit?
- C5 = `caller.requires ∧ provider.provides`. Can a plugin invoke an **undeclared** capability (escalation)? The gateway **re-stamps identity downstream** — is there a **confused-deputy** path (induce the core to act with authority the caller lacks)?
- The core relays **opaque** frames and never deserializes payloads. Does that genuinely shrink the core's attack surface, or does "core can't see the payload" enable a method/payload mismatch (a declared capability invoked with a payload meant for a different, undeclared method)?

**B — Plugin sandbox / isolation (I9).**
- Threat: a hostile third-party plugin. Is the **I9 minimum** (non-root, cap-drop ALL, no-new-privs, read-only-fs, seccomp *default*, private netns) sufficient containment? What's missing for hostile workloads — user namespaces, a tighter seccomp profile, `/proc` masking, **resource-limit/DoS** enforcement, the writable `/tmp` + persistent `/data`?
- The runtime **refuses below** the I9 minimum. Is "refuse below minimum" the right gate, or should the runtime *impose* the max? Can a plugin influence its own `LaunchSpec` to **request weaker isolation**?
- **metadata-egress:** today blocked only by the rootless netns (explicit drop deferred). On a real cloud host, is the netns alone enough, or is the `169.254.169.254` SSRF / credential-theft path effectively open?

**C — The bytes leg (bypasses the core).**
- The `ArrowStream` ticket (HMAC-SHA256, TTL, single-use, `{stream,caller,tenant}`-bound) is the **only** gate on bulk bytes. Attack it: **key management** (who holds the HMAC key, rotation, per-producer vs shared?), **replay** (single-use is producer-enforced in-memory — does it survive a producer restart or multiple producer replicas?), **leakage** (TTL-window blast radius), **confused-deputy** (a leaked ticket presented from a *different* authenticated connection — this leans on C2), **cross-tenant** byte access.
- Making **each producer** the enforcement point (no core backstop on the bytes): sound, or a footgun where one buggy producer = a silent data breach?

**D — Credential vending & tenancy (C7).**
- `VendCredentials` issues short-TTL, tenant+prefix-scoped creds; the tenant comes from the (C2-deferred) envelope; path-containment refuses escaping prefixes. Attack: **scope escape** (prefix tricks beyond `..`/symlinks), **over-vending** (mode/prefix broadening), and the **blast radius** of a vended cred (it's an opaque provider credential — the core can't constrain it after vending).
- **Cross-tenant:** is the tenancy model (tenant-in-envelope + per-plugin/tenant scoping + deny-by-default state namespacing) airtight, or are there shared-substrate leaks (state keys, event-bus subjects, the shared deployment-runtime/host)?

**E — Supply chain & audit integrity.**
- The conformance **attestation** is ed25519-signed with a `key_id` keyring (rotation/agility). Validate: the **authority** trust model (who signs attestations — can that authority be compromised? what's the revocation story?), and the gap that the **audit chain is unsigned** in the spike (the planned core-signed, hash-chained, append-only audit — even with `audit-log: none` — is the seed). What's the **minimum signing** that must ship before multi-tenant?

## Already acknowledged (don't flag as novel)

C2 channel auth (the deferral above); unsigned audit records (signing seeded by D4); metadata-egress explicit drop; structured `IsolationAttestation`; write-leg idempotency vs a real format ref (a correctness, not security, residual).

## Materials & reading order (security-relevant)

1. This brief + the trust-model table above.
2. [reviews/04-security-reviewer.md](../04-security-reviewer.md) + [reviews/board/security.md](board/security.md) — the internal security review (challenge its conclusions).
3. `docs/architecture/overview.md` §"data plane bypasses core" + §reconciliation; [ADR-001](../../docs/architecture/adrs/001-everything-is-a-plugin.md) (tier-0).
4. Contracts: `common/v1/data.proto` (`ArrowStream.ticket`, SEC-14) · `common/v1/audit.proto` (signing/hash-chain) · `storage/v1` (C7 vend-credentials) · `deploymentruntime/v1` (the I9 `IsolationProfile`).
5. The enforcement, in `core/`: `gateway/gateway.go` (C5/C4/C3 + the passthrough relay + the C2-deferred note) · `deploymentruntime/podman.go` (I9 enforcement) · `arrowticket/` (the ticket) · `conformance/attestation.go` (ed25519) · `composition/composition_storagecreds_test.go` (cred scoping).

## Findings & logistics

Same format + logistics as the [main brief](Q02-external-review-brief.md#how-to-deliver-findings): per-finding {severity · area · finding · why-it-matters · suggested-direction}, plus a bottom line — *would you run this multi-tenant, and what's the one boundary you'd fix first?* A **Critical** = "I would not run untrusted plugins / multiple tenants until this is resolved." A focused 1–2 day read is plenty; unpublished + confidential.
