# How-to: Chat + LLM quickstart

The fastest path from "datawatch is running" to "I'm chatting with an
LLM through Signal / Telegram / the PWA". Picks the operator's most
common backend pairings and walks one end-to-end.

## What it is

Datawatch's primary use is operator-driven AI chat. A spawned session
is a tmux-backed conversation with one LLM backend; messages flow in
either direction through any configured surface (PWA, mobile, chat
channel, CLI, REST). This howto stitches a backend + a chat channel
together.

## Base requirements

- `datawatch start` — daemon up. See [`setup-and-install.md`](setup-and-install.md).
- One LLM backend installed + reachable from the host:
  - `claude-code` (Anthropic, paid) — fast, smart; needs `claude` CLI
    + `~/.claude.json`.
  - `ollama` (local) — free; needs `ollama` running + at least one
    model pulled (`ollama pull llama3.1:8b`).
  - `openai` (OpenAI, paid) — needs API key.
- (Optional but recommended for chat) one comm channel:
  - **Signal** (E2E, the default datawatch flagship) — needs
    `signal-cli` + a linked device.
  - **Telegram** — needs a bot token from @BotFather.
  - **Slack** / Discord / Matrix — see [`comm-channels.md`](comm-channels.md).

## Setup

Configure ONE backend + ONE channel. Picking the cheapest path:

```sh
# Backend: ollama (free, local).
ollama pull llama3.1:8b
datawatch config set llm.backends.ollama.enabled true
datawatch config set llm.backends.ollama.url http://localhost:11434
datawatch config set session.default_backend ollama

# Channel: Telegram (free, fast to set up).
datawatch secrets set TELEGRAM_BOT_TOKEN "<from-BotFather>"
datawatch config set channels.telegram.enabled true
datawatch config set channels.telegram.bot_token '${secret:TELEGRAM_BOT_TOKEN}'
datawatch reload
```

Verify:

```sh
datawatch backends list           # ollama: ENABLED, reachable
datawatch channels list           # telegram: connected
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Spawn a session pointed at ollama.
SID=$(datawatch sessions start --backend ollama --model llama3.1:8b \
  --task "Help me draft a one-paragraph commit message" \
  --project-dir ~/work/foo 2>&1 | grep -oP 'session \K[a-z0-9-]+')

# 2. Watch its first response stream in.
datawatch sessions tail $SID -f
# (Ctrl-C when you're done watching.)

# 3. Send a follow-up.
datawatch sessions input $SID "Make it punchier — one sentence."

# 4. Inspect last response (used for /copy + alerts).
cat ~/.datawatch/sessions/$SID/response.md

# 5. Wrap up.
datawatch sessions kill $SID
```

### 4b. Happy path — PWA

1. Bottom nav → **Sessions** → **+** FAB.
2. Wizard:
   - Backend: ollama → Model: llama3.1:8b
   - Task: *"Help me draft a one-paragraph commit message"*
   - Workspace: pick a project profile or `/tmp`
   - **Start**
3. Detail view opens with xterm streaming. Type follow-ups in the
   input bar at the bottom; press Enter to send.
4. Channel tab (top of the detail view) renders the conversation as
   bubbles for backends that emit structured channel events
   (opencode-acp, claude-code MCP). For ollama, channel = chat-message
   events.
5. To stop: Stop button in the toolbar.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same flow as PWA — Sessions tab → + FAB → backend / task / workspace
→ Start. Detail view with xterm + channel + stats tabs.

### 5b. REST

```sh
# Start a session.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"backend":"ollama","model":"llama3.1:8b","task":"...","project_dir":"/tmp"}' \
  $BASE/api/sessions/start

# Send a follow-up.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"<full-id>","text":"Make it punchier"}' \
  $BASE/api/sessions/<full-id>/input

# Read the latest output.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/sessions/<full-id> | jq .last_response
```

### 5c. MCP

Tools: `session_start`, `session_input`, `session_get`. From inside
an MCP host (claude-code MCP, Cursor) the operator's AI can spawn
sessions in datawatch and watch / drive them; useful for
multi-session orchestration where one LLM coordinates others.

### 5d. Comm channel — the operator default

The whole point of datawatch's comm-channel surface: chat into a
session from your phone.

```
You (Telegram → @datawatch_bot):
   start: Help me draft a one-paragraph commit message

Datawatch (replies):
   started session ralfthewise-7a3c (ollama llama3.1:8b)
   <LLM's first response, streamed back>

You:
   Make it punchier — one sentence.

Datawatch:
   <LLM's reply>

You:
   stop:7a3c
```

The chat session continues in-line; each non-prefix message is sent
as input to the most-recent session in this chat. Verbs:

| Verb | Effect |
|---|---|
| `start: <task>` | Spawn a new session, default backend. |
| `<reply>` | Continue the most-recent session in this chat. |
| `state <id>` | Returns current state. |
| `stop:<id>` | Kills the session. |
| `restart:<id>` | Restarts a terminal-state session, seeded from prior. |

### 5e. YAML

Default backend in `~/.datawatch/datawatch.yaml`:

```yaml
session:
  default_backend: ollama
  default_model: llama3.1:8b
  default_effort: normal     # claude-code only

channels:
  telegram:
    enabled: true
    bot_token: ${secret:TELEGRAM_BOT_TOKEN}
    allowed_chats: [123456]   # restrict to a specific chat ID
```

## Diagram

```
   You (PWA / mobile / Telegram / Signal / CLI)
                   │
                   ▼
       ┌──────────────────────────┐
       │ datawatch session manager │
       │  cs-<id> tmux              │
       └──────────────┬───────────┘
                      │
                      ▼
              LLM backend
       (ollama / claude-code / openai)
                      │
                      ▼ output
              session/<id>/output.log
                      │
                      ▼ stream back
              all surfaces (PWA, mobile, chat, ...)
```

## Common pitfalls

- **No backend reachable.** `datawatch backends list` shows
  `unreachable`. Common: ollama not running (`ollama serve`), wrong
  URL, claude binary not on PATH. Fix the path / start the service.
- **Telegram bot replies not arriving.** The bot must be added to
  the chat AND the chat ID listed in `allowed_chats`. Verify with
  `datawatch channels test telegram`.
- **Spawning without a project_dir.** Defaults to `/tmp`, which is
  fine for chat but not for code work. Pass `--project-dir` or use
  a Project Profile.
- **Session state stays Running while LLM is idle.** See
  [`channel-state-engine.md`](channel-state-engine.md) — wait 15 s for the gap watcher to flip,
  or check that LCE is bumping.

## Linked references

- See also: [`comm-channels.md`](comm-channels.md) for full per-channel setup.
- See also: [`voice-input.md`](voice-input.md) to add voice transcription.
- See also: [`profiles.md`](profiles.md) for per-workspace defaults.
- See also: [`sessions-deep-dive.md`](sessions-deep-dive.md) for the session anatomy.

## Screenshots needed (operator weekend pass)

- [ ] PWA new-session wizard with backend dropdown open
- [ ] Telegram chat round-trip with the bot
- [ ] Signal chat round-trip
- [ ] Session detail showing first LLM response streaming in xterm
- [ ] CLI `datawatch backends list` output (healthy)
