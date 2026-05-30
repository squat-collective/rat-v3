# Ideas capture rules

Ideas die unless captured. This directory is the inbox for thoughts that spark during work — your own thinking, Tom's, anyone else's.

## Where things go

- **`inbox.md`** — raw capture. Append at the bottom; date each entry. Don't worry about format.
- **Promoted ideas** — when an idea solidifies enough to act on, promote it:
  - Architectural decision → new ADR in `docs/architecture/adrs/`.
  - Big design topic → new doc in `docs/architecture/`.
  - Worth exploring more — keep iterating in `inbox.md` until it's ready.

Mark promoted entries with `[promoted → docs/...]` in `inbox.md` so the history stays linkable.

## Capture format

```markdown
## 2026-05-30 — [tag1, tag2] One-line title

What the idea is. 2-3 sentences max for capture. Don't try to fully think it through here — that's what promotion is for. Just enough to recall what you were thinking when you wrote it.

Open question: <if there's a specific thing to figure out>
Related: <links to ADRs, prior conversations, similar ideas>
```

## What counts as an idea

- A pattern you'd want to apply ("manifest schemas should be JSON-Schema with refs")
- A risk you want to track ("event bus throughput could bottleneck at 10k events/sec")
- A useful comparison ("VSCode's `contributes` model maps to our `provides`")
- A future ADR topic ("we'll need an ADR on plugin sandboxing")
- A naming question ("should it be `plugin/v1` or `rat-plugin/v1`?")
- An open architectural question ("how do we handle event-bus failure modes?")

## What doesn't count

- Implementation TODOs (those go in commit messages or PR descriptions when code lands)
- Bug reports (we don't have code yet)
- Feature wishes that don't connect to architecture (those go in a different file, when we have one)

## Capture liberally; promote selectively

Better to write down a half-formed idea and decide later than to lose it. The inbox is allowed to grow. Promotion to ADR is the filtering step.

## Periodic sweep

Once a month: read the inbox, promote what's ready, archive what's stale (move stale entries to `inbox-archive.md` with a date). Keep `inbox.md` fresh.
