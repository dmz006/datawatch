# datawatch Backends

This document lists all supported LLM and messaging backends with their configuration keys.

---

## LLM Backends

| Backend | Name | Config key | Description |
|---|---|---|---|
| claude-code | `claude-code` | auto-enabled | Anthropic Claude Code CLI (default; supports interactive input) |
| aider | `aider` | `aider` | aider (https://aider.chat) |
| goose | `goose` | `goose` | Block's goose agent (https://github.com/block/goose) |
| Gemini CLI | `gemini` | `gemini` | Google Gemini CLI |
| opencode | `opencode` | `opencode` | opencode (https://github.com/sst/opencode) |
| opencode ACP | `opencode-acp` | `opencode` | opencode via HTTP/SSE API; supports interactive input and channel replies |
| Ollama | `ollama` | `ollama` | Local models via Ollama (no API key required) |
| OpenWebUI | `openwebui` | `openwebui` | OpenAI-compatible API via OpenWebUI |
| shell | `shell` | `shell_backend` | Custom shell script |

See [docs/llm-backends.md](llm-backends.md) for full setup instructions for each backend.

### Example config

```yaml
aider:
  enabled: true
  binary: aider           # path to aider binary

goose:
  enabled: true
  binary: goose

gemini:
  enabled: true
  binary: gemini

opencode:
  enabled: true
  binary: opencode

shell_backend:
  enabled: true
  script_path: ~/scripts/my-ai.sh   # receives $1=task $2=projectDir

session:
  llm_backend: aider      # which backend to use
```

---

## Messaging Backends

| Backend | Name | Config key | Direction | Description |
|---|---|---|---|---|
| Signal | `signal` | `signal` | bidirectional | Signal messenger via signal-cli |
| Telegram | `telegram` | `telegram` | bidirectional | Telegram bot |
| Matrix | `matrix` | `matrix` | bidirectional | Matrix/Element rooms |
| Discord | `discord` | `discord` | bidirectional | Discord bot in a server channel |
| Slack | `slack` | `slack` | bidirectional | Slack bot in a workspace channel |
| Twilio SMS | `twilio` | `twilio` | bidirectional | SMS via Twilio |
| GitHub webhooks | `github` | `github_webhook` | inbound | GitHub issue/PR/workflow events |
| Generic webhook | `webhook` | `webhook` | inbound | HTTP POST JSON tasks |
| ntfy | `ntfy` | `ntfy` | outbound | Push notifications via ntfy.sh |
| Email (SMTP) | `email` | `email` | outbound | Email notifications |

See [docs/messaging-backends.md](messaging-backends.md) for full setup instructions for each backend.

### Signal

Requires signal-cli and a linked device. See the main README for setup.

```yaml
signal:
  account_number: +12125551234
  group_id: <base64-group-id>
  config_dir: ~/.local/share/signal-cli
  device_name: my-server
```

### Telegram

Create a bot via @BotFather, then add it to your group.

```yaml
telegram:
  enabled: true
  token: "123456:ABCdefGHIjklMNOpqrsTUVwxyz"
  chat_id: -1001234567890    # negative for groups
```

### Matrix

Requires a Matrix homeserver account with an access token.

```yaml
matrix:
  enabled: true
  homeserver: https://matrix.org
  user_id: "@mybot:matrix.org"
  access_token: "syt_..."
  room_id: "!roomid:matrix.org"
```

### GitHub Webhooks

Point your GitHub repo webhook at `http://your-server:9001/webhook`.
Set the same `secret` in your repo and config.

```yaml
github_webhook:
  enabled: true
  addr: :9001
  secret: "mysecret"
```

Supported events: `issue_comment`, `pull_request_review_comment`, `workflow_dispatch` (reads `inputs.task`).

### Generic Webhook

POST JSON to `http://your-server:9002/task`:

```json
{"task": "write unit tests", "project_dir": "/opt/myapp"}
```

```yaml
webhook:
  enabled: true
  addr: :9002
  token: "mytoken"    # optional bearer token
```

### ntfy

Send-only push notifications to your phone via ntfy.sh or self-hosted ntfy.

```yaml
ntfy:
  enabled: true
  server_url: https://ntfy.sh
  topic: my-datawatch-notifications
  token: ""    # optional auth token
```

### Email (SMTP)

Send-only email notifications on session state changes.

```yaml
email:
  enabled: true
  host: smtp.gmail.com
  port: 587
  username: me@gmail.com
  password: "app-password"
  from: me@gmail.com
  to: me@gmail.com
```
