# Ideas inbox

Append new ideas at the bottom. Format: `## YYYY-MM-DD — [tags] one-line title` + a few sentences.

---

## 2026-05-30 — [naming] Project codename

`RAT v3` is the working title since the folder is `rat/`. Possible future renames if we want a distinct identity:
- **Pith** (the central core of a stem)
- **Atrium** (the central courtyard everything flows through)
- **Conduit**
- **Sieve**
- **Burrow** (where RATs live — ties to brand)

Decision can wait until we ship. The internal codename matters less than the eventual product name.

---

## 2026-05-30 — [marketplace, distribution] Plugin distribution as a first-class concern

VSCode's marketplace + Cargo's crates.io are why those ecosystems flourished. RAT v3 needs a plugin marketplace from year 1, not as an afterthought. Options:
- GitHub-based: discover via topic tag `rat-plugin`, install via `gh` CLI.
- Dedicated registry: like crates.io, hosted by the project.
- Multi-source: operator points at N registries (one curated, one community, one internal).

Open question: should the marketplace ALSO be a plugin? (Yes, almost certainly. `kind: marketplace`.)

---

## 2026-05-30 — [contracts, schemas] Generate manifest schema from proto?

If proto files define the gRPC service shapes, we could generate the `plugin.yaml` manifest schemas from them. Reduces drift between "what a plugin says it provides" and "what the proto actually defines."

Tradeoff: protobuf and JSON Schema have different expressiveness. Generation works for simple cases; complex constraints (cross-field validation) might not transfer. Worth a spike.

Related: Q03 in [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md).

---

## 2026-05-30 — [event-bus, performance] Event bus failure modes

If the event bus is the coordination substrate, what happens when it's degraded?
- Stale notifications → reconciler converges slowly but eventually
- Lost events → reconciler needs idempotent retries (every loop iteration)
- Out-of-order events → reconciler must be reorder-tolerant

This argues for the reconciler being **the source of truth, not the events**. Events are hints for "you might want to look now"; the reconciler always re-reads state. Same model as K8s.

Future ADR: event-bus durability semantics + reconciler-as-source-of-truth.

---

## 2026-05-30 — [security] Plugin sandboxing

A 3rd-party `rat-plugin-foo` runs *somewhere* in the operator's environment. Trust model:
- Solo: same process as core; full trust. Container model overkill.
- Team: containerized; trust at the network level.
- Enterprise: signed images + capability whitelist + network policy.

Each level is a different `deployment-runtime` plugin doing different isolation. The core doesn't enforce sandboxing — the deployment-runtime does. Worth an ADR when we start implementing the runtime axis.

Related: v2's ADR-017 (Python pipeline trust model) — same pattern.

---

## 2026-05-30 — [migration] Bridge plugins from v2 to v3

How does v2 → v3 migration work for users?
- Option A: hard cutover — operators export v2 state, import to v3. Painful.
- Option B: bridge plugins — v3 plugins that wrap v2 services (e.g. v3 calls into v2's runner). Smoother but holds v2 hostage.
- Option C: v3 reads v2's postgres state directly + serves both APIs during transition. Most user-friendly; most engineering.

Probably (C) for v3 GA, (A) for early adopters who can re-import.

Related: Q07 in [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md).
