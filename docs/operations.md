# Operations Guide — claude-signal

---

## 1. Service Management

### System service (Linux, installed with root)

```bash
# Start the daemon
sudo systemctl start claude-signal

# Stop the daemon
sudo systemctl stop claude-signal

# Restart the daemon (picks up config changes)
sudo systemctl restart claude-signal

# Check daemon status
sudo systemctl status claude-signal

# Enable automatic start on boot
sudo systemctl enable claude-signal

# Disable automatic start on boot
sudo systemctl disable claude-signal

# Follow live logs
journalctl -u claude-signal -f

# Show last 100 log lines
journalctl -u claude-signal -n 100

# Show logs since last boot
journalctl -u claude-signal -b
```

### User service (Linux, no root required)

```bash
# Start the daemon
systemctl --user start claude-signal

# Stop the daemon
systemctl --user stop claude-signal

# Restart the daemon
systemctl --user restart claude-signal

# Check daemon status
systemctl --user status claude-signal

# Enable automatic start at login
systemctl --user enable claude-signal

# Follow live logs
journalctl --user -u claude-signal -f

# Show last 100 log lines
journalctl --user -u claude-signal -n 100
```

Note: For user services to start at boot (without login), enable lingering:
```bash
sudo loginctl enable-linger $USER
```

### Direct invocation

```bash
# Start with default config (~/.claude-signal/config.yaml)
claude-signal start

# Start with verbose (debug) logging
claude-signal start --verbose

# Start with a custom config file
claude-signal start --config /path/to/config.yaml

# Start in the foreground with verbose output (useful for debugging)
claude-signal start --verbose 2>&1 | tee /tmp/claude-signal.log
```

---

## 2. CLI Session Management (without Signal)

All Signal commands are also available directly from the CLI. These commands connect to the local daemon's data directory and do not require Signal connectivity.

```bash
# List all sessions with status
claude-signal session list

# Start a new claude-code session
claude-signal session new "build a REST API in Go"

# Show recent output from a session
claude-signal session status a3f2

# Get the last 50 lines of output
claude-signal session tail a3f2 --lines 50

# Send input to a session waiting for a prompt
claude-signal session send a3f2 "yes, continue"

# Terminate a session
claude-signal session kill a3f2

# Print the tmux attach command for a session
claude-signal session attach a3f2
# Prints: tmux attach -t cs-myhost-a3f2
# Run that command to get a full terminal in the session
```

The `--config` flag works with all session subcommands:

```bash
claude-signal session list --config /path/to/config.yaml
```

---

## 3. Configuration

Full `~/.claude-signal/config.yaml` with all fields and example values:

```yaml
# Identifies this machine in Signal messages and session IDs.
# Auto-detected from OS hostname if not set.
hostname: hal9000

# Root directory for sessions.json, logs/, and config.
# Default: ~/.claude-signal
data_dir: /home/user/.claude-signal

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
```

**Minimum viable config** (everything else uses defaults):

```yaml
signal:
  account_number: +12125551234
  group_id: abc123base64groupid==
```

---

## 4. Signal Account Management

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
claude-signal link

# Or using signal-cli directly
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

### Unregister from Signal entirely

```bash
signal-cli -u +12125551234 unregister
```

---

## 5. Backup and Recovery

### What to back up

| Path | Contents | Importance |
|---|---|---|
| `~/.claude-signal/config.yaml` | All daemon configuration | Critical — required to restart |
| `~/.claude-signal/sessions.json` | Session state and history | Important — lose this and running sessions can't be resumed |
| `~/.local/share/signal-cli/` | Signal account keys and identity | Critical — lose this and you must re-link from scratch |
| `~/.claude-signal/logs/` | Session output logs | Nice to have — historical output |

### Backup command

```bash
tar czf claude-signal-backup-$(date +%Y%m%d).tar.gz \
  ~/.claude-signal/ \
  ~/.local/share/signal-cli/
```

### Restore on a new machine

1. Install dependencies: `signal-cli`, Java 17+, `tmux`, `claude` (claude-code CLI), `claude-signal`

2. Restore the backup:
   ```bash
   tar xzf claude-signal-backup-20260325.tar.gz -C ~/
   ```

3. Verify the restored config points to the correct paths:
   ```bash
   cat ~/.claude-signal/config.yaml
   ```

4. Start the daemon:
   ```bash
   claude-signal start
   ```

5. Verify it connects to Signal:
   ```bash
   journalctl --user -u claude-signal -f
   # Look for: "subscribed to Signal messages"
   ```

Note: Sessions that were `running` on the old machine will be marked `failed` on resume because their tmux sessions no longer exist. This is expected.

---

## 6. Troubleshooting

### signal-cli not found in PATH

**Symptom:** `claude-signal start` fails with `signal-cli: executable file not found in $PATH`

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
sudo systemctl edit claude-signal
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

3. **QR code expired:** The sgnl:// link expires after a few minutes. Run `claude-signal link` again and scan immediately.

4. **Wrong Signal account:** Ensure the account number in `config.yaml` matches the Signal account you're scanning with.

### Sessions not resuming after restart (tmux sessions gone)

**Symptom:** After `systemctl restart claude-signal`, all sessions show as `failed`

**Explanation:** This is expected behavior. When the daemon restarts, it checks whether the tmux session for each `running`/`waiting_input` session still exists. If the machine was rebooted or tmux was killed, those sessions are gone and are marked `failed`.

**Prevention:**
- Use tmux server persistence plugins (e.g. `tmux-resurrect`) to restore tmux sessions across reboots
- Only restart the daemon, not the whole machine, to preserve running sessions
- For the daemon itself: `systemctl restart claude-signal` preserves tmux sessions; only a machine reboot or `tmux kill-server` loses them

### PWA can't connect (port, firewall, Tailscale)

**Symptom:** Browser shows "ERR_CONNECTION_REFUSED" or WebSocket fails to connect

**Diagnostic steps:**

```bash
# 1. Verify the daemon is running and listening
systemctl --user status claude-signal
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
systemctl --user edit claude-signal
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
- The device is not yet linked — run `claude-signal link` and scan the QR

### Multiple machines replying (expected behavior explanation)

**Symptom:** You send `list` and receive two or more replies from different machines

**This is expected behavior.** Each machine running `claude-signal` in the same Signal group will receive and process `list` commands independently, replying with its own sessions. Each reply is prefixed with `[hostname]` so you know which machine is responding.

To send a command to a specific machine's session, use the hostname-prefixed ID:
```
status myhost-a3f2
send myhost-a3f2: yes
```

Short IDs (`a3f2`) work if only one machine has a session with that ID. If both machines have a session `a3f2`, the command is ambiguous — each machine will act on its own `a3f2` session.

---

## 7. Monitoring

### Health check

```bash
curl http://localhost:8080/api/health
# Response: {"status":"ok","uptime":"1h23m4s"}
```

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
tail -f ~/.claude-signal/logs/myhost-a3f2.log

# Show last 50 lines
tail -n 50 ~/.claude-signal/logs/myhost-a3f2.log

# Count sessions in sessions.json
jq 'length' ~/.claude-signal/sessions.json

# Show all session states
jq '.[] | {id, state, task}' ~/.claude-signal/sessions.json
```

---

## 8. Log Levels

```bash
# Info logging (default) — startup, session state changes, errors
claude-signal start

# Debug logging (verbose) — all Signal messages, JSON-RPC traffic, monitor events
claude-signal start --verbose
```

When running as a systemd service, adjust the service override to add `--verbose`:

```bash
systemctl --user edit claude-signal
```

Add:
```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/claude-signal start --verbose --config /home/user/.claude-signal/config.yaml
```

Then:
```bash
systemctl --user daemon-reload
systemctl --user restart claude-signal
journalctl --user -u claude-signal -f
```
