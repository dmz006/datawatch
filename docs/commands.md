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
help              - show this help
```

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

### `datawatch session schedule add <session-id> <command> [--at <when>]`

Schedule a command to be sent to a session.

| Flag | Description |
|---|---|
| `--at` | When to run: `now`, `HH:MM` (24h), or RFC3339 timestamp. Default: on next input prompt |

```bash
# Run when session next asks for input
datawatch session schedule add a3f2 "yes, continue"

# Run at 14:30 today
datawatch session schedule add a3f2 "run the tests" --at 14:30

# Stack commands: run B after A completes
datawatch session schedule add a3f2 "commit the changes" --at now
```

### `datawatch session schedule list`

List all scheduled commands.

### `datawatch session schedule cancel <schedule-id>`

Cancel a pending scheduled command.

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
| `schedule_add` | Schedule a command (`session_id`, `command`, optional `run_at`) |
| `schedule_list` | List all pending scheduled commands |
| `schedule_cancel` | Cancel a scheduled command by ID |

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
