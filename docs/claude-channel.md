# Claude Code Channel Integration

datawatch supports two distinct communication modes when running claude-code sessions:
**channel mode** (MCP channel) and **console mode** (tmux direct I/O). Understanding
the difference matters for automation, permission handling, and troubleshooting.

---

## Two Modes: Channel vs Console

### Console mode (tmux)

Every claude-code session always runs inside a tmux window. The tmux pane is where
the actual `claude` process lives — it has a real TTY, renders Claude's interactive
UI, and receives raw keyboard input via `tmux send-keys`.

Console mode is the **only** path for:

- **Folder trust/permission prompts** — the very first time claude-code accesses a new
  directory it shows a numbered menu asking whether you trust the folder. This prompt
  is rendered in the terminal UI and must be answered by sending `1` (or `Enter`) to
  the tmux pane. It cannot be answered over the MCP channel.
- **Auth dialogs** — `claude auth login`, browser-based OAuth callbacks, and similar
  flows require terminal interaction.
- **Raw terminal input** — anything that requires a real TTY (password prompts from
  subprocesses, `vim`, interactive shells spawned by Claude).
- **Session resume prompts** — if Claude's conversation history has grown large, it
  may ask before continuing; this appears in the terminal.

datawatch detects these console prompts via the idle-detection system and marks the
session as `waiting_input`. You respond via `send <id>: 1` (Signal/Telegram/etc.) or
the web UI quick-input buttons, which pipe the reply back through `tmux send-keys`.

### Channel mode (MCP channel, `--dangerously-load-development-channels`)

When `channel_enabled: true` is set, each session is launched with:

```
claude --dangerously-load-development-channels server:datawatch --add-dir <dir>
```

This loads the datawatch MCP channel server as a development channel. Claude receives
an experimental `claude/channel` capability and can communicate bidirectionally with
datawatch over MCP protocol — independently of the terminal.

Channel mode enables:

- **Tool-use notifications** — Claude proactively sends messages to datawatch when it
  runs tools, completes work, or needs guidance, without waiting for a terminal prompt.
- **Bash/shell tool output** — results from Claude's `bash` tool, file edits, grep
  searches, and all other tool executions flow through the channel as structured MCP
  notifications, giving datawatch a clean, ANSI-free view of what Claude is doing.
- **Reply routing** — messages sent from Signal/Telegram/the web UI are delivered to
  Claude via the `reply` MCP tool, routing around the terminal entirely. Claude
  processes them as task continuations without any tmux input.
- **Rate-limit signalling** — Claude can signal `DATAWATCH_RATE_LIMITED: resets at
  <time>` over the channel, letting datawatch schedule an automatic retry.
- **Session completion** — Claude sends `DATAWATCH_COMPLETE: <summary>` over the
  channel when a task finishes.

---

## What goes where: quick reference

| Interaction | Console (tmux) | Channel (MCP) |
|---|---|---|
| Folder trust prompt (first run per dir) | **Yes — only here** | No |
| Dev-channels consent prompt (first run w/ `channel_enabled`) | **Yes — only here** | No |
| Auth / OAuth dialogs | **Yes — only here** | No |
| Tool execution output (bash, edit, grep…) | Rendered in tmux UI | **Also here (clean)** |
| Claude's task reply/commentary | Rendered in tmux UI | **Also here (structured)** |
| `send <id>: <msg>` from messaging backends | **Piped via tmux** | **Piped via reply tool** |
| Rate-limit detection | Idle timeout + pattern | **Channel signal** |
| Task-complete detection | `DATAWATCH_COMPLETE` pattern | **Channel signal** |

> **Note:** The console and channel run in parallel — they are not mutually exclusive.
> Channel mode adds a second communication path on top of the normal tmux session.
> Responses sent from datawatch go to whichever path Claude is listening on at the
> time; in practice this is the channel for mid-task messages and the tmux pane for
> initial permission prompts.

---

## One-time prompts (console only)

Two prompts appear exactly once per environment and require console interaction:

### 1. Folder trust prompt

Appears the **first time** `claude` accesses a given directory:

```
Quick safety check: Is this a project you created or one you trust?
  ❯ 1. Yes, I trust this folder
    2. No, exit

Enter to confirm · Esc to cancel
```

**How to handle:**
- datawatch detects this via the `trust this folder` / `Enter to confirm` prompt
  patterns and marks the session `waiting_input`.
- Reply with `send <id>: 1` (or press `Enter` via the web UI quick-input button).
- After trusting once, the folder is remembered by claude-code and the prompt will
  not appear again for that directory.

### 2. Development channels consent prompt

Appears the **first time** `--dangerously-load-development-channels` is used:

```
I am using this for local development
Loading development channels…
```

**How to handle:**
- datawatch detects this via the `I am using this for local development` pattern and
  marks the session `waiting_input`.
- Reply with `send <id>: ` (blank Enter) or use the `[Enter]` quick-input button.
- After confirming once, the consent is remembered across sessions.

Both prompts can be answered automatically by adding a `send_input` filter:

```bash
# Auto-approve folder trust (use with caution — only in controlled environments)
datawatch cmd filter add "Yes, I trust|trust this folder" send_input 1

# Auto-confirm dev channels consent
datawatch cmd filter add "I am using this for local development" send_input ""
```

---

## Enabling channel mode

In `~/.datawatch/config.yaml`:

```yaml
session:
  llm_backend: claude-code
  channel_enabled: true
```

The MCP channel server (`channel/index.js`) must be registered with Claude Code:

```bash
claude mcp add -s user \
  -e DATAWATCH_CHANNEL_PORT=7433 \
  -e DATAWATCH_API_URL=http://localhost:8080 \
  -- datawatch node /path/to/datawatch/channel/dist/index.js
```

Then restart `datawatch`. Sessions started after this point will use channel mode.

---

## Session mode badge

The web UI shows a mode badge on each session card and in the session detail header:

| Badge | Mode | Meaning |
|---|---|---|
| `tmux` | Console only | Standard mode; all I/O through tmux |
| `ch` | Channel | claude-code with MCP channel enabled |
| `acp` | ACP | opencode with ACP API enabled |

In channel mode, Claude's replies appear in the output area as amber-highlighted
channel reply lines (distinct from raw tmux terminal output).

---

## Troubleshooting

### Claude isn't receiving my messages in channel mode

1. Check that the channel server is running: `claude mcp list` should show `datawatch`.
2. Verify `DATAWATCH_API_URL` points to the running datawatch server.
3. Check the channel server port matches `session.channel_port` in config (default: 7433).

### The folder trust prompt isn't being detected

The session must go idle (no output for the configured idle timeout, default 2s) before
datawatch checks for prompt patterns. If the prompt appears and then more output follows
immediately, the idle check may miss it.

With the `detect_prompt` filter action (seeded by `datawatch seed`), prompt patterns
are detected immediately on each output line without waiting for idle timeout.
Run `datawatch seed` if you haven't already.

### I see the trust prompt every time

The trust prompt appears per directory. If your `project_dir` varies (e.g. a temp path)
Claude will prompt for each one. Use a stable project directory path, or add an
auto-approve filter (see "One-time prompts" above) if you're in a controlled environment.

### Channel messages appear but terminal output is garbled

The channel and tmux pane carry the same information but in different formats. The tmux
pane renders Claude's full interactive UI (with ANSI colours, spinner, etc.) while the
channel delivers clean UTF-8 text. If both are shown together in the web UI output area,
you may see duplicated or interleaved content — this is expected; the channel reply lines
(amber border) are the canonical structured output.
