# Setup Guide

Step-by-step instructions for getting `claude-signal` running from scratch.

---

## Prerequisites

- Linux or macOS
- Java 17 or later (for signal-cli)
- tmux
- A Signal account (phone number)
- `claude-code` CLI installed and authenticated

---

## Step 1: Install signal-cli

signal-cli is a command-line interface for the Signal messenger. It wraps the official Signal library (libsignal) with Java bindings.

**Download the latest release:**

```bash
# Check https://github.com/AsamK/signal-cli/releases for the latest version
VERSION=0.13.7
wget https://github.com/AsamK/signal-cli/releases/download/v${VERSION}/signal-cli-${VERSION}-Linux.tar.gz
tar xf signal-cli-${VERSION}-Linux.tar.gz
sudo mv signal-cli-${VERSION}/bin/signal-cli /usr/local/bin/
sudo mv signal-cli-${VERSION}/lib /usr/local/lib/signal-cli
```

**Verify installation:**

```bash
signal-cli --version
```

> **Note:** signal-cli requires Java 17+. Install with `sudo apt install openjdk-17-jre` (Debian/Ubuntu) or `brew install openjdk@17` (macOS).

---

## Step 2: Install claude-signal

```bash
go install github.com/dmz006/claude-signal/cmd/claude-signal@latest
```

Or build from source:

```bash
git clone https://github.com/dmz006/claude-signal.git
cd claude-signal
make install
```

---

## Step 3: Register or link signal-cli to your account

### Option A: Use claude-signal's built-in link command (recommended)

```bash
claude-signal link
```

Follow the prompts. Scan the displayed QR code with your Signal mobile app:
**Settings → Linked Devices → Link New Device**

### Option B: Use signal-cli directly

```bash
# Link as a new device (recommended — avoids registering a new number)
signal-cli link -n my-server
# Scan the QR code printed to terminal
```

The config directory defaults to `~/.local/share/signal-cli/`.

---

## Step 4: Create a Signal group

1. Open Signal on your phone
2. Tap the compose button → **New Group**
3. Add yourself (and any other accounts you'll control the daemon from)
4. Name the group (e.g., "claude-signal control")
5. Do **not** add the phone number linked to signal-cli — it's already part of the group via your account

---

## Step 5: Get the group ID

```bash
signal-cli -u +12125551234 listGroups
```

Look for your group in the output. Copy the `Id:` field — it looks like a base64 string:

```
Id: aGVsbG8gd29ybGQ=
Name: claude-signal control
Members: ...
```

---

## Step 6: Configure claude-signal

```bash
claude-signal config init
```

You'll be prompted for:
- **Signal phone number** — the number you registered/linked (e.g. `+12125551234`)
- **Signal group ID** — the base64 ID from Step 5
- **Hostname** — auto-detected; identifies this machine in Signal messages
- **Device name** — shown in Signal's linked devices list
- **claude-code binary path** — defaults to `claude`

Config is saved to `~/.claude-signal/config.yaml`.

---

## Step 7: Test Signal connectivity

Send a test message to your group via signal-cli to confirm everything is working:

```bash
signal-cli -u +12125551234 send -g "aGVsbG8gd29ybGQ=" -m "signal-cli test"
```

You should see the message appear in your Signal group.

---

## Step 8: Start the daemon

```bash
claude-signal start
```

You should see:

```
[my-server] claude-signal v0.1.0 started. Listening on group aGVsbG8gd29ybGQ=
```

---

## Step 9: Verify with help command

Send `help` in your Signal group. You should receive a reply:

```
[my-server] claude-signal commands:
new: <task>       - start a new claude-code session
list              - list sessions + status
...
```

---

## Step 10: Run as a background service (optional)

### Using tmux (simple)

```bash
tmux new-session -d -s claude-signal 'claude-signal start'
```

### Using systemd (recommended for servers)

Create `/etc/systemd/system/claude-signal.service`:

```ini
[Unit]
Description=claude-signal daemon
After=network.target

[Service]
Type=simple
User=youruser
ExecStart=/usr/local/bin/claude-signal start
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now claude-signal
sudo journalctl -u claude-signal -f
```

---

## Troubleshooting

### signal-cli fails to start

**Symptom:** `claude-signal start` fails with "start signal-cli: ..."

**Solutions:**
- Verify `signal-cli` is in your PATH: `which signal-cli`
- Verify Java 17+ is installed: `java --version`
- Check signal-cli works directly: `signal-cli --version`
- Verify the account is linked: `signal-cli -u +<number> listAccounts`

### QR code scan not detected

**Symptom:** Scanned QR code but signal-cli link never completes

**Solutions:**
- Make sure you're scanning with the correct Signal account
- Try re-running `claude-signal link` — the QR code expires
- Ensure the Signal app on your phone is up to date

### Messages not received

**Symptom:** Daemon is running but doesn't respond to Signal messages

**Solutions:**
- Confirm the `group_id` in config matches the output of `signal-cli listGroups`
- Ensure the linked device is still active: check Signal → Settings → Linked Devices
- Check signal-cli can receive: `signal-cli -u +<number> receive`

### Session stays in `running` state forever

**Symptom:** A session never transitions to `waiting_input` or `complete`

**Solutions:**
- Check the log file: `cat ~/.claude-signal/logs/<hostname>-<id>.log`
- Verify tmux session exists: `tmux list-sessions`
- Attach to the session to see what claude-code is doing: `tmux attach -t cs-<hostname>-<id>`
- Adjust `input_idle_timeout` in config if claude-code takes a long time to produce output

### "session not found" error

**Symptom:** `status a3f2` returns "not found"

**Solutions:**
- Run `list` to see current session IDs
- Session IDs are 4 hex characters — check for typos
- Sessions are scoped per-host; `status` on a different machine won't find the session
