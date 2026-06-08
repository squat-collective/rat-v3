# rat-notifications-inmemory-py — `notifications` reference

> ⚠️ **WIRE-CONTRACT REFERENCE — NOT PRODUCTION-HARDENED.** This validates the `notifications/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory store**. A production `notifications` plugin adds a durable/real backend + the enforcement the core will demand (Phase 1) — it demonstrates the contract, not a deployment. See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The `kind: notifications` reference for the experience axis. It conforms to the
`notifications/v1` contract by passing the
[golden vectors](../../../contracts/conformance/notifications-v1.json): an
independently-written implementation that drives
`NotificationsService.Send` over real gRPC.

Notifications is a **delivery sink** — slack, email, webhook, PagerDuty. Given a
message it delivers it and reports `(delivered, message_id)`. **WHAT to notify on
is NOT this plugin's job**: it's the operator's subscription config
(overview.md reconciliation: `subscriptions = [event, action]`). The plugin owns
only the delivery side. The single RPC under test is `Send`.

This reference is an **in-memory sink** — instead of delivering anywhere it
*captures* each notification into a list (a test / webhook stand-in), so the
harness can assert the delivery contract end to end.

## What's special about this axis

Unlike `storage`, notifications is **not tenant-scoped** — there is no
per-tenant credential or `rat-callmeta-bin` tenant handling here. A notification
is delivered as-is to its `target`. Emitters MUST NOT put secrets in `title` /
`body` (I13); the core redacts, but emitters share the obligation.

## Capabilities

| Capability | RPC | Behavior |
|---|---|---|
| `rat://notifications/v1/send` | `Send` | Deliver one notification → `(delivered, message_id)`. Empty `title` → `INVALID_ARGUMENT`. |

## Severity levels

The `severity` field maps to the proto `Severity` enum:

| Name | Enum |
|---|---|
| `INFO` | `SEVERITY_INFO` |
| `WARNING` | `SEVERITY_WARNING` |
| `ERROR` | `SEVERITY_ERROR` |
| `CRITICAL` | `SEVERITY_CRITICAL` |

(`SEVERITY_UNSPECIFIED` is the proto zero value; the vectors use named levels.)

## Files

| File | Role |
|---|---|
| `store.py` | `NotificationSink` — in-memory capture + monotonic `message_id`; empty title → `INVALID_ARGUMENT` |
| `server.py` | `NotificationsServicer.Send`; wraps the sink, maps `NotificationError` → gRPC abort |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/notifications-v1.json`, drives this impl over real gRPC; asserts delivery + captured-count + error codes |

## How it's tested

The harness boots `NotificationsServicer` with a **shared** `NotificationSink`,
sends every `send` vector over gRPC (asserting `delivered` + a non-empty
`message_id`), then asserts the sink `captured` exactly the delivered cases and
spot-checks a captured title. Each `errors` vector must abort with the expected
gRPC status (`INVALID_ARGUMENT` for an empty title).

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/notifications/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-notifications-inmemory-py conformed to notifications/v1 golden vectors`.
