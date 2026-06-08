# Board review — SECURITY & TRUST (post-freeze, `rat/1.4`)

> Reviewer lens: security & trust. Scope: the now-frozen 18-axis contract surface + 32
> reference plugins. Method: claims ground-truthed against the actual proto/vector/ref
> files (cited `file:line`). Tags: **[V2-REGRET]** frozen-shape flaw needing a v2;
> **[ADDITIVE]** fixable without a wire break; **[PROCESS]** conformance/trust-root gap,
> no wire change. Prior pass: [reviews/04](../../04-security-reviewer.md) + the freeze
> residuals R1–R3 ([reviews/07](../../07-freeze-review.md)); not repeated unless the freeze
> changed the picture.
>
> **Headline:** the frozen *wire shapes* are defensively sound — opaque `ticket` bytes,
> `signature`+`key_id`, free-form attestation `detail` are all future-proof, and I found
> **no hard [V2-REGRET]**. The danger is the opposite: the freeze + "`make conformance`
> 20/20" badge **overstates the security guarantee**, because the three most load-bearing
> trust boundaries (plugin isolation, the bytes-leg ticket, storage cred scoping) are
> **honor-system properties that conformance does not actually test**. A plugin can be
> 20/20-conformant and enforce none of them.

---

## Findings (ranked)

### 1. [HIGH] [PROCESS] The I9 isolation gate is real; I9 *enforcement* is honor-system, and conformance can't tell the difference
**Evidence.** The gate IS real and identical across runtimes: `check_spec` refuses to
launch below the I9 minimum
([local-process-py/store.py:44-53](../../../plugins/deploymentruntime/local-process-py/store.py#L44),
[k8s-dryrun-py/store.py:41-51](../../../plugins/deploymentruntime/k8s-dryrun-py/store.py#L41)),
and the error vector asserts `FAILED_PRECONDITION`
([deploymentruntime-v1.json](../../../contracts/conformance/deploymentruntime-v1.json) `below_i9_minimum`).
That part holds.

But the *enforcement receipt is a self-asserted echo of the request*, not evidence of
anything applied. `honored()` returns `iso.read_only_root_fs` / `iso.block_metadata_egress`
**verbatim from the spec**
([local-process-py/store.py:32-41](../../../plugins/deploymentruntime/local-process-py/store.py#L32)),
while the actual "launch" is a bare `subprocess.Popen([...sleep 300])`
([store.py:69](../../../plugins/deploymentruntime/local-process-py/store.py#L69)) — **no
namespaces, no seccomp, no cap-drop, no read-only mount, no egress filtering**. So a
local-process runtime reports `read_only_root_fs: true` "honored" while enforcing nothing.
Worse, the conformance vector only checks the **three gate bools**
(`expect_isolation_honored = {run_as_non_root, drop_all_capabilities, no_new_privileges}`)
and never `read_only_root_fs` / `block_metadata_egress`
([deploymentruntime-v1.json](../../../contracts/conformance/deploymentruntime-v1.json)).

**Answer to the board question:** *Yes — the contract lets a runtime claim conformance
while enforcing nothing.* The trust boundary the "install many 3rd-party plugins" bet
leans on (reviews/01 F6, the proto SECURITY block, deployment_runtime.proto:14-19) is, at
v1, a **promise the harness does not verify**. Only k8s-dryrun maps the profile to a
real `securityContext` ([k8s-dryrun-py/store.py:54-63](../../../plugins/deploymentruntime/k8s-dryrun-py/store.py#L54)),
and even that is dry-run (no admission actually enforces it).

**Recommendation.** (a) Document loudly that I9 is a *gate + attestation*, not a
verified-enforcement guarantee, in the deployment-runtime `CONTRACT.md`. (b) Ship a
conformance vector that asserts the *full* profile is honored AND a reference that
genuinely enforces (a real podman/container runtime, not dry-run) — additive, no wire
change. (c) Consider an additive structured `IsolationAttestation` instead of stuffing
the receipt into the free-form `HealthcheckResponse.detail` string
([deployment_runtime.proto:103-105](../../../contracts/proto/rat/deploymentruntime/v1/deployment_runtime.proto#L103)),
so a reconciler can machine-verify rather than parse self-authored JSON.

### 2. [HIGH] [PROCESS]/[ADDITIVE] The ArrowStream ticket is the *only* gate on the core-bypassing bytes leg, and its security is prose-only + unconformanced
**Evidence.** The bytes leg bypasses the core by design (ADR-005 §33;
[invoke.proto:28-31](../../../contracts/proto/rat/core/v1/invoke.proto#L28)), so the ticket
is the sole authorization gate. The contract *says* the right thing — "a conformant
producer MUST issue tickets that are short-TTL, single-use, and bound to
{caller_plugin, tenant, this stream}"
([data.proto:56-62](../../../contracts/proto/rat/common/v1/data.proto#L56)) — but immediately
punts: *"The detailed ticket-format spec is enforcement-layer (GA)."* The frozen wire is
only `bytes ticket` (opaque, `debug_redact`). **No conformance vector exercises ticket
TTL, single-use, or binding** (I asked `contracts` to confirm; nothing in
`contracts/conformance/` tests it). Two impls can both be 20/20-conformant while issuing
guessable, non-expiring, replayable tickets — and a leaked ticket then yields cross-tenant
bulk-data read with the core entirely out of the loop.

**Assessment.** The *shape* is fine to freeze — opaque `bytes` can carry any future format,
so this is **not** a [V2-REGRET]. The gap is purely that the security property is
unconformanced prose, so the ecosystem has no forcing function and a weak-ticket norm can
calcify. Strong enough to *freeze the field*, not strong enough to *claim the guarantee*.

**Recommendation.** Add a gateway/stream conformance vector that asserts TTL expiry +
single-use rejection + cross-tenant ticket rejection against a reference Flight producer
(parquet-py already produces real Flight). Additive. Until then, mark the bytes-path
tenant-isolation guarantee as "impl-asserted, untested at v1" in `data.proto` + storage
`CONTRACT.md`.

### 3. [MEDIUM] [PROCESS] The whole keystone rests on authenticated transport (mTLS) that no frozen artifact mandates
**Evidence.** `context.proto` is explicit that the integrity of the two *unsigned*
principals — `caller_plugin` (re-derived per hop) and `tenant` — "rests on AUTHENTICATED
TRANSPORT (C2: mTLS / per-plugin token on the core↔plugin channel). On an unauthenticated
channel they are forgeable — so a non-mTLS transport is out of contract for any
multi-tenant deployment"
([context.proto:147-153](../../../contracts/proto/rat/common/v1/context.proto#L147)).
"Out of contract" is **prose**. `UNAUTHENTICATED` in the error model is about *presence of
a credential* the gateway checks ([ERROR_MODEL.md:46](../../../contracts/proto/rat/common/v1/ERROR_MODEL.md#L46)),
not a structural requirement that the channel be *mutually* authenticated. Nothing
freezable in a `.proto` can mandate mTLS — it is inherently a deployment property — so the
three-principal model's bottom turtle is a deployment-time honor-system assumption.

**Assessment.** Inherent and correctly documented; I flag it because the keystone's marketing
("never trust the wire copy") is only true *given* C2, and C2 is the part most likely to be
mis-deployed (a solo→multi-tenant upgrade that forgets to turn on mTLS silently makes
`tenant` forgeable). `architect` confirmed (and sharpened): transport authentication is
**structurally inexpressible in proto3** — no field/RPC can mandate "this channel is mTLS" —
so this could *never* be a frozen wire MUST; prose + enforcement-layer is the strongest the
contract can be. The identity axis contracts token *issuance* (identity.proto:14,
"per-plugin token at registration"), but binding token→transport→the unsigned principals is
enforcement-layer. So the gap is real and irreducible at the wire; only conformance can close it.

**Recommendation.** Make C2 a named, testable **deployment conformance** item (a
"multi-tenant profile requires mutual auth on the core↔plugin channel" check), and have the
core refuse to enter multi-tenant mode on a non-authenticated transport. Not a wire change.

### 4. [MEDIUM] [PROCESS] Audit chain defeats forge + reorder, but *drop* (tail-truncation) at the sink is not cryptographically prevented; key-ring trust-root is unspecified
**Evidence.** Forge: the sink holds only the core's **public** key
([store.py:17-19,44-56](../../../plugins/auditlog/inmemory-py/store.py#L43)) — it can verify,
never forge. Reorder/mid-gap: `prev_hash` must equal the chain head or the record (and the
prefix after it) is REJECTED
([store.py:76-80](../../../plugins/auditlog/inmemory-py/store.py#L76)), and `key_id` is inside
the signed canonical bytes, defeating key-substitution
([audit.proto:49-50,79](../../../contracts/proto/rat/common/v1/audit.proto#L49)). Strong.

The residual is **drop**: a malicious/faulty *sink* can truncate the tail of the chain and
what remains is still internally valid — `prev_hash` continuity only detects a gap *between
records you hold*, not records the sink silently never reveals. The design's actual defense
is out-of-band: "Records are also retained core-locally, so the sink is a fan-out, not the
only copy" ([auditlog.proto:65-67](../../../contracts/proto/rat/auditlog/v1/auditlog.proto#L65)),
reconciled via the `last_committed_id`/`hash` watermark
([auditlog.proto:114-119](../../../contracts/proto/rat/auditlog/v1/auditlog.proto#L107)). So
**drop-detection depends on the core-local copy + watermark reconciliation, not on the
sink** — acceptable, but it means "tamper-*evident*" is only true when someone compares the
sink against the core copy; the sink alone cannot prove completeness. Separately, the
**trust root of the key-ring is unspecified**: verification picks a key by `key_id` from
"the core's *published* keyring"
([context.proto:131-133](../../../contracts/proto/rat/common/v1/context.proto#L131),
[audit.proto:72-78](../../../contracts/proto/rat/common/v1/audit.proto#L72)) — whoever can
publish a key into that ring can mint records/assertions that verify. How the ring is
distributed and pinned is prose-absent.

**Recommendation.** (a) State explicitly that drop-detection is a core-copy-vs-sink
reconciliation obligation (and ideally periodically emit a signed "chain head = id N"
checkpoint so a sink-only auditor can detect truncation). (b) Specify the keyring's
trust-root / distribution + rotation-revocation as a frozen-adjacent doc, since the entire
audit + SubjectAssertion trust collapses to it.

### 5. [MEDIUM] [ADDITIVE] Storage `VendCredentials` tenant-scoping (R2) is tested only against a JSON stand-in, not the real opaque credential
**Evidence.** R2 is correctly accepted as a residual, but worth re-stating its blast radius:
the core can't inspect the opaque STS blob (the one acknowledged direct-dial bearer
exception, ADR-005), so C7 for the bytes leg "reduces to a per-impl property the conformance
vectors test via a stand-in 'scope receipt'"
([reviews/07 R2](../../07-freeze-review.md), storage
[CONTRACT.md](../../../contracts/proto/rat/storage/v1/CONTRACT.md) "Conformance obligations" §3:
*"The conformance 'scope receipt' is a JSON stand-in for an opaque STS token so the harness
can assert the binding"*). A production plugin can therefore mint **over-broad real creds**
(wrong tenant prefix, too-long TTL) and still pass conformance, because the harness only
sees the receipt the plugin chose to emit — not the token it actually vended. This is the
**second** honor-system trust point on the same core-bypassing bytes path (with the
ArrowStream ticket, finding 2): together they mean the bulk-data plane's cross-tenant
isolation is, at v1, entirely impl-asserted.

**Assessment.** Inherent to the bytes-bypass exception; accept for v1 as documented. But the
*pair* (ticket + cred) deserves a combined "bytes-path isolation is impl-trusted" callout —
right now each is footnoted separately and the aggregate risk is under-stated.

**Recommendation.** Where feasible, add an integration conformance that vends a *real*
scoped credential against a local-fs/minio backend and proves an out-of-prefix / cross-tenant
read is actually refused by the backend (localfs-go already does path containment) — closes
the gap between "receipt says scoped" and "backend enforces scoped."

### 6. [LOW-MEDIUM] [ADDITIVE] `SubjectAssertion` bound to the operation, not the capability/hop (R1) — bounded confused-deputy, accept v1
**Evidence.** The assertion binds to `bound_correlation_id` only
([context.proto:159-166](../../../contracts/proto/rat/common/v1/context.proto#L159)); within one
operation any plugin holding it can present it to **any capability it already `requires`**
under the user's authority. Bounded by C5 (manifest `requires` is the blast radius), as R1
states. Confirmed the signature *does* cover `tenant` + the M4 bare-mirror cross-check is now
mandated (verification steps 1-4,
[context.proto:129-143](../../../contracts/proto/rat/common/v1/context.proto#L129)) — so the
"tenant unsigned" framing from earlier passes is genuinely fixed.

**Assessment.** The residual confused-deputy is real but bounded and acceptable for v1.
Tightening to per-hop/per-capability binding needs a new `bound_capability` field — **additive,
not a wire break** — so deferring is safe. Note the field count: `SubjectAssertion` has room to
add this later without breaking `rat/1`.

### 7. [LOW-MEDIUM] [ADDITIVE]/[PROCESS] Secret anti-enumeration is airtight at the response/data layer but says nothing about timing/error-path side-channels
**Evidence.** The ref is airtight *at the data layer*: `(tenant, secret_ref)` is the dict key,
so a foreign-tenant ref and a nonexistent ref both fall to the identical `(False, b"", 0)`
branch ([secret/inmemory-py/store.py:27-38](../../../plugins/secret/inmemory-py/store.py#L27)),
and the error model *forbids* `PERMISSION_DENIED` here, mandating the collapse to
`found=false`+`OK` ([ERROR_MODEL.md:72-79](../../../contracts/proto/rat/common/v1/ERROR_MODEL.md#L72)).
The response-shape defense is correct and well-specified.

But anti-enumeration via *response equality* is necessary, not sufficient. A real backend
where "exists-but-forbidden" takes a different code path (extra ACL lookup, a DB round-trip,
a decrypt attempt) than "absent" leaks the distinction via **latency / error timing** even
when the response body is byte-identical. The contract pins the response collapse and is
silent on timing-equivalence.

**Recommendation.** Add a one-line conformance note that the not-found/forbidden collapse must
be **timing-indistinguishable** (constant-work resolution), and flag it as a known residual for
network-backed secret plugins. Cheap, additive doc.

### 8. [MEDIUM] [ADDITIVE] No contractual *terminal* audit record — streams audit at open only; unary completion-outcome is unpinned (raised by `sre`)
**Evidence.** Streaming Invoke emits "one C8 audit record per stream... at open"
([invoke.proto:53-55](../../../contracts/proto/rat/core/v1/invoke.proto#L53)) — so a stream that
dies mid-relay after open produces **only the open-time record**; there is no contractual
close/terminal event. For unary, the C8 record is pinned at the *enforcement decision*
([invoke.proto:18-20](../../../contracts/proto/rat/core/v1/invoke.proto#L18); audit.proto
AUDIT-ON-DENY), but the contract does **not** state the record is written at *completion*
carrying the terminal outcome — an impl that audits at decision-time (allow) then sees the
provider die can leave a "started/allowed" record with no terminal-failure record.

**Why it's a security/trust issue, not just ops:** an audit trail that records "access was
authorized" but not "and here is how it ended" cannot support incident reconstruction — a
provider crash, hang, or partial cross-tenant stream read leaves no terminal evidence.
`AUDIT_OUTCOME_ERROR` already exists ([audit.proto:36](../../../contracts/proto/rat/common/v1/audit.proto#L36)),
so this is **additive** (no wire change).

**Recommendation.** Pin as a C8 conformance obligation: "every call emits one record at the
enforcement decision; streams additionally emit a terminal close record with
`outcome ∈ {SUCCESS, ERROR, DENIED}`." Credit: `sre` operability consult.

---

## Cross-consults dispatched (non-blocking)
- **→ `contracts`:** is the ArrowStream ticket TTL/single-use/binding pinned in the frozen
  wire or prose-only? (finding 2)
- **← `architect`:** confirmed (finding 3) — C2 transport-auth is structurally inexpressible in
  proto3, so prose + enforcement-layer is the strongest the contract can be; only a gateway
  conformance vector ("unauthenticated core↔plugin channel REFUSED in multi-tenant mode") makes
  it testable. Aligns with my finding-3 recommendation.
- **← `sre`** (audit emission when a plugin crashes mid-call): answered → promoted to **finding
  8**. Unary completion-outcome is unpinned; streams audit at open only (no terminal record).
  Both additive to fix.

---

**Biggest concern:** the `rat/1` freeze + "20/20 conformance" badge **overstates the security
guarantee** — the three load-bearing trust boundaries (I9 plugin isolation, the ArrowStream
ticket, and storage credential scoping) are honor-system properties conformance never tests, so
a fully "conformant" plugin set can enforce none of them; the frozen *shapes* are fine, but the
*guarantee* must be re-labeled "impl-asserted, untested" until enforcement-level conformance lands.
