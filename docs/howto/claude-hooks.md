---
docs:
  index: true
  topics: [hooks, claude, status, statusline]
exec_params:
  - {name: session_id, required: true, description: "Full session ID (hostname-id) to set up hooks for"}
exec_steps:
  - tool: session_get
    description: Confirm the session exists and is running
    args: {id: "{{params.session_id}}"}
    read_only: true
  - tool: session_hook_status
    description: Check whether hook scripts are already installed
    args: {id: "{{params.session_id}}"}
    read_only: true
---

# How-to: Claude Code hooks + Status board

The session detail's **Status** sub-tab renders a live "where is this
session right now" board fed by Claude Code's hook events. datawatch
auto-installs the hook scripts at session spawn (claude-code backend
only) and tears them down when the session ends. This howto covers
the auto-install behaviour, opt-out, manual setup, and how to extend
the Status board with richer payloads.

## What it is

Three Claude Code hooks (`Stop`, `PostToolUse`, `UserPromptSubmit`)
call a per-session endpoint on the daemon. The daemon stores the last
50 events per session and derives a board from the most-recent
`current_focus`, `sprint`, `tests`, and `git` payload fields. The
PWA's Status sub-tab polls this board and renders a card per field.

Additionally:
- When a `Stop` hook fires, the daemon's session state engine prefers
  it over screen-buffer pattern matching — faster, more accurate
  completion detection.
- `[sess] needs input` alerts are enriched with `last_prompt`,
  `last_assistant_text`, `last_tool`, and `idle_since` from the
  hook stream.
- Opencode sessions emit equivalent hook events through the same
  pipeline where opencode exposes compatible hooks.

## Base requirements

- `datawatch start` — daemon up.
- A `claude-code` session (auto-install is claude-code specific;
  other backends can use manual setup).
- `session.auto_install_hooks` not set to `false` in
  `~/.datawatch/datawatch.yaml` (default: enabled).

## Setup

No manual setup required for claude-code sessions — auto-install
handles it. To **opt out** for a project:

```yaml
# ~/.datawatch/datawatch.yaml
session:
  auto_install_hooks: false
```

Or pass `--no-hooks` on `datawatch sessions start`.

## Two happy paths

### 4a. Happy path — auto-install (default)

```sh
# 1. Start a claude-code session as normal.
SID=$(datawatch sessions start --llm claude-code \
  --task "refactor auth module" \
  --project-dir ~/work/myproject 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')

# 2. Daemon auto-installs hooks into <project_dir>/.claude/:
#      settings.json  — appends Stop/PostToolUse/UserPromptSubmit entries
#      sprint/post-event.sh  — the hook script (chmod +x)
#      sprint/.dw-env        — DAEMON_URL + SESSION_ID + TOKEN (chmod 600)
#    Existing settings.json hooks are preserved; daemon entry appended
#    idempotently (detected by path, safe to re-install).

# 3. Watch the Status board update as the session works.
datawatch sessions status $SID
#  → current_focus: "refactoring CookieAuthMiddleware"
#    tests:  { pass: 12, fail: 0 }
#    git:    { branch: "feat/auth", dirty: true }
```

### 4b. Happy path — PWA

1. Start a session (Sessions → + FAB → LLM: claude-code → Start).
2. Open the session detail → **Status** tab.
3. As Claude Code runs, cards populate:
   - **Current focus** — task description from the most-recent hook
   - **Sprint** — sprint tree if your project maintains `state.json`
   - **Tests** — pass/fail/skip counts
   - **Git** — branch + dirty flag
4. Tab badge pulses amber if no hook activity for >2 s.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Status sub-tab with the same card layout. Badge state matches
the PWA.

### 5b. REST

```sh
# POST a hook event (normally done by the hook scripts, not operators).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"event":"Stop","payload":{"current_task":"tests green","tests":{"pass":14,"fail":0}}}' \
  $BASE/api/sessions/$SID/hook-event

# GET the derived status board.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/sessions/$SID/status
#  → { "current_focus": "...", "sprint": {...}, "tests": {...}, "git": {...} }

# GET raw event stream (last 50).
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/sessions/$SID/hook-events
```

### 5c. MCP

Tools: `session_hook_status` (check install state), `session_post_hook_event`
(inject a test event), `session_status` (read the derived board).

### 5d. Comm channel

| Verb | Example |
|---|---|
| `session status <id>` | Returns the current board as a card. |

### 5e. YAML

```yaml
# ~/.datawatch/datawatch.yaml
session:
  auto_install_hooks: true       # default; set false to disable globally
  hook_script_dir: ""            # override: path to custom post-event.sh
```

## Manual setup

If auto-install is disabled or you need a custom hook script:

```json
// <project_dir>/.claude/settings.json (append to existing hooks)
{
  "hooks": {
    "Stop": [
      { "type": "command", "command": "/absolute/path/.claude/sprint/post-event.sh Stop" }
    ],
    "PostToolUse": [
      { "type": "command", "command": "/absolute/path/.claude/sprint/post-event.sh PostToolUse $TOOL_NAME" }
    ],
    "UserPromptSubmit": [
      { "type": "command", "command": "/absolute/path/.claude/sprint/post-event.sh UserPromptSubmit" }
    ]
  }
}
```

```bash
# <project_dir>/.claude/sprint/post-event.sh
#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/.dw-env"
EVENT="$1"; TOOL="${2:-}"
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

```bash
# <project_dir>/.claude/sprint/.dw-env  (chmod 600)
DAEMON_URL=https://localhost:8443
SESSION_ID=<full-session-id>
TOKEN=<daemon-bearer-token>
```

## Rich payload fields

The board renders a card for each field present in the payload:

| Field | Card | Example |
|---|---|---|
| `current_task` | Current focus | `"refactoring router"` |
| `sprint` | Sprint tree | full `.claude/sprint/state.json` |
| `tests` | Tests | `{"pass": 12, "fail": 0, "skip": 1}` |
| `git` | Git | `{"branch": "feat/x", "dirty": true}` |

Fields not present in the payload are omitted from the board.

## Common pitfalls

- **Hooks not firing.** Check `chmod +x post-event.sh` and that
  the absolute path in `settings.json` is correct. Claude Code
  requires executable scripts.
- **`.dw-env` not found.** The script sources it relative to itself
  (`$(dirname "$0")/.dw-env`). Don't move `post-event.sh` without
  also moving `.dw-env`.
- **Token expired.** `.dw-env` is written at session spawn with the
  current token. If you rotate the token mid-session, re-install
  manually or restart the session.
- **Opt-out globally but want it for one session.** Override per-spawn:
  `datawatch sessions start --hooks` (opposite of `--no-hooks`).

## Linked references

- See also: [`sessions-deep-dive.md`](sessions-deep-dive.md) — session detail tabs.
- See also: [`daemon-operations.md`](daemon-operations.md) — token rotation.
- Architecture: `../architecture-overview.md` § Hook pipeline.

## Screenshots

![Session detail — Status sub-tab (hook board: current focus, sprint, tests, git)](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/session-detail-status.png)

<!-- Screenshots still needed: Status tab with amber pulse badge (idle > 2s), CLI output -->

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [howto/daemon-operations](daemon-operations.md)
