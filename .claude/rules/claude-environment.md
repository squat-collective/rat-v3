# Claude Code environment — maintained as architecture, not afterthought

> No `paths:` on purpose. Loads every session. Codifies the discipline for the `.claude/` directory itself.

## The premise

The same minimalist plugin-architecture discipline that governs RAT v3's code (`.claude/rules/plugin-architecture.md`) applies to its Claude Code configuration. **Built-ins are the core; custom agents/rules/hooks/skills are plugins; each must justify itself.**

The Claude Code surface is small but real:
- **Built-in agents:** `claude`, `Explore`, `Plan`, `general-purpose`, `claude-code-guide`, `statusline-setup`
- **Built-in skills:** `deep-research`, `update-config`, `keybindings-help`, `verify`, `code-review`, `simplify`, `fewer-permission-prompts`, `loop`, `schedule`, `claude-api`, `run`, `init`, `review`, `security-review`
- **Built-in tools:** Read, Edit, Write, Bash, Agent, AskUserQuestion, Workflow, SendUserFile, ScheduleWakeup, Skill, ToolSearch, plus deferred tools (TaskCreate, EnterPlanMode, WebFetch, WebSearch, MCP servers, etc.)
- **Built-in conventions:** CLAUDE.md hierarchy auto-loads; `.claude/rules/*.md` with `paths:` frontmatter auto-load by file scope; settings.json hierarchy; hooks via settings.json

## Core principles

### 1. Built-in first, custom second

Before proposing a custom agent / hook / skill / MCP server, **check the built-ins**. If a built-in covers the use case (even imperfectly), use it. The Claude Code team maintains built-ins; we don't.

The canonical "have I checked?" list:
- **Claude Code questions?** → built-in `claude-code-guide` agent
- **Settings/hooks/permissions?** → built-in `update-config` skill (or `/fewer-permission-prompts` for the specific permissions case)
- **Multi-step research?** → built-in `deep-research` skill
- **Code review?** → built-in `code-review` skill
- **Periodic / scheduled work?** → built-in `loop` or `schedule` skills
- **Bug verification?** → built-in `verify` skill
- **Codebase docs?** → built-in `init` skill

If you find yourself thinking "I should build a custom X," first answer: **which built-in have I dismissed, and why?**

### 2. Reference the official documentation

Source: `https://code.claude.com/docs/` (the official Claude Code docs). When making config decisions, **cite specific doc pages** in commits and ADRs. This prevents two failure modes:
- Drift from official patterns
- Re-inventing things Anthropic already shipped

When the docs change, our config should change. The `claude-engineer` agent (this directory) is responsible for tracking that.

### 3. Minimal `.claude/` surface

Same discipline as the 6-thing core:
- Every file in `.claude/` has a justification visible in this repo
- Stale agents/rules/hooks are deleted, not "kept just in case"
- Quarterly audit (see Maintenance below)

Anti-pattern: accumulating custom agents that overlap with built-ins because "we already wrote it."

### 4. Use the `claude-engineer` agent for Claude Code work

This project ships one custom agent: [`claude-engineer`](../agents/claude-engineer.md). It is the right tool for:
- Adding / modifying agents, rules, hooks, skills, settings, MCP servers
- Auditing the `.claude/` directory
- Researching Claude Code patterns
- Choosing built-in vs custom

It is distinct from the built-in `claude-code-guide` (research-only) — `claude-engineer` can make changes.

Default to spawning it for any Claude Code config task instead of doing it in the main session. Reason: it has the docs context primed; the main session doesn't.

### 5. Audit periodically (quarterly)

A stale Claude config is worse than a thin one. Every quarter (or when the Claude Code product releases a significant update — check the docs):

1. Spawn `claude-engineer` with the audit mandate.
2. It walks `.claude/`, identifies stale entries (referenced docs gone, replaced by new built-ins, unused for N months).
3. It produces a diff: what to keep / update / delete.
4. Land the diff via normal commit flow.

This is the same pattern as the roadmap maintenance rule (CLAUDE.md principle #9): the operational artifacts must stay fresh, or they actively mislead.

## When adding to `.claude/`

### A new agent
- Justify: which built-in agent did you consider? Why doesn't it fit?
- Cite docs: which doc pattern is this following?
- Decide: which existing built-in tools does it need? Don't grant `*` unless genuinely needed.
- Decide: model tier? `sonnet` is the default; `opus` only for hard reasoning; `haiku` for narrow tasks.
- Document: agent file lives in `.claude/agents/<name>.md` with frontmatter + system prompt.

### A new rule (`.claude/rules/<name>.md`)
- Justify: why can't this live in an existing `CLAUDE.md`? Good reasons: path-scoping (loads only when editing matching files), longer rule that would bloat CLAUDE.md, cross-cutting reference.
- Add `paths:` frontmatter if the rule is path-scoped (matches v2's `.claude/rules/` pattern). Omit for always-load rules.

### A new hook (in `settings.json`)
- Justify: why can't this be a CLAUDE.md instruction the model follows itself? Good reasons: deterministic enforcement (the model might forget), cross-session enforcement, automation around git/commits/tests.
- Be specific: hooks are powerful and easy to over-use.

### A new skill (`.claude/skills/<name>/`)
- Built-ins almost always suffice. If you ship a custom skill, document the gap in the built-ins.
- Skills are the most heavyweight addition; reserve for genuinely-novel automations.

### A new MCP server
- Justify the specific external system being integrated.
- Default off in this project unless actively used.

## What NOT to add

- ❌ A custom agent that wraps a built-in for "better defaults" (just call the built-in with better args)
- ❌ A hook that duplicates a CLAUDE.md rule
- ❌ A rule that duplicates content from `CLAUDE.md` files
- ❌ An MCP server for "we might need it someday"
- ❌ A skill for one-off automation (just script it in Bash)

## When in doubt

Spawn `claude-engineer`. It exists specifically for these decisions.

## Related

- [`.claude/agents/claude-engineer.md`](../agents/claude-engineer.md) — the agent referenced above
- [`.claude/rules/plugin-architecture.md`](plugin-architecture.md) — the founding architecture invariant; this rule is its meta-application
- [`.claude/README.md`](../README.md) — entry point to the `.claude/` directory
- Claude Code documentation: `https://code.claude.com/docs/`
