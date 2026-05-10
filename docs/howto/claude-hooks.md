---
docs:
  index: true
  topics: [hooks, claude, status, statusline]
exec_params:
  - {name: session_id, required: true, description: "Full session ID (hostname-id) to set up hooks for"}
exec_steps:
  - description: "Manual setup walkthrough — see body of this howto. Auto-install lands alpha.34a."
    read_only: true
---

# Claude Code hooks → Status board (alpha.34 #202)

The session detail view's **Status** sub-tab renders a per-session
"where in the process is this session right now" board fed by Claude
Code's hook events. This howto walks you through wiring `.claude/`
hooks in your project so the daemon receives those events and the
PWA's Status tab lights up.

> Auto-install of these scripts at session spawn is tracked as a
> follow-up (POST v7.0). For v7.0, set them up by hand once per project.

## What ships in v7.0

- `POST /api/sessions/<session_id>/hook-event` — endpoint your hook
  scripts call to send each event.
- `GET  /api/sessions/<session_id>/status` — derived board read by the
  PWA Status tab.
- PWA Status sub-tab + state badge on the tab.
- Hook event store retains the **last 50 events per session** + the
  most-recent `current_focus`, `sprint`, `tests`, `git` payloads.

## What lands later (post-GATE)

- **Auto-install** of `.claude/sprint/*` scripts at session spawn
  (alpha.34a). Will write to `<project_dir>/.claude/sprint/` with the
  daemon URL + per-session bearer token in `.dw-env` (chmod 600).
- **Detection augmentation** — when a fresh `Stop` hook fires, the
  daemon's session state engine prefers it over screen-buffer pattern
  matching (alpha.34b).
- **Alert enrichment** — `[sess] needs input` alerts gain
  `last_prompt`, `last_assistant_text`, `last_tool`, `idle_since` fields
  from the hook event stream (alpha.34b).
- **Opencode hook mapping** — same pipeline if/where opencode exposes
  equivalent hooks (alpha.34c).

## Manual setup (one-time per project)

Create `<project_dir>/.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      { "type": "command", "command": "/absolute/path/to/.claude/sprint/post-event.sh Stop" }
    ],
    "PostToolUse": [
      { "type": "command", "command": "/absolute/path/to/.claude/sprint/post-event.sh PostToolUse $TOOL_NAME" }
    ],
    "UserPromptSubmit": [
      { "type": "command", "command": "/absolute/path/to/.claude/sprint/post-event.sh UserPromptSubmit" }
    ]
  }
}
```

Create `<project_dir>/.claude/sprint/post-event.sh`:

```bash
#!/usr/bin/env bash
# post-event.sh — POST a Claude hook event to datawatch.
# Args: $1 = event name (Stop/PostToolUse/UserPromptSubmit), $2 = optional tool name.
set -euo pipefail

# Per-project config (chmod 600). Set DAEMON_URL + SESSION_ID + TOKEN.
source "$(dirname "$0")/.dw-env"

EVENT="$1"
TOOL="${2:-}"

# Optional: include sprint state from .claude/sprint/state.json if you
# maintain one. The hook doesn't require it.
PAYLOAD='{}'
if [[ -f "$(dirname "$0")/state.json" ]]; then
  PAYLOAD=$(cat "$(dirname "$0")/state.json")
fi

curl -ks -X POST "${DAEMON_URL}/api/sessions/${SESSION_ID}/hook-event" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{\"event\":\"${EVENT}\",\"tool\":\"${TOOL}\",\"payload\":${PAYLOAD}}" \
  >/dev/null 2>&1 || true
```

`chmod +x post-event.sh`

Create `<project_dir>/.claude/sprint/.dw-env`:

```bash
DAEMON_URL=https://localhost:8443
SESSION_ID=<your-session-full-id>
TOKEN=<your-daemon-bearer-token>
```

`chmod 600 .dw-env`

## Verifying

Trigger any tool use in your Claude Code session. The PWA Status tab
should show the current event under "Current focus" + the tab state
badge updates (🟢 running / 🟠 waiting / ⚪ idle).

You can also POST manually for testing:

```bash
curl -ks -X POST https://localhost:8443/api/sessions/<sid>/hook-event \
  -H 'Content-Type: application/json' \
  -d '{"event":"Stop","payload":{"current_task":"writing tests","tests":{"pass":12,"fail":0}}}'
```

Then `GET /api/sessions/<sid>/status` returns the board JSON.

## Optional: rich payloads

The board renders cards for any of these payload fields when present:

| Field | Card | Example |
|---|---|---|
| `current_task` | Current focus | `"refactor router"` |
| `sprint` | Sprint / PRD tree | full `.claude/sprint/state.json` |
| `tests` | Tests | `{"pass": 12, "fail": 0, "skip": 1}` |
| `git` | Git | `{"branch": "feat/x", "dirty": true}` |

Council / Skills / Tracker / closed-task summaries land in alpha.34a
once payload conventions for those settle.
