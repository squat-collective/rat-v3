# Done — completed work log

Reverse chronological. Each entry: date, what was accomplished, links to artifacts (commits, files, ADRs).

---

## 2026-05-30 — Core language locked: Go (ADR-004)

Wrote [ADR-004](../docs/architecture/adrs/004-core-language-go.md) to **ratify and lock** the Go decision that [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D1 had already made. The decision itself wasn't new — D1 said "Core language: Go" all along — but the project *prose* (CLAUDE.md "Rust or Go") and the just-landed allowlist (both toolchains) were still treating it as open. ADR-004 closes that gap before code starts.

### Why an ADR if D1 already said Go
The gap between "decided in the ADR" and "treated as open in prose/tooling" is exactly the kind of drift the ADR-first discipline exists to catch. ADR-004 also records something D1 left implicit: Go is the **committed default**, with the door to re-language kept open as a Phase-0 validation checkpoint per D1's own re-language meta-principle (not via a prototype spike — ADR-002 specifies none).

### Changes
- **`docs/architecture/adrs/004-core-language-go.md`** — new ADR (Accepted).
- **`docs/architecture/adrs/002-founding-tech-stack.md`** — D1 cell annotated "Locked & ratified by ADR-004" (cross-link only; decision unchanged).
- **`docs/architecture/adrs/README.md`** — index row for ADR-004.
- **`.claude/settings.json`** — **trimmed to the Go toolchain**: dropped the 7 Cargo/Rust allow-rules (`cargo build/test/check/clippy/run/fmt/doc`) added in the prior entry. The "both toolchains until locked" rationale no longer holds. 26 → 19 rules.
- **`roadmap/current.md`** — updated.

### Rationale source
Grounded in ADR-002 D1: ecosystem alignment (etcd/NATS/K8s/Temporal/Crossplane all Go), mature gRPC tooling (`grpc-go`), faster MVP, larger cloud-native contributor pool, plus plugin-SDK ergonomics (no borrow-checker barrier for plugin authors — the ADR-001 bet). Contracts remain language-neutral, so third-party plugins stay any-language.

---

## 2026-05-30 — `.claude/settings.json` coding-phase allowlist

> **Superseded in part (same day):** the Cargo/Rust rules added here were removed once the language locked to Go — see the ADR-004 entry above.


Pre-coding permissions audit (via `claude-engineer` agent). Expanded the `permissions.allow` array to cover both candidate toolchains — the Go vs Rust language decision from ADR-002 is still open ("Both / undecided"), so both are pre-allowed so Phase 0 can start in either direction without permission-prompt friction.

### Changes

- **`.claude/settings.json`** — added `$schema` key (IDE autocomplete); populated `permissions.allow` with 26 command rules:
  - **Go:** `go build/test/vet/mod/generate/run/fmt`
  - **Rust/Cargo:** `cargo build/test/check/clippy/run/fmt/doc`
  - **Protobuf (buf):** `buf generate/lint/breaking/format`
  - **Podman:** `podman build/run/compose` (per Tom's container-only rule in root CLAUDE.md)
  - **Git (safe):** `git commit/add/diff/log/status`

### Deliberately omitted (keep prompting)

`git push`, `git reset`, `git rebase`, `podman rm`, `podman rmi` — destructive or remote-affecting; prompt behavior preserved.

### Two deferred items queued in backlog

(See `backlog.md` — "Claude Code config: deferred until first code file" section.)

### Rationale

`go test ./...`, `cargo clippy`, `buf generate` etc. are not in Claude Code's built-in read-only set and would prompt on every invocation. Listing them in `permissions.allow` removes the friction without relaxing any destructive-command guardrails. Cite: `https://code.claude.com/docs/en/permissions.md`.

---

## 2026-05-30 — `.claude/` configuration landed

Project-specific Claude Code setup created. Same minimalist discipline as the architecture: built-ins first, custom additions justified, docs cited.

### Files added
- `.claude/README.md` — entry point + structure
- `.claude/settings.json` — `permissions.allow` empty (honest: every common command in transcripts was either auto-allowed or mutating; nothing meaningful to allowlist)
- `.claude/rules/plugin-architecture.md` — founding constitutional invariant from ADR-001 (always-load, no `paths:` frontmatter). Codifies the 6-thing core + 16+ axes; the "tier 0" callout from synthesis Finding 6; cross-cutting concerns owned by the core.
- `.claude/rules/claude-environment.md` — meta-discipline for `.claude/` itself. Built-in first, cite official docs, minimal surface, quarterly audit. Names the built-in agents + skills explicitly so future sessions can't drift.
- `.claude/agents/claude-engineer.md` — specialized custom agent (model: sonnet; tools: Read/Edit/Write/Bash/WebFetch/Grep/Glob) for Claude Code config work. Reads `https://code.claude.com/docs/` before recommending changes; distinct from built-in `claude-code-guide` (research-only) — `claude-engineer` can make changes.

### Files updated
- Root `CLAUDE.md` — new principle #10 "Maintain the Claude Code environment as architecture"; directory map extended with `.claude/` tree
- `.gitignore` — excludes `.claude/settings.local.json` (per-user overrides not committed)

### What was NOT added
- ❌ Hooks — the synthesis warned against premature automation; CLAUDE.md rules are enough for now
- ❌ Custom skills — built-in skills (deep-research, code-review, etc.) cover the needs
- ❌ MCP servers — nothing project-specific yet
- ❌ Other custom agents — built-ins (claude, Explore, Plan, general-purpose, claude-code-guide) cover everything except Claude-Code-config-itself, which is what claude-engineer is for

### Rationale
Tom asked for the setup as part of "before anything of this could you tell me the claude setup for this new sandbox." The audit surfaced that the project had only CLAUDE.md rules — no agents, hooks, settings. Rather than adding a wide surface, we mirrored the architecture's discipline: a minimal `.claude/` core (rules + one custom agent) that future additions must justify against built-ins.

The `claude-engineer` agent is the operational equivalent of ADR-003's "two reference implementations before contract freeze" rule — it forces every Claude Code config change to go through a docs-citing, built-in-first filter, instead of accumulating ad-hoc custom additions over time.

---

## 2026-05-30 — Phase −1 complete

The full architectural-design + adversarial-review phase, landed in one day with Claude.

### Roadmap structure + ADR-003 + post-synthesis doc updates
- Created `roadmap/` directory with CLAUDE.md (maintenance rules), README, phases.md, current.md, done.md, backlog.md
- Wrote **ADR-003** — "Two independent reference implementations before any contract freezes" (the C9 forcing function from synthesis)
- Updated **ADR-001 Migration section** — Phase 0 timeline shifted to 4-6 months; Critical cross-cutting concerns baked in; all 5 phases expanded with post-synthesis scope
- Updated **vision.md** — added "Anti-goals" section: (1) no new plugin axis in year 1 until 100 daily users on core, (2) effort ratio must invert from 95/5 architecture-to-GTM toward 60/40
- Updated **ADRs index** with ADR-003
- Updated **root CLAUDE.md** with roadmap reference + maintenance rule

### 5-perspective adversarial team review
- Spawned `rat-v3-review` team with 5 teammates (adversarial-architect, plugin-ecosystem-builder, operations-sre, security-reviewer, product-gtm) running in parallel via the experimental agent-teams feature
- Each produced a deep critique against the founding ADRs
- Wrote **synthesis** (`reviews/00-synthesis.md`) — 5 cross-cutting themes converged across all 5 reviewers, 10 Critical findings, 17 Important findings, 26 prospective ADRs, 2 roadmap shifts
- Headline finding: *the architecture is sound; the cross-cutting concerns that genuinely have to span plugins (trust, conformance, observability, distribution) have no owner; the proposed mitigations for the documented failure modes don't escape them*
- 5 review files: `01-adversarial-architect.md`, `02-plugin-ecosystem-builder.md`, `03-operations-sre.md`, `04-security-reviewer.md`, `05-product-gtm.md`
- Team gracefully shut down post-synthesis

### Founding ADRs landed
- **ADR-001** — "Everything is a plugin" (the founding principle: 6-thing core + 16+ plugin axes)
- **ADR-002** — "Founding tech stack + strategy decisions" (Go + NATS + JSON Schema + Apache 2.0 + K8s patterns + 7 other decisions across 10 questions; meta-principle: AI-rewrite escape hatch lowers tech-choice stakes)
- Both ADRs include rejected-alternatives tables, structured Consequences sections, and links to the conversations that produced them
- Conversation distillations committed: `docs/conversations/2026-05-30-the-vision-conversation.md` + `docs/conversations/2026-05-30-tech-stack-decisions.md`

### Initial scaffold
- Created `~/sandbox/rat/` project directory + git init
- Project CLAUDE.md with working principles (contracts before code, six-thing-core discipline, ADR-first, honest tradeoffs, capture-ideas-where-they're-born, save load-bearing conversations)
- README + .gitignore
- Vision document (the thesis) — minimal core + everything pluggable
- Architecture overview document — the integrated picture
- ADRs README with template + numbering/discipline rules
- Sub-CLAUDE.md files for `docs/architecture/adrs/`, `ideas/`, `docs/conversations/`
- ideas/inbox.md with 6 seed entries (later expanded to 11)
- research/prior-art/README.md (K8s, OSGi, VSCode, NATS, Temporal, etc.)
- research/competitors.md (Snowflake, Databricks, dbt, Airflow, Iceberg, MotherDuck — the landscape)
- 14 files, ~1276 lines, 1 commit (`7d57eab`)

### Git commits this session

```
536c9c1 docs(review): synthesis + remaining 2 reviews — 5-perspective adversarial audit
4d2af82 docs(review): security threat model (STRIDE) for RAT v3
778e79d docs(review): 3rd-party plugin author ecosystem review
dbdcde5 docs(review): adversarial architect review
33c1ec0 docs(adr): lock founding tech stack — Go, NATS, Apache 2.0, K8s patterns (ADR-002)
7d57eab chore: initial scaffold for RAT v3
```

(This entry's own commit lands separately — see git log for `docs(roadmap+adr): ...`.)

### What's true at end of day 2026-05-30

- Project lives at `~/sandbox/rat/`, git-initialized, ~3000 lines of architecture + thinking
- 3 Accepted ADRs (001, 002, 003)
- 2 conversation distillations
- 5 adversarial reviews + 1 synthesis
- 11 ideas captured in `ideas/inbox.md`
- Research scaffold present (prior art + competitors)
- Roadmap structure operational; this file is the proof
- **No code yet.** Awaiting Tom's commitment decision before Phase 0 starts.
