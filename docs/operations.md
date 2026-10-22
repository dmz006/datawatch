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

# Dedicated tmux server that hosts every session (installed alongside
# datawatch.service by install.sh). It has its own cgroup and Restart=always,
# so sessions survive daemon restarts and are not tied to any login session.
systemctl --user status datawatch-tmux
TMUX_TMPDIR=~/.datawatch/tmux tmux ls    # plain `tmux ls` won't show sessions

# Follow live logs
journalctl --user -u datawatch -f

# Show last 100 log lines
journalctl --user -u datawatch -n 100
```

Note: For user services to start at boot (without login), enable lingering:
```bash
sudo loginctl enable-linger $USER
```

**Keeping scripts away from the live tmux server.** PRD workers, verifiers and guardrails run with `TMUX` and `TMUX_TMPDIR` removed. Scripts and tests must never run `tmux kill-server` or `tmux kill-session -a` except against an explicit `-S` throwaway socket or under `env -u TMUX -u TMUX_TMPDIR`; setting `TMUX_TMPDIR` alone is not enough. `scripts/check-no-unscoped-tmux.sh` enforces this in `make lint`, in CI and at decompose time. See [testing.md](testing.md#tmux-safety-rules-for-tests-and-scripts).

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

Active tmux sessions survive the daemon restart — they are independent OS processes. Sessions in `running` or `waiting_input` state continue uninterrupted, and the daemon re-attaches to them on startup. The exact guarantees, and the cases that do not hold, are in [Restart safety](#restart-safety) below.

### Restart safety

A restart (`systemctl --user restart datawatch`, `datawatch restart`, or a stop and start) does not require waiting for work to finish first, for the tmux-backed sessions that are the default. The table states what is verified and what enforces it.

The first two rows were observed on sandbox daemons during the restart audit, by signalling the daemon and relaunching it. The systemd unit path was not exercised there. The live test is the check to re-run on any daemon before relying on them. The release build has not yet been run through that live test, so the lease and subprocess rows are confirmed by unit tests only until it is.

| What survives a restart | Status | Enforced by |
|---|---|---|
| tmux pane and the LLM process inside it | Verified. The pane and process stay up across the restart. | `TestBL462_DaemonRestartSurvivesLive` (opt-in, below) |
| Session state and output monitoring | Verified. A live session stays `running` or `waiting_input`, and its log keeps growing after the restart (the output pipe is re-attached). | `TestBL462_DaemonRestartSurvivesLive` (opt-in, below) |
| Capacity-ledger lease for a PRD task on a live session | Fixed. A task in `verifying` on a live session has its lease re-adopted at boot, so the host pool counts it. Before the fix, the lease was dropped and the pool undercounted. | Unit tests `TestAdoptRestoresLeaseOverLimitWithoutWaiting`, `TestAdoptIsIdempotent` and `TestCapacity_BootResumeReadoptsLiveTaskLease`. The live test does not check leases. |
| Task completion and verification | Observed. A PRD task that was `verifying` on a live session ran to completion after two daemon restarts in the audit. | Live audit only. The live test does not check task completion. |

**Tasks that were mid-run.** A task that was executing (not yet verifying) when the daemon stopped has no session to re-attach to. It is reset at boot and re-run, and it holds no lease to restore. This is the one case where a restart interrupts work: that task starts again from the beginning.

**Subprocess sessions** (`subprocess: true`, no tmux pane). A subprocess session records its process ID and exit status in its session directory. At boot, the daemon re-attaches to a subprocess that is still running and keeps the session `running` until the process exits, then records the exit status. If the process ended while the daemon was down, the recorded status is used. The session is marked `failed` only when the process is gone and no exit status was recorded. This restart behaviour is fixed but not yet confirmed by the live test. A systemd stop or restart kills the daemon's child processes too (the unit's default `KillMode=control-group`), so a subprocess survives only a restart that signals the daemon process alone.

**Encrypted sessions** (`--secure`). Not covered by this guarantee. At boot the daemon does not re-attach the output pipe for encrypted sessions, so their output may stop after a restart. After restarting a `--secure` daemon, check that each session that matters is still producing output, and restart any session whose output has stopped.

**Long task text and no-override restarts.** Restarting a session is no longer limited by how long its task text is. The claude-code backend, and the aider and goose-prompt one-shot paths, deliver the task through a scratch file that the typed command reads, so the full text is not typed inline. A no-override restart of a resumed conversation therefore no longer resends the original task. If a tmux command still fails, the returned and logged error now carries tmux's own stderr text (for example `command too long`) instead of a bare exit status.

**Confirming on a live daemon.** `TestBL462_DaemonRestartSurvivesLive` in `internal/session/restart_safety_live_test.go` restarts a daemon and checks that a live probe session survives it. It is skipped unless `DW_LIVE_RESTART_TEST=1` is set, so `go test ./...` does not run it. It needs `DW_BASE` set to the daemon under test, and it must run on the daemon's host with the same `TMUX_TMPDIR`. The default restart path is `systemctl --user restart datawatch`. Use `DW_LIVE_RESTART_CMD` only for a sandbox daemon that systemd does not manage. Run it against a sandbox, never against production.

### Daemon restart and in-flight Automata

Automata (PRDs) do **not** silently resume across a daemon restart. The daemon only auto-resumes a PRD that was in `running` state when it stopped, and only if that work still looks fresh: the PRD's newest recorded activity must be within `autonomous.resume_max_age_seconds` (default 3600 — one hour; set `0` to never auto-resume, `-1` for no age limit), and the PRD's project directory must still exist on disk (a removed temp workspace means the work cannot continue in place). A PRD that fails either check is **parked**: it is set to `blocked` with a plain-English reason, an operator alert is raised, and it does **not** run. A PRD still in `planning` state (decompose in flight when the daemon stopped) is reset to `draft` instead so you can re-run planning, and a `failed` PRD is recovered only when all of its failed tasks were killed by the daemon with no recorded result.

**What the startup log says.** Each decision is printed as it happens and summarized at the end:

```
[autonomous] boot-resume: prd=a1b2c3 resumed age=412s reason=last activity 412 seconds ago is within the 3600-second resume limit and its project directory still exists
[autonomous] boot-resume: prd=d4e5f6 parked age=86400s reason=last activity was 86400 seconds ago ...
[autonomous] boot-resume: resumed 1, parked 1
```

**Seeing what the last restart did.** The per-PRD boot-resume report for the current boot — outcome (`resumed` or `parked`), activity age in seconds, and the reason — is on the loop status: `GET /api/autonomous/status` (field `boot_resume`), the same data `datawatch autonomous status` prints, and as the `boot_resume` array on the loop-state JSON. A parked PRD also shows its reason as its blocked reason on its card in the PWA Automata view.

**Resuming a parked PRD by hand.**

1. Read the blocked reason first (PWA Automata card, or the `boot_resume` entry above). It names exactly why the PRD was held.
2. If the reason is a missing project directory but the project still exists at another path, re-point the PRD at the live directory (PRD settings in the PWA detail view) — the gate checks the directory at startup only, but the executor also needs it to exist while the work runs.
3. Open the blocked PRD in the PWA and use **Retry** on the stopped task(s) in its stories. Retrying a failed or blocked task is allowed while the PRD is blocked, re-queues it to pending, moves the PRD back to `running`, and the executor picks it up — completed work is kept.
4. From a bare daemon or a shell, kick the executor the same way: `datawatch autonomous prd-run <id>` (`POST /api/autonomous/prds/<id>/run`) once the PRD is back in a runnable state.
5. If the work is stale or no longer wanted, or the project directory is genuinely gone, do not resume: cancel the PRD (PWA or `DELETE /api/autonomous/prds/<id>`) so it stops showing as blocked.

If you routinely see PRDs parked for age, either raise `autonomous.resume_max_age_seconds` (or set it to `-1`) or resume them by hand within the window; if they are parked for the missing-directory reason, the projects were in a temp location that did not survive the restart — give long-running Automata a stable project directory.

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
# (or TMUX_TMPDIR=~/.datawatch/tmux tmux attach -t cs-myhost-a3f2 when
# sessions run on the dedicated datawatch-tmux.service socket)
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

Autonomous Automata state lives as plain JSONL files under the autonomous data directory: `prds.jsonl` (Automata) and, alongside it, `automations.jsonl` (Automations — named groups of Automata with next-step edges). Both are captured automatically by any backup of that directory.

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
- For the daemon itself: `systemctl restart datawatch` preserves tmux sessions; only a machine reboot or `tmux kill-server` loses them. Which restart cases are covered, and which are not, is in [Restart safety](#restart-safety)

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

`?view=summary` returns a compact list (long text fields truncated,
only the newest ended sessions kept); `?states=active` and
`?since=<RFC3339|unix>` narrow it further. See
[api/sessions.md](api/sessions.md#compact-session-list-viewsummary).

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/sessions?view=summary&states=active" | jq '.[] | {id,state,task}'
```

### Payload settings for low-bandwidth clients

These `server.*` settings tune the compact session list, the WebSocket
streams and API compression used by phones and other low-bandwidth
clients. Every one is read from the live config, so a change applies
without a restart (WebSocket defaults apply without a reconnect). The
recommended client flow is in
[api/mobile-surface.md](api/mobile-surface.md#low-bandwidth-client-flow).

| Key | Default | Meaning |
|---|---|---|
| `server.session_summary_terminal_limit` | `50` | Ended (complete / failed / killed) sessions kept by `GET /api/sessions?view=summary`, newest first; `0` = no cap. The unfiltered list is never capped. |
| `server.session_summary_text_chars` | `200` | Characters kept of `task`, `prompt_context`, `last_response` and `last_summary_long` in the summary view and in summary `session_state` frames (minimum 20). |
| `server.ws_session_list_default` | `full` | WebSocket session-list mode for clients that send no `hello`: `full` or `delta`. |
| `server.ws_output_scope_default` | `all` | WebSocket terminal-output mode for clients that send no `output` in `hello`: `all` or `subscribed`. Keep `all` while the web app is in use. |
| `server.api_gzip` | `true` | Gzip `/api/*` JSON responses for clients sending `Accept-Encoding: gzip`. |
| `server.api_gzip_min_bytes` | `1024` | Only bodies larger than this are compressed. |

Each key is reachable through all five configuration methods:

| Key | YAML (`server:` block) | CLI | Web UI (Settings → General → Sessions) | REST | Chat |
|---|---|---|---|---|---|
| `server.session_summary_terminal_limit` | `session_summary_terminal_limit: 50` | `datawatch config set server.session_summary_terminal_limit 50` | *Session summary: ended sessions kept (0 = all)* | `PUT /api/config` `{"server.session_summary_terminal_limit": 50}` | `configure server.session_summary_terminal_limit=50` |
| `server.session_summary_text_chars` | `session_summary_text_chars: 200` | `datawatch config set server.session_summary_text_chars 200` | *Session summary: text length (chars)* | `PUT /api/config` `{"server.session_summary_text_chars": 200}` | `configure server.session_summary_text_chars=200` |
| `server.ws_session_list_default` | `ws_session_list_default: full` | `datawatch config set server.ws_session_list_default delta` | *Live session updates (clients without hello)* | `PUT /api/config` `{"server.ws_session_list_default": "delta"}` | `configure server.ws_session_list_default=delta` |
| `server.ws_output_scope_default` | `ws_output_scope_default: all` | `datawatch config set server.ws_output_scope_default subscribed` | *Live terminal output (clients without hello)* | `PUT /api/config` `{"server.ws_output_scope_default": "subscribed"}` | `configure server.ws_output_scope_default=subscribed` |
| `server.api_gzip` | `api_gzip: true` | `datawatch config set server.api_gzip false` | *Compress API responses (gzip)* | `PUT /api/config` `{"server.api_gzip": false}` | `configure server.api_gzip=false` |
| `server.api_gzip_min_bytes` | `api_gzip_min_bytes: 1024` | `datawatch config set server.api_gzip_min_bytes 4096` | *API compression threshold (bytes)* | `PUT /api/config` `{"server.api_gzip_min_bytes": 4096}` | `configure server.api_gzip_min_bytes=4096` |

MCP agents can use `config_set` with the same keys. `GET /api/info`
lists what the daemon supports in its `capabilities` array (for example
`sessions.view_summary`, `alerts.count`, `ws.hello`, and `api.gzip`
while compression is on).

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

## Audit & Access Logging

Two append-only JSON-lines logs under `data_dir`:

- **`audit.log`** — operator action log: session/config/etc. state
  changes (`start`, `kill`, `send_input`, `configure`, `rollback`,
  `schedule`, ...). Query: `GET /api/audit` or the `audit_query` MCP
  tool.
- **`access.log`** — every HTTP request/response, auth failure, and
  WebSocket connect/disconnect, from all of CLI, REST, and MCP (all
  three are HTTP clients against the same `/api/*` surface behind one
  auth middleware, so this one log covers all three). Query:
  `GET /api/audit/access` or the `audit_access_query` MCP tool, both
  taking `actor`, `action`, `since`, `until`, `limit`.

Neither log ever records the `Authorization` header or a token/nonce
value — only request metadata (method, path, status, remote IP, user
agent) and the resolved principal (`admin`, `session-scoped`,
`peer:<name>`, `proxy:<name>`, or `unauthenticated`).

**Retention**: `audit.retention_days` (default 30; negative = never
prune) applies to both logs, pruned at daemon startup and every 24h.

**SIEM forwarding**: set `audit.cef_mirror_enabled: true` to
additionally write a CEF-formatted line (`audit.log.cef`,
`access.log.cef`) alongside every entry — point your Splunk/QRadar/
ArcSight/Sentinel forwarder at the `.cef` file. The JSON-lines file
stays the one the daemon's own REST/MCP query surface reads.

**Metrics**: `datawatch_access_log_events_total{action,principal_kind}`
(Prometheus, `/metrics`) and `access_log_events_total`/
`access_log_auth_failures_total` on `GET /api/stats` (in-process
counts, reset on restart — the Prometheus counter is the durable one).

Example — find every successful connection and auth failure from a new
IP in the last day:
```bash
curl -s "$DATAWATCH_API_URL/api/audit/access?action=http_access&since=$(date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" | jq -r '.entries[].details.remote_ip' | sort -u
curl -s "$DATAWATCH_API_URL/api/audit/access?action=auth_failure" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" | jq '.entries'
```

**Cross-hop attribution (federation)**: when a request to this daemon
was forwarded by another datawatch daemon acting as a federation peer
— LLM delegation (`ProxyRouter`), `/api/proxy/{name}/...`, or
`/remote/{name}/...` — a `peer:<name>` entry only tells you which peer
presented the request, not who behind that peer actually initiated it.
If the forwarding daemon sent a verified `X-Datawatch-Hop-Chain`
header, the entry also carries `details.origin_actor` (who initiated
it, e.g. `admin` or `session-scoped`, on the forwarding daemon) and the
full `details.hop_chain` (every daemon the request passed through).
The chain is HMAC-signed per hop using each pair of daemons' existing
shared peer bearer token — not new asymmetric keys — and verified
hop-by-hop: each daemon checks only the link signed with the token it
directly shares with its sender, not the whole chain back to the
origin. A chain that fails verification is dropped (never trusted,
never rejected) and logged as `details.hop_chain_invalid: true`; a
peer running an older version that doesn't send a chain at all just
gets the plain `peer:<name>` attribution, same as before this existed.
```bash
curl -s "$DATAWATCH_API_URL/api/audit/access?action=http_access" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" \
  | jq '.entries[] | select(.details.origin_actor != null) | {actor, origin_actor: .details.origin_actor, hop_chain: .details.hop_chain}'
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

### Native ACME / Let's Encrypt (BL397)

For a public deployment with a real hostname, datawatch can obtain and
auto-renew a browser-trusted certificate itself — no external tool
(Caddy, certbot) and no DNS management. The daemon performs the full
ACME HTTP-01 handshake, writes the issued cert alongside the existing
`{data_dir}/tls/` auto-cert location, and renews automatically.

```yaml
acme:
  enabled: true
  endpoint: staging              # staging (default, untrusted certs,
                                  # safe to test against) | production
  domains:
    - datawatch.example.com      # one or more names on ONE cert
  method: http01                 # http01 (no DNS management) | dns01
                                  # (wildcard support, needs a provider token)
  renewal_days: 30               # renew when <= N days of life remain
  retry:
    interval_minutes: 30
    max_consecutive_failures: 5
  apply:
    update_mcp_cert: true        # also point mcp.tls_cert/tls_key at it
    hot_swap: false              # true = most renewals apply with zero
                                  # downtime (see "Cert apply" below)
```

**What `http01` requires**: one A/AAAA record for each domain pointing
at this host, and **inbound TCP/80 reachable from the public internet**
— Let's Encrypt's validator connects to port 80 specifically, which is
not the same as `server.port` (default `8080`; the existing dual-port
model only *redirects* 8080→8443, it doesn't bind port 80). To actually
satisfy HTTP-01 you need either:
- `server.port: 80` directly (root, or `setcap cap_net_bind_service=+ep`
  on the binary if running as an unprivileged user), or
- an external port-forward from 80 to whatever `server.port` is.

**What `dns01` requires** (no port 80 needed — use this for wildcards,
or when port 80 genuinely can't be opened): a zone-scoped DNS provider
API token. Only Cloudflare is wired in this build:

```yaml
acme:
  method: dns01
  dns01:
    provider: cloudflare
    token_secret: "${secret:cf-zone-edit-token}"  # Zone > DNS > Edit on
                                                     # ONE zone — never the
                                                     # account-global key
    zone_id: "..."                                  # reference only
```

Store the token via the secrets manager (`datawatch secret set
cf-zone-edit-token <token>` or the PWA Secrets card) — `token_secret` is
a `${secret:name}` reference, same convention as every other credential
in this codebase. The resolved token is held only in memory; `config.yaml`
always keeps the unresolved reference, even across renewals.

**Workflow**: start with `endpoint: staging` (Let's Encrypt's staging
directory — generous rate limits, but the cert itself isn't browser
trusted) to verify the whole flow works, then flip to
`endpoint: production` for a real cert. Each endpoint is tracked as a
**separate ACME account registration** — switching endpoints re-registers
automatically (same underlying account key, new registration), so no
manual account reset is needed. Note that flipping `endpoint` alone
doesn't force a new order — the existing cert isn't due for renewal yet —
run `datawatch acme renew` explicitly after the flip.

**Cert apply**: by default, applying a new/renewed cert writes the PEM,
updates `server.tls_cert`/`tls_key` (and MCP's, if `apply.update_mcp_cert`),
and restarts the daemon — one short window per issue/renewal (roughly
every 90 days). Set `apply.hot_swap: true` to apply with **zero
downtime** instead: the TLS listener reloads the cert from disk on its
next handshake when the file's mtime changes, no restart. The one
exception is the very first switch to ACME (or the first-ever issue),
where the cert *path* itself is changing — a running listener can't
hot-reload a path it was never watching, so that one restart still
happens regardless of `hot_swap`.

**Other surfaces**: `GET/POST /api/acme/{status,renew,verify}`; MCP tools
`acme_status`/`acme_renew`/`acme_issuer_log`; CLI
`datawatch acme status|renew|verify`; comm verb
`!acme status|renew|verify`; PWA Settings → Web Server card's
certificate-source selector (Self-signed | Custom | Let's Encrypt), with
a validation-method picker when Let's Encrypt is selected.

Full design, the operator-interview decision log, and the real bugs
found during live verification: `docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md`.

### APNs Push Notifications (BL397 Phase 4 / BL335)

The daemon can send push notifications directly to registered iOS
devices via Apple's APNs provider API — closing the gap where device
registration (`POST /api/devices/register`, `kind: apns`) worked but
nothing ever actually sent a push.

```yaml
push:
  apns:
    enabled: true
    key_id: "ABC1234567"        # from the Apple Developer portal
    team_id: "TEAM123456"
    bundle_id: "com.example.YourApp"
    key_secret: "${secret:apns-auth-key}"  # the .p8 key's PEM contents
    # key_path: "/path/to/AuthKey_ABC1234567.p8"  # alternative to key_secret
    sandbox: false               # true for TestFlight / development builds
```

Either `key_secret` (a `${secret:name}` reference holding the raw `.p8`
PEM — the Auth Key is a long-lived credential, treat it the same as any
other secret) or `key_path` (a file on disk) works; `key_secret` takes
precedence if both are set.

**Dispatch**: every new alert automatically fans out to every registered
`kind: apns` device — same trigger point the existing FCM/UnifiedPush
path uses. A device that returns Apple's `410 Unregistered` (app
uninstalled, token rotated) is automatically pruned from the registry.

**Test send**: `POST /api/push/apns/test` (optional `{"device_id":
"..."}` to target one device, omit for all) or
`datawatch push apns-test [--device-id <id>]` — useful for verifying the
key/config is correct without waiting for a real alert.

**Sandbox vs. production**: each device's own `apns_environment`
(`"production"` or `"development"`, sent by the app at
`POST /api/devices/register` and refreshed on every re-registration —
GH#183) decides which host that device's pushes go to:
`api.sandbox.push.apple.com` for `"development"` (TestFlight and Xcode
debug builds), `api.push.apple.com` for `"production"` (App Store
builds). This is per-device because a daemon can have both kinds of
builds registered at once. `push.apns.sandbox: true` only sets the
fallback used for a device that never sent `apns_environment` at all
(devices registered before this field existed) — it is not a daemon-wide
switch once devices report their own environment.

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

### Generic Webhook Image Attachments

`POST /task` on the generic webhook listener (`webhook.addr`, default
`127.0.0.1:9002`) accepts an optional `image_url` field. A
`data:<mime>;base64,...` value always works. A plain local file path is
**disabled by default** — set `webhook.image_dir` to the one directory
you want this feature allowed to read from:

```yaml
webhook:
  image_dir: "/home/you/datawatch-webhook-images"
```

Only paths resolving inside that directory are accepted; anything else
(including `../` traversal attempts) is rejected. Before this existed,
`image_url` accepted **any** path the daemon process could read, with no
restriction at all — and this listener's bearer token is itself optional
(`webhook.token` unset means no auth), so any caller able to reach the
listener could read any file. If you were relying on local-path image
attachments before upgrading, set `webhook.image_dir` to get the feature
back, scoped.

### Outbound Mobile Push Endpoint Validation

`POST /api/push/register` lets a mobile client register an endpoint the
daemon delivers push notifications to. By default the daemon:

- Always refuses loopback, link-local, and cloud-metadata addresses — no
  configuration can allow these.
- Requires `https://` for the registered endpoint.
- Allows private-range addresses (RFC1918, Tailscale CGNAT `100.64.0.0/10`,
  IPv6 ULA) — this is deliberate, since that's where most self-hosted push
  distributors (ntfy, Gotify) actually run.

If your self-hosted push distributor runs over plain `http://` on your
LAN (common for a local ntfy instance with no TLS in front of it), set:

```yaml
push:
  allow_insecure_endpoints: true
```

If you want to additionally lock the daemon down so it can *only* push to
public internet addresses (e.g. a hosted ntfy/UnifiedPush provider, never
anything on your own network), set:

```yaml
push:
  block_private_endpoints: true
```

Both default `false`. If push notifications stop arriving after an
upgrade and you run a self-hosted distributor, check these two settings
first before anything else — see `internal/server/push_ssrf.go`'s header
comment for the full design rationale if you need to dig further.

### Remote PWA Proxy Sandbox Origin

Viewing a federation peer's own PWA (the "PWA" link next to a server in
Settings → Servers, or `GET /remote/{name}/`) is served from a **second
listener**, bound to `server.proxy_sandbox_port` (default `8444`) on the
same host and TLS cert as the main server — a genuinely separate browser
origin from your own dashboard, not just a different path. This exists
because the PWA authenticates every API call with a token read from
`localStorage`, not a cookie: without origin separation, a malicious or
compromised peer's proxied JS could read that token directly out of
`localStorage` and act with your full daemon privileges. A different
origin has its own, naturally separate `localStorage`, so there is
nothing for proxied content to steal regardless of what its JS does.

`GET /remote/{name}/` on the **main** origin now only 301-redirects to
the sandbox origin (old bookmarks/links still work); it never serves
proxied content itself. If `server.proxy_sandbox_port` is `0` or
negative, the sandbox listener is disabled and viewing a remote peer's
PWA becomes unavailable — it does **not** fall back to the old,
same-origin behavior. If a firewall or port conflict prevents the
sandbox port from binding, the daemon logs a warning and starts
normally otherwise; check the startup log for `proxy sandbox listener`
if the "PWA" link stops working after an upgrade.

The sandbox origin's own CSP names your main dashboard's origin in
`frame-ancestors` (computed per-request from whatever hostname you used
to reach it, so it works the same over localhost, a LAN IP, or a
Tailscale name) rather than `'self'` — it is only ever meant to be
embedded by your own daemon, never anyone else. See
`docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §6 for the
full design writeup, including why this needed a second origin rather
than CSP or iframe sandboxing alone.

**Embedded viewer:** the "PWA" link opens the peer's dashboard inline
(a modal, with an "↗ New tab" link and an expand toggle, for the cases
where you want a full separate window instead) rather than only
switching tabs. The embedded dashboard authenticates its own API calls
with a short-lived (1 hour), single-peer-scoped token — minted only
after your own login already succeeded, valid for nothing except
proxying to that one peer, and never the real admin token, so a
compromised peer's JS reading it out of the sandbox origin's storage
can't reach anything beyond that one peer for longer than an hour. One
small, known, cosmetic gap: the embedded view's own staleness check
(`/api/health`) always 401s, since that endpoint is intentionally
unauthenticated on the main origin but the proxy path requires auth for
its whole surface — harmless, just means that one version-mismatch
reload guard doesn't fire for a proxied view. See
`docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §6a for the
full writeup, including four more bugs (a missing CSP `frame-src`, the
token-passing gaps above, and a pre-existing `Accept-Encoding` bug that
silently corrupted every proxied script) found only by actually driving
a real browser against this feature.

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

**Why most config changes don't trigger a restart even when this is on:** the daemon hot-applies the majority of config keys via `applyConfigPatch` — LLM toggles, comm-channel routing, autonomous knobs, memory settings, plugins. Only keys in the `RESTART_FIELDS` set genuinely can't hot-reload (port bind, TLS cert path, MCP server start). When you save one of those with auto-restart on, the daemon restarts; when you save anything else, the change applies live and the toggle stays silent — that's by design, not a bug.

**Subsystem hot-reload (v5.27.2 — operator-asked).** `POST /api/reload?subsystem=<name>` hot-reloads a specific subsystem without restarting the daemon. Registered names: `config` (full hot-reload), `filters` (invalidate FilterEngine regex cache), `memory` (refresh memory adapter caches). Reachable through every parity surface:

```bash
# CLI
datawatch reload                # full config reload (default)
datawatch reload filters        # subsystem-specific

# REST
curl -X POST 'https://localhost:8443/api/reload?subsystem=filters' \
  -H "Authorization: Bearer $TOKEN"

# MCP (any IDE client)
{ "name": "reload", "arguments": { "subsystem": "filters" } }

# Comm channel (Signal / Telegram / etc.)
reload filters
```

### Claude Auto-Accept Disclaimer (v5.27.2)

**Config key:** `session.claude_auto_accept_disclaimer` (default: `false`)

When enabled, datawatch detects claude-code's startup disclaimer / "trust this folder" / "Loading development channels" prompts and auto-accepts them by sending the appropriate confirmation key (Enter for plain confirms; "1\n" for the numbered "Yes, I trust this folder" menu).

The detection mirrors the PRD-automation pattern: the prompt is recognised via `FilterActionDetectPrompt`, marked as `waiting_input`, and 750 ms later the auto-response is sent. Only fires when the active backend is `claude-code` and the flag is on; the existing operator-side trust-prompt review path is unaffected when the flag is off.

```bash
# Enable via CLI
datawatch config set session.claude_auto_accept_disclaimer true
datawatch reload                # apply (the flag is read at runtime so reload picks it up)

# Reachable through every parity surface — REST PUT /api/config,
# MCP config_set, comm `configure session.claude_auto_accept_disclaimer true`,
# PWA Settings → LLM → Auto-accept startup disclaimer toggle.
```

### ANSI Terminal (xterm.js)

Session output is rendered in a real terminal emulator (xterm.js) with full ANSI color and TUI support. TUI applications like `top`, `htop`, and interactive LLM UIs (claude, opencode) render correctly with cursor positioning, colors, and scrollback.

The terminal auto-fits to the container width and supports 5000-line scrollback. If xterm.js fails to load, output falls back to plain-text rendering.

**Click-to-type:** You can click directly on the terminal area and type — keystrokes are sent to the tmux session in real time. This works for interactive prompts, shell commands, and any input the running process expects. The input bar below the terminal also sends text, but direct typing in the terminal is often faster for short responses.

**Anti-clobber typing hold (v8.9.17+):** When an agent or automation sends a message into an active session, the daemon checks whether you are actively typing in the xterm terminal. If TTY activity was detected within the last 30 seconds, the message is held until you pause (up to 90 seconds, then it sends anyway so no message is ever lost). Multiple messages that arrive while you are typing queue up in order — the first message waits for the idle gap, subsequent queued messages flush immediately after without re-waiting. This prevents agent pushes from clobbering mid-keystroke operator input when multiple sessions are running in parallel.

**Line-printing renderer lock (v8.9.20+):** datawatch writes `"tui": "default"` into its scoped claude settings at startup (`<dataDir>/.claude/settings.json`). This pins the claude-code renderer to line-printing mode so tmux scrollback and `pipe-pane` capture continue to work correctly. Without this, Anthropic's server-side `tengu_pewter_brook` flag can switch claude-code sessions into an alternate-screen mode (`\e[?1049h`) that bypasses the tmux scrollback buffer and breaks `pipe-pane` capture.

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

**Finished one-shot sessions:** a one-shot whose command has ended but whose session
never reached complete is closed automatically at startup and every minute (it runs
the normal completion step, closes its pane, removes its task file). Settings → About →
*Finished one-shot sessions* lists any still open and cleans them up on demand
(`GET`/`POST /api/sessions/finished-oneshots`). A one-shot started by hand commits only
after its `--validate` commands pass. Finished session folders are deleted after
`session.session_retention_days` (default 30).

**Two Automata on one tree:** starting an Automaton is refused while another one is
running on the same project tree. Run them one at a time, or give one its own worktree.

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

### MCP transport diagnostics (connection drops, stream health)

Every MCP SSE stream is observed: the daemon logs one `STREAM OPEN` / `STREAM CLOSE`
line pair per stream (duration, write count, bytes, idle time at close, and the first
write/flush error if one occurred), so client disconnects, middlebox kills,
and server-side write failures are distinguishable from the daemon log alone. The
stdio MCP entry points (`datawatch mcp`) log their listener start and stop reason
(client disconnect / error / recovered panic). Failed tool handlers no longer kill
the bridge process — a panic returns an `isError` result and the session keeps
serving.

- **Keepalive:** the SSE stream pings every 10 s so idle read timeouts in proxies or
  clients do not silently kill held-open streams.
- **Timeout profile:** `ReadTimeout` 30 s / `IdleTimeout` 120 s, `WriteTimeout`
  deliberately unset (it is the only stdlib deadline that can kill an in-flight SSE
  stream); the exact profile is printed at serve start.
- **Verbose per-request logging:** set `DATAWATCH_MCP_DEBUG=1` on the daemon or
  bridge process.
- **`DATAWATCH_MCP_DEBUG` and the stream lines are log-channel output only** — the
  stdio MCP server's stdout stays strict JSON-RPC; recovery/diagnostic messages go
  to stderr, so no line outside the protocol can corrupt a client's stream.

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

### `detection.alert_settle` (default: 45 seconds)

A "needs input" alert or push is only sent once the session has stayed in `waiting_input` for this long. If it goes back to `running` first (a pause, a long tool call), the alert is dropped. Set higher to be quieter.

### `detection.alert_repeat` (default: 300 seconds)

An identical "needs input" alert for the same session (same prompt, no new input from you in between) is not sent again within this window. A new prompt, or a prompt after you reply, alerts normally.

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

## Autonomous PRD LLM Configuration

Autonomous PRDs use two separate backends:

| Role | Config | Description |
|------|--------|-------------|
| **Planning** (decompose) | `autonomous.planning_backend` (global) or `decomposition_profile` (per-PRD, takes priority) | Any configured LLM. `opencode`/`claude-code` spawn a full session with codebase tool access; `ollama`/`openwebui` run headless via `/api/ask`. |
| **Execution** (task sessions) | `backend` per-PRD | Spawns interactive coding sessions. Any registered backend: `opencode`, `claude-code`, `goose`, `aider`, etc. |

**Global planning backend** (applies to all PRDs that don't set `decomposition_profile`):

```yaml
autonomous:
  planning_backend: "opencode"    # any configured LLM; opencode/claude-code get full codebase tool access
  planning_effort: "thorough"
```

**Per-PRD settings** — use the PWA Settings modal (Settings button on any PRD in the detail view), the MCP tool, CLI, or REST API. Per-PRD `decomposition_profile` takes priority over the global setting.

MCP:
```
autonomous_prd_set_llm(
  id="<prd-id>",
  backend="opencode",              # task execution (any session backend)
  decomposition_profile="opencode", # planning (overrides global; any LLM)
  effort="thorough"
)
```

CLI:
```bash
datawatch autonomous prd-set-llm <id> --backend opencode --decomposition-profile opencode --effort thorough
```

REST API (also used by Android / iPhone clients):
```bash
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"backend":"opencode","decomposition_profile":"opencode","effort":"thorough"}'
```

**Guardrail judge LLM** — `guardrail_backend` / `guardrail_model` choose
the LLM that judges guardrail/verification results, per Automaton
(`set_llm`) or per task (`set_task_llm`). Resolution: task →
Automaton → global `autonomous.verification_backend` / `verification_model`. These two keys are
presence-aware: leave a key out to keep the current value, send `""`
to clear it. Setting only guardrail keys does not change the worker
`backend` / `effort` / `model` / `decomposition_profile`.

```
# MCP
autonomous_prd_set_llm(id="<prd-id>", guardrail_backend="ollama", guardrail_model="qwen3.8:27b")
autonomous_prd_set_task_llm(id="<prd-id>", task_id="<task-id>", guardrail_backend="")   # clear

# CLI
datawatch autonomous prd-set-llm <id> --guardrail-backend ollama --guardrail-model qwen3.8:27b
datawatch autonomous prd-set-task-llm <id> --task <task-id> --guardrail-model ""

# Comm channel
autonomous set-llm <id> guardrail_backend=ollama guardrail_model=qwen3.8:27b
autonomous set-task-llm <id> <task-id> guardrail_backend=ollama

# REST
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"guardrail_backend":"ollama","guardrail_model":"qwen3.8:27b"}'
```

**Accepted while** — task-level and story-level LLM override calls are
accepted while the Automaton's status matches one of
`autonomous.task_override_statuses` (default: `needs_review`,
`revisions_asked`, `cancelled`, `blocked` — v9.5.5 adds `blocked`:
repointing the next retry's LLM on a blocked Automaton used to be refused with
`status "blocked" is locked`). Change the set with
`datawatch config set autonomous.task_override_statuses blocked,running`
(or REST `PUT /api/config {"autonomous.task_override_statuses":["blocked","running"]}`,
MCP `autonomous_config_set`, or comm channel
`configure autonomous.task_override_statuses=blocked,running`). Unknown
status names are rejected with a 400 that lists the known statuses; an
empty list restores the 4-item default. Automaton-level overrides (per-Automaton
`set_llm`, per-Automaton guardrail / verification LLM) are not affected by this
setting and remain locked to the 3 pre-Run statuses.

**Verification LLM** — `verification_backend` / `verification_model` /
`verification_effort` choose the LLM that verifies a task's work, per
task, per story or per Automaton. Resolution: task → story → Automaton
→ global `autonomous.verification_backend` / `verification_model` /
`verification_effort`; a backend set at any per-unit level bypasses the
`autonomous.verification_backends` pool. Judge sessions (verifier and guardrail) are bounded by `autonomous.verification_timeout_seconds` (default 30m). If a `verifier(session):` or `guardrail(session):` failure names `backend=`, `model=` and `timed out after X`, raise that key rather than treating the model's output as a judgment. Same presence rules: omit to
keep, `""` to clear. The task's `verification` result records
`verifier_backend` / `verifier_model` / `verifier_effort` /
`verifier_source`. Full reference: [`api/autonomous.md`](api/autonomous.md).

```
# MCP
autonomous_prd_set_task_verification_llm(id="<prd-id>", task_id="<task-id>", verification_backend="claude-code", verification_model="opus")
autonomous_prd_set_story_verification_llm(id="<prd-id>", story_id="<story-id>", verification_backend="ollama")
autonomous_prd_set_verification_llm(id="<prd-id>", verification_effort="")   # clear

# CLI
datawatch autonomous prd-set-task-verification-llm <id> --task <task-id> --backend claude-code --model opus
datawatch autonomous prd-set-story-verification-llm <id> --story <story-id> --backend ollama
datawatch autonomous prd-set-verification-llm <id> --effort ""

# Comm channel
autonomous set-task-verification-llm <id> <task-id> backend=claude-code model=opus
autonomous set-story-verification-llm <id> <story-id> backend=ollama
autonomous set-verification-llm <id> effort=

# REST
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verification_llm \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","verification_backend":"claude-code","verification_model":"opus"}'
```

PWA: the task **Edit**, story **LLM** and Automaton **Set LLM** modals
each have a *Verification LLM* row; the task's verification result
shows **Judged by**.

`verification_script`, `files_generated` and `files_touched` are
filled in by the system and are read-only; view them with
`autonomous_prd_get` / `prd-get` / `autonomous get`.

**Typical setup for opencode-based PRDs:**

1. Configure a global planning backend in `config.yaml` (`autonomous.planning_backend: opencode`)
2. Create the PRD — decompose will spawn an opencode session that reads the codebase before writing the plan
3. Or set per-PRD via PWA Settings modal → "Planning backend" dropdown → pick any configured LLM
4. Decompose → Review → Approve → Run

### Plan requirement coverage gate (autonomous.plan_coverage_check)

Before an Automaton is Approved for its first `Run`, datawatch runs a
**deterministic, no-LLM** plan-requirement coverage check and stores the result
on the PRD as the `plan_coverage` record. It is a function of the spec text and
the plan text — the same inputs always produce the same report — so nothing
about it varies from run to run the way a model judgment would.

**What the report contains.** The settings the PRD was pinned to (gate `mode`
and `plan_coverage_min_ratio` at decompose time), the report (the covered /
missing / weakened requirement counts and each unmatched requirement with its
verdict — `covered`, `missing` or `weak`), any **structural findings** (a
dangling `DependsOn`, a dependency cycle, a plan entry with no file provenance,
a step with no verification reason, a bad tracker-ID name), and — when one
exists — the operator `ack` `{by, at, note, fingerprint}`.

**How the `unresolved_file` finding is determined.** File-reference
recognition is deliberately conservative so plan prose is not mistaken for
paths:

- **Bare file names resolve by tree search.** A bare name (no directory
  component, e.g. `executor.go`) passes when a file with that base name exists
  *anywhere* under the project tree — it does not need to sit at a specific
  directory.
- **Prose is not a file.** Tokens without a known file extension, dotted code
  identifiers (`json.Unmarshal`, `GuardrailVerdict.Issues`) and
  slash-separated prose lists (`absent/null/empty`, `task/story/PRD`) are not
  treated as file references and never surface as `unresolved_file` findings.
  A slash-separated token counts as a path only if its last segment carries a
  known file extension or its first segment names an existing directory.
- **Untracked runtime files are ignored.** Runtime artifacts such as
  `output.log` (like other `*.log`, `*.pid`, `*.tmp`, `*.out` files) are not
  flagged when they are absent from the tree — they are usually produced at
  run time, not checked in.
- **Declared outputs count as existing.** A file listed in the `FilesGenerated` of the same task, or of any task in the referencing story or an
  earlier story, satisfies the reference — the plan may name a file a task is
  about to create. (A file a *later* task generates still counts as a miss at
  the earlier reference point.)
- **Real gaps are still reported.** A file path under an existing directory
that does not exist in the tree and is not generated by any same-or-earlier
task or story is still flagged as `unresolved_file`, so genuinely broken file
references keep failing the report.

**A report fails in two ways**, and both stop approval in `block` mode:
the covered/extractable requirement ratio is below `plan_coverage_min_ratio`, or
any structural finding is present (findings fail the report regardless of ratio).

**The three modes** (`autonomous.plan_coverage_check`, default **block**):

| Mode | Behavior on a failing report |
|---|---|
| `block` (default) | Approve is **blocked** until the operator acknowledges that exact failing surface. |
| `warn` | The failing report is stored and shown on the PRD; **approval proceeds**. |
| `off` | No gating; the report is still recorded for visibility. |

**Reading the stored report on a `needs_review` PRD.**

```sh
# CLI — pretty-print the stored plan_coverage record
# (mode, min_ratio, report, findings, ack)
datawatch autonomous prd-coverage <id>

# REST — full PRD detail carries the plan_coverage field
curl -sX GET http://localhost:7433/api/autonomous/prds/<id>?detail=full

# MCP — the full detail exposes the plan_coverage record
autonomous_prd_get(id="<prd-id>", level="full")
```

In the **PWA**, open the PRD; the coverage panel shows the status line —
"Plan covers the spec requirements" (pass), "Plan coverage below threshold —
warning mode" (`warn`), or "Plan coverage below threshold — blocked mode"
(`block`) — next to the covered/missing requirement list and any findings.

**warn vs block in practice.** In `warn` mode you see the same failing report
in the panel, but Approve is enabled and the PRD moves on — the coverage gap is
informational. In `block` mode the same report locks Approve: the panel shows
the failing surface and an **Acknowledge** control instead of letting Approve
through. `off` behaves like `warn` but the report is purely for the record.

**Acknowledging a failing report (the `block`-mode override).** To unblock
Approve without fixing the plan, an operator acknowledges the *exact* failing
surface. Every surface requires a **non-empty note** (the justification) and an
actor identity. The ack is **bound to the stored report's fingerprint** — a
stable hash of the unmatched requirements + findings at the time it was recorded —
so it cannot silently cover a *different* failure surface: if the plan changes
after an ack, the fingerprint no longer matches and a fresh ack is required.

```sh
# CLI — note is required (non-empty); actor defaults to "operator"
datawatch autonomous prd-ack-coverage <id> --actor alice --note "step 3 covers this via the shared helper, acknowledged for this release"

# MCP — id + non-empty note
autonomous_prd_plan_coverage_acknowledge(id="<prd-id>", note="...")
```

```sh
# REST POST
# -H "Authorization: Bearer <token>" -H "Content-Type: application/json"
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/plan_coverage/acknowledge \
  -d '{"actor":"alice","note":"acknowledged for this release"}'
```

In the **PWA** the PRD coverage panel exposes an **Acknowledge** control: type
the note, submit, and the panel switches to "Acknowledged: <actor> — <note>".
Approval is then allowed.

**Why this is deterministic (no LLM).** The coverage check and the structural
findings are pure string-level functions of the spec and the plan: no model
call, no filesystem access, no clock in the verdict. That means the report on a
`needs_review` PRD is exactly reproducible — reading it again, or re-running the
decompose from the same spec + plan, gives the same covered/missing/weakened
counts, the same findings, and the same fingerprint. The only judgment in the
flow is the operator's own acknowledgement, which is why that step requires an
actor and a non-empty note and is pinned to the failing surface.

**Changing the settings.** Both keys are hot-applied (no restart) and reachable
on every parity surface that carries `autonomous.*` config: YAML `config.yaml`,
REST `PUT /api/config`, REST `GET/PUT /api/autonomous/config`, MCP
`autonomous_config_set`, CLI `datawatch config set autonomous.plan_coverage_check=…`
/ `autonomous.plan_coverage_min_ratio=…`, comm `configure
autonomous.plan_coverage_check=…`, and the PWA Settings fields. A PRD is pinned
**at decompose time**: a mode/ratio changed later does not retroactively
re-evaluate an already-`needs_review` PRD's stored report, and does not undo an
ack — the ack is bound to the stored report's fingerprint, not to the live
settings.

### Session idle timeout (autonomous.session_idle_timeout_seconds)

This bounds how long one autonomous worker or verifier session may sit
idle before datawatch gives up on it. The default is 900 seconds (15
minutes).

- **What it watches:** a worker task session or a verifier one-shot
  session whose state is `waiting_input` or `idle` and that has no live
  child process.
- **When it fires:** the session has stayed in that state with no live
  child process for longer than the bound.
- **What you see:**
  - a datawatch alert for the session;
  - the task failure reason `session idle without completion`, naming
    the session id, backend and model;
  - the task is retried through the existing `auto_fix_retries` budget.
    Once that budget is spent, the task stays failed like any other
    failed task.
- **Setting it:** `autonomous.session_idle_timeout_seconds` in
  `config.yaml`, or through the web settings card, the REST
  `autonomous.*` config endpoints, or the MCP `autonomous_config_set`
  tool. A value of `0` means "use the default 900".

```yaml
autonomous:
  session_idle_timeout_seconds: 900
```

---

## Autonomous Plan Review before approval (optional, off by default)

An optional second opinion sits between decompose and your approval:
a one-shot, non-interactive LLM judge looks at the Automaton's plan (spec vs.
decomposed stories/tasks) and records a verdict on the Automaton. **This is
off by default** — with the keys unset, datawatch behaves exactly as
before: no LLM verdict call, no effect on approval.

**Keys** (all under `autonomous:`):

| Key | Default | Meaning |
|-----|---------|---------|
| `plan_review_backend` | `""` (off) | LLM that does the review. Any configured LLM, same rules as `planning_backend`. |
| `plan_review_model` | `""` | Model for the reviewer; empty = the backend's own default. |
| `plan_review_mode` | `""` (= off) | `off` / `warn` / `block`. Unknown values are a config error — a typo cannot silently disable a gate. |
| `smoke_verification_backend` | `""` | LLM that verifies the plan's own smoke task (see below). Empty = no split. |
| `smoke_verification_model` | `""` | Model for that verifier; empty = backend default. |

Set them any surface: `~/.datawatch/config.yaml`, REST `PUT /api/config`
(dot-path keys), MCP `autonomous_config_set`, comm channel
`configure autonomous.plan_review_mode=warn`, CLI
`datawatch config set autonomous.plan_review_mode warn`, or the PWA
Settings (Autonomous section).

### Modes

- **off** (default): the review does not run; nothing is recorded; approval is unaffected.
- **warn**: the review runs after decompose; the verdict (`ok`, findings,
  summary, cost/tokens) is recorded on the Automaton and shown in the PWA, REST
  and MCP; approval is always still allowed — the verdict is advisory.
- **block**: a verdict with `ok: false` refuses `approve`
  (`POST /api/autonomous/prds/{id}/approve`) with the finding categories
  and the reviewer's summary. Approval stays refused until the verdict is
  **replaced** — re-run the review until it passes, or re-decompose. There
  is no force-approve bypass: the point of block mode is that the gate
  works.

### What the reviewer sees and judges

The reviewer gets a **bounded projection** of the plan — the spec plus
story/task titles, task specs, dependencies, kinds and verification
scripts, each field length-capped (it never sees the whole repository).
It judges: faithfulness to the spec, completeness (nothing important
missing), task size/verifiability, dependency ordering, acceptance
criteria that can actually be checked, and in/out-of-scope work. It
reports findings only — it does not re-plan. Each finding carries a
bounded category (`missing`, `weakened`, `too_large`,
`wrong_dependency`, `fragile_verification`, `out_of_scope`) and one
actionable sentence; `high` severity is the approval-blocking grade.
The Automaton content goes to the reviewer inside a neutralized data block so a
spec string cannot inject instructions into the prompt.

### Reviewer outage — record only, never block

If the reviewer call fails or returns an unparseable verdict, the
recorded verdict carries `reviewer_failed: true` (the PWA shows an error-
styled "reviewer failed — outage, not a plan failure" panel). In every
mode, an outage verdict **never blocks approval**: the reviewer's
availability must not wedge your approval gate. Re-run the review once
the backend is back.

### Re-running (works in every mode)

- **PWA**: the re-run button in the Automaton detail's plan-review panel.
- **REST**: `POST /api/autonomous/prds/{id}/plan-review` (body
  `{actor?, note?}`; the response is the updated Automaton with the new
  verdict).
- **MCP**: `autonomous_prd_plan_review`.
- **Comm channel**: `autonomous plan-review <id> [note]`.
- **CLI**: `datawatch autonomous prd-plan-review <id> [note]` (alias `plan-review`).

Only the **latest verdict counts** — a re-run or a re-decompose *replaces*
the previous one, so a blocked Automaton unblocks itself as soon as one review
run passes. In mode `off` a re-run is a no-op that returns the Automaton as
stored. Each verdict also appends a `plan_review` decision row to the Automaton's
decision log (the PWA decision list shows it with the reviewer's
summary) and emits an SSE update on the Automaton channel.

### Reviewer cost and tokens

The verdict records `est_cost_usd`, `tokens_in` / `tokens_out` when the
backend reports spend (one-shot backends usually don't — the fields are
0 in that case); the automation budget policy reads these off the verdict
for Automaton-level cost accounting. The PWA shows the line
"cost: $… · in/out tokens" only when present.

### Smoke / final-gate verifier split

When the plan contains a task **that is itself the smoke verification**,
decompose tags it `kind: "smoke"` (the terminal verification task is
`kind: "final_gate"` and runs the script itself). The judge for that smoke
task can be a *different* LLM — the verifier — set via
`autonomous.smoke_verification_backend` / `..._model`. These two keys are
the **default surface only**: an explicit per-task verifier
(`set-task-verification-llm`, including a deliberate `""` clear) always
wins and survives re-decompose (the bulk apply never clobbers a filled
slot). Titles like "smokescreen" are not smoke tasks — matching is
word-boundary (`smoke`, `final gate`, `final-gate`) plus the explicit
`kind` tag. The PWA shows a kind chip + the resolved verifier LLM on
those tasks.

### Mobile parity

Android/iOS equivalents (verdict panel, re-run button, long
approve-rejection toast, the three settings fields, task kind chips) are
tracked in [dmz006/datawatch-app#266](https://github.com/dmz006/datawatch-app/issues/266).

---

## Autonomous Prompt Injection Protection

When using autonomous PRDs with user-supplied specs (especially via federation peers, comm channels, or operator-created tasks), enable the injection guard to detect and optionally block known prompt injection patterns before they reach the LLM:

```yaml
autonomous:
  injection_guard: true       # log hits on every create/edit
  block_on_injection: true    # reject suspicious input with HTTP 400 (optional)
```

**Data-boundary tags** — regardless of the guard flag, all three LLM call sites (PRD decomposition, task verification, guardrail invocation) always wrap user-supplied content in `<user_data>` XML tags and prefix prompts with a security preamble that instructs the model to treat the content as data, not instructions.

**Federation trust notice** — when a PRD was submitted by a remote federation peer (`autonomous.owner_peer` is set), both the verifier and guardrail prompts include an explicit untrusted-origin notice. This applies automatically; no additional configuration is needed.

**Prometheus metric** — `datawatch_injection_guard_hits_total` counts all scanner hits (raised whether blocking or warn-only) for dashboarding and alerting.

---

## Undeclared Edit Mode (`autonomous.undeclared_edit_mode`)

Every direct-mode (non-worktree) autonomous task is checked after
verification for **undeclared edits**: files the task's own worker
committed in `<pre-task SHA>..HEAD` that were clean before the task started
and are in neither the task's planned `files` nor its generated files.
Pre-existing uncommitted files it swept in belong to `drift_mode` (see
[docs/howto/file-drift-guardrail.md](howto/file-drift-guardrail.md)), and a
file is never reported by both. The check never reverts or modifies
anything; it decides only what *happens* to the task:

| Value | Behavior on a finding |
|---|---|
| `off` | The check does nothing: nothing is recorded or alerted. |
| `warn` (default) | The finding is recorded on the task (`undeclared_edits`, lines summed across retries), appended as an `undeclared_edit_detected` decision, audited, logged and raised as a warn-level alert. The task continues and completes normally. |
| `block` | The task is set to **Blocked** with error `undeclared_edit: <files>` and the Automaton stops walking its tasks. |

Only `off`, `warn` and `block` are accepted. `PUT /api/config` and
`PUT /api/autonomous/config` reject anything else with HTTP 400; a bad value
that is stored another way (hand-edited YAML) resolves to `warn`, never `off`.

### Reading and writing it

| Surface | Read | Write |
|---|---|---|
| **CLI** | `datawatch config get autonomous.undeclared_edit_mode` | `datawatch config set autonomous.undeclared_edit_mode block` |
| **REST** | `GET /api/config` (key `autonomous.undeclared_edit_mode`, `"warn"` when unset); `GET /api/autonomous/config` | `PUT /api/config` with `{"autonomous.undeclared_edit_mode":"block"}`, or `PUT /api/autonomous/config` with `{"undeclared_edit_mode":"block"}` |
| **MCP** | `autonomous_config_get` | `autonomous_config_set` with `{"undeclared_edit_mode":"block"}` |
| **Comm chat** | `configure` (no args) lists the key | `configure autonomous.undeclared_edit_mode=block` |
| **Web (PWA)** | Settings → Autonomous Automata planning → **Undeclared edit mode** | Same select: `off`, `warn` or `block` |
| **YAML** | `autonomous.undeclared_edit_mode:` in `~/.datawatch/config.yaml` | Set it under `autonomous:` |

---

## Notification Routing Across Comm Channels

Every comm channel you enable (Signal, Telegram, Discord, Slack, Matrix,
`imap_mcp`, ntfy, email, generic webhook) receives the **same**
session-lifecycle notification broadcast by default — task started,
waiting for input, completed, or failed. This has always been true;
what's easy to miss is that it applies identically to every channel,
including ones you enabled for a different reason (e.g. `imap_mcp` for
inbound email commands) — if the channel is enabled, it gets the
broadcast too, with no separate per-category toggle.

```yaml
notify_exclude:
  - imap_mcp   # stays enabled for inbound commands, opted out of the broadcast
  - ntfy
```

Found 2026-10-10: an operator was surprised by `imap_mcp` emails that, it
turned out, had always been sent — they'd simply always bounced before a
separate outbound-recipient bug (B110) was fixed, so this was the first
time that channel's share of the existing broadcast actually arrived.
`notify_exclude` (Settings → Comms → Notifications in the PWA) lets you
keep a channel enabled for whatever else you use it for while opting it
out of this specific broadcast — inbound commands on an excluded channel
are unaffected. Checked fresh on every send, so a config change applies
without a restart.

---

## Autonomous External GitHub Actions

Two autonomous-PRD mechanisms call out to GitHub on the operator's own
`gh` credentials, without an explicit per-call confirmation step — worth
knowing both exist before enabling them:

- **Auto-PR on completion** — when a `ProjectProfile`'s `Git.AutoPR` is
  set, a worker session's branch is pushed and a PR opened automatically
  when its session ends (`internal/agents/post_session.go`).
- **`file_upstream_issue` project rule action** (BL406 Phase 4) — a
  configured `autonomous.scan.project_rules` entry with
  `action: file_upstream_issue` files a real issue in the repo named by
  its `upstream_target` (resolved against `autonomous.upstream_repos`)
  whenever that rule's finding fires — at task, story, or PRD
  completion, per the rule's own `granularity`.

Both use the same `gh`-CLI-backed provider (`internal/git`), so they
need `gh auth login` already run on the daemon host — the same
credential the operator uses interactively. Neither mechanism asks for
confirmation before acting; review a project rule's `action` field (or
a profile's `Git.AutoPR`) before enabling it on a PRD whose task specs
you don't fully trust.

---

## Guided Mode and the Per-Task Approval Gate

Setting `guided_mode: true` on a PRD (`POST .../set_guided_mode`, the PRD
Settings checkbox, or the New-Automaton wizard checkbox) pauses the
executor before every task and waits for an explicit approval before it
runs — `POST .../approve_task` (REST), `autonomous_prd_approve_task`
(MCP), `autonomous approve-task <prd-id> <task-id>` (comm), or the
Approve button on a task row showing the ⏸ glyph in the PWA. A gated
task sits in `awaiting_approval` status and is skipped by the run loop
until approved; approving one task does not auto-approve the rest. This
is independent of the existing per-story approval gate
(`per_story_approval`) — a PRD can use either, both, or neither.

Before v9.0.17 this checkbox had no effect at all (tracked as B111): the
set-side plumbing existed across every surface, but nothing in the
executor ever read the flag. It is useful specifically as a checkpoint
against decomposer scope drift — see AGENT.md's "Decomposer Scope-Drift
Rule" (BL384) for when to reach for it, and the `scope_drift` project
rule below for an automated detector covering the same failure mode.

Internally, the flag is now `GuidedModeSource` (`""` off, `operator`
real today, `council`/`guardrail_auto` declared but not yet
differentiated — `council` falls back to `operator` with a logged
reason until BL405 Phase 8 ships a real council-consensus mechanism).
The boolean wire format (`guided_mode: true/false`) is unchanged on
every surface; there is no separate "source" field to set.

### `scope_drift` project rule (opt-in, B112)

A built-in `scope_drift` entry in `autonomous.scan.project_rules` flags
a task spec containing code-creation language ("Implement ...", "Write
code for ...", "Create .go files ...") whenever the parent PRD's own
spec declares doc-only/no-code-changes work. Unlike the
parity-inheritance rule, this is **not** enabled by default — a normal
PRD with no such constraint saying "Implement X" is expected work, not
drift, so defaulting it on would false-positive constantly. See
`docs/config-reference.yaml`'s `scan.project_rules` for the commented-out
example to copy in; run `autonomous_prd_scan` after decomposition and
before approving any task to check it.

---

## PRD Git-Worktree Isolation + Completion PR (BL407, all phases)

By default, a PRD with no explicit `project_dir`/`project_profile`/
`cluster_profile` runs directly in whatever directory the daemon
process itself happens to be in — fine for an operator typing
commands, risky for an autonomous loop about to run unattended against
a real backlog (uncommitted/in-progress work in that directory is
exposed to every task's worker).

Setting `autonomous.worktree_base_repo` to a real git repo path (e.g.
this checkout, for self-build/dogfooding work) changes that default:
every future PRD meeting the condition above gets its own `git
worktree` under `autonomous.worktree_dir` (default
`~/.datawatch/prd-worktrees/<prd-id>/`), checked out on its own branch
(`automaton/<prd-id>-<slug>`) sharing the base repo's `.git` object
store — cheap, and isolated at the filesystem/git level. This is
automatic once configured, not a per-PRD opt-in: an operator who sets
this once gets the safety default for every PRD going forward without
having to remember to ask for it each time. A PRD that already sets an
explicit `project_dir` is left alone (explicit always wins). Task
sessions always commit automatically now, in every mode — the
worktree case is no longer a special case (`session.auto_git_commit` is
deprecated and ignored everywhere) — the whole point of the branch is
to be reviewable, which breaks silently if nothing ever gets committed
onto it.

The worktree is created once, at first `Run()`, and is idempotent
across a daemon restart mid-run. Setting `PRD.Git.AutoPR` to `true` (per-PRD, defaults false — or set
`autonomous.default_auto_pr: true` to start every future PRD with it
already on; see `docs/config-reference.yaml`) makes a
successfully-completed worktree-mode
PRD push its branch and open a real PR automatically — one PR per PRD,
not one per task — then remove the local worktree directory (the
branch + PR are the durable record now, not the local checkout). A
push or PR failure is logged and recorded on the PRD (check its
Decisions) but never fails the run; the worktree is left in place for
inspection whenever that happens, and whenever `AutoPR` is left false.
This is the same category of unconfirmed GitHub action as "Autonomous
External GitHub Actions" above — review before enabling on a PRD whose
task specs you don't fully trust. Set it (or the base-branch override)
on an existing PRD via `POST /api/autonomous/prds/{id}/set_git`, the
`autonomous_prd_set_git` MCP tool, `datawatch autonomous prd-set-git
<id> on|off [--base-branch <name>]`, or the PWA's PRD Settings → Git
section. See `docs/plans/2026-10-10-prd-git-workflow.md`.

A `cluster_profile` PRD gets the same `Git.AutoPR` behavior, but the
mechanics differ: there's no local worktree for the parent to push
from (the worker runs in its own container/pod), so the **worker**
commits and pushes its own branch itself, using the same git token the
parent minted it to clone with, then reports the branch + commit SHA
back on completion. The parent only ever opens the PR from that
reported branch — it never attempts a local push for cluster-dispatched
work. Everything else (review-before-enabling, non-fatal on failure,
Decision-logged) is identical to worktree mode.

As of Phase 4, cluster mode gets the same dedicated per-PRD
`automaton/<prd-id>-<slug>` branch worktree mode always has — the
dispatching worker clones the Project Profile's repo on its own
default branch, then creates the PRD's branch locally (`git checkout
-b`) rather than landing on whatever branch the Project Profile's
`git.branch` names. Two or more tasks on the same cluster-dispatched
PRD requesting that same branch concurrently is expected (one PR per
PRD) and handled by waiting, not failing: a second task's dispatch
waits for the first task's agent to finish before it gets its turn —
shown as the task's normal capacity-wait state — rather than being
rejected outright.

**Out of scope, named so it isn't lost**: everything above covers the
*final* diff — the branch push + PR at PRD completion. It does not
cover interim session output/telemetry durability *during* a run for a
container/cluster-dispatched PRD — that path is still pull-only today
(`forwardSessionToAgent` live-proxies every output request; nothing
durable accumulates on the orchestrating daemon), so an abrupt
container death before a clean session-end loses the transcript with
no forensic record. That's a separate, not-yet-designed F-2
container-hardening concern — see
`docs/plans/2026-10-04-f2-session-worker-isolation.md` §11 for the
operator's push-primary/poll-fallback direction for it.

---

## Session git opt-in (`git` true/false at start, `session.git_default`)

Every session start can opt the session in or out of git activity in
its project directory. This is the mechanism that makes a session's
output reviewable and rewritable: checkpoint tags around the work, a
pre-session snapshot for PRD task sessions, and a gated completion
commit.

### `git: true` — what the session does in the project tree

1. **Repo setup (only if needed).** If the project directory is not yet
   a git repository, one is initialized there (branch `main`, committer
   identity `datawatch <datawatch@localhost>`) so the steps below have
   something to commit to.
2. **Pre-session snapshot (PRD-owned task sessions only).** The tree
   state at launch is committed (message starts `pre-session: <task`),
   so the work the session is about to do can always be told apart from
   what was already there. Ad-hoc one-shot sessions without a PRD owner
   never get a snapshot commit — an operator's in-flight work is not
   something a session should own.
3. **Checkpoint tags.** An annotated tag `datawatch-pre-<session id>` is
   placed before the session runs and `datawatch-post-<session id>`
   after it finishes. These are the handles for review and rollback:
   - `git diff datawatch-pre-<id> datawatch-post-<id>` shows exactly
     what the session changed.
   - `POST /api/sessions/<id>/rollback` (or the `session_rollback` MCP
     tool) resets the tree back to the pre-session state. It refuses
     while uncommitted changes exist unless you pass `{"force": true}`.
4. **Completion commit.** When the session signals completion, its work
   is committed exactly once, message built from the session's
   completion summary (falling back to the task title):
   - A PRD-owned session is gated by the PRD's own verifier before it
     reaches the commit.
   - An ad-hoc one-shot without a PRD owner commits only after its
     declared validation commands all pass; with none declared the
     commit is skipped and the reason is logged.
   - Staging is scoped to the session's write directories — files
     elsewhere in the same tree are never swept in. If the session has
     no write scope, only the files changed during that run are staged,
     and a log warning names them.

### `git: false` — what the session does

Nothing to git, full stop: no `git init`, no snapshot commit, no
checkpoint tags, and no completion commit — on any path (completion
watcher, one-shot reaper, daemon restart) that would otherwise try to
commit a finished session. This is the safe setting for a tree you
don't want the daemon touching.

### What the default is on each surface

| Start surface | Default |
|---|---|
| PRD task session spawned by the autonomous executor | Git on when the PRD has git settings (a non-empty PRD git block — provider/URL/branches/Auto PR — or a project profile); off otherwise |
| Any other start request that omits `git` | `session.git_default` (default **false**) |
| Any start request with an explicit `git: true` / `git: false` | The explicit value always wins |

So PRD task sessions are git-enabled out of the box when the PRD is set
up with git; everything else (MCP, schedules, chat, API, PWA) is
git-less unless it asks for it explicitly or you flip the daemon-wide
default.

### `session.git_default`

The daemon-wide default for the `git` field when a start request does
not set it:

```yaml
session:
  git_default: false   # default: sessions started without git leave the tree alone
```

- `git_default: true` — every session started without an explicit `git`
  field runs under git (repo setup, pre-session snapshot for PRD task
  sessions, checkpoint tags, gated completion commit). Useful when your
  one-shots come from MCP, schedules, or chat and you want the completion
  commit in history.
- `git_default: false` (default) — keeps the historical behaviour:
  sessions started without a `git` field touch git in no way.
- An explicit `git: true` or `git: false` in the start request always
  beats the setting, in both directions.
- Change it like any other setting: `config.yaml`, the PWA settings
  surface, or the REST config API (key `session.git_default`). It is
  read at session start, so the new value applies to sessions started
  after the change — no restart needed.

---

## Project-tree write serialization (one writer per tree)

When more than one Automaton can touch the same working tree, datawatch
serializes writes per tree. This exists because concurrent PRDs running
workers in one tree interleave commits and diffs — the 2026-10-13 incident
saw three Automata do exactly that, producing cross-contaminated changes
and release-version drift in one checkout.

### What gets serialized, and what still runs in parallel

**Serialized:** task runs whose canonical tree resolves to the same key.
In direct mode that means the main checkout, any subdirectory of it, and
every linked worktree of the same git repository — the lock key is the
repo's shared base, so a linked worktree of your main branch serializes
against the main checkout. An ancestor (parent) Automaton that spawned
the running one is exempt — it is waiting on its child, not competing
with it.

**Still in parallel:** Automata on different trees (different checkout
roots, different worktrees, or different non-git directories) — the
mechanism is per-tree, not global. Worktree-mode Automata (per-PRD
`automaton.git.branch`) each key on their own worktree, so they run in
parallel with each other and with other branches. Capacity admission
(`autonomous.capacity.*`) is a separate gate on top.

### What you see

- A task blocked on the tree lock shows a wait reason:
  `tree lock: <tree> held by task <id> (PRD <id>) since <time>`, and it
  resumes when the holder releases (auto-wait works at the task level).
- Starting an Automaton whose tree is already held by a running one is
  refused with `tree busy: PRD <id> is already running on this tree; run
  one PRD per direct tree or use worktree mode`. The Automaton stays in
  an approvable state; re-kick it after the first one completes. Known
gap: there is no PRD-level auto-wait/auto-resume yet — a refused loser
  sits in `approved` until re-kicked (live-confirmed on 2026-10-18, see
  `tests/live-smoke/REPORT.md`).
- If you enabled `autonomous.refuse_dirty_tree_start`, a task whose tree
  has uncommitted changes is refused at admission with the dirty-file
  count and the options (commit or stash, `git clean`, or set the key
  back to `false`).

### Inspecting and unblocking the lock

- The lock file is at `<canonical-tree>/.datawatch/tree.lock`. Its JSON
  names the holder task, the Automaton and the acquisition time — that is
  who holds it and since when.
- It is git-ignored (`.datawatch/`), so a held lock never dirties `git
  status`.
- Normal case: an orphaned lock is reclaimed automatically once its
  holder task is gone or terminal (bounded reclaim attempts; a 30-second
  grace protects a fresh-but-unreadable record from being reclaimed
  mid-write). No operator action needed.
- Force-clear: if you are certain the holder is dead (daemon crash
  mid-write, corrupted task record) and the tree is wedged, delete
  `.datawatch/tree.lock` in the canonical tree; the next task re-creates
  it. Destructive — only when you can confirm no live worker is mid-commit.

### `refuse_dirty_tree_start` — the tradeoff

- **On:** a task in a dirty tree is refused at admission instead of
  starting. Catches the "previous run left cruft behind" state before
  any work begins, at the cost of requiring you to commit/stash/`git
  clean` before re-kicking.
- **Off (default):** tasks start regardless of tree state; the
  foreign-file drift guard after verification remains the after-the-fact
  detector for unexplained changes.
- The lock is advisory: it serializes only datawatch's own task writers.
  A human hand-editing the tree while an Automaton runs is outside the
  lock's scope — if that happens regularly, worktree mode (below) is the
  fix, not a setting.

### Worktree mode as the parallelism escape hatch

Want more than one Automaton on the same repo without waiting? Use
worktree mode (per-PRD `automaton.git.branch`) — each gets its own
worktree and the per-tree lock keys on each worktree's own path, so
there is no cross-worktree contention. The 2026-10-18 live smoke
verified this exactly: two PRDs on distinct trees ran concurrently for
the full duration while a third PRD on the shared tree was correctly
serialized (evidence: `tests/live-smoke/EVIDENCE/20261019-1637/`).

Reproduce the smoke: `bash tests/live-smoke/tree-serialization-smoke.sh`
against an isolated scratch daemon (never your production daemon).

---

## Compute-node queue (max_engines_per_node)

A compute node can only run so many local models at once. Each node has a
capacity pool named `node:<name>`, and its limit is
`autonomous.max_engines_per_node` (default `1`; values below `1` are clamped
to `1`). A node's own `max_concurrent_sessions` still acts as a ceiling when
it is set and smaller.

### What is gated

Only sessions whose model runs on a local engine on that node are gated: the
`pi`, `pi-prompt`, `ollama`, `opencode` and `opencode-acp` backends, when the
model's provider prefix (the text before the first `/`) names the node. Hosted
backends such as `claude-code` are never gated by this setting, and neither is
a model with no provider prefix.

### Waiting for node

When a node's pool is full, a session that needs it does not start. Its task
shows as `waiting_capacity`, and the wait reason names the node, for example:

```
waiting for node alpha: 1/1 engines in use, held by task-42
```

The `GET /api/capacity` response lists each `node:<name>` pool with its
`limit`, `held`, `queue_depth` (how many requests wait on that pool) and
`holder` (the IDs occupying its slots). The wait ends when a slot frees. It
is bounded by `autonomous.capacity_wait_timeout_seconds`, and it needs
capacity admission on (`autonomous.capacity_enabled`, on by default).

### Queue priority

When a slot frees, waiting requests are granted in this order:

1. **Verifier and guardrail sessions** for a task that already has finished
   work. They go first so finished work is checked and released.
2. **Workers**: ordinary task sessions.
3. **Planners**: planning and decomposition sessions. They wait behind workers
   and verifiers, so a PRD waiting on a busy node does not start extra
   planning work past the limit.

Within the same role, the existing order applies: higher PRD priority, then
waiters that have aged past the override window, then model affinity on the
node, then the least-recently-served PRD, then first in, first out.

### Setting it

```bash
datawatch autonomous config-set '{"max_engines_per_node":2}'
```

The same key is available in `config.yaml`, `PUT /api/config`, the MCP
`autonomous_config_set` tool, and the comm `configure` command
(`configure autonomous.max_engines_per_node=2`). The full
key list is in `docs/config-reference.yaml`. Raising the value lets more local
sessions run on each node at once. It does not change how many hosted-backend
sessions run.

---

## Per-LLM concurrency limits and plan tiers

Each LLM registry entry carries a concurrency cap for its `llm:<name>`
pool. Where it comes from, in precedence order:

1. **`max_inflight` > 0** on the LLM entry (explicit operator override);
2. **`plan_tier` + the `autonomous.plan_tier_max_inflight` table** — the
   entry's subscription tier (`unset`, `pro`, `max_5x`, `max_20x`, `api`)
   mapped to a cap (default table: `pro: 1`, `max_5x: 2`, `max_20x: 4`,
   `api: 0` / unlimited);
3. **kind default** — 1 in-flight for subscription kinds (claude-code and
   the other non-local, non-API kinds), unlimited for local
   Ollama/OpenWebUI kinds (those stay governed by the `node:` pools and
   `max_engines_per_node`) and for dollar-metered API kinds.

The table is **operator policy, not a technical limit** — tune it to what
your subscription plans actually allow, in one place:

```yaml
autonomous:
  plan_tier_max_inflight:
    pro: 1        # one concurrent session
    max_5x: 2     # the plan tolerates two
    max_20x: 4
    api: 0        # unlimited (until the dollar-budget phase lands)
```

An explicit `max_inflight` on the entry always beats the tier (a cap set
by mistake on one entry does not leak into every entry of that tier). The
caps count simultaneous autonomous inflight sessions per LLM; they are not
a rate limit, and they are only enforced when `autonomous.capacity_enabled`
is on (default). Setting `max_inflight` and `plan_tier` to the same value
is harmless (same limit, one source). Every value is visible: `GET /api/llms`
and `GET /api/capacity` each carry `effective_max_inflight` plus
`inflight_source` (`explicit` / `plan_tier` / `kind`), so you can see which
rule is actually applying.

### Setting it

| Channel | How |
|---|---|
| YAML | `autonomous.plan_tier_max_inflight: {...}` (see above) |
| PWA | Settings → Compute → LLM edit: *Plan tier* and *Max in flight* fields |
| REST | `PUT /api/llms/{name}` with `plan_tier` and/or `max_inflight`; `GET /api/llms` to read them back |
| MCP | `llm_update` / `llm_add`; `capacity_status` to see the effective limit |
| CLI | `datawatch llm update <name> ...` (tier and explicit cap) |

An unknown tier or a negative cap is a config error: the daemon refuses to
start and names the key.

---

## Parking a task while waiting on an external condition

A worker sitting on a long external event — a CI run, a deploy finishing,
an upstream review — would otherwise pin its capacity slot (and, while it
has work to do, its project-tree lock) for the whole wait. Instead, the
worker emits one marker line and stops:

```
DATAWATCH_AWAIT_EXTERNAL: CI run on the branch
```

What the operator sees after:

- **The slot frees immediately.** The task returns its capacity lease the
  moment the marker is detected, so queued tasks move forward instead of
  waiting behind a parked one. It is then re-admitted through the normal
  bounded capacity wait (`capacity_wait_timeout_seconds`); when the slot
  comes back, the parked session is re-prompted ("[automaton await-external]
  Your await-external cycle is complete …") and continues on the **same
  session** with its full context — no fresh spawn, and no retry or failure
  is counted.
- **The wait is visible, not a stall.** The task's `wait_reason` carries the
  `await_external` sub-reason with the operator's condition (e.g. `await_external:
  CI run on the branch`) on every status surface; the one-shot kill paths
  (waiting-input, idle, stale-log) are suspended while parked, so the task
  is neither killed nor auto-retried for waiting.
- **The tree lock follows the work.** The project-tree write lock is
  released with the slot — *unless the tree has uncommitted changes or
  commits not reachable from a remote-tracking ref*, i.e. the branch is
  ahead of its remote. Unpushed work keeps the lock (a sibling on the same
  tree could still interleave), fully pushed-and-clean releases it so other
  trees' work is not blocked by a parked task.
- **Timeouts and restarts are safe.** Re-admission failure leaves the task
  lease-free in its wait state and hands it back to the normal retry
  machinery. A daemon restart re-adopts a parked task whose session
  survived (or re-queues it if the session is dead), so a parked task never
  leaks a slot across restarts.
- **Every cycle is audited.** `autonomous_idle_wait_release` records which
  capacity pools the task's lease was released from, plus the tree key
  unlocked on the same cycle — a `null` tree key means the tree was held
  for unpushed work.

If the await-external cycle itself fails, the task is handed back to the
normal retry machinery — it is never silently counted as a task failure.

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
