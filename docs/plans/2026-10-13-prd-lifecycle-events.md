# BL427 — Automaton lifecycle event notifications (design pass)

**Date:** 2026-10-13 (investigation and writing finished 2026-10-14)
**Version:** v9.0.75 (`var Version` in `cmd/datawatch/main.go` at time of writing)
**Status:** Phase 1 Done (v9.0.85, 2026-10-14). Phases 2–5 Planned.

Terminology: this doc says **Automaton / Automata** in prose, per AGENT.md's
terminology rule. The internal Go type is `PRD` (and identifiers such as
`PRDStatus`, `PRDCompleted` keep that name). **Automation** (capital A,
singular concept) refers only to BL431's coordinator object — a named group of
member Automata — and is a different thing from an individual Automaton.

Every claim below was checked against code read during this pass unless it is
explicitly marked **not verified**. Line numbers are as of v9.0.75.

---

## Scope

**This phase is doc-only.** No `.go`, `.ts`, `.js`, config or test file was
created or changed, and no Automaton was created, approved or run.

A future implementation (not approved) would touch:

- `internal/autonomous` — event emission at the transition points tabulated
  below (`internal/autonomous/executor.go`, `internal/autonomous/manager.go`,
  `internal/autonomous/api.go`, `internal/autonomous/capacity.go`), a new
  event type plus an in-process dispatcher with multi-subscriber registration,
  and notification-policy resolution mirroring
  `resolveGuardrails` in `internal/autonomous/guardrail_registry.go`.
- `cmd/datawatch/main.go` — wiring sinks onto the dispatcher (comm routers,
  ntfy/email, `server.PublishToTopic`, the alert store, session `SendInput`),
  the same place every existing `amgr.Set*Fn` hook is wired today.
- `internal/server` — REST for subscription CRUD and an event feed; WS
  broadcast payload enrichment.
- `internal/mcp` — subscribe/unsubscribe/list tools and a pending-events read.
- `internal/router` — comm-channel verbs.
- `cmd/datawatch` CLI — subscription commands.
- `internal/server/web/app.js` (PWA) — subscription UI.
- A persisted subscription store, modelled on
  `internal/session/discussion_subs.go`.

---

## Problem and evidence

### Motivating example 1 — the hand-written BL422 watch-and-inject script (2026-10-13/14)

While BL422's model-tiering eval Automaton was running, the operator needed to
inject a corrected instruction into one specific **not-yet-started task's**
session the moment that task actually started — to override a timeout value
and to add a safety preamble to the child tasks it would generate. Nothing
could tell them when that happened. The only workaround was a disposable
script, `bl422-watch-and-inject.sh`, kept in a scratchpad directory outside
this repo, that:

1. called `datawatch autonomous prd-get <id>` (CLI defined in
   `cmd/datawatch/cli_autonomous.go:217`) about every 25 s;
2. checked whether the target task's `session_id` field had become non-empty;
3. as soon as it had, called `datawatch session send <session_id> <text>`
   (CLI `send <id> <text>`, `cmd/datawatch/main.go:9539`).

That is a by-hand version of what this design is meant to make unnecessary.
It also shows that the needed signal is **task-level** — "task T got a
`session_id`" — not only Automaton-level status. The PRD status was
`running` the whole time and never changed when the event the operator cared
about happened.

### Motivating example 2 — the BL431 overnight queue stall (2026-10-13/14)

A sibling design pass, **BL431** (Automaton `c187962a`, "Automation:
multi-Automaton orchestration (sequencing, cost, model policy, PWA)"), hit the
same gap from another angle. Queue advancement across Automata stalled for 5+
hours overnight, because nothing on the server reacted when an Automaton
reached a terminal state. The operator-confirmed sequencing addendum in
`docs/plans/README.md` (the "2026-10-14 sequencing addendum" paragraph)
records this as "the exact gap that caused a 5+ hour queue stall overnight on
2026-10-13/14 with no daemon-side detection".

BL431's design defines an **Automation**: a named, graph-shaped group of
member Automata coordinated on the server. When a member reaches a terminal
state, a coordinator decides whether to auto-advance, gate for approval,
branch, or alert on a stall. That coordinator is designed to be a
**consumer of this doc's dispatcher**, not a second hook into the same
terminal-state write path. BL431's own doc
(`docs/plans/2026-10-14-automation-orchestration-design.md`) **did not exist
in the tree when this doc was written**. Its content is described here from
the task brief and the README addendum only: **not verified against BL431's
doc text**.

### Current state in code

- **No outbound notification happens at any Automaton terminal transition.**
  In `Manager.Run`, the roll-up branch that sets `prd.Status = PRDCompleted`
  (`internal/autonomous/executor.go:677`) does three things: the BL387
  memory-report goroutine (`memoryReportFn`), `handleWorktreeCompletion` and
  `handleClusterCompletion` (`internal/autonomous/executor.go:681-722`). The
  `PRDFailed` (`:616`) and `PRDBlocked` (`:578`, `:625`, `:636`, `:663`)
  branches just `SavePRD` and return. None of them calls an alert, comm,
  webhook or push function.
- **The only existing hook is a single-slot WS refresh.** `PRDUpdateFn` /
  `SetOnPRDUpdate` (`internal/autonomous/manager.go:851-875`) holds exactly
  **one** function. `cmd/datawatch/main.go:5181` wires it to
  `httpServer.BroadcastPRDUpdate` (`internal/server/server.go:1068-1076`),
  which sends a `MsgPRDUpdate` WS frame with payload `{prd_id, status?,
  deleted?}`. The only purpose is to make the PWA reload. Limits:
  - It only fires where something explicitly calls `EmitPRDUpdate`, which
    happens in the `API` wrappers in `internal/autonomous/api.go` and in
    `setWaiting` / `clearWaiting` in `internal/autonomous/capacity.go:113,122`.
    The executor itself never calls it. The PRD-level terminal state reaches
    the WS only through `API.Run`'s goroutine, which calls `EmitPRDUpdate`
    once after `Manager.Run` returns (`internal/autonomous/api.go:344-346`).
  - Task-level transitions inside `executeOne` (`TaskInProgress`, setting
    `SessionID`, `TaskVerifying`, `TaskCompleted`) only call
    `store.SaveTask`. `Store.SaveTask` (`internal/autonomous/store.go:344`)
    has no hook, so **none of them emit anything**. This is exactly the
    signal that example 1 needed.
  - Child Automata spawned by `recurseChildPRD` run through `m.Decompose` /
    `m.Approve` / `m.Run` directly (`internal/autonomous/executor.go`, around
    lines 1263-1283), not through the `API` wrappers. So, as far as this
    read went, a child Automaton's own transitions do not emit a WS update.
    (Whether the PWA polls to cover this was **not verified**.)
- **Consumers poll today.** `autonomous_status` / `autonomous_prd_get` (MCP),
  the matching REST endpoints, and `datawatch autonomous prd-get` (CLI) are
  all request/response.

---

## Event source

The source of truth is the Automaton's own persisted state:
`PRD.Status` / `Task.Status` / `Task.SessionID`, plus the append-only
`Decision` log.

- `type Decision` (`internal/autonomous/models.go:50-66`) has the fields `At`,
  `Kind`, `Backend`, `Model`, `CostUSD`, `VerdictOutcome`, `Actor`, `Note`
  and `FailureCategory`. Its `Kind` comment lists `decompose | run | verify |
  approve | reject | request_revision | edit_task | template_instantiate |
  ci_watch`. The code also appends kinds that comment does not list:
  `archive` (`manager.go:1433`), `cancel` (`api.go:404`), `cancel_story`
  (`manager.go:1816`), `cancel_task` (`manager.go:1872`), `pause`
  (`manager.go:2554`) and `resume` (`manager.go:2576`).
- **Caveat:** the log is trimmed to the last `maxDecisionsPerPRD = 200`
  entries on every `SavePRD` (`internal/autonomous/manager.go:27-37`,
  `internal/autonomous/store.go:202`). It is an audit trail, not a durable
  event queue. A dispatcher must emit at the write site, not tail the log.
- **Not every status change appends a Decision.** For example, `PRDCompleted`,
  `PRDFailed` and `PRDBlocked` in `Manager.Run` do not, and neither do any
  task-level transitions. So the Decision log alone is **not sufficient** as
  the event source. Events have to be emitted at the status-assignment sites
  below, and a Decision (where one exists) is attached as context.
- **Scope warnings** — `PRD.ScopeWarnings` (`internal/autonomous/models.go:345`)
  is written by `LintPlanScope` after decompose (`manager.go:1103`, `:1258`,
  `:3409`). Its only consumer found is the PWA (`internal/server/web/app.js`).
  Nothing is sent outbound: it is **silent/advisory**.
- **Guardrail verdicts** are appended to `Task.Verdicts`
  (`executor.go:1382`), `Story.Verdicts` (`executor.go:1472`) and
  `PRD.Verdicts` (`ci_watch.go:568`, `rules_review.go:171`). They are surfaced
  only through the Automaton read APIs. A `block` verdict turns into
  `PRDBlocked`, which, as shown above, also notifies nobody.
- **Audit mirror:** `SetAuditFn` (`manager.go:631`) is wired at
  `cmd/datawatch/main.go:5167` to the security audit log. Today it is called
  from `internal/autonomous/file_drift.go:212`. It is an audit sink, not a
  notification path.

### PRD-level (Automaton) transition points

| New status | Site (function) | Decision appended? |
|---|---|---|
| `planning` | `manager.go:1009`, `:1164` (decompose / re-decompose) | — |
| `draft` (decompose error / reset) | `manager.go:1054,1064,1079,1214,1224,1237,1396`; `api.go:106` | — |
| `needs_review` | `manager.go:1094`, `:1249`; template instantiate `:2876` | `decompose` / `template_instantiate` |
| `approved` | `manager.go:1286` (Approve) | `approve` |
| `approved` (revert after Run error) | `api.go:339` (`API.Run` goroutine) | — |
| `rejected` | `manager.go:1340` | `reject` |
| `revisions_asked` | `manager.go:1369` | `request_revision` |
| `running` | `api.go:299` (`API.Run`); `executor.go:158` (`Manager.Run`, Decision at `:208`); `manager.go:805` (`resetKilledTasksForResume`); `manager.go:1752` (`ResetTask`); `manager.go:2574` (resume) | `run` / reset kind / `resume` |
| `paused` | `manager.go:2552` | `pause` |
| `cancelled` | `api.go:399` | `cancel` |
| `archived` | `manager.go:1431` | `archive` |
| `blocked` | `executor.go:327,340,532,546` (per-task guardrail halt paths), `:578` (per-story guardrail), `:625` (project-rules check), `:636` (rules review), `:663` (ci-watch) | — |
| `failed` | `executor.go:616` (roll-up, any task failed) | — |
| `completed` | `executor.go:677` (roll-up) | — |

### Task-level transition points

| Transition | Site (function) | Emits today? |
|---|---|---|
| → `in_progress` (attempt start, `StartedAt` set) | `executor.go:823` (`executeOne`) | No (`SaveTask` only) |
| **`session_id` first set** (session spawned) | `executor.go:894` (`executeOne`, right after `spawn` returns and `led.Bind`) | **No** — this is example 1's signal |
| `session_id` cleared before respawn | `executor.go:819`; also reset paths `manager.go:725,793,1711,3051` | No |
| → `waiting_capacity` / back | `capacity.go:110-122` (`setWaiting` / `clearWaiting`) | WS `EmitPRDUpdate` only |
| → `verifying` | `executor.go:910` | No |
| → `completed` | `executor.go:977`, `:1298` (child-Automaton path) | No |
| → `blocked` (guardrail) | `executor.go:988`, `:1279` | No |
| → `failed` | `executor.go:962,1042`; child path `:1205,1210,1238,1264,1270,1284,1294`; worker paths `:294,309,418,475,503` | No |
| → `awaiting_approval` | `manager.go:1316` | No |
| → `cancelled` | `manager.go:1798,1854` (cancel_story / cancel_task) | Decision only |
| → `pending` (reset) | `manager.go:726,791,2153,3049` | No |

"Session end" for a task is not a task-status field. It is the worker
session's own state machine (`internal/session/manager.go`'s
`onStateChange`, fired from `internal/session/state_engine.go:129,217`). A
task-level `session_ended` event would join on `Task.SessionID`.

---

## Q1 — Which transitions are notification-worthy

**Options.**

- (a) Every status change at every level. This is complete, but noisy:
  `waiting_capacity` can flip back and forth repeatedly, and `pending`
  resets come in bursts.
- (b) Only Automaton-level terminal/attention states: `needs_review`,
  `blocked`, `failed`, `completed`, `cancelled`.
- (c) (b) plus a small set of opt-in task-level events. Example 1 shows
  that `task.session_started` (first non-empty `SessionID`) is a real,
  demonstrated need, not a hypothetical. `task.session_ended` (its pair) and
  `task.failed` / `task.blocked` are natural additions.
- (d) Also expose the currently silent advisories: `scope_warnings` produced
  at decompose, and guardrail `warn`/`block` verdicts.

**Recommended:** (c) plus (d) as opt-in.

- Default set: `prd.needs_review`, `prd.blocked`, `prd.failed`,
  `prd.completed`, `prd.cancelled`. Each carries the triggering Decision when
  one exists, and the blocking verdict for `blocked`.
- Opt-in event kinds: `prd.status_changed` (every change),
  `task.session_started`, `task.session_ended`, `task.status_changed`,
  `prd.scope_warning`, `guardrail.verdict`.
- Subscriptions name the event kinds they want, and can optionally be scoped
  to one Automaton or to a single task ID (example 1 wants exactly one task).
  BL431's coordinator subscribes to the terminal set across all Automata
  that belong to an Automation.

**Configurability.** Two choices:

- Mirror the guardrail precedence in `resolveGuardrails`
  (`internal/autonomous/guardrail_registry.go:122-148`): per-Automaton
  explicit list > named profile > global `Config` list.
- Or keep it simpler: the default event set is global config, and each
  subscription carries its own filter.

The recommendation is the simpler form first: global default plus
per-subscription filters. Add a per-Automaton override field (`notify_events`
on the Automaton) only if the operator wants per-Automaton policy separate
from who is listening. That field would then resolve as
per-Automaton > profile > global, exactly like `resolveGuardrails`, so no
new precedence idea is introduced. Whether a profile level is needed at all
is left to the operator (open question 2).

---

## Q2 — Who subscribes and via what transport

Four subscriber shapes are real today or concretely planned:

1. **In-process Go callback — BL431's Automation coordinator.** This is the
   cheapest case and the most concrete present-day need. The coordinator
   lives in the same daemon binary as `internal/autonomous`. It needs a
   function call when `prd.completed` / `prd.failed` / `prd.blocked` /
   `prd.cancelled` fires. No serialization, no transport, no auth. There
   is direct precedent: the Manager already exposes a family of in-process
   `Set*Fn` hooks (`SetMemoryReportFn` `manager.go:677`, `SetAuditFn` `:631`,
   `SetSessionKillerFn` `:555`, and others), all wired in
   `cmd/datawatch/main.go`. However, each of those is **single-slot**, like
   `SetOnPRDUpdate`. The dispatcher needs **multi-subscriber** registration
   (`Subscribe(filter, func(Event)) (unsubscribe func())`) so that the PWA
   WS broadcast, the comm fan-out and the Automation coordinator can all
   attach at once. This shape must be the core API, and every other
   transport becomes an adapter registered through it. It must not be a
   special case bolted onto a webhook-shaped API.
2. **Comm channel through the existing messaging backends.** The proven path
   is the needs-input pipeline in `cmd/datawatch/main.go`:
   `SetNeedsInputHandler` (`:7355`) → `needsInputFire` (`:7261`) →
   `bundleRemoteAlert` (`:6831`). That pipeline coalesces events per session
   over a 5 s quiet period, then calls `r.SendThreaded` /
   `r.SendWithButtons` on every comm `router.Router`, filtered by
   `cfg.NotifyExclude`, plus `ntfyBackend.Send` and `emailBackend.Send`
   (`:6964-7004`). Automaton events can reuse the same fan-out, with a
   per-Automaton thread key instead of a per-session one.
3. **Raw outbound webhook URL per subscription.** The existing
   `internal/messaging/backends/webhook/backend.go` is **inbound-only**. It
   accepts `POST {"task", "project_dir"}` to start work, and its
   `Send(recipient, message string) error { return nil }` (line 56) is a
   no-op. So "use the webhook backend" does **not** give outbound delivery
   today. An outbound HTTP POST sink (URL + optional HMAC secret per
   subscription) would be new code. It is not a reuse.
4. **A live AI session, via a `discussion_subscribe`-style path** — see Q3.

Also available as sinks:

- **The mobile/UnifiedPush topic bus**: `server.PublishToTopic` /
  `PublishToTopics` (`internal/server/push.go:98-115`), which fans out to
  SSE subscribers on `GET /api/push/<topic>` and to registered mobile push
  endpoints.
- **The in-app alert store**: `alerts.Store.Add` / `AddSystem`
  (`internal/alerts/store.go:114,144`), which is the backing store for
  `create_alert` / `get_alerts`. Its listener (`cmd/datawatch/main.go:6600`)
  only notifies the web server.

**Recommended:** several at once, all as adapters over one dispatcher. In
build order:

1. in-process callback (core API, needed by BL431);
2. alert store + `PublishToTopic` + comm fan-out (cheap reuse of existing
   code);
3. session push (Q3);
4. outbound webhook URL last, because it is the only fully new network sink
   and needs SSRF/secret handling.

`channel_routing_config_get/set` (`internal/server/channel_routing.go`) does
**not** apply here: it maps *inbound* channel identities to federation peers
(`docs/howto/channel-routing.md`). It is not an outbound routing table.

---

## Q3 — How a subscribing AI session actually receives a push

Findings, separated by delivery class:

- **The `discussion_subscribe` precedent is send_input, not MCP push.**
  - `toolDiscussionSubscribe` (`internal/mcp/bl358_discussion_subscribe.go:20-26`)
    says entries are "delivered to the session via send_input".
  - `dispatchDiscussionEntry` (`:114-127`) looks up each subscriber with
    `manager.FindSessionByName(sname)` and calls
    `manager.SendInput(sess.FullID, "[discussion:<id>] <content>",
    "discussion-sub")`. The write tool in
    `internal/mcp/bl332_discussion_tools.go:61` triggers this with
    `go s.dispatchDiscussionEntry(...)`.
  - Subscriptions are persisted by `session.DiscussionSubStore`
    (`internal/session/discussion_subs.go:21-30`).
  - Concretely, the event is **text typed into a datawatch-managed tmux
    session** (or the backend's equivalent input path). It only works for a
    session that datawatch itself runs, addressed by session name. It cannot
    reach an arbitrary MCP client, and it is **not** MCP-protocol push.
- **The Claude Code channel bridge is true push, but only into
  datawatch-launched Claude Code sessions.**
  - Both bridges — the TS one, `channel/index.ts`, and the Go one,
    `cmd/datawatch-channel/main.go` — declare the experimental capabilities
    `claude/channel` and `claude/channel/permission` (`channel/index.ts:45-47`).
  - They listen on `127.0.0.1:<port>` (`channel/index.ts:250`) and turn
    `POST /send` into an MCP `notifications/claude/channel` notification
    (`channel/index.ts:199-213`). The Go bridge does the same with mcp-go's
    `SendNotificationToAllClients` (`cmd/datawatch-channel/main.go:377-408`).
  - The daemon reaches a session's bridge at its registered `ChannelPort`
    (`internal/session/store.go:84`), via
    `http://127.0.0.1:%d/send` in `internal/server/api.go:8576`.
  - Claude Code shows these as `<channel source="datawatch">` blocks.
  - Registration is per session (`RegisterSessionMCP`,
    `internal/channel/channel.go:181`). This is (b): real push, but only for
    Claude Code sessions launched by datawatch with the bridge attached.
- **The daemon's own MCP server cannot push events to a generic client
  today.**
  - `internal/mcp/server.go:308-316` builds the server with
    `WithResourceCapabilities(false, false)`; the comment says subscribe is
    deferred.
  - mcp-go v0.46.0 does have server-to-client notification APIs:
    `SendNotificationToClient`, `SendNotificationToSpecificClient` and
    `SendNotificationToAllClients` (module `server/session.go:263-345`).
    The protocol types `SubscribeRequest` and
    `notifications/resources/updated` also exist (module
    `mcp/types.go:89,716-751`).
  - But the v0.46.0 **server** request dispatcher (module
    `server/request_handler.go`) has no case for `resources/subscribe`.
    Only the client side (`client/client.go:359`) sends it. MCP
    `resources/subscribe` → `notifications/resources/updated` would mean
    datawatch handling that method itself, or upgrading mcp-go
    (**not verified** whether a later mcp-go adds a server handler).
  - Even then, a custom or `resources/updated` notification only helps if
    the client actually surfaces it to the model mid-turn. For generic MCP
    clients that is **not verified** and in general not guaranteed.
    `notifications/resources/updated` only says "re-read this URI", and most
    clients treat it as a cache hint.
  - The MCP spec text for `resources/subscribe` was **not re-read from the
    web in this pass**. The description here comes from mcp-go's own type
    comments.

Conclusion per client kind:

| Client | Class | Mechanism |
|---|---|---|
| BL431 Automation coordinator (in-process Go) | **(c) in-process direct dispatch** | Registered callback on the dispatcher. No session-push mechanism at all. Strictly easier than (a) or (b), because the event never leaves the process. |
| Claude Code launched by datawatch, bridge attached | **(b) push into datawatch-launched Claude Code sessions** | Dispatcher adapter → `POST 127.0.0.1:<ChannelPort>/send` → `notifications/claude/channel` |
| Any datawatch-managed tmux session (opencode, other non-Claude backends, Claude without the bridge) | **(a)-like "typed input"** | `SendInput`, exactly like `dispatchDiscussionEntry`. It interrupts or queues into the TUI. Not protocol push. |
| Generic MCP client over stdio / SSE (Cursor, an external Claude Code that is not datawatch-launched) | **(d) efficient polling** | A `autonomous_events_pending(since_cursor)` tool, where the next tool call returns queued events. MCP `resources/updated` is a possible later upgrade, not verified useful. |
| PWA / mobile apps | push | WS `MsgPRDUpdate` (enriched) / `PublishToTopic` SSE + UnifiedPush/APNs |

Applied to example 1: with this in place, the operator's own Claude Code
session (datawatch-launched, so class (b)) subscribes to
`task.session_started` for that one task ID. When `executor.go:894` sets
`t.SessionID`, the dispatcher delivers a `<channel source="datawatch">`
block, and the session immediately calls `send_input` itself. Simpler still,
a subscription can carry an **action**: "on `task.session_started` for task
T, `SendInput` this text into the new session". This removes the
`bl422-watch-and-inject.sh` polling script entirely, along with its 25 s
polling latency and the window in which a fast task could start before the
injection arrives.

For the BL431 Automation coordinator, none of the session-push machinery
applies. It calls `dispatcher.Subscribe(filter, fn)` once at startup.

---

## Q4 — Rate limiting and noise control

Noise sources found in code:

- `waiting_capacity` ↔ `in_progress` flips via `setWaiting` / `clearWaiting`
  (`internal/autonomous/capacity.go:107-123`), which already emit WS updates
  on every flip;
- bursts of reset-to-`pending`;
- recursive corrective-work loops (BL410 `synthesizeCorrectiveWork` →
  `m.Run` recursion in `executor.go`), which can produce repeated
  in_progress/verifying cycles on the same task.

Recommended controls:

- **Per-subscriber level filter.** `level: prd | task`. Automaton-level-only
  subscribers (comm channels by default, and the BL431 coordinator) never
  see task churn.
- **Coalescing for human-facing sinks.** Reuse the `bundleRemoteAlert`
  pattern: a per-key quiet period (5 s in `cmd/datawatch/main.go:6845`)
  that joins events as "a → b → c" into one message. Key it per Automaton.
- **The needs-input gate as an existing debounce precedent.**
  `needsInputGate.Schedule` uses `detection.alert_settle` /
  `detection.alert_repeat` (`cmd/datawatch/main.go:7355-7367`). A similar
  settle/repeat pair suppresses identical repeated events.
- **No coalescing for the in-process callback or for single-task action
  subscriptions.** The coordinator and example 1 both need every matching
  event, immediately. Deliver synchronously-queued (per-subscriber buffered
  channel), and never block the executor's write path.
- **`cfg.NotifyExclude`** (already honoured by the comm fan-out) keeps
  working for opting out per channel.

---

## Q5 — Intersection with BL422

BL422 (`docs/plans/README.md` item 21) is the Claude model/effort tiering
eval. It covers execution, decompose and verification across a
4-model × 3-effort grid, and reports which tier is safe per touchpoint. Its
results doc `docs/testing/model-tiering-eval.md` **does not exist yet**, so
nothing about its outcome is assumed here.

Example intersection, **not to be built now**: emit a
`prd.tier_mismatch` event when an Automaton's resolved model/effort for a
touchpoint differs from BL422's recommended tier for that task type. The
resolved values come from `resolveTaskBackendModel` and the effort
fall-through in `executeOne` (`internal/autonomous/executor.go:826-836`),
plus BL430's `verifier_backend` / `verifier_model` / `verifier_effort`
fields.

This is why the work is sequenced after BL422: there is no "recommended
tier" to compare against until BL422 produces one, and how that
recommendation is encoded (config map, profile, Automaton-type default) is
BL422's decision to make. **Separate follow-up**, not part of this design's
implementation PRD: "tier-policy events on top of the lifecycle dispatcher",
filed once BL422's results land. It would be one more event kind on the
same dispatcher; it does not need a new mechanism.

---

## Ruled-out mechanisms

- **`schedule_add` / `schedule_spawn` (`internal/session/schedule.go`).**
  Purely time- or input-triggered:
  - `RunAt` (a zero value means "when the session enters waiting_input",
    line 48);
  - `RunAfterID` chaining;
  - `RecurEverySeconds`;
  - 5-field `CronExpr` (line 85).

  Using it to watch Automata would only move the polling loop to another
  clock. It has no notion of Automaton or task state.
- **`alertrules` (`internal/alertrules/`).** Scoped to observer metrics. The
  package doc (`internal/alertrules/types.go`) describes rules evaluated
  against observer envelopes every 30 s, and `metricValue`
  (`internal/alertrules/evaluator.go:176-198`) recognizes only `cpu_pct`,
  `mem_pct`, `gpu_pct`, `rss_bytes`, `net_rx_bps`, `net_tx_bps`. It has no
  Automaton awareness (no PRD/autonomous references in the package), and it
  is poll-based by design.
- **Tailing the Decision log.** Trimmed at 200 entries, and many
  transitions append nothing (see Event source).
- **Extending the single-slot `SetOnPRDUpdate` as-is.** It is one function
  pointer, fires only from `API` wrappers, and carries only
  `{prd_id, status}`. It should become *one subscriber* of the new
  dispatcher (the WS adapter), not the dispatcher itself.
- **The inbound webhook backend as an outbound sink.** `Send` is a no-op
  (Q2).

---

## Recommendation

Build one **in-process lifecycle event dispatcher** in `internal/autonomous`.
It reuses existing pieces and treats direct callback registration as the
core API:

1. **Event type.** `LifecycleEvent{ID, At, Kind, Level (prd|task), PRDID,
   ParentPRDID, StoryID, TaskID, OldStatus, NewStatus, SessionID, Decision
   *Decision, Verdict *GuardrailVerdict, Host}`.
2. **Emission at the write sites** in the transition tables above, through a
   single `m.emit(ev)` helper called next to each `prd.Status =` /
   `t.Status =` / `t.SessionID =` assignment, after the store save succeeds.
   Not from `Store.Save*`, which would lose old/new status and the Decision
   context. The Decision log stays the audit record; when an event coincides
   with an appended Decision, the event carries that Decision.
3. **Multi-subscriber dispatcher.** `Subscribe(filter, fn) (cancel func())`.
   Each subscriber gets its own buffered queue and goroutine, so a slow sink
   never blocks the executor. In-process callbacks are first-class
   subscribers, not a special case. **This is what lets BL431's Automation
   coordinator consume terminal events without a second hook into the
   `PRDCompleted` / `PRDFailed` / `PRDBlocked` write path.** The coordinator
   registers `Subscribe({Kinds: terminal, Level: prd}, coord.onMemberTerminal)`
   at startup.
4. **Adapters**, wired in `cmd/datawatch/main.go` the same way the `Set*Fn`
   hooks are today:
   - WS: the existing `BroadcastPRDUpdate`, with a richer payload;
   - comm: the `bundleRemoteAlert`-style fan-out to `router.Router`s, ntfy
     and email, honouring `NotifyExclude`;
   - mobile: `server.PublishToTopic` on `alerts` and an `automaton-<id>`
     topic;
   - alert store: `alerts.Store.Add`;
   - session push: the `dispatchDiscussionEntry`-style `SendInput`, with
     the channel bridge `/send` preferred when the target session has a
     `ChannelPort`;
   - later, an outbound webhook URL.
5. **Persisted subscriptions** for the external shapes (comm / session /
   webhook / topic), stored like `session.DiscussionSubStore`. In-process
   subscriptions are code-registered and are not persisted.
6. **Pending-events queue** per external MCP subscriber, for class (d)
   clients.

How it removes example 1's script: the operator (or their datawatch-launched
Claude Code session) creates a subscription
`{kind: task.session_started, task_id: <T>, action: send_input, text: <preamble>}`.
When `executor.go:894` assigns `t.SessionID`, the dispatcher fires, and the
session adapter types the text into the new worker session within
milliseconds. No polling, no scratchpad script, no 25 s race window.

---

## Phases

Phase 1 is **Done** (v9.0.85); Phases 2–5 are **Planned**. Each phase's own checklist must include
AGENT.md's **Phase Completion Checklist** (functionality, unit tests, live
smoke against a real daemon, full `go test ./...` + `node --test`, the
8-item documentation checklist, conditional docs).

1. **Phase 1 — Done (v9.0.85).** Event type, `emit` helper at every PRD-level and
   task-level site tabulated above, multi-subscriber in-process dispatcher,
   and the existing WS broadcast migrated to be one subscriber. No new
   external surface. *(Phase Completion Checklist applies.)* Shipped as
   `internal/autonomous/lifecycle_event.go` (`LifecycleEvent`,
   `LifecycleFilter`, `Manager.Subscribe`); `cmd/datawatch/main.go`
   registers `BroadcastPRDUpdate` as a `prd.updated` subscriber. Checklist:
   unit tests + full `go test ./...` / `node --test` green; live WS smoke
   against the redeployed daemon recorded in `docs/testing.md` (v9.0.85);
   CHANGELOG + README updated; `docs/config-reference.yaml` and
   `docs/operations.md` N/A (no config keys, no operator procedure);
   `docs/testing-tracker.md` row added; definitions/flow/howto N/A (no
   operator-visible concept or workflow yet — the frame and the PWA are
   unchanged); Mobile-Parity N/A (WS `prd_update` contract unchanged);
   Localization N/A (no `app.js` change); version bumped to v9.0.85.
2. **Phase 2 — Planned.** Built-in sinks: alert store, `PublishToTopic`,
   comm fan-out with per-Automaton coalescing; global default event-set
   config. *(Phase Completion Checklist applies.)*
3. **Phase 3 — Planned.** Persisted subscriptions + REST/MCP/CLI/comm CRUD;
   session-push adapter (bridge `/send` preferred, `SendInput` fallback)
   including the `send_input` action subscription. *(Phase Completion
   Checklist applies.)*
4. **Phase 4 — Planned.** Pending-events MCP tool for generic MCP clients;
   PWA subscription UI. *(Phase Completion Checklist applies.)*
5. **Phase 5 — Planned.** Outbound webhook URL sink (HMAC, SSRF guard,
   retry/backoff), federation forwarding. *(Phase Completion Checklist
   applies.)*

---

## Follow-up implementation PRD — what it would need to build

- **Data model:** `LifecycleEvent` (above). `Subscription{ID, Owner,
  Filter{Kinds, Level, PRDID, TaskID, AutomationID?}, Sink{type:
  comm|session|topic|alert|webhook|mcp_queue, target}, Action?{send_input
  text}, Coalesce, CreatedAt, ExpiresAt?, OneShot bool}`. A one-shot flag
  covers example 1: fire once, then delete.
- **Subscription store:** JSON file under the data dir, encrypted via
  `secfile` like `channel_routing.json`, modelled on
  `internal/session/discussion_subs.go`.
- **Dispatcher hook points:**
  - `Manager.Run` roll-up (`executor.go:578-677`);
  - per-task guardrail block paths (`executor.go:327,340,532,546`);
  - `executeOne` (`:823`, `:894`, `:910`, `:977`, `:988`, failure sites);
  - `recurseChildPRD`;
  - `Manager.Approve` / `Reject` / `RequestRevision` / `Pause` / resume /
    `ResetTask` / archive / `cancel_story` / `cancel_task`
    (`internal/autonomous/manager.go`);
  - decompose completion (`manager.go:1094,1249`);
  - `API.Run` / `API.Cancel` (`internal/autonomous/api.go:299,339,399`);
  - `setWaiting` / `clearWaiting` (`internal/autonomous/capacity.go`);
  - task `session_ended` via the session `onStateChange` chain in
    `cmd/datawatch/main.go:7034`, joined on `Task.SessionID`.
- **Config keys (proposed names):**
  - `autonomous.notify.enabled`;
  - `autonomous.notify.default_events`;
  - `autonomous.notify.comm_coalesce_seconds`;
  - `autonomous.notify.webhook.allow_hosts`;
  - optional per-Automaton `notify_events` if open question 2 says yes;
  - all documented in `docs/config-reference.yaml`.
- **Surfaces:** see Parity surface.
- **Audit logging:** per AGENT.md's Audit Logging Rule, subscription
  create/delete and every outbound webhook delivery are emitted in both
  JSON-lines and CEF, through the existing audit writer (`SetAuditFn` path).
  In-process deliveries are not individually audited (too high volume, and
  not a security boundary).
- **Federation:**
  - Events carry `Host`.
  - Whether a peer's Automaton events should be forwardable to a
    subscriber on another host is **not decided here**. The
    discussion-sync path is the obvious precedent, but how it forwards was
    **not verified** in this pass.
  - Subscriptions targeting a session on another host would need a
    peer-forwarding hop, not local `SendInput`.
- **Tests:**
  - unit tests per emit site (table-driven over the transition tables);
  - dispatcher fan-out and slow-subscriber isolation;
  - coalescing;
  - one-shot `send_input` action;
  - a live sandbox smoke reproducing example 1 (subscribe to
    `task.session_started` on a fresh Automaton, assert the text reached
    the worker session);
  - an E2E story.
- **Sequencing with BL431:** BL431's Automation coordinator is designed as
  an in-process subscriber of this dispatcher, and BL431's own doc carries
  the same open question from its side. The dependency is one-way: the
  coordinator needs at least Phase 1 (event type + in-process `Subscribe`).
  Phases 2–5 are independent of BL431. Whether this implementation PRD and
  BL431's coordinator implementation PRD should be run in sequence,
  combined, or overlapped (Phase 1 first, then both in parallel) is **left
  to the operator**. The operator-confirmed README addendum already orders
  them BL427 → BL428 → BL431.

---

## Parity surface

Phase 1 (v9.0.85) adds **no** external surface: the dispatcher is
in-process only, and the WS `MsgPRDUpdate` frame keeps its existing
`{prd_id, status}` / `{prd_id, deleted:true}` shape (it is not yet enriched).
**None** of the following is implemented yet. Intent for Phases 2–5:

- **REST:** `GET/POST/DELETE /api/autonomous/subscriptions`;
  `GET /api/autonomous/events?since=<cursor>` (pending-events feed); WS
  `MsgPRDUpdate` payload enriched with event kind + task fields.
- **MCP:** `autonomous_subscribe`, `autonomous_unsubscribe`,
  `autonomous_subscriptions`, `autonomous_events_pending`.
- **CLI:** `datawatch autonomous subscribe|unsubscribe|subscriptions|events`
  (including `--task <id> --on session_started --send "<text>"` for
  example 1).
- **Comm channel:** verbs for subscribe/unsubscribe/list, scoped to the
  calling channel as the sink. Delivery uses the existing comm router
  fan-out.
- **YAML/config:** `autonomous.notify.*` keys (above) in
  `docs/config-reference.yaml`; per-Automaton `notify_events` only if
  approved.
- **PWA:** subscription list/editor on the Automaton detail view; live
  toasts driven by the enriched WS frame.
- **Android:** receives events via the existing `PublishToTopic` →
  UnifiedPush path (no new daemon surface). Subscription management is a
  `datawatch-app` issue to file when Phase 3 ships.
- **iPhone/iOS:** same as Android, via the existing APNs-backed push
  registration. The app-side UI is likewise tracked as a `datawatch-app`
  issue, not built in this repo.

---

## Open questions for the operator

1. **Default event set:** is `needs_review / blocked / failed / completed /
   cancelled` the right Automaton-level default? Should
   `task.session_started` / `task.session_ended` be on by default or only
   opt-in?
2. **Configurability shape:** global default + per-subscription filters only
   (recommended), or also a per-Automaton `notify_events` with
   per-Automaton > profile > global precedence mirroring `resolveGuardrails`?
3. **Action subscriptions:** may a subscription carry an action
   (`send_input` text into the newly started session, as in example 1), or
   should subscriptions only notify, with the subscriber acting itself?
4. **Scope warnings and guardrail `warn` verdicts:** should these
   currently silent advisories become notifiable event kinds (opt-in), or
   stay PWA-only?
5. **Outbound webhook sink:** wanted at all (Phase 5), and if so, with what
   host allow-list and secret handling?
6. **Generic MCP clients:** is the pending-events polling tool enough, or
   should datawatch also implement MCP `resources/subscribe` /
   `notifications/resources/updated` (requires handling the method itself,
   or an mcp-go upgrade)?
7. **Federation:** should Automaton events from one host be deliverable to
   subscribers on another, and through which existing federation path?
8. **Sequencing with BL431:** should this PRD's dispatcher implementation
   ship **before**, **with**, or **after** BL431's Automation-coordinator
   implementation, given that BL431 designed the coordinator as an
   in-process subscriber of this dispatcher?
9. **BL422 follow-up:** confirm that tier-mismatch events are a separate
   follow-up filed only after BL422's results land.
