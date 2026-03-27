# Setup Guide

Step-by-step instructions for getting `datawatch` running from scratch.

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

## Step 2: Install datawatch

```bash
go install github.com/dmz006/datawatch/cmd/datawatch@latest
```

Or build from source:

```bash
git clone https://github.com/dmz006/datawatch.git
cd datawatch
make install
```

---

## Step 3: Link and set up the control group (one command)

```bash
datawatch link
```

You'll be prompted for your Signal phone number, then a QR code is displayed.

**Scan the QR code** with your Signal app: **Settings → Linked Devices → Link New Device**

After you scan, datawatch automatically:
- Confirms the link
- Creates a Signal group called `datawatch-<hostname>`
- Saves the group ID to `~/.datawatch/config.yaml`
- Prints: `datawatch start`

That's it — no manual group creation, no `listGroups`, no `config init` needed.

**Example output:**

```
Linking device 'my-server' to Signal account +12125551234...
Scan the QR code with your Signal app:
  Settings → Linked Devices → Link New Device

[QR code displayed here]

Waiting for you to scan the QR code...

Device linked successfully!

Creating Signal control group 'datawatch-my-server'...
Group created: datawatch-my-server (ID: aGVsbG8gd29ybGQ=)

Setup complete! Start the daemon with:
  datawatch start

Send 'help' in the 'datawatch-my-server' group on Signal to verify.
```

### If auto-group creation fails

If the group creation step fails (rare — usually a signal-cli startup timing issue), fall
back to manual setup:

```bash
# Option A: create the group from your phone
# Open Signal → new group → add yourself → name it "datawatch control"
# Then get the group ID:
signal-cli -u +12125551234 listGroups
# Copy the base64 Id and run:
datawatch config init

# Option B: create via signal-cli directly
signal-cli -u +12125551234 updateGroup -n "datawatch control" -m +12125551234
# Copy the returned group ID, then:
datawatch config init
```

---

## Step 4: Start the daemon

```bash
datawatch start
```

You should see:

```
[my-server] datawatch v0.1.0 started.
```

---

## Step 5: Verify with help command

Send `help` in the `datawatch-<hostname>` group on Signal. You should receive:

```
[my-server] datawatch commands:
new: <task>       - start a new claude-code session
list              - list sessions + status
...
```

---

## Step 6: Run as a background service (optional)

### Using tmux (simple)

```bash
tmux new-session -d -s datawatch 'datawatch start'
```

### Using systemd (recommended for servers)

Create `/etc/systemd/system/datawatch.service`:

```ini
[Unit]
Description=datawatch daemon
After=network.target

[Service]
Type=simple
User=youruser
ExecStart=/usr/local/bin/datawatch start
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now datawatch
sudo journalctl -u datawatch -f
```

---

## Troubleshooting

### signal-cli fails to start

**Symptom:** `datawatch start` fails with "start signal-cli: ..."

**Solutions:**
- Verify `signal-cli` is in your PATH: `which signal-cli`
- Verify Java 17+ is installed: `java --version`
- Check signal-cli works directly: `signal-cli --version`
- Verify the account is linked: `signal-cli -u +<number> listAccounts`

### QR code scan not detected

**Symptom:** Scanned QR code but signal-cli link never completes

**Solutions:**
- Make sure you're scanning with the correct Signal account
- Try re-running `datawatch link` — the QR code expires
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
- Check the log file: `cat ~/.datawatch/logs/<hostname>-<id>.log`
- Verify tmux session exists: `tmux list-sessions`
- Attach to the session to see what claude-code is doing: `tmux attach -t cs-<hostname>-<id>`
- Adjust `input_idle_timeout` in config if claude-code takes a long time to produce output

### "session not found" error

**Symptom:** `status a3f2` returns "not found"

**Solutions:**
- Run `list` to see current session IDs
- Session IDs are 4 hex characters — check for typos
- Sessions are scoped per-host; `status` on a different machine won't find the session
