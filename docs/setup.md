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

## Optional: Set up Voice Input (Whisper)

Voice messages sent via Telegram or Signal can be automatically transcribed to text. This requires OpenAI Whisper running locally.

### Install dependencies

```bash
# ffmpeg is required for audio decoding
sudo apt install ffmpeg        # Debian/Ubuntu
brew install ffmpeg             # macOS

# Create a Python virtual environment and install Whisper
cd /path/to/datawatch          # or wherever you run datawatch from
python3 -m venv .venv
.venv/bin/pip install openai-whisper
```

> **Note:** On systems without a GPU, pip will install CPU-only PyTorch automatically. If you have a CUDA GPU and want faster transcription, install the CUDA version of PyTorch first: `.venv/bin/pip install torch --index-url https://download.pytorch.org/whl/cu121` before installing whisper.

### Configure

All configuration methods:

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `whisper:` section (see below) |
| **Web UI** | Settings tab → General → **Voice Input (Whisper)** card |
| **REST API** | `PUT /api/config` with `{"whisper.enabled": true, "whisper.model": "base"}` |
| **Comm channel** | `configure whisper.enabled=true`, `configure whisper.model=small` |
| **Test endpoint** | `POST /api/test/message` with `{"text": "configure whisper.enabled=true"}` |

```yaml
whisper:
  enabled: true
  model: base          # tiny, base, small, medium, large
  language: en         # ISO 639-1 code, or "auto" for detection
  venv_path: .venv     # path to the Python venv (relative or absolute)
```

### Model selection

| Model | Size | Speed | Best for |
|-------|------|-------|----------|
| tiny | 39 MB | fastest | Quick commands, low-resource servers |
| base | 74 MB | fast | General use (default) |
| small | 244 MB | moderate | Mixed languages, accented speech |
| medium | 769 MB | slow | High accuracy |
| large | 1.5 GB | slowest | Maximum accuracy, all languages |

### Supported languages

99 languages supported. Common codes: `en` (English), `es` (Spanish), `de` (German), `fr` (French), `ja` (Japanese), `zh` (Chinese), `ko` (Korean), `pt` (Portuguese), `ru` (Russian), `ar` (Arabic), `hi` (Hindi).

Set `language: auto` for automatic detection (slower, may be less accurate for short messages).

Full list: [Whisper language support](https://github.com/openai/whisper#available-models-and-languages)

> **Multi-user note:** Currently a single default language is configured globally. Per-user language preferences are planned as part of the multi-user access control feature.

### How it works

1. Send a voice message via Telegram or Signal
2. The backend downloads the audio to a temp file
3. Whisper transcribes the audio to text
4. The router echoes `Voice: <transcribed text>` back to the channel
5. The transcribed text is processed as a normal command (`new`, `send`, implicit send, etc.)
6. Temp audio files are cleaned up after transcription

### Verify

Send a voice message in your Telegram or Signal group. You should see:

```
[myhost] Voice: your spoken words here
[myhost][a1b2] Input sent.
```

---

## Optional: Set up RTK (Token Savings)

[RTK (Rust Token Killer)](https://github.com/rtk-ai/rtk) is a Rust CLI proxy that compresses AI coding agent output, reducing token consumption by 60-90%.

### Install RTK

```bash
# Download the latest release
curl -fsSL https://github.com/rtk-ai/rtk/releases/latest/download/rtk-linux-amd64 \
  -o ~/.local/bin/rtk && chmod +x ~/.local/bin/rtk

# Initialize hooks for your AI coding tool
rtk init -g
```

### Configure in datawatch

All configuration methods:

| Method | How |
|--------|-----|
| **CLI** | `datawatch setup rtk` (interactive wizard) |
| **YAML** | Edit `~/.datawatch/config.yaml` → `rtk:` section (see below) |
| **Web UI** | Settings tab → General → **RTK (Token Savings)** card |
| **REST API** | `PUT /api/config` with `{"rtk.enabled": true}` |
| **Comm channel** | `configure rtk.enabled=true`, `configure rtk.auto_init=true` |

```yaml
rtk:
  enabled: true
  binary: rtk              # path to RTK binary
  show_savings: true       # show savings in stats dashboard
  auto_init: true          # auto-run 'rtk init -g' if hooks missing
  discover_interval: 0     # seconds between optimization checks (0 = disabled)
```

RTK only activates for supported backends: **claude-code**, **gemini**, **aider**.

### Where to see RTK stats

| Location | What you see |
|----------|-------------|
| **Web UI** | Settings → Monitor tab → **RTK Token Savings** card (version, hooks, tokens saved, avg %, commands) |
| **REST API** | `GET /api/stats` → `rtk_installed`, `rtk_version`, `rtk_hooks_active`, `rtk_total_saved`, `rtk_avg_savings_pct`, `rtk_total_commands` |
| **Comm channel** | `stats` command includes RTK summary when enabled |
| **Prometheus** | `GET /metrics` includes RTK gauge metrics |

See [rtk-integration.md](rtk-integration.md) for full details.

---

## Optional: Set up Backend Profiles and Fallback Chains

Profiles allow multiple accounts or API keys for the same LLM backend. Fallback chains auto-switch to the next profile when the primary hits a rate limit.

### Configure profiles

```yaml
profiles:
  claude-work:
    backend: claude-code
    # Uses default claude auth (e.g. Max subscription)
  claude-personal:
    backend: claude-code
    env:
      ANTHROPIC_API_KEY: "sk-ant-..."
  gemini-fallback:
    backend: gemini
    env:
      GEMINI_API_KEY: "AIza..."
```

### Set up a fallback chain

```yaml
session:
  fallback_chain:
    - claude-personal
    - gemini-fallback
```

When the primary backend hits a rate limit, datawatch automatically starts a new session with the next profile in the chain, copying the task and project directory.

### Manage profiles

All configuration methods:

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `profiles:` and `session.fallback_chain:` sections |
| **Web UI** | Settings tab → General → **Profiles & Fallback** card (fallback chain); New Session form → **Profile dropdown** |
| **REST API** | `GET /api/profiles` (list), `POST /api/profiles` (create), `DELETE /api/profiles?name=X` (remove) |
| **Comm channel** | `configure session.fallback_chain=claude-personal,gemini-backup` |
| **New session with profile** | `new claude-personal: <task>` (from any comm channel) |

---

## Optional: Set up Proxy Mode (Multi-Machine)

Proxy mode lets one datawatch instance manage sessions on multiple remote machines.
Commands from Signal/Telegram are auto-routed to the correct machine.

### Add a remote server

| Method | How |
|--------|-----|
| **CLI** | `datawatch setup server` (interactive wizard) |
| **YAML** | `servers:` list in `~/.datawatch/config.yaml` (see below) |
| **Web UI** | Settings tab → Comms → **Servers** section |
| **REST API** | N/A (edit config directly — servers require daemon restart) |
| **Comm channel** | N/A |

```yaml
servers:
  - name: prod               # short name used in commands
    url: http://192.168.1.10:8080   # remote datawatch URL
    token: "bearer-token"     # must match remote server.token
    enabled: true
  - name: pi
    url: http://10.0.0.50:8080
    token: "another-token"
    enabled: true
```

Restart the daemon after adding servers.

### Usage

From any messaging channel or the test endpoint:

```
list                          — shows sessions from ALL servers
status a3f2                   — auto-finds which server has session a3f2
send a3f2: yes                — routes to the correct remote
kill a3f2                     — kills on the owning remote
new: @prod: deploy pipeline   — starts session on specific remote
new: @pi: run backups         — starts on the pi server
```

### Web UI

In Settings → Comms → Servers, click a remote server to switch the web UI to that
instance. All session views, terminal output, and input are proxied through the local
instance — no direct network access to the remote is needed.

### Where to see aggregated sessions

| Location | What you see |
|----------|-------------|
| **Web UI** | Server badges on session cards; server indicator in toolbar |
| **REST API** | `GET /api/sessions/aggregated` — all servers, tagged with `server` field |
| **Comm channel** | `list` shows `[local]`, `[prod]`, `[pi]` sections |
| **CLI** | `datawatch --server prod session list` (targets one remote) |

### Network requirements

- The proxy instance must be able to reach each remote's HTTP port
- Remote instances must have `server.token` set (the proxy injects it as Bearer auth)
- WebSocket relay requires persistent TCP connection to each remote
- No inbound connections from remotes to proxy are needed

See [architecture.md](architecture.md#proxy-mode-architecture) for flow diagrams.

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

## Episodic Memory Setup (optional)

Enable the memory system for semantic search across sessions, knowledge graph,
and auto-context injection.

### Prerequisites

- **Ollama** running with an embedding model (e.g., `nomic-embed-text`)
  ```bash
  ollama pull nomic-embed-text
  ```

### Enable memory

**Web UI:** Settings → LLM → Episodic Memory → click "Test" then toggle on

**CLI/comm channel:**
```bash
configure memory.enabled=true
```

**YAML config:**
```yaml
memory:
  enabled: true
  embedder_model: nomic-embed-text
  # embedder_host defaults to ollama.host if empty
```

### Memory commands

```
remember: always run go mod tidy before committing
recall: how to commit
memories                    # list recent
memories reindex            # re-embed after model change
forget 42                   # delete by ID
learnings                   # extracted task learnings
kg add Alice works_on myapp # knowledge graph
kg query Alice              # entity relationships
```

### PostgreSQL backend (enterprise)

For team deployments or large memory stores, use PostgreSQL instead of SQLite:

```bash
# Prerequisites: PostgreSQL 14+ with pgvector
sudo apt install postgresql-17 postgresql-17-pgvector

# Create database
sudo -u postgres psql -c "CREATE USER datawatch WITH PASSWORD 'datawatch';"
sudo -u postgres psql -c "CREATE DATABASE datawatch OWNER datawatch;"
sudo -u postgres psql -d datawatch -c "CREATE EXTENSION IF NOT EXISTS vector;"

# Configure
configure memory.backend=postgres
configure memory.postgres_url=postgres://datawatch:datawatch@127.0.0.1/datawatch
```

See [memory.md](memory.md) for full PostgreSQL setup and migration instructions.

### Memory encryption

When using `--secure` mode, memory content is automatically encrypted with
XChaCha20-Poly1305 on both SQLite and PostgreSQL backends.
See [encryption.md](encryption.md) for details.

### Session chaining (pipelines)

Chain multiple tasks in sequence:
```
pipeline: analyze auth code -> write JWT middleware -> update tests
pipeline status
pipeline cancel pipe-12345
```

See [docs/memory.md](memory.md) for full memory documentation.

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

Encryption can be enabled **at any time** — you do not need to start with encryption. Existing plaintext data files are automatically migrated to encrypted on the first `--secure` start.

```bash
# Option A: Enable encryption on an existing installation
# Just add --secure to your start command — plaintext files are migrated automatically
datawatch --secure start --foreground

# Option B: Start fresh with encryption from the beginning
datawatch --secure config init
datawatch --secure start --foreground

# Option C: Non-interactive (for systemd / background use)
export DATAWATCH_SECURE_PASSWORD="your-passphrase"
datawatch --secure start
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
