---
docs:
  index: true
  topics: [setup, install, onboarding]
exec_params: []
exec_steps:
  - tool: get_config
    description: Verify the daemon is reachable + read defaults
    args: {}
    read_only: true
  - tool: backends_list
    description: Show which LLM backends are configured / reachable
    args: {}
    read_only: true
  - tool: agent_list
    description: List any pre-existing agent workers
    args: {}
    read_only: true
---
# How-to: Setup + install

End-to-end first-time install, in three stages, each a complete
working state on its own:

1. **Install** — download the binary, start the daemon, smoke-test.
2. **Minimal setup** — one backend (opencode or claude-code), one
   session. No messaging, no LLM registry, no MCP yet.
3. **Full datawatch setup** — the LLM registry (more backends/hardware,
   failover), MCP, messaging channels, mobile, REST.

Stop after stage 2 if that's all you need today.

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
- **GPU (NVIDIA)**: NVIDIA drivers + CUDA, and the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) if you plan to run Ollama inside Docker (`docker-network` routing). Without the toolkit, Docker containers fall back to CPU silently. See [`compute-routing.md`](compute-routing.md) for the install steps.
- **Disk**: ~500 MB for the binary + per-session log retention.
- **Ports**: 8080 (HTTP / redirect) + 8443 (HTTPS) by default;
  customizable.

## Stage 1 — install

```sh
# 1. Download the binary for your platform from GitHub Releases.
LATEST=$(curl -sk https://api.github.com/repos/dmz006/datawatch/releases/latest | jq -r .tag_name)
curl -L -o /tmp/datawatch \
  https://github.com/dmz006/datawatch/releases/download/$LATEST/datawatch-linux-amd64
# /tmp is often mounted noexec, so chmod after moving to the final dest.
sudo mv /tmp/datawatch /usr/local/bin/datawatch
sudo chmod +x /usr/local/bin/datawatch

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

## Stage 2 — minimal setup: one backend, one session (opencode or claude)

The fastest path from a fresh install to a real AI session: no
messaging backend, no LLM registry, no MCP server required yet. Pick
whichever coding CLI you already have on your machine.

```sh
# opencode
datawatch config set llm.backends.opencode.enabled true
datawatch config set llm.backends.opencode.path "$(which opencode)"

# — or — claude-code
datawatch config set llm.backends.claude_code.enabled true
datawatch config set llm.backends.claude_code.path "$(which claude)"

datawatch reload

# Confirm backend is healthy.
datawatch backends list
#  → opencode | claude-code  ENABLED  reachable  models=[...]

# Spawn a smoke session.
SID=$(datawatch sessions start --llm opencode \
  --task "What model are you?" --project-dir /tmp 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
# (swap --llm claude-code if that's the one you configured)
sleep 5
datawatch sessions tail $SID | head -20
datawatch sessions kill $SID
```

Or the same thing from the PWA instead of the CLI:

1. Open `https://localhost:8443` in your browser. Accept the
   self-signed cert (or trust the CA bundle from
   `~/.datawatch/tls/ca.pem`).
2. PWA prompts for the bearer token printed at `datawatch init` time.
   Paste + click **Save & Reconnect**.
3. PWA loads. Bottom nav: Sessions / Automata / Alerts / Observer /
   Settings.
4. Settings → LLM → pick opencode or claude-code → fill in the config
   card (binary path, etc.) → **Save**. The status row turns green
   when the daemon can reach the backend.
5. Bottom nav → **Sessions** → **+** FAB → backend dropdown → Task
   "Hello, what model are you?" → **Start**.
6. Watch the session detail open with xterm-streamed output. Confirm
   the LLM answers.

This is a complete, working datawatch install — everything in Stage 3
is additive, not required.

Later, to update: `datawatch update` (downloads the latest version if
one is available) then `datawatch restart` (graceful stop + start;
preserves running sessions via pipe-pane re-establish).

## Stage 3 — full datawatch setup: LLM registry + MCP

Layer on the rest once the Stage 2 minimal path works.

### LLM registry — more backends/hardware, ordered failover

Stage 2 configured exactly one backend by hand. The **LLM Registry**
maps named entries to an ordered failover list of compute nodes, used
by every consumer (sessions, Council, Automata, `/api/ask`):

```sh
datawatch identity configure   # operator identity / Telos
#  or open the PWA and click the 🤖 robot icon in the header

datawatch llm list
datawatch compute node list
datawatch compute pull-model datawatch-ollama llama3.1:8b
datawatch sessions start --llm ollama --model llama3.1:8b --task "Hello"
```

See [`llm-registry.md`](llm-registry.md) and
[`compute-nodes.md`](compute-nodes.md) for the full model.

### MCP — expose datawatch's 60+ tools to Claude Desktop / Cursor / VS Code

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

### REST

```sh
TOKEN=$(cat ~/.datawatch/token); BASE=https://localhost:8443

curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/health
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/info
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/reload
```

Full Swagger UI at `/api/docs`; raw OpenAPI at `/api/openapi.yaml`.

### Messaging channel (optional)

Out of the box, datawatch listens on no comm channels. Configure one
(Signal / Telegram / Slack / etc.) — see [`comm-channels.md`](comm-channels.md). After
linking, `health` from any channel returns daemon status.

### Mobile (Compose Multiplatform)

Download the companion app (link in Settings → About → Mobile app
pointer once published). On first launch, paste the same bearer token
+ daemon URL. Connects over Tailscale or LAN.

### (Optional) Install as PWA

Browser menu → Install Datawatch. Adds it to your launcher; runs in
its own window.

### YAML

`~/.datawatch/datawatch.yaml` is the source of truth for everything
above. Auto-generated by `datawatch init`; edit + `datawatch reload`
(or `restart` for top-level structural changes).

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

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/daemon-operations](daemon-operations.md)
- [howto/profiles](profiles.md)
- [setup](../setup.md)
- [install](../install.md)
