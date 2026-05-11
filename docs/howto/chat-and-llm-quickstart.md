---
docs:
  index: true
  topics: [chat, llm, quickstart, ask]
exec_params:
  - {name: question, required: true, description: "Single-shot question for the LLM"}
  - {name: backend, required: false, default: "ollama", description: "ollama or openwebui"}
  - {name: model, required: false, default: "", description: "Optional model override"}
exec_steps:
  - tool: ask
    description: One-shot LLM ask — no session, no tmux
    args:
      question: "{{params.question}}"
      backend: "{{params.backend}}"
      model: "{{params.model}}"
    read_only: true
---
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
- At least one LLM configured in the registry (v7.0+):
  - `claude-code` (Anthropic) — needs `claude` CLI + `~/.claude.json`;
    auto-registered as `claude-code` on first start.
  - `ollama` (local) — free; needs `ollama` running + at least one
    model pulled (`ollama pull llama3.1:8b`); auto-registered as `ollama`.
    Models can also be pulled directly from the PWA via the Ollama
    marketplace (Settings → Compute Nodes → node → Models → marketplace
    icon). See [`ollama-marketplace.md`](ollama-marketplace.md).
  - Any OpenAI-compatible endpoint — add via Settings → Compute → LLM
    Configuration → **+ Add LLM**.
- (Optional but recommended for chat) one comm channel:
  - **Signal** (E2E, the default datawatch flagship) — needs
    `signal-cli` + a linked device.
  - **Telegram** — needs a bot token from @BotFather.
  - **Slack** / Discord / Matrix — see [`comm-channels.md`](comm-channels.md).

> **v7.0 change:** LLM configuration moved from `config.yaml` blocks to the
> **LLM Registry** (Settings → Compute → LLM Configuration). Each entry has a
> name, kind, and per-node model list. See [`llm-registry.md`](llm-registry.md)
> and [`compute-nodes.md`](compute-nodes.md) for the full picture.

## Setup

Configure ONE LLM + ONE channel. Picking the cheapest path:

```sh
# LLM: ollama (free, local). Ollama is auto-registered as "ollama" if
# cfg.ollama.host is set. Add a model:
datawatch llm models add ollama --node datawatch-ollama --model llama3.1:8b

# Or pull directly:
ollama pull llama3.1:8b
# (daemon auto-discovers it on next refresh-models cycle)

# Channel: Telegram (free, fast to set up).
datawatch secrets set TELEGRAM_BOT_TOKEN "<from-BotFather>"
datawatch config set channels.telegram.enabled true
datawatch config set channels.telegram.bot_token '${secret:TELEGRAM_BOT_TOKEN}'
datawatch reload
```

Verify:

```sh
datawatch llm list                # shows ollama: enabled, models listed
datawatch channels list           # telegram: connected
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Spawn a session using the LLM registry name.
#    --llm picks the named LLM entry; dispatcher routes to the right node.
SID=$(datawatch sessions start --llm ollama --model llama3.1:8b \
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

<!-- screenshot: PWA new-session wizard with LLM dropdown open -->

1. Bottom nav → **Sessions** → **+** (lightning bolt) FAB.
2. Wizard:
   - **LLM**: pick from your configured LLM registry entries (e.g. `ollama`)
   - **Model**: picked from that LLM's enabled models list
   - **Task**: *"Help me draft a one-paragraph commit message"*
   - **Workspace**: pick a project profile or `/tmp`
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
# Start a session (v7.0: use "llm" for registry name, "model" for model override).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"llm":"ollama","model":"llama3.1:8b","task":"...","project_dir":"/tmp"}' \
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
  default_llm: ollama
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

- **No LLM reachable.** `datawatch llm list` shows `unreachable`. Common: ollama not running (`ollama serve`), wrong URL (check Settings → Compute → Compute Nodes), claude binary not on PATH. Fix the path / start the service.
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

- See also: [`llm-registry.md`](llm-registry.md) for full LLM registry management.
- See also: [`compute-nodes.md`](compute-nodes.md) for compute node setup.
- See also: [`comm-channels.md`](comm-channels.md) for full per-channel setup.
- See also: [`voice-input.md`](voice-input.md) to add voice transcription.
- See also: [`profiles.md`](profiles.md) for per-workspace defaults.
- See also: [`sessions-deep-dive.md`](sessions-deep-dive.md) for the session anatomy.

## Screenshots needed

- [ ] PWA new-session wizard with LLM dropdown open
- [ ] Telegram chat round-trip with the bot
- [ ] Signal chat round-trip
- [ ] Session detail showing first LLM response streaming in xterm
- [ ] CLI `datawatch llm list` output (healthy)

---

## See also

- [howto/llm-registry](llm-registry.md)
- [howto/compute-nodes](compute-nodes.md)
- [howto/comm-channels](comm-channels.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [howto/mcp-tools](mcp-tools.md)
