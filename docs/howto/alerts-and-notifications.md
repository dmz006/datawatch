---
docs:
  index: true
  topics: [alerts, notifications, alert-dock]
exec_params: []
exec_steps:
  - tool: get_alerts
    description: List all pending alerts (none = healthy)
    args: {}
    read_only: true
  - tool: mark_alert_read
    description: Mark all alerts as read
    args: {all: true}
    read_only: false
---
# How-to: Alerts + Notifications

The persistent alert dock is the single notification surface in
datawatch. Every system event, session state change, LLM response, and
operator-directed message lands there. There are no ephemeral pop-up
toasts; every alert persists until you dismiss it.

## What it is

The alert dock has two visible components that are always present in
the PWA:

1. **Header badge** (`🔔 N`) — lives in the global header bar on
   every page. Shows the total unread count. Dimmed when the count is
   zero; full-color accent when one or more alerts are waiting.
   Clicking it opens the dock panel. When muted, shows `🔕 muted`.

2. **Dock panel** — a floating slide-out panel anchored to the
   top-right of the app frame. Displays up to 100 alerts grouped by
   type, with per-card dismiss and a scrollable body. Opens when you
   click the header badge or when a new alert arrives while the panel
   is already open.

Alerts are stored server-side (`~/.datawatch/alerts.json`), survive
daemon restarts, and are available over REST, MCP, and the comm
channel — not just in the PWA.

## Base requirements

- `datawatch` daemon running and reachable at `https://<host>:8443`.
- PWA open in a browser (for the visual dock and header badge).
- Any REST client or comm channel connection for the non-visual
  surfaces.
- For mobile push alerts (`waiting_input` and `error` events): a paired mobile device is required. See [push-notifications.md](push-notifications.md) for device pairing setup.

No extra config is required. The alert store is initialized
automatically on first `datawatch start`.

## Setup

Nothing to configure for basic alerts. The store is on by default.

To enable mobile push for alert events, pair the datawatch mobile app
(`datawatch setup link` or Settings → Pair device) so that
`waiting_input` and `error` alerts also push to your phone.

## Two happy paths

### 4a. Happy path — CLI

```sh
# List all alerts (unread marked with *).
datawatch alerts
#  → 3 alerts (2 unread):
#
#   * [info]  2026-05-10 14:03:11 — [myproject] Session started
#   * [warn]  2026-05-10 14:04:22 — Backend responded slowly (3.2s)
#     [info]  2026-05-10 13:55:00 — Daemon restarted

# List only system-level alerts (pipeline / plugin / backend failures).
datawatch alerts --system

# Mark one alert read.
datawatch alerts --mark-read <id>

# Mark all alerts read.
datawatch alerts --mark-all-read
```

### 4b. Happy path — PWA

1. **Check the header badge.** The `🔔 N` badge in the top-right
   corner of the header bar shows the live unread count. The badge is
   always visible regardless of which page you are on.

2. **Open the dock.** Click the badge. The dock panel slides open
   from the top-right. The collapsed header shows:
   - Total alert count + per-type chips (e.g. `error ×2  warn ×1  info ×5`)
   - A `⌃` / `⌄` chevron to expand or collapse the card list
   - `✕` — dismiss the dock panel (new alerts will re-spawn it)
   - `🔕` — mute all alerts for this browser session

3. **Read and dismiss individual alerts.** With the panel expanded,
   each alert card shows:
   - Color rail on the left edge (red = error, amber = warning,
     green = success, blue = info)
   - Alert type badge, timestamp, and the message text
   - `×N` coalesce badge when the same alert repeated N times
   - `✕` dismiss button on the top-right of the card
   - Long messages (> 140 chars or multi-line) are clamped to 3 lines
     with a `▸ more` toggle to expand

4. **Alerts tab.** Navigate to the **Alerts** page from the sidebar.
   This is the full-screen alert manager:
   - Three tabs: **Active**, **Historical**, **System**
   - Filter chips: all / prompts / errors / warn / info
   - Search bar (searches title + body)
   - Sort toggle: by-session (sessions waiting for input shown first)
     or chronological (newest-first flat list)
   - Sessions sorted: `waiting_input` → `running` → `completed`
   - Prompt alerts include a **Quick reply** dropdown for saved commands
   - `✕ all` dismisses everything; `🔕` mutes for the session; `↻` refreshes

5. **Mute and un-mute.** Click `🔕` to suppress all incoming alerts
   for the current browser tab. The badge shifts to `🔕 muted` style.
   Mute is stored in `sessionStorage` — it clears when you close the
   tab. To un-mute, click the muted badge; the dock opens and mute is
   cleared automatically.

## Other channels

### 5a. Mobile (Compose Multiplatform)

The datawatch mobile app (Android / Wear OS) receives push
notifications for `waiting_input` and `error`-level alerts via the
push-token registry. The Alerts screen on mobile mirrors the Active +
Historical + System tabs. Wear OS support is a separate planned effort
— see the datawatch-app issue tracker.

### 5b. REST

```sh
export BASE=https://localhost:8443
export TOKEN=<your_bearer_token>

# List all alerts.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alerts"
#  → {"alerts": [...], "unread_count": 2}

# List only system alerts.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alerts?source=system"

# List alerts for a specific session.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/alerts?source=<session_id>"

# Mark one alert read.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"<alert_id>"}' \
  "$BASE/api/alerts"

# Mark all alerts read.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"all":true}' \
  "$BASE/api/alerts"
```

Response schema (per alert):

```json
{
  "id":         "string",
  "level":      "info|warn|error|success",
  "title":      "string",
  "body":       "string",
  "source":     "string",
  "session_id": "string",
  "created_at": "2026-05-10T14:03:11Z",
  "read":       false
}
```

### 5c. MCP

MCP tools for alerts are callable from any MCP host (Claude Code,
Cursor, VS Code, etc.):

| Tool | Description |
|------|-------------|
| `get_alerts` | List alerts; optional `limit` and `session_id` filters |
| `mark_alert_read` | Mark one alert read (`id`) or all (`all: true`) |

Example (Claude Code):

```
get_alerts limit=20
mark_alert_read all=true
```

Useful for an LLM coordinator that polls for `waiting_input` alerts
before deciding to send a reply to a blocked session.

### 5d. Comm channel

Send these messages via any connected comm channel (Telegram, Signal,
Discord, Slack, Matrix, etc.):

| Verb | Description |
|------|-------------|
| `alerts` | Last 5 alerts across all sessions |
| `alerts 20` | Last N alerts |
| `alerts system` | System-level alerts only (pipeline / plugin / backend failures) |

Example Telegram session:

```
/alerts
→ 3 alerts (2 unread):
  [warn] 14:04 — Backend responded slowly (3.2s)
  [info] 14:03 — [myproject] Session started
  [info] 13:55 — Daemon restarted
```

### 5e. YAML

No per-alert YAML configuration is required. Alerts are emitted
automatically by the daemon internals (session starts, input-waiting
events, backend errors, pipeline failures, plugin hook results).

The one optional YAML knob is mobile push routing — alerts tagged with
the `waiting_input` event are automatically published to the `alerts`
topic for paired mobile devices:

```yaml
# ~/.datawatch/datawatch.yaml
# Nothing to add for basic alerts.
# Mobile push is wired by pairing a device:
#   datawatch setup link
# After pairing, waiting_input + error alerts push automatically.
```

To disable push for a specific session backend, use the per-deployment
`detection` block (see `channel-state-engine.md`).

## Diagram

```
  ┌───────────────────────────────────────────────────┐
  │  PWA / Browser tab                                │
  │                                                   │
  │  ┌──────────────────────────────────────────┐     │
  │  │  Global header bar            [ 🔔 3 ] ◄─┼─── badge (always-on)
  │  └──────────────────────────────────────────┘     │
  │                                         ▼         │
  │                          ┌──────────────────────┐ │
  │                          │  Alert dock panel    │ │
  │                          │  🔔 3 alerts         │ │
  │                          │  error ×1  warn ×2   │ │
  │                          │  ─────────────────── │ │
  │                          │  ✕ ERROR 14:04       │ │
  │                          │  Backend timeout     │ │
  │                          │  ─────────────────── │ │
  │                          │  ⚠ WARN  14:03  ×2   │ │
  │                          │  Slow response       │ │
  │                          └──────────────────────┘ │
  └───────────────────────────────────────────────────┘

  datawatch daemon
    │
    ├─ alerts.json          (persistent store, survives restart)
    │
    ├─ GET  /api/alerts     → list + unread_count
    ├─ POST /api/alerts     → mark read (id or all:true)
    │
    ├─ MCP  get_alerts      → same list, MCP clients
    ├─ MCP  mark_alert_read → same mark-read, MCP clients
    │
    ├─ CLI  datawatch alerts [--system] [--mark-read <id>]
    │                        [--mark-all-read]
    │
    └─ Comm  alerts [N] [system]
```

## Common pitfalls

- **Badge shows 0 but alerts are there.** The badge reflects the
  dock's in-memory state synced from the server over WebSocket. If
  the connection dropped, click the badge — the dock opens and
  refreshes from REST. Reconnect banners and "Disconnected" events
  are cleared from the dock automatically when the connection is
  restored.

- **Dock panel disappears and re-spawns every time.** Clicking `✕`
  dismisses the panel and clears all in-memory alerts — but new
  alerts will re-spawn it as they arrive. If you want silence for a
  session, use `🔕` mute instead of `✕` dismiss.

- **Mute didn't carry over after closing the tab.** Mute is stored in
  `sessionStorage`, which is per-tab and cleared on tab close. This
  is by design. There is no persistent mute; re-mute if you open a
  new tab.

- **`datawatch alerts` returns "daemon not reachable".** The CLI
  reads the port from your `~/.datawatch/datawatch.yaml`. If the
  daemon is on a non-default port, ensure your config reflects it, or
  set `DATAWATCH_URL` in the environment.

- **`get_alerts` MCP tool returns nothing.** The alert store must be
  initialized — it is created on first daemon start. If you're calling
  via MCP against a freshly initialized daemon that has never started,
  start the daemon once first.

- **System alerts not appearing.** System alerts require a failure
  path to fire (pipeline error, plugin hook error, eBPF loader
  failure, memory backend degradation). Use `datawatch alerts
  --system` on a healthy system and expect an empty list.

- **Mobile push not firing for `waiting_input`.** Pair the device
  first (`datawatch setup link`) and verify the pairing in
  Settings → Devices. Push only fires for `waiting_input` and `error`
  level events; `info` and `warn` are not pushed to mobile by default.

## Linked references

- [`comm-channels.md`](comm-channels.md) — configure Telegram, Signal,
  Discord, Slack, and other channels that carry the `alerts` comm verb.
- [`daemon-operations.md`](daemon-operations.md) — daemon start / stop /
  restart; the alert store lives at `~/.datawatch/alerts.json`.
- [`datawatch-definitions.md`](../datawatch-definitions.md) — glossary
  of system terms including alert levels and session states.
- [`sessions-deep-dive.md`](sessions-deep-dive.md) — `waiting_input`
  state that triggers prompt-category alerts.
- [`channel-state-engine.md`](channel-state-engine.md) — state
  transitions that generate system events surfaced as alerts.

## Screenshots needed (operator weekend pass)

- [ ] Header badge in global nav bar: count = 0 (dimmed), count = 3 (full-color), muted state
- [ ] Dock panel open with mixed error/warn/info cards (expanded, showing ×N coalesce and ▸ more toggle)
- [ ] Alerts tab — Active sub-tab with prompt card highlighted yellow and quick-reply dropdown
- [ ] Alerts tab — System sub-tab with pipeline failure alert
- [ ] Mobile app Alerts screen (Android) — Active tab with waiting_input push badge

---

## See also

- [howto/comm-channels](comm-channels.md)
- [howto/daemon-operations](daemon-operations.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [datawatch-definitions](../datawatch-definitions.md)
