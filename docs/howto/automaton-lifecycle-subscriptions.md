---
docs:
  index: true
  topics: [autonomous, lifecycle, subscriptions, events]
exec_params:
  - {name: kinds, required: false, default: "task.session_started", description: "Comma-separated lifecycle event kinds to watch"}
  - {name: level, required: false, default: "task", description: "Event level: prd or task"}
  - {name: sink, required: false, default: "topic", description: "Sink type: comm | session | topic | alert | mcp_queue"}
  - {name: target, required: false, default: "lifecycle-howto", description: "Sink target (session id, topic, router or alert name, or the mcp_queue queue name)"}
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
| **Filter** | `kinds` (event kinds; repeatable) and/or `level` (`prd` or `task`), plus optional scope to one Automaton (`prd_id`), one task (`task_id`) or one federation peer (`peer`, see [Events from a federation peer](#events-from-a-federation-peer)). At least one of `kinds` / `level` is required. |
| **Sink** | One of: `comm` (router fan-out), `session` (a datawatch session), `topic` (SSE / mobile-push topic bus), `alert` (in-app alert store), `mcp_queue` (a named pending-events queue that a client polls). Each has a `target`; for `mcp_queue` it is the queue name. |
| **Action** *(optional)* | `send_input` with text (max 4096 chars), only with a `session` sink — the text is typed into the target session on every match. |
| **Flags** | `one_shot` (delete after first *successful* delivery), `expires_at` (TTL), `coalesce` (batch human-facing deliveries in quiet windows). |

Event kinds are a closed set held in one in-daemon **event kind
registry**: every Automaton status kind (`prd.draft`, `prd.planning`,
`prd.needs_review`, `prd.completed`, `prd.blocked`, …), every task status
kind (`task.pending`, `task.queued`, `task.in_progress`, `task.completed`,
`task.failed`, `task.blocked`, …), the non-status kinds
`task.session_started`, `task.session_cleared`, `prd.updated`,
`question.answered` and `question.withdrawn`, the opt-in warning kinds
`scope_warning` and `guardrail_warn`, and the opt-in supervisor kinds
described under *Events for an event-driven supervisor* below. Each
registry entry has a one-line description and a group (automaton, task,
review, capacity, budget); the full table is in the
[lifecycle reference](../api/autonomous.md#lifecycle-event-kinds). The
daemon rejects unknown kinds at creation (`"filter kind %q is not a known
event kind"`), so a typo fails fast instead of silently never firing. A
plugin can register further kinds and sink types at start-up (see the
reference); they validate like the built-in ones.

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

# Park matching events in a queue a generic MCP client can poll:
datawatch autonomous subscribe --on task.failed --on prd.blocked --sink mcp_queue --target ops-watch

# List, change, test, then delete by id:
datawatch autonomous subscriptions [--owner operator]
datawatch autonomous subscription-update <subscription-id> --on prd.blocked --sink comm --target my-router
datawatch autonomous subscription-test <subscription-id>
datawatch autonomous unsubscribe <subscription-id>
```

`subscription-update` takes the same flags as `subscribe` and **replaces**
the whole configuration: anything you leave out is cleared, so pass
everything you want to keep. Unlike `subscribe`, `--task` does not imply
`--one-shot`.

## Events for an event-driven supervisor

A supervisor session or script used to poll for the states that need a
decision: an Automaton that finished every task but never left `running`, a
branch waiting to be merged, a task stuck waiting for a lane, a judge that
blocked, a burn guard that stepped down. Each of those is now an event kind,
so one subscription wakes the supervisor and the schedule becomes the
fallback. All of them are **opt-in** (add them to the notify set, next
section) and informational: none of them changes a status, a verdict or a
gate outcome.

| Kind | Fires when | Once per |
|---|---|---|
| `automaton.stuck_running` | the stuck-running watchdog sees an Automaton `running` with every task terminal, no live session and no executor | condition: once when it starts to hold, again only after it cleared |
| `automaton.merge_ready` | a finished Automaton's work branch is waiting to be merged (the merge-pending pass) | condition, as above |
| `automaton.tasks_done_or_cancelled` | a running Automaton has at least one task and every task is `completed` or `cancelled` | condition, as above |
| `task.capacity_wait_too_long` | a task has waited for a worker lane longer than half of `autonomous.capacity_wait_timeout_seconds` (default wait 4 h, so after 2 h); `details` says how long | waiting episode: a later wait can fire again |
| `burn_guard.rung_changed` | the burn guard's rung in force changed; `old_status` / `new_status` carry the rungs (empty = none applied) | change |
| `judge.verdict_block`, `judge.verdict_warn`, `judge.verdict_pass` | a guardrail judge verdict with that outcome was recorded; same payload as `guardrail_warn` (`guardrail`, `verdict`, `decision`) and the same level rule (task, story or Automaton) | verdict |
| `task.final_gate_failed` | a final gate task finished without passing; `details` carries the reason | gate failure |

`burn_guard.rung_changed` belongs to no Automaton, so only the global tier
(`autonomous.notify.default_events`) can allow it.

Worked example — wake a supervisor session on the states that need it:

```sh
# 1. allow the kinds globally (lowest tier; every Automaton inherits them)
datawatch config set autonomous.notify.default_events \
  "prd.needs_review,prd.blocked,prd.failed,prd.completed,prd.cancelled,automaton.stuck_running,automaton.merge_ready,judge.verdict_block,task.capacity_wait_too_long,burn_guard.rung_changed"

# 2. one subscription types a wake-up line into the supervisor session
datawatch autonomous subscribe \
  --on automaton.stuck_running --on automaton.merge_ready \
  --on judge.verdict_block --on task.capacity_wait_too_long \
  --on burn_guard.rung_changed \
  --send "A supervisor event arrived; read the Automata events and act." \
  --sink session --target "$SUPERVISOR_SESSION" --one-shot=false --coalesce
```

The global default-events change applies at the next daemon start; the
per-Automaton and profile tiers apply at once. A queue sink
(`--sink mcp_queue`) works the same way for a supervisor that polls.

## Find the kinds and preview a filter

You do not need this page's tables to pick kinds: the daemon lists them, and
for a filter you can ask how much it would have caught before you save it.

**List every kind.** The list is the daemon's own registry, so it includes
kinds a plugin registered. Each kind shows its group, its one-line
description and whether it is in the default set that wakes the supervisor.

| Surface | How |
|---|---|
| CLI | `datawatch autonomous event-kinds`; add `--group task` to keep one group or `--json` for the raw body. |
| Comm channel | `autonomous event-kinds`, or `autonomous event-kinds group=task`. |
| MCP | the `autonomous_event_kinds` tool, no parameters. |
| REST | `GET /api/autonomous/event-kinds`. |
| Web UI | the kind checkboxes in **Add subscription** (see the PWA walkthrough below). |

**Preview a filter.** Over REST, post the filter to
`POST /api/autonomous/subscriptions/preview`:

```sh
curl -s -X POST "$DATAWATCH/api/autonomous/subscriptions/preview" \
  -H 'Content-Type: application/json' \
  -d '{"kinds":["task.failed","automaton.stuck_running"],"owner":"<automaton-id>"}'
# {"count":3,"window_hours":24,"sample":[ ...newest matches, at most 20... ],"lifecycle_since":"2026-11-12T08:00:00Z"}
```

The preview stores nothing and changes no subscription. `kinds` takes the
lifecycle kinds that `GET /api/autonomous/event-kinds` lists, which are the
ones the picker shows, and also the kinds of the stream the **Events** tab shows
(see [Automata events](automata-events.md)): `watch`, `decision`, `retry`,
`check`, `cleanup`, `alert` and `error`. A kind that is neither is refused with
a `400` (`unknown kind`). An empty list means every Events log kind, `owner`
narrows to one Automation or Automaton, and `project` narrows the Events log
kinds. It does not apply the notify set.

**Review before relying on it.** A lifecycle kind is counted from the
lifecycle events this daemon has recorded in memory since it started, and
`lifecycle_since` in the answer says when that recording began. Right after a
restart a low count may mean a short history rather than a quiet day. There is
no CLI, comm or MCP form of the preview.

## Test a subscription before you rely on it

A subscription that never fires is hard to tell from one that has
nothing to fire on. **Test-fire** sends one labelled event to just that
subscription's sink, now:

```sh
datawatch autonomous subscription-test <subscription-id>
```

The message text starts with `[test] ` and describes a synthetic
`prd.updated` event, so you can see it arrive in the comm channel,
session, topic, alert list or queue (in a queue it appears as an event
with `"test": true`). A test never types the
subscription's `send_input` action text and never uses up a one-shot, so
it is safe on a live rule. The answer is `ok` with the sink type, or an
error such as `sink not available` when that delivery path is not wired
in this daemon.

## PWA walkthrough

Open an Automaton in the PWA (**Automata** → the Automaton) and choose the
**Subscriptions** tab.

1. **Read the list.** The tab shows every subscription that applies to this
   Automaton: rules scoped to it, plus unscoped rules that fire for every
   Automaton ("all automata"). Each row shows the event kinds or level, the
   sink and target, and badges for one-shot, coalesce, expiry and owner.
2. **Add.** Choose **Add subscription**. Tick at least one event kind or
   pick a level. The picker loads every kind from
   `GET /api/autonomous/event-kinds` and shows each with its one-line
   description, grouped Automaton, Task, Review, Capacity and Budget. If the
   list cannot be loaded, a note says so and the built-in list is shown. The
   line under the picker asks `POST /api/autonomous/subscriptions/preview` for
   "Would have matched in the last day: N"; it refreshes shortly after each
   tick and is skipped while nothing is ticked. A refused or failed preview
   shows "Preview unavailable" and never blocks saving. Read
   [Find the kinds and preview a filter](#find-the-kinds-and-preview-a-filter)
   before trusting the count: it covers the events since the daemon started.
   The **?** beside "Event kinds" opens this page.
   Choose the scope (this Automaton or all automata), an optional task id,
   the sink type and its target, and optionally send-input text (session
   sink only), one-shot, coalesce and an expiry in hours. The form checks
   the same limits as the daemon (queue name characters and length, 4096
   characters of action text) before it saves.
3. **Test.** Choose **Test** on a row. A line under the tab header reports
   "Delivered via <sink>" or why the test failed.
4. **Edit.** Choose **Edit**, change what you need and save. The edit
   replaces the rule's configuration; the id is kept.
5. **Delete.** Choose **Delete** and confirm.

If the tab says "Lifecycle subscriptions are disabled on this server.",
the daemon's subscription engine is not wired (see Troubleshooting). The
tab is loaded when you open it and after each change; it does not poll.

## A generic MCP client: the pending-events queue

An MCP host that cannot receive pushes (no session to type into, no
comm channel) can still follow Automaton events by **polling a queue**.

1. **Register a queue subscription** (once). The sink target is the
   queue name — letters, digits and `. _ : -`, at most 64 characters:

   ```sh
   datawatch autonomous subscribe --on task.completed --on task.failed \
     --on prd.blocked --sink mcp_queue --target ops-watch
   ```

   From an MCP client: `autonomous_subscribe(kinds="task.completed,task.failed,prd.blocked", sink_type="mcp_queue", sink_target="ops-watch")`.

2. **Poll with a cursor.** The first call uses cursor 0:

   `autonomous_events_pending(queue="ops-watch", since_cursor=0)`

   returns

   ```json
   {"events":[{"cursor":1,"event":{"kind":"task.completed","prd_id":"…"}}],"next_cursor":1,"dropped":false}
   ```

   (shape abbreviated). Handle the events, then pass `next_cursor` back as
   `since_cursor` on the next call. `limit` defaults to 50 and is capped at
   200. The same data is available over REST
   (`GET /api/autonomous/events?queue=ops-watch&since=1`), the CLI
   (`datawatch autonomous events --queue ops-watch --since 1 [--json]`) and a
   comm channel (`autonomous events queue=ops-watch since=1`).

3. **Watch for `dropped`.** Each queue is bounded
   (`autonomous.notify.pending_events_max`, default 500) and evicts the
   oldest events first. `dropped: true` means events you had not read yet
   were evicted — re-read the Automaton's state instead of assuming the
   history is complete. Queues live in memory only, so after a daemon
   restart the queue is empty and cursors start again from 1; the
   subscription itself survives. Deleting the last subscription that
   feeds a queue discards the events it still held.

Poll no faster than you need; there is no push. For immediate delivery
use a `session`, `comm` or `topic` sink instead.

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
- `autonomous_subscription_update(id, …)` — same optional fields as
  `autonomous_subscribe`; replaces the configuration and returns the
  updated record.
- `autonomous_subscription_test(id)` — test-fire; returns
  `{"ok","sink","error"}`.
- `autonomous_prd_set_notify_events(id, notify_events)` — set the
  Automaton's own notify-event kinds (a non-empty list), or clear them
  (empty/omitted — the Automaton then inherits its guardrail profile or
  the global default set). See *Choosing which events notify* below; the
  response carries the resolved set with its source tier.
- `autonomous_events_pending(queue, since_cursor?, limit?)` — poll an
  `mcp_queue` queue (see above).
- `autonomous_event_kinds()` — list every kind a subscription can match, with
  its group, description and whether it wakes the supervisor by default
  (see *Find the kinds and preview a filter* above).

### MCP resource subscriptions: a push path that stores nothing

A client that can receive MCP notifications has a second way in. With
`mcp.resource_subscriptions: true` (default `false`; restart to apply) it calls
`resources/subscribe` on the lifecycle events resource and gets a
`notifications/resources/updated` message each time a matching event fires:

```
datawatch://events/lifecycle?kinds=task.failed,automaton.stuck_running&level=task
```

It is additive. The subscription is one more consumer of the same dispatcher
your stored subscriptions use, so the filter rules, the kind list and the
notify-set gate are identical, and nothing is written to the subscription
store, a sink or a queue. It belongs to the MCP session: it ends on
`resources/unsubscribe` or when the session closes, and a session can hold at
most 32. Existing delivery (comm, session, topic, alert and queue sinks, and
the supervisor wake) behaves the same with the setting on or off.

The notification carries the subscribed `uri` and the event's `event_id`,
`kind`, `prd_id` and `task_id`; read the resource for the recent events. Add
`&peer=<registered peer name>` to the URI to receive only the events that
federation peer forwarded (see
[Events from a federation peer](#events-from-a-federation-peer)). With the setting off the server
advertises nothing new and refuses `resources/subscribe`. The full parameter
list and the places to set it are in the [MCP reference](../mcp.md#resource-subscriptions).
Because nothing is stored, a client that is offline when an event fires does
not get it later; use an `mcp_queue` subscription (above) when it must not
miss one.

## Comm channel (owner-scoped)

From any configured comm channel:

```
autonomous subscribe task.session_started prd=<prd-id>
autonomous subscriptions
autonomous subscription-update <id> kinds=prd.blocked,prd.failed coalesce=true
autonomous subscription-test <id>
autonomous events [queue=<name>] [since=<cursor>]
autonomous unsubscribe <id>
autonomous set-notify-events <prd-id> <kind,kind,...|inherit>
```

A guardrail profile's own list travels under the `guardrail` verb
namespace: `guardrail profile notify-events <id> <kind,kind,...|inherit>`
(`inherit` clears it).

In `subscription-update`, fields you do not name keep their stored value
(keys: `kinds`, `level`, `prd`, `task`, `coalesce`, `one_shot`, `expires`;
`prd=`, `task=` and `expires=0` clear). `events` defaults the queue to one
derived from the calling channel.

The calling channel is **both the owner and the sink target**. That is
deliberate: a channel can only list and un-subscribe *its own*
subscriptions — it can never see, change, test or remove another
channel's, and it can only read queues its own subscriptions feed.

## REST

```
GET    /api/autonomous/subscriptions[?owner=<owner>]   (autonomous:read)
POST   /api/autonomous/subscriptions                    (autonomous:write) → 201 + record
PUT    /api/autonomous/subscriptions/{id}               (autonomous:write) → 200 + record
POST   /api/autonomous/subscriptions/{id}/test          (autonomous:write) → 200 {"ok","sink","error"}
DELETE /api/autonomous/subscriptions/{id}               (autonomous:write)
GET    /api/autonomous/events?queue=<name>[&since=<cursor>][&limit=<n>]   (autonomous:read)
POST   /api/autonomous/prds/{id}/set_notify_events      (autonomous:write) → 200 + record (with notify_events_effective)
```

`503` when the lifecycle engine is not wired (bare daemon / unit test
contexts). `400` on a malformed or unknown-kind payload (or a missing
`queue`, non-numeric `since` / `limit` on events), `404` on an unknown id.

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

## One-shot, expiry, coalesce, and how delivery works

- **`one_shot`** — deleted only after *successful* delivery; see above
  for the failure behavior.
- **`expires_at`** — a janitor prunes expired subscriptions
  continuously; they stop matching as soon as the timestamp elapses.
- **`coalesce`** — for human-facing sinks (comm/alert) deliveries are
  batched within a quiet window instead of firing one message per
  event.
- **Delivery** — this is not an option but how every subscription
  delivers. Each subscription consumes from its own bounded in-memory
  queue (depth 256) on its own goroutine; emitting an event never waits
  on any subscriber. A subscriber whose queue is full misses events of
  its own (its own dropped counter rises and the daemon logs a
  rate-limited warning, at most one per subscriber per 10 seconds) while
  every other subscriber keeps receiving — one slow sink can never stall
  the others, the emitting Automaton or the event itself.

## Choosing which events notify

A subscription filter decides which *matching* events a subscriber gets;
the **notify set** decides which event kinds an Automaton notifies for at
all. A subscription for a kind that is not in its Automaton's resolved
set never fires — the filter can only *narrow* what the set already
allows, a subscription can never *widen* it.

The resolved set has three tiers, highest first:

1. the Automaton's own `notify_events` list;
2. the `notify_events` list of its assigned guardrail profile;
3. the global default `autonomous.notify.default_events` — unset means
   the shipped five-kind baseline:
   `prd.needs_review, prd.blocked, prd.failed, prd.completed,
   prd.cancelled`.

The first **non-empty** tier wins. At tiers 1–2 an empty (or missing)
list means *inherit*, never *off*; only the global tier can resolve to
"no kinds" (explicit `default_events: []`). Each resolved answer also
names its **source**: `automaton`, `profile` or `global` — that is
what the surfaces show as `notify_events_effective`.

`task.session_started`, `task.session_cleared`, `scope_warning`,
`guardrail_warn` and the supervisor kinds (`automaton.stuck_running`,
`automaton.merge_ready`, `automaton.tasks_done_or_cancelled`,
`task.capacity_wait_too_long`, `burn_guard.rung_changed`,
`judge.verdict_block`, `judge.verdict_warn`, `judge.verdict_pass`,
`task.final_gate_failed`) are **opt-in** kinds and are not in the default
set: a subscription on one of them fires only on Automata whose resolved set
includes that kind (add it to the per-Automaton, profile or global
default list). The PWA marks each of them *(opt-in)* in the kind pickers. `scope_warning` (a task touched files outside its declared
scope; the event lists the files) and `guardrail_warn` (a guardrail
returned a warn outcome; the event names the guardrail and carries the
level: task, story or Automaton) are bare kind names with no `prd.` or
`task.` prefix. They are informational only and never block or change any
task, story or Automaton. The usable closed set is every known
lifecycle event kind except `prd.updated`, which is deliberately not a
notify kind. Unknown or blank kinds are rejected (400 / a tool error).

**Per-Automaton** (one Automaton, everything else unchanged):

```sh
# CLI — list wins; `inherit` (or omitting the kinds) clears the list
datawatch autonomous prd-set-notify-events <id> prd.blocked,task.session_started
datawatch autonomous prd-set-notify-events <id> inherit

# Comm
autonomous set-notify-events <prd-id> prd.blocked,prd.failed
```

plus MCP `autonomous_prd_set_notify_events(id, notify_events)`, REST
`POST /api/autonomous/prds/{id}/set_notify_events` with a
`{"notify_events": [...]}` body (an absent key, `null` or `[]` is a
clear), and the PWA settings on the Automaton view — the
"Notification events" panel with an inherit toggle, grouped
kind checkboxes and the resolved set + source shown after save.

**Per guardrail profile** (every Automaton assigned to it):

```sh
# CLI
datawatch guardrail profile create --name ops-watch \
  --guardrails ci-watch --notify-events prd.blocked,prd.failed
datawatch guardrail profile update <id> --notify-events prd.failed,task.session_started

datawatch guardrail automaton set <automaton-id> --profile ops-watch

# Comm
guardrail profile notify-events <id> prd.blocked,prd.failed
guardrail profile notify-events <id> inherit
```

plus the `notify_events` field in the profile create/update body on
REST, the `guardrail_profile_create` / `guardrail_profile_update` MCP
tools, and the profile editor in PWA **Settings → Automate → Guardrail
Profiles**.

**Globally** (every Automaton; the lowest tier, and the only tier that can
express *no kinds*):

```yaml
autonomous:
  notify:
    # adds guardrail_warn to the five-kind baseline for every Automaton
    default_events: [prd.needs_review, prd.blocked, prd.failed, prd.completed, prd.cancelled, guardrail_warn]
```

under `autonomous.notify` in the config file (annotated example in
[config-reference.yaml](../config-reference.yaml)), or without touching the
file: REST `PUT /api/config` with `{"autonomous.notify.default_events": [...]}`
(JSON array; read back via `GET /api/config`), MCP `config_set`, CLI
`datawatch config set autonomous.notify.default_events <kinds>`, comm channel
`configure autonomous.notify.default_events=<kinds>`, or the PWA settings
field (Settings → General → Automate). Changed once, applies at daemon start;
an explicit empty list (`default_events: []`) turns the global tier off.

#### Enabling warning kinds from every surface

| What you want | YAML | CLI | Web UI | REST | comm channel |
|---|---|---|---|---|---|
| One or both warning kinds for one Automaton | — (the per-Automaton list is data, set through the other surfaces) | `datawatch autonomous prd-set-notify-events <id> prd.blocked,guardrail_warn` | Automaton settings → *Notification events* panel: tick the opt-in kinds, toggle *Inherit* off | `POST /api/autonomous/prds/{id}/set_notify_events` with `{"notify_events": ["prd.blocked", "guardrail_warn"]}` | `autonomous set-notify-events <id> prd.blocked,guardrail_warn` (`inherit` clears it back to the profile/global tier) |
| Warning kinds for all Automata on one guardrail profile | — (the profile's list is data, set through the other surfaces) | `datawatch guardrail profile update <id> --notify-events prd.failed,scope_warning` | Settings → Automate → Guardrail Profiles → the profile's notify list | `PUT /api/autonomous/guardrail_profiles/{id}` with `{"notify_events": ["scope_warning"]}` | `guardrail profile notify-events <id> prd.failed,scope_warning` (or `inherit`) |
| Warning kinds globally (lowest tier) | `autonomous.notify.default_events` (example above) | `datawatch config set autonomous.notify.default_events "prd.blocked,prd.failed,guardrail_warn"` | Settings → General → Automate → the default-events field | `PUT /api/config` with `{"autonomous.notify.default_events": ["prd.blocked", "prd.failed", "guardrail_warn"]}` | `configure autonomous.notify.default_events=prd.blocked,prd.failed,guardrail_warn` |

MCP joins every row: `autonomous_prd_set_notify_events(id, notify_events)` for
the Automaton tier, `guardrail_profile_update(id, notify_events_json="[\"scope_warning\"]")`
for the profile tier, and `config_set` / `autonomous_config_set` for the
global tier. Every surface validates the kinds, reports the effective set on
read, and rejects a typo'd kind with a near-miss hint.

#### Worked example: a guardrail `warn` into a queue

1. **Allow the kind** on the Automaton (any tier — the highest non-empty tier wins):

   ```sh
   datawatch autonomous prd-set-notify-events a1b2c3d4 prd.blocked,guardrail_warn
   ```

2. **Subscribe** the kind to the sink you want:

   ```sh
   # a named queue a generic MCP client polls:
   datawatch autonomous subscribe --on guardrail_warn --prd a1b2c3d4 --sink mcp_queue --target warn-watch
   # or a datawatch session (the event is typed in when it fires):
   datawatch autonomous subscribe --on guardrail_warn --prd a1b2c3d4 --sink session --target "$SESSION"
   ```

3. **When a guardrail judge returns `warn`**, the event is delivered to that
   sink: the Automaton, and (for a task- or story-scoped warning) its task and
   story; `guardrail` names the guardrail; `verdict` carries the outcome,
   summary and issues; `decision` is the verdict decision the same check
   recorded; and `link` is the PWA alert deep-link when the same check also
   raised an alert. In a queue it looks like:

   ```json
   {"kind":"guardrail_warn","level":"task","prd_id":"a1b2c3d4","story_id":"story_2","task_id":"task_9","guardrail":"ci-watch","verdict":{"outcome":"warn","summary":"re-run flake","issues":["flaky re-run"]},"link":"?alert=alert_171"}
   ```

   A `scope_warning` event is the same shape with `level` always `task` and
   `files` listing what the task touched outside its declared scope (including
   pre-existing foreign files swept into the task's commit), and, only when the
   finding did raise an alert, the same `link`. The full field list and the
   closed kind set are in the [lifecycle reference](../api/autonomous.md#lifecycle-event-kinds).

Two things stay fixed regardless of the notify set: **the PWA still shows the
warning either way** — the alert and the inline scope-drift marker appear for
every operator no matter what the tiers say — and **the warnings never block
or alter anything**: the task keeps running, the verdict does not fail the
work, and no status changes. The notify events only add delivery destinations
on top of the PWA warning.

Check where an Automaton stands at any tier with
`datawatch autonomous prd-get <id>` (`notify_events`,
`notify_events_effective`) or the `notify` block of
`GET /api/autonomous/status` (the global `default_events` plus the
per-Automaton overrides, capped at 100, each with its source). Turning notifications off for one Automaton,
replacing the global baseline, or restoring a cleared list is covered
in `operations.md` under *Managing Automata lifecycle event
subscriptions*.

Every set/clear is audit-logged (action `autonomous_notify_events_changed`)
with the old and new lists; the Automaton entry also records which tier
the resolved set came from after the change.

## Events from a federation peer

When another datawatch daemon forwards an event to this one (see
[peer forwarding](automaton-lifecycle-webhooks.md#peer-forwarding)), the event
carries `peer`: the **registered name** of the sending peer. This daemon sets it
from the authenticated peer identity of the request, never from the body, so a
`peer` value inside the payload is ignored. Events that started on this daemon,
and forwards delivered by an admin caller (who is not a registered peer), have
no `peer`.

A subscription filter can select on it. Add `"peer": "hub-east"` to the filter
and only the events that `hub-east` forwarded match; a local event never
matches a non-empty `peer`. The rest of the filter works as before, and the
notify-event gate still applies.

```json
{
  "filter": {"kinds": ["task.failed", "prd.blocked"], "peer": "hub-east"},
  "sink": {"type": "alert", "target": "hub-east-failures"}
}
```

- The `peer` name must be 1–64 letters, digits, `.`, `_` or `-`, the same rule
  as a peer sink target. Anything else is refused when the subscription is
  created.
- `peer` is accepted in the `filter` body of `POST` and `PUT
  /api/autonomous/subscriptions` and as the `peer` query parameter of the MCP
  [lifecycle events resource](../mcp.md#resource-subscriptions). The
  `autonomous_subscribe` MCP tool, the CLI `autonomous subscribe` command, the
  comm verb and the PWA form have no `peer` field yet, so a stored
  subscription that filters on a peer is created with REST.
- A peer filter matches nothing until a peer actually forwards events here:
  `autonomous.notify.forwarding.enabled` must be on at this daemon (the switch
  covers both directions) and the sender needs a peer subscription that names
  this daemon.
- The event JSON, the `mcp_queue` events and the webhook envelopes show the same
  `peer` field, so your own code can tell where an event came from.

### A peer being down never blocks local delivery

Forwarding is best-effort and runs beside local delivery, never in front of it.
If a peer hangs (accepts a connection and never answers) or refuses
connections, every local sink (comm, session, topic, alert, `mcp_queue`,
webhook, and the built-in alert, topic and comm sinks) still receives each
local event in the normal time, once. The retries for the dead peer run on
their own schedule, and when the peer comes back the exchange resumes with no
daemon restart. Events dropped while it was away are not replayed: forwarding
has no outbox.

### Reading a peer's events

A **peer event subscription** is the opposite direction: this daemon reads a
registered peer's pending-events queue (`GET /api/autonomous/events`) with that
peer's stored bearer token and feeds each event into local dispatch with `peer`
set. Turn on `autonomous.notify.forwarding.enabled` and
`autonomous.notify.forwarding.peer_events`, then start a read:

```sh
datawatch autonomous peer-events start hub-east ops   # peer name, queue name
datawatch autonomous peer-events status
datawatch autonomous peer-events stop hub-east
```

The peer needs `autonomous:read` for this daemon and a queue filled by an
`mcp_queue` subscription there. The same controls exist as the `autonomous
peer-events` comm verb, `/api/autonomous/peer-events` on REST and the
`autonomous_peer_events` and `autonomous_peer_events_control` MCP tools. The
rules it follows (lifecycle events only, no echo loops) are in
[datawatch-definitions.md](../datawatch-definitions.md#lifecycle-subscriptions--outbound-webhook-and-peer-forwarding)
and [operations.md](../operations.md#outbound-webhook-sink-and-peer-forwarding-ships-disabled);
the whole walkthrough is in
[The event-driven supervisor](automaton-event-driven-supervisor.md#reading-a-peers-events-the-pull-direction).

## Storage, audit & restart behavior

- Persisted at `<data_dir>/lifecycle_subscriptions.json`, encrypted at
  rest with the daemon's secfile key (same treatment as channel
  routing rules). It survives daemon restarts — re-apply after
  migrating the data dir.
- Create/update/test/delete are audit-logged. Per-event delivery is intentionally
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
- **A supervisor kind never fires** — it is opt-in: check
  `notify_events_effective` on the Automaton includes it; for
  `burn_guard.rung_changed` only the global `default_events` list counts.
  The condition kinds fire once per episode — a condition that already
  held when you subscribed fires again only after it clears.
- **Session sink never fires** — the event kind or task scope doesn't
  match what actually happens (e.g. subscribed to
  `task.session_cleared` instead of `session_started`), or the
  one-shot already consumed its event. List with
  `datawatch autonomous subscriptions` and re-register for the next
  boundary if needed.
- **Text arrived but looks wrong** — verify the `lifecycle-sub`
  source tag in the session input history; `send_input` types verbatim,
  4096-char cap.
- **`webhook` or `peer` sink is refused** — both ship off. Turn on
  `autonomous.notify.webhook.enabled` or `autonomous.notify.forwarding.enabled`
  (see [lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md)).
- **A `peer` filter never matches** — the filter selects only events a
  registered peer forwarded to this daemon. Check that
  `autonomous.notify.forwarding.enabled` is on here, that the sender has a peer
  subscription naming this daemon, and that the name in the filter is the
  peer's registered name (`datawatch federation peer list`). Events that started
  on this daemon never match.
- **Polling a queue returns nothing** — confirm a subscription with sink
  `mcp_queue` and the *same* queue name exists
  (`datawatch autonomous subscriptions`), that the events you expect match
  its filter, and that the `since` cursor is not from before a daemon
  restart (cursors restart at 1).
- **Test says `sink not available`** — that delivery path is not wired in
  this daemon (for example no comm router is configured for a `comm`
  sink).

## See also

- [flow/lifecycle-subscription-flow.md](../flow/lifecycle-subscription-flow.md) — event → dispatcher → subscription engine → sink
- [Design pass](../plans/2026-10-13-prd-lifecycle-events.md) — phases, parity surface, open questions
- [datawatch-definitions.md](../datawatch-definitions.md) — "lifecycle event subscription" glossary entry
- [How-to: autonomous planning](autonomous-planning.md) — the Automaton lifecycle the events describe
