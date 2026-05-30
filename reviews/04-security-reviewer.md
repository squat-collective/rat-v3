# Security review — RAT v3

*Reviewer role: security / threat-modeling. Scope: the architecture as written in ADR-001, ADR-002, `docs/vision.md`, `docs/architecture/overview.md`, and the two 2026-05-30 conversations. Cross-referenced against v2's ADR-017/019/020, which already solved several of the problems v3 has un-solved.*

---

## Headline

RAT v3's trust story today is a single sentence buried in `ideas/inbox.md`: *"The core doesn't enforce sandboxing — the deployment-runtime does."* That is not a security model — it is the **absence** of one, deferred to a plugin axis that has no contract yet. The highest-priority gap is that **the entire security posture has been delegated to plugins (sandboxing → deployment-runtime, isolation → tenancy, authn → identity, secrets → secret-backend, audit → audit-log) while the core — the one component that is supposedly trustworthy and small — defines no trust boundary, no plugin-to-core authentication, no capability enforcement, and no audit obligation.** "Everything is a plugin" has quietly become "every security control is optional," and on a platform whose headline feature is *"anyone can publish a plugin and an operator installs it,"* that is the dominant risk.

Concretely: v2 already shipped ADR-019 (internal listener split), ADR-020 (per-plugin platform token, with a constant-time-compare CVE-class fix), and ADR-017 (blocklist-is-not-a-boundary). **v3 inherits none of these as contracts.** The lessons exist on disk one directory over and have not made it into a single v3 ADR. That regression is the through-line of this review.

---

## What's strong security-wise

Calibration first — several decisions genuinely *help* security, and a smaller attack surface is a real asset.

1. **A 5–10k-LOC core is auditable.** ADR-001's size discipline is the single best security property here. A control plane one person can read in a day has a tractable attack surface and a realistic path to a formal audit / fuzzing campaign. Most data platforms cannot say this. Keep this religiously.
2. **Data plane bypasses the core (vision §"Data plane bypasses the control plane").** Bytes never transit the core, so the core is not a data-exfiltration chokepoint and a core compromise does not directly leak table contents. This shrinks the blast radius of the most-audited component. (It *enlarges* the blast radius of storage credential vending — see DP-1 below — but the split itself is sound.)
3. **Reconciliation over imperative orchestration (overview §"reconciliation model").** Desired-state convergence with the reconciler as source-of-truth (inbox "event bus failure modes" entry) means forged/lost/replayed *events* degrade liveness but should not, by design, corrupt state — the loop re-reads truth every iteration. This is a strong anti-tampering primitive **if** write-access to desired state is controlled (it currently is not — see T-1).
4. **Manifest-in-image with operator override (ADR-002 D6).** Coupling the manifest to the image it describes (OCI label / `/manifest.yaml`) closes a class of "manifest says one thing, image does another" confusion attacks, and the operator-override path supports air-gapped/compliance review. This is a good substrate for signing — it just isn't signed yet.
5. **NATS JetStream as the bus (ADR-002 D2)** is a mature substrate with native authn/authz, per-subject permissions, and accounts/multi-tenancy. The *capability* to isolate publishers/subscribers exists in the chosen technology; v3 just hasn't specified using it (see S-2 / EoP-2).

These are real. The critique below is not "the design is careless" — it's "the design has deferred every adversarial question to a future ADR, and the deferrals collide."

---

## STRIDE table

| Threat | Coverage in ADRs | Gap | Severity |
|---|---|---|---|
| **Spoofing** | Identity gateway delegates to identity plugin; "anonymous-root if none" (ADR-001 §core-2). Q13 (plugin↔core auth) **deferred**. | No plugin-to-core authentication. Any process that can reach the core API or the bus can *claim* to be `rat-plugin-secrets` or emit events "from" another plugin. v2 solved exactly this with ADR-020 platform tokens; v3 dropped it. Default `anonymous-root` = unauthenticated superuser. | **Critical** |
| **Tampering** | Reconciler-as-source-of-truth resists event tampering (inbox entry). Manifest-in-image resists manifest/image drift (D6). | No control on **who may write desired state** (planes, pipelines, subscriptions, bindings). No manifest signing / provenance. No integrity on state-backend reads. A plugin with state-gateway access can rewrite another plugin's desired state or its config. | **Critical** |
| **Repudiation** | `audit-log` listed as a plugin axis (ADR-001). | Audit is **optional and pluggable, including `kind: audit-log → none`** (overview axis list). No core-mandated, tamper-evident record of installs, binding changes, credential vends, or config writes. Q14/Q15 mention sandboxing, never audit. Nothing is non-repudiable by default. | **High** |
| **Information disclosure** | Secret-backend axis; storage vends scoped credentials (`VendCredentials(prefix)`). Data-plane-bypasses-core limits core exposure. | No isolation between plugins at the state gateway — `rat-plugin-evil` can `Get` `rat-plugin-secrets`' keys/config. No stated scoping on `VendCredentials` (prefix is an argument the caller chooses). Secrets in events/logs/manifests unaddressed. No tenant data isolation contract. | **Critical** |
| **Denial of service** | Event-bus scaling noted as a risk (ADR-001 neg-4: "~10k events/sec"). Leader+lease for reconciler HA (D5). | No rate limiting, quota, or resource-ask enforcement on plugins. No protection against a malicious plugin flooding the bus, the state gateway, or the reconcile queue. API gateway has no documented rate-limit/CORS posture. A single plugin can starve the plane. | **High** |
| **Elevation of privilege** | Capability URIs are namespaced + versioned (D4). `requires`/`provides` in manifest. | Capabilities are **declarative, not enforced** — nothing stops a plugin from calling capabilities it didn't declare. No sandbox by default (solo = in-process = full host trust, per inbox). Container escape (CVE-2019-5736) → host. A malicious plugin = code execution inside the trust boundary with the core's authority. | **Critical** |

---

## Concrete threat scenarios

Written from the attacker's chair. Preconditions are realistic for a marketplace-default platform.

### TS-1 — Marketplace plugin exfiltrates the warehouse *(Information disclosure / EoP)* — **Critical**
**Attacker capabilities:** Publishes a useful-looking `rat-format-parquet-fast` to the default community marketplace (ADR-002 D9 puts a community marketplace in the *default solo/team bundle*). No signing required (no signing ADR exists). Wins installs on utility.
**Attack steps:**
1. Manifest declares `provides: format`, `requires: storage-capability`. All legitimate.
2. On first `format.Write`, the plugin calls `storage.VendCredentials(prefix="")` — or any prefix it likes; the contract (`storage/v1.VendCredentials`) takes the prefix from the *caller*, with no documented authorization that the caller is entitled to that prefix.
3. It receives real S3 credentials and, on its own data-plane connection (which by design bypasses the core, so the core never sees it), copies every object to an attacker bucket.
4. Optionally it also reads `rat-plugin-secrets`' stored config via the shared state gateway (see TS-3).
**Impact:** Full warehouse exfiltration + cloud credential theft. Blast radius = whatever the vended credentials can touch.
**Defenses missing:** plugin signing/provenance (Sigstore/cosign); `VendCredentials` authorization scoped to the *plugin's declared + bound* storage prefix, not a caller argument; capability enforcement; egress network policy; marketplace review/trust badges. This is the **npm dependency-confusion / VSCode-extension-malware pattern** (e.g. the 2021 `ua-parser-js` hijack; the 2023 malicious VSCode "Theme Darcula dark" / `prettiest` typosquats that stole tokens) applied to data infra, where the payoff is the entire data lake.

### TS-2 — Plugin impersonation against the core *(Spoofing)* — **Critical**
**Attacker capabilities:** Code execution in any container on the plugin network (a second compromised plugin, or SSRF from a benign one). This is the *exact* threat v2's ADR-019 names.
**Attack steps:**
1. Q13 is unresolved → core↔plugin calls have no mutual auth in the architecture. The attacker opens a gRPC connection to the core API/state gateway and identifies as `rat-plugin-secrets`.
2. With no `platform_token`-equivalent (v2 ADR-020) and `anonymous-root` default identity, the core accepts it.
3. The attacker calls state-gateway `Get` for the secrets plugin's keys, or `Put`s a poisoned manifest/binding.
**Impact:** Full impersonation of the most privileged plugin; secret theft; desired-state poisoning.
**Defenses missing:** plugin-to-core authentication (mTLS or per-plugin token — v2 *already built this in ADR-020 and even hardened it with `crypto/subtle.ConstantTimeCompare` after realizing a naive `!=` leaked the token in ~2²⁰ requests*). v3 must not regress below v2's shipped posture. Also: the internal-vs-public listener split (v2 ADR-019) has no v3 equivalent — the API gateway is described as a single public entry point.

### TS-3 — Cross-plugin state theft *(Information disclosure)* — **Critical**
**Attacker capabilities:** Any installed plugin with state-gateway access (i.e., effectively all of them — the state gateway is a core primitive every plugin uses).
**Attack steps:**
1. State gateway exposes `Get/Put/Watch/List` (overview §core-3). No ADR describes per-plugin namespacing or ACLs on it.
2. `rat-plugin-evil` calls `List` then `Get` on keys owned by `rat-plugin-secrets`, `rat-tenancy-*`, or `rat-billing-*`.
3. `Watch` gives it a live feed of every config/state change platform-wide.
**Impact:** Reads encrypted-vault config, tenant boundaries, billing data; can `Put` to corrupt them.
**Defenses missing:** state-gateway isolation — per-plugin key prefixes enforced by the *gateway* (not by plugin goodwill), capability-checked `Get/Put`, and a deny-by-default on cross-plugin reads. This is the v2 lesson from the secrets plugin generalized: a shared KV with no tenancy is a shared secret store.

### TS-4 — Event forgery / malicious trigger *(Spoofing / Tampering)* — **High**
**Attacker capabilities:** Any plugin that can publish to the bus (all of them; the bus is how plugins coordinate).
**Attack steps:**
1. No event authenticity is specified. Attacker publishes `pipeline_run_requested` for a plane/pipeline it doesn't own, or `plugin_installed`/`plane_warmed` to drive reconciler behavior.
2. It forges `*_completed`/`*_failed` to mislead observability and notifications, or to mark a malicious run "succeeded" so quality gates are skipped.
3. It subscribes to *all* subjects (no subject-level ACL specified) and harvests pipeline metadata, table names, run payloads.
**Impact:** Unauthorized pipeline execution, audit/observability poisoning, metadata disclosure. Reconciler-as-truth blunts state corruption, but triggering work and lying about results is enough.
**Defenses missing:** signed/authenticated events (publisher identity bound to subject), NATS subject-level authz per plugin (JetStream supports this — D2 picked the right tech but didn't specify using its authz), event schema + provenance validation in the bus.

### TS-5 — Malicious desired state injection *(Tampering / EoP)* — **High**
**Attacker capabilities:** Anyone who can write desired state. The architecture never says who that is — "operator declares via UI or API" (overview), with `anonymous-root` as the default identity and no authz model.
**Attack steps:**
1. Attacker (or a low-privilege user, since there's no authz contract) `Put`s a new `plane` with `axis-bindings` pointing the engine/storage at attacker-controlled plugin images, or adds a `subscription` that runs an attacker action on every `*_completed`.
2. The reconciler faithfully converges to this state — it's *designed* to make declared state real, and it has no notion of "is this declaration authorized."
3. Attacker now has a persistent foothold that the platform actively maintains and restarts (reconciler restarts unhealthy plugins — overview).
**Impact:** Persistent RCE, supply-chain pivot, the platform fights *for* the attacker.
**Defenses missing:** authz on desired-state writes (who may create planes / bind axes / add subscriptions), admission control on bindings (signed/allowlisted images only), and an approval step for privileged bindings. This is the **Crossplane/K8s `Compositions` lesson** — "let users declare infra" is RCE-equivalent without RBAC + admission webhooks.

### TS-6 — Container escape from a data-plane plugin to host/cloud *(EoP)* — **Critical**
**Attacker capabilities:** Got a malicious or compromised engine/format plugin installed (TS-1) running under the `deployment-runtime`.
**Attack steps:**
1. The plugin runs arbitrary native code by design (engines are C++/Rust/JVM). It exploits a runtime escape — **CVE-2019-5736** (runc `/proc/self/exe` overwrite), **CVE-2022-0492** (cgroups v1 `release_agent`), or **CVE-2024-21626** (runc fd leak / "Leaky Vessels").
2. From the host it reaches the node's IMDS (`169.254.169.254`) and steals the node IAM role, or pivots to other tenants' containers.
**Impact:** Host compromise, cloud-account credential theft, multi-tenant breakout.
**Defenses missing:** a *mandated minimum* isolation profile in the `deployment-runtime` contract — non-root, `cap_drop: ALL`, `no-new-privileges`, read-only FS, seccomp, IMDSv2-hop-limit / metadata blocking, gVisor/Kata for untrusted plugins. v2's ADR-017 *already specifies all of this* for the runner container; v3 has no equivalent contract and explicitly says solo runs plugins **in-process** (inbox sandboxing entry) — i.e., zero isolation by default.

### TS-7 — Multi-tenant breakout when tenancy ships *(EoP / Info disclosure)* — **Critical (latent)**
**Attacker capabilities:** A tenant on a deployment where `tenancy` is installed as a *plugin* with a `DecisionHook` (proto list, overview).
**Attack steps:**
1. Tenancy is a *decision hook* — advisory, called by the core at decision points. Nothing in the architecture makes isolation *structural*; it's an authorization opinion the core asks for.
2. The attacker reaches a code path that doesn't call the hook (state gateway? bus subscribe? credential vend? — none are specified to consult tenancy), and reads another tenant's state/data directly (see TS-3/TS-4).
3. Or it declares a plane in another tenant's namespace (TS-5) because binding-creation isn't tenancy-checked.
**Impact:** Full cross-tenant data breach — the worst outcome for the "same codebase, multi-tenant SaaS, no fork" promise (vision).
**Defenses missing:** tenancy as a **structural** isolation boundary (namespaced state, per-tenant bus accounts, per-tenant credential scopes) enforced by core primitives, *not* an advisory hook bolted on later. A decision-hook tenancy model retrofitted onto a platform whose state gateway, bus, and credential vending have no tenant dimension is the **multi-tenant SaaS breach pattern** (cf. the class of IDOR/namespace-confusion bugs that hit early multi-tenant data tools). "Tenancy is a future plugin" means *isolation is a future feature* — and isolation cannot be retrofitted onto shared primitives.

### TS-8 — Secret leakage through the seams *(Information disclosure)* — **High**
**Attacker capabilities:** Read access to logs, the event bus, backups, or manifests — i.e., a low-privilege insider or a benign-but-curious plugin.
**Attack steps:**
1. Secrets resolved by `secret-backend` flow to plugins that put them in error messages, event payloads (`pipeline_run_failed` with a connection string), or `stdout` observability (a default axis option).
2. State-backend backups (postgres/sqlite/dynamo) contain the secrets plugin's vault config with no stated encryption-at-rest requirement.
3. `VendCredentials` results are logged by a debug-happy plugin.
**Impact:** Credential disclosure via side channels — the classic way vaults leak.
**Defenses missing:** a secret-handling contract (no secrets in events/logs/manifests; redaction obligation; short-TTL vended credentials; encryption-at-rest mandate on state backends that hold secret-plugin data; scoped, audited `VendCredentials`).

### TS-9 — Capability over-reach via undeclared calls *(EoP)* — **Medium**
**Attacker capabilities:** Any installed plugin.
**Attack steps:** The manifest's `requires`/`provides` (ADR-001 contract triple) are *declarations* used for dependency negotiation, not a runtime sandbox. A plugin that declared only `requires: storage` opens a gRPC client to the `catalog`, `identity`, or `secret-backend` service and calls it. Nothing checks the manifest at call time.
**Impact:** A plugin's effective privilege = the union of every reachable service, not its declared set. Manifests become security theater.
**Defenses missing:** capability enforcement — the core (or a per-plugin proxy) must reject calls to capabilities the plugin didn't declare and the operator didn't grant. This is the **OSGi lesson**: OSGi had elaborate package/permission declarations, but class-loader-boundary escapes (and the fact that `Permissions` were rarely enforced in practice) meant the declared isolation didn't hold. Declared ≠ enforced.

### TS-10 — Reconciler/leader-lease takeover *(Tampering / DoS)* — **Medium**
**Attacker capabilities:** A plugin or process with state-gateway access (the lease lives in the state backend — D5).
**Attack steps:** Leader election uses the state-backend CAS primitive (D5). If state-gateway writes aren't authz'd (TS-3), the attacker writes the lease key to itself, or thrashes it to prevent any leader from holding it (continuous failover = no reconciliation = platform-wide stall).
**Impact:** Control-plane takeover or denial. With the lease, the attacker *is* the reconciler.
**Defenses missing:** restrict lease-key writes to the core's own identity; authz on the state gateway; lease writes that don't share a trust domain with plugin state.

---

## Recommended additions

Prioritized. Each is a missing ADR or contract clause, named so it can be written.

### Critical — must address before any production (team+) deployment
- **ADR: Plugin-to-core authentication (resolve Q13 now, not "when the API hardens").** Mandate per-plugin identity: mTLS for container runtimes, a per-startup platform token for simpler ones. **Port v2 ADR-020 wholesale** — including the `crypto/subtle.ConstantTimeCompare` lesson. Default identity must not be `anonymous-root` for any networked deployment.
- **ADR: Plugin supply-chain trust.** Signed plugin images (Sigstore/cosign keyless or Notation), provenance attestations (SLSA), signature verification at install time, and a manifest-signing requirement. Default-deny unsigned plugins in team+ bundles. The marketplace plugin must surface signing/trust status; "install on utility" cannot be the trust model.
- **ADR: State-gateway isolation.** Per-plugin key namespaces enforced by the gateway, capability-checked `Get/Put/Watch/List`, deny-by-default cross-plugin reads, and a separate trust domain for the leader lease. The state gateway is a core primitive — this *must* be core-enforced, not plugin-cooperative.
- **ADR: Deployment-runtime minimum isolation profile.** A mandated baseline every `deployment-runtime` must meet for non-in-process plugins: non-root, `cap_drop: ALL`, `no-new-privileges`, read-only FS, seccomp default, blocked cloud-metadata egress. Untrusted/marketplace plugins get gVisor/Kata. Generalize v2 ADR-017's container block into the contract. Document that **in-process solo mode = full trust** loudly, and forbid in-process for any non-first-party plugin.
- **ADR: Tenancy as structural isolation (write before the tenancy plugin is designed).** Decide *now* that state, bus subjects, and credential scopes carry a tenant dimension in the core primitives, so a future tenancy plugin enforces an existing boundary rather than inventing one on shared state. A decision-hook-only tenancy model is unsafe; say so.

### Important — address by GA
- **ADR: Capability enforcement (not just declaration).** Per-plugin call authorization derived from `requires` + operator grant; reject undeclared cross-service calls at the gateway/proxy. Add an operator-facing "this plugin requests these capabilities — approve?" install step (the Android/VSCode permission-prompt pattern).
- **ADR: Desired-state authorization + admission control.** RBAC on who may create planes, bind axes, add subscriptions; admission rules that bindings reference signed/allowlisted images only; an approval gate for privileged bindings. (The K8s RBAC + admission-webhook pattern.)
- **ADR: Event-bus authn/authz.** Bind publisher identity to events; NATS subject-level permissions per plugin (JetStream supports it); reject cross-plugin event forgery; per-tenant accounts.
- **ADR: Mandatory audit trail.** A core-level, append-only, tamper-evident record of installs, binding/config changes, credential vends, and privileged state writes that exists **even when `audit-log: none`**. Audit cannot be a fully-optional plugin; the core must emit an immutable minimum.
- **ADR: Secret-handling contract.** No secrets in events/logs/manifests; redaction obligation on plugins; short-TTL scoped `VendCredentials` audited per call; encryption-at-rest requirement for state backends holding secret-plugin data.
- **ADR: API gateway hardening.** Rate limiting, request quotas, restrictive CORS (carry over v2's `security.md` posture), authn on every route, and the internal-vs-public listener split (v2 ADR-019) as a core invariant.

### Nice-to-have — as the ecosystem matures
- Plugin reputation / verified-publisher badges and a security-disclosure process for the marketplace.
- Per-plugin egress network policy templates per deployment-runtime.
- A `rat audit` / `rat diagnose --security` tool that reports the effective trust posture (which plugins are signed, what capabilities they hold, what credentials they can vend) — the security analog of the `rat diagnose` already promised in ADR-001.
- Fuzzing + a third-party audit of the 5–10k-LOC core once it exists (the small size makes this uniquely feasible — capitalize on it).

---

## The trust-model gap

RAT v3's mental model assumes **operators trust the plugins they install.** ADR-017 (inherited from v2 and cited as still-applicable in the overview) frames plugin/pipeline code as *second-party trusted* — operator-written or operator-reviewed. That assumption is defensible for v2, where plugins are first-party containers on a private docker network. **It is not defensible for v3**, whose headline features explicitly break it:

- **A community marketplace is in the *default* bundle (D9).** The moment a solo dev clicks "install" on a third-party plugin, the trust assumption is void — the code is now *third*-party, untrusted, and running with the platform's authority.
- **"Plugins in any language, anyone can publish" (vision, contract triple).** This is the npm/PyPI/VSCode-marketplace threat model. Those ecosystems learned — through dependency-confusion (2021), typosquatting, and extension malware — that "users trust what they install" is false at scale and must be backstopped by signing, sandboxing, and capability prompts.
- **Solo mode runs plugins in-process (inbox).** There, a malicious plugin isn't sandboxed at all — it *is* the platform. "Full trust, container overkill" is fine for first-party bundle plugins and catastrophic for a marketplace install.

The assumption breaks precisely at the platform's most-marketed boundary: **the marketplace install.** And the architecture has no defense at that boundary — no signing, no sandbox mandate, no capability enforcement, no audit.

The right model is **"trust the core, verify every plugin"** — a tiered trust model the core enforces, rather than a flat trust the operator is assumed to extend:

1. **First-party / bundled plugins** — signed by the project, run with broad capability. The trusted base.
2. **Verified-publisher plugins** — signed by a known publisher, sandboxed, capabilities granted explicitly at install via an operator prompt.
3. **Community/unverified plugins** — strong sandbox (gVisor/Kata, egress-denied, minimal caps) by default, capabilities default-deny, audit-everything, and never in-process.

The core's job in this model is small and matches the six-thing discipline: **authenticate every plugin (Q13), enforce the capabilities each plugin declared and the operator granted, isolate plugin state, and emit a non-repudiable audit record — regardless of which plugins are installed.** That is four core obligations, all currently absent. They are not new axes; they are properties the *existing* six things (identity gateway, state gateway, registry, API gateway) must enforce. The trust boundary belongs in the core precisely *because* the core is the one component small enough to audit and the one component every plugin must go through.

Until those four obligations are written as contracts, "everything is a plugin" reads, from an attacker's chair, as **"every security control is opt-in, and the default is off."** The architecture is elegant; the security model is a `TODO` in an ideas file. The good news: the small-core discipline makes the fix tractable — there is a clean, auditable place to put the trust boundary. Put it there before the marketplace ships, not after the first `rat-format-evil` does.
