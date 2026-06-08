# ADR-034: The rat security responsibility model — what rat owns, what plugins enforce, what the environment provides

## Status: Accepted (2026-06-04)

## Context

rat is, by design, **open and accessible at the network layer** — it does not bake in a perimeter.
The remote-access + many-workspaces conversation (and [ADR-033](033-workspace-federation-hub.md),
the federation hub) forced the question this ADR answers: **which security concerns does rat own,
and which does it delegate?** Without a written boundary, two opposite failure modes appear:

- **Scope creep.** rat starts reinventing VPCs, firewalls, service-mesh mTLS — *worse* than the
  dedicated tools, and a direct violation of the six-thing discipline ([ADR-001](001-everything-is-a-plugin.md)).
- **The "default-open" breach.** If "open and accessible" is misread as "no auth needed," rat
  becomes the next **open etcd / public MongoDB / exposed Kubernetes dashboard** — every one of
  those was a *default-open binary handed a public IP without a perimeter or auth*.

Today's state: **C5** capability authorization is enforced, **C8** audit is emitted, the **identity
gateway** (core thing #2) is a slot — but the caller identity is **trust-asserted** (`--as` is taken
on faith), there is **no TLS** on the listeners, and there is **no stated policy** on rat-vs-environment
responsibility. The prior art is well-trodden and worth naming: **AWS's Shared Responsibility Model**
("AWS secures *of* the cloud; you secure *in* the cloud") and **Kubernetes** (the API server does
authN/authZ/admission; the *network* is the CNI's + cloud's job). rat needs the same explicit split.

## Decision

Adopt the **rat Shared Responsibility Model**: three rings, a secure-by-default posture, and an
irreducible core minimum. **One-line form: *authentication is delegated, authorization is owned, the
perimeter is environmental.***

### 1. The three rings

| Ring | Owns | Examples | Why it lives here |
|---|---|---|---|
| **🌍 Environment** (delegated + assumed) | the **perimeter** | VPC · subnets · firewall/security-groups · service-mesh mTLS · NetworkPolicy · ingress + TLS edge · host/OS hardening · DDoS/WAF | rat can't out-engineer a cloud VPC or Istio, and **doesn't need to exist to create the network** — so it's neither core nor a plugin. It is *around* rat. |
| **🔌 Plugins** (pluggable enforcement) | the **"how" of trust** | **identity** (who? — OIDC/token/SPIFFE) · secret storage · audit sink · tenancy rules · *optionally* connectivity/transport | swappable per deployment; the **identity axis** answers "who is this caller." |
| **⚙️ Core** (irreducible — can't be a plugin) | the **authorization decision + the guarantees** | identity *required* on every request · **C5** capability authz · **C8** mandatory audit · **C3** per-plugin state isolation · **C1** trace propagation | chicken-and-egg: plugins are reached *through* the gateway, so the gateway must enforce these *before* any plugin runs. It cannot be reached through itself. |

### 2. The irreducible core minimum — true even with **zero** plugins installed

The core owns the application-layer trust the environment **cannot** provide (the environment does
not understand capabilities or tenants). With no identity/audit/tenancy plugins present, the core
MUST STILL:

- **require identity** on every request (the identity gateway rejects an anonymous call to a
  sensitive capability),
- **enforce C5** (caller's manifest `requires` ∧ provider `provides` — the allow/deny),
- **emit a C8 audit record** (to a default sink — stderr/file — when no audit plugin gives it a
  durable home).

This is **not a 7th core thing.** These are the *correctness conditions of the existing six*
(identity gateway + API gateway + the cross-cutting enforcement, see
[`plugin-architecture.md`](../../.claude/rules/plugin-architecture.md) "Cross-plugin concerns" and
[reviews/00](../../reviews/00-synthesis.md) C1–C10). The count stays **6**.

### 3. Authentication + transport are **plugin / environment** concerns

The core enforces *that* identity is present and *that* it gates C5; it delegates **who** (the
identity plugin: API token → OIDC → SPIFFE/SVID) and **encryption-in-transit** (TLS on the listener,
or mesh-provided mTLS — the environment). A caller presents a credential → the identity plugin
validates → maps to a principal (who + tenant) → C5 authorizes the principal's grant. This is where
the trust-asserted `--as` gap is closed: **at the edge, by the identity plugin.**

### 4. Secure-by-default binding posture (the guardrail on "fully open")

"Open and accessible" is correct as a **capability**, dangerous as a **default**. So:

| Bind | Allowed when |
|---|---|
| `unix:.rat/daemon.sock` (default) | always — local only; filesystem perms are the boundary |
| `0.0.0.0:port` (public TCP) | **requires TLS + an identity plugin** — else **refuse / fail loud** ("you are exposing an unauthenticated control plane") |
| trust-asserted `--as` (no real authn) | **localhost / unix-socket only** — never the mode on a public bind |

"Fully open" must mean *easy to open*, never *accidentally open*. The default ships **closed**;
exposure is a deliberate, guard-railed act. This applies equally to `rat serve` and `rat hub`
(ADR-033): a public hub is the single front door, and it is the place TLS + identity are mandatory.

### 5. Defense-in-depth — the perimeter is a ring, not the wall

"The environment does the network" must **not** become "rat does no security thinking." Every
capability hop — local or remote, inside the perimeter or at the edge — re-runs identity + C5 +
audit (**zero-trust**: being past the VPC buys nothing). **Reachable ≠ exposed.** rat still
terminates/propagates TLS-mTLS where it can; the environment is the *outer* ring, rat's authz the
*inner* one. You want both.

## Consequences

**Positive.**
- **On-thesis, no scope creep.** rat does the one security job only it can (capability + tenant-aware
  authz + audit) and delegates the rest. The six-thing core is untouched; the perimeter is not even
  a plugin.
- **The default-open breaches are structurally prevented** — the default is closed, and public
  exposure trips a TLS+identity requirement.
- **Composes across every topology** (the [ADR-033](033-workspace-federation-hub.md) spectrum): solo
  laptop (unix socket, no auth) → team (VPC + OIDC) → SaaS (managed edge + per-tenant identity) — same
  core, outer rings populated to taste.
- **Identity composes at the hub** (federation): authenticate once, each workspace authorizes per its
  own C5 against the forwarded principal.

**Negative — accepted.**
- **rat depends on the environment being configured correctly.** It cannot save you from a
  misconfigured VPC or a hub bound to `0.0.0.0` with auth disabled — it can only make the secure path
  the easy/default one and fail loud on the obvious foot-guns. This is the *same* contract AWS/k8s
  offer; it must be **documented prominently** or users will still expose things.
- **The secure-by-default coupling is owed, not built.** Today `--as` is trust-asserted and there is
  no TLS — this ADR turns "public bind without TLS+identity" into a **blocker to implement**
  (refuse/fail-loud), and makes the **identity-axis reference plugin + TLS** prerequisites before any
  public bind / public hub ships (Q01–Q02).
- **Two trust modes to reason about** (localhost-trust vs authenticated) — a deliberate ergonomics
  concession so `chmod +x ./rat` stays frictionless for a solo dev.

**Neutral.** This ADR is a *codification* — it names a boundary the architecture already implied
(the cross-cutting concerns, the identity gateway). It adds no contract surface; `make breaking` is
untouched. The behavioral changes it mandates (the binding guardrail, the always-on audit default)
are follow-on builds.

## Open questions

- **Q01 — the identity-axis reference plugin** (token → OIDC → SPIFFE) — owed before a public bind is
  safe; the concrete "who?" the model delegates. (Feeds [ADR-033](033-workspace-federation-hub.md) Q03,
  identity-at-hub.)
- **Q02 — TLS surface + the binding guardrail:** cert source (file / ACME / mesh-provided), and how
  the public-bind check is enforced (hard refuse vs `--insecure` escape hatch + loud warning) in
  `rat serve` / `rat hub`.
- **Q03 — capability sensitivity classes:** is *everything* deny-by-default-without-identity, or may
  a narrow set (health, version) be anonymous? Default: deny-by-default; revisit per capability.
- **Q04 — the default audit sink** when no audit plugin is installed (stderr vs a rotating file vs
  the event bus) — the "always-on audit" needs a concrete floor.
- **Q05 — workspace ACLs** (which principals may reach which workspaces) — the per-workspace grant,
  shared with [ADR-033](033-workspace-federation-hub.md) Q04.

## Alternatives considered

- **rat bakes in network security** (built-in VPN / mesh / firewall). Rejected: worse than the
  dedicated tools, can't out-engineer a cloud VPC, and a flat six-thing-core violation. Network is
  the environment's ring.
- **Fully open, no minimum** (trust the network entirely, no identity/C5/audit floor). Rejected:
  exactly the default-open breach class; abandons zero-trust the moment the perimeter is wrong.
- **Mandatory authentication always** (no localhost-trust dev mode). Rejected: kills the
  `chmod +x ./rat` solo ergonomics; a single-user local unix socket where filesystem perms *are* the
  boundary is a reasonable trusted default.
- **A single "security" plugin** owning authn+authz+audit together. Rejected: authz (C5) is a core
  correctness condition (can't be a plugin — chicken-and-egg); only authn + audit-sink are pluggable.
  Bundling them re-introduces the bypass risk the ring split removes.

## Migration

1. **Codify** the three rings + secure-by-default posture in
   [`plugin-architecture.md`](../../.claude/rules/plugin-architecture.md) (extends the existing
   "Cross-plugin concerns" section) so the boundary is always-loaded discipline.
2. **Implement the binding guardrail** in `rat serve` + `rat hub`: a public TCP bind requires TLS +
   an identity plugin, else refuse / fail-loud (Q02).
3. **Identity-axis reference + TLS** (Q01) — the prerequisite build for any public bind / public hub
   ([ADR-033](033-workspace-federation-hub.md) Q03 depends on this).
4. **Default audit sink** (Q04) so the always-on C8 floor is real with zero plugins.

No proto change; `make breaking` clean.

## Related

- [ADR-001](001-everything-is-a-plugin.md) — the six-thing core + plugin discipline this model keeps.
- [ADR-033](033-workspace-federation-hub.md) — the federation hub; this model is what secures it (the
  single front door is where TLS + identity become mandatory).
- [ADR-007](007-call-context-in-metadata.md) — identity rides in `rat-callmeta-bin`; the gateway
  re-derives `caller_plugin` per hop (wire identity is not trusted).
- [ADR-019](019-rat-serve-cold-executable-spec.md) — the daemon's bind address (default unix socket)
  this posture builds on.
- [`plugin-architecture.md`](../../.claude/rules/plugin-architecture.md) + [reviews/00](../../reviews/00-synthesis.md)
  C1–C10 — the cross-cutting correctness conditions this names.
