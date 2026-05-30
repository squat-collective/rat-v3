# Conversations rules

Distilled Claude sessions where architectural shape emerged. The point: institutional memory. When future-us (or future-Claude) opens the project, the conversations dir is the second thing read (after the architecture overview) — it answers *how did we get here?*

## When to save a conversation

- **Yes:** the session shaped or rebuilt the architecture. Examples: the founding "what if we started from scratch?" thread; an ADR that emerged from a multi-hour debate; a research deep-dive into prior art.
- **Yes:** decisions were made that didn't end up in an ADR (yet). Conversations are the ADR draft pool.
- **No:** straightforward implementation work. ("Tom asked me to fix a typo.") That's commit-message territory.
- **No:** routine status updates. Doesn't need a record.

## Format

```markdown
---
date: YYYY-MM-DD
participants: [Tom, Claude (model name)]
topics: [vision, architecture, plugin-model, ...]
key_decisions:
  - one-line summary of each decision made
  - link to ADR if promoted
key_questions_opened:
  - one-line summary of each question raised (for future ADRs)
status: distilled | raw
---

# Title — descriptive, dated

## Context
What sparked this conversation? What was the entering question?

## The shape that emerged
The synthesis. Not a transcript — the distilled architecture / decisions / framings that came out of the discussion. Should read in 10-15 minutes and convey the full thinking.

## Concrete artifacts produced
Links to ADRs, doc updates, code, anything tangible. So future-us can trace "this conversation → these changes."

## Open threads
Things that came up but weren't resolved. These often become ideas/inbox.md entries or future ADRs.

## Tone notes
Anything about *how* this conversation went that future-us should know. (e.g., "Tom pushed back hard on X; the eventual decision was Y after his challenge.")
```

## Distill, don't transcribe

A 4-hour conversation should become a 200-line distillation. The raw chat log is too long to read fresh; the distillation should capture:
- The framings that worked (and how they evolved)
- The decisions made (with brief why)
- The open questions raised
- The character of the discussion (was it adversarial? convergent? exploratory?)

If a specific exchange was load-bearing, quote it. Otherwise, write in your own words.

## Naming

`YYYY-MM-DD-short-topic.md`. Topic should be specific enough to recognize months later. `2026-05-30-the-vision-conversation.md` is fine; `2026-05-30-discussion.md` is not.

## Linking

When a conversation produces an ADR, link both ways:
- ADR's `Related` section links the conversation.
- Conversation's `Concrete artifacts` links the ADR.

This is the trace of "thinking → decision."
