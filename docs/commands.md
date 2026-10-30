# Command Reference

All commands are sent as plain text messages in the configured Signal group,
or via any other enabled messaging backend (Telegram, Matrix, Discord, Slack, Twilio, DNS channel).

> **DNS channel note:** All commands below work over DNS except `attach` (requires interactive terminal)
> and `history` (response may exceed DNS size limits). DNS commands are encoded as TXT queries;
> see [messaging-backends.md](messaging-backends.md#dns-channel-covert) for the wire format.

---

## Session ID Format

Session IDs are 4 hexadecimal characters (e.g., `a3f2`). They are randomly generated when a session starts and are unique per hostname. The full session identifier is `<hostname>-<id>` (e.g., `myserver-a3f2`), but most commands accept the short 4-character form.

---

## Hostname Prefix

Every reply from `datawatch` is prefixed with `[hostname]` to identify which machine is responding. When multiple machines share a group, each machine replies to commands independently.

Example: `[laptop][a3f2] Started session for: write unit tests`

---

## Commands

### `new: <task>`

Start a new `claude-code` session with the given task description.

**Syntax:** `new: <task description>`

**Example:**
```
new: refactor the authentication module to use JWT
```

**Response:**
```
[myserver][a3f2] Started session for: refactor the authentication module to use JWT
Tmux: cs-myserver-a3f2
Attach: tmux attach -t cs-myserver-a3f2
```

**Notes:**
- The task is passed directly to `claude-code` as the prompt
- A tmux session is created named `cs-<hostname>-<id>`
- Output is logged to `~/.datawatch/logs/<hostname>-<id>.log`

---

### `list`

List all sessions on this machine and their current state.

**Syntax:** `list`

**Example:**
```
list
```

**Response:**
```
[myserver] Sessions:
  [a3f2] running         14:32:01
    Task: refactor the authentication module to use JWT
  [b7c1] waiting_input   14:45:22
    Task: add Docker support to the project
  [c9d0] complete        13:10:05
    Task: write unit tests for config module
```

**Session states:**
| State | Meaning |
|---|---|
| `running` | claude-code is actively working |
| `waiting_input` | claude-code is waiting for a response from you |
| `complete` | Session finished (tmux exited cleanly) |
| `failed` | Session ended unexpectedly |
| `killed` | Session was terminated with `kill` |

---

### `status <id>`

Show recent output from a session (last 20 lines by default).

**Syntax:** `status <id>`

**Example:**
```
status a3f2
```

**Response:**
```
[myserver][a3f2] State: running
Task: refactor the authentication module to use JWT
---
  Updating auth/jwt.go...
  Running tests...
  All tests passed.
  Creating commit...
```

---

### `tail <id> [n]`

Show the last N lines of a session's output log. Default is 20.

**Syntax:** `tail <id> [n]`

**Examples:**
```
tail a3f2
tail a3f2 50
tail a3f2 5
```

**Response:**
```
[myserver][a3f2] Last 50 lines:
<output lines>
```

---

### `send <id>: <message>`

Send input to a session that is waiting for a response.

**Syntax:** `send <id>: <your message>`

**Example:**
```
send b7c1: yes, proceed with the changes
```

**Response:**
```
[myserver][b7c1] Input sent.
```

**Notes:**
- The session must be in `waiting_input` state
- The input is sent to the tmux session as if you typed it at the terminal
- After sending, the session transitions back to `running`

---

### `kill <id>`

Terminate a session immediately.

**Syntax:** `kill <id>`

**Example:**
```
kill a3f2
```

**Response:**
```
[myserver][a3f2] Session killed.
```

**Notes:**
- This kills the tmux session, which terminates `claude-code`
- Session state is set to `killed`
- This action cannot be undone

---

### `attach <id>`

Get the tmux attach command to view the session interactively.

**Syntax:** `attach <id>`

**Example:**
```
attach a3f2
```

**Response:**
```
[myserver][a3f2] Run on myserver:
  tmux attach -t cs-myserver-a3f2
```

**Notes:**
- You must SSH into the host machine to attach
- Attaching lets you interact with claude-code directly from the terminal

---

### `help`

Show the command reference.

**Syntax:** `help`

**Response:**
```
[myserver] datawatch commands:
new: <task>       - start a new claude-code session
list              - list sessions + status
status <id>       - recent output from session
send <id>: <msg>  - send input to waiting session
kill <id>         - terminate session
tail <id> [n]     - last N lines of output (default 20)
attach <id>       - get tmux attach command
restart           - restart the datawatch daemon
setup <service>   - configure a backend (signal/telegram/discord/slack/matrix/twilio/ntfy/email/webhook/github/web)
copy [id]         - get last LLM response (default: most recent session)
prompt [id]       - get last user prompt sent to a session
remember: <text>  - save a memory for the current project
recall: <query>   - semantic search across memories
memories [n]      - list recent memories (default 10)
memories reindex  - re-embed all memories after model change
memories tunnels  - show cross-project room connections
forget <id>       - delete a memory by ID
learnings [search: <query>] - list or search task learnings
kg query <entity> - knowledge graph query entity relationships
kg add <s> <p> <o> - add a relationship triple
kg timeline <entity> - chronological entity history
kg stats          - knowledge graph statistics
pipeline: t1 -> t2 -> t3 - chain tasks in a pipeline
pipeline status [id] - show pipeline status
pipeline cancel <id> - cancel a pipeline
configure <key>=<val> - change a config setting at runtime
configure list    - list configurable keys
help              - show this help
```

---

### `copy [id]`

Get the last captured LLM response for a session.

**Syntax:** `copy` or `copy <session_id>`

**Example:**
```
copy
[myserver] Last response [a3f2]:
  **The auth module has been refactored to use JWT...**
```

**Notes:**
- If no ID given, uses the most recently updated session
- Uses rich markdown formatting on Slack/Discord/Telegram (code blocks, bold)
- Response captured from `/tmp/claude/response.md` or tmux fallback

---

### `prompt [id]`

Get the last user prompt sent to a session.

**Syntax:** `prompt` or `prompt <session_id>`

---

### `remember: <text>`

Save a memory for the current project with vector embedding for semantic search.

**Syntax:** `remember: <text to remember>`

**Image attachment:** send `remember:` (with optional caption) alongside a photo/image. The vision service describes the image and the description is stored as the memory text: `remember: [image: <desc>] <caption>`. Requires `vision.enabled: true`.

**Example:**
```
remember: the CI pipeline requires Go 1.24 and golangci-lint must pass
[myserver] Saved memory #4
```

**Example (image attachment):**
```
remember: architecture diagram [+ image attached]
[myserver] Saved memory #5   ← stored as "remember: [image: three-tier diagram with Redis cache] architecture diagram"
```

---

### `recall: <query>`

Semantic search across all stored memories, ranked by similarity.

**Syntax:** `recall: <search query>`

**Example:**
```
recall: CI requirements
[myserver] Recall results:
  #4 [60%] manual: the CI pipeline requires Go 1.24 and golangci-lint must pass
  #5 [48%] manual: production deploys go through staging first
```

---

### `memories [n]`

List the N most recent memories (default 10).

**Subcommands:**
- `memories reindex` — re-embed all memories after changing the embedding model
- `memories tunnels` — show rooms that appear in multiple projects (cross-project links)

---

### `forget <id>`

Delete a memory by its numeric ID.

**Syntax:** `forget 42`

---

### `learnings [search: <query>]`

List or search task learnings extracted from completed sessions.

---

### `kg query <entity>`

Query the knowledge graph for all relationships involving an entity.

**Syntax:** `kg query Alice`

**Example:**
```
kg query Alice
[myserver] KG: Alice
  #1 Alice works_on datawatch (from 2026-04-09)
  #2 Alice uses Claude (from 2026-04-09)
```

---

### `kg add <subject> <predicate> <object>`

Add a relationship triple to the knowledge graph.

**Syntax:** `kg add Alice works_on datawatch`

---

### `kg timeline <entity>`

Get the chronological history of an entity's relationships.

---

### `kg stats`

Get knowledge graph statistics (entity count, triple count, active vs expired).

---

### `pipeline: <spec>`

Chain tasks in a pipeline DAG. Tasks execute in dependency order with parallelism.

**Syntax:** `pipeline: task1 -> task2 -> task3`

**Example:**
```
pipeline: analyze auth code -> write JWT middleware -> update tests
[myserver] Pipeline started: [pipe-12345] 3 tasks (0 done, 0 running, 3 pending)
```

**Subcommands:**
- `pipeline status` — list all pipelines
- `pipeline status <id>` — show specific pipeline
- `pipeline cancel <id>` — cancel a pipeline

**How it works:**
1. Each task in the chain runs as a separate datawatch session
2. Tasks execute in dependency order — task2 waits for task1 to complete
3. Up to 3 tasks run in parallel (configurable) when dependencies allow
4. If any task fails, the pipeline stops and remaining tasks are cancelled
5. Cycle detection prevents circular dependencies

**Examples:**
```
# Linear chain — each step waits for the previous
pipeline: read the auth module -> refactor to use JWT -> write integration tests

# Check status
pipeline status
[myserver] Pipelines:
  [pipe-42851] 3 tasks (1 done, 1 running, 1 pending)

# Check specific pipeline
pipeline status pipe-42851
[myserver] [pipe-42851] refactor auth: 3 tasks (1 done, 1 running, 1 pending)
  t1: read the auth module [completed] session a3f2
  t2: refactor to use JWT [running] session b7c1
  t3: write integration tests [pending]

# Cancel a running pipeline
pipeline cancel pipe-42851
[myserver] Pipeline pipe-42851 cancelled.
```

**Access methods:**
| Method | How |
|--------|-----|
| Comm channel | `pipeline: task1 -> task2 -> task3` |
| API | `POST /api/test/message` with `{"text": "pipeline: ..."}` |
| Web UI | (planned) |
| MCP | (planned) |

---

## Implicit Reply

If exactly one session on a machine is in `waiting_input` state, you can reply without specifying the session ID. Just type your response directly.

**Example:**

```
[myserver][b7c1] Needs input:
Found 3 existing migration files. Overwrite? [y/N]

Reply with: send b7c1: <your response>
```

You can simply reply:

```
y
```

And `datawatch` routes it to `b7c1` automatically.

If multiple sessions are waiting for input, the implicit reply is rejected and you must use the explicit `send <id>: <message>` format.

### `alerts [n]`

Show the last N alerts. Default is 5.

**Syntax:** `alerts [n]`

**Examples:**
```
alerts
alerts 10
```

**Response:**
```
[myserver] Last 5 alert(s):
  [a1b2] 14:30:01 INFO — Rate limit detected
    claude-code rate-limited; session a3f2 paused
  [c3d4] 14:28:44 WARN — Trust dialog
    Permission dialog detected in session b7c1
```

**Notes:**
- Alerts are also sent proactively to all messaging backends when they fire
- The full alert history is viewable in the Web UI Alerts tab
- When a session is waiting for input, the alert includes the last N non-empty lines of terminal output as context (default 10, configurable via `session.alert_context_lines`)

---

### `schedule-results`

Read the **scheduled-job result panel** — the same data the PWA panel and the `datawatch schedule-results` CLI show (this channel command is read-only; posting is done by the job itself with `datawatch schedule-results post`).

| Syntax | Description |
|---|---|
| `schedule-results` | One line per schedule: last status icon (✓ ok / ⚠ warn / ✗ error), one-line summary, elapsed time |
| `schedule-results list` | Same as above |
| `schedule-results <schedule_name>` | Recent history for that schedule, most recent first (up to 10 lines; the complete history is on the CLI/PWA) |
| `schedule-results history <schedule_name>` | Alias for the previous one |

Examples:
- `schedule-results` → all schedules and their most recent result
- `schedule-results imap-hourly-rules` → recent results for that one schedule

### `setup <service>`

Start an interactive setup wizard for a backend or subsystem. Can be sent from any connected messaging channel.

**Example:**
```
setup telegram
setup llm aider
setup session
setup mcp
```

**Response (first step):**
```
[myserver] Telegram Setup
...
Step 1/3: Enter bot token:
```

**Available services:**

*Messaging backends:* `signal`, `telegram`, `discord`, `slack`, `matrix`, `twilio`, `ntfy`, `email`, `webhook`, `github`, `web`, `server`

*LLM backends:* `llm claude-code`, `llm aider`, `llm goose`, `llm gemini`, `llm opencode`, `llm ollama`, `llm openwebui`, `llm shell`

*Session and MCP:* `session`, `mcp`

*Integrations:* `rtk` (RTK token savings), `ebpf` (per-session network tracking)

**Notes:**
- Signal setup cannot be performed over a messaging channel (QR code required). You will receive instructions to run `datawatch setup signal` on the host machine.
- Type `cancel` or `abort` at any prompt to exit the wizard.

### `autonomous subscribe <kinds|short-names> [prd=<id>] [task=<id>]`

Subscribe this comm channel to Automaton (PRD/task) lifecycle events
(BL427 phase 3). When a matching event fires, the daemon delivers it back
over this channel's comm sink, batched per Automaton during the quiet
window (coalesced by default).

**Syntax:** `autonomous subscribe <kinds> [prd=<id>] [task=<id>]`

- `<kinds>` — one or more event kinds, comma-separated. Shorthand words
  map to task kinds (`session_started` = `task.session_started`);
  full kinds pass through (`prd.blocked`, `task.completed`), and any
  other bare word is treated as a task status kind (`waiting_capacity`
  → `task.waiting_capacity`).
- `prd=<id>` / `task=<id>` — scope the subscription to one Automaton or
  one task.

**Example:**
```
autonomous subscribe session_started,prd.blocked prd=prd_a3f9
```
→ `subscribed sub_a3f2  kinds=task.session_started,prd.blocked  prd=prd_a3f9  sink=comm -> telegram:-1001234  coalesce=yes  (owner telegram:-1001234)`

Each channel's identity (`<backend>:<group-id>`) becomes both the sink
**target** and the **owner** — a channel receives the events it
subscribed to and manages only its own subscriptions.

### `autonomous subscriptions`

List this channel's lifecycle-event subscriptions (owner-scoped — you
never see other channels' subscriptions). One record per line:
```
sub_a3f2  kinds=task.session_started,prd.blocked  prd=prd_a3f9  sink=comm -> telegram:-1001234  coalesce=yes
```

### `autonomous unsubscribe <id>`

Delete one of **this channel's** subscriptions. The id is verified
against this channel's owner-scoped list before the delete reaches the
deemon — a channel that created a subscription (or the daemon itself)
removes it:
```
autonomous unsubscribe sub_a3f2
```
→ `deleted sub_a3f2`

> The other `autonomous` verbs (status, list, plan-review, …) work from
> a comm channel the same way; this section documents the lifecycle
> subscription verbs. The CLI equivalents are
> `datawatch autonomous subscribe/unsubscribe/subscriptions` (richer
> flags, including session/topic/alert sinks).

### `automation set-budget <id> key=value...`

Set, replace or clear the spending limit of an Automation (a chain of
Automata), or of one of its members. It does not start, pause or resume
anything by itself. The arguments are `key=value` pairs; the policy keys
replace the whole limit.

**Syntax:**
```
automation set-budget <id> ceiling_type=dollar|allocation_pct ceiling_value=<n> window=automation|day|week|month
    [pacing=none|even] [on_breach=pause|block] [backend=<llm>] [allocation_estimate=true]
    [peer_window=<a,b>] [reset_time=HH:MM] [reset_weekday=<day>] [reset_day=1..28] [reset_zone=<zone>]
    [member=<id>] [estimated_cost_usd=<n>] [expected_version=<n>]
automation set-budget <id> clear=true [member=<id>] [expected_version=<n>]
```

- `ceiling_type`, `ceiling_value` and `window` are required together.
  Only a `dollar` ceiling can stop work; `allocation_pct` shows an estimate
  and never pauses or blocks.
- `on_breach` defaults to `pause`: the Automation pauses and resumes by
  itself at the window reset when the reset time is known, and is blocked
  until you resume it when the reset time is unknown. `block` always blocks.
  An alert is raised either way.
- `peer_window` names, comma separated, the other daemons whose usage counts
  toward the ceiling (off by default; no automatic confirmation that a peer
  shares the account).
- `member=<id>` targets that member's own limit; `estimated_cost_usd=<n>`
  with `member` declares its expected cost (0 removes it).
- `clear=true` removes the limit and cannot be combined with policy keys.
- `expected_version=<n>` must match the Automation's current version or the
  daemon refuses with a 409.

**Example:**
```
automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week on_breach=pause
```
→ `automation ab12cd34 (nightly build): status active, version 8, 3 members`
followed by the budget block: scope, `spent: $0.00 of $40.00 ceiling (week window)`, the
window, the reset time, `on breach: pauses and resumes by itself at the reset`, and one
line per counted peer.

`automation status <id>` prints the same budget block. A mistake in the
arguments is answered with the usage line and nothing is sent to the daemon.

---

### `progress [automation=<id>] [tag=<id>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]` / `progress trend [days]`

Show how far the Automata have got and when the rest should be done. Both
verbs are read-only and print the same text as `datawatch autonomous progress`
and `datawatch autonomous progress-trend`, built from the same object as
`GET /api/autonomous/progress`, `GET /api/autonomous/progress/trend` and the
`autonomous_progress` and `autonomous_progress_trend` MCP tools.

**Syntax:**
```
progress
progress tag=<id> since=2026-10-01
progress automation=<id>
progress trend 14
```

- `automation=<id>` counts only the Automata of one Automation; `tag=<id>`
  only the Automata carrying one tag. `since` is the first day counted and
  `until` the last (inclusive); both are `YYYY-MM-DD`. A mistake in the
  arguments is answered with the usage line and nothing is sent to the daemon.
- `progress trend [days]` reads the saved daily snapshots for the last `days`
  days (1 to 365, default 30) and says whether the completion range is
  converging, widening, flat or unknown.

**What the reply shows:**
- a header with the calendar days, the active days after the inactive windows
  are left out, and the number of lanes;
- one row each for feature, bug (and other, when it has any) and the total:
  Automata, done, running, waiting, blocked, tasks, done and left, work time,
  the median wall time of a finished Automaton and the mean time of a task;
- `LEFT`: the known work (Automata that exist) and the estimated work (rows
  not filed yet, Automata with no tasks yet) on separate lines, then the
  combined figure, labelled as an estimate;
- `ETA`: the completion range in active days with the method behind each
  figure and what drives the spread;
- `LEAD`, `WAITS` (every inactive window, never hidden) and the first 14
  backlog rows not filed yet, as plain item text.

Anything that has not been measured reads `not measured`, never zero.

---

### `federation fingerprint list [state]` / `federation fingerprint confirm <id>` / `federation fingerprint reject <id>`

List the **pending matches** this daemon found while reconciling resource fingerprints with its federation peers, and **decide** them yourself. A match means the two daemons appear to be holding the *same underlying resource* — a compute node (e.g. the same GPU), or an LLM entry billed to a shared account. **Confirm** makes those two resources one shared pool (for a compute node, the peer's admitted work now also counts against our admission capacity; rejecting again stops that). **Reject** keeps them as separate things (and undoes a prior confirmation). Nothing is confirmed automatically: a match stays pending until we decide.

```
federation fingerprint list [pending|confirmed|rejected]      # default: all states
federation fingerprint confirm <id>                            # shared pool from now on
federation fingerprint reject <id>                             # keep separate
```

→ `mm2f8c01  pending  node  local=node-a  peer=beta  peer_name=node-b`
followed by one line noting what confirm/reject do. A bad state word, or an unknown id, is replied with a usage line and nothing is sent to the daemon.

### `federation peer fingerprint <name> on|off`

Opt a peer **in or out** of the fingerprint exchange. The flag is `fingerprint_sync` on the peer record: per-peer, off by default, and it needs to be on **both sides** — we must also be opted in on that peer's own copy of us. (The LLM-entry side of the exchange is a label, not a toggle: set `billing_group=<label>` in `llm add|update`.)

```
federation peer fingerprint beta on
```
→ the peer's record in JSON, now with `fingerprint_sync: true`.

> The exchange never sends raw values — only per-peer-pair **hashes** of the node's own identity (or an LLM entry's billing-group label), keyed on the peer pair — so neither peer can learn our value from the other's hash, and a peer token without federation control capabilities cannot even read the exchange endpoint at all. See `docs/howto/federation-fingerprint.md`.

---

### `usage` / `usage advice` / `usage reading` / `usage extra` / `usage work`

The account **usage calculator** — the account's usage verdict, lane count, pace, policy and typed figures — the same data the REST, MCP and PWA surfaces show (design: `docs/plans/2026-10-23-usage-calculator-design.md`). Every reply stays within 50 columns and closes with the forms the operator can send back.

| Form | Meaning |
|---|---|
| `usage` | The advice for the configured variant, policy and lane count. Same as `usage advice`. |
| `usage advice [variant=V] [policy=P] [lanes=N]` | Advice for this one call. Overrides apply to this call only and are not saved. |
| `usage reading <percent> [lanes=N] [session=N]` | Store a typed weekly percent (optionally a lane count and a second session's percent), then show the refreshed advice. |
| `usage extra <points>` | Set the bought extra-credit points, then show the refreshed advice. |
| `usage work <points\|clear>` | Set (or clear) the typed remaining-work figure, then show the refreshed advice. When cleared, the per-task estimate stands in. |

The `variant` / `policy` / `lanes` / `session` keys are case-insensitive; values pass through verbatim and the daemon validates them.

**Example:**
```
usage advice variant=lane-planner lanes=3
[myserver] usage advice
  … 50-column advice text …
  reply with one of:
    usage advice [variant=V] [policy=P] [lanes=N]
    usage reading <pct> [lanes=N] [session=N]
    usage extra <pts>
    usage work <pts|clear>

usage reading 72.5 lanes=3 session=41
[myserver] usage reading
  reading stored: 72.5%
  … refreshed advice …
```

**Notes:**
- `usage` / `usage advice` are read-only and reflect the stored `usage.calculator.*` settings; set them with `configure usage.calculator.variant=…` or `configure usage.calculator.policy=…`. A per-call `variant=` / `policy=` saves nothing.
- `reading`, `extra` and `work` write first; the reply acknowledges the write, then shows the refreshed advice.
- A bare or unrecognized form answers with the grammar lines and touches nothing on the daemon.
- This is the comm-channel view of the calculator; it is separate from `usage_backoff` (the per-backend rate-limit hold).

---

## CLI Commands

In addition to the messaging interface, datawatch has a full CLI for local session management. These run against the data directory directly (or talk to the running daemon via HTTP).

### `datawatch start [flags]`

Start the datawatch daemon. By default, daemonizes (background process + PID file at `~/.datawatch/daemon.pid`). Use `--foreground` to run in the current terminal.

| Flag | Default | Description |
|---|---|---|
| `--foreground` | false | Run in the foreground (no daemonize, log to stdout) |
| `--llm-backend <name>` | config value | Override the active LLM backend for this run |
| `--host <addr>` | config value | Override HTTP server bind address |
| `--port <n>` | config value | Override HTTP server port |
| `--no-server` | false | Disable the HTTP/WebSocket PWA server |
| `--no-mcp` | false | Disable the MCP server |
| `--verbose` / `-v` | false | Enable debug logging |

**Notes:**
- In daemon mode, logs go to `~/.datawatch/daemon.log` and the PID is written to `~/.datawatch/daemon.pid`.
- With an encrypted config (`--secure`), use `--foreground` — daemon mode cannot prompt for a password.

### `datawatch update [--check]`

Check for and install updates.

| Flag | Description |
|---|---|
| `--check` | Only check; do not install |

Queries the GitHub releases API for the latest version tag. If a newer version is found and `--check` is not set, runs `go install github.com/dmz006/datawatch/cmd/datawatch@vX.Y.Z`.

### `datawatch status`

Show whether the daemon is running and list all active sessions.

```bash
datawatch status
```

Output:
- Daemon state: `running (PID 12345)` or `stopped`
- Table of active sessions: ID, STATE, BACKEND, UPDATED, NAME/TASK
- Sessions in `waiting_input` state are shown as `WAITING INPUT ⚠`

Falls back to the local session store if the daemon API is not reachable (e.g. running without the web server).

---

### `datawatch stop [flags]`

Stop a running datawatch daemon.

| Flag | Default | Description |
|---|---|---|
| `--sessions` | false | Also kill all active AI sessions before stopping |

Reads `~/.datawatch/daemon.pid` and sends SIGTERM to the daemon process.

### `datawatch restart`

Stop the running daemon and start a fresh one. Active AI sessions in their tmux windows are preserved.

Equivalent to `datawatch stop && datawatch start` but in a single command. The new daemon re-reads the config file.

The `restart` command is also available via:
- **All messaging backends**: send `restart` to your Signal/Telegram/Discord/Slack/Matrix group
- **Web UI**: Settings → About → Restart button (calls `POST /api/restart`)
- **MCP**: `restart` tool

### `datawatch setup <service>`

Interactive wizard to configure a backend or subsystem. Available services:

**Messaging backends:**

| Service | Description |
|---|---|
| `signal` | Link a Signal account (delegates to `datawatch link`) |
| `server` | Add or update a remote datawatch server connection |
| `telegram` | Configure a Telegram bot |
| `discord` | Configure a Discord bot |
| `slack` | Configure a Slack app |
| `matrix` | Configure a Matrix bot |
| `twilio` | Configure Twilio SMS |
| `ntfy` | Configure ntfy push notifications |
| `email` | Configure SMTP email |
| `webhook` | Configure a generic HTTP webhook receiver |
| `github` | Configure a GitHub webhook receiver |
| `web` | Enable/disable the web UI and configure port/TLS |

**LLM backends:**

| Service | Description |
|---|---|
| `llm claude-code` | Configure claude CLI binary and permission settings |
| `llm aider` | Configure aider (binary path, enable/disable) |
| `llm goose` | Configure goose (binary path, enable/disable) |
| `llm gemini` | Configure Gemini CLI (binary path, enable/disable) |
| `llm opencode` | Configure opencode (binary path, enable/disable) |
| `llm ollama` | Configure Ollama (model, host, enable/disable) |
| `llm openwebui` | Configure OpenWebUI (URL, model, API key, enable/disable) |
| `llm shell` | Configure shell script backend (script path, enable/disable) |

**Session and MCP:**

| Service | Description |
|---|---|
| `session` | Configure session defaults (LLM backend, max sessions, timeout, etc.) |
| `mcp` | Configure the MCP server (enable, SSE, port, TLS, token) |

The `setup` command is also available via any active messaging backend (Signal, Telegram, Discord, Slack, Matrix, etc.). Send `setup telegram` in your Signal group to start the Telegram setup wizard interactively. Send `setup llm aider` to configure the aider backend from any messaging channel.

### `datawatch test [--pr]`

Collect status and configuration details for all enabled interfaces, and optionally open a GitHub PR updating `docs/testing-tracker.md`.

| Flag | Description |
|---|---|
| `--pr` | Open a GitHub PR with collected interface details |

**Example:**
```bash
# Show interface status summary
datawatch test

# Collect details and open a GitHub PR updating testing-tracker
datawatch test --pr
```

**What it collects (non-sensitive details only):**
- Which interfaces are enabled/disabled
- Endpoints, binary paths, model names, hostnames — never tokens, passwords, or API keys
- Validation checklists for each enabled interface

**Notes:**
- Requires `gh` CLI for `--pr` (GitHub CLI)
- Phone numbers are partially masked (last 4 digits only)
- Tokens and secrets are never included in output or PRs

### `datawatch session new [flags] <task>`

Start a new AI coding session.

| Flag | Description |
|---|---|
| `--dir` / `-d` | Project directory (default: current working directory) |
| `--name` / `-n` | Optional human-readable name for the session |
| `--backend` | LLM backend to use for this session (e.g. `claude-code`, `aider`) |

**Example:**
```bash
datawatch session new --name "auth refactor" --backend aider "refactor the auth module"
```

### `datawatch session rename <id> <name>`

Set or update the human-readable name for a session.

**Example:**
```bash
datawatch session rename a3f2 "auth refactor"
```

### `datawatch session stop-all`

Kill all running and waiting-input sessions on this host. Equivalent to calling `kill` on each active session.

**Example:**
```bash
datawatch session stop-all
```

### `datawatch backend list`

List all registered LLM backends. The active backend (from config or `--llm-backend`) is marked with `*`.

**Example output:**
```
BACKEND      ACTIVE  VERSION
claude-code  *       1.2.3
aider                0.58.0
goose
```

### `datawatch --server <name> <command>`

Target a remote datawatch server for any CLI command. The server must be configured with `datawatch setup server`.

```bash
# List sessions on a remote server named "prod"
datawatch --server prod session list

# Start a session on remote "pi"
datawatch --server pi session new "fix the auth bug"

# Stop the remote daemon
datawatch --server prod stop --sessions
```

### `datawatch session schedule add <session-id> <command> [flags]`

Schedule a command to be sent to an existing session.

| Flag | Description |
|---|---|
| `--at` | When to run: `now`, `HH:MM` (24h), or RFC3339 timestamp. Default: on next input prompt |
| `--session-name` | Target session by name instead of positional ID |
| `--cron` | 5-field cron expression for recurring fires (e.g. `"*/5 * * * *"`) |
| `--schedule-name` | Human-readable label for lookup and cancel |

```bash
# Run when session next asks for input
datawatch session schedule add a3f2 "yes, continue"

# Run at 14:30 today
datawatch session schedule add a3f2 "run the tests" --at 14:30

# Recurring: ping the session every 5 minutes by name
datawatch schedule add --session-name worker "status update" --cron "*/5 * * * *" --schedule-name heartbeat
```

### `datawatch schedule spawn [flags]`

Schedule a fresh ephemeral session to spawn at a time or on a cron, independent of any running session. The spawned session is automatically terminated when it outputs `DATAWATCH_COMPLETE:`.

Includes **overlap guard**: if the previous spawn for this schedule is still running, the cron fire is skipped and `RunAt` advances to the next slot instead of stacking a concurrent run.

| Flag | Description |
|---|---|
| `--task` | Task prompt for the spawned AI session |
| `--shell` | Shell command to run (sets `--subprocess` automatically; no LLM required) |
| `--path` | Working directory (alias for `--dir`) |
| `--dir` | Working directory for the spawned session |
| `--backend` | LLM backend name (e.g. `claude-code`) |
| `--model` | Model name override |
| `--llm-ref` | Unified LLM registry reference (alternative to `--backend`) |
| `--effort` | Thoroughness: `quick`, `normal`, or `thorough` |
| `--cron` | 5-field cron for recurring spawns (e.g. `"0 * * * *"` for hourly) |
| `--at` | First fire time: `HH:MM` or RFC3339 |
| `--schedule-name` | Name for this schedule entry (for lookup/cancel) |
| `--name` | Human-readable name for each spawned session |
| `--one-shot` | Auto-terminate on `DATAWATCH_COMPLETE:` (default: true) |
| `--ephemeral` | Reap workspace directory when session is deleted |
| `--subprocess` | Run via `bash -c`; exit code signals completion. No Claude Code TUI. |

A recurring spawn (`--cron`) survives a failed start: if a fire cannot start its session (for example the
session cap is reached), the failure is recorded (`last_fire_result: failed`, `consecutive_failures`), a
system alert is raised on the first failure and daily after, and the schedule is re-armed for its next time.
A one-time spawn that fails to start is marked `failed`.

```bash
# Pure shell job — run imap-mcp rule engine hourly (no LLM needed)
datawatch schedule spawn \
  --cron "0 * * * *" \
  --path /home/user/workspace/imap-mcp \
  --schedule-name imap-hourly-rules \
  --shell "imap-mcp run-rules --config ~/workspace/dmz-imap.yaml"

# AI task — nightly report with Claude Code
datawatch schedule spawn \
  --cron "0 2 * * *" \
  --path /home/user/workspace/project \
  --backend claude-code \
  --schedule-name nightly-report \
  --task "Generate a summary of today's commits and open issues"

# Run once at a specific time
datawatch schedule spawn --at 23:00 --shell "rsync -avz /data /backup"
```

`schedule list` shows run history: FIRES / LAST-FIRE / LAST-RESULT columns (values: `spawned`, `skipped`, `failed`).

### `datawatch schedule-results [list | history | post]`

Read and post the structured result panel for scheduled jobs — the same data the PWA result panel and the MCP `post_result` / `schedule_results_list` tools operate on.

| Subcommand | What it does |
|---|---|
| `datawatch schedule-results list` | Latest result per schedule: status, title, summary, posted time, retained count, recent-status sequence. `--json` prints the raw API response. |
| `datawatch schedule-results history \<schedule_name\>` | Full retained history for one schedule, oldest first (e.g. `datawatch schedule-results history "imap-mcp run-rules"`). |
| `datawatch schedule-results post …` | Post a structured result for a session (`POST /api/sessions/{id}/result`) |

`post` flags:

| Flag | Required | Description |
|---|---|---|
| `--title` | yes | Human-readable result title |
| `--status` | no (default `ok`) | `ok`, `warn`, or `error` |
| `--summary` | no | One-line summary |
| `--metrics` | no | JSON object of scalar metrics, e.g. `{"active_rules": 40}` |
| `--table` | no (repeatable) | JSON table: `{"title": "…", "columns": […], "rows": [[…]]}` |
| `--markdown` | no | Longer markdown report / findings section |
| `--file` | no | Seed the payload from a JSON file; individual flags override its fields |
| `--session` | outside a spawned session | Session to post for; defaults to `DATAWATCH_SESSION_ID` inside a spawned scheduled-job session |

```bash
# From inside a spawned scheduled-job session (no --session needed;
# the daemon injects DATAWATCH_SESSION_ID + DATAWATCH_RESULT_TOKEN):
datawatch schedule-results post \
  --title "40 active rules" \
  --status ok \
  --summary "2 messages actioned" \
  --metrics '{"active_rules": 40, "actioned": 2}' \
  --table '{"title":"Actions","columns":["action","count"],"rows":[["reply",1],["flag",1]]}'

# From outside, on behalf of a session (needs an operator token):
datawatch schedule-results post \
  --session host-a1b2 \
  --title "40 active rules" \
  --status ok \
  --file result.json

# Inspect what the panel is showing:
datawatch schedule-results list
datawatch schedule-results history nightly-report
```

Payload rules (enforced client-side before any request and re-checked by the daemon): `title` required; `status` one of `ok`/`warn`/`error`; `metrics` a flat object of scalars; at most 20 tables of 100 rows each; long strings are truncated with a `…(truncated)` marker; the whole payload must serialize to ≤ 64 KB. A session that belongs to a named schedule keeps its last N results (default 20, `results.schedule_history_limit`); every post also updates the session's status-board result slot.

### `datawatch session schedule list`

List all scheduled commands, including run history for spawn-type entries (FIRES, LAST-FIRE, LAST-RESULT columns).

### `datawatch session schedule cancel <schedule-id-or-name>`

Cancel a pending scheduled command by ID or schedule name.

### `datawatch session send <session-id> <text>`

Send text input directly to a session via the dedicated `POST /api/sessions/send` endpoint. Unlike the general command router, this bypasses text-command parsing so the text is always delivered as raw input.

```bash
datawatch session send a3f2 "yes, proceed"
```

### `datawatch cmd add <name> <command>`

Add a named command to the saved command library.

```bash
datawatch cmd add approve "yes"
datawatch cmd add reject "no"
datawatch cmd add abort $'\x03'   # Ctrl-C
```

### `datawatch cmd list`

List all saved commands.

### `datawatch cmd delete <name>`

Delete a saved command by name.

### `datawatch seed`

Pre-populate the command library and output filter store with useful defaults.

Seeded commands: `approve` (yes), `reject` (no), `enter` (newline), `continue`, `skip`, `abort` (Ctrl-C)

Seeded filters include common claude-code patterns:
- `Do you want to proceed?` → schedule auto-approve
- Rate limit messages → alert
- Trust dialog → alert (no auto-approve)

Existing entries are never overwritten by `seed`.

### `datawatch reload`

Hot-reload daemon config (BL17 — SIGHUP equivalent). Re-reads
`~/.datawatch/config.yaml` and re-applies messaging + LLM + observer
wiring without restarting the daemon. Use after `datawatch config set`
or after editing the YAML directly. For changes that aren't
hot-reloadable (port + log path), use `datawatch restart`.

```bash
datawatch reload
```

Posts to `/api/reload` on the running daemon. Same surface available
via the MCP `reload` tool and the SIGHUP signal.

### `datawatch ask <question>`

Single-shot LLM ask — no tmux session, no log files. Returns the
answer to stdout. Routes through `/api/ask` which honors the
`autonomousGuardrail` redaction in autonomous-mode environments.

| Flag | Default | Description |
|---|---|---|
| `--backend` | `ollama` | `ollama` or `openwebui` |
| `--model` | (backend default) | Model name override |

### `datawatch assist <question>`

Like `ask` but routes through the rules engine first — applies
routing-rules, RTK token-savings logging, and project-context
auto-attach if a project profile maps the cwd.

### `datawatch autonomous <subcommand>`

PRD lifecycle — full CRUD parity with the REST + MCP surfaces.

| Subcommand | Description |
|---|---|
| `status` | One-shot status (running PRDs, child PRD count, max recursion depth, blocked PRDs, verdict counts) |
| `config get/set` | Get / set autonomous engine config (max_recursion_depth, auto_approve_children, per_task_guardrails, per_story_guardrails) |
| `learnings` | List engine learnings (BL202) |
| `progress [--json] [--automation <id>] [--tag <id>] [--since <date>] [--until <date>]` | Automata progress: counts by kind, work left (known and estimated, shown separately), the completion range with its methods, lead time and inactive windows. `--json` prints the `GET /api/autonomous/progress` body untouched; chat: `progress [automation=<id>] [tag=<id>] [since=<date>]` |
| `progress-trend [--days <n>] [--json]` | How the completion range moved over the saved daily snapshots (1 to 365 days, default 30); `GET /api/autonomous/progress/trend`; chat: `progress trend [days]` |
| `lane-fill [--limit <n>] [--json]` | Lane fill: each GPU and Claude lane with what it is running or why it is idle, the queued eligible counts and the newest decisions (`--limit` 1 to 1000, default 50). `--json` prints the `GET /api/autonomous/lane-fill` body untouched; a bad `--limit` is refused before anything is sent |
| `triage [--automation <id>] [--prd <id>] [--status failed\|blocked] [--json]` | Triage records: one row per failed or blocked Automaton — identity, the failed task, first-seen and alarm times, the retry budgets shown as given, the notes, and the alarm threshold in `alert_minutes`. `--status` is `failed` or `blocked` (case-sensitive, refused locally for anything else); an empty list prints its own line. `--json` prints the `GET /api/autonomous/triage` body untouched; chat: `autonomous triage [automation-id]` (one optional positional argument, the automation id) |
| `decisions [automaton-id] [--prd <id>] [--state open\|answered\|withdrawn\|expired] [--limit <n>] [--json]` | Operator decisions, newest first: one line each with the id, the state, the question (cut to 120 characters) and, for an answered decision, the answer id, the actor and the quote verbatim; a task held on a decision is named on its line with its wait reason. The Automaton id is the argument or `--prd` (give it once). `--limit` is a positive number, default 100, a larger value is capped at 1000. `--json` prints the `GET /api/autonomous/decisions` body untouched; chat: `autonomous decisions [automaton-id]`. A read: nothing is written |
| `subscribe --on <kind> [--level prd|task] [--prd <id>] [--task <id>] --sink comm\|session\|topic\|alert --target <t> [--send <text>] [--one-shot] [--coalesce] [--expires <secs>]` | Subscribe to Automaton lifecycle events and route them to a sink (BL427 phase 3); `--on` is repeatable; shorthand words map to task kinds, full kinds pass through |
| `unsubscribe <id>` | Delete a lifecycle-event subscription by id (BL427 phase 3) |
| `subscriptions [--owner <owner>]` | List lifecycle-event subscriptions, optionally scoped to one owner (BL427 phase 3) |
| `prd list/get/create/decompose/edit/edit-task/delete/instantiate/run/cancel` | PRD CRUD (cancel + edit + delete added in v5.19.0) |
| `prd approve/reject/request-revision` | BL191 Q1 review/approve gate (v5.2.0) |
| `prd-resume <id> [--note ...]` (alias `resume`) | Resume a paused PRD or one the boot-resume gate parked; chat: `autonomous resume <id> [note]`; REST `POST /api/autonomous/prds/{id}/resume` |
| `prd children` | List child PRDs spawned via SpawnPRD (v5.9.0, BL191 Q4) |
| `prd set-llm/set-task-llm` | Pin LLM backend/effort/model at PRD or task scope (v5.4.0, BL203) |
| `prd failure-escalation show\|set\|clear <id> [--task <task>]` | Show, set or clear the failure escalation override on an Automaton or one task. Chat: `autonomous set-failure-escalation <id> [task] clear \| enabled=true\|false max_failures=N steps=backend:model[@attempts],...`. REST `POST /api/autonomous/prds/{id}/set_failure_escalation` |
| `prd resolve-review <id> --task <task> --action retry\|cancel` | Retry (optionally with a new spec, worker, file scope or note) or cancel a task held for operator review. Works while the Automaton is running other tasks; there is no wait and no second executor. Chat: `autonomous review-task <id> <task> retry\|cancel [backend=X] [model=Y] [effort=Z] [files=a,b] [spec=...] [note=...]`. REST `POST /api/autonomous/prds/{id}/resolve_task_review` |
| `datawatch config set autonomous.failure_escalation.max_failures 4` | Daemon-wide failure escalation default (also `.enabled` and `.steps`). Chat: `configure autonomous.failure_escalation.max_failures=4` |
| `prd-set-task-verification-llm <id> --task <task>` / `prd-set-story-verification-llm <id> --story <story>` / `prd-set-verification-llm <id>` | Pick the LLM that verifies one task / every task in a story / the whole Automaton. Flags `--backend` / `--model` / `--effort`; omitted flags are unchanged, `--backend ""` clears. Precedence task > story > Automaton > global `autonomous.verification_*` (v9.0.70) |
| `prd-set-notify-events <prd-id> <kind,kind,...|inherit>` | Replace the Automaton's notify set — the list of lifecycle event kinds that produce notifications for it (Unreleased). Kinds are comma-separated; `inherit` (or an omitted/empty list) clears it back to the guardrail-profile tier, then the global `autonomous.notify.default_events`. Validates against the known kinds (`prd.updated` is not a valid kind). `POST /api/autonomous/prds/{id}/set_notify_events`; audit action `autonomous_notify_events_changed` |
| `tag-list [--include-sub] [--counts]` (alias `tags`) | List Automata tags as a tree (`parent_id`) with the number of Automata carrying each; `--counts` adds subtree and direct counts for Automata and sessions plus the untagged totals (the subtree count is the figure a tag browser shows); `GET /api/autonomous/tags[?counts=true]` |
| `tag-create <name> [--parent <id>] [--color <hex>] [--description <text>]` | Create a tag, optionally nested under a parent; `POST /api/autonomous/tags` |
| `tag-rename <id> <new-name>`, `tag-color <id> <color>`, `tag-move <id> --parent <new-parent-id\|root>` | Edit a tag; `PATCH /api/autonomous/tags/{id}`. A move that would make a tag its own ancestor is refused |
| `tag-delete <id>` | Delete a tag; refused while it has child tags, and refused with a count while Automata still carry it |
| `tag-registry` | Show this daemon's shared tag registry status: role (standalone, keeper or peer), keeper, stale, last sync, revision, pending sync count, notes and last sync error; the `registry` field of `GET /api/autonomous/tags` |
| `tag-registry-dismiss <note-id>` | Dismiss one merge or removed-on-keeper note on this daemon; `POST /api/autonomous/tags/registry/notes/{id}/dismiss`. An unknown note is refused with 404 |
| `prd-set-tags <prd-id> [--tags <id1,id2,...>] [--primary <tag-id>]` | Replace the tags on an Automaton (an empty `--tags` value clears them) and/or set its primary tag, used for sorting (an empty `--primary` clears it). Give `--primary` alone to keep the tags; `POST /api/autonomous/prds/{id}/set_tags` |
| `session tags <session-id> --tags <id1,id2,...>` | Replace the tags a session carries itself (an empty `--tags` value clears them). Inherited tags come from the Automaton and are not set here; `POST /api/sessions/tags` |
| `attention` | List the Automata that need attention: the attention statuses (`blocked`, `needs_review`, `paused` by default) plus any with a task held for review; `GET /api/autonomous/attention` |
| `prd-set-kind <id> <feature\|bug\|other>` | Set the Automaton kind the progress figures count it under; an unknown kind is refused before anything is sent; `POST /api/autonomous/prds/{id}/set_kind` (body `{"kind": ...}`, an empty kind derives it again) |
| `prd-create <spec> --kind <feature\|bug\|other>` | Give the kind at creation; omit it to derive the kind from the title and spec |
| `prd-set-lane-preference <id> <gpu\|claude\|any>` | Set which lane an Automaton may take; an unknown value is refused before anything is sent; `POST /api/autonomous/prds/{id}/set_lane_preference` (body `{"lane_preference": ...}`) |
| `prd-create <spec> --lane-preference <gpu\|claude\|any>` | Give the lane preference at creation; omit it to take any lane |

Comm channels: `autonomous tags [counts]`, `attention`, `session-tags <session-id> <tag-id1,tag-id2,...>` (omit the list to clear), `set-primary <prd-id> <tag-id|none>`, `tag-create <name> [parent=<id>] [color=<hex>] [description=<text>]`,
`tag-rename <id> <new-name>`, `tag-move <id> <parent-id|root>`, `tag-color <id> <color>`,
`tag-delete <id>`, and `set-tags <prd-id> <tag-id1,tag-id2,...>` (omit the list to clear).
`tag-registry` shows the shared tag registry status and `tag-registry-dismiss <note-id>` dismisses one of its notes on this daemon.
The registry itself, and the feature-access table for its endpoints, are in [api/autonomous.md](api/autonomous.md#shared-tag-registry).
`set-notify-events <prd-id> [kind,kind,...|]` likewise sets the Automaton's notify set (omit the list to clear back to inherit); `status` includes the global notify tier and every Automaton's override.

The guardrail-profile notify tier is the same story one level up: `datawatch guardrail profile create --name <name> [--guardrails <list>] --notify-events kind,kind,...` / `datawatch guardrail profile update <id> --notify-events kind,kind,...` (`inherit` or an omitted value clears the tier back to the global default; on `update`, omitting leaves it unchanged), and comm `guardrail profile notify-events <id> <kind,kind,...|inherit>` (or an omitted list). See the how-to [howto/guardrail-library.md](howto/guardrail-library.md), section *Notify events on a profile*.

Comm channels for the progress figures: `progress [automation=<id>] [tag=<id>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]`
and `progress trend [days]` (see their own section above), and
`autonomous set-kind <prd-id> <feature|bug|other>` to set an Automaton kind.

Comm channels for lane fill: `autonomous lane-fill [limit]` replies with one line per lane, the queued
eligible counts, then the newest five decisions (`limit` is how many decisions are read); and
`autonomous set-lane-preference <prd-id> <gpu|claude|any>` sets the lane an Automaton may take.

Comm channel for triage records: `autonomous triage [automation-id]` (one optional positional argument, the automation id) replies with one line per record (newest first) and the alert threshold; extra arguments are refused, the daemon's refusal (no manager) is passed through. The records are read: the daemon neither starts nor retries from one of them.

Comm channel for operator decisions: `autonomous decisions [automaton-id]` (one optional positional argument, the Automaton id) replies with the newest ten decisions, one line each, in the same text as the CLI; extra arguments get a usage reply. The decisions are read: nothing is answered or changed from this verb.

Lane fill settings are configuration keys, not commands: `datawatch config set autonomous.lane_fill.mode shadow` (or `on`, or `off`), and the other `autonomous.lane_fill.*` keys. In chat, `configure autonomous.lane_fill.mode=on`. The keys and the surfaces are listed in the Lane fill section of `docs/operations.md`.

### `datawatch automation <subcommand>`

Automations: chains of Automata run one after another or in branches. Every
control takes `--actor`, `--reason` and `--expected-version`; a stale expected
version is refused with HTTP 409.

| Subcommand | Description |
|---|---|
| `import-graph <graph-id>` | Turn an orchestrator graph into a draft Automation (`--dry-run` previews it, nothing stored). The comm verb is `automation import-graph <graph-id> [dry-run]` |
| `list`, `get <id>` | List Automations; show one (members, edges, active cursors, budget) |
| `create`, `activate <id>` | Create a draft from JSON; start a draft |
| `pause`, `resume`, `cancel <id>` | Operator control (`cancel` requires `--cancel-in-flight true\|false`) |
| `skip`, `insert`, `remove`, `edges`, `approve` | Edit a running chain |
| `set-budget <id>` | Set, replace or clear the spending limit of the Automation, or of one member with `--member` |

`set-budget` flags: `--ceiling-type dollar|allocation_pct`, `--ceiling-value <n>`
and `--window automation|day|week|month` (required together); optional
`--pacing none|even`, `--on-breach pause|block`, `--backend <llm>`,
`--allocation-estimate`, `--peer-window a,b`, `--reset-time HH:MM`,
`--reset-weekday <day>`, `--reset-day 1..28`, `--reset-zone <zone>`;
`--member <id>` with `--estimated-cost-usd <n>` declares a member's expected
cost (0 removes it); `--clear` removes the limit and cannot be combined with
the policy flags. The flags are checked against each other before any request
is sent. On a breach the Automation pauses and resumes by itself at the window
reset when the reset time is known, and is blocked until you resume it when it
is unknown; an alert is raised either way. `--allocation-estimate` adds an
estimated figure that never pauses or blocks, and `--peer-window` counts only
the peers you name, with no automatic confirmation that they share the account.

```bash
datawatch automation set-budget ab12cd34 --ceiling-type dollar --ceiling-value 40 \
  --window week --pacing even --reset-weekday monday --reset-time 09:00
datawatch automation set-budget ab12cd34 --clear
```

### `datawatch observer <subcommand>`

Federated observer ops (BL180/BL171/BL172).

| Subcommand | Description |
|---|---|
| `stats` | Local observer stats (CPU, memory, top processes, ollama runtime tap) |
| `envelope <id>` | Single envelope by ID |
| `envelopes` | All envelopes (local) |
| `envelopes-all-peers` | Cross-host federation envelope view (v5.12.0, BL180) |
| `agent list/stats` | Agent worker observer roll-ups |
| `peer list/get/register/delete/stats` | Peer registry (BL172) |
| `config get/set` | Observer config (peer_threshold_ms, conn_correlator, peers.*, ebpf_enabled) |

### `datawatch orchestrator <subcommand>`

PRD-DAG orchestrator (BL117).

| Subcommand | Description |
|---|---|
| `graph create/get/list/run/cancel/plan` | DAG CRUD + execution |
| `verdicts <graph-id>` | Aggregate guardrail verdicts |
| `config get/set` | Orchestrator config |

**Deprecated:** `orchestrator create`, `orchestrator plan` and `orchestrator run` (CLI and comm channel) still work but print a deprecation notice; use `datawatch automation import-graph <graph-id>` instead.

### `datawatch agent <subcommand>`

Agent worker lifecycle (F10).

| Subcommand | Description |
|---|---|
| `spawn` | Spawn a worker (`--project-profile`, `--cluster-profile`, optional `--task`) |
| `list` | List every tracked agent + state |
| `show <id>` | Fetch one agent by ID |
| `logs <id>` | Tail container logs |
| `kill <id>` | Terminate an agent |

### `datawatch profile <subcommand>`

Project + cluster profile CRUD (F10 sprint 2).

| Subcommand | Description |
|---|---|
| `project list/create/update/delete/smoke` | Project profiles |
| `cluster list/create/update/delete/smoke` | Cluster profiles |

### `datawatch projects <subcommand>`

| Subcommand | Description |
|---|---|
| `summary <dir>` | Project-summary one-shot (git status, recent sessions, stats, BL35) |
| `upsert` | Add/update a project record from operator input (BL27) |

### `datawatch cooldown <subcommand>`

| Subcommand | Description |
|---|---|
| `status` | Current cooldown state (per-backend) |
| `set` | Force a cooldown |
| `clear` | Clear all cooldowns |

### `datawatch device-alias <subcommand>`

Mobile push-token registry (Issue #1).

| Subcommand | Description |
|---|---|
| `list` | List registered device aliases |
| `upsert` | Register / update an alias |

### `datawatch routing-rules <subcommand>`

| Subcommand | Description |
|---|---|
| `list` | List configured routing rules |
| `test <text>` | Dry-run a rule match |

### `datawatch template <subcommand>`

PRD template library (BL191 Q2).

| Subcommand | Description |
|---|---|
| `list` | List template PRDs |
| `upsert` | Create / update a template |

### `datawatch plugins <subcommand>` + `datawatch plugin <subcommand>`

Plugin framework (BL33, S7 v3.11.0).

| Subcommand | Description |
|---|---|
| `plugins list` | List discovered + enabled plugins |
| `plugins reload` | Reload `~/.datawatch/plugins/` |
| `plugin get <name>` | Plugin metadata + status |
| `plugin enable/disable <name>` | Enable / disable a plugin |
| `plugin test <name>` | Dry-run a plugin's hook against a fixture |

### `datawatch cost <subcommand>`

Per-session token accounting (BL6).

| Subcommand | Description |
|---|---|
| `summary` | Per-project / per-PRD cost roll-ups |
| `usage` | Raw token usage rows |
| `rates` | Operator-overridable price tables |

### `datawatch audit query [filters]`

Query the operator audit log (BL9, append-only JSONL or CEF).

### `datawatch diagnose`

Run the daemon diagnostic battery (BL37): tmux availability, config
parse, port reachability, embedded-doc integrity, plugin discovery,
LLM-backend health, observer kprobe load.

### `datawatch alerts [n]`

List the last `n` alerts (default 20). Same source as the PWA Alerts
tab.

### `datawatch question <subcommand>`

The operator-question channel from the CLI (v9.33+): read and answer a
question an Automaton has raised. All of these speak the daemon's
`/api/questions` routes, so an answer here is the same record as one
over MCP, comm, or the PWA; while the `autonomous.questions.enabled`
switch is off the daemon 503s and names the switch.

| Subcommand | Description |
|---|---|
| `list [--state <state>] [--context <ctx>] [--asker <who>] [--text <search>] [--limit n] [--json]` | List the questions in a state (default `open`); print the answer log with `show` |
| `show <id>` | One question, its current answer (option id, verbatim quote, actor, surface) and the full answer log |
| `answer <id> (--option <option id> and/or --text <verbatim words>) [--supersedes <answer id>]` | Record an answer (option id and/or the operator's own words, verbatim, never trimmed); `--supersedes` names the standing answer a changed mind replaces |
| `withdraw <id> --reason <text>` | Withdraw an open question; the reason is recorded verbatim |
| `raise --text <text> --option id=label [..] [--default <option id or hold>]` | Raise a question to the Automaton's operator: name what it is about with the context flags (`--prd-id`, `--task-id`, `--proposal-path`, `--registry-row-key`), add a recommendation with `--recommend`/`--rationale`, and opt this question into the session line with `--return-path --session <id>` |

See [howto/operator-questions-answering.md](howto/operator-questions-answering.md)
for the full walkthrough and
[howto/operator-questions-notifications.md](howto/operator-questions-notifications.md)
for the notify/alert/return-path switches.

### `datawatch health [--url=<url>] [--endpoint=<path>] [--timeout=<sec>]`

One-shot health probe — used by container `HEALTHCHECK`. Defaults:
`http://127.0.0.1:8080/healthz`, 5 s timeout. Exits 0 / non-zero.

### `datawatch link`

Link a Signal account interactively. Delegates to `signal-cli link`
under the hood; prints a QR code in the terminal that the operator
scans on the primary device.

### `datawatch logs`

Tail `~/.datawatch/daemon.log` with deduplication and grouping.

### `datawatch rollback <session-id>`

Checkout the per-session pre-run git tag (BL12). Useful after a
failed autonomous run to undo all session edits in one step.

### `datawatch stale`

List sessions whose tmux pane has gone away but whose state hasn't
been reaped yet. Used by `datawatch session reconcile`.

### `datawatch export <kind>`

| Kind | Description |
|---|---|
| `memory` | Dump episodic memory + KG to JSONL (round-trips with `datawatch memory import`) |
| `sessions` | Dump session records |

### `datawatch config <subcommand>`

| Subcommand | Description |
|---|---|
| `init` | Generate a fresh `~/.datawatch/config.yaml` from defaults |
| `generate` | Print a fully-populated annotated config to stdout |
| `show` | Print the current parsed config |
| `get <key>` | Get a single config key (dot-path) |
| `set <key> <value>` | Set a single config key (dot-path); auto-reloads when the key is hot-reloadable |
| `edit` | Open `$EDITOR` on the config file |

### `datawatch test [--pr]`

Operator-canonical test runner — runs `go test ./...` with the same
flags CI uses, optionally formats output as a PR-comment markdown
table. Used by the `datawatch session start --before/--after-cmd`
gate examples.

### `datawatch session schedule add/list/cancel`

Schedule a saved-command send for a session at a future time (BL28).
See `datawatch cmd add` for saved-command authoring.

### `datawatch completion <shell>`

Generate shell completion scripts. Supported shells: `bash`, `zsh`, `fish`, `powershell`.

**Setup examples:**
```bash
# bash
source <(datawatch completion bash)

# zsh
source <(datawatch completion zsh)

# fish
datawatch completion fish | source

# powershell
datawatch completion powershell | Out-String | Invoke-Expression
```

To persist, add the `source` line to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.).

---

## MCP Tools

The MCP server exposes the full feature set as tools for AI clients (Cursor, Claude Desktop, VS Code, etc.).

| Tool | Description |
|---|---|
| `list_sessions` | List all sessions on this host |
| `start_session` | Start a new AI session (`task`, optional `project_dir`) |
| `session_output` | Last N lines of output (`session_id`, optional `lines`) |
| `session_timeline` | Structured event timeline (`session_id`) |
| `send_input` | Send text input to a waiting session |
| `kill_session` | Terminate a session |
| `rename_session` | Set a human-readable name (`session_id`, `name`) |
| `stop_all_sessions` | Kill all running/waiting sessions |
| `get_alerts` | List alerts, optionally filtered (`limit`, `session_id`) |
| `mark_alert_read` | Mark alert(s) as read (`id` or omit for all) |
| `restart_daemon` | Restart the daemon in-place |
| `get_version` | Current version + latest available check |
| `list_saved_commands` | List the saved command library |
| `send_saved_command` | Send a named saved command to a session |
| `schedule_add` | Schedule a command to an existing session (`session_id`/`session_name`, `command`, optional `run_at`, `cron_expr`, `schedule_name`) |
| `schedule_list` | List all pending scheduled commands (includes run history for spawn entries) |
| `schedule_cancel` | Cancel a scheduled command by ID or name |
| `schedule_spawn` | Schedule a fresh ephemeral session spawn (`task` or `shell`, `dir`, `backend`, `cron`, `schedule_name`) |
| `memory_remember` | Store a memory with vector embedding (`text`, optional `project_dir`) |
| `memory_recall` | Semantic search across memories (`query`) |
| `memory_list` | List recent memories (optional `project_dir`, `n`) |
| `memory_forget` | Delete a memory by ID (`id`) |
| `memory_stats` | Memory system statistics (counts, DB size, encryption status) |
| `memory_reindex` | Re-embed all memories after model change |
| `copy_response` | Get last LLM response (optional `session_id`) |
| `get_prompt` | Get last user prompt (optional `session_id`) |
| `kg_query` | Query entity relationships (`entity`, optional `as_of`) |
| `kg_add` | Add relationship triple (`subject`, `predicate`, `object`, optional `valid_from`) |
| `kg_invalidate` | End relationship validity (`subject`, `predicate`, `object`) |
| `kg_timeline` | Chronological entity history (`entity`) |
| `kg_stats` | Knowledge graph statistics |
| `ollama_stats` | Remote Ollama server stats (models, VRAM, running) |

MCP can be used via stdio (local IDE) or HTTP/SSE (remote). Configure with `datawatch setup mcp`.

---

## `datawatch session timeline <id>`

Show the structured event timeline for a session (state changes, inputs with source attribution, rate limits, etc.).

```bash
datawatch session timeline a3f2
```

Falls back to reading `timeline.md` directly if the daemon is not running.

---

## Multi-Machine Behavior

When multiple machines share a Signal group, each machine processes commands independently:

- `list` causes every machine to reply with its own sessions
- `status a3f2` is handled by the machine that has a session with ID `a3f2` — other machines ignore it
- `new: <task>` causes every machine to start a session (to target a specific machine, consider prefixing tasks with the hostname or using machine-specific groups)

See [multi-session.md](multi-session.md) for multi-machine coordination patterns.
