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
- If they'd rather size it up first, attach / link the [reviewer brief](Q02-external-review-brief.md).
- Optional: front-load the question area that matches them (a security person → the data-plane/credential questions; an SRE → the operability ones).
