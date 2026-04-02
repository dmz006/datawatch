# Setup Guide

Step-by-step instructions for getting `datawatch` running from scratch.

---

## Messaging Backends

datawatch supports many messaging backends. You don't need Signal — pick whichever you use:

| Backend | Setup command | Prerequisites |
|---------|--------------|---------------|
| **Signal** | `datawatch link` or `datawatch setup signal` | Java 17+, signal-cli, phone number |
| **Telegram** | `datawatch setup telegram` | Telegram bot token (from @BotFather) |
| **Discord** | `datawatch setup discord` | Discord bot token and channel ID |
| **Slack** | `datawatch setup slack` | Slack bot token and channel ID |
| **Matrix** | `datawatch setup matrix` | Matrix homeserver URL, user, token, room ID |
| **Twilio SMS** | `datawatch setup twilio` | Twilio account SID, auth token, phone numbers |
| **ntfy** | `datawatch setup ntfy` | ntfy server URL and topic (push notifications only) |
| **Email** | `datawatch setup email` | SMTP server details (outbound alerts only) |
| **Webhooks** | `datawatch setup webhook` | Webhook URL for incoming/outgoing |
| **GitHub** | `datawatch setup github` | GitHub webhook secret |
| **Web UI only** | `datawatch setup web` | No external service needed |

All backends can also be configured via the **web UI** (Settings tab), the **REST API** (`/api/config`), or by editing `~/.datawatch/config.yaml` directly. See [messaging-backends.md](messaging-backends.md) for detailed configuration of each backend.

You can enable multiple backends simultaneously — messages are routed to all active channels.

---

## Prerequisites

- Linux or macOS (WSL2 also works)
- tmux
- An LLM backend CLI installed and authenticated (e.g. `claude` for claude-code)
- For Signal: Java 17+, signal-cli, phone number (see below)
- For voice input (optional): Python 3, ffmpeg, and `openai-whisper` in a venv (see [messaging-backends.md](messaging-backends.md#voice-input-whisper-transcription))

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

## Step 3: Set up a messaging backend

### Option A: Interactive setup wizard (any backend)

```bash
datawatch setup telegram   # or discord, slack, matrix, web, etc.
```

The wizard prompts for the required credentials and writes them to `~/.datawatch/config.yaml`.

### Option B: Signal — link and set up the control group (one command)

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

---

## Config File Security

The config file at `~/.datawatch/config.yaml` contains sensitive credentials (bot tokens, API keys, phone numbers). By default it is stored in plaintext with `0600` permissions (readable only by your user).

### Encrypting the Config File

To encrypt the config file with a password:

```bash
# Initialize config with encryption
datawatch --secure config init

# Start the daemon (must use --foreground with encrypted config)
datawatch --secure start --foreground
```

When `--secure` is set, **all data files** in `~/.datawatch/` are encrypted, not just `config.yaml`:

| File | Contents |
|---|---|
| `config.yaml` | Bot tokens, API keys, credentials |
| `sessions.json` | Session metadata and task descriptions |
| `schedule.json` | Scheduled commands |
| `commands.json` | Saved command library |
| `filters.json` | Output filter rules |
| `alerts.json` | System alert history |

**How it works:**
- The config file is encrypted with XChaCha20-Poly1305 using Argon2id key derivation (64 MB, 4 threads, run once). The salt is embedded in the config file header.
- A 32-byte data key is derived from the same password + salt — this avoids re-running the expensive KDF on every session state update.
- All data store writes use XChaCha20-Poly1305 with a fresh random nonce per write.
- Session output logs use streaming block encryption (DWLOG1 format) via FIFO.
- On first `--secure` start, existing plaintext files are automatically migrated to encrypted.

### Important Warnings

> **If you lose the encryption password, all data files cannot be recovered.**
> Back up your bot tokens, API keys, and session data before enabling encryption.

- The password is never stored or cached — you must enter it each time the daemon starts.
- Encrypted config is not compatible with daemon mode (background daemonize requires interactive password input). Use `--foreground` with `--secure`, or set `DATAWATCH_SECURE_PASSWORD` environment variable.
