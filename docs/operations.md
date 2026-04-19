# Operations Guide — datawatch

---

## Configuration Methods

All settings in this guide can be changed through any of these methods:

| Method | Example |
|--------|---------|
| **YAML config** | Edit `~/.datawatch/config.yaml` directly |
| **Interactive wizard** | `datawatch setup signal`, `datawatch setup web`, etc. |
| **Web UI** | Settings tab in the PWA (saves via REST API) |
| **REST API** | `PUT /api/config` with JSON body |
| **Chat commands** | `setup signal`, `setup web` from messaging channels |

Changes to most settings take effect immediately. Some (host, port, TLS) require a daemon restart — the web UI shows a restart hint when needed.

---

## 1. Service Management

### System service (Linux, installed with root)

```bash
# Start the daemon
sudo systemctl start datawatch

# Stop the daemon
sudo systemctl stop datawatch

# Restart the daemon (picks up config changes)
sudo systemctl restart datawatch

# Check daemon status
sudo systemctl status datawatch

# Enable automatic start on boot
sudo systemctl enable datawatch

# Disable automatic start on boot
sudo systemctl disable datawatch

# Follow live logs
journalctl -u datawatch -f

# Show last 100 log lines
journalctl -u datawatch -n 100

# Show logs since last boot
journalctl -u datawatch -b
```

### User service (Linux, no root required)

```bash
# Start the daemon
systemctl --user start datawatch

# Stop the daemon
systemctl --user stop datawatch

# Restart the daemon
systemctl --user restart datawatch

# Check daemon status
systemctl --user status datawatch

# Enable automatic start at login
systemctl --user enable datawatch

# Follow live logs
journalctl --user -u datawatch -f

# Show last 100 log lines
journalctl --user -u datawatch -n 100
```

Note: For user services to start at boot (without login), enable lingering:
```bash
sudo loginctl enable-linger $USER
```

### Direct invocation

```bash
# Start daemon (background, PID written to ~/.datawatch/daemon.pid)
datawatch start

# Stop daemon (sends SIGTERM, removes PID file)
datawatch stop

# Stop daemon and kill all active sessions
datawatch stop --sessions

# Start in foreground (log to stdout, no PID file)
datawatch start --foreground

# Start in foreground with verbose logging
datawatch start --foreground --verbose

# Start with encrypted config (requires --foreground for interactive password)
datawatch --secure start --foreground

# Start with a custom config file
datawatch start --config /path/to/config.yaml
```

**Daemon mode details:**
- Logs are written to `~/.datawatch/daemon.log`
- PID is written to `~/.datawatch/daemon.pid`
- **PID lock:** On startup, datawatch checks `daemon.pid` — if the PID is still running, it refuses to start a second instance. This prevents multiple daemons from competing for the same sessions and Signal connection.
- Use `datawatch stop` to send SIGTERM gracefully
- Daemon mode is incompatible with `--secure` (encrypted config); use `--foreground` instead

---

## 2. Upgrading

### Check for updates

```bash
# Check only — prints latest version and whether an upgrade is available
datawatch update --check

# Download and install the latest version
datawatch update
```

`datawatch update` queries the GitHub releases API and, if a newer version is found, runs `go install github.com/dmz006/datawatch/cmd/datawatch@vX.Y.Z`. The binary is replaced in-place; the running daemon is unaffected until it is restarted.

### In-place upgrade procedure

```bash
# 1. Check what version is running and what's available
datawatch update --check

# 2. Stop the daemon gracefully (preserves tmux sessions)
datawatch stop
# or: sudo systemctl stop datawatch

# 3. Install the new binary
datawatch update

# 4. Restart the daemon
datawatch start
# or: sudo systemctl start datawatch
```

Active tmux sessions survive the daemon restart — they are independent OS processes. Sessions in `running` or `waiting_input` state continue uninterrupted. The daemon re-attaches to them on startup by re-scanning tmux.

### Data compatibility

datawatch data files (`sessions.json`, `schedule.json`, `commands.json`, `filters.json`, `alerts.json`) use a flat JSON schema. Fields are added forwards-compatibly; the daemon ignores unknown fields from older schema versions.

**Encrypted stores (`--secure` mode):** The encryption formats (`DWDAT2\n` XChaCha20-Poly1305 and legacy `DWDAT1\n` AES-256-GCM) are stable. The derived key (Argon2id with salt embedded in config header) is deterministic — the same password always yields the same key across versions. Upgrading does not require re-encrypting data files. Legacy v1 files are read transparently.

### Rolling back

If you need to downgrade:

```bash
# Install a specific version
go install github.com/dmz006/datawatch/cmd/datawatch@v0.3.0

# Restart
datawatch start
```

Data files written by a newer version are generally readable by older versions (unknown fields are dropped). Exception: if a major version bump explicitly changes the schema, the CHANGELOG will note it.

---

## 3. CLI Session Management (without Signal)

All Signal commands are also available directly from the CLI. These commands connect to the local daemon's data directory and do not require Signal connectivity.

```bash
# List all sessions with status (shows name, backend, state)
datawatch session list

# Start a new claude-code session (uses current directory)
datawatch session new "build a REST API in Go"

# Start with a name, explicit directory, and backend
datawatch session new --name "api work" --dir ~/projects/api --backend aider "build a REST API"

# Show recent output from a session
datawatch session status a3f2

# Get the last 50 lines of output
datawatch session tail a3f2 --lines 50

# Send input to a session waiting for a prompt
datawatch session send a3f2 "yes, continue"

# Rename a session at any time
datawatch session rename a3f2 "api refactor"

# Terminate a session
datawatch session kill a3f2

# Kill all running sessions on this host
datawatch session stop-all

# Print the tmux attach command for a session
datawatch session attach a3f2
# Prints: tmux attach -t cs-myhost-a3f2
# Run that command to get a full terminal in the session
```

The `--config` flag works with all session subcommands:

```bash
datawatch session list --config /path/to/config.yaml
```

### Command Library

```bash
# Save a named reusable command
datawatch cmd add approve "yes"
datawatch cmd add deny "no"
datawatch cmd add skip "skip"

# List all saved commands
datawatch cmd list

# Delete a saved command
datawatch cmd delete approve
```

Saved commands are stored at `~/.datawatch/commands.json` and can be referenced in sessions by name.

### Seeding Defaults

```bash
# Pre-populate default commands and output filters
datawatch seed
```

`datawatch seed` writes a curated set of common commands and output filter rules to
`commands.json` and `filters.json`. It is safe to run on an existing installation —
it only adds entries that do not already exist.

---

## 4. Configuration

Full `~/.datawatch/config.yaml` with all fields and example values:

```yaml
# Identifies this machine in Signal messages and session IDs.
# Auto-detected from OS hostname if not set.
hostname: hal9000

# Root directory for sessions.json, logs/, and config.
# Default: ~/.datawatch
data_dir: /home/user/.datawatch

signal:
  # Your Signal phone number in E.164 format.
  account_number: +12125551234

  # Signal group ID in base64 format.
  # Get it from: signal-cli -u +12125551234 listGroups
  group_id: abc123base64groupid==

  # signal-cli data directory containing account keys and identity.
  # Default: ~/.local/share/signal-cli
  config_dir: /home/user/.local/share/signal-cli

  # Name shown in Signal's Settings → Linked Devices list.
  # Default: hostname
  device_name: hal9000

session:
  # Maximum number of concurrent claude-code sessions.
  # Default: 10
  max_sessions: 10

  # Seconds of idle output before declaring a session is waiting for input.
  # Lower values = faster detection; higher values = fewer false positives.
  # Default: 10
  input_idle_timeout: 10

  # Default number of lines returned by tail and status commands.
  # Default: 20
  tail_lines: 20

  # Path to the claude-code binary. Can be an absolute path or a PATH-relative name.
  # Default: claude
  claude_code_bin: /usr/local/bin/claude

  # Active LLM backend. Must match a registered backend name.
  # Available: claude-code, aider, goose, gemini, opencode, shell
  # Default: claude-code
  llm_backend: claude-code

  # Default project directory for sessions started via messaging commands.
  # Sessions started via CLI or PWA use the directory selected at session creation.
  # Default: ~/.datawatch/workspace
  default_project_dir: ~/projects

  # Pass --dangerously-skip-permissions to claude-code, bypassing permission prompts.
  # Useful for headless/unattended sessions. Use with caution.
  # Default: false
  skip_permissions: false

  # Kill all active sessions when the daemon exits (SIGINT/SIGTERM).
  # If false, sessions continue running in tmux after the daemon stops.
  # Default: false
  kill_sessions_on_exit: false

  # Restrict the web UI file browser to this directory tree.
  # Users cannot navigate above this path when choosing a project directory.
  # Leave empty to allow browsing the entire filesystem.
  # Default: "" (no restriction)
  root_path: ~/projects

update:
  # Enable automatic background updates.
  # When enabled, the daemon checks for a new release on the configured schedule
  # and replaces the running binary in-place.
  # Default: false
  enabled: false

  # How often to check for updates. Options: hourly, daily, weekly.
  # Default: daily
  schedule: daily

  # Time of day to perform the update check (24-hour HH:MM).
  # Only used when schedule is "daily" or "weekly".
  # Default: "03:00"
  time_of_day: "03:00"

server:
  # Enable the HTTP/WebSocket server for the PWA.
  # Default: true
  enabled: true

  # Bind address. Use 0.0.0.0 for all interfaces (including Tailscale).
  # Use a specific IP to bind only on that interface.
  # Default: 0.0.0.0
  host: 0.0.0.0

  # HTTP/WebSocket listen port.
  # Default: 8080
  port: 8080

  # Optional bearer token for PWA authentication.
  # Empty string = no authentication (rely on Tailscale for network security).
  # Default: "" (no auth)
  token: ""

  # Optional TLS certificate path.
  # Leave empty for plain HTTP (Tailscale provides encryption at the network layer).
  tls_cert: ""

  # Optional TLS key path.
  tls_key: ""

mcp:
  # Enable MCP server (stdio transport) for Cursor, Claude Desktop, VS Code.
  # Default: true
  enabled: true

  # Enable HTTP/SSE transport for remote AI clients.
  # Default: false
  sse_enabled: false

  # Bind address for the SSE server.
  # Default: 127.0.0.1 (localhost only — set to 0.0.0.0 for remote AI agents)
  sse_host: "127.0.0.1"

  # Listen port for the SSE server.
  # Default: 8081
  sse_port: 8081

  # Bearer token for SSE connections. Empty = no auth.
  # Default: ""
  token: ""

  tls_enabled: false
  tls_auto_generate: true

  # Maximum retries when a per-session MCP channel server fails to connect.
  # The daemon retries with exponential backoff before marking the channel unavailable.
  # Default: 3
  max_retries: 3

dns_channel:
  # Enable the DNS covert channel backend.
  # Default: false
  enabled: false

  # Operating mode: "server" (authoritative DNS) or "client" (query via resolver).
  mode: server

  # Domain name for DNS queries. Must match your DNS zone delegation.
  domain: ctl.example.com

  # Server: bind address for the DNS server.
  # Default: ":53"
  listen: ":15353"

  # Client: upstream resolver address (IP:port).
  upstream: "8.8.8.8:53"

  # Shared secret for HMAC-SHA256 query authentication.
  # Must match on both server and client.
  secret: "your-shared-secret-here"

  # Response TTL in seconds. 0 = non-cacheable.
  # Default: 0
  ttl: 0

  # Maximum response size in bytes.
  # Default: 512
  max_response_size: 512

  # Client polling interval (Go duration string).
  # Default: "5s"
  poll_interval: "5s"

  # Per-IP rate limit: max queries per IP per minute.
  # Default: 30. Set to -1 to disable.
  rate_limit: 30

servers:
  # Remote datawatch server connections. Added via: datawatch setup server
  # Use --server <name> flag to target a specific remote server.
  - name: ""         # Short identifier, e.g. "nas" or "workstation"
    url: ""          # Base URL of the remote instance, e.g. http://203.0.113.10:8080
    token: ""        # Bearer token for that remote server
    enabled: true
```

**Note on `--secure` mode:** When started with `datawatch --secure start`, ALL data stores
are encrypted with XChaCha20-Poly1305: `sessions.json`, `schedule.json`, `commands.json`,
`filters.json`, and `alerts.json` — not just `config.yaml`. The encryption key is derived
from your passphrase using Argon2id with a salt embedded in the encrypted config header.
Set `DATAWATCH_SECURE_PASSWORD` env variable for non-interactive restarts.
See [docs/encryption.md](encryption.md) for full details.

**Minimum viable config** (everything else uses defaults):

```yaml
signal:
  account_number: +12125551234
  group_id: abc123base64groupid==
```

---

## 5. Signal Account Management

### Find your group ID

```bash
# List all joined Signal groups
signal-cli -u +12125551234 listGroups
# Output:
# Id: abc123base64groupid==  Name: AI Control  Active: true  Members: +12125551234, +19175559876
```

### List linked devices

```bash
signal-cli -u +12125551234 listDevices
# Output:
# Device 1 (primary): iPhone, last seen: 2026-03-24
# Device 2: hal9000, last seen: 2026-03-25
# Device 3: nas, last seen: 2026-03-25
```

### Remove a linked device

```bash
signal-cli -u +12125551234 removeDevice --deviceId 3
```

### Re-link a device (after removal or key rotation)

```bash
# Interactive re-link with QR in terminal
datawatch link

# Or using signal-cli directly
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

### Unregister from Signal entirely

```bash
signal-cli -u +12125551234 unregister
```

---

## 6. Backup and Recovery

### What to back up

| Path | Contents | Importance |
|---|---|---|
| `~/.datawatch/config.yaml` | All daemon configuration | Critical — required to restart |
| `~/.datawatch/sessions.json` | Session state and history | Important — lose this and running sessions can't be resumed |
| `~/.local/share/signal-cli/` | Signal account keys and identity | Critical — lose this and you must re-link from scratch |
| `~/.datawatch/logs/` | Session output logs | Nice to have — historical output |

### Backup command

```bash
tar czf datawatch-backup-$(date +%Y%m%d).tar.gz \
  ~/.datawatch/ \
  ~/.local/share/signal-cli/
```

### Restore on a new machine

1. Install dependencies: `signal-cli`, Java 17+, `tmux`, `claude` (claude-code CLI), `datawatch`

2. Restore the backup:
   ```bash
   tar xzf datawatch-backup-20260325.tar.gz -C ~/
   ```

3. Verify the restored config points to the correct paths:
   ```bash
   cat ~/.datawatch/config.yaml
   ```

4. Start the daemon:
   ```bash
   datawatch start
   ```

5. Verify it connects to Signal:
   ```bash
   journalctl --user -u datawatch -f
   # Look for: "subscribed to Signal messages"
   ```

Note: Sessions that were `running` on the old machine will be marked `failed` on resume because their tmux sessions no longer exist. This is expected.

---

## 7. Troubleshooting

### signal-cli not found in PATH

**Symptom:** `datawatch start` fails with `signal-cli: executable file not found in $PATH`

**Fix:**
```bash
# Check if signal-cli is installed
which signal-cli

# If not found, install it:
wget https://github.com/AsamK/signal-cli/releases/latest/download/signal-cli.tar.gz
tar xf signal-cli.tar.gz
sudo mv signal-cli-*/bin/signal-cli /usr/local/bin/
sudo mv signal-cli-*/lib/ /usr/local/lib/signal-cli/

# For systemd services, the PATH may differ from your shell PATH
# Add to the service override:
sudo systemctl edit datawatch
# Add:
# [Service]
# Environment=PATH=/usr/local/bin:/usr/bin:/bin
```

### Java not installed or wrong version

**Symptom:** signal-cli starts but immediately exits, or shows `UnsupportedClassVersionError`

**Fix:**
```bash
# Check Java version
java -version
# Need: java version "17" or higher

# Install Java 17+ (Ubuntu/Debian)
sudo apt-get install openjdk-17-jre-headless

# Install Java 17+ (Fedora/RHEL)
sudo dnf install java-17-openjdk

# Install Java 17+ (macOS)
brew install openjdk@17

# Set JAVA_HOME if multiple versions installed
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64
```

### QR code appears but scanning fails

**Symptom:** QR is displayed, Signal scanner reads it, but linking fails or times out

**Possible causes and fixes:**

1. **signal-cli config directory permissions:**
   ```bash
   ls -la ~/.local/share/signal-cli/
   # Should be owned by your user with 700 permissions
   chmod 700 ~/.local/share/signal-cli/
   ```

2. **Clock skew:** Signal is sensitive to time drift. Check your system clock:
   ```bash
   timedatectl status
   # Ensure NTP is synchronized
   sudo timedatectl set-ntp true
   ```

3. **QR code expired:** The sgnl:// link expires after a few minutes. Run `datawatch link` again and scan immediately.

4. **Wrong Signal account:** Ensure the account number in `config.yaml` matches the Signal account you're scanning with.

### Sessions not resuming after restart (tmux sessions gone)

**Symptom:** After `systemctl restart datawatch`, all sessions show as `failed`

**Explanation:** This is expected behavior. When the daemon restarts, it checks whether the tmux session for each `running`/`waiting_input` session still exists. If the machine was rebooted or tmux was killed, those sessions are gone and are marked `failed`.

**Prevention:**
- Use tmux server persistence plugins (e.g. `tmux-resurrect`) to restore tmux sessions across reboots
- Only restart the daemon, not the whole machine, to preserve running sessions
- For the daemon itself: `systemctl restart datawatch` preserves tmux sessions; only a machine reboot or `tmux kill-server` loses them

### PWA can't connect (port, firewall, Tailscale)

**Symptom:** Browser shows "ERR_CONNECTION_REFUSED" or WebSocket fails to connect

**Diagnostic steps:**

```bash
# 1. Verify the daemon is running and listening
systemctl --user status datawatch
curl http://localhost:8080/api/health

# 2. Check the port is open
ss -tlnp | grep 8080

# 3. Find your Tailscale IP
tailscale ip -4

# 4. Test connectivity from the phone (via Tailscale)
# On the server:
curl http://$(tailscale ip -4):8080/api/health

# 5. Check for firewall rules blocking the port
sudo ufw status
# If firewall is active, allow the port on the Tailscale interface:
sudo ufw allow in on tailscale0 to any port 8080

# 6. Verify Tailscale is connected on both devices
tailscale status
```

### claude-code not found (PATH in service environment)

**Symptom:** Session starts, tmux session is created, but immediately transitions to `failed`. Logs show `claude: command not found`.

**Cause:** systemd services have a restricted PATH. The `claude` binary may be in a location not on the service's PATH (e.g. `~/.local/bin`, `~/.npm/bin`, `/usr/local/bin`).

**Fix:**

Option 1 — Set the full path in config:
```yaml
session:
  claude_code_bin: /home/user/.local/bin/claude
```

Option 2 — Add the PATH to the systemd service:
```bash
systemctl --user edit datawatch
# Add:
# [Service]
# Environment=PATH=/home/user/.local/bin:/usr/local/bin:/usr/bin:/bin
```

### Group ID not found / wrong format

**Symptom:** Daemon starts, no errors, but Signal messages are ignored

**Diagnosis:**
```bash
# List all groups your account knows about
signal-cli --config ~/.local/share/signal-cli -u +12125551234 listGroups
```

**Common issues:**
- Group ID in config is missing the trailing `==` (base64 padding) — copy the full ID from `listGroups` output
- Using the wrong account number — the phone number must match the account that joined the group
- The device is not yet linked — run `datawatch link` and scan the QR

### Multiple machines replying (expected behavior explanation)

**Symptom:** You send `list` and receive two or more replies from different machines

**This is expected behavior.** Each machine running `datawatch` in the same Signal group will receive and process `list` commands independently, replying with its own sessions. Each reply is prefixed with `[hostname]` so you know which machine is responding.

To send a command to a specific machine's session, use the hostname-prefixed ID:
```
status myhost-a3f2
send myhost-a3f2: yes
```

Short IDs (`a3f2`) work if only one machine has a session with that ID. If both machines have a session `a3f2`, the command is ambiguous — each machine will act on its own `a3f2` session.

---

## 8. Monitoring

### Health and Readiness (Kubernetes probes)

| Method | How |
|--------|-----|
| **REST API** | `GET /healthz` (liveness), `GET /readyz` (readiness) — both public, no auth |
| **Web UI** | Settings → Monitor tab → Infrastructure card shows bound interfaces |
| **Comm channel** | `stats` command includes session count and uptime |
| **CLI** | `curl http://localhost:8080/healthz` |

```bash
# Liveness probe — returns 200 if the HTTP server is responding
curl http://localhost:8080/healthz
# Response: {"status":"ok"}

# Readiness probe — returns 200 when the daemon is fully operational
curl http://localhost:8080/readyz
# Response: {"status":"ready","active_sessions":3}
# Returns 503 if session store is not loaded or backends are not connected
```

Both endpoints are public (no auth required) and follow the Kubernetes probe convention.

### Legacy health check

```bash
curl http://localhost:8080/api/health
# Response: {"status":"ok","uptime":"1h23m4s","version":"1.1.0"}
```

### Prometheus Metrics

| Method | How |
|--------|-----|
| **REST API** | `GET /metrics` — Prometheus text format, public, no auth |
| **Web UI** | Settings → Monitor tab → all stats cards (CPU, Memory, Disk, GPU, Daemon, RTK) |
| **Comm channel** | `stats` command returns CPU, memory, disk summary |
| **Grafana** | Add Prometheus data source pointing to `http://datawatch-host:8080/metrics` |

```bash
curl http://localhost:8080/metrics
```

Returns Prometheus text format with gauges, counters, and histograms:

| Metric | Type | Description |
|--------|------|-------------|
| `datawatch_sessions_active` | gauge | Active sessions (labels: backend, state) |
| `datawatch_sessions_total` | counter | Total sessions created (labels: backend) |
| `datawatch_cpu_usage_percent` | gauge | CPU usage |
| `datawatch_memory_usage_bytes` | gauge | Memory usage |
| `datawatch_messages_total` | counter | Messages sent/received (labels: channel, direction) |
| `datawatch_uptime_seconds` | gauge | Daemon uptime |

### Daemon info

```bash
curl http://localhost:8080/api/info
# Response: {"hostname":"hal9000","version":"0.1.0","sessions":3,"uptime":"1h23m4s"}
```

### Session list via API

```bash
curl http://localhost:8080/api/sessions | jq '.[] | {id,state,task}'
```

### Session output via API

```bash
curl "http://localhost:8080/api/output?id=a3f2&n=50"
```

### Check log files directly

```bash
# Tail the output log for session a3f2
tail -f ~/.datawatch/logs/myhost-a3f2.log

# Show last 50 lines
tail -n 50 ~/.datawatch/logs/myhost-a3f2.log

# Count sessions in sessions.json
jq 'length' ~/.datawatch/sessions.json

# Show all session states
jq '.[] | {id, state, task}' ~/.datawatch/sessions.json
```

---

## 9. Log Levels

```bash
# Info logging (default) — startup, session state changes, errors
datawatch start

# Debug logging (verbose) — all Signal messages, JSON-RPC traffic, monitor events
datawatch start --verbose
```

When running as a systemd service, adjust the service override to add `--verbose`:

```bash
systemctl --user edit datawatch
```

Add:
```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/datawatch start --verbose --config /home/user/.datawatch/config.yaml
```

Then:
```bash
systemctl --user daemon-reload
systemctl --user restart datawatch
journalctl --user -u datawatch -f
```

---

## 7. Network Security

### Listener Overview

datawatch exposes multiple network services. Each should be configured for the deployment environment.

| Service | Default Bind | Default Port | Auth | TLS | Purpose |
|---------|--------------|--------------|------|-----|---------|
| HTTP/PWA | `0.0.0.0` | 8080 | Optional token | Optional | Web UI, REST API, WebSocket |
| MCP SSE | `127.0.0.1` | 8081 | Optional token | Optional | AI agent tool server |
| DNS Channel | `:53` | 53 | HMAC-SHA256 | N/A (DNS) | Covert command channel |
| GitHub Webhook | `127.0.0.1` | 9001 | HMAC-SHA256 | No | GitHub event receiver |
| Generic Webhook | `127.0.0.1` | 9002 | Optional token | No | HTTP POST task receiver |
| Twilio Webhook | `127.0.0.1` | 9003 | None built-in | No | SMS receiver |
| Per-session MCP | `127.0.0.1` | Random | None | No | Internal claude-code channels |

### Recommended: Localhost + Reverse Proxy

For production deployments, bind all services to `127.0.0.1` and use a reverse proxy (nginx, caddy, Tailscale) for external access:

```yaml
server:
  host: "127.0.0.1"    # PWA only via reverse proxy or Tailscale
  port: 8080
  token: "strong-random-token-here"

mcp:
  sse_host: "127.0.0.1"  # AI agents on same machine
  sse_port: 8081
  token: "mcp-auth-token"
```

### DNS Channel (Public-Facing)

The DNS service is designed to be the **only publicly exposed** listener. All other services should be bound to localhost or behind a VPN.

**Security features:**
- **HMAC-SHA256 authentication** — every query must include a valid signature
- **Nonce replay protection** — bounded store (10K entries, 5-minute TTL)
- **Per-IP rate limiting** — default 30 queries/IP/minute (configurable)
- **Uniform REFUSED response** — invalid auth, wrong domain, non-TXT queries, and rate-exceeded all return identical `REFUSED` responses (no information leakage)
- **No recursion** — the server does not resolve queries for other domains

**Recommended DNS deployment:**

```yaml
dns_channel:
  enabled: true
  mode: server
  domain: ctl.example.com
  listen: "0.0.0.0:53"       # public-facing
  secret: "$(openssl rand -hex 32)"  # 64-char random secret
  rate_limit: 30              # per-IP per-minute
  max_response_size: 512
```

**Firewall rules (iptables example):**

```bash
# Allow DNS on port 53 (UDP + TCP)
iptables -A INPUT -p udp --dport 53 -j ACCEPT
iptables -A INPUT -p tcp --dport 53 -j ACCEPT

# Block all other datawatch ports from external access
iptables -A INPUT -p tcp --dport 8080 -s 127.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 8081 -s 127.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

**With Tailscale (recommended):**

```bash
# Bind HTTP to Tailscale interface only
server:
  host: "100.x.y.z"   # Your Tailscale IP
  port: 8080
  token: "your-token"

# DNS on public interface
dns_channel:
  listen: "0.0.0.0:53"
```

### Webhook Security

Webhooks default to `127.0.0.1` and require a reverse proxy or tunnel for external access:

```yaml
# GitHub: always set a webhook secret
github_webhook:
  addr: "127.0.0.1:9001"
  secret: "github-webhook-secret"

# Generic webhook: set a bearer token
webhook:
  addr: "127.0.0.1:9002"
  token: "webhook-bearer-token"

# Twilio: use behind a reverse proxy with IP allowlisting
twilio:
  webhook_addr: "127.0.0.1:9003"
```

For external access, use a reverse proxy or `ngrok`/`cloudflared` tunnel.

### PWA Install (Add to Home Screen)

> **Full PWA setup guide:** See [pwa-setup.md](pwa-setup.md) for complete instructions including TLS configuration, CA certificate installation, and standalone mode checklist.

Datawatch is a Progressive Web App. When installed to your home screen, it runs in
standalone mode (no browser address bar, full-screen app experience).

**Requirements for standalone mode:**
- **HTTPS** — Chrome requires HTTPS for service worker registration (except localhost).
  Without a service worker, "Add to Home Screen" creates a browser shortcut, not a standalone app.
- **PNG icons** — Chrome Android requires PNG icons (192x192 and 512x512) in the manifest.
  SVG-only manifests may fall back to browser mode.

**To enable standalone PWA on your network:**

1. Enable TLS in datawatch:
   ```yaml
   server:
     tls: true
     tls_auto_generate: true
   ```
2. Navigate to `https://your-host:8080` on your phone
3. Accept the self-signed certificate warning
4. Tap the browser menu (three dots) → "Add to Home Screen" or "Install app"
5. The app will launch in standalone mode (no address bar)

**If using Tailscale:** Access via `https://your-host.your-tailnet.ts.net:8080` —
Tailscale provides valid HTTPS certificates automatically.

**Tab navigation in standalone mode:** Use the bottom navigation bar built into
datawatch (Sessions, New, Alerts, Settings) — the browser tab bar is hidden.

---

### Chrome Push Notifications (Android / HTTP)

Chrome on Android blocks notification permissions on non-HTTPS sites. If you see
"Notifications blocked" when trying to enable push notifications, use one of these fixes:

**Option A: Enable TLS in datawatch (recommended)**
```yaml
server:
  tls: true
  tls_auto_generate: true  # auto-generates self-signed cert
```
Or via comm channel: `configure server.tls=true`

Access via `https://your-host:8080` and accept the certificate warning once.

**Option B: Tailscale HTTPS**

If using Tailscale, access via the auto-HTTPS URL:
```
https://your-host.your-tailnet.ts.net:8080
```
Tailscale provides valid certs automatically.

**Option C: Install CA certificate on your device**

When using self-signed TLS (`tls_auto_generate: true`), install the CA cert so your browser trusts it. In the web UI: Settings > Comms > Web Server > **Download CA Certificate (.crt)**. Or visit: `https://your-host:8443/api/cert?format=der`

**Android:**
1. Download the .crt file from the link above
2. Settings → Security & privacy → More security & privacy → Encryption & credentials → Install a certificate → CA certificate
3. Select the downloaded file, confirm install

**iPhone/iPad:**
1. Download the .pem file (use the "PEM format" link or `https://your-host:8443/api/cert`)
2. Settings → General → VPN & Device Management → tap the downloaded profile → Install
3. Settings → General → About → Certificate Trust Settings → enable full trust for the datawatch certificate

**Option D: Chrome flag override (quick workaround, no TLS needed)**

1. On Android Chrome, navigate to `chrome://flags`
2. Search for **Insecure origins treated as secure**
3. Add your datawatch URL: `http://your-host.example.com:8080`
4. Set to **Enabled**, relaunch Chrome
5. Navigate back to datawatch — notification permission prompt will now appear

> **Note:** Options A–C also enable service worker registration for PWA install-to-homescreen.

---

### Prompt and Input Logging

**Important:** Session prompts and user inputs are logged in alert messages, session tracking files, and communication channel notifications. This means any text typed into a session prompt — including responses to AI tool-approval dialogs — is visible in:

- Alert history (`datawatch alerts`)
- Communication channel messages (Signal, Telegram, etc.)
- Session tracking folder (`conversation.md`, `timeline.md`)
- Web UI alert panel

**Never type passwords, API keys, or secrets directly into AI session prompts.** Instead:
- Use environment variables (`export MY_API_KEY=...`)
- Use secrets managers or credential stores
- Configure credentials in config files (encrypted with `--secure`)
- Use `.env` files that are gitignored

This is good security practice for AI coding in general — AI assistants may log, transmit, or include prompt text in API calls.

### TLS Configuration

Both the HTTP server and MCP SSE server support TLS:

```yaml
server:
  tls: true
  tls_cert: "/path/to/cert.pem"    # omit for auto-generated self-signed
  tls_key: "/path/to/key.pem"

mcp:
  tls_enabled: true
  tls_cert: "/path/to/cert.pem"
  tls_key: "/path/to/key.pem"
```

When `tls_auto_generate` is true (default), self-signed certificates are generated in `{data_dir}/tls/` if no cert/key paths are provided.

### Encryption at Rest

When `--secure` mode is enabled, all data stores are encrypted with XChaCha20-Poly1305.
Set `DATAWATCH_SECURE_PASSWORD` as an environment variable for automatic restarts.
See [encryption.md](encryption.md) for details.

### Interface Configuration Summary

Every listener's bind address is fully configurable:

| Config Key | Field | Controls |
|------------|-------|----------|
| `server.host` | HTTP/PWA bind interface | `0.0.0.0` / `127.0.0.1` / specific IP |
| `server.port` | HTTP/PWA port | Any port number |
| `mcp.sse_host` | MCP SSE bind interface | `0.0.0.0` / `127.0.0.1` / specific IP |
| `mcp.sse_port` | MCP SSE port | Any port number |
| `dns_channel.listen` | DNS bind address | `host:port` format |
| `github_webhook.addr` | GitHub webhook bind | `host:port` format |
| `webhook.addr` | Generic webhook bind | `host:port` format |
| `twilio.webhook_addr` | Twilio webhook bind | `host:port` format |

---

## 8. Web UI Features

### Alert Format

Alert titles follow the format `hostname: name [id]: event` (e.g.
`ralfthewise: myproject [a1b2]: running → waiting_input`). Toast notifications in the
web UI show a truncated title only (no body text); the full alert with prompt context
is available in the Alerts tab.

For claude-code sessions with an active MCP channel, console-based state detection is
suppressed after the channel connects (`channel_ready: true`). This eliminates the
rapid `running ↔ waiting_input` cycling caused by screen-scraping Claude's animated
terminal. See `docs/claude-channel.md` for details.

### Suppress Toasts for Active Session

**Config key:** `server.suppress_active_toasts` (default: `true`)

When enabled, toast notifications about a session's state changes (e.g. "running → waiting_input") are hidden while you are actively viewing that session's detail page. This reduces notification noise when you're already watching the output.

The setting is stored server-side in the config file and applies across all browsers/devices.

### Auto-Restart on Config Save

**Config key:** `server.auto_restart_on_config` (default: `false`)

When enabled, the daemon automatically restarts after saving configuration changes that require a restart (host, port, TLS, MCP binds). If the config is encrypted and `DATAWATCH_SECURE_PASSWORD` is not set, the restart is skipped with a warning toast.

### ANSI Terminal (xterm.js)

Session output is rendered in a real terminal emulator (xterm.js) with full ANSI color and TUI support. TUI applications like `top`, `htop`, and interactive LLM UIs (claude, opencode) render correctly with cursor positioning, colors, and scrollback.

The terminal auto-fits to the container width and supports 5000-line scrollback. If xterm.js fails to load, output falls back to plain-text rendering.

**Click-to-type:** You can click directly on the terminal area and type — keystrokes are sent to the tmux session in real time. This works for interactive prompts, shell commands, and any input the running process expects. The input bar below the terminal also sends text, but direct typing in the terminal is often faster for short responses.

**Known issue:** The web terminal display occasionally gets out of sync with the actual tmux pane content (blank screen, frozen output, or garbled rendering). If this happens, navigate back to the session list and re-enter the session, or refresh the browser page. This resets the screen capture and xterm.js state. Pull requests to improve terminal sync reliability are welcome.

### Scheduled Prompts

Schedule commands to be sent to sessions at a future time or when a session enters the waiting-for-input state.

**Natural language time:** "in 30 minutes", "at 14:00", "tomorrow at 9am", "next wednesday at 2pm"

View and manage schedules in:
- **Session detail** — inline pending schedules with cancel buttons
- **Sessions page** — badge showing pending schedule count with dropdown
- **Settings** — collapsible paginated list of all scheduled events

### Detection Filters

Prompt, completion, rate-limit, and input-needed patterns are configurable via the `detection` config section. Defaults match the built-in patterns; customize per deployment via config file or API.

### System Statistics

Statistics are collected every 5 seconds and streamed to the web UI in real-time via WebSocket.

**Metrics collected:**

| Category | Metric | Source | Root Required |
|----------|--------|--------|---------------|
| CPU | Load average (1/5/15 min), core count | `/proc/loadavg` | No |
| Memory | Total, used, available, swap | `/proc/meminfo` | No |
| Disk | Total, used, free (data_dir partition) | `syscall.Statfs` | No |
| GPU | Name, temperature, utilization, VRAM | `nvidia-smi` exec | No |
| Network | Total RX/TX bytes (all interfaces) | `/proc/net/dev` | No |
| Daemon | RSS, goroutines, open FDs | `/proc/self/statm`, `/proc/self/fd` | No |
| Sessions | Active/total count | Session store | No |
| Per-session | Memory (RSS + children), uptime | `/proc/<pid>/statm`, `pgrep` | No |
| Tmux | Session count, orphaned sessions | `tmux list-sessions` | No |
| Uptime | Daemon uptime | Internal timer | No |
| Interfaces | Bound network interfaces | Config | No |

**API endpoints:**
- `GET /api/stats` — latest snapshot (all metrics above)
- `GET /api/stats?history=N` — last N minutes of time-series data (ring buffer, 720 entries max)
- `POST /api/stats/kill-orphans` — kill orphaned tmux sessions
- `WS stats` message — real-time broadcast on every collection cycle

**Web UI:** Settings → System Statistics shows a live dashboard with auto-updating cards.
No manual refresh needed — data streams via WebSocket every 5 seconds.

**Messaging:** `stats` command returns a text summary (CPU, memory, disk, sessions, GPU).

**RTK Token Savings:** When `rtk.enabled: true` and RTK is installed, the stats dashboard
includes token savings metrics: total commands compressed, total tokens saved, and average
savings percentage. RTK auto-initializes hooks on supported backends (claude-code, gemini,
aider) if `rtk.auto_init: true`. See `rtk` section in `docs/config-reference.yaml`.

**Future:** Per-session network/CPU via eBPF (see `docs/plans/2026-03-30-ebpf-stats.md`).

### Rate-Limit Auto-Recovery

When a session hits a rate limit, datawatch automatically:
1. Detects the rate limit via console output patterns
2. Selects the "wait" option in Claude's rate limit menu
3. Creates a **persisted scheduled command** to resume after the limit resets
4. The schedule survives daemon restarts (stored in `schedule.json`)
5. At the scheduled time, sends a resume message with context from `PAUSED.md`

This is a key differentiator — no other tool recovers from rate limits automatically.

### Backend Profiles and Fallback Chains

Named profiles allow different accounts/API keys for the same backend.

| Method | How |
|--------|-----|
| **YAML** | `profiles:` section + `session.fallback_chain:` in `config.yaml` |
| **Web UI** | Settings → General → **Profiles & Fallback** card; New Session → **Profile dropdown** |
| **REST API** | `GET /api/profiles`, `POST /api/profiles`, `DELETE /api/profiles?name=X` |
| **Comm channel** | `configure session.fallback_chain=claude-personal,gemini-backup`; `new claude-personal: <task>` |

```yaml
profiles:
  claude-work:
    backend: claude-code
  claude-personal:
    backend: claude-code
    env:
      ANTHROPIC_API_KEY: "sk-ant-..."

session:
  fallback_chain: ["claude-personal", "gemini-fallback"]
```

When the primary backend hits a rate limit, datawatch auto-switches to the next profile in the chain.

### Voice Input (Whisper Transcription)

Voice messages sent via Telegram or Signal are automatically transcribed to text using
OpenAI Whisper, then processed as normal commands.

| Method | How |
|--------|-----|
| **YAML** | `whisper:` section in `config.yaml` |
| **Web UI** | Settings → General → **Voice Input (Whisper)** card |
| **REST API** | `PUT /api/config` with `{"whisper.enabled": true, "whisper.model": "small"}` |
| **Comm channel** | `configure whisper.enabled=true`, `configure whisper.language=es` |

**Requirements:** Python venv with `openai-whisper`, ffmpeg on PATH.

```yaml
whisper:
  enabled: true
  model: base        # tiny/base/small/medium/large
  language: en       # ISO 639-1 code, or "auto" for detection
  venv_path: .venv   # path to Python venv
```

99 languages supported. Per-user language preferences are planned for the multi-user access control feature.

See [setup.md](setup.md#optional-set-up-voice-input-whisper) for installation and
[messaging-backends.md](messaging-backends.md#voice-input-whisper-transcription) for details.

### Browser Auto-Refresh

When the daemon is updated and restarted, connected browsers automatically reload to
pick up new JavaScript/CSS assets. The daemon version is included in WebSocket `sessions`
messages; if the client detects a version mismatch after reconnecting, it triggers
`location.reload()`.

### Proxy Mode (Multi-Machine Management)

When remote servers are configured, datawatch acts as a proxy relay:

| Feature | How it works |
|---------|-------------|
| **Aggregated list** | `list` command shows sessions from all servers |
| **Auto-routing** | `send a3f2: yes` finds which server owns the session |
| **Explicit routing** | `new: @prod: task` creates on a specific remote |
| **WS relay** | Selecting a remote in web UI proxies all API calls + WS |
| **CLI** | `datawatch --server prod session list` targets a specific remote |

**Setup:** Add remote servers via `datawatch setup server` or edit `config.yaml`:

```yaml
servers:
  - name: prod
    url: http://203.0.113.10:8080
    token: "bearer-token"
    enabled: true
```

**Monitoring:**
- `GET /api/servers` — lists all configured remotes with health status
- `GET /api/sessions/aggregated` — sessions from all remotes, tagged with `server` field
- Web UI server picker (Settings → Comms → Servers) switches between instances

**Troubleshooting:**
- If a remote is unreachable, `list` skips it (5s timeout per server)
- Session discovery cache refreshes every 30 seconds
- Check remote health: `curl http://remote:8080/healthz`
- Verify token: proxy injects `Authorization: Bearer <token>` on all forwarded requests

See [setup.md](setup.md#optional-set-up-proxy-mode-multi-machine) for full setup and [architecture.md](architecture.md#proxy-mode-architecture) for flow diagrams.

### Configure via Chat Commands

All comm channel backends (Signal, Telegram, Discord, Slack, etc.) support the `configure` command to change settings without the web UI:

```
configure list                          — show all configurable keys
configure session.console_cols=120      — set a value
configure session.llm_backend=aider     — change LLM backend
configure rtk.enabled=true              — enable RTK
configure whisper.enabled=true          — enable voice input
```

Changes are applied via the REST API (`PUT /api/config`) and saved to `config.yaml`. Some settings (host, port, TLS) require a daemon restart.

### Test Message Endpoint

`POST /api/test/message` simulates an incoming comm channel message through the router. This enables testing all chat commands without needing actual Signal/Telegram connections:

```bash
# Test help command
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"help"}'

# Test session creation
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"new: echo hello"}'

# Test configure
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"configure list"}'
```

Returns `{"input":"...","responses":[...],"count":N}` with the exact responses the router would send to the messaging backend.

Supported commands: `help`, `list`, `status <id>`, `tail <id>`, `send <id>: <msg>`, `kill <id>`, `new: <task>`, `version`, `stats`, `alerts`, `configure`, `schedule`, `setup`, `update check`.

### MCP Channel Cleanup

On daemon startup, stale MCP channel registrations (from deleted sessions) are
automatically removed from Claude's MCP config. This prevents the MCP server list from
growing unboundedly over time.

---

## Chat Mode and Output Modes

Sessions can display in three output modes, configurable per-backend:

| Mode | Display | Best for |
|------|---------|----------|
| `terminal` | xterm.js interactive terminal (capture-pane) | Claude Code, OpenCode TUI apps |
| `log` | Formatted log viewer (color-coded status lines) | Headless/script backends |
| `chat` | Rich chat UI (message bubbles, streaming, memory commands) | Ollama, OpenWebUI, OpenCode-ACP |

### Defaults

| Backend | Default output_mode |
|---------|-------------------|
| Ollama | `chat` |
| OpenWebUI | `chat` |
| OpenCode-ACP | `chat` |
| Claude Code | `terminal` |
| OpenCode | `terminal` |
| All others | `terminal` |

### Configure per-backend

**Web UI:** Settings > LLM backend > Output mode dropdown (terminal / log / chat)

**API:**
```bash
curl -X PUT http://localhost:8080/api/config \
  -H 'Content-Type: application/json' \
  -d '{"ollama.output_mode": "chat"}'
```

**Comm channel:** `configure ollama.output_mode=chat`

**Config file:**
```yaml
ollama:
  output_mode: chat
opencode_acp:
  output_mode: chat
```

---

## Prompt Detection and Debounce

Datawatch monitors sessions for idle prompts to detect when an LLM is waiting for user input. Two configurable timers prevent false positives and notification floods:

### `detection.prompt_debounce` (default: 3 seconds)

After a prompt pattern is first detected, datawatch waits this many seconds before transitioning to `waiting_input`. If new output arrives during this window, the timer resets. This prevents false alerts during LLM thinking pauses (e.g., Claude between tool calls).

### `detection.notify_cooldown` (default: 15 seconds)

Minimum time between repeated "needs input" notifications for the same session. Even if the session oscillates between `running` and `waiting_input`, only one notification is sent per cooldown window.

### Configure

```bash
# API
curl -X PUT http://localhost:8080/api/config \
  -d '{"detection.prompt_debounce": 5, "detection.notify_cooldown": 30}'

# Comm channel
configure detection.prompt_debounce=5

# Config file
detection:
  prompt_debounce: 5
  notify_cooldown: 30
```

### Chat-mode sessions skip terminal detection

Sessions with `output_mode: chat` skip tmux capture-pane and idle timeout prompt detection entirely. Chat sessions use their conversation manager for state — the tmux pane is a fallback log, not the interactive surface.

---

## Session Reconnect on Daemon Restart

When the daemon restarts, running sessions in tmux survive but in-memory backend state is lost. Datawatch automatically reconnects:

### How it works

1. On session launch, backend connection state is saved to `backend_state.json` in the session's tracking directory
2. On daemon startup, `ReconnectBackends()` scans all running sessions
3. For each session with saved state:
   - **ACP**: probes HTTP server, re-registers state map, re-subscribes to SSE event stream
   - **Ollama**: restores conversation history into conversation manager
   - **OpenWebUI**: restores conversation history into conversation manager
4. If the tmux session no longer exists, the session is marked complete

### What's preserved

| Backend | Preserved across restart |
|---------|------------------------|
| ACP | HTTP port, session ID, SSE subscription |
| Ollama | Host, model, full conversation history |
| OpenWebUI | URL, model, API key, full conversation history |

### Cleanup

`backend_state.json` is automatically deleted when a session completes, fails, or is killed.

---

## Terminal Performance

### xterm.js load time

The terminal loads session content in ~30-80ms:

- **TailOutput** reads only the last 64KB of the log file (not the entire file)
- **Initial pane_capture** sent synchronously on WebSocket subscribe
- **Pending capture buffer** catches captures that arrive before terminal initializes

### Output batching

Real-time output lines are batched per session at 100ms intervals before broadcasting via WebSocket. This reduces message count from 300+ to ~2-5 per batch, preventing send channel saturation.

### Crash resilience

- `pane_capture` writes wrapped in try/catch with auto-recovery
- ResizeObserver cleaned up on terminal dispose (prevents memory leak)
- Frame rate capped at ~30fps to prevent xterm.js buffer overload
