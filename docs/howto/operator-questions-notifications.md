---
docs:
  index: true
  topics: [autonomous, operator-questions, notifications, websockets, subscriptions, comm]
exec_params: []
exec_steps:
  - tool: config_get
    description: Read the operator question channel switches and their three notification toggles (all default on when the key is absent, the switch itself off)
    args: {key: "autonomous.questions.notify_alert"}
    read_only: true
  - tool: autonomous_subscriptions
    description: List the persisted lifecycle subscriptions that already catch question.answered and question.withdrawn
    args: {}
    read_only: true
---
# How-to: getting notified when an Automaton asks you a question

An Automaton that runs under datawatch can stop and **ask the operator a
question**: a short decision statement, two to twenty numbered options, the
asker's recommendation, and the context it is being asked in. The question
is stored durably, appears in the PWA question list and in the REST
questions API — and in this step of the channel, it is also **announced**:

- a **WebSocket `question_raised` message** to every connected client
  (the PWA surfaces it the moment it lands);
- a **line in every configured messaging channel** (the comm router
  targets you have set up);
- an **alert** in the alert store (warning level, broadcast to
  connected clients) — high-priority questions only.

When you later **answer** the question — or it gets **withdrawn** — that
too is an event: `question.answered` and `question.withdrawn`, both
subscribable through the ordinary lifecycle subscription surface (REST,
MCP, CLI, comm channel, one-shot notify) with every ordinary sink (comm
route, session, topic, alert store, `mcp_queue`, webhook, peer).

Finally, an Automaton that asked the question gets it back on the path you
chose at raise time:

1. **The default path — event plus polled GET.** The answer is an event on
   the bus, and `GET /api/questions/{id}` carries the current answer's
   summary (chosen option, the verbatim quote, actor, when). This path
   needs nothing from the asker.
2. **The opt-in line — a single `DATAWATCH_ANSWER:` line dropped into the
   asker's session.** One per answer, only when the question was raised
   with `return_path: true` *and* the daemon's `autonomous.questions`
   switch and `return_path` toggle are both on. A session that has gone
   away by then simply gets nothing: the answer stays in the store, the
   asker can still read it back by polling.

> **The whole channel is off by default.** `autonomous.questions.enabled`
> starts `false`: none of the announce steps above runs, the REST question
> endpoints refuse with `503`, and no event fires. Questions **never
> expire** — an open one stays open and answerable until answered,
> withdrawn, or the store is removed.

This guide assumes the
[lifecycle subscriptions](automaton-lifecycle-subscriptions.md) and
[alerts and notifications](alerts-and-notifications.md) guides if you have
not read them — the question events ride exactly those rails.

## Base requirements

- A daemon with the **questions channel built in** (current datawatch
  builds; the channel is gated behind its switch, so builds without the
  channel simply never fire it).
- At least **one comm router backend** if you want the messaging group
  line (Telegram, Slack, or the console fallback). Without any router the
  messaging announcement has nowhere to go and is skipped silently; the
  WebSocket, alert and event paths are unaffected.
- Any of the ordinary ways to turn settings on and off — YAML config,
  the REST settings endpoint, the MCP `config_set`/`config_get` tools,
  `datawatch` CLI, a comm channel `configure` instruction, or the PWA
  settings card. All of them work; the PWA card is the only surface with a
  ready-made checkbox for this feature.
- For the return line: a **live datawatch session** to receive it (the
  question's `session_id`), and that session's output stream checked for a
  line beginning `DATAWATCH_ANSWER:`.

## Setup: turning the channel on, and the three announces

The four switches, in `autonomous.questions`:

| Key | Default (key absent) | Controls |
|---|---|---|
| `enabled` | **off** | the whole channel: REST endpoints, the raise-time announcement (WebSocket, messaging, alert), the `question.answered` / `question.withdrawn` events, and the opt-in line. Everything else in datawatch is untouched when it is off. |
| `notify_messaging` | **on** | adds/removes the messaging-group line on every raise. |
| `notify_alert` | **on** | adds/removes the alert on every **high-priority** raise. |
| `return_path` | **on** | whether a raise *may* ask for the single `DATAWATCH_ANSWER:` session line (the question still has to opt in per-question, see below). |

All four are read **live**: changing any of them takes effect from the
next question onwards, no restart, and the PWA settings card flips each
with a checkbox.

### All the ways to turn it on

YAML (under the `autonomous:` section of the daemon config):

```yaml
autonomous:
  questions:
    enabled: true          # the channel switch — off until this is true
    # the three toggles below default to true when the key is absent;
    # uncomment only to switch an announce off:
    # notify_messaging: false   # no messaging-group line on raise
    # notify_alert: false       # no high-priority alerts on raise
    # return_path: false        # refuse the per-question session line
```

| Surface | How to turn the channel on |
|---|---|
| YAML | as above, under `autonomous.questions` in the daemon config file. |
| REST | `curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"autonomous.questions.enabled": true}' https://localhost:8443/api/config` |
| MCP | `config_set(key="autonomous.questions.enabled", value=true)` — then `config_get(key="autonomous.questions.notify_alert")` reads a toggle back. |
| CLI | `datawatch config set autonomous.questions.enabled true` — and, to quiet an announce, `datawatch config set autonomous.questions.notify_messaging false`; read either one back with `datawatch config get <key>`. |
| Comm channel | `configure autonomous.questions.enabled=true` (all three toggles are listed in the daemon's `configure` help text). |
| PWA | **Settings → Autonomous Automata planning**: the four checkboxes — *Operator questions for Automata (off by default)*, *Notify on question asked and answered (on by default)*, *Alert on high-priority questions (on by default)*, and *Allow the per-question session return line on top of the event plus polled answer path (on by default)*. Flip the first one and the channel starts announcing from the next raise. |

The REST, MCP, CLI, comm and PWA changes are all read live by the daemon
at the moment the next question matters — no restart.

### Turning the three announces off, independently

The toggles sit *under* the switch: with `enabled: false` nothing fires
regardless of the toggles, and with it `true` each announce runs unless
its own toggle says otherwise.

- **No messaging line?** `notify_messaging: false` (any surface above).
  The WebSocket and high-priority alert still fire.
- **No alert?** `notify_alert: false`. The WebSocket and messaging lines
  still fire; normal-priority questions never alerted in the first place.
- **No session line ever?** `return_path: false`. The default
  event-plus-polled path is untouched — an asker that does not ask for
  the line is never affected by this toggle.

Each toggle is read live at the moment it matters: messaging and alert at
raise time, `return_path` at answer time.

## Walkthrough: a question is raised

Here is the full shape of a raise (REST, same fields everywhere):

```bash
curl -s -X POST "$BASE/api/questions" -H 'Content-Type: application/json' \
  -d '{
    "label": "decision",
    "context": "task",
    "task_id": "task_9f3",
    "text": "Should the retry backoff cap at five minutes, or unbounded?",
    "options": [
      {"id": "cap",  "label": "Cap at five minutes"},
      {"id": "none", "label": "Leave it unbounded"}
    ],
    "default": "cap",
    "risk": "normal",
    "priority": "high",
    "return_path": true,
    "asked_by": "session-s1",
    "asker_kind": "session",
    "session_id": "s1"
  }'
# → 201, the stored question with its id
```

Two fields belong to this step of the channel:

- **`priority`** — `normal` (the default) or `high`. It changes nothing
  but the announces: a **high** question additionally raises an alert in
  the alert store. Both priorities get the WebSocket message and the
  messaging line (while their toggles are on).
- **`return_path`** — `true` asks for the opt-in session line on the way
  back (it still requires `session_id` and both switches). `false` or
  omitted keeps the default event-plus-polled path.

### What the daemon does with it, in order

```
POST /api/questions ──▶ 1. store the question (durable) ──▶ 201 back to the asker
                                              2. WebSocket `question_raised` to every connected client
                                              3. one messaging line per configured comm route
                                              4. alert in the alert store      (priority = high, toggle on)
```

The raise is already answered with `201` before any announcement runs —
a slow router never holds the raise hostage. Each of the three announces
has its own toggle under `autonomous.questions` (WebSocket rides the
switch itself).

### 1. The WebSocket message

Message type `question_raised`, one message per raise, to every client
connected to the daemon's WS hub (the PWA is one such client and
surfaces it):

```json
{
  "type": "question_raised",
  "data": {
    "id": "q_7d21c9e8f43c",
    "label": "decision",
    "text": "Should the retry backoff cap at five minutes, or unbounded?",
    "options": [
      {"id": "cap",  "label": "Cap at five minutes"},
      {"id": "none", "label": "Leave it unbounded"}
    ],
    "priority": "high",
    "context": "task"
  }
}
```

A minimal client:

```js
const ws = new WebSocket("wss://host/ws");
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.type === "question_raised") {
    console.log(`[${msg.data.priority || "normal"}] ${msg.data.text} → ${msg.data.id}`);
    msg.data.options?.forEach((o, i) => console.log(`  ${i + 1}. ${o.label} (${o.id})`));
  }
};
```

### 2. The messaging group line

One line per active messaging route, built the same way, in the daemon
hostname style — for a question `q_7d21c9e8f43c`, label `decision`,
priority `high`:

```
[myhost] question q_7d21c9e8f43c — decision (high priority)
Should the retry backoff cap at five minutes, or unbounded?
Options:
1. Cap at five minutes
2. Leave it unbounded
Answer: POST /api/questions/q_7d21c9e8f43c/answer, or in the Automaton PWA.
```

The line names the hostname marker, the question id and label, a
`(high priority)` marker when set, the question text, the options as a
numbered list, and the two answer routes (the REST endpoint and the PWA).
It never carries anything the daemon would consider a secret, and a
question with no options (an open-ended one) simply leaves the options
block out. Suppress the line with `notify_messaging: false`; with no
messaging route configured at all it is skipped silently and the
WebSocket and alert parts are unaffected.

### 3. The alert (high priority only)

A high-priority raise also lands in the
[alert store](alerts-and-notifications.md) at **warning level**: the title
is `Operator question raised (high priority): <question-id> — <label>`,
the body carries the question text plus the option labels, and the daemon
broadcasts the alert over the same WebSocket hub every connected client
is already watching (the PWA surfaces it like any other alert).
Normal-priority raises never alert. The alert is one-way (it says nothing
back to the asker), and `notify_alert: false` turns it off. Read the
alert store with the ordinary alert surface (`GET /api/alerts`).

## Walkthrough: the question gets answered — or withdrawn

When you answer an open question — from the PWA, over REST, or from a
comm channel (the daemon records which surface carried it: `pwa`, `rest`,
`cli`, `mcp` or `comm:<channel>`) — the daemon records the answer first
and then fires **one** `question.answered` event. Answering a question
that already has an answer is a *supersede*: you name the answer the new
one replaces (`supersedes`), it fires `question.answered` for the **new**
answer, and the old one stays in the answer log. A withdrawal fires one
`question.withdrawn`.

```
answer lands ──▶ 1. the answer is recorded (durable, append-only) ──▶ 2xx to you
                               2. the question's state becomes answered
                               3. event `question.answered` → every matching subscription
                               4. (if this question opted in) one DATAWATCH_ANSWER: line → the asker's session
```

The operator's own words go on a single answer body:

```bash
curl -s -X POST "$BASE/api/questions/q_7d21c9e8f43c/answer" \
  -H 'Content-Type: application/json' \
  -d '{
    "chosen_option_id": "cap",
    "quote": "Cap it — unbounded backoff wedged the worker in prod",
    "actor": "operator"
  }'
# → 201, the recorded answer plus the question's new state
```

`chosen_option_id` names the option, `quote` is your verbatim text (the
daemon stores it byte-for-byte, up to the quote cap, and records its
SHA-256 so later copies can be checked against the record), `actor` names
who is answering (absent = the caller of record), and a superseding
answer must carry `supersedes: <answer id>`. An answer that trips the
daemon's secret-pattern check is refused with `422` naming only the rule,
never the text. Withdrawing:

```bash
curl -s -X POST "$BASE/api/questions/q_7d21c9e8f43c/withdraw" \
  -H 'Content-Type: application/json' -d '{"actor": "operator", "reason": "duplicated"}'
```

### The two events

| Kind | Fired when | Carries |
|---|---|---|
| `question.answered` | an open question's state becomes `answered` — first answer or a superseding one | the question id, the answer id it names, the chosen option, the actor, one-line details ("question q_… answered, option cap, by operator"), and the PRD/task ids when the question named them. |
| `question.withdrawn` | an open question's state becomes `withdrawn` | the question id, the actor, the reason (when given), one-line details. |

Both are regular lifecycle events: they ride the same per-subscriber queue
as `task.completed` and friends, and each fires **exactly once** even
across a daemon restart — the store keeps an emit marker per question (and
per answer), and a startup reconcile re-fires exactly the ones that were
recorded but never delivered. The event's `details` is one bounded line
(capped at 512 characters) naming the question, the answer's option and
actor; the event carries the PRD and task ids when the question named
them, which is what routes it into the right scope. With the channel
switch off, neither event fires — though the answer and withdrawal are
still recorded either way, and readable by polling.

### Subscribing — every surface

**REST** (create a subscription that catches the two kinds, same shape as
any lifecycle subscription):

```bash
curl -s -X POST "$BASE/api/autonomous/subscriptions" \
  -H 'Content-Type: application/json' \
  -d '{"filter": {"kinds": ["question.answered", "question.withdrawn"]},
       "sink": {"type": "comm", "target": "my-group"}}'
# → 201 + the stored record (with its id); list later with GET /api/autonomous/subscriptions
```

**MCP** (the ordinary `autonomous_subscribe`; the kinds accept the two
question kinds alongside the task and PRD kinds):

```
autonomous_subscribe(kinds="question.answered,question.withdrawn", sink_type="comm", sink_target="my-group")
persistent: autonomous_subscribe(kinds="question.answered", sink_type="mcp_queue", sink_target="ops")
```

**CLI**:

```bash
datawatch autonomous subscribe \
  --on question.answered --on question.withdrawn \
  --sink comm --target my-group
# into a pollable queue instead:
datawatch autonomous subscribe --on question.answered --sink mcp_queue --target ops
# and read it later:
datawatch autonomous queue ops
```

**Comm channel** (a verb to the daemon):

```
autonomous subscribe question.answered question.withdrawn --sink comm --target my-group
```

`mcp_queue` subscribers get a line per event they can poll; `comm:<channel>`
surfaces see the same. Webhook and peer sinks work exactly as described
in the [lifecycle webhooks guide](automaton-lifecycle-webhooks.md) — the
question kinds are accepted by every sink type.

### Polling instead of subscribing: GET /api/questions/{id}

An asker that cannot subscribe does not wait on the bus — it polls one
read. That read now carries the whole answer path: the question (state,
priority, return path, options, the asker's recommendation, answer count)
**plus, when the question is answered, the current answer's summary**:

```bash
curl -s "$BASE/api/questions/q_7d21c9e8f43c"
```

```json
{
  "question": {
    "id": "q_7d21c9e8f43c",
    "label": "decision",
    "context": "task",
    "task_id": "task_9f3",
    "text": "Should the retry backoff cap at five minutes, or unbounded?",
    "options": [
      {"id": "cap",  "label": "Cap at five minutes"},
      {"id": "none", "label": "Leave it unbounded"}
    ],
    "default": "cap",
    "priority": "high",
    "return_path": true,
    "asked_by": "session-s1",
    "asker_kind": "session",
    "session_id": "s1",
    "state": "answered"
  },
  "answer_count": 2,
  "current_answer_id": "a2",
  "answer": {
    "id": "a2",
    "chosen_option_id": "cap",
    "quote": "Cap it — unbounded backoff wedged the worker in prod",
    "actor": "operator",
    "surface": "pwa",
    "created_at": "2026-10-27T09:41:00Z"
  }
}
```

Read this one endpoint for the whole return path: `state` tells you the
question's state, `answer_count` every answer ever recorded (superseded
answers stay in the log — they are decided, never deleted),
`current_answer_id` which answer stands now, and `answer` the standing
answer's summary — its id, chosen option, the **verbatim quote** exactly
as received (never trimmed or re-encoded), who answered, on which
surface, and when. While the question has no answer, `answer` is
`null`. The full answer log of record is `GET /api/questions/{id}/answers`.

## Walkthrough: the opt-in session line (the single DATAWATCH_ANSWER)

This is for the one case where event-plus-polled is not enough: an
Automaton whose session must *see* the answer come back, without any
polling loop of its own. It is deliberately the narrowest path in the
channel and it is **off unless the question asked for it**.

### The three conditions

A `DATAWATCH_ANSWER:` line is sent for an answer **only when all three
hold**, each read live at the moment of the answer:

1. the question was raised with `return_path: true` **and** a
   `session_id` (a line with nowhere to go is refused at raise time);
2. the daemon's `autonomous.questions.enabled` switch is on; and
3. the daemon's `autonomous.questions.return_path` toggle is on (it is on
   when the key is absent).

If any is false — or the session has gone away — the answer is still
recorded exactly once, and the asker's only path back is the polled
`GET /api/questions/{id}` (or its subscription, if it has one). Nothing is
queued for later delivery; the line is fire-and-forget.

### The line

Exactly **one line**, appended to the asker's session output the instant
the answer is recorded, JSON-escaped so a multi-line quote never breaks
it into several lines:

```
DATAWATCH_ANSWER: {"question_id":"q_7d21c9e8f43c","answer_id":"a2","chosen_option":"cap","quote":"Cap it — unbounded backoff wedged the worker in prod","actor":"operator","at":"2026-10-27T09:41:00Z"}
```

The fields:

| Field | Meaning |
|---|---|
| `question_id` | the question this answer stands for. |
| `answer_id` | this answer's id (a supersede sends a new line with the new id). |
| `chosen_option` | the option id the answer picked; empty when it was answered with a quote alone. |
| `quote` | the verbatim quote, exactly as it was received. |
| `actor` | who answered. |
| `at` | when it was recorded (RFC 3339). |

`DATAWATCH_ANSWER:` is the same family as
`DATAWATCH_COMPLETE:` and `DATAWATCH_NEEDS_INPUT:` — a single-token
prefix an asker can grep for — but it is a **distinct** marker: the three
prefixes are separate strings, so a session that completes while carrying
an answer can never be mistaken for one of the other two. The line is
appended to the session's output and never injected as input, and a gone
session costs the asker exactly one warning log line in the daemon (no
retry, no repeated warnings).

### What a session that receives it should do

```bash
# inside the asker's own loop, when the line shows up:
if line starts with "DATAWATCH_ANSWER: ";
    parse the JSON after the marker
    # the answer is already recorded — do not re-answer;
    # continue with whatever the question was blocking.
fi
```

And if the line never comes: re-read `GET /api/questions/{id}`; if the
state is `answered`, take the answer from that read. The line is a
courtesy, the store is the record.

## ALL options: every switch, field and announce at a glance

| Name | Where | Default | Effect on |
|---|---|---|---|
| `autonomous.questions.enabled` | YAML/REST/MCP/CLI/comm/PWA | **off** | the channel itself: REST question endpoints (503 when off), the raise-time announces, the `question.answered`/`question.withdrawn` events, the opt-in line. Everything else in datawatch is untouched when off. |
| `autonomous.questions.notify_messaging` | same surfaces | on | the messaging-group line on every raise. |
| `autonomous.questions.notify_alert` | same surfaces | on | the alert on every **high-priority** raise. |
| `autonomous.questions.return_path` | same surfaces | on | whether a question may opt in to the `DATAWATCH_ANSWER:` session line. |
| `priority` (raise body / raise call) | raise | `normal` | `high` additionally alerts (toggle permitting). |
| `return_path` (raise body / raise call) | raise | `false` | ask for the session line for this question specifically. |
| `session_id` (raise body / raise call) | raise | — | the session that would receive the line (required for the line). |
| the question kinds in a subscription's `filter.kinds` | REST/MCP/CLI/comm | — | how `question.answered` / `question.withdrawn` reach you without polling. |
| `GET /api/questions/{id}` | REST | — | the asker's single polled read of state + current answer summary. |

Every setting is live: flip any of them and the next question behaves
accordingly, without a restart, and the switch and all three toggles are
in the PWA settings card.

## Troubleshooting

| Symptom | Check |
|---|---|
| `503` on any question endpoint | the channel switch is off — `autonomous.questions.enabled` is `false` (or absent, its default). Turn it on, any surface. |
| Question raised, but no announces at all | switch off at the moment of the raise, or (for the line) the question was not raised with `return_path: true` + `session_id`. |
| Messaging line missing, WebSocket fine | `notify_messaging` is `false`, or no comm router backend is configured (the line is skipped silently in that case; WS and alert are unaffected). |
| High-priority question, no alert | `notify_alert` is `false`, or the question's priority is `normal` (alerts are high only). |
| Line never reached the session | session gone at answer time (one warning in the daemon log, answer still stored), or the `return_path` toggle was turned off after the raise. Either way: poll `GET /api/questions/{id}` — the answer is there. |
| Two `question.answered` events for one answer | not possible from the daemon side — the marker plus reconcile fires each answer exactly once. If your subscriber saw two, its subscription matched twice (two subscriptions); dedupe on the answer id. |
| An open question is "old" | by design — questions never expire. Answer it (`POST /api/questions/{id}/answer`), withdraw it (`POST /api/questions/{id}/withdraw`), or leave it open; it will stay open and answerable. |

## Related

- [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md) — the rails the question events ride (REST/MCP/CLI/comm + queues).
- [Automaton lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md) — the two sinks that send events out of this daemon; question kinds are accepted.
- [Alerts and notifications](alerts-and-notifications.md) — the alert the high-priority raise lands in.
- [Operator decision channel (proposal)](../plans/2026-10-25-operator-decision-channel-proposal.md) — the design this step implements.
- REST details: [API → autonomous.md](../api/autonomous.md) "Operator question channel" section; OpenAPI: [openapi.yaml](../api/openapi.yaml) schemas `Question`, `QuestionRaise`, `Answer`, `QuestionAnswer`.
