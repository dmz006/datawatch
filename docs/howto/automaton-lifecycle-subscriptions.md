---
docs:
  index: true
  topics: [autonomous, lifecycle, subscriptions, events]
exec_params:
  - {name: kinds, required: false, default: "task.session_started", description: "Comma-separated lifecycle event kinds to watch"}
  - {name: level, required: false, default: "task", description: "Event level: prd or task"}
  - {name: sink, required: false, default: "topic", description: "Sink type: comm | session | topic | alert"}
  - {name: target, required: false, default: "lifecycle-howto", description: "Sink target (session id, topic, router or alert name)"}
exec_steps:
  - tool: autonomous_subscriptions
    description: List the currently persisted lifecycle event subscriptions
    args: {}
    read_only: true
  - tool: autonomous_subscribe
    description: Register a one-shot subscription for the chosen kinds (it self-deletes after its first delivery)
    args:
      kinds: "{{params.kinds}}"
      level: "{{params.level}}"
      sink_type: "{{params.sink}}"
      sink_target: "{{params.target}}"
      one_shot: true
    read_only: false
  - tool: autonomous_subscriptions
    description: Confirm the new subscription is registered
    args: {}
    read_only: true
---
# How-to: Automaton lifecycle event subscriptions

Automata emit lifecycle events as they move — PRD and task status
changes (`prd.blocked`, `task.completed`, …), worker session
start/clear (`task.session_started`, `task.session_cleared`), and PRD
updates (`prd.updated`). A **subscription** is one persisted rule:
*when this event fires, deliver it there*. Register it once and the
daemon handles the rest — no session-list polling, no racing your
scripts against the worker pane.

## What it solves: the watch-and-inject polling script

The classic pattern: a task spawns its worker session, and you want
text typed into that session the moment it exists. Before:

```sh
# old: poll and race — the pane may not be ready when you inject
while ! datawatch sessions list | grep -q "$TASK_ID"; do sleep 5; done
datawatch session-ops send "$SESSION" "Start with the Setup section of TASK.md"
```

After — one rule, registered ahead of time:

```sh
datawatch autonomous subscribe \
  --task "$TASK_ID" --on session_started \
  --send "Start with the Setup section of TASK.md" \
  --sink session --target "$SESSION" --one-shot
```

When the task's worker session starts, the daemon delivers the text to
the session and the subscription deletes itself (one-shot). If you
missed the window and the one-shot is already gone, register it again
from the *next* event boundary — the rule is declarative, not
positional.

## Base requirements

- `datawatch start` — a daemon with the lifecycle engine wired (any
  current release; the engine is on by default).
- For `--sink session`: a datawatch-managed session as the target (it
  may not exist yet — delivery happens when the event fires).
- For `--sink comm`: at least one comm router backend configured.

## The subscription model

Every subscription is **filter → sink → optional action**:

| Part | What it is |
|---|---|
| **Filter** | `kinds` (event kinds; repeatable) and/or `level` (`prd` or `task`), plus optional scope to one Automaton (`prd_id`) or one task (`task_id`). At least one of `kinds` / `level` is required. |
| **Sink** | One of: `comm` (router fan-out), `session` (a datawatch session), `topic` (SSE / mobile-push topic bus), `alert` (in-app alert store). Each has a `target`. |
| **Action** *(optional)* | `send_input` with text (max 4096 chars), only with a `session` sink — the text is typed into the target session on every match. |
| **Flags** | `one_shot` (delete after first *successful* delivery), `expires_at` (TTL), `coalesce` (batch human-facing deliveries in quiet windows). |

Event kinds are a closed set: every PRD status kind (`prd.draft`,
`prd.planning`, `prd.needs_review`, `prd.completed`, `prd.blocked`,
…), every task status kind (`task.pending`, `task.queued`,
`task.in_progress`, `task.completed`, `task.failed`, `task.blocked`,
…), plus the non-status kinds `task.session_started`,
`task.session_cleared` and `prd.updated`. The daemon rejects unknown
kinds at creation (`"filter kind %q is not a known event kind"`), so a
typo fails fast instead of silently never firing.

Note: the CLI/comm shorthand `prd_updated` currently resolves to
`task.prd_updated`, which is **not** in the known-kind set — use the
full form `--on prd.updated` for PRD-update notifications.

## Quickstart (CLI)

```sh
# One-shot watch-and-inject (the polling-script replacement):
datawatch autonomous subscribe \
  --task "$TASK_ID" --on session_started \
  --send "Pick up the brief in TASK.md — Start with its Setup section." \
  --sink session --target "$SESSION" --one-shot
```

Notes:

- `--on` is repeatable; shorthand words map to task kinds
  (`session_started` → `task.session_started`,
  `session_cleared` → `task.session_cleared`); full kinds containing a
  dot pass through (`prd.blocked`, `prd.updated`).
- `--task` implies `--one-shot` by default (that is the
  watch-and-inject shape); pass `--one-shot=false` to keep it.
- `--send` requires `--sink session` and sets the action text.
- `--target` for a session sink may be a session id; if omitted the
  CLI falls back to `$DATAWATCH_SESSION_ID` (subscribe *from* the
  target session).
- `--expires <seconds>` sets a TTL (`0` = never).
- `--coalesce` batches human-facing sink deliveries in quiet windows.

Other examples:

```sh
# Notify a comm router whenever any Automaton reaches blocked (kept
# forever, delivery batched):
datawatch autonomous subscribe --on prd.blocked --sink comm --target my-router --coalesce

# One Automaton's completions to the alert store:
datawatch autonomous subscribe --on prd.completed --prd <prd-id> --sink alert --target system

# List, then delete by id:
datawatch autonomous subscriptions [--owner operator]
datawatch autonomous unsubscribe <subscription-id>
```

## MCP

The MCP tools proxy the daemon's REST surface on loopback — the daemon
store is the source of truth, so nothing is lost when an MCP host
restarts:

- `autonomous_subscribe(kinds, level, prd_id, task_id, sink_type,
  sink_target, action_text?, one_shot?, coalesce?,
  expires_in_seconds?)` — `kinds` accepts a comma-separated string or a
  JSON array; returns the stored record (with its `id`).
- `autonomous_unsubscribe(id)`
- `autonomous_subscriptions()` — list everything (filterable by owner
  on the REST route).

## Comm channel (owner-scoped)

From any configured comm channel:

```
autonomous subscribe task.session_started prd=<prd-id>
autonomous subscriptions
autonomous unsubscribe <id>
```

The calling channel is **both the owner and the sink target**. That is
deliberate: a channel can only list and un-subscribe *its own*
subscriptions — it can never see or remove another channel's.

## REST

```
GET    /api/autonomous/subscriptions[?owner=<owner>]   (autonomous:read)
POST   /api/autonomous/subscriptions                    (autonomous:write) → 201 + record
DELETE /api/autonomous/subscriptions/{id}               (autonomous:write)
```

`503` when the lifecycle engine is not wired (bare daemon / unit test
contexts). `400` on a malformed or unknown-kind payload, `404` on an
unknown id.

Example create body:

```json
{
  "owner": "operator",
  "filter": {"kinds": ["task.session_started"], "level": "task", "task_id": "task_7"},
  "sink": {"type": "session", "target": "5a9e0c41"},
  "action": {"type": "send_input", "text": "Start with the Setup section of TASK.md."},
  "one_shot": true,
  "expires_at": "2026-10-21T23:59:59Z"
}
```

## Delivery to a session: bridge preferred, SendInput fallback

When a subscription with a `session` sink fires, the daemon:

1. **Bridge first** — if the target session has a channel bridge with a
   local port (a live Signal/Telegram/etc. bridge for that session),
   it POSTs the message to the bridge's `http://127.0.0.1:<port>/send`
   (5 s timeout).
2. **Fallback** — port 0, POST error, or no bridge at all: it types the
   text into the session's input via `SendInput`, tagged with source
   `lifecycle-sub` (visible in the session's input history so you can
   tell daemon-typed text from operator-typed text).

A failed one-shot delivery is retried once; after that the
subscription is dropped and the failure is logged (the daemon never
holds a one-shot hostage to a dead target).

## One-shot, expiry, coalesce

- **`one_shot`** — deleted only after *successful* delivery; see above
  for the failure behavior.
- **`expires_at`** — a janitor prunes expired subscriptions
  continuously; they stop matching as soon as the timestamp elapses.
- **`coalesce`** — for human-facing sinks (comm/alert) deliveries are
  batched within a quiet window instead of firing one message per
  event.

## Storage, audit & restart behavior

- Persisted at `<data_dir>/lifecycle_subscriptions.json`, encrypted at
  rest with the daemon's secfile key (same treatment as channel
  routing rules). It survives daemon restarts — re-apply after
  migrating the data dir.
- Create/delete are audit-logged. Per-event delivery is intentionally
  not audited (high-frequency, same as discussion `subscribe`
  deliveries) — inspect the daemon log (`lifecycle-sub` lines) for
  delivery failures.
- **No daemon restart needed** to apply changes: the engine
  re-attaches to the store immediately on any add/remove
  (`OnChanged`), and the REST/MCP/CLI/comm surfaces all hit the same
  store.

## Troubleshooting

- **`503` from the REST route** — this daemon build's engine isn't
  wired (unit-test or stripped-down context); use a normal daemon.
- **`filter kind "…" is not a known event kind`** — use the full kind
  (`prd.updated`, `task.session_started`); check the closed set above.
- **Session sink never fires** — the event kind or task scope doesn't
  match what actually happens (e.g. subscribed to
  `task.session_cleared` instead of `session_started`), or the
  one-shot already consumed its event. List with
  `datawatch autonomous subscriptions` and re-register for the next
  boundary if needed.
- **Text arrived but looks wrong** — verify the `lifecycle-sub`
  source tag in the session input history; `send_input` types verbatim,
  4096-char cap.
- **`webhook` / `mcp_queue` sinks are refused** — by design in this
  release; they arrive in a later phase (outbound webhooks, MCP
  pending-events queue).

## See also

- [flow/lifecycle-subscription-flow.md](../flow/lifecycle-subscription-flow.md) — event → dispatcher → subscription engine → sink
- [Design pass](../plans/2026-10-13-prd-lifecycle-events.md) — phases, parity surface, open questions
- [datawatch-definitions.md](../datawatch-definitions.md) — "lifecycle event subscription" glossary entry
- [How-to: autonomous planning](autonomous-planning.md) — the Automaton lifecycle the events describe
