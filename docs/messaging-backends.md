# Messaging Backends

datawatch supports multiple messaging platforms for sending commands and receiving
notifications. Multiple backends can be active simultaneously.

### How to configure any backend

Every backend in this document can be configured through any of these methods:

| Method | How |
|--------|-----|
| **CLI wizard** | `datawatch setup <backend>` — interactive prompts write config for you |
| **YAML** | Edit `~/.datawatch/config.yaml` directly (YAML blocks shown below for reference) |
| **Web UI** | Settings tab → **Comms** tab → expand the backend card |
| **REST API** | `PUT /api/config` with JSON e.g. `{"telegram.enabled": true, "telegram.token": "..."}` |
| **Comm channel** | `configure telegram.enabled=true` or `setup telegram` from any active channel |

The YAML blocks shown in each section below are for reference. The recommended setup path
is `datawatch setup <backend>` or the web UI, which handle validation and save automatically.

See [setup.md](setup.md) for full installation walkthrough.

---

## Backend Overview

| Backend | Direction | Use case |
|---|---|---|
| [Signal](#signal) | Bidirectional | Phone-based control via Signal messenger |
| [Telegram](#telegram) | Bidirectional | Telegram bot in a group or channel |
| [Matrix](#matrix) | Bidirectional | Matrix/Element room |
| [Discord](#discord) | Bidirectional | Discord server channel |
| [Slack](#slack) | Bidirectional | Slack workspace channel |
| [Twilio SMS](#twilio-sms) | Bidirectional | SMS via Twilio |
| [ntfy](#ntfy) | Outbound only | Push notifications to phone via ntfy.sh |
| [Email (SMTP)](#email-smtp) | Outbound only | Email notifications on session events |
| [GitHub Webhook](#github-webhook) | Inbound only | GitHub issue/PR/workflow events trigger sessions |
| [Generic Webhook](#generic-webhook) | Inbound only | HTTP POST from any system triggers sessions |

**Bidirectional:** You can send commands (`new:`, `list`, `send`, etc.) and receive notifications.

**Outbound only:** datawatch sends notifications but cannot receive commands.

**Inbound only:** External systems trigger sessions; notifications go via another backend.

---

## Command Support by Backend

The table below shows which datawatch commands are available through each backend.
Full command syntax is in [docs/commands.md](commands.md).

| Command | Signal | Telegram | Discord | Slack | Matrix | Twilio | ntfy | Email | GitHub WH | Generic WH |
|---|---|---|---|---|---|---|---|---|---|---|
| `new: <task>` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | ✓ (event) | ✓ (POST) |
| `list` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `status <id>` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `tail <id> [n]` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `send <id>: <msg>` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `kill <id>` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `attach <id>` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `alerts [n]` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `setup <service>` | ✓* | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `version` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `update check` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `schedule <id>:` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| `help` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| Implicit reply | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | — | — |
| Alert broadcast | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — |
| State notifications | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — |

**✓*** Signal `setup signal` requires CLI (QR scan); all other setup wizards work via Signal messages.

**GitHub Webhook** and **Generic Webhook** only start sessions — they do not accept commands or send notifications directly. Use a bidirectional backend alongside them for full control.

## Feature Parity by Backend

Rich messaging features are implemented via optional interfaces. Backends without a feature fall back to plain text.

| Feature | Signal | Telegram | Discord | Slack | Matrix | Twilio | ntfy | Email |
|---------|--------|----------|---------|-------|--------|--------|------|-------|
| **Threading** | — | ✓ (reply_to) | ✓ (threads) | ✓ (thread_ts) | — | — | — | — |
| **Rich markdown** | — | ✓ (Markdown) | ✓ (native) | ✓ (mrkdwn) | — | — | — | — |
| **Interactive buttons** | — | — | ✓ (components) | ✓ (Block Kit) | — | — | — | — |
| **File upload** | — | — | ✓ | ✓ | — | — | — | — |
| **Voice input** | ✓ (attachments) | ✓ (Voice/Audio) | — | — | — | — | — | — |

**Threading:** Per-session alerts are grouped in threads, keeping channels clean when managing multiple sessions. Thread IDs are stored on the session for follow-up replies.

**Rich markdown:** Alert headers in bold, task descriptions in italics, output context in code blocks. Each backend converts to its native format.

**Interactive buttons:** When a session is waiting for input, alerts include action buttons (Approve, Reject, Enter). Clicking a button sends the corresponding input to the session.

**File upload:** Session output logs are uploaded to the thread when a session completes.

**Voice input:** Voice messages are downloaded, transcribed via Whisper, and routed as text commands. See [Voice Input](#voice-input-whisper-transcription) for setup.

---

## Signal

Signal is the original and most fully-featured backend. It requires
[signal-cli](https://github.com/AsamK/signal-cli) to be installed and linked.

### Prerequisites

- Java 17 or later
- signal-cli installed (the datawatch installer handles this automatically)

### Setup

```bash
datawatch link
```

Enter your Signal number, scan the QR code (**Settings → Linked Devices → Link New Device**).

datawatch automatically creates a `datawatch-<hostname>` group and saves the group ID
to config. No manual group creation or `listGroups` needed.

See [docs/setup.md](setup.md) for the full walkthrough including signal-cli installation.

### Configuration

Setup: `datawatch link` (CLI) or `datawatch setup signal`. Also configurable via web UI (Settings → Comms → Signal) or REST API.

After `datawatch link` completes, config is written automatically:

```yaml
# Reference — written by 'datawatch link', editable via any method above
signal:
  account_number: +12125551234              # Your Signal phone number
  group_id: aGVsbG8gd29ybGQ=               # Saved automatically by datawatch link
  config_dir: ~/.local/share/signal-cli    # signal-cli data directory
  device_name: my-server                   # Name shown in Signal linked devices
```

Signal is always enabled when `account_number` and `group_id` are set. It does not
need an `enabled: true` key.

### Supported Commands

Signal supports the **full command set** — all commands listed in [docs/commands.md](commands.md)
are available, including `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, and implicit reply.

**Setup wizard note:** `setup signal` cannot be run via a Signal message (QR code requires CLI). All other setup wizards (`setup telegram`, `setup llm aider`, etc.) work normally via Signal.

### How it works

datawatch runs signal-cli as a child process in `jsonRpc` mode, communicating over
stdin/stdout with newline-delimited JSON-RPC 2.0 messages. All messages sent to the
configured group are routed through the command parser.

### Troubleshooting

- **Messages not received:** verify `group_id` with `signal-cli -u +<number> listGroups`
- **signal-cli crashes:** check Java version (`java --version`, needs 17+)
- **Linked device expired:** re-run `datawatch link` to re-link

---

## Telegram

Control datawatch via a Telegram bot added to a group or channel.

### Prerequisites

- A Telegram account
- A bot token from [@BotFather](https://t.me/BotFather)

### Setup

**1. Create a bot**

Open Telegram and message @BotFather:
```
/newbot
```
Follow the prompts. You'll receive a token like `123456789:ABCdefGHIjklMNO...`.

**2. Get your chat/group ID**

Add the bot to your group. Then call:
```
https://api.telegram.org/bot<TOKEN>/getUpdates
```
Look for `"chat":{"id": ...}` in the response. Group IDs are negative numbers.

Alternatively, message the bot directly — the chat ID is your Telegram user ID (positive number).

**3. Configure** — run `datawatch setup telegram` (CLI wizard) or use the web UI (Settings → Comms → Telegram). YAML reference:

```yaml
telegram:
  enabled: true
  token: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
  chat_id: -1001234567890    # negative for groups, positive for direct messages
```

### Supported Commands

Telegram supports the **full command set**: `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, `help`, and implicit reply. All setup wizards (including `setup llm`, `setup session`, `setup mcp`) work via Telegram.

### How it works

datawatch polls the Telegram Bot API for new messages and processes commands from the
configured `chat_id`. Replies and notifications are sent back to the same chat.

### Notes

- Only messages from the configured `chat_id` are processed — others are ignored
- The bot must be added to the group and have permission to send/read messages
- In group chats, commands do not need a `/` prefix (unlike typical bot commands)
- Telegram does not support QR code linking (`datawatch link` is Signal-only)

---

## Matrix

Control datawatch via a Matrix bot in a room. Works with any Matrix homeserver
(matrix.org, Element, self-hosted Synapse, etc.).

### Prerequisites

- A Matrix account on any homeserver
- An access token for the account
- A Matrix room ID

### Setup

**1. Create a Matrix account for the bot** (optional — you can use your own account)

**2. Get an access token**

```bash
curl -XPOST \
  'https://matrix.org/_matrix/client/r0/login' \
  -H 'Content-Type: application/json' \
  -d '{"type":"m.login.password","user":"@mybot:matrix.org","password":"yourpassword"}'
```

Copy the `access_token` from the response.

**3. Get the room ID**

In Element (or any Matrix client), open the room settings → Advanced → Internal Room ID.
It looks like `!roomid:matrix.org`.

**4. Configure** — run `datawatch setup matrix` or use web UI (Settings → Comms → Matrix). YAML reference:

```yaml
matrix:
  enabled: true
  homeserver: https://matrix.org
  user_id: "@mybot:matrix.org"
  access_token: "syt_..."
  room_id: "!abcdef1234:matrix.org"
  auto_manage_room: false    # if true, creates a room named after hostname when room_id is empty
```

### Supported Commands

Matrix supports the **full command set**: `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, `help`, and implicit reply.

### How it works

datawatch uses the Matrix Client-Server API to join the room, listen for messages via
long polling (`/sync`), and send replies. All messages in the room from any user are
processed as commands.

### Notes

- The bot account must be joined to the room before starting datawatch
- `auto_manage_room: true` will attempt to create and join a room named after the hostname
- Works with self-hosted homeservers — set `homeserver` to your server URL

---

## Discord

Control datawatch via a Discord bot in a server channel.

### Prerequisites

- A Discord account
- A Discord application and bot token from the [Discord Developer Portal](https://discord.com/developers/applications)
- A Discord server channel ID

### Setup

**1. Create a Discord application and bot**

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications)
2. Click **New Application** → give it a name
3. Go to **Bot** → **Add Bot**
4. Copy the **Token** (keep it secret)
5. Under **Privileged Gateway Intents**, enable **Message Content Intent**

**2. Invite the bot to your server**

In **OAuth2 → URL Generator**, select:
- Scopes: `bot`
- Bot permissions: `Send Messages`, `Read Message History`, `View Channels`

Copy the generated URL and open it in a browser to invite the bot to your server.

**3. Get the channel ID**

In Discord, enable Developer Mode (User Settings → Advanced → Developer Mode).
Right-click the channel → **Copy ID**.

**4. Configure** — run `datawatch setup discord` or use web UI (Settings → Comms → Discord). YAML reference:

```yaml
discord:
  enabled: true
  token: "your-bot-token"
  channel_id: "1234567890123456789"
  auto_manage_channel: false    # if true, creates a channel named after hostname when channel_id is empty
```

### Supported Commands

Discord supports the **full command set**: `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, `help`, and implicit reply.

### How it works

datawatch connects to Discord's Gateway API using a bot token and listens for messages
in the configured channel. Commands are parsed and executed; replies and notifications
are sent back to the same channel.

### Notes

- Only messages in the configured `channel_id` are processed
- The bot needs **Message Content Intent** enabled in the Developer Portal
- `auto_manage_channel: true` will create a channel named after the hostname if `channel_id` is empty

---

## Slack

Control datawatch via a Slack bot in a workspace channel.

### Prerequisites

- A Slack workspace where you can install apps
- A Slack bot token (starts with `xoxb-`)

### Setup

**1. Create a Slack app**

1. Go to [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From scratch**
2. Under **OAuth & Permissions**, add Bot Token Scopes:
   - `chat:write` — send messages
   - `channels:history` — read messages in public channels
   - `groups:history` — read messages in private channels
3. Install the app to your workspace → copy the **Bot User OAuth Token** (`xoxb-...`)
4. Enable **Event Subscriptions** and subscribe to `message.channels` or `message.groups`

**2. Get the channel ID**

In Slack, right-click the channel → **View channel details** → copy the Channel ID at the bottom.

**3. Configure** — run `datawatch setup slack` or use web UI (Settings → Comms → Slack). YAML reference:

```yaml
slack:
  enabled: true
  token: "xoxb-your-bot-token"
  channel_id: "C1234567890"
  auto_manage_channel: false    # if true, creates a channel named after hostname when channel_id is empty
```

### Supported Commands

Slack supports the **full command set**: `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, `help`, and implicit reply.

### How it works

datawatch uses the Slack RTM (Real-Time Messaging) API to receive messages from the
configured channel and posts replies back using the Slack Web API.

### Notes

- The bot must be invited to the channel: `/invite @your-bot-name`
- `auto_manage_channel: true` will create and join a channel named after the hostname

---

## Twilio SMS

Send commands and receive notifications via SMS using [Twilio](https://twilio.com).

### Prerequisites

- A Twilio account with a phone number
- Twilio Account SID and Auth Token
- A publicly accessible URL for the incoming SMS webhook (or use ngrok for testing)

### Setup

**1. Get Twilio credentials**

Log in to [twilio.com/console](https://www.twilio.com/console). Note your:
- Account SID
- Auth Token
- Phone number (e.g. `+15005550006`)

**2. Configure the incoming SMS webhook**

In the Twilio Console, go to your phone number → **Messaging** → set the webhook URL to:
```
http://your-server:9003/sms
```

The server must be reachable from Twilio's servers. For local testing, use
[ngrok](https://ngrok.com): `ngrok http 9003`.

**3. Configure** — run `datawatch setup twilio` or use web UI (Settings → Comms → Twilio). YAML reference:

```yaml
twilio:
  enabled: true
  account_sid: "ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  auth_token: "your-auth-token"
  from_number: "+15005550006"   # Your Twilio number
  to_number: "+12125551234"     # Your personal number (receives notifications)
  webhook_addr: ":9003"         # Address for the incoming SMS webhook
```

### Supported Commands

Twilio SMS supports the **full command set**: `new:`, `list`, `status`, `tail`, `send`, `kill`, `alerts`, `schedule`, `setup`, `version`, `update check`, `help`, and implicit reply.

**SMS note:** Individual SMS messages are limited to 160 characters. Long `status` or `tail` output is truncated. Use `tail <id> 5` for shorter output over SMS.

### How it works

- **Inbound:** Twilio sends a POST to `webhook_addr/sms` when you send an SMS to the
  Twilio number. datawatch parses the body as a command.
- **Outbound:** datawatch sends SMS to `to_number` via the Twilio API for notifications.

### Notes

- Only SMS from `to_number` are processed as commands — others are ignored
- The webhook must be reachable from Twilio's servers
- SMS has a 160-character limit; long session output may be truncated

---

## ntfy

[ntfy](https://ntfy.sh) is a simple push notification service. This is an outbound-only
backend — you receive notifications on your phone but cannot send commands via ntfy.

### Prerequisites

- The [ntfy app](https://ntfy.sh/#subscribe) on your phone (Android/iOS)
- A topic name (acts as a shared secret — use something unguessable)
- Optional: ntfy token if your topic requires auth, or a self-hosted ntfy server

### Setup

**1. Install the ntfy app** on your phone and subscribe to your topic.

**2. Configure** — run `datawatch setup ntfy` or use web UI (Settings → Comms → ntfy). YAML reference:

```yaml
ntfy:
  enabled: true
  server_url: https://ntfy.sh     # or your self-hosted URL
  topic: my-datawatch-xyz123      # your topic name
  token: ""                       # optional auth token
```

### Notification Events

ntfy is **outbound only** — it receives notifications but cannot be used to send commands. datawatch pushes the following events:

| Event | Notification content |
|---|---|
| Session started | `[host][id] running → running: <task>` |
| Session waiting for input | `[host][id] running → waiting_input: <task>` |
| Session complete | `[host][id] waiting_input → complete: <task>` |
| Session failed | `[host][id] running → failed: <task>` |
| Alert fired | `[host] ALERT [LEVEL] <title>` |

To send commands, combine ntfy with a bidirectional backend (Signal, Telegram, etc.).

### How it works

When a session changes state (started, waiting for input, complete, failed), datawatch
sends an HTTP POST to `<server_url>/<topic>` with the notification message.

### Self-hosted ntfy

```yaml
ntfy:
  enabled: true
  server_url: https://ntfy.example.com   # your ntfy server
  topic: datawatch
  token: "your-ntfy-token"              # if auth is enabled
```

### Notes

- ntfy is purely notification-only; combine with a bidirectional backend (Signal, Telegram, etc.) for full control
- Topics are public by default on ntfy.sh — use a random, unguessable topic name
- The ntfy app can show notifications with priority and tags set by datawatch

---

## Email (SMTP)

Send email notifications when sessions start, complete, need input, or fail.
This is an outbound-only backend.

### Prerequisites

- An SMTP server (Gmail, SendGrid, your own mail server, etc.)
- SMTP credentials

### Configuration

Setup: `datawatch setup email` or web UI (Settings → Comms → Email). YAML reference:

```yaml
email:
  enabled: true
  host: smtp.gmail.com
  port: 587                          # 587 for STARTTLS, 465 for SSL, 25 for plain
  username: me@gmail.com
  password: "app-password"           # use App Password for Gmail
  from: datawatch@gmail.com
  to: me@gmail.com
```

### Notification Events

Email is **outbound only** — it receives notifications but cannot be used to send commands. datawatch sends emails for:

| Event | Subject |
|---|---|
| Session started | `[datawatch][host][id] Session started` |
| Session waiting for input | `[datawatch][host][id] Needs input` |
| Session complete | `[datawatch][host][id] Session complete` |
| Session failed | `[datawatch][host][id] Session failed` |
| Alert fired | `[datawatch][host] ALERT: <title>` |

Body includes session task, current state, and recent output (truncated). To send commands, combine email with a bidirectional backend.

### Gmail setup

Gmail requires an [App Password](https://support.google.com/accounts/answer/185833)
if you use 2-factor authentication:

1. Google Account → Security → 2-Step Verification → App passwords
2. Generate a password for "Mail" / "Other"
3. Use that password in `email.password`

### Notes

- Email is notification-only; combine with a bidirectional backend for full control
- Notifications are sent for: session started, session complete, waiting for input, session failed
- Long session output is truncated in email notifications; use the PWA for full output

---

## GitHub Webhook

Trigger datawatch sessions from GitHub events. This is an inbound-only backend —
it starts sessions based on GitHub events but sends notifications via other backends.

### Prerequisites

- A publicly accessible URL for the webhook (or SSH tunnel / Tailscale Funnel)
- A GitHub repository where you can configure webhooks

### Setup

**1. Add the webhook in GitHub**

Go to your repository → **Settings** → **Webhooks** → **Add webhook**:
- **Payload URL:** `http://your-server:9001/webhook`
- **Content type:** `application/json`
- **Secret:** choose a random string (same as `github_webhook.secret` in config)
- **Events:** select:
  - Issue comments
  - Pull request review comments
  - Workflow dispatches

**2. Configure** — run `datawatch setup github` or use web UI (Settings → Comms → GitHub Webhook). YAML reference:

```yaml
github_webhook:
  enabled: true
  addr: :9001            # address for the webhook listener
  secret: "mysecret"     # must match the GitHub webhook secret
```

### Supported event types

| GitHub event | How it triggers a session |
|---|---|
| `issue_comment` | Comment body starting with `datawatch:` triggers a session with the rest as task |
| `pull_request_review_comment` | Same as issue_comment |
| `workflow_dispatch` | Reads `inputs.task` from the workflow dispatch payload |

### Example: triggering from a GitHub Actions workflow

```yaml
# .github/workflows/ai-task.yml
name: AI Task
on:
  workflow_dispatch:
    inputs:
      task:
        description: Task for datawatch
        required: true

jobs:
  trigger:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger datawatch
        uses: actions/github-script@v7
        with:
          script: |
            await github.rest.repos.createDispatchEvent({
              owner: context.repo.owner,
              repo: context.repo.repo,
              event_type: 'datawatch',
              client_payload: { task: '${{ inputs.task }}' }
            })
```

### Notes

- Webhook signatures are verified using the shared secret (HMAC-SHA256)
- Combine with ntfy or email to receive notifications about triggered sessions

---

## Generic Webhook

Trigger sessions from any HTTP client — a cron job, CI system, home automation, or
any service that can make HTTP requests. This is an inbound-only backend.

### Configuration

Setup: `datawatch setup webhook` or web UI (Settings → Comms → Webhook). YAML reference:

```yaml
webhook:
  enabled: true
  addr: :9002       # address for the webhook listener
  token: "mytoken"  # optional bearer token (recommended)
```

### API

**Start a session via HTTP POST:**

```bash
curl -X POST http://your-server:9002/task \
  -H "Authorization: Bearer mytoken" \
  -H "Content-Type: application/json" \
  -d '{"task": "run database migrations", "project_dir": "/opt/myapp"}'
```

Request body fields:

| Field | Required | Description |
|---|---|---|
| `task` | Yes | Task description passed to the AI backend |
| `project_dir` | No | Absolute path to the project directory. Defaults to `session.default_project_dir` |

Response:

```json
{
  "session_id": "a3f2",
  "full_id": "myserver-a3f2",
  "status": "started"
}
```

**Check session status:**

```bash
curl http://your-server:9002/status/a3f2 \
  -H "Authorization: Bearer mytoken"
```

### Authentication

When `token` is set, all requests must include:
```
Authorization: Bearer <token>
```

Requests without a valid token receive `401 Unauthorized`.

### Example: trigger from a cron job

```bash
# Run at 2am every night
0 2 * * * curl -s -X POST http://localhost:9002/task \
  -H "Authorization: Bearer mytoken" \
  -H "Content-Type: application/json" \
  -d '{"task": "run nightly database cleanup", "project_dir": "/opt/myapp"}'
```

---

## DNS Channel (Covert)

The DNS channel encodes datawatch commands in DNS TXT queries, enabling low-profile communication through standard DNS infrastructure. Commands are HMAC-SHA256 authenticated and responses are fragmented across TXT records.

### Prerequisites

- A domain name with authoritative DNS pointing to the datawatch host (server mode)
- OR a DNS resolver that forwards to the authoritative server (client mode)
- `github.com/miekg/dns` (included in build)

### Setup

Configure via `datawatch setup dns` (CLI), web UI (Settings → Comms → DNS Channel), or YAML below.

**Server mode** (datawatch acts as authoritative DNS server):

```yaml
dns_channel:
  enabled: true
  mode: server
  domain: ctl.example.com
  listen: ":15353"         # bind address (use :53 for production)
  secret: "your-shared-secret-here"
  ttl: 0                   # 0 = non-cacheable
  max_response_size: 512   # max response bytes
```

**Client mode** (CLI queries via resolver):

```yaml
dns_channel:
  enabled: true
  mode: client
  domain: ctl.example.com
  upstream: "8.8.8.8:53"   # resolver that forwards to your DNS server
  secret: "your-shared-secret-here"
```

### Query format

```
<nonce8>.<hmac8>.<base64-labels>.cmd.<domain>.
```

- **nonce**: 8 random hex characters (replay protection)
- **hmac**: first 8 hex chars of HMAC-SHA256(nonce + payload, secret)
- **base64-labels**: command encoded in base64url, split into ≤60-char DNS labels
- **cmd**: fixed literal marker

### Response format

Fragmented TXT records: `"0/N:<chunk>"`, `"1/N:<chunk>"`, ...

Chunks are base64url-encoded, max 240 chars each. Client reassembles by index.

### Supported commands

All standard commands work over DNS: `list`, `status`, `tail`, `new`, `kill`, `send`, `alerts`, `version`.

Commands NOT supported: `attach` (requires interactive terminal), `history` (response too large).

### Testing

```bash
# Encode and send a command via dig
dig @127.0.0.1 -p 15353 TXT <encoded-query>.cmd.ctl.example.com

# Use datawatch CLI in client mode
datawatch --server dns-test session list
```

### Security

The DNS server is designed to be the only publicly exposed datawatch service. All other services (HTTP, MCP, webhooks) should be bound to `127.0.0.1` or behind a VPN.

**Authentication & access control:**
- **HMAC-SHA256** authentication on every query — shared secret required
- **Nonce replay protection** — bounded LRU store (10K entries, 5-minute TTL)
- **Per-IP rate limiting** — default 30 queries/IP/minute (configurable via `rate_limit`)
- **Uniform REFUSED response** — all failure cases (bad HMAC, replay, wrong domain, non-TXT query, rate exceeded) return identical `REFUSED` responses to prevent oracle attacks
- **No recursion** — the server refuses queries for other domains; it cannot be used as an open resolver
- **Catch-all handler** — queries for domains other than the configured one are refused silently

**Secret generation:**
```bash
# Generate a strong 64-character hex secret
openssl rand -hex 32
```

**Deployment model (recommended):**
- DNS server on public interface (`0.0.0.0:53`)
- All other datawatch services on `127.0.0.1`
- See [operations.md](operations.md#7-network-security) for firewall rules and Tailscale setup

### Notes

- DNS label limit is 63 characters; commands are split across multiple labels
- Total query name must be ≤253 characters
- Response limited to `max_response_size` (default 512 bytes) for DNS compatibility
- For full design rationale, see [docs/covert-channels.md](covert-channels.md)

---

## Running Multiple Backends

All backends can run simultaneously. datawatch fans out notifications to every enabled
outbound backend and accepts commands from every enabled bidirectional backend.

**Example: Signal + ntfy + email**

```yaml
signal:
  account_number: +12125551234
  group_id: aGVsbG8gd29ybGQ=

ntfy:
  enabled: true
  server_url: https://ntfy.sh
  topic: my-datawatch-xyz

email:
  enabled: true
  host: smtp.gmail.com
  port: 587
  username: me@gmail.com
  password: "app-password"
  from: me@gmail.com
  to: me@gmail.com
```

Commands are accepted via Signal; notifications go to Signal, ntfy, and email.

---

## Voice Input (Whisper Transcription)

Voice messages sent via Telegram or Signal are automatically transcribed to text and processed as normal commands.

### Requirements

- **Python venv** with `openai-whisper` installed:
  ```bash
  python3 -m venv .venv
  .venv/bin/pip install openai-whisper
  ```
- **ffmpeg** on PATH (used by Whisper for audio decoding)
- For Signal: signal-cli must be configured to download attachments (default behaviour)

### Configuration

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `whisper:` section (see below) |
| **Web UI** | Settings tab → General → **Voice Input (Whisper)** card |
| **REST API** | `PUT /api/config` with `{"whisper.enabled": true, "whisper.model": "base"}` |
| **Comm channel** | `configure whisper.enabled=true`, `configure whisper.model=small`, `configure whisper.language=es` |

```yaml
whisper:
  enabled: true
  model: base        # tiny, base, small, medium, large
  language: en       # ISO 639-1 code, or "auto" for detection
  venv_path: .venv   # path to Python venv with whisper
```

### Supported Languages

Whisper supports 99 languages. Common codes: `en` (English), `es` (Spanish), `de` (German), `fr` (French), `ja` (Japanese), `zh` (Chinese), `ko` (Korean), `pt` (Portuguese), `ru` (Russian), `ar` (Arabic), `hi` (Hindi).

Set `language: auto` for automatic detection (slower, may be less accurate for short messages).

Full list: [Whisper language support](https://github.com/openai/whisper#available-models-and-languages)

### How It Works

1. User sends a voice message via Telegram or Signal
2. Backend downloads the audio file to a temp directory
3. Router detects the audio attachment and calls Whisper CLI
4. Whisper transcribes the audio to text
5. Router echoes the transcription: `Voice: <transcribed text>`
6. Transcribed text is routed as a normal command (e.g. `new`, `send`, or implicit send)
7. Temp audio file is deleted after transcription

### Model Selection Guide

| Model | Size | Speed | Accuracy | Recommended For |
|-------|------|-------|----------|-----------------|
| tiny | 39M | fastest | basic | Quick commands, low-resource servers |
| base | 74M | fast | good | General use (default) |
| small | 244M | moderate | better | Mixed language environments |
| medium | 769M | slow | high | Professional use, accented speech |
| large | 1.5G | slowest | best | Maximum accuracy, multi-language |

### Multi-User Note

Currently a single default language is configured globally. Per-user language preferences are planned as part of the multi-user access control feature.

---

## Adding a New Messaging Backend

To add a new backend:

1. Create `internal/messaging/backends/<name>/backend.go` implementing the `messaging.Backend` interface:

```go
type Backend interface {
    Name() string
    Send(recipient, message string) error
    Subscribe(ctx context.Context, handler func(Message)) error
    Link(deviceName string, onQR func(qrURI string)) error
    SelfID() string
    Close() error
}
```

2. Register it in `internal/messaging/registry.go`
3. Add config fields to `internal/config/config.go`
4. Wire it up in `cmd/datawatch/main.go`
5. **Document it in this file** (`docs/messaging-backends.md`) with full setup and config details
6. Update `docs/backends.md` summary table
7. Update `CHANGELOG.md` under `[Unreleased]`
