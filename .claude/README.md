# `.claude/` — Claude Code configuration for RAT v3

What lives here, what doesn't, and the discipline for maintaining it.

## Structure

```
.claude/
├── README.md                          # this file
├── settings.json                      # project-shared permissions + config (committed)
├── settings.local.json                # per-user overrides (gitignored)
├── agents/
│   └── claude-engineer.md             # custom agent for Claude Code config work
└── rules/
    ├── plugin-architecture.md         # founding architectural invariant (always-load)
    └── claude-environment.md          # discipline for this directory (always-load)
```

## What's here, briefly

- **`settings.json`** — empty `permissions.allow` array (no commands needed beyond Claude Code's built-in safe-readonly list). Future additions land here when justified.
- **`agents/claude-engineer.md`** — specialized agent for any Claude Code setup / audit / extension work. Reads the official docs, prefers built-ins, makes minimal changes.
- **`rules/plugin-architecture.md`** — codifies the *"6-thing core + everything else is a plugin"* invariant from [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md). Always-load (no `paths:`).
- **`rules/claude-environment.md`** — the meta-discipline: built-in first, cite docs, minimal surface, audit quarterly. Always-load.

## Maintenance

See [`rules/claude-environment.md`](rules/claude-environment.md) for the rules. The short version:

- **Built-in first.** Before adding a custom agent / hook / skill / MCP server, prove the built-in doesn't fit.
- **Cite the docs.** Reference `https://code.claude.com/docs/` in commits when adding things.
- **Minimal surface.** Same discipline as the architecture. Every file justified.
- **Audit quarterly.** Spawn `claude-engineer` with the audit mandate; remove stale entries.

## When in doubt

Spawn the `claude-engineer` agent. That's what it's for.

## Why this directory matters

The project's working principles (CLAUDE.md) are read every session — they shape behavior. The `.claude/` directory is the operational layer underneath: it controls *what* tools/agents/rules are available, *how* permissions resolve, *what* gets validated automatically. A neglected `.claude/` directory leads to one of two failure modes:

1. **Bloat:** custom agents/hooks/skills accumulate; they stop matching how the project actually works; new sessions get confused.
2. **Stagnation:** the directory is created once and never updated; Claude Code product improvements don't propagate.

Treat this directory with the same discipline as the architecture itself. Read [`rules/claude-environment.md`](rules/claude-environment.md) before adding anything.
