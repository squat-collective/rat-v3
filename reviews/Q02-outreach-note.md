# Q02 — reviewer outreach note (template)

> The recruiting message to send a candidate external reviewer. Personalize the
> `[bracketed]` bits, keep it short. The detailed [reviewer brief](Q02-external-review-brief.md)
> goes *after* they say yes (or attach it if they prefer to judge before committing).

---

**Subject:** Would you try to break a data-platform architecture I'm betting on? (~1–2 days, confidential)

Hi [Name],

[Personal hook — one specific, genuine line: e.g. "I've leaned on your writing about OSGi's service-registry pitfalls for years" / "your Temporal work is exactly the control-plane experience I'm missing."] You've *built* systems like the one I'm about to describe, which is why I'm reaching out.

I've been building **RAT v3**, a from-scratch data platform on a single bet: *the platform is a minimal control plane that orchestrates self-describing plugins* — a roughly six-thing core, and everything else (state backend, scheduler, engine, format, deployment runtime, catalog, …) is a plugin behind a frozen contract. The contracts are frozen and the core is built and tested end-to-end against real plugins.

Before I commit the next year-plus to it, I want someone who's lived this to **try to break the premise** — tell me where it's naïve, where the frozen wire will force a painful v2, or where I'm cheerfully repeating a mistake [OSGi / Kubernetes / Temporal] already paid for. Every review so far has been my own, so it shares my blind spots; I need genuinely outside eyes.

It's a focused read: a short brief frames exactly what to look at and the handful of questions I care most about — 1–2 days is plenty, and async is completely fine. The work is unpublished, so I'd ask you to keep it confidential.

Up for it? A flat "no" is totally fine — and if someone else comes to mind who'd be sharper on this, a pointer is just as valuable.

Thanks,
Tom

---

### Personalize before sending
- `[Name]` and the **personal hook** — make it specific and true; this is why they'll say yes.
- `[OSGi / Kubernetes / Temporal]` — name the system *they* actually built/operated.
- If they'd rather size it up first, attach / link the matching brief (below).
- Pick the right lens from [Q02-reviewer-shortlist.md](Q02-reviewer-shortlist.md) and use its variant line.

### Per-lens variants (swap the third paragraph: "Before I commit … I want someone who's lived this to **try to break the premise** …")

Keep the rest of the note; replace just that "try to break" paragraph + send the matching brief.

- **Architect / contracts** → [`Q02-brief-architect.md`](Q02-brief-architect.md)
  > Two things are now hard to undo: the premise is committed and the wire is **frozen** (a contract mistake found later is a v2 break). Before I build a year on it, I want someone who's designed an extension contract that had to survive third parties to tell me where the premise is wrong or which frozen shape I'll regret.

- **Security** → [`Q02-brief-security.md`](Q02-brief-security.md)
  > The whole bet is "install many third-party plugins," so the security is in the boundaries, not any one plugin. I want you to be the adversary — a malicious plugin / tenant / leaked ticket — and tell me where a boundary breaks. (One gap I'll flag up front: identity is currently envelope-derived, not yet channel-authenticated — I want your read on the planned fix.)

- **SRE / operability** → [`Q02-brief-sre.md`](Q02-brief-sre.md)
  > Plainly: **would you carry the pager for this?** It's a control plane over N polyglot plugin processes; my own SRE review was the harshest of the lot and most of it is still open. Tell me what wedges at 3am and what must be true before it runs in production.

- **Ecosystem / plugin-author** → [`Q02-brief-ecosystem.md`](Q02-brief-ecosystem.md)
  > It only works if third parties actually build plugins. There are ~30 first-party references and **zero** third-party ones. You've watched ecosystems reach critical mass or die — tell me whether this crosses cold-start, and whether you'd build a plugin (or bet a tool's distribution) on it.
