# Q02 — reviewer shortlist & sourcing guide

> How to find the Q02 reviewers. **This is profiles + sourcing pools, not a contacts list** — pick real humans from these pools and vet them against the checklist. Pair each with the matching tailored brief (and a cover note from [Q02-outreach-note.md](Q02-outreach-note.md)).

## How many, and which lenses

You do **not** need all five. Two to three sharp reviewers beat five shallow ones. Priority order by "what would most change the bet":

1. **Architect/contracts** — the premise + the frozen wire. If the foundation is wrong, nothing else matters. *(Non-negotiable.)*
2. **Security** — multi-tenant trust boundaries + the C2 gap + the core-bypassing bytes leg. *(Non-negotiable before any multi-tenant use.)*
3. **SRE** — would you carry the pager; the state-backend SPOF + operability gaps.
4. **Ecosystem** — only if *adoption* (not correctness) is your current worry; it's the most "it depends" lens.

**Minimum viable Q02:** architect + security (2 people). **Comfortable:** + SRE (3). Add ecosystem if you want an adoption read.

## Selection principles (all lenses)

- **Scars, not enthusiasm.** They must have *built or operated* the analog, not just used it.
- **Willing to disagree.** A reviewer who'll write "I wouldn't do this" is worth ten who'll say "neat." Pick known skeptics.
- **No conflict of interest.** Not someone who'd benefit from RAT existing (or from it not existing); not a friend who'll soften.
- **Recent + hands-on.** Someone who shipped/operated the analog in the last ~5 years, close to the metal — not a pure-strategy exec.

## Per-lens profile + where to source

### Architect / contracts → send [Q02-brief-architect.md](Q02-brief-architect.md)
- **Profile:** has *designed an extension/plugin contract surface* or a control-plane API that had to stay stable across versions and third parties. They've felt the pain of a frozen wire they later regretted.
- **Source pools:** maintainers of K8s API-machinery / CRD / apimachinery (SIG-API-Machinery); VSCode / LSP extension-API designers; OSGi / Eclipse platform contributors; Temporal / Crossplane / Backstage contract designers; people who've *written publicly* about "designing extension points" or "API evolution / deprecation."
- **What you want from them:** "is the premise sound, and which frozen-wire shape forces a v2?"

### Security → send [Q02-brief-security.md](Q02-brief-security.md)
- **Profile:** has threat-modeled a multi-tenant control plane, or worked on capability-security / sandboxing / credential-vending / supply-chain integrity.
- **Source pools:** container-runtime + isolation folks (runc / gVisor / Kata / Firecracker); the Sigstore / SLSA / in-toto supply-chain community; STS/credential-vending + IAM designers; capability-based-security + fine-grained-authz people (Zanzibar / OPA / biscuit); cloud-security researchers who publish threat models.
- **What you want from them:** "where does a malicious plugin / tenant / leaked ticket break a boundary — and is the planned C2 fix sound?"

### SRE / operability → send [Q02-brief-sre.md](Q02-brief-sre.md)
- **Profile:** has *operated* a multi-tenant orchestrator or controller-based system in production and written the incident retros.
- **Source pools:** the SREcon / "operating Kubernetes at scale" circle; K8s SIG-Scalability / SIG-Instrumentation; teams who run controller-managers, Temporal/Cadence, or large reconcile loops; platform-engineering / infra SRE leads.
- **What you want from them:** "would you carry the pager, and what's the one operability gap you'd close first?"

### Ecosystem / plugin-author → send [Q02-brief-ecosystem.md](Q02-brief-ecosystem.md)
- **Profile:** has *grown* a plugin/extension ecosystem (DevRel/ecosystem lead) or *authored many integrations* and knows what makes authors show up.
- **Source pools:** prolific integration authors (dbt adapters, Airflow providers, Singer taps, Terraform providers, Backstage/VSCode extensions); ecosystem/DevRel leads of those communities; people who've written about "why plugin ecosystems succeed or die."
- **What you want from them:** "would you build a plugin / bet a tool's distribution on this, and what crosses cold-start?"

## Logistics reminders

- Reach out with a [cover note](Q02-outreach-note.md) (per-lens variant) **first**; attach the matching brief on a yes.
- It's **confidential / unpublished** — say so up front; a light NDA is reasonable if they want one.
- Track each in [Q02-tracker.md](Q02-tracker.md); their findings land as `reviews/11-q02-<name>.md` (template in the tracker).
- Budget the *synthesis*: once ≥2 reviews are in, reconcile + severity-rank them, and feed the bottom-line into the **Q01** (v2-vs-v3) call.
