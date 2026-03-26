# Command Reference

All commands are sent as plain text messages in the configured Signal group.

---

## Session ID Format

Session IDs are 4 hexadecimal characters (e.g., `a3f2`). They are randomly generated when a session starts and are unique per hostname. The full session identifier is `<hostname>-<id>` (e.g., `myserver-a3f2`), but most commands accept the short 4-character form.

---

## Hostname Prefix

Every reply from `claude-signal` is prefixed with `[hostname]` to identify which machine is responding. When multiple machines share a group, each machine replies to commands independently.

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
- Output is logged to `~/.claude-signal/logs/<hostname>-<id>.log`

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
[myserver] claude-signal commands:
new: <task>       - start a new claude-code session
list              - list sessions + status
status <id>       - recent output from session
send <id>: <msg>  - send input to waiting session
kill <id>         - terminate session
tail <id> [n]     - last N lines of output (default 20)
attach <id>       - get tmux attach command
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

And `claude-signal` routes it to `b7c1` automatically.

If multiple sessions are waiting for input, the implicit reply is rejected and you must use the explicit `send <id>: <message>` format.

---

## Multi-Machine Behavior

When multiple machines share a Signal group, each machine processes commands independently:

- `list` causes every machine to reply with its own sessions
- `status a3f2` is handled by the machine that has a session with ID `a3f2` — other machines ignore it
- `new: <task>` causes every machine to start a session (to target a specific machine, consider prefixing tasks with the hostname or using machine-specific groups)

See [multi-session.md](multi-session.md) for multi-machine coordination patterns.
