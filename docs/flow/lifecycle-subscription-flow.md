# Flow: Automaton lifecycle event subscriptions (Phase 3)

Design: [plans/2026-10-13-prd-lifecycle-events.md](../plans/2026-10-13-prd-lifecycle-events.md) ·
How-to: [howto/automaton-lifecycle-subscriptions.md](../howto/automaton-lifecycle-subscriptions.md)

## The pipeline

```
 ┌───────────────────────────────────────────────────────────────────────┐
 │                     Automaton manager (per-site emits)               │
 │   Run roll-up · guardrail blocks · executeOne · status transitions   │
 │   Approve/Reject/Pause · ResetTask · decompose completion · deletes  │
 └───────────────────────────────────┬───────────────────────────────────┘
                                     │  emit(LifecycleEvent{level, kind, prd_id, task_id, …})
                                     ▼
 ┌───────────────────────────────────────────────────────────────────────┐
 │                In-process lifecycle dispatcher                        │
 │   multi-subscriber fan-out · per-subscriber overflow isolation ·     │
 │   panic recovery per subscriber                                       │
 └──────┬──────────────────────┬───────────────────────┬────────────────┘
        │                      │                       │
        ▼                      ▼                       ▼
 ┌──────────────┐      ┌───────────────────┐   ┌──────────────────────────┐
 │ WS broadcast │      │ Alert store sink  │   │ Topic sink               │
 │ prd.updated  │      │ (v9.3.0)          │   │ server.PublishToTopic    │
 │ → PWA frame  │      │ datawatch alerts  │   │ → SSE / UnifiedPush /    │
 └──────────────┘      └───────────────────┘   │   APNs / ntfy            │
                                               └──────────────────────────┘
        ▲ (existing subscribers — Phase 1 WS frame + v9.3.0 sinks)
        │
        ▼
 ┌───────────────────────────────────────────────────────────────────────┐
 │           Subscription engine  (LifecycleSubscriptionEngine)          │
 │                                                                       │
 │  1. match every stored Subscription against the event:                │
 │       kinds ∋ event.kind ∧ level = event.level                        │
 │       ∧ (prd_id = ""  ∨ = event.prd_id)                               │
 │       ∧ (task_id = "" ∨ = event.task_id)                              │
 │  2. per matched sink → deliver (see below)                            │
 │  3. one_shot ⇒ delete the record only after successful delivery:      │
 │       delivery err → retry once → still err ⇒ log + drop              │
 │  4. janitor: prune expires_at elapsed; coalesce human-facing sinks    │
 │  5. live re-attach: store OnChanged → swap the active set             │
 │     (add/delete applies immediately — no daemon restart)              │
 └──────┬──────────────────┬──────────────────────┬──────────────────────┘
        │                  │                      │
        ▼                  ▼                      ▼
 ┌───────────────┐  ┌─────────────────────┐  ┌───────────────────────────┐
 │ comm sink     │  │ session sink        │  │ alert sink                │
 │ → comm router │  │ ① bridge POST      │  │ → alerts.Store.Add        │
 │   fan-out     │  │   127.0.0.1:<port> │  │   (in-app alert store)    │
 │ (routers,     │  │   /send  (5 s)     │  └───────────────────────────┘
 │  ntfy, email) │  │ ② fallback:        │
 │               │  │   SendInput,       │
 │               │  │   source tag       │
 │               │  │   "lifecycle-sub"  │
 │               │  │  (+ send_input     │
 │               │  │    action text)    │
 └───────────────┘  └─────────────────────┘
```

## State

```
 ┌────────────────────────────────────┐      ┌───────────────────────────┐
 │ <data_dir>/lifecycle_subscriptions│      │   Write surfaces (all     │
 │ .json  — secfile, encrypted at    │◄────┤   hit the same store):    │
 │ rest, modeled on                  │      │                           │
 │ discussion_subs / channel_routing │      │  REST  /api/autonomous/   │
 │                                  │      │        subscriptions      │
 │ { ID, Owner, Filter, Sink,       │      │  MCP   autonomous_*       │
 │   Action?, Coalesce, OneShot,    │      │  CLI   autonomous …       │
 │   CreatedAt, ExpiresAt? }        │      │  comm  autonomous …       │
 └────────────────────────────────────┘      │        (owner-scoped)    │
                                            └───────────────────────────┘
```

## One-shot watch-and-inject (the example that motivated this)

```
 operator                      daemon                            worker session
    │                           │                                       │
    │ autonomous subscribe      │                                       │
    │  --task T --on            │  store.Add(record) →                  │
    │  session_started          │  OnChanged → engine re-attaches       │
    │  --send "Start…"          │                                       │
    │  --sink session --one-shot│                                       │
    │◄── 201 {id,…}             │                                       │
    │                           │                                       │
    │                           │  manager spawns session for T        │
    │                           │  emit task.session_started ──────────►│ (pane ready)
    │                           │        │                              │
    │                           │        ▼                              │
    │                           │  engine matches record →             │
    │                           │  session adapter:                    │
    │                           │   ① bridge? POST :port/send ────────►│ ✅ text in,
    │                           │   ② else  SendInput "lifecycle-sub"─►│
    │                           │  success ⇒ store.Remove(record)       │
    │                           │  (one-shot consumed; audit-logged)    │
```

Contrast with the pre-Phase-3 shape: an operator script polling
`sessions list` on a 5 s cycle and racing the pane's first capture —
with a 25 s window of missed injections per the design pass. The
subscription moves the reaction *into* the event path: the daemon
delivers exactly once, at the exact emit, with a single bounded
retry.

## What this flow is NOT (yet)

- **No outbound webhook delivery** — `webhook` sink is rejected at
  creation (Phase 5: HMAC + SSRF guard + backoff).
- **No MCP pending-events queue** — generic MCP clients still read
  events via the dispatcher's WS/REST surfaces (Phase 4).
- **No cross-host delivery** — subscriptions resolve sinks on the
  daemon that owns the Automaton; federation forwarding of lifecycle
  events is an open question in the design pass, not built here.
- **No PWA subscription UI** — the PWA editor is Phase 4 of the same
  design pass; Android/iOS receive events through the existing
  (v9.3.0) topic-push path.
