# `notifications/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> the reference against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: notifications` plugin. Pairs with the wire
> contract [`notifications.proto`](notifications.proto) and the golden vectors
> [`notifications-v1.json`](../../../../conformance/notifications-v1.json). Status: **v1 (frozen — rat/1.4, ADR-003: experience = one ref + conformance)**.

## What a `notifications` plugin is

A `kind: notifications` plugin (Slack, email, PagerDuty, webhook) is a **delivery sink**:
given a `(severity, title, body, target, attributes)` tuple it delivers one message to a
channel and reports `(delivered, message_id)`. **What** to notify on is the operator's
subscription config (`subscriptions = [event, action]` in the reconciliation model) — that
is NOT this plugin. This axis owns only the delivery side.

This is an EXPERIENCE-axis plugin. There is no Arrow data plane, no branching, and no
tenant-scoped routing. Emitters (the core or other plugins) call `Send`; the plugin
delivers.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://notifications/v1/send` | `Send` | deliver one notification — `(severity, title, body, target, attributes)` → `(delivered, message_id)` |

## The RPCs

- **`Send(severity, title, body, target, attributes)` → `{delivered, message_id}`** — deliver
  one notification. `severity` is the `Severity` enum (`INFO`, `WARNING`, `ERROR`, `CRITICAL`).
  `target` is a plugin-specific routing hint (channel name, email address, webhook URL) — the
  plugin interprets it. `attributes` is a `map<string, string>` for structured context,
  templating, and correlation (includes `correlation_id`, satisfying C1).
  Empty `title` → `INVALID_ARGUMENT` (a notification must have a subject). A successful
  delivery returns `delivered = true` and a non-empty `message_id` (the provider's handle for
  delivery tracking, if any). Emitters MUST NOT include secrets in `title` or `body` (I13);
  the core redacts, but the obligation is shared.

## Conformance obligations

Pass [`notifications-v1.json`](../../../../conformance/notifications-v1.json): the vectors
exercise `Send` for `INFO` and `CRITICAL` severities (expect `delivered = true`) and the
`empty_title` error path (expect `INVALID_ARGUMENT`).

- **`empty_title` → `INVALID_ARGUMENT`** — a `SendRequest` with `title = ""` MUST fail with
  `INVALID_ARGUMENT`. This is a request-grammar violation: the caller is broken, not the
  backend. Retrying identically will fail identically.
- **Successful `Send`** MUST return `delivered = true` and a non-empty `message_id` when the
  provider accepted the message. If the provider has no tracking id, generate a synthetic one
  (the `inmemory-py` reference uses a monotonic `msg-<n>`).
- **Transient provider failures** → `UNAVAILABLE` (retryable). Permanent provider errors → `INTERNAL`.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response fields for normal domain outcomes.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `NotificationsService` server.

## Writing a plugin

1. Implement `NotificationsService.Send` over your delivery provider (Slack API, SMTP,
   PagerDuty Events API, outbound HTTP webhook, etc.).
2. Validate `title` on entry — return `INVALID_ARGUMENT` immediately if empty. Do not
   pass an empty title to the provider.
3. Map provider errors: transient/network failures → `UNAVAILABLE`; unexpected panics →
   `INTERNAL`; do not leak provider-specific status codes directly.
4. Declare `kind: notifications` and `provides: [rat://notifications/v1/send]` in your
   `plugin.yaml` manifest ([ADR-011](../../../../../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md)).
5. Pass [`notifications-v1.json`](../../../../conformance/notifications-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/notifications/inmemory-py`](../../../../../plugins/notifications/inmemory-py) | 1 (wire) | delivery-sink reference: captures messages in-memory so the harness can assert delivery without a real provider |

## Related

[`notifications.proto`](notifications.proto) · [`notifications-v1.json`](../../../../conformance/notifications-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (experience = one ref + conformance) ·
[ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md) · [ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md) · [ADR-011](../../../../../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md)
