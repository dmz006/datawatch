# Implementation Guide — datawatch

This document is intended for developers who want to understand, modify, or extend `datawatch`.

---

## 1. Project Layout

```
datawatch/
├── cmd/
│   └── datawatch/          # CLI entry point (cobra commands)
│       ├── main.go             # Root command, start, link, config, session subcommands
│       └── ...
├── internal/
│   ├── alerts/
│   │   └── store.go            # Alert, Store — persistent system alert log
│   ├── config/
│   │   ├── config.go           # Config struct, Load(), Save(), LoadSecure(), SaveSecure(), applyDefaults()
│   │   └── encrypt.go          # AES-256-GCM encryption helpers (IsEncrypted, Encrypt, Decrypt, DeriveKey, LoadOrGenerateSalt)
│   ├── secfile/
│   │   └── secfile.go          # AES-256-GCM file encryption helpers (Encrypt/Decrypt/ReadFile/WriteFile)
│   ├── llm/
│   │   ├── backend.go          # llm.Backend interface definition
│   │   ├── registry.go         # Register() and Get() for named backends
│   │   └── claudecode/
│   │       └── backend.go      # claude-code implementation of llm.Backend
│   ├── messaging/
│   │   └── backend.go          # messaging.Backend interface + registry
│   ├── router/
│   │   └── commands.go         # Parse(), HelpText(), Command types (including CmdSetup)
│   ├── server/
│   │   ├── server.go           # HTTP server setup, mux routing, auth middleware
│   │   ├── api.go              # REST API handlers (/api/sessions, /api/config, etc.)
│   │   └── ws.go               # WebSocket hub, client pumps, message types
│   ├── wizard/
│   │   ├── wizard.go           # WizardManager, WizardSession, Step, Def — stateful multi-turn wizard engine
│   │   └── defs.go             # Wizard definitions for all 12 services (signal/telegram/.../web/server)
│   ├── session/
│   │   ├── store.go            # Session struct, Store (JSON persistence), state constants
│   │   ├── schedule.go         # ScheduledCommand, ScheduleStore — persistent command scheduler
│   │   ├── cmdlib.go           # SavedCommand, CmdLibrary — named reusable command library
│   │   └── filter.go           # FilterPattern, FilterStore, FilterEngine, ActionHandlers
│   └── signal/
│       ├── backend.go          # SignalBackend interface, Group, IncomingMessage types
│       ├── signalcli.go        # SignalCLIBackend — signal-cli subprocess management
│       └── types.go            # JSON-RPC types: Request, Response, Envelope, DataMessage
├── docs/
│   ├── api/                    # OpenAPI specification
│   ├── architecture.md         # Component overview and diagrams
│   ├── app-flow.md             # Application flow with Mermaid diagrams
│   ├── commands.md             # Command reference
│   ├── data-flow.md            # Data flow sequence diagrams
│   ├── design.md               # Design rationale and decisions
│   ├── future-native-signal.md # Roadmap for native Go Signal backend
│   ├── implementation.md       # This document
│   ├── multi-session.md        # Multi-machine setup guide
│   ├── operations.md           # Operations and troubleshooting
│   ├── planning.md             # Development phases and milestones
│   ├── pwa-setup.md            # PWA and Tailscale setup
│   └── setup.md                # Installation and quickstart
├── install/
│   ├── install.sh              # System-wide install script (requires root)
│   └── install-user.sh         # User service install script (no root)
├── web/                        # PWA static files (embedded into binary)
│   ├── index.html
│   ├── app.js
│   ├── style.css
│   └── manifest.json
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

---

## 2. Core Concepts

### Session Lifecycle and States

A session moves through these states in its lifetime:

```
[created] → running → waiting_input ⇄ running
                    ↓
              complete | failed | killed
```

| State | Meaning |
|---|---|
| `running` | tmux session alive, claude-code executing, output being produced |
| `waiting_input` | Output has been idle for `input_idle_timeout` seconds and the last line matches a prompt pattern |
| `complete` | claude-code exited cleanly (tmux session gone, no error) |
| `failed` | tmux session gone unexpectedly, or claude-code exited non-zero |
| `killed` | User sent `kill <id>` command |

State transitions trigger the `onStateChange` callback set on the session manager, which both the Signal router and the HTTP WebSocket hub subscribe to.

### The Session Store

`internal/session/store.go` implements a simple in-memory cache backed by a flat JSON file.

- All sessions are kept in `map[string]*Session` (key: `FullID = hostname-shortid`)
- A `sync.RWMutex` protects concurrent access from multiple goroutines (monitor goroutines, HTTP handlers, router callbacks)
- Every mutating operation (Save, Delete) calls `persist()`, which atomically rewrites the full JSON file
- On startup, `NewStore()` reads and unmarshals the existing file; an empty or missing file produces an empty store

### The tmux Integration

Every session is backed by a named tmux session:

- **Naming convention:** `cs-{hostname}-{shortid}` (e.g. `cs-myhost-a3f2`)
  - `cs-` prefix namespaces datawatch sessions from user sessions
  - hostname is included to avoid collisions when multiple users share a tmux server
- **Output capture:** `tmux pipe-pane -o -t <session> 'cat >> <logfile>'` redirects all pane output to a log file
  - The `-o` flag means "only pipe stdout, not stdin"
  - The log file path is `{data_dir}/logs/{hostname}-{shortid}.log`
- **Input injection:** `tmux send-keys -t <session> "<text>" Enter` sends keystrokes to the running process

### Output Monitoring

Each active session has a dedicated `monitorOutput` goroutine that uses **fsnotify**
(interrupt-driven file watching) instead of polling. The goroutine:

1. Opens the session's log file (waits if not yet created)
2. Seeks to the end of the file (skips history on resume)
3. Watches the log file with fsnotify; reads new lines on write events
4. Maintains a line buffer and resets an idle timer on each new line
5. When the idle timer fires (no new output for `input_idle_timeout` seconds):
   - Checks if the tmux session still exists
   - If gone: marks the session `complete` or `failed`
   - If alive: checks whether the last buffered line matches a prompt pattern (ends with `?`, `>`, `[y/N]`, `: `, etc.)
   - If pattern matches: marks `waiting_input`, calls `onNeedsInput`
6. When new output arrives after `waiting_input`: marks `running` again
7. The goroutine exits when the session reaches a terminal state or the context is cancelled

### Needs-input alert gating

`onNeedsInput` detections pass through `session.AlertGate` (`internal/session/alertgate.go`)
before any alert, push or comm message is sent. Config (`DetectionConfig` in
`internal/config/config.go`, read via `Config.GetDetection`):

| Field | Default | Meaning |
|---|---|---|
| `detection.alert_settle` | 45 | seconds a session must stay `waiting_input` before alerting; leaving the state cancels it |
| `detection.alert_repeat` | 300 | seconds an identical prompt for the same session is not re-alerted |

Both are settable via YAML, `PUT /api/config`, comm `configure detection.alert_settle=N`,
MCP `detection_config_set`, CLI `datawatch config set`, and the PWA Settings > Detection timing.
Android/iPhone reflect the behaviour through push (no client setting; see datawatch-app#190).

### Capacity admission

`internal/capacity.Ledger` holds named pools (`host`, `node:<n>`, `llm:<n>`) with
limits. `Manager.admit` (`internal/autonomous/capacity.go`) acquires an all-or-nothing
lease before each task spawn and releases it when `executeOne` returns; waiting shows
as `waiting_capacity` with `wait_reason`. `cmd/datawatch/capacity_wiring.go` builds the
ledger, syncs limits every 10 s from `session.max_sessions`/`reserved_interactive`,
compute node `max_concurrent_sessions` and LLM `max_inflight`, reaps leaked leases and
raises a 30-minute wait alert. Config: `autonomous.capacity_enabled`,
`autonomous.capacity_wait_timeout_seconds`, `autonomous.capacity_gpu_util_pct`,
`session.reserved_interactive`. See `docs/howto/automata-capacity.md`.

`autonomous.ci_watch_timeout_seconds` (default 1500) bounds how long the optional
`ci-watch` completion guardrail waits for the CI runs of the commit an Automaton
pushed. It only applies when `ci-watch` is listed in `autonomous.per_prd_guardrails`
(or an Automaton's own PRD-level guardrails); the list is empty by default.

Foreign-file drift is checked for direct-mode (non-worktree) Automata only. The
capture point is the worker spawn in `cmd/datawatch/main.go`: alongside the pre-task
`HEAD` SHA and untracked files it calls `autonomous.CapturePreTaskDirtyFiles`
(unstaged ∪ staged tracked files), skipped when the task runs in a PRD-owned worktree.
The values ride back on the spawn result into `Task.PreTaskSHA` /
`PreTaskUntrackedFiles` / `PreTaskDirtyFiles`. In `executeOne`, right after the
verifier returns, `checkForeignFileDrift` (`internal/autonomous/file_drift.go`)
unions the files of every commit in `<pre-task SHA>..HEAD` and flags those that were
already dirty/untracked and are not in the task's `files`. A finding is stored on
`Task.ForeignFileDrift`, appended as a `file_drift_detected` decision and emitted as
the `autonomous_file_drift_detected` audit event. It is warn-only unless
`autonomous.block_on_file_drift` (resolved Automaton > guardrail profile > global,
default `false`) is on, in which case the task is blocked and the walk halts. It only
runs read-only git commands and never rewrites history. See
`docs/howto/file-drift-guardrail.md`.

### Per-task project directory (Automata)

`autonomous.Task.ProjectDir` (JSON `project_dir`) overrides the
Automaton's `PRD.ProjectDir` for one task. `ResolveTaskProjectDir(prd,
task)` returns the task value when it is set and the PRD value when it
is empty, so the precedence is task → Automaton and an empty value
inherits. Every per-task check calls the resolver: diff capture, scope
(task-scoped write dirs), file drift, quality gates and the verifier.
`Manager.SetTaskProjectDir` validates the path (it must be absolute, must
not be a filesystem root or system directory, and must be inside the
Automaton's write dirs when those are set) and applies the same lock rule as
`SetTaskLLM`. An empty dir clears the override.

Surfaces: REST `POST /api/autonomous/prds/{id}/set_task_project_dir`
(`{task_id, project_dir}`), MCP `autonomous_prd_set_task_project_dir`,
CLI `datawatch autonomous prd-set-task-project-dir <prd> <task> [dir]`,
comm verb `autonomous set-task-project-dir`, and the PWA task edit
modal's **Project dir override** field (shown on the task card as a
`dir:` badge).

### Per-tree write lock (Automata)

The per-tree write lock (BL428 Phase 2,
docs/plans/2026-10-14-concurrency-and-budget-design.md §3a) makes writes to a
single project tree exclusive. It fixes the failure mode where two or more
Automata ran workers in the same working tree concurrently — interleaved
diffs and release-version drift — the shape of the 2026-10-13 incident
that three concurrent PRDs in one tree produced. Two cooperating layers
protect a tree:

**Layer 1 — PRD admission gate (BL469, `internal/autonomous/tree_admission.go`).**
`admitTree` runs under the manager lock before a PRD is marked `running`. It
refuses to start a PRD whose canonical tree is already held by a PRD in
`running` or `active` status, with a message naming the holder
(`tree busy: PRD %s is already running on this tree (%s); run one PRD per
direct tree or use worktree mode`). Ancestor (parent/child) PRDs never
conflict: a spawned Automaton running on its parent's tree is exempt, since
the parent is waiting on it, not competing with it. The walk is bound to 32
levels to defend against a corrupt parent chain. No project dir yet =
nothing to contend on, so it's a no-op.

**Layer 2 — task writer lock (BL428 §3a, `internal/autonomous/tree_lock.go`).**
This is the finer-grained mechanism. `executeOne` (executor.go) acquires the
task's canonical-tree lock before spawning the worker and releases it after
verification finishes. The lock is held across the full critical section
(spawn → run → verify → retry loop), covering every exit path including
cancellation and daemon shutdown. The `holdTree` closure in `executeOne`
is idempotent per task: re-calling `acquireTreeWritersLock` with the same
holder is a no-op (the record already names this task).

Key resolution — `ResolveTreeKey`:

- **Git repo (no explicit branch)**: canonical base = `git rev-parse
  --git-common-dir` resolved to an absolute path. All worktrees of the same
  repo map to the same key, so a linked worktree serializes against the
  main checkout.
- **Worktree-mode Automaton** (`prd.Git.Branch != ""`): key = the worktree's
  own path (no canonicalization). Each worktree is its own lock domain, so
  worktree-mode PRDs run in parallel with each other.
- **Non-git path**: key = the project directory itself (path-based key, no
  git).

Lock file: `<canonical-tree>/.datawatch/tree.lock` (a `tree.lock` file
inside a `.datawatch/` directory at the tree root). datawatch's own
`.gitignore` covers `.datawatch/`, and the regression test
(`TestTreeLockFileDoesNotDirtyGitStatus`) pins the invariant that holding
the lock must never show up in `git status --porcelain` (a project that
adopts the lock gitignores `.datawatch/` for the same reason). The lock is
advisory by design: only datawatch's own task writers consult it; a human
editor or another tool bypassing it is outside the lock's scope.

Lock record (`LockRecord`): `holder_task_id`, `holder_prd_id`,
`acquired_at` (unix seconds), `pid`. This lets the PWA, CLI and operator
tell who holds the lock and since when.

Stale lock reclamation:
- A lock whose holder task no longer exists in the store → stale, reclaimed
  (up to `maxStaleReclaimAttempts = 3` consecutive reclaim attempts).
- A lock whose holder task is in a terminal status → stale, reclaimed.
- A lock whose record is missing/unreadable but the file is younger than
  `staleLockGrace = 30s` → not reclaimed (likely a live acquirer two steps
  into writing the record); after the grace window, reclaimed.
- A live holder's record → never reclaimed (`TestTreeLock_LiveHolderWaits_NotReclaimed`).

Wait behavior: if the lock is held by another live task, `AcquireTreeLock`
polls every `defaultTreeLockPoll = 5s` (production passes the
Manager-controlled interval) until the lock is released or the
context timeout (`Manager.capacityWait()`, default
`capacity_wait_timeout_seconds = 14400s` = 4 h) fires. While waiting, the
task's wait reason is visible in the PWA as:
`tree lock: <canonical-tree> held by task <id> (PRD <id>) since <time>`.
On timeout the acquire returns an error and the task is refused — the PRD
returns to an approvable state (this is the known PRD-level auto-wait gap;
the per-task wait+auto-resume in the lock itself works as documented
above and is exercised by `TestExecutorTreeLock_WaitReason`).

The `resolveRefuseDirtyTree` companion gate:
`autonomous.refuse_dirty_tree_start` (default `false`, per-PRD override via
`PRD.RefuseDirtyTreeStart *bool`) is checked at admission, before the tree
lock is acquired. When enabled, `dirtyTreeRefused` calls
`git status --porcelain` on the canonical tree; non-empty output → the task
is refused with a message naming the dirty-file count and the options
(commit or stash first, `git clean`, or the config key back to `false`).
Git not available or not a repo → the gate passes the tree through silently
(the gate only reasons about dirt it can observe).

Config: `autonomous.project_write_serialization` — `advisory` (default) or
`parallel` (recognized, reserved; daemon still enforces the advisory lock
and logs a startup warning). Any other value is a config error and prevents
daemon start.

CI-watch own-commit pin (BL423, `internal/autonomous/ci_watch.go`):
the ci-watch completion guardrail now judges the last completed task's
recorded post-verify SHA (`Task.PostVerifySHA`, selected by
`prdLastPostVerifySHA` — iteration order over the PRD's stories/tasks, so
the last one in that order wins) rather than the tree's live `HEAD`.
This prevents a later write on the shared tree from dragging
someone else's CI run into a PRD's verdict. If no
task in the PRD recorded a `PostVerifySHA` (legacy store), ci-watch falls
back to live `HEAD` (the pre-fix behavior). If the pinned SHA is not
reachable from the remote (`git merge-base --is-ancestor <sha> <remote-tracking-ref/>` fails), ci-watch skips with a logged reason —
this is the expected behavior for worktree/auto-PR Automata that push
after completion.

Tests: `tree_lock_test.go` (20 tests covering key resolution, lock path,
exclusivity, stale reclamation, release guards, and git-status purity),
`executor_tree_lock_test.go` (9 tests: same-tree serialization, wait reason,
release-on-spawn-failure, release-after-verify, dirty-tree refused,
clean-tree admitted, gate-off, missing-tree, worktree parallel),
`same_tree_serialization_test.go` (2 regression tests for the
2026-10-13 shape: three-PRD serialization and release-bump no-clobber),
and the live smoke `tests/live-smoke/tree-serialization-smoke.sh` (2026-10-18,
v9.5.4; same-tree serialized PASS, cross-tree parallel PASS, loser
auto-resume GAP documented in `tests/live-smoke/REPORT.md`).

### Message Routing

The router (`internal/router`) is stateless. It:

1. Receives an `IncomingMessage` (or `messaging.Message`) from the Signal backend
2. Filters by group ID — messages from other groups are ignored
3. Filters out messages from the daemon's own account (echo suppression)
4. Calls `Parse(text)` to produce a `Command`
5. Dispatches to the appropriate session manager method
6. Formats a reply and calls `backend.Send(groupID, reply)`

The router never holds state. Session state lives exclusively in the session store.

### DNS Channel Backend

The DNS channel (`internal/messaging/backends/dns/`) implements the `messaging.Backend` interface
using DNS TXT queries for covert command/response communication.

**Package structure:**
- `protocol.go` — `EncodeQuery`/`DecodeQuery` (HMAC-SHA256), `EncodeResponse`/`DecodeResponse` (fragmented TXT)
- `nonce.go` — `NonceStore` (bounded LRU, 10K entries, 5-minute TTL)
- `server.go` — `ServerBackend` implementing `messaging.Backend` (miekg/dns UDP+TCP)
- `client.go` — `Client.Execute()` for CLI DNS queries

**Config struct:** `DNSChannelConfig` in `internal/config/config.go`
- Fields: `enabled`, `mode`, `domain`, `listen`, `upstream`, `secret`, `ttl`, `max_response_size`, `poll_interval`

**Router exception:** DNS messages bypass the group ID filter since DNS has no concept of groups.
The router checks `msg.Backend == "dns"` and processes commands regardless of group ID.

---

## 3. Adding a New LLM Backend

To add support for a new AI coding assistant (e.g. `aider`):

### Step 1: Create the backend package

```
internal/llm/aider/backend.go
```

### Step 2: Implement `llm.Backend`

```go
package aider

import (
    "context"
    "fmt"
    "os/exec"

    "github.com/dmz006/datawatch/internal/llm"
)

// Backend implements llm.Backend for aider.
type Backend struct {
    bin string // path to aider binary
}

// New creates an aider backend.
func New(bin string) *Backend {
    if bin == "" {
        bin = "aider"
    }
    return &Backend{bin: bin}
}

// Name returns the backend name.
func (b *Backend) Name() string { return "aider" }

// SupportsInteractiveInput returns true — aider accepts yes/no on stdin.
func (b *Backend) SupportsInteractiveInput() bool { return true }

// Launch starts aider for the given task inside the pre-created tmux session.
// Output is captured to logFile via tmux pipe-pane (already set up by the session manager).
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, logFile string) error {
    // Build the aider command: run non-interactively with an initial message
    aiderCmd := fmt.Sprintf("%s --yes --message %q", b.bin, task)

    // Send the command to the existing tmux session
    cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, aiderCmd, "Enter")
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("send aider command to tmux: %w", err)
    }
    return nil
}

// Ensure Backend implements the interface at compile time.
var _ llm.Backend = (*Backend)(nil)
```

### Step 3: Register in `internal/llm/registry.go`

```go
import "github.com/dmz006/datawatch/internal/llm/aider"

func init() {
    Register(aider.New(""))
}
```

### Step 4: Add a config option

In `internal/config/config.go`, add to `SessionConfig`:

```go
// LLMBackend selects the LLM backend. One of: "claude-code", "aider".
// Defaults to "claude-code".
LLMBackend string `yaml:"llm_backend"`
```

### Step 5: Wire into the session manager

In the session manager's `Start()` method, look up the backend by name:

```go
backend, ok := llm.Get(cfg.Session.LLMBackend)
if !ok {
    backend, _ = llm.Get("claude-code") // fallback
}
if err := backend.Launch(ctx, task, tmuxSession, logFile); err != nil {
    return nil, fmt.Errorf("launch backend: %w", err)
}
```

Users then configure the backend in `config.yaml`:

```yaml
session:
  llm_backend: aider
```

---

## 4. Adding a New Messaging Backend

To add Slack as a control channel:

### Step 1: Create the backend package

```
internal/messaging/slack/backend.go
```

### Step 2: Implement `messaging.Backend`

```go
package slack

import (
    "context"
    "github.com/dmz006/datawatch/internal/messaging"
)

type Backend struct {
    botToken  string
    channelID string
    client    *slackAPIClient
}

func New(botToken, channelID string) *Backend {
    return &Backend{botToken: botToken, channelID: channelID}
}

func (b *Backend) Name() string { return "slack" }
func (b *Backend) SelfID() string { return b.client.BotUserID() }

func (b *Backend) Send(recipient, message string) error {
    return b.client.PostMessage(recipient, message)
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
    // Use Slack Events API Socket Mode for no-public-URL deployment
    return b.client.RunSocketMode(ctx, func(event SlackEvent) {
        if event.Type != "message" || event.BotID != "" {
            return // ignore bot messages
        }
        handler(messaging.Message{
            GroupID:    event.Channel,
            Sender:     event.User,
            SenderName: event.UserName,
            Text:       event.Text,
            Backend:    b.Name(),
        })
    })
}

// Link is a no-op for Slack — bots authenticate with a bot token, no QR linking needed.
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

func (b *Backend) Close() error { return b.client.Close() }

var _ messaging.Backend = (*Backend)(nil)
```

### Step 3: Add a config section

In `internal/config/config.go`:

```go
type SlackConfig struct {
    BotToken  string `yaml:"bot_token"`
    ChannelID string `yaml:"channel_id"`
}

type Config struct {
    // ...existing fields...
    Slack SlackConfig `yaml:"slack"`
}
```

### Step 4: Wire into main.go

```go
if cfg.Slack.BotToken != "" {
    slackBackend := slack.New(cfg.Slack.BotToken, cfg.Slack.ChannelID)
    messaging.Register(slackBackend)
    go slackBackend.Subscribe(ctx, router.HandleMessage)
}
```

### Step 5: Configure

```yaml
slack:
  bot_token: xoxb-...
  channel_id: C01234567
```

---

## 5. signal-cli JSON-RPC Protocol

`signal-cli` in `jsonRpc` mode reads newline-delimited JSON-RPC 2.0 requests from stdin and writes responses and notifications to stdout.

### Subscribe to incoming messages

Request:
```json
{"jsonrpc":"2.0","method":"subscribeReceive","id":1}
```

Response:
```json
{"jsonrpc":"2.0","result":{},"id":1}
```

After subscribing, incoming messages arrive as notifications (no `id` field):
```json
{
  "jsonrpc": "2.0",
  "method": "receive",
  "params": {
    "envelope": {
      "source": "+12125551234",
      "sourceName": "Alice",
      "dataMessage": {
        "message": "new: write tests",
        "groupInfo": {
          "groupId": "base64groupid=="
        }
      }
    }
  }
}
```

### Send a message to a group

Request:
```json
{
  "jsonrpc": "2.0",
  "method": "send",
  "params": {
    "groupId": "base64groupid==",
    "message": "[myhost][a3f2] Started session for: write tests"
  },
  "id": 2
}
```

Response:
```json
{"jsonrpc":"2.0","result":{"timestamp":1711234567890},"id":2}
```

### List joined groups

Request:
```json
{"jsonrpc":"2.0","method":"listGroups","id":3}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "result": [
    {"id": "base64groupid==", "name": "AI Control"},
    {"id": "anothergroup==", "name": "Family"}
  ],
  "id": 3
}
```

### Link a new device

Device linking is not a JSON-RPC call — it uses a separate `signal-cli link` subprocess invocation:

```bash
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

stdout/stderr: `sgnl://linkdevice?uuid=...&pub_key=...`

The Go code captures this URI, converts it to a QR code, and displays it (in terminal or via SSE to the PWA).

---

## 6. WebSocket Protocol

All messages use a common envelope:

```json
{
  "type": "<message_type>",
  "data": { ... },
  "ts": "2026-03-25T14:32:01.123Z"
}
```

### Server → Client Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `sessions` | On connect; on any session state change | `{"sessions": [<Session>, ...]}` |
| `session_state` | When one session's state changes | `{"session": <Session>}` |
| `output` | New output lines from a monitored session | `{"session_id": "a3f2", "lines": ["line1", "line2"]}` |
| `needs_input` | Session enters `waiting_input` | `{"session_id": "a3f2", "prompt": "Do you want to continue? [y/N]"}` |
| `notification` | General informational message | `{"message": "Session a3f2 killed."}` |
| `error` | Command or protocol error | `{"message": "Session not found: zz99"}` |
| `hello_ack` | Reply to `hello` | `{"session_list": "delta", "session_view": "summary", "output": "subscribed", "capabilities": ["ws.hello", ...]}` — the modes now in effect |
| `session_removed` | A session left the list (`session_list=delta` clients only) | `{"session_id": "myhost-a3f2"}` |
| `alert` | A new system alert | `<Alert>` — `{"id": "...", "level": "warn", "title": "...", "body": "...", "read": false, ...}` |
| `alerts_read` | Alerts were marked read on any client | `{"ids": ["..."], "unread_count": 2}` or `{"all": true, "unread_count": 0}` |

With `session_view=summary`, `session_state` carries the compact
`GET /api/sessions?view=summary` record instead of the full session.
With `output=subscribed`, `output`, `raw_output`, `pane_capture` and
`chat_message` arrive only for subscribed sessions. See "WebSocket
session updates" and "WebSocket output scoping" below.

**Example — sessions message on connect:**
```json
{
  "type": "sessions",
  "data": {
    "sessions": [
      {
        "id": "a3f2",
        "full_id": "myhost-a3f2",
        "task": "write tests for auth module",
        "tmux_session": "cs-myhost-a3f2",
        "state": "running",
        "created_at": "2026-03-25T14:00:00Z",
        "updated_at": "2026-03-25T14:01:30Z",
        "hostname": "myhost"
      }
    ]
  },
  "ts": "2026-03-25T14:32:01.000Z"
}
```

**Example — needs_input event:**
```json
{
  "type": "needs_input",
  "data": {
    "session_id": "a3f2",
    "prompt": "Do you want to overwrite auth_test.go? [y/N] "
  },
  "ts": "2026-03-25T14:35:22.000Z"
}
```

### Client → Server Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `command` | Raw command string (same syntax as Signal) | `{"text": "list"}` |
| `new_session` | Start a new session | `{"task": "build a REST API"}` |
| `send_input` | Send input to a waiting session | `{"session_id": "a3f2", "text": "y"}` |
| `subscribe` | Subscribe to output lines for a session (full or short ID) | `{"session_id": "a3f2"}` |
| `unsubscribe` | Stop output for a session and stop the screen capture `subscribe` started | `{"session_id": "a3f2"}` |
| `hello` | Negotiate lighter streams; every key optional | `{"session_list": "delta", "session_view": "summary", "output": "subscribed"}` |
| `ping` | Keepalive (server replies with pong) | `{}` |

**Example — subscribe to session output:**
```json
{
  "type": "subscribe",
  "data": {"session_id": "a3f2"},
  "ts": "2026-03-25T14:32:05.000Z"
}
```

After subscribing, the server sends `output` messages for that session as new lines appear.

---

## 7. Output Monitoring Algorithm

The `monitorOutput` goroutine implements the following algorithm:

```
1. Open log file at sess.LogFile
   - If file does not exist, wait with exponential backoff (up to 5s) then retry
2. Seek to EOF (skip historical output on resume after daemon restart)
3. Create an idle timer: time.NewTimer(cfg.Session.InputIdleTimeout * time.Second)
4. Enter read loop:
   a. Attempt to read the next line from the file
   b. If a new line is available:
      - Append to in-memory line buffer (capped at 100 lines)
      - Reset the idle timer
      - If current state is waiting_input: transition back to running
   c. If no new line (EOF):
      - Wait for the next fsnotify write event (interrupt-driven, no polling)
   d. If idle timer fires (no new output in InputIdleTimeout seconds):
      - Check if tmux session exists: `tmux has-session -t <name>`
      - If session is gone:
          * If last state was running/waiting_input: mark complete or failed
          * Call onStateChange(sess, newState)
          * Return (goroutine exits)
      - If session is alive and last line matches prompt pattern:
          * Mark waiting_input
          * Set sess.LastPrompt = last buffered line
          * Call onNeedsInput(sess, lastPrompt)
          * Call onStateChange(sess, waiting_input)
      - Reset idle timer and continue loop
5. On context cancellation: return (daemon shutting down)
```

**Prompt patterns** (heuristic — last line ends with one of):
- `? ` — generic question
- `[y/N] ` or `[Y/n] ` — yes/no prompt
- `> ` — interactive continuation prompt
- `: ` — input request
- `(Y/n)` — alternate format

These patterns are checked case-insensitively on the last non-empty line in the buffer.

---

## 8. Configuration Reference

All fields in `~/.datawatch/config.yaml`:

| Field | Type | Default | Description |
|---|---|---|---|
| `hostname` | string | OS hostname | Identifies this machine in Signal messages and session IDs |
| `data_dir` | string | `~/.datawatch` | Root directory for sessions.json, logs/, and config |
| `signal.account_number` | string | (required) | Signal phone number in E.164 format, e.g. `+12125551234` |
| `signal.group_id` | string | (required) | Signal group ID in base64 format (from `signal-cli listGroups`) |
| `signal.config_dir` | string | `~/.local/share/signal-cli` | signal-cli data directory containing account keys |
| `signal.device_name` | string | hostname | Name shown in Signal's Linked Devices list |
| `session.file_service_root` | string | `""` | Root directory for the federated file service and PWA image uploads. Per-peer files land in `<root>/peers/<name>/`; per-discussion files in `<root>/discussions/<id>/`. Empty = falls back to `session.root_path`, then the user home directory |
| `session.max_sessions` | int | `10` | Maximum number of concurrent claude-code sessions |
| `session.input_idle_timeout` | int | `10` | Seconds of idle output before declaring a session is waiting for input |
| `session.tail_lines` | int | `20` | Default number of lines returned by `tail` and `status` commands |
| `session.claude_code_bin` | string | `claude` | Path to the claude-code binary (can be absolute or a `PATH`-relative name) |
| `server.enabled` | bool | `true` | Whether to start the HTTP/WebSocket server |
| `server.host` | string | `0.0.0.0` | Bind address. Use `0.0.0.0` for all interfaces (including Tailscale) |
| `server.port` | int | `8080` | HTTP/WebSocket listen port |
| `server.token` | string | `""` | Optional bearer token for PWA authentication. Empty = no auth |
| `server.tls_cert` | string | `""` | Path to TLS certificate PEM file. Leave empty for plain HTTP |
| `server.tls_key` | string | `""` | Path to TLS key PEM file. Leave empty for plain HTTP |
| `server.tls_enabled` | bool | `false` | Enable TLS. Use with `tls_auto_generate=true` or explicit cert/key paths |
| `server.tls_auto_generate` | bool | `false` | Auto-generate a self-signed certificate at `~/.datawatch/tls/server/` on start |
| `server.session_summary_terminal_limit` | int | `50` | Maximum number of terminal (complete / failed / killed) sessions returned by `GET /api/sessions?view=summary`, newest by `updated_at` first. Non-terminal sessions are never dropped. `0` = no cap. Only applies to `view=summary`; see "Session list views" below |
| `server.session_summary_text_chars` | int | `200` | Characters (runes) kept of `task`, `prompt_context`, `last_response` and `last_summary_long` in `GET /api/sessions?view=summary`. Minimum `20` |
| `server.ws_session_list_default` | string | `full` | WebSocket session-list mode for clients that never send `hello`: `full` (a `sessions` frame with the whole list on every change) or `delta` (only `session_state` / `session_removed` frames; no connect-time list — fetch it with `GET /api/sessions?view=summary`). Read live on every broadcast. See "WebSocket session updates" below |
| `server.ws_output_scope_default` | string | `all` | WebSocket terminal-output mode for clients that do not send `output` in `hello`: `all` (every session's `output` / `raw_output` / `pane_capture` / `chat_message` frames) or `subscribed` (only sessions the client sent `subscribe` for). Read live for every frame. See "WebSocket output scoping" below |
| `server.api_gzip` | bool | `true` | Gzip-compress `/api/*` responses for clients that send `Accept-Encoding: gzip`. See "API response compression" below |
| `server.api_gzip_min_bytes` | int | `1024` | A `/api/*` response is compressed only once its body exceeds this many bytes; smaller bodies are sent unchanged. Minimum `0` |
| `servers[].name` | string | — | Short identifier for a remote datawatch server (used with `--server` flag) |
| `servers[].url` | string | — | Base URL of the remote server (e.g. `http://203.0.113.10:8080`) |
| `servers[].token` | string | — | Bearer token for the remote server |
| `servers[].enabled` | bool | `true` | Whether this remote server is active |
| `mcp.max_retries` | int | `3` | Number of automatic retries when a per-session MCP channel server fails to start or loses connection |
| `goose.enabled` | bool | `false` | Enable the goose/goose-prompt LLM backend |
| `goose.binary` | string | `goose` | Path to the goose binary (absolute or PATH-relative) |
| `goose.console_cols` | int | `0` | Terminal width for goose sessions (0 = session default) |
| `goose.console_rows` | int | `0` | Terminal height for goose sessions (0 = session default) |
| `goose.provider` | string | `""` | Provider injected as `GOOSE_PROVIDER` env var (e.g. `anthropic`, `openai`, `google`) |
| `goose.model` | string | `""` | Model injected as `GOOSE_MODEL` env var (e.g. `claude-sonnet-5`) |
| `goose.api_key_ref` | string | `""` | API key (literal or `${secret:name}`) injected as the provider-specific key env var; resolved at startup |
| `goose.channel_enabled` | bool | `false` | When true, inject `GOOSE_MCP__DATAWATCH__*` env vars so Goose connects back as an MCP client (stdio transport) |
| `autonomous.verifier_diff_max_bytes` | int | `0` | Byte cap on the git diff injected into the verifier prompt for evidence grounding. `0` = 8192. Raise for large changesets; lower to reduce token cost. |
| `autonomous.verification_timeout_seconds` | int | `1800` | Declared in `internal/config/config.go` (`VerificationTimeoutSeconds`). Bounds one verifier or per-task guardrail judgment session; consumed by the verifier and guardrail closures in `cmd/datawatch/main.go` through the `verificationSessionTimeout` resolver. `0` or negative = 1800-second (30m) fallback. Exposed in the PWA Settings autonomous card. |
| `autonomous.default_quality_gates.enabled` | bool | `false` | Master toggle for per-PRD test regression gates. When true, the executor captures a test baseline before the first task and re-runs after each task completes. |
| `autonomous.default_quality_gates.test_command` | string | `""` | Shell command used to run the test suite (e.g. `go test ./...`). Run relative to the PRD's `project_dir`. |
| `autonomous.default_quality_gates.timeout` | int | `0` | Per-run timeout in seconds for the test command. `0` = no cap. |
| `autonomous.default_quality_gates.block_on_regression` | bool | `false` | When true, a task that introduces a test regression is marked failed and triggers an auto-fix retry cycle. |
| `autonomous.max_engines_per_node` | int | `1` | Local-model sessions one compute node runs at once. Read in `cmd/datawatch/capacity_wiring.go` (`syncPools`) to set the `node:<name>` pool limit for nodes that host local engines; a node's own `max_concurrent_sessions` stays a ceiling when smaller. Values below `1` clamp to `1` (`internal/config/config.go`). Only local-engine sessions (`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`) are gated; hosted backends are not. Waiting sessions show `waiting for node <name>`. Operator guide: `docs/operations.md` → "Compute-node queue". |
| `autonomous.injection_guard` | bool | `false` | Scan user-supplied PRD/task specs for prompt injection phrases at the API boundary on create and edit. Findings are always logged. |
| `autonomous.block_on_injection` | bool | `false` | When true (requires `injection_guard: true`), requests containing injection phrases are rejected with HTTP 400 instead of warn-only. |
| `autonomous.notify.default_events` | []string | `[]` | Lifecycle event kinds the built-in sinks (alert-store write, topic push to `alerts` + `automaton-<id>`, comm fan-out) act on. Empty list (the default) = nothing new is sent. The global `notify_exclude` still applies to the comm fan-out |
| `autonomous.project_write_serialization` | string | `advisory` | BL428 §3a (docs/plans/2026-10-14-concurrency-and-budget-design.md): how Automaton task writes targeting the same project tree are serialized. `advisory` (default) = exclusive per-canonical-tree lock at admission — same-tree tasks queue, different trees run in parallel. `parallel` = RESERVED but DISABLED — recognized and accepted until the mechanism lands in the follow-up implementation PRD; the daemon still enforces the advisory lock and logs a startup warning. Any other value is a config error and the daemon refuses to start |
| `autonomous.refuse_dirty_tree_start` | bool | `false` | BL428 §3a ii admission-time companion: when true, task admission hard-fails if the PRD's project tree has uncommitted changes (`git status --porcelain` non-empty). The error names the dirty file count and the options: commit or stash first, `git clean`, or set this key back to `false` |
| `autonomous.stale_task_seconds` | int | `900` | Stall window for a running worker, in seconds. Activity-based: a worker counts as stalled only when it has no output, no live child process, no backend activity, and no file change in its work directory for this many seconds. `0` or negative inherits `session.stale_timeout_seconds`. |
| `autonomous.preserve_attempt_work` | bool | `true` | When a stalled or killed attempt is stopped, its uncommitted work is saved to `refs/datawatch/attempts/<task>/<n>` and restored on the next attempt, unless that attempt was started with `clean_restart` (`reset_task`). `false` disables the save and restore. |
| `web_search.enabled` | bool | `false` | Inject a `web_search` MCP tool into opencode and goose sessions via SearXNG. |
| `web_search.provider` | string | `searxng` | Search provider. Only `"searxng"` is supported. |
| `web_search.url` | string | `""` | Base URL of the SearXNG instance (required when enabled). |
| `web_search.engine` | string | `bing` | SearXNG engine to use. Only `bing` is reliable; others trigger CAPTCHA/rate-limiting. |
| `web_search.num_results` | int | `10` | Maximum results returned per query. |
| `audit.access_log_enabled` | bool | `true` | Log every HTTP request/response, auth failure, and WS connect/disconnect to `access.log` (method, path, status, remote IP, user agent, resolved principal — never the token). |
| `audit.retention_days` | int | `30` | Prune entries older than this from both `audit.log` and `access.log`. Negative = never prune. |
| `audit.cef_mirror_enabled` | bool | `false` | Additionally write a CEF-formatted line (`<file>.cef`) alongside every entry, for SIEM forwarding. |
| `imap_mcp.token` | string | `""` | Bearer token imap-mcp ≥ 0.5.3 requires on every request except `GET /api/health`. Accepts a `${secret:name}` reference. Needs `read` + `send` scopes. |
| `plugins.managed` | []string | `[]` | Names of `kind: service` plugins the operator allows datawatch to start/stop as supervised child processes. Absent = never managed. Only honoured when the plugin's manifest `lifecycle.supported_modes` declares `managed` and the plugin is enabled. |
| `cross_session.enabled` | bool | `true` | Inject the Cross-Session Communication Rule into spawned claude-code sessions' `CLAUDE.md` — steers the LLM toward datawatch's own audited memory/discussion/reply tools instead of Claude Code's native cross-session messaging. |

### Session list views

`GET /api/sessions` accepts three optional query parameters. Without any
of them the response is exactly what it has always been.

- `view=summary` — same JSON array of session objects (including
  `scheduled_count` when schedules are enabled), but the long text
  fields `task`, `prompt_context`, `last_response` and
  `last_summary_long` are cut to `server.session_summary_text_chars`
  characters with a trailing `…`. Each cut record gets a
  `truncated_fields` array naming the fields that were shortened.
  Terminal sessions (complete, failed, killed) are capped to the newest
  `server.session_summary_terminal_limit` by `updated_at`; when any are
  dropped the response carries `X-Datawatch-Sessions-Omitted: <n>`. Any
  other `view` value returns 400.
- `since=<RFC3339 | unix seconds>` — only sessions whose `updated_at` is
  strictly after the given time. Unparseable values return 400.
- `states=active` — only non-terminal sessions (running, waiting_input,
  rate_limited). A comma list of explicit state names is also accepted,
  e.g. `states=complete,failed`; unknown names return 400.

`since` and `states` also work without `view=summary` (full records).
`tree=1` and `orphaned=1` take precedence and ignore all three. Fetch
the full record for one session with `GET /api/sessions/{id}`.

**Retention decision:** the unfiltered list is intentionally not capped,
so existing clients keep working unchanged. Retention applies only to
`view=summary`. Clients should check for the `sessions.view_summary`
entry in the `capabilities` array of `GET /api/info` before using these
parameters. Both limits are read from the live config on each request,
so a `PUT /api/config` change applies without a restart.

### WebSocket session updates

By default every session change is sent to every WebSocket client as a
`sessions` frame carrying the whole list. A client can ask for less by
sending `hello` at any time after connecting:

```json
{"type":"hello","data":{"session_list":"delta","session_view":"summary"}}
```

The daemon answers with `hello_ack`, echoing the modes now in effect and
the WebSocket capabilities it supports:

```json
{"type":"hello_ack","data":{"session_list":"delta","session_view":"summary","capabilities":["ws.hello","ws.session_list_delta","ws.session_view_summary"]}}
```

- `session_list: "full"` — the existing stream (whole-list `sessions`
  frames plus per-change `session_state`).
- `session_list: "delta"` — no `sessions` frames. Each session that is new
  or changed gets one `session_state` `{session}` frame; each session that
  leaves the list gets one `session_removed` `{session_id}` frame. Fetch
  the starting list with `GET /api/sessions?view=summary` right after
  `hello_ack` and apply the stream on top of it.
- `session_view: "summary"` — `session_state` carries the same compact
  record as `GET /api/sessions?view=summary` (long text fields cut to
  `server.session_summary_text_chars`, with `truncated_fields`).

Unknown `hello` keys are ignored and an unknown value leaves that mode
unchanged. Clients that never send `hello` get
`server.ws_session_list_default`. `GET /api/info` lists `ws.hello`,
`ws.session_list_delta` and `ws.session_view_summary` in `capabilities`.

### WebSocket output scoping

By default every running session's terminal output reaches every
WebSocket client, whether or not it has that session open. A client can
opt in to output for only the sessions it subscribed to by adding
`"output":"subscribed"` to `hello` (`hello_ack` echoes the effective
`output` mode, `all` or `subscribed`):

```json
{"type":"hello","data":{"output":"subscribed"}}
{"type":"subscribe","data":{"session_id":"myhost-a1b2"}}
{"type":"unsubscribe","data":{"session_id":"myhost-a1b2"}}
```

- Scoped frames: `output`, `raw_output`, `pane_capture` and
  `chat_message` (the chat-mode sessions' message stream).
- Never scoped: `response` (one per completed turn), `session_state`,
  `sessions`, `session_removed`, `needs_input`, `alert`, `alerts_read`,
  `notification`, `channel_*` and every other control frame.
- `subscribe` / `unsubscribe` accept the full or short session ID; either
  form matches output for that session.
- `unsubscribe` (any output mode) removes the subscription and stops the
  screen capture the client's `subscribe` started. Before, captures ran
  until the client disconnected.

Clients that never send `output` get `server.ws_output_scope_default`
(`all` unless configured). The PWA buffers output for sessions it is not
viewing, so leave that at `all` while PWA clients are connected.
`GET /api/info` lists `ws.output_scoped` and `ws.unsubscribe` in
`capabilities`.

### API response compression

When `server.api_gzip` is on (the default), every `/api/*` response
carries `Vary: Accept-Encoding`, and a response is sent with
`Content-Encoding: gzip` (and no `Content-Length`) when all of these
hold:

- the request sends `Accept-Encoding: gzip` and is not `HEAD`;
- the body grows past `server.api_gzip_min_bytes` bytes — the daemon
  buffers up to that many bytes first, so small responses go out
  byte-for-byte unchanged with their original headers;
- the `Content-Type` is `application/json`, `application/*+json`,
  `text/plain` or YAML.

Never compressed: WebSocket upgrades and other connection takeovers,
`text/event-stream` streams, a response flushed before reaching the
threshold (it is committed uncompressed so streaming is not delayed),
`204`/`206`/`304`, responses that already set `Content-Encoding` or
`Content-Disposition`, `/api/files/download`, and the `/api/proxy/...`
forwarding paths (a peer's body is passed through as received; Go's
HTTP client already decodes a peer's own gzip transparently). Both
settings are read on each request. Clients can look for `api.gzip` in
the `capabilities` array of `GET /api/info`; it is listed only while
`server.api_gzip` is on.

### Lifecycle event sinks (`autonomous.notify.default_events`)

`autonomous.notify.default_events` is the list of PRD/task lifecycle event
kinds (e.g. `prd.updated`, `prd.completed`, `prd.failed`,
`task.session_started`, `task.completed`, `task.failed`) the built-in
sinks act on. The daemon-wide list enables each kind only when it is
explicitly present.

**The default is off.** An empty list — the shipped default — means
nothing new is sent and the daemon behaves exactly as it always has.
Enabling a kind is always an explicit operator decision, per kind.

The key is enforced at the autonomous sink installer + daemon wiring
(`internal/autonomous`): each sink subscribes only to the lifecycle event
kinds listed in `default_events`, and when one of those kinds fires the
sinks

- write an entry to the alert store (the PWA Alerts tab / `GET /api/alerts`);
- push a notification to the `alerts` topic and the emitting Automaton's
  own `automaton-<id>` topic;
- fan out to comm channels, coalesced per Automaton so a burst of events
  from one Automaton does not become a burst of messages.

The global `notify_exclude` list (see the `notify_exclude` entry above)
still applies to the comm fan-out: a channel named there skips the
lifecycle message broadcast even when the event kind is enabled, and
still handles inbound commands normally.

### Dependencies

- **fsnotify** — used for interrupt-driven log file monitoring (replaces polling). Each session's `monitorOutput` goroutine watches the log file via fsnotify and reads new lines on write events, reducing CPU usage and improving latency.

### Per-Session MCP Architecture

When `channel_enabled: true`, each session gets its own dedicated MCP channel server
on a random port. This replaces the previous global MCP channel and enables true
multi-session support — each claude-code instance communicates with its own channel
server independently. The per-session servers are started automatically when a session
launches and stopped when the session ends. The `mcp.max_retries` config field controls
how many times a failed channel server connection is retried before giving up.

### Data Files

All persistent data is stored in `~/.datawatch/` (or `data_dir` if overridden):

| File | Description |
|---|---|
| `config.yaml` | Daemon configuration |
| `sessions.json` | Session state and history |
| `schedule.json` | Persistent command scheduler entries |
| `commands.json` | Named reusable command library (SavedCommand records) |
| `filters.json` | Output filter rules (FilterPattern records) |
| `alerts.json` | Persistent system alert log |
| `audit.log` | Operator action log (session/config/etc. state changes) — JSON-lines |
| `access.log` | HTTP access / WS lifecycle / auth-failure log — JSON-lines |
| `audit.log.cef`, `access.log.cef` | CEF mirror of the above, written only when `audit.cef_mirror_enabled: true` |
| `logs/<hostname>-<id>.log` | Per-session output log |
| `tls/server/` | Auto-generated TLS certificate/key (when `tls_auto_generate: true`) |
| `daemon.pid` | PID file written in daemon mode |
| `daemon.log` | Daemon log in daemon mode |

---

## 9. Building

### Local binary

```bash
make build
# produces: ./bin/datawatch
```

Or directly with Go:

```bash
go build -o datawatch ./cmd/datawatch/
```

### Cross-compilation

```bash
make cross
# produces:
#   bin/datawatch-linux-amd64
#   bin/datawatch-linux-arm64
#   bin/datawatch-darwin-amd64
#   bin/datawatch-darwin-arm64
```

### Install to `$GOPATH/bin`

```bash
make install
# equivalent to: go install ./cmd/datawatch/
```

### Build tags

No build tags are currently required. CGO is disabled for the default build (`CGO_ENABLED=0`), which produces a fully static binary.

The future native Signal backend (Phase 4) will require `CGO_ENABLED=1` and a Rust toolchain for libsignal-ffi.

---

## 10. Local Development

### Running without a real Signal account

You can run the daemon and exercise all session management functionality without a Signal account by disabling the Signal backend and using the CLI or HTTP API directly.

**Option 1: Use `datawatch session` subcommands (no daemon needed)**

```bash
datawatch session list
datawatch session new "write a hello world program"
datawatch session tail a3f2 --lines 50
datawatch session send a3f2 "yes, continue"
```

**Option 2: Start the daemon with Signal disabled**

Remove (or comment out) the `signal.account_number` and `signal.group_id` from config. The daemon will start with only the HTTP server active:

```bash
datawatch start --config ./dev-config.yaml
```

Then drive commands via curl:

```bash
# List sessions
curl http://localhost:8080/api/sessions | jq .

# Start a new session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "new: write hello world in Go"}'

# Get session output
curl "http://localhost:8080/api/output?id=a3f2&n=50"

# Send input to a waiting session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "send a3f2: yes"}'
```

**Option 3: Mock signal-cli with a shell script**

Create a fake `signal-cli` that reads JSON-RPC from stdin and writes a subscription acknowledgement:

```bash
#!/bin/bash
# fake-signal-cli.sh — minimal signal-cli mock for development
while IFS= read -r line; do
  # Echo a subscribeReceive response
  echo '{"jsonrpc":"2.0","result":{},"id":1}'
done
```

Point the config at it via a wrapper that puts it on PATH before the real signal-cli.

### Using the `--config` flag

All daemon commands accept `--config` to point at a non-default config file:

```bash
datawatch start --config /tmp/dev-config.yaml
datawatch session list --config /tmp/dev-config.yaml
```

### Testing with a dedicated Signal number

The recommended approach for integration testing is to use a cheap VoIP number (e.g. Google Voice, Twilio) to register a dedicated Signal account for the daemon. Keep a test Signal group with that account and your personal number, then run the daemon against it in a tmux session on your development machine.

---

## 11. Memory Lifecycle Management (v8.30.0)

### Memory scope hierarchy

Six-layer scope hierarchy (broadest → narrowest):

```
persona-global → persona-in-project → project-shared → prd-shared → story-shared → session-local
```

`ScopeRef.Resolve()` maps a scope + IDs to `(projectDir, role, sessionID)`:

| Scope | role | sessionID |
|---|---|---|
| `project-shared` | `""` | `""` |
| `prd-shared` | `"prd/<prdID>"` | `""` |
| `story-shared` | `"story/<storyID>"` | `""` |
| `session-local` | `""` | session ID |

SQLite deduplication is per `(projectDir, content_hash)` — same content in the same project only stores once regardless of role.

### New REST endpoints (v8.30.0)

#### Archive import

```
POST /api/memory/scopes/archive-import
```

Seeds memories from archived breadcrumbs into a target Automaton's `prd-shared` scope. Optional `role_filter`, `max`, `dry_run` params.

Request body:
```json
{
  "target_scope": {"scope": "prd-shared", "project": "/path", "prd_id": "abc"},
  "source_project": "/path",
  "role_filter": "archived",
  "max": 50,
  "dry_run": false
}
```

#### Scope inventory

```
GET /api/memory/scopes/inventory?project=/path/to/project
```

Returns all role/session_id groups with row counts for a given project directory.

Response:
```json
{
  "project": "/path/to/project",
  "scopes": [
    {"role": "prd/abc123", "count": 12},
    {"role": "story/s01", "count": 4},
    {"role": "", "session_id": "sess-xyz", "count": 7}
  ],
  "count": 3
}
```

#### Automaton memory report

```
GET /api/autonomous/prds/{id}/memory-report?include_scopes=prd,story,session&max_per_scope=50
```

Aggregates and deduplicates all memories for a completed Automaton across all configured scopes. Returns grouped results with total deduplicated count.

Response:
```json
{
  "prd_id": "abc",
  "total": 18,
  "scopes": {
    "prd-shared": [...],
    "story-shared": [...],
    "session-local": [...]
  }
}
```

### New MCP tools (v8.30.0)

| Tool | Description |
|---|---|
| `memory_archive_import` | Import archived memories into a target Automaton scope; supports `dry_run` |
| `memory_handoff` | Write a structured handoff summary to `story-shared` for the next task |
| `memory_prd_report` | Aggregate all memories for an Automaton across scopes (wraps REST endpoint) |
| `memory_scope_inventory` | Discover which scopes have data for a project, with row counts |

### Internal packages

- **`internal/memory/scopes.go`** — `PurgeScope`, `ArchiveScope`, `SweepScopedByAge`
- **`internal/memory/backend.go`** — `InventoryBackend` optional interface + `ScopeInventoryEntry`
- **`internal/memory/store.go`** — `Store.Inventory()` GROUP BY SQL; `.UTC()` fix on all `Prune*` calls
- **`internal/server/memory_scopes.go`** — `archive-import` and `inventory` REST handlers
- **`internal/server/autonomous.go`** — `memory-report` route + `handlePRDMemoryReport`
- **`internal/mcp/memory_scopes.go`** — all four new MCP tools

## Session idle timeout (autonomous.session_idle_timeout_seconds)

- **Field:** `autonomous.session_idle_timeout_seconds`, default `900` (15 minutes). Bounds one autonomous worker or verifier session that sits in `waiting_input` or `idle` with no live child process.
- **Surfaces:**
  - config YAML (`internal/config/config.go`, `Autonomous.SessionIdleTimeoutSeconds`);
  - REST `GET` / `PUT` on the `autonomous.*` config (`internal/server/api.go`);
  - MCP `autonomous_config_set` (`internal/mcp/autonomous.go`, `session_idle_timeout_seconds` parameter);
  - web settings card (`internal/server/web/app.js`) with strings in 5 locales (en, de, es, fr, ja).
- **Default:** `SessionIdleTimeoutDefaultSeconds = 900` and `NormalizeSessionIdleTimeoutSeconds` in `internal/config/session_idle_timeout.go`. Zero or negative values normalize to the default.
- **Detector hook points:**
  - `cmd/datawatch/main.go`, the worker wait loop, for task sessions;
  - `cmd/datawatch/session_judgment.go`, the verifier one-shot session.
- **Failure path:** a tripped bound is reported as the infra failure `session idle without completion`, raises an alert, and is retried through the existing `auto_fix_retries` budget.

## Pi worker observability (progress, heartbeat, loop detection)

How datawatch watches pi-based autonomous workers, and where the two
operator knobs live. Operator-facing behavior:
"Pi worker observability" in [`operations.md`](operations.md).

### Components

- **`internal/pilog`** — read-only reader over pi's native session log (a
  stream of JSON lines written by pi itself). `LocateLog` finds the log
  newest-for the task session under the pi sessions root; `Read` builds a
  `pilogo.Progress` snapshot (last event time, assistant-turn count,
  per-tool call counts, last tool name + compact args, last text/thinking
  summary truncated to 200 chars, token counts); `ReadTurns`/`Recent`
  expose the last N assistant turns for loop detection. datawatch never
  writes to the log and never parses the pane.
- **`cmd/datawatch/progress_refresh.go`** — `refreshTaskProgress` is the
  progress + loop-detection half: locate log → read → snapshot →
  evaluate both loop detectors. Called from the autonomous manager's wait
  loop (`cmd/datawatch/main.go`) about every 30 seconds while a pi task is
  running. Writes the snapshot to `Task.Progress` and, in addition to the
  loop warning, the `autonomous` package's live detection.
- **Heartbeat (stall) check** — `cmd/datawatch/stale_worker.go`,
  `stall_decision.go`, `stale_evidence.go`: a worker counts as stalled
  only when *every* signal of life is absent for longer than
  `autonomous.stale_task_seconds` (falling back to
  `session.stale_timeout_seconds`): no log output (log mtime *and* age of
  the newest log event), no live runner child process, no busy backend,
  no recently changed file in the task's work directory. The failure names
  that evidence (`worker stalled: no output, no children, no file changes
  for N seconds (stale_task_seconds=...)`) and wraps `ErrWorkerStalled` so
  the executor's retry path picks it up. `staleWorkerAppliesTo` exempts
  the `pi-prompt` family from the log-mtime part of the check: `pi
  --print` writes its log only on completion, so the mtime gate would
  report false stalls there.
- **`internal/autonomous/loop_detect.go`** — `DetectLoop(turns, n)`: over
  the last `n` assistant turns, fires `KindAnnounce` (a turn claims a next
  action — "now i will …", "let me create/write …", "i will now …" — but
  calls no tool) or `KindRepeat` (the identical tool + arguments repeated
  across the window). Produces a `LoopFinding` with `Warning()`
  (plain-language text recorded on the task) and a `Hint()`.
- **`internal/autonomous/loop_rescue.go`** — `RescueLoop` has three parts,
  in order: deliver the rescue (a corrective message into an input-
capable pi session via `SendInput`, or stop the one-shot `pi-prompt`
  session), record the loop warning on `Task.LoopingWarning`, and emit the
  `task.looping` lifecycle event (`KindTaskLooping`). A stopped one-shot
  run is routed into the worker's stall-retry path via `LoopRetryError`
  (wraps `ErrWorkerStalled`, hint in the error text) so the next
  attempt's spec carries the loop hint. Fires at most once per distinct
  finding: an already-rescued `(session, finding-key)` pair (`kind + named-
  action`) is reported as `skipped` without re-record, re-emit, re-send, or
  re-stop; a failed delivery consumes no fires-once opportunity, so the
  next poll retries cleanly.
- **`cmd/datawatch/progress_refresh.go` streak detector** — the always-on
  coarser warning: the same tool name + arguments unchanged across three
  consecutive ~30 s refreshes adds `looping suspected: tool … repeated
  across N consecutive progress refreshes` to the task. Evolving args
  reset the streak.
- **`internal/pilog` UI consumer** — the web progress block
  `renderPiProgressBlock` in `internal/server/web/app.js` renders
  `Task.Progress`: last-event age, assistant-turn count, tool-calls
  table, last tool, last thinking/text summary, tokens, and the loop
  warning. Pinned by `app-pi-progress.test.js` (progress keys +
  poll-stacking guard).

### Knobs

- **`pi.thinking_level`** (`PiConfig.ThinkingLevel` in
  `internal/config/config.go`, validated in `internal/config/pi_thinking_level.go`;
  `""` → no `--thinking` flag, i.e. pi's model default; `off|minimal|low|medium|high` otherwise).
  Applied to every pi worker launch (`cmd/datawatch/main.go` and
  `autonomous_effort.go` build the launch request with the global
  `pi.thinking_level`); per-task/story/PRD thinking-LLM overrides
  (`/api/autonomous/prds/…/set_thinking*`) take precedence over the
global default. Web surface: settings card *Pi (worker backend)*
  (`id: 'piworker'` in `GENERAL_CONFIG_FIELDS`, `internal/server/web/app.js`)
  with labels `settings_pi_thinking_level` in all 5 locales.
- **`autonomous.loop_detect_turns`** (`Autonomous.LoopDetectTurns *int` in
  `internal/config/config.go`; `internal/config/loop_detect_turns.go`:
  `nil` → `DefaultLoopDetectTurns = 3`, `0` → detection off, `n > 0` →
  window of n turns, negative rejected by
  `ValidateLoopDetectTurns` at every write edge). Consumed by
  `refreshTaskProgress` (`cmd/datawatch/progress_refresh.go`) as the
  `DetectLoop` turn window. Web surface: the autonomous settings card
  (`GENERAL_CONFIG_FIELDS`, `internal/server/web/app.js`) with label
  `settings_loop_detect_turns` in all 5 locales.

### Tests

- `internal/autonomous/loop_detect_test.go`, `loop_rescue_test.go` — detector and rescue semantics.
- `cmd/datawatch/progress_refresh_test.go` — refresh with `loop_detect_turns = 0` (detector off, snapshot still produced), `TestLoopDetectTurnsEffective`, `TestValidatePiThinkingLevel`.
- `internal/autonomous/thinking_level_test.go`, `internal/session/thinking_level_passthrough_test.go` — thinking-level inheritance down to the launch.
- `internal/server/loop_detect_thinking_test.go` — REST round-trips: `pi.thinking_level` and `autonomous.loop_detect_turns` accepted/validated through `PUT /api/config` (invalid levels rejected, wrong types rejected, `""` re-enables the pi default).
- `internal/server/web/app-autonomous-settings-fields.test.js` — asserts `autonomous.loop_detect_turns` renders as a number with its label key, `pi.thinking_level` renders as a select offering `""/off/minimal/low/medium/high`, and both `settings_loop_detect_turns` / `settings_pi_thinking_level` labels are non-empty in en, de, es, fr, ja.
- `internal/server/web/app-pi-progress.test.js` — the PWA progress block: the progress keys it renders and the poll-stacking guard.
