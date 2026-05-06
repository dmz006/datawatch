# How-to: Daemon operations

Day-two operator workflow: start, stop, restart, upgrade, diagnose,
reload, logs. The "what to do when something is off" doc.

## What it is

A single Go binary that runs as the long-lived daemon process. State
lives at `~/.datawatch/`; logs at `~/.datawatch/daemon.log`. Sessions
(tmux + LLM) survive daemon restart via pipe-pane re-establishment.

## Base requirements

- `datawatch` binary on PATH.
- `tmux` ≥ 3.0 on the host (sessions need it).

## Setup

`datawatch init` (one-time) creates `~/.datawatch/` + auto-TLS certs
+ initial config. After that, no setup beyond config edits.

## Two happy paths

### 4a. Happy path — CLI

Daily operations:

```sh
# Start.
datawatch start
#  → datawatch listening on https://0.0.0.0:8443

# Health check.
datawatch health
#  → status: ok  version: 6.13.0  uptime: 2h 14m  sessions: 4 (3 running, 1 waiting_input)

# Reload config (no restart; picks up most YAML changes).
datawatch reload

# Restart (graceful; preserves running sessions via pipe-pane re-establish).
datawatch restart

# Stop (terminates daemon; running tmux sessions stay alive).
datawatch stop

# Upgrade.
datawatch update                     # downloads + replaces binary
datawatch restart                    # apply

# Status.
datawatch status
#  → daemon: running  PID 49334
#    listeners: 0.0.0.0:8443 (https), 0.0.0.0:8080 (http→redirect)
#    sessions: 4
#    backends: claude-code (✓), ollama (✓)
#    channels: telegram (✓)

# Diagnose (deep health — backends, channels, observer, memory, KG).
datawatch diagnose
#  → ✓ daemon healthy
#    ✓ all backends reachable
#    ✓ telegram channel connected
#    ✓ observer pushing to primary-kona (last 3s ago)
#    ⚠ memory backend: postgres disk usage 78% (threshold 80%)
#    ✓ KG queries respond <50ms p95

# Logs.
datawatch logs                        # full log
datawatch logs -f                      # follow
datawatch logs --since 10m            # tail recent
datawatch logs --grep MarkChannel     # filter
```

### 4b. Happy path — PWA

1. Settings → About card. Top-of-card actions:
   - **Check now** — manual update check.
   - **Restart** — graceful daemon restart.
   - **Stop** — full stop (you'll need shell access to start back up).
2. Settings → About → **Orphaned tmux sessions** row — lists `cs-*`
   tmux sessions on the host with no daemon record. Click any row
   to kill.
3. Observer → **Daemon log** card. Tail of the daemon log; click any
   row to expand the full message. Filter by level (info / warn /
   error) or text substring.
4. Observer → **Audit log** card. Every operator action recorded.
   Filter by actor / action / time.
5. Observer → **Process envelopes** card. Real-time view of where the
   host's CPU + RAM are going (per session, per backend, per
   container).

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Settings → About + Observer → Daemon log + Audit log surfaces.
Restart-from-mobile is gated behind a confirm dialog.

### 5b. REST

```sh
# Health (no auth required for /api/health).
curl -sk $BASE/api/health

# Info (auth required).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/info

# Reload.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/reload

# Restart (warns before; idempotent).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/restart

# Diagnose.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/diagnose

# Audit log.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/audit?actor=operator&action=secrets_get&limit=50"
```

### 5c. MCP

Tools: `daemon_health`, `daemon_info`, `daemon_reload`,
`daemon_diagnose`, `audit_list`.

Useful for an LLM coordinator that needs to verify daemon health
before kicking off a long-running workflow.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `health` | One-line health for chat. |
| `status` | Multi-line with sessions / backends / channels. |
| `diagnose` | Deep health (slower). |
| `audit list actor=... limit=20` | Filtered audit entries. |

`reload` / `restart` / `stop` are CLI-only (too dangerous to expose
to chat).

### 5e. YAML

`~/.datawatch/datawatch.yaml` is the source of truth for everything:
backends, channels, secrets backend, observer, memory, automata, MCP,
TLS, server port. Auto-generated on first `datawatch init`; edit + `datawatch reload` to apply (or `restart` for top-level changes).

Key sections:

```yaml
server:
  host: 0.0.0.0
  port: 8443
  tls_enabled: true
  tls_cert: ~/.datawatch/tls/cert.pem
  tls_key:  ~/.datawatch/tls/key.pem
  bearer_token: ${secret:SERVER_TOKEN}

session:
  default_backend: claude-code
  default_effort: normal
  max_sessions: 50

memory:    # see cross-agent-memory.md
mcp:       # see mcp-tools.md
observer:  # see federated-observer.md
agents:    # see container-workers.md + tailscale-mesh.md
secrets:   # see secrets-manager.md
channels:  # see comm-channels.md
```

## Diagram

```
   ┌────────────────────────────────┐
   │ datawatch daemon (single Go    │
   │ binary; long-lived process)    │
   └─────────┬──────────────────────┘
             │
             ▼
   ┌──────────────────────────────────────┐
   │ ~/.datawatch/                         │
   │  ├─ datawatch.yaml                    │
   │  ├─ identity.yaml                     │
   │  ├─ daemon.log         ← diagnostic   │
   │  ├─ tls/{cert,key}.pem                │
   │  ├─ secrets.db (or kdbx / op vault)   │
   │  ├─ sessions/<id>/...                 │
   │  ├─ profiles/{projects,clusters}/     │
   │  ├─ council/personas/                 │
   │  ├─ evals/{<name>.yaml,runs/}         │
   │  ├─ algorithm/<session-id>/           │
   │  ├─ autonomous/{prds/,templates/}     │
   │  ├─ orchestrator/graphs/              │
   │  ├─ pipelines/<id>.yaml               │
   │  └─ skills/<name>/                    │
   └──────────────────────────────────────┘
```

## Common pitfalls

- **`datawatch reload` doesn't pick up the change.** Some sections
  require restart (`server.port`, `tls.*`, `agents.*`, plugin
  entries). When in doubt, `datawatch restart`.
- **`datawatch stop` leaves orphan tmux sessions.** That's by design
  — the LLM keeps working. To fully stop including sessions, kill
  them first: `datawatch sessions list | xargs -L1 datawatch sessions kill`.
- **Update mid-session.** Safe — `datawatch update` swaps the binary;
  `datawatch restart` re-attaches via pipe-pane. Sessions reach
  `LastChannelEventAt = now` and continue.
- **Disk fills.** `output.log` grows append-only. Configure
  `session.log_rotate_mb` to roll automatically (default 100 MB per
  session).
- **`/api/health` returns 200 but daemon is misbehaving.** `health`
  is a shallow check. Use `diagnose` for the deep one — backends,
  channels, observer, memory, KG.

## Linked references

- See also: [`setup-and-install.md`](setup-and-install.md) — first-run.
- See also: every other howto — they call back here for restart /
  reload / log inspection.
- Architecture: `../architecture-overview.md` § Daemon lifecycle.

## Screenshots needed (operator weekend pass)

- [ ] Settings → About card with restart / update / orphaned-tmux rows
- [ ] Observer → Daemon log card with filter
- [ ] Observer → Audit log card
- [ ] CLI `datawatch status` output
- [ ] CLI `datawatch diagnose` output (with a warn line)
