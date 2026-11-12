---
docs:
  index: true
  topics: [autonomous, supervisor, events, subscriptions, federation, mcp]
exec_params:
  - {name: kinds, required: false, default: "task.failed,automaton.stuck_running,judge.verdict_block", description: "Comma-separated event kinds that wake the supervisor (run the event-kinds step first to see every valid kind)"}
  - {name: coalesce_seconds, required: false, default: "30", description: "Quiet window in seconds, 0 to 3600; 0 wakes on every event"}
  - {name: fallback_interval_minutes, required: false, default: "20", description: "Minutes between scheduled passes when no event wakes the supervisor, 1 to 1440"}
  - {name: enabled, required: false, default: "true", description: "true turns the event wake on; false leaves the supervisor on its schedule alone"}
exec_steps:
  - tool: autonomous_event_kinds
    description: List every event kind a subscription or the wake can name, grouped, with the ones in the default wake set marked
    args: {}
    read_only: true
  - tool: supervisor_loop_stats
    description: Read the supervisor loop numbers and the wake status before changing anything
    args: {}
    read_only: true
  - tool: config_set
    description: Choose the event kinds that wake the supervisor
    args:
      key: "supervisor_loop.wake.kinds"
      value: "{{params.kinds}}"
    read_only: false
  - tool: config_set
    description: Set the quiet window in which further events fold into one pass
    args:
      key: "supervisor_loop.wake.coalesce_seconds"
      value: "{{params.coalesce_seconds}}"
    read_only: false
  - tool: config_set
    description: Set how often the scheduled fallback check still runs
    args:
      key: "supervisor_loop.wake.fallback_interval_minutes"
      value: "{{params.fallback_interval_minutes}}"
    read_only: false
  - tool: config_set
    description: Turn the event wake on or off
    args:
      key: "supervisor_loop.wake.enabled"
      value: "{{params.enabled}}"
    read_only: false
  - tool: supervisor_loop_stats
    description: Confirm the wake settings in effect and when the supervisor last woke for an event
    args: {}
    read_only: true
---
# How-to: The event-driven supervisor

The supervisor used to find out what needed a decision by looking: on a
schedule it listed Automata, checked branches, and read capacity. An
**event-driven supervisor** is told instead. When an Automaton blocks, a task
fails, a judge returns a verdict or a branch is ready to merge, the daemon
raises an event, and the supervisor starts a pass at once. The schedule does
not go away. It stays as the **fallback check**, so an event that never
arrives, or arrives while no supervisor session is running, is still caught on
the next scheduled pass.

> **What happens when you turn it on.** Setting `supervisor_loop.wake.enabled`
> to true registers the wake within a few seconds, with no restart. From then on
> each matching event runs one supervisor loop pass at once and, when a
> supervisor session is running, types a `supervisor wake:` line into it. The
> scheduled check keeps running as the fallback, at
> `supervisor_loop.wake.fallback_interval_minutes`, and the wake status shows the
> time and count of both. Turning the setting off removes the wake again; a
> daemon with it off behaves exactly as before.

This page walks through the whole path, in the order you would set it up:

1. [Subscribe the supervisor](#1-subscribe-the-supervisor) with the wake settings.
2. [Choose events in the picker](#2-choose-events-in-the-picker).
3. [Subscribe to a peer](#3-subscribe-to-a-peer), so another daemon's events reach this one.
4. [Add an event kind or a sink](#4-add-an-event-kind-or-a-sink-through-the-extension-registration) from your own code.
5. [Follow events as an MCP client](#5-mcp-resource-subscriptions) with a resource subscription.
6. [What is and is not forwarded across a peer](#what-is-and-is-not-forwarded-across-a-peer).

Every setting is listed in [the settings table](#settings-at-a-glance), and
[Change a setting on every surface](#change-a-setting-on-every-surface) gives the
exact form for the config file, REST, MCP, CLI, messaging channel and web
settings.

## Base requirements

- `datawatch start`, on a release that has the supervisor loop
  (`autonomous.supervisor_loop.enabled` is on by default). The wake is a part of
  that loop and does nothing while the loop is off.
- A supervisor session running. The wake types its line into that session.
  With none running the wake is dropped and the scheduled check covers it.
- For [step 3](#3-subscribe-to-a-peer): at least one registered federation peer
  (`datawatch federation peer list`).

## Settings at a glance

| Setting | Default | What it does |
|---|---|---|
| `supervisor_loop.wake.enabled` | `false` | Turns the event wake on. Off, the supervisor runs on its schedule alone and nothing is registered. A change applies within a few seconds, with no restart. |
| `supervisor_loop.wake.kinds` | the default list | The event kinds that wake the supervisor. At most 64, no duplicates, none empty while the wake is on, each a kind a subscription can filter on. |
| `supervisor_loop.wake.coalesce_seconds` | `30` | The quiet window, `0` to `3600`. Events inside it fold into one pass. `0` wakes on every event. |
| `supervisor_loop.wake.fallback_interval_minutes` | `20` | Minutes between scheduled fallback checks, `1` to `1440`. While the wake is on, the scheduled check runs a pass only when this many minutes have passed since its last pass; events run passes in between. While the wake is off the scheduled check runs on every tick of the idle loop, as before. Read live on every tick. |
| `mcp.resource_subscriptions` | `false` | Lets an MCP client subscribe to the lifecycle events resource and be notified of matching events. Needs a daemon restart to change. |
| `autonomous.notify.forwarding.enabled` | `false` | The one switch for federation event exchange, in both directions. Needed for a peer sink and for receiving forwarded events. |
| `autonomous.notify.forwarding.peer_events` | `false` | Lets this daemon read a registered federation peer's lifecycle events and feed them into its own local dispatch (step 3). Start, stop and read it with the peer-events command. Needs `autonomous.notify.forwarding.enabled` too. |
| `autonomous.notify.forwarding.max_hops` | `3` | The hop cap on a forwarded event. A receiver refuses an event that has reached it. |
| `autonomous.notify.default_events` | the five-kind baseline | The global notify set, the lowest tier. Stored subscriptions and MCP resource subscriptions only deliver kinds the resolved notify set allows. |

In the config file the wake keys are flat dotted names with the full
`supervisor_loop.` prefix inside the `autonomous.supervisor_loop` block. On every
other surface except MCP `config_set` they are written
`autonomous.supervisor_loop.wake.<key>`. Each has an entry in the
[definitions](../datawatch-definitions.md#supervisor-loop-wake-enabled), and the
web fields carry a **?** help link that opens the
[supervisor loop how-to](supervisor-loop.md#settings-and-defaults).

## 1. Subscribe the supervisor

The wake is switched on with one setting. When
`supervisor_loop.wake.enabled` is true the built-in `supervisor-wake`
subscription set is registered, one subscription per kind in
`supervisor_loop.wake.kinds`. Nothing is registered while the wake is off, and
none of your own stored subscriptions are touched. The daemon re-reads the
settings every few seconds, so a change to the setting, the kinds or the quiet
window takes effect without a restart.

```sh
# Read where it stands first.
datawatch supervisor-loop wake

# Choose the kinds, then switch it on.
datawatch supervisor-loop wake \
  --kinds task.failed,automaton.stuck_running,judge.verdict_block \
  --coalesce-seconds 30 --fallback-interval-minutes 20 --enabled=true
```

How a pass starts:

- Each matching event is offered to a **coalescer**. Events that arrive within
  `supervisor_loop.wake.coalesce_seconds` of each other fold into one pass, so a
  burst of failures does not start a pass per failure.
- Only one pass runs at a time. Events that arrive during a pass are queued and
  handled by the next one.
- A pass runs the supervisor loop's mechanical steps at once (the same steps the
  scheduled check runs). If a supervisor session is running (a session with the
  `role:supervisor` tag that is running or waiting for input), the pass also types
  a line such as `supervisor wake: task.failed automaton=<id> task=<id>` into it,
  listing every folded event separated by `; `. With no session running the pass
  still runs; only the line is skipped.
- The scheduled check keeps running as the fallback. While the wake is on it runs
  a pass every `supervisor_loop.wake.fallback_interval_minutes`, whether or not an
  event arrived.

The wake status shows the settings in effect, and when the supervisor last woke
for an event and when it last ran a scheduled check, with a count of each. A
count goes up once per event wake and once per scheduled pass. If the event path
has been silent for more than an hour the status says so. That usually means
the kinds list is too narrow or nothing has happened that you subscribed to.

**Review before relying on it.** The wake is off by default and nothing turns it
on for you. The wake set listens on the daemon's event dispatcher by kind, and it
does not apply the notify set that stored subscriptions and MCP resource
subscriptions apply. After you switch it on, cause a real event and confirm that
the status shows an event wake and, if a supervisor session is running, that a
`supervisor wake:` line reaches it.

### A stored subscription for the supervisor

A stored subscription with a `session` sink types a line into a session you name
each time a listed event fires. It is an alternative to the wake when you only
want the line typed, and it does not depend on the wake settings or run a pass.

```sh
# Allow the kinds for every Automaton: the five-kind baseline plus the three you
# want (global notify set; applies at the next daemon start). Setting this list
# replaces the baseline, so keep the baseline kinds in it.
datawatch config set autonomous.notify.default_events \
  "prd.needs_review,prd.blocked,prd.failed,prd.completed,prd.cancelled,task.failed,automaton.stuck_running,judge.verdict_block"

datawatch autonomous subscribe \
  --on automaton.stuck_running --on judge.verdict_block --on task.failed \
  --send "A supervisor event arrived; read the Automata events and act." \
  --sink session --target "$SUPERVISOR_SESSION" --one-shot=false --coalesce
```

A stored subscription applies the notify set, so each kind it names must be in the
resolved set (the first command) before it fires. The full set of sink types, the
notify set tiers and the per-surface forms are in
[Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md#events-for-an-event-driven-supervisor).

## 2. Choose events in the picker

There are two places to choose events, and both read the same list: the daemon's
**event kind registry**.

**Which kinds exist.** The list includes every built-in kind and any kind an
extension registered. Each kind shows its group (Automaton, Task, Review,
Capacity or Budget), a one-line description and whether it is in the default set
that wakes the supervisor.

| Surface | How |
|---|---|
| CLI | `datawatch autonomous event-kinds`; add `--group task` to keep one group, or `--json` for the raw body. |
| Messaging channel | `autonomous event-kinds`, or `autonomous event-kinds group=task`. |
| MCP | the `autonomous_event_kinds` tool, no parameters. |
| REST | `GET /api/autonomous/event-kinds`. |
| Web UI | the kind checkboxes in **Add subscription** (below). |

**The wake kinds field.** In **Settings → Automata → Supervisor wake**, the
**Wake event kinds** field takes a comma-separated list. Use the list above to
choose. A kind the registry does not know, a duplicate, or an empty list while
the wake is on is refused, and the message names the setting. The default list
covers a blocked Automaton, a task needing review, a failed task or final gate,
every judge verdict, all tasks done or cancelled, an Automaton stuck running, a
long capacity wait and a burn guard rung change.

**The Add subscription picker.** For your own rules, open an Automaton in the
web UI (**Automata** → the Automaton → **Subscriptions** → **Add subscription**).

1. Tick at least one event kind, or pick a level.
2. The kinds are grouped Automaton, Task, Review, Capacity and Budget, each with
   its description. A kind that only fires when the notify set allows it is marked
   *(opt-in)*. If the list cannot be loaded, a note says so and the built-in list
   is shown.
3. Choose the scope (this Automaton or all Automata), the sink and its target, and
   optionally one-shot, coalesce and an expiry.
4. The **?** beside **Event kinds** opens the
   [subscriptions how-to](automaton-lifecycle-subscriptions.md#pwa-walkthrough).

Wake kinds are the daemon's choice of what starts a supervisor pass. A stored
subscription is the other direction: it delivers events to a place you choose.
Use the wake for the supervisor and stored subscriptions for everything else, and
see [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md) for the sink types,
the notify set and the per-surface forms.

The supervisor kinds are opt-in and informational. None of them changes a status,
a verdict or a gate outcome:

| Kind | Fires when |
|---|---|
| `automaton.stuck_running` | an Automaton is `running` with every task finished, no live session and no executor |
| `automaton.merge_ready` | a finished Automaton's work branch is waiting to be merged |
| `automaton.tasks_done_or_cancelled` | a running Automaton has at least one task and every task is completed or cancelled |
| `task.capacity_wait_too_long` | a task has waited for a worker lane longer than half of `autonomous.capacity_wait_timeout_seconds` |
| `burn_guard.rung_changed` | the burn guard's rung in force changed |
| `judge.verdict_block`, `judge.verdict_warn`, `judge.verdict_pass` | a guardrail judge verdict with that outcome was recorded |
| `task.final_gate_failed` | a final gate task finished without passing |

## 3. Subscribe to a peer

Two things are called a peer subscription, and only one of them is switched on in
a running daemon today.

### Forwarding: the sending peer pushes (works now)

The sending daemon registers the receiving daemon as a federation peer and adds a
subscription with the **peer sink**. Its target is the registered peer name, and no
address or token is stored in the subscription.

```sh
# On the sending daemon: register the receiver, then forward matching events.
datawatch federation peer add --name hub-east \
  --url https://hub-east.example.net:8443 \
  --token "<token issued to this daemon>" \
  --capabilities autonomous-operator

datawatch config set autonomous.notify.forwarding.enabled true

datawatch autonomous subscribe --on task.failed --on automaton.stuck_running \
  --sink peer --target hub-east
```

Do the same on the receiving daemon with the sending daemon registered as a peer
under the same token, and turn on `autonomous.notify.forwarding.enabled` there as
well. Without it the receiver refuses the forwards. The full setup, the request
headers and the refusal codes are in
[Automata lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md#peer-forwarding).

On the receiving daemon a forwarded event is re-emitted to its local
subscriptions with `peer` set to the sender's registered name. That includes the
`supervisor-wake` set: it filters by kind only, so a forwarded event of a listed
kind starts a supervisor pass on the receiver like a local one. A stored subscription
with a session sink types its line the same way.

To react to one peer only, add `"peer": "hub-east"` to a stored subscription's
filter. Only events that peer forwarded match it, and a local event never does.
The name is 1 to 64 letters, digits, `.`, `_` or `-`.

| Surface | Peer sink | `peer` filter |
|---|---|---|
| REST | `POST /api/autonomous/subscriptions` with `"sink": {"type": "peer", "target": "hub-east"}` | `"filter": {"kinds": [...], "peer": "hub-east"}` |
| MCP | `autonomous_subscribe(kinds=..., sink_type="peer", sink_target="hub-east")` | the `peer` parameter of the lifecycle events resource URI (step 5) |
| CLI | `datawatch autonomous subscribe --sink peer --target hub-east` | no flag yet |
| Messaging channel | `autonomous subscribe task.failed sink=peer target=hub-east` | no field yet |
| Web UI | choose **Federation peer** as the sink and pick the peer | no field yet |

### Reading a peer's events: the pull direction

A **peer event subscription** is the opposite direction: this daemon reads a
registered peer's pending-events queue with that peer's stored bearer token and
feeds each event into local dispatch with `peer` set, so a local subscription
(including a `peer` filter) sees the peer's events like any other. Use it when the
peer cannot push to you. It follows the rules in
[What is and is not forwarded across a peer](#what-is-and-is-not-forwarded-across-a-peer).

You need three things before it starts:

1. `autonomous.notify.forwarding.enabled` true (the one switch for federation event
   exchange) and `autonomous.notify.forwarding.peer_events` true.
2. The peer registered as a federation peer (`datawatch federation peer list`), enabled,
   with a bearer token and the `autonomous:read` capability.
3. A named pending-events queue on the peer, filled by an `mcp_queue` subscription there
   (the queue name is that subscription's target).

```sh
datawatch config set autonomous.notify.forwarding.enabled true
datawatch config set autonomous.notify.forwarding.peer_events true
datawatch autonomous peer-events start hub-east ops     # peer name, queue name
datawatch autonomous peer-events status
# peer event reading: on
# reading: hub-east
datawatch autonomous peer-events stop hub-east
```

A read lasts until you stop it or the daemon restarts, so run the start command again
after a restart. A start that is refused (setting off, unknown peer, peer without the
read capability, no token, a peer already being read) says why and starts nothing.

| Surface | Start | Stop | Read |
|---|---|---|---|
| CLI | `datawatch autonomous peer-events start <peer> <queue> [--since N]` | `datawatch autonomous peer-events stop <peer>` | `datawatch autonomous peer-events status` |
| Messaging channel | `autonomous peer-events start <peer> <queue> [since=N]` | `autonomous peer-events stop <peer>` | `autonomous peer-events status` |
| REST | `POST /api/autonomous/peer-events/start` with `{"peer","queue","since"}` | `POST /api/autonomous/peer-events/stop` with `{"peer"}` | `GET /api/autonomous/peer-events` |
| MCP | `autonomous_peer_events_control` with `action` start | `autonomous_peer_events_control` with `action` stop | `autonomous_peer_events` |
| Config file / web settings | `autonomous.notify.forwarding.peer_events` (the switch) in the `forwarding` block; web: Settings → Comms → Notifications, **Read a peer's lifecycle events** | | |

## 4. Add an event kind or a sink through the extension registration

A plugin, or any other Go code that links the `internal/autonomous` package, can
extend the registry at start-up instead of editing the built-in list. There is no
manifest key and no setting for it: it is two function calls made before the daemon
reads its settings and serves requests.

```go
err := autonomous.RegisterEventKind(autonomous.EventKindInfo{
    Kind:        "deploy.finished",          // lowercase parts joined by dots
    Description: "A deploy of the project finished", // one line, 200 characters at most
    Group:       autonomous.EventGroupTask,  // an existing group
})

err = autonomous.RegisterSinkType("pagerduty", func(s autonomous.SubscriptionSink) error {
    // validate the target; return an error to refuse the subscription
    return nil
})
```

The rules the registry enforces:

- A name is lowercase letters, digits and underscores with dots between parts,
  at most 64 characters.
- A kind needs a one-line description of at most 200 characters and one of the
  existing groups: Automaton, Task, Review, Capacity or Budget.
- A name that is a built-in kind or sink, or was registered before, is refused.
- A sink type may pass `nil` for its validator. A `secret_ref` is refused unless
  the validator is set and accepts it. A target is capped at 512 characters.

What registration does and does not do:

- A registered kind passes subscription validation, appears in the event kind list
  on every surface (CLI, messaging channel, MCP, REST and the web picker), and can
  be named in `supervisor_loop.wake.kinds`.
- A registered sink type is accepted and stored by a subscription. The daemon's
  delivery engine only delivers to the built-in sinks, so a matching event is not
  sent to a registered sink for you. The extension that registered it must listen
  for events itself and deliver them.
- Registering a kind does not emit it. The code that registers the kind is the code
  that must raise events of that kind.
- Register before the settings are read. A wake kind that is not registered yet is
  an unknown kind, and the setting refuses it.

Check the result with `datawatch autonomous event-kinds`. The kind appears in its
group with its description, and `wake_default` is false for it.

## 5. MCP resource subscriptions

A client that can receive MCP notifications has a push path that stores nothing.
With `mcp.resource_subscriptions` on (default `false`), it calls
`resources/subscribe` on the lifecycle events resource and gets a
`notifications/resources/updated` message each time a matching event fires:

```
datawatch://events/lifecycle?kinds=task.failed,automaton.stuck_running&level=task
```

```sh
datawatch config set mcp.resource_subscriptions true
systemctl --user restart datawatch     # the setting is read at start-up
```

- It is additive. It is one more consumer of the same dispatcher your stored
  subscriptions use, so the filter rules, the kind list and the notify set gate are
  the same, and nothing is written to the subscription store, a sink or a queue.
- Add `&peer=<registered peer name>` to receive only the events that federation
  peer forwarded.
- A subscription belongs to the MCP session. It ends on `resources/unsubscribe` or
  when the session closes, and a session can hold at most 32.
- The notification carries the subscribed `uri` and the event's `event_id`, `kind`,
  and the Automaton and task ids. Read the resource for the recent events.
- With the setting off the server advertises nothing new, lists no lifecycle events
  resource and refuses `resources/subscribe`.
- Because nothing is stored, a client that is offline when an event fires does not
  get it later. Use an `mcp_queue` subscription when it must not miss one.

The full parameter list is in the [MCP reference](../mcp.md#resource-subscriptions).

## What is and is not forwarded across a peer

Forwarding sends events between daemons, and the receiver treats what arrives as a
notification only.

**Forwarded**

- Lifecycle events: an Automaton, task or Automation changing status, a task session
  starting or clearing, and an Automaton update notice, as this daemon's own emitter
  produces them. The sender forwards only the events a peer subscription of its own
  selects, and only when `autonomous.notify.forwarding.enabled` is on.
- The registered name of the sending peer, as `peer`. The receiver sets it from the
  authenticated identity of the request, never from the payload.
- The origin host, the hop count and the list of hosts the event passed through, in a
  `forward` block.

**Not forwarded**

- Session output, secrets, configuration and any other peer data. The exchange carries
  lifecycle events only.
- An event the sender had itself received as forwarded. A daemon never forwards a
  forward, so two peers that subscribe to each other do not echo an event back.
- A synthetic test event from a subscription test.
- Anything to or from a peer that is not registered, is not enabled, is not a
  federation peer, or lacks the `autonomous:write` capability for a push (the
  `autonomous:read` capability for the pull direction).
- Events the receiver refuses: one that started on the receiver, one that already
  visited it, and one that reached `autonomous.notify.forwarding.max_hops`. Those
  get a `409` and are not retried.

**On the receiving daemon**

- A forwarded event is never written to the Automaton state and never drives an
  Automaton. It feeds notification sinks, the `supervisor-wake` set and MCP resource
  subscriptions only.
- Human-readable sinks show the words `(forwarded from <origin host>)`.
- A peer that hangs or refuses connections never blocks local delivery. Every local
  sink still receives each local event once, the retries for the dead peer run on
  their own schedule, and forwarding resumes when the peer returns. Events dropped
  while it was away are not replayed, because forwarding has no outbox.

## Change a setting on every surface

Each row names one setting. In the config file the wake keys sit in the
`autonomous.supervisor_loop` block and keep the `supervisor_loop.` prefix, and
`mcp.resource_subscriptions` sits in the `mcp:` block.

| Setting | Config file | REST | MCP | CLI | Messaging channel | Web settings |
|---|---|---|---|---|---|---|
| `supervisor_loop.wake.enabled` | `"supervisor_loop.wake.enabled": true` | `PUT /api/config` with `{"autonomous.supervisor_loop.wake.enabled": true}` | `config_set` key `supervisor_loop.wake.enabled`, value `true` | `datawatch supervisor-loop wake --enabled=true`, or `datawatch config set autonomous.supervisor_loop.wake.enabled true` | `configure autonomous.supervisor_loop.wake.enabled=true`, or `autonomous supervisor wake set enabled=true` | Settings → Automata → Supervisor wake → **Wake on events** |
| `supervisor_loop.wake.kinds` | a list under `"supervisor_loop.wake.kinds":` | `PUT /api/config` with a JSON array under `autonomous.supervisor_loop.wake.kinds` | `config_set` key `supervisor_loop.wake.kinds`, a comma list or JSON array | `datawatch supervisor-loop wake --kinds A,B` | `autonomous supervisor wake set kinds=A,B` | Settings → Automata → Supervisor wake → **Wake event kinds** |
| `supervisor_loop.wake.coalesce_seconds` | `"supervisor_loop.wake.coalesce_seconds": 30` | `PUT /api/config` with `autonomous.supervisor_loop.wake.coalesce_seconds` | `config_set` key `supervisor_loop.wake.coalesce_seconds` | `datawatch supervisor-loop wake --coalesce-seconds 30` | `autonomous supervisor wake set coalesce_seconds=30` | **Wake quiet window** |
| `supervisor_loop.wake.fallback_interval_minutes` | `"supervisor_loop.wake.fallback_interval_minutes": 20` | `PUT /api/config` with `autonomous.supervisor_loop.wake.fallback_interval_minutes` | `config_set` key `supervisor_loop.wake.fallback_interval_minutes` | `datawatch supervisor-loop wake --fallback-interval-minutes 20` | `autonomous supervisor wake set fallback_interval_minutes=20` | **Wake fallback interval** |
| `mcp.resource_subscriptions` | `resource_subscriptions: true` in the `mcp:` block | `PUT /api/config` with `{"mcp.resource_subscriptions": true}` | `config_set` key `mcp.resource_subscriptions`, value `true` | `datawatch config set mcp.resource_subscriptions true` | `configure mcp.resource_subscriptions=true` | Settings → General → MCP Server → **Resource subscriptions** |
| `autonomous.notify.forwarding.peer_events` | `peer_events: true` in `autonomous.notify.forwarding` | `PUT /api/config` with `{"autonomous.notify.forwarding.peer_events": true}` | `config_set` key `autonomous.notify.forwarding.peer_events` | `datawatch config set autonomous.notify.forwarding.peer_events true` | `configure autonomous.notify.forwarding.peer_events=true` | Settings → Comms → Notifications, **Read a peer's lifecycle events** |
| `autonomous.notify.forwarding.enabled` | `enabled: true` in `autonomous.notify.forwarding` | `PUT /api/config` with `{"autonomous.notify.forwarding.enabled": true}` | `config_set` key `autonomous.notify.forwarding.enabled` | `datawatch config set autonomous.notify.forwarding.enabled true` | `configure autonomous.notify.forwarding.enabled=true` | Settings → Comms → Notifications, the peer forwarding toggle |

Read the settings back with `GET /api/config`, `get_config` (for example
`section=supervisor_loop`), `datawatch config get`, or the status below. A wake
setting change applies to the running supervisor without a daemon restart.
`mcp.resource_subscriptions` is read when the daemon starts.

Read the wake status on any surface:

| Surface | Status |
|---|---|
| Web UI | the status line in **Settings → Automata → Supervisor wake** |
| CLI | `datawatch supervisor-loop wake` |
| REST | `GET /api/supervisor-loop/wake` |
| MCP | the `wake` object in `supervisor_loop_stats` |
| Messaging channel | `autonomous supervisor wake` |

## Check that it works

1. `datawatch autonomous event-kinds` lists the kinds you chose, and the wake status
   shows `wake: on` with them.
2. Cause one of the events, or wait for one, and read the wake status again. The
   last event time moves and the event count goes up. A scheduled check shows in
   the same status at the fallback interval. If both stay at `not yet` after a real
   event, check that the kind is in `wake.kinds`; a setting change can take a few
   seconds to apply.
3. In the supervisor session you see a `supervisor wake:` line (from the wake) or
   your own `--send` text (from a stored subscription).
4. For a peer sink, run `datawatch autonomous subscription-test <id>` on the peer
   subscription. It reports `ok` with the sink type, or an error that names the
   failed condition.
5. For reading a peer, run `datawatch autonomous peer-events status`; it lists the
   peer. An event the peer forwards to the queue then reaches a local subscription
   with `peer` set.

## Troubleshooting

- **The status never shows an event wake.** Check `wake.enabled` is true and the kind you
  expect is in `wake.kinds`. The wake runs a pass even with no supervisor session
  running; only the typed line needs one. The registration follows a setting change
  within a few seconds.
- **`peer-events start` is refused.** Read the reason it prints. The usual causes are
  `autonomous.notify.forwarding.enabled` or `autonomous.notify.forwarding.peer_events`
  off, a peer that is not a federation peer or lacks `autonomous:read`, and a missing
  queue name.
- **`unknown event kind` when setting `wake.kinds`.** The kind is not in the registry.
  Compare it with `datawatch autonomous event-kinds`, and check an extension kind was
  registered before the settings were read.
- **`at least one event kind is needed while ... is on`.** The list is empty with the
  wake on. Turn the wake off first, or list a kind.
- **A peer sink is refused.** Peer forwarding ships off. Turn on
  `autonomous.notify.forwarding.enabled`, and check the peer is registered, enabled
  and granted `autonomous:write`.
- **A `peer` filter never matches.** It matches only events that peer forwarded here.
  Check forwarding is on at this daemon, the sender has a peer subscription that names
  this daemon, and the name is the peer's registered name.
- **`resources/subscribe` is refused.** `mcp.resource_subscriptions` is off, or the
  daemon was not restarted after you turned it on.
- **A registered sink gets nothing.** The delivery engine does not deliver to a
  registered sink type. The extension must deliver.

## Open items for the operator

These are the points this release leaves for you to review or decide. Each one is
described where it applies above; this list gathers them in one place.

1. **The wake ignores the notify set.** A stored subscription and an MCP resource
   subscription are both limited by the notify set; the wake set is not. Decide
   whether that is what you want before you turn it on.
2. **A peer read does not survive a restart.** `peer-events start` is not stored; start it again after the daemon restarts.
3. **The `peer` filter is partial.** It is set through REST and the MCP resource URI
   only. The CLI, the messaging channel, the `autonomous_subscribe` tool and the web
   form do not have it yet.
4. **A registered sink type is not delivered to.** An extension that registers one
   must deliver the events itself.
5. **Mobile parity.** Android and iPhone are tracked in the mobile app project and
   are not changed here.

## Parity surface

This how-to touches these surfaces. Each row says what is covered or why it is
not.

| Surface | Covered |
|---|---|
| REST | Yes. `GET /api/autonomous/event-kinds`, `GET /api/supervisor-loop/wake`, `POST /api/autonomous/subscriptions` (peer sink and `peer` filter), and `PUT /api/config` for every setting. |
| MCP | Yes. `autonomous_event_kinds`, `supervisor_loop_stats`, `autonomous_subscribe`, `config_set`, and the lifecycle events resource subscription. The `peer` filter is the resource URI parameter only; the `autonomous_subscribe` tool has no field for it yet. |
| CLI | Yes. `datawatch supervisor-loop wake`, `datawatch autonomous event-kinds`, `datawatch autonomous subscribe`, and `datawatch config set`. No `peer` filter flag yet. |
| Messaging channel | Yes. `autonomous supervisor wake`, `autonomous event-kinds`, `autonomous subscribe`, and `configure`. No `peer` filter field yet. |
| Web settings | Yes. Settings → Automata → Supervisor wake and Settings → General → MCP Server, each with a **?** help entry. The Add subscription picker lists the kinds. No `peer` filter field yet. |
| YAML | Yes. The `autonomous.supervisor_loop` block, the `mcp:` block and `autonomous.notify.forwarding`. |
| Extension registration | Go only, by design. It is a start-up call with no setting, REST route, MCP tool, CLI command or messaging verb. |
| Peer event subscription (pull) | Yes, on every operator surface. Setting `autonomous.notify.forwarding.peer_events` (config file, `PUT /api/config`, `config_set`, `datawatch config set`, `configure`, and the web toggle in Settings → Comms → Notifications). Start, stop and read: `autonomous peer-events` on the CLI and the messaging channel, `/api/autonomous/peer-events` on REST, and the `autonomous_peer_events` and `autonomous_peer_events_control` tools on MCP. |
| Android and iPhone | Tracked separately in the mobile app project. |

## See also

- [Supervisor loop](supervisor-loop.md): the jobs, the steps and the wake settings in context.
- [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md): sinks, the notify set, the pending-events queue and every surface.
- [Automata lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md): the peer sink setup, the headers and loop prevention.
- [MCP reference: resource subscriptions](../mcp.md#resource-subscriptions)
- [Definitions](../datawatch-definitions.md): the event kind, subscription, sink, wake, coalesce, fallback check and peer event entries.
