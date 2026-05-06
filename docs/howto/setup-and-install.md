# How-to: Setup + install

End-to-end first-time install: download the binary, start the daemon,
configure auth + a backend, smoke-test, find your way to the logs.
After this you have a daemon you can spawn sessions in and a PWA you
can drive it from.

## What it is

A single Go binary (`datawatch`) that runs a daemon, embeds the PWA,
exposes the REST + MCP surface, and manages tmux-backed sessions on
the host. No external services required to run it; optional backends
add capabilities (PostgreSQL for memory, Ollama for embeddings,
Tailscale for agent mesh, etc.).

## Base requirements

- **OS**: Linux (any modern distro), macOS (12+), WSL2.
- **Tools on PATH**: `tmux` (≥ 3.0), `git`, `bash`.
- **Optional**: an LLM CLI (`claude`, `aider`, `goose`, `gemini`, `opencode`),
  `ollama` for local models, `docker` for container workers, `kubectl`
  for k8s clusters, `keepassxc-cli` for KeePass-backed secrets.
- **Disk**: ~500 MB for the binary + per-session log retention.
- **Ports**: 8080 (HTTP / redirect) + 8443 (HTTPS) by default;
  customizable.

## Setup

```sh
# 1. Download the binary for your platform from GitHub Releases.
LATEST=$(curl -sk https://api.github.com/repos/dmz006/datawatch/releases/latest | jq -r .tag_name)
curl -L -o /tmp/datawatch \
  https://github.com/dmz006/datawatch/releases/download/$LATEST/datawatch-linux-amd64
chmod +x /tmp/datawatch
sudo mv /tmp/datawatch /usr/local/bin/datawatch

# 2. First-run init — creates ~/.datawatch/, generates auto-TLS certs,
#    writes a starter datawatch.yaml.
datawatch init
#  → ~/.datawatch/datawatch.yaml created
#    ~/.datawatch/tls/{cert,key}.pem generated (self-signed)
#    bearer token printed once: paste into the PWA on first connect

# 3. Start the daemon.
datawatch start
#  → datawatch listening on http://0.0.0.0:8080 (redirects to TLS port 8443)
#    datawatch listening on https://0.0.0.0:8443

# 4. Smoke-test.
curl -sk https://localhost:8443/api/health
#  → {"status":"ok","version":"...","hostname":"...","encrypted":false,...}
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# Configure a backend (claude-code as example).
datawatch config set llm.backends.claude_code.enabled true
datawatch config set llm.backends.claude_code.path /home/$USER/.local/bin/claude
datawatch reload

# Confirm backend is healthy.
datawatch backends list
#  → claude-code  ENABLED  reachable  models=[claude-sonnet-4-5,...]

# Spawn a smoke session.
SID=$(datawatch sessions start --backend claude-code \
  --task "What model are you?" --project-dir /tmp 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
sleep 5
datawatch sessions tail $SID | head -20
datawatch sessions kill $SID

# Update + restart later (preserves running sessions via pipe-pane re-establish).
datawatch update           # downloads latest if available
datawatch restart          # graceful stop + start
```

### 4b. Happy path — PWA

1. Open `https://localhost:8443` in your browser. Accept the
   self-signed cert (or trust the CA bundle from
   `~/.datawatch/tls/ca.pem`).
2. PWA prompts for the bearer token printed in step 2 above. Paste +
   click **Save & Reconnect**.
3. PWA loads. Bottom nav: Sessions / Automata / Alerts / Observer /
   Settings.
4. Settings → LLM → pick a backend (e.g. claude-code) → fill in the
   config card (binary path, etc.) → **Save**. The status row turns
   green when the daemon can reach the backend.
5. Bottom nav → **Sessions** → **+** FAB → backend dropdown → Task
   "Hello, what model are you?" → **Start**.
6. Watch the session detail open with xterm-streamed output. Confirm
   the LLM answers.
7. (Optional) **Install as PWA**: browser menu → Install Datawatch.
   Adds it to your launcher; runs in its own window.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Download the companion app (link in Settings → About → Mobile app
pointer once published). On first launch, paste the same bearer token
+ daemon URL. Connects over Tailscale or LAN.

### 5b. REST

```sh
TOKEN=$(cat ~/.datawatch/token); BASE=https://localhost:8443

curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/health
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/info
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/reload
```

Full Swagger UI at `/api/docs`; raw OpenAPI at `/api/openapi.yaml`.

### 5c. MCP

After install, point your MCP host at:

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"],
      "env": { "DATAWATCH_TOKEN": "<bearer>" }
    }
  }
}
```

Live tool catalogue at `https://localhost:8443/api/mcp/docs`.

### 5d. Comm channel

Out of the box, datawatch listens on no comm channels. Configure one
(Signal / Telegram / Slack / etc.) — see [`comm-channels.md`](comm-channels.md). After
linking, `health` from any channel returns daemon status.

### 5e. YAML

`~/.datawatch/datawatch.yaml` is the source of truth. Auto-generated
by `datawatch init`; edit + `datawatch reload` (or `restart` for
top-level structural changes).

## Diagram

```
   ┌──────────────────────────────────────┐
   │ datawatch (single Go binary)          │
   │  ├─ HTTP/HTTPS server (PWA + API)     │
   │  ├─ MCP server (stdio)                │
   │  ├─ tmux session manager              │
   │  ├─ session-state engine              │
   │  ├─ memory (SQLite or PostgreSQL)     │
   │  ├─ secrets store                     │
   │  ├─ comm channel adapters             │
   │  └─ observer / stats collector         │
   └──────────────────────────────────────┘
              │           │
              ▼           ▼
      Local sessions  Remote agents (Docker / k8s)
      (cs-* tmux)     joining via Tailscale mesh
```

## Common pitfalls

- **Self-signed cert browser block.** First load shows a security
  warning. Either accept the exception OR install the CA bundle from
  `~/.datawatch/tls/ca.pem` to trust it permanently.
- **Bearer token lost.** Reset with `datawatch token rotate`; the
  PWA will prompt for the new one. Old sessions stay alive.
- **Backend binary not on PATH.** `datawatch backends list` shows
  `unreachable` for misconfigured backends. Set the absolute `path:`
  in the backend config card.
- **`tmux` version too old (< 3.0).** Some pipe-pane features used
  by datawatch require 3.0+. Upgrade via your package manager.
- **Port 8443 in use.** Edit `server.port` in `datawatch.yaml` and
  restart.

## Linked references

- See also: [`chat-and-llm-quickstart.md`](chat-and-llm-quickstart.md) for the most-common
  chat × backend pairings.
- See also: [`daemon-operations.md`](daemon-operations.md) for day-two operator workflow.
- See also: [`comm-channels.md`](comm-channels.md) for messaging-channel setup.
- Architecture: `../architecture-overview.md`.

## Screenshots needed (operator weekend pass)

- [ ] First-run terminal output (`datawatch init` → `datawatch start`)
- [ ] PWA bearer-token paste prompt
- [ ] Settings → LLM card with claude-code configured
- [ ] First spawned smoke session in detail view
- [ ] Browser "Install as PWA" prompt
- [ ] `/api/health` JSON response
