# How-to: Communication channels

Datawatch listens for commands and pushes notifications across 11
messaging backends. Configure one (or many); each operator-typed
message becomes session input, each LLM reply gets pushed back. This
howto walks the per-channel setup, the canonical verbs, and the
per-channel divergences.

## What it is

Each backend is a long-running adapter inside the daemon that:

- Connects to its provider with operator-supplied credentials.
- Receives messages from configured chats / users / rooms.
- Routes inbound messages into sessions (verbs like `start:`, replies,
  `stop:`).
- Pushes outbound notifications + LLM replies back to the same chat.

Supported backends: **Signal**, **Telegram**, **Discord**, **Slack**,
**Matrix**, **Twilio (SMS)**, **GitHub webhook**, **Generic webhook**,
**Email**, **DNS channel** (covert), **ntfy**.

## Base requirements

Per-backend; the cheapest paths are summarized:

| Backend | Setup cost | Notes |
|---|---|---|
| Telegram | Bot token from @BotFather | Free, fast, no client needed. |
| Signal | `signal-cli` + linked device | E2E, the flagship; needs Java runtime. |
| Slack | OAuth app + bot scopes | Workspace-admin install. |
| Discord | Bot token + guild intent | Free; works in any guild you can add the bot to. |
| Matrix | Homeserver account + access token | Self-hosted-friendly. |
| Twilio | Twilio account SID + auth token + number | Paid SMS. |
| GitHub webhook | Webhook secret | Inbound only (issues / PRs / etc.). |
| Generic webhook | Shared secret | Inbound; any HTTP source. |
| Email | SMTP creds + IMAP receive | Async; slower roundtrip. |
| DNS | Custom NS or DoH gateway | Covert; for restricted environments. |
| ntfy | ntfy.sh account or self-hosted | Push-only (one-way). |

## Setup

Per-backend YAML block in `~/.datawatch/datawatch.yaml`. Telegram
example:

```yaml
channels:
  telegram:
    enabled: true
    bot_token: ${secret:TELEGRAM_BOT_TOKEN}
    allowed_chats: [123456, 789012]   # restrict to specific chat IDs
    default_backend: ollama            # which LLM backend to spawn against
    rate_limit_per_minute: 30
```

Signal example:

```yaml
channels:
  signal:
    enabled: true
    signal_cli_path: /usr/local/bin/signal-cli
    device_number: "+15555550100"
    daemon_socket: /run/signal-cli/socket
    allowed_groups: ["GROUP_ID_BASE64"]
```

Apply: `datawatch reload` (or `restart` for credential changes).

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Store the credential.
datawatch secrets set TELEGRAM_BOT_TOKEN "<from BotFather>"

# 2. Enable the channel + point at the secret.
datawatch config set channels.telegram.enabled true
datawatch config set channels.telegram.bot_token '${secret:TELEGRAM_BOT_TOKEN}'
datawatch config set channels.telegram.allowed_chats '[123456]'
datawatch reload

# 3. List + verify.
datawatch channels list
#  → telegram     CONNECTED   2 chats allowed
#    signal       OFF
#    discord      OFF

datawatch channels test telegram
#  → POST /api/channels/telegram/test → ok; reply received within 230ms

# 4. Tail traffic.
datawatch channels tail telegram -f
# (Now type into the Telegram chat — you'll see in/out messages here.)

# 5. Disable when done.
datawatch channels disconnect telegram
```

### 4b. Happy path — PWA

1. Settings → Comms → **Communication Configuration** card.
2. Find the Telegram row → click **Configure**. Modal opens with the
   bot token field, allowed chats list, default backend dropdown.
3. Paste the bot token (auto-stored as `${secret:TELEGRAM_BOT_TOKEN}`).
4. Add a chat ID via **+ Allow chat**.
5. **Save**. The row's status dot turns green when connected.
6. Click **Test** to send a self-test message; the bot should reply.
7. Now from the Telegram app: send `start: Tell me a joke`. The bot
   responds with the LLM's first reply.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Comms → same Communication Configuration card. Per-channel
edit modals match the PWA. Useful when configuring on a phone with
the bot token in your password manager.

### 5b. REST

```sh
# List channels.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/channels

# Connect / disconnect.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/channels/telegram/connect
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/channels/telegram/disconnect

# Test (sends a self-test message).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/channels/telegram/test

# Send arbitrary text via the channel.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"chat_id":123456,"text":"Hello from datawatch"}' \
  $BASE/api/channels/telegram/send
```

### 5c. MCP

Tools: `channel_list`, `channel_connect`, `channel_disconnect`,
`channel_test`, `channel_send`.

Useful when an LLM in a session wants to ping the operator out-of-band:
*"I'm done; I'll Telegram you the summary"* → calls `channel_send`.

### 5d. Comm channel (cross-channel + canonical verbs)

Every channel adapter recognizes the same verb vocabulary:

| Verb | Effect |
|---|---|
| `start: <task>` | Spawn a session using the channel's default backend. |
| `<reply>` | Continue the most-recent session in this chat. |
| `state <id>` | One-line state for the session. |
| `state list` | All sessions for this chat. |
| `stop:<id>` | Kill the session. |
| `restart:<id>` | Restart a terminal-state session, seeded from prior. |
| `health` | Daemon health one-liner. |
| `secrets list` | Operator-only; gated to private chats. |
| `council quick "<proposal>"` | Trigger a Council Mode quick check. |
| `automaton: <spec>` | Decompose into a PRD; queue for review. |

Each adapter may add channel-specific verbs (Slack supports threaded
replies; Matrix supports room-scoped routing).

### 5e. YAML

`channels.<name>.*` block per backend. Common fields:

```yaml
channels:
  <name>:
    enabled: true
    default_backend: ollama          # which LLM to spawn for "start:"
    rate_limit_per_minute: 30
    allowed_chats: [...]              # or allowed_groups, allowed_rooms
    push_notifications: true          # daemon-emitted alerts → this channel
    operator_chat: 123456             # private chat for secrets/admin verbs
```

Backend-specific fields (`bot_token`, `signal_cli_path`, etc.)
documented inline in the auto-generated `datawatch init` template.

## Diagram

```
   Telegram     Signal      Discord     Slack     Matrix     ...
       │           │            │          │         │
       └───────────┴────────────┴──────────┴─────────┘
                                │
                                ▼
                  ┌──────────────────────────┐
                  │ datawatch channel adapters│
                  │  (canonical verb parser)  │
                  └──────────┬───────────────┘
                             │
                             ▼
                  ┌──────────────────────────┐
                  │ Session manager           │
                  └──────────────────────────┘
                             │
                             ▼ output
                       Reply pushed
                       back via the
                       same adapter
```

## Common pitfalls

- **Bot replies not arriving.** The bot must be added to the chat AND
  the chat ID listed in `allowed_chats`. Verify with `channel test`.
- **Credentials in plaintext YAML.** Use `${secret:NAME}` references
  (see [`secrets-manager.md`](secrets-manager.md)) instead of inlining.
- **Rate-limit hits at the provider.** Telegram throttles
  ~30 msg/sec/chat; Twilio per-minute. Tune
  `rate_limit_per_minute` per channel.
- **Operator-only verbs over a group chat.** `secrets list` /
  `council` etc. should be gated to a private operator chat. Set
  `operator_chat: <id>` to enforce.
- **Email roundtrip is async.** Don't expect <2s replies; configure
  IMAP poll interval (default 30s) for snappier RTT.

## Linked references

- See also: [`voice-input.md`](voice-input.md) — voice notes auto-transcribe.
- See also: [`secrets-manager.md`](secrets-manager.md) — credential storage.
- See also: [`chat-and-llm-quickstart.md`](chat-and-llm-quickstart.md) — single-channel quickstart.
- Architecture: `../architecture-overview.md` § Comm channels.

## Screenshots needed (operator weekend pass)

- [ ] Settings → Comms → Communication Configuration card with all 11 backends listed
- [ ] Signal device-link QR
- [ ] Telegram bot in a chat, full round-trip
- [ ] Routing Rules card
- [ ] CLI `datawatch channels list` + `datawatch channels test` output
