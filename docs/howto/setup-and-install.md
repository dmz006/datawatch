# How-to: First-time setup and install

Get a working datawatch daemon on a fresh machine in ten minutes.
This how-to assumes you've never run datawatch before; pick **one**
install path, complete the smoke test, then jump to
[chat-and-llm-quickstart](chat-and-llm-quickstart.md) to wire your
first session.

## Pick an install path

| Path | When to pick it | Time to ready |
|------|-----------------|---------------|
| **One-liner** (recommended) | You have a Linux / macOS box with curl + bash | ~30 s |
| **Pre-built binary** | You're on Windows, an air-gapped host, or want to pin a version | ~1 min |
| **`go install`** | You already have Go ≥ 1.22 and want the latest tip | ~1 min |
| **Container** (`ghcr.io/dmz006/datawatch-parent-full`) | You want everything in one image (daemon + worker shells) | ~2 min |

### Option A — one-liner

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install.sh | bash
```

The script downloads the right binary for your platform into
`~/.local/bin/datawatch`, sets the executable bit, and prints the
next-step `datawatch setup` invocation.

### Option B — pre-built binary

Grab the asset that matches your platform from the latest release and
drop it on `$PATH`:

```bash
# linux-amd64 example
curl -L -o ~/.local/bin/datawatch \
  https://github.com/dmz006/datawatch/releases/latest/download/datawatch-linux-amd64
chmod +x ~/.local/bin/datawatch
```

Available assets: `datawatch-{linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64.exe}` plus a `checksums-vX.Y.Z.txt` you can verify with `sha256sum -c`.

### Option C — `go install`

```bash
go install github.com/dmz006/datawatch/cmd/datawatch@latest
```

Lands in `$(go env GOPATH)/bin/datawatch`. Use this only if you're
comfortable building from source; release notes don't apply to tip.

### Option D — container

```bash
docker pull ghcr.io/dmz006/datawatch-parent-full:latest
docker run -it --rm \
  -v ~/.datawatch:/root/.datawatch \
  -p 8443:8443 -p 8080:8080 \
  ghcr.io/dmz006/datawatch-parent-full:latest start
```

Persist the data dir to a volume so config + sessions survive
restarts.

## Initial config wizard

```bash
datawatch setup
```

Walks you through:

1. **Bind addresses** — defaults to `0.0.0.0:8443` (HTTPS) +
   `0.0.0.0:8080` (HTTP redirect to TLS). Change with `--host`/`--port`
   on `datawatch start` if needed.
2. **TLS certificate** — auto-generates a self-signed cert under
   `~/.datawatch/tls/` on first run.
3. **First-time auth token** — generated and printed once; saved to
   `~/.datawatch/token`. Header for API calls:
   `Authorization: Bearer <token>`.
4. **Default LLM backend** — claude / ollama / etc. You can skip this
   and configure later in [chat-and-llm-quickstart](chat-and-llm-quickstart.md).

Re-run any time to re-walk individual sections (`datawatch setup ebpf`,
`datawatch setup signal`, etc.).

## Start the daemon

```bash
datawatch start            # daemonize in the background
datawatch start --foreground   # run in the current terminal
```

Verify it's up:

```bash
datawatch ping             # → "pong"
datawatch status           # daemon + sessions one-liner
```

Open the PWA in a browser:

```
https://localhost:8443
```

You'll get the self-signed-cert warning the first time — accept it,
then the PWA loads at the Sessions tab.

The Settings → Monitor tab confirms the daemon is healthy and shows
which subsystems are wired (CPU/mem/disk/GPU, daemon RSS, RTK token
savings, episodic memory, ollama runtime tap if configured):

![Settings → Monitor — System Statistics card](screenshots/settings-monitor.png)

Settings → About surfaces the version, the GitHub link for the mobile
companion, and the orphan-tmux maintenance affordance:

![Settings → About tab](screenshots/settings-about.png)

## Smoke test — start your first session

```bash
datawatch session start --task "echo hello from datawatch"
```

The session appears in the PWA Sessions list within ~2 s with state
`running`, then `done`. Click into it to see the tmux output and the
captured response.

## What got installed where

| Path | Purpose |
|------|---------|
| `~/.local/bin/datawatch` | The binary itself |
| `~/.datawatch/config.yaml` | Operator-edited config (sane defaults, override anything) |
| `~/.datawatch/daemon.log` | Live daemon log (`tail -f` it) |
| `~/.datawatch/sessions/` | Per-session output + metadata |
| `~/.datawatch/autonomous/` | PRD store (BL24 / BL191) |
| `~/.datawatch/memory/` | Episodic memory + KG (when enabled) |
| `~/.datawatch/tls/` | Self-signed cert + key |

## Upgrades

```bash
datawatch update --check    # dry-run — what version would land
datawatch update            # download + install the latest minor
datawatch restart           # apply the new binary; preserves tmux sessions
```

The PWA also exposes a one-click "Update" + "Restart" pair under
Settings → About.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | first-time setup | `datawatch setup` |
| CLI | start / restart | `datawatch start` / `datawatch restart` |
| CLI | health probe | `datawatch ping` / `datawatch status` |
| REST | health | `GET /healthz` |
| MCP | (no setup tools — daemon must already be running) | — |
| PWA | post-install | Settings → About → Update / Restart |

## See also

- [How-to: Chat + LLM quickstart](chat-and-llm-quickstart.md) — wire your first conversational backend
- [How-to: Daemon operations](daemon-operations.md) — day-two ops (start/stop/upgrade/diagnose)
- [`docs/setup.md`](../setup.md) — exhaustive install reference
- [`docs/operations.md`](../operations.md) — systemd unit / hardened deploy
