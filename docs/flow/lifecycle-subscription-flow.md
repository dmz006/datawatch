# Flow: Automaton lifecycle event subscriptions

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
        │                  │                      │                  │
        ▼                  ▼                      ▼                  ▼
 ┌───────────────┐  ┌─────────────────────┐  ┌─────────────────┐  ┌──────────────────┐
 │ comm sink     │  │ session sink        │  │ alert sink      │  │ mcp_queue sink   │
 │ → comm router │  │ ① bridge POST      │  │ → alerts.Store  │  │ → PendingEvent-  │
 │   fan-out     │  │   127.0.0.1:<port> │  │   .Add (in-app  │  │   Queue.Append   │
 │ (routers,     │  │   /send  (5 s)     │  │   alert store)  │  │   (named ring,   │
 │  ntfy, email) │  │ ② fallback:        │  └─────────────────┘  │   in memory,     │
 │               │  │   SendInput,       │                       │   bounded)       │
 │               │  │   source tag       │                       └────────┬─────────┘
 │               │  │   "lifecycle-sub"  │                                │ poll
 │               │  │  (+ send_input     │                                ▼
 │               │  │    action text)    │                       (pending-events path
 └───────────────┘  └─────────────────────┘                        below)

 ┌───────────────────────────────┐  ┌───────────────────────────────────────────┐
 │ webhook sink  (ships OFF)     │  │ peer sink  (ships OFF)                    │
 │ → WebhookSink.Deliver         │  │ → PeerForwarder.Forward                   │
 │   guard · sign · POST https   │  │   registry lookup · autonomous:write ·    │
 │   (see webhook path below)    │  │   sign · POST /api/autonomous/events/     │
 └───────────────────────────────┘  │   forward  (see peer path below)          │
                                    └───────────────────────────────────────────┘
```

## Outbound webhook and peer forwarding (both ship disabled)

Both sinks are inert until their switch is on: `autonomous.notify.webhook.enabled`
and `autonomous.notify.forwarding.enabled`. With a switch off, creation is refused
and a stored subscription delivers nothing. Design and the values still pending
confirmation: [plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md](../plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md).
Operator guide: [howto/automaton-lifecycle-webhooks.md](../howto/automaton-lifecycle-webhooks.md).

### Webhook path

```
 matched webhook subscription { sink: webhook, target: https://…, secret_ref: ${secret:name} }
        │
        ▼
 settings read live (enabled, allow_hosts, retry curve, body cap)
        │  disabled ─────────────────────────────────────► nothing sent
        ▼
 ValidateWebhookURL: https (http only if allow-listed), no userinfo,
   host on allow_hosts, no obfuscated IP spelling
        │  refused ─► audit (refused) · no retry
        ▼
 resolve ${secret:name} from the vault  (every delivery; never stored/logged)
        │  unresolved ─► audit (failed) · no retry
        ▼
 build ONE envelope { version, delivery_id, sent_at, host, subscription_id,
                      event, truncated?, forward? }  · cap 64 KiB
        │  over cap after dropping decision/verdict ─► audit (failed) · no retry
        ▼
 ┌─────────────── attempt n (timeout 10 s, Client: dialer re-checks IP,
 │                            no redirects, no proxy) ──────────────┐
 │  sign: X-Datawatch-Signature  t=<now>,v1=HMAC-SHA256(secret, t.body)
 │  headers: X-Datawatch-Delivery (same on every retry), X-Datawatch-Event
 │  2xx ─────────────────────────────► delivered (audit)            │
 │  network / timeout / 408 / 429 / 5xx ─► retry after 1,4,16,64 s  │
 │                                       (±20 % jitter; 429 Retry-After)
 │  other 4xx / 3xx / guard refusal ──► failed (audit, no retry)    │
 └──────────────────────────────────────────────────────────────────┘
        │  attempts exhausted
        ▼
 one alert "Lifecycle webhook delivery failed" (error) + audit (failed).
 Subscription stays armed; a one-shot is removed only after a 2xx.
```

### Peer path

```
 matched peer subscription { sink: peer, target: <registered peer name> }
        │
        ▼
 registry lookup: exists · Enabled · Federated · capability autonomous:write
        │  refused ─► audit (refused) · no retry
        │  event arrived forwarded? ─► refused: never forwarded on
        ▼
 hop chain: origin = this host, hops = 1, visited = [this host]  (X-Datawatch-Hop-Chain)
        │
        ▼
 POST <peer url>/api/autonomous/events/forward
   Authorization: Bearer <peer token>
   X-Datawatch-Signature: HMAC keyed with the peer token (same format as webhook)
        │
        ▼                       ─────────── receiving daemon ───────────
 bearer auth → autonomous:write for the presenting peer → signature
   → envelope shape/size → loop rules:
       origin == local host ─────────────► 409 (refused, audited, not retried)
       local host in visited ────────────► 409
       hops at the cap ──────────────────► 409
   → 202 accepted: re-emit into the LOCAL dispatcher, marked forwarded
        (every local sink sees it, text ends "(forwarded from <origin>)";
         never written to executor state; never driving the coordinator;
         never forwarded on)
   → 503 when forwarding is off or the receiver is not wired (sender retries)
   → 401 / 403 / 400 are permanent at the sender (no retry)
        │
        ▼  sender: 5xx and transport errors retried on the webhook schedule;
           exhausted ─► one alert "Lifecycle peer forward failed" + audit.
           No outbox: an offline peer loses the event (the Automaton state
           remains readable through the API).
```

## The pending-events queue path (generic MCP clients)

```
 subscription { sink: mcp_queue, target: "ops-watch" }
        │ matching event
        ▼
 PendingEventQueue.Append("ops-watch", event)  → cursor n (monotonic, shared)
        │   ring of at most autonomous.notify.pending_events_max (default 500);
        │   over the bound the oldest entry is evicted and the ring remembers
        │   the newest evicted cursor
        ▼
 client poll ─ MCP autonomous_events_pending(queue, since_cursor, limit)
             ─ REST GET /api/autonomous/events?queue=&since=&limit=
             ─ CLI  datawatch autonomous events --queue …
             ─ comm autonomous events queue=… since=…  (owner-checked)
        │
        ▼
 { events: [ {cursor, event} … newer than since, oldest first, ≤ limit ],
   next_cursor: n,            ← pass back as since on the next poll
   dropped: true|false }      ← true when unread events were evicted

 daemon restart → queues are empty, cursors restart at 1 (the subscription persists)
```

## Update and test-fire

```
 PUT  /api/autonomous/subscriptions/{id}        ─► store.Update (replace mutable fields,
 (MCP autonomous_subscription_update,               keep id + created_at) → OnChanged →
  CLI subscription-update, comm, PWA Edit)          engine re-attaches live

 POST /api/autonomous/subscriptions/{id}/test   ─► engine.FireTest(id): one synthetic
 (MCP autonomous_subscription_test,                 "[test] …" prd.updated event to THAT
  CLI subscription-test, comm, PWA Test)            subscription's sink only — no action
                                                    text, one-shot not consumed, no
                                                    coalescing → {ok, sink, error}
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

- **No durable queue** — pending events are in memory only and are lost
  on daemon restart; the bound drops the oldest first. Outbound webhook and
  peer deliveries have no outbox either: a retry in progress is lost on
  restart, and a peer that stays offline loses the event.
- **No WebSocket payload enrichment or live toasts** — the WS
  `prd_update` frame keeps its `{prd_id, status}` shape; follow-up work.
- **Outbound webhook and peer forwarding are off by default** — both
  switches ship disabled until the operator confirms the proposal.
- **No Android/iOS subscription UI yet** — the PWA has a Subscriptions
  tab on the Automaton detail view; the apps receive events through the
  existing (v9.3.0) topic-push path and their management UI is tracked
  separately.
