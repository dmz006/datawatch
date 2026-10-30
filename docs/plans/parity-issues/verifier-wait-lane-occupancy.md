Issue: dmz006/datawatch-app#301

# [sync] Lane fill verifier-wait lanes: new REST state and fields, MCP, CLI, comm, PWA lane view, count_verifier_wait setting and three locale keys

## Body of the issue

Lane fill learned a new lane state. A Claude lane whose Automaton has running work with every piece blocked waiting for a verifier slot is now reported as `waiting_verifier`, and the lane fill planner counts it as available under the new `autonomous.lane_fill.count_verifier_wait` setting (on by default). The app already renders the lane fill status view; this records the exact contract to render, the setting to expose, and the locale keys for the labels.

### New REST fields on the existing endpoints

`GET /api/autonomous/lane-fill`:

| Where | Field | Notes |
|---|---|---|
| `lanes[]` | `state` | was `idle` / `occupied`; now also `waiting_verifier`: the lane is occupied, its occupant's work is all blocked waiting on a verifier slot, and the planner counts the lane as available under `count_verifier_wait` |
| `lanes[]` | `waiting_on_verifier` | boolean, omitted when false: the lane's occupant is blocked waiting on a verifier slot |
| `lanes[]` | `wait_reason` | string, omitted when absent: the occupant's readable wait reason |
| `lanes[].occupant` | `waiting_on_verifier` | boolean, omitted when false: same flag on the occupant object |
| `lanes[].occupant` | `wait_reason` | string, omitted when absent: the occupant's readable reason text |
| `decisions[]` | `wait_reason` | string, omitted when absent: the wait reason a planner decision made on such a lane rests on |

Everything else is unchanged (`mode`, `last_poll_at`, `lanes[].id` / `.type` / `.occupant` / `.blocker` / `.eligible_waiting`, `queued_eligible`, `evidence`, `decisions[]`, `alerts[]`). Unknown fields are ignored, as before.

`GET` / `PUT /api/config` (under `autonomous.lane_fill`):

| Field | Notes |
|---|---|
| `count_verifier_wait` | boolean, default `true`; set `false` to make such lanes count as busy again |

### MCP

- Tool `autonomous_lane_fill_status` returns the same object as `GET /api/autonomous/lane-fill`, so the fields above appear in `lanes[]` and `decisions[]` exactly as listed. No new tool is added; the setting travels through the existing config tools.

### CLI

- `datawatch autonomous lane-fill`: a lane's line now reads `occupied by <id> "<title>" (<backend>), waiting on verifier: <reason>` when the occupant flag is set (`, waiting on verifier` when the reason is empty); a decision's line carries its `wait_reason` when present.
- `datawatch config set autonomous.lane_fill.count_verifier_wait true|false` sets the key.

### Comm channel

- `autonomous lane-fill` carries the same compact per-lane lines, including the `, waiting on verifier: ...` suffix.
- `configure autonomous.lane_fill.count_verifier_wait=<bool>` sets the key.

### PWA lane view (reference render)

- The lane fill card shows, per lane: `id · type`, then the state label — "Waiting on verifier" for state `waiting_verifier` (otherwise idle / occupied); when the lane waits on a verifier, a reason line "Waiting on verifier: `<reason>`" (lane `wait_reason`, falling back to the occupant's); the occupant's `title · backend`; the eligible-waiting count; the blocker text unchanged.
- The Automata settings block carries the `autonomous.lane_fill.count_verifier_wait` toggle next to the other lane fill settings.

### Setting to add to the mobile app

| Item | Value |
|---|---|
| Config key | `autonomous.lane_fill.count_verifier_wait` |
| REST field | `count_verifier_wait` under `autonomous.lane_fill` in `GET` / `PUT /api/config` |
| Label | Lane fill count verifier wait (count a Claude lane whose Automaton waits on a verifier slot as available, off keeps the lane busy; on by default) |
| Type | boolean |
| Default | `true` |
| Placement | the lane fill settings fields, next to the other lane fill settings |

### New locale keys

| Key | EN value | de / es / fr / ja (placeholder until translated) |
|---|---|---|
| `lane_fill_state_waiting_verifier` | Waiting on verifier | same English text as a placeholder for now |
| `lane_fill_waiting_verifier_reason` | Waiting on verifier: %1$s | same English text as a placeholder for now |
| `settings_lane_fill_count_verifier_wait` | Lane fill count verifier wait (count a Claude lane whose Automaton waits on a verifier slot as available, off keeps the lane busy; on by default) | same English text as a placeholder for now |

Please send translations of the three keys into DE, ES, FR and JA (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- The lane fill status view renders the `waiting_verifier` state with its own label (not as occupied or idle), shows the reason line when `wait_reason` is present (lane field, falling back to the occupant's), and shows the occupant's title and backend.
- A decision's `wait_reason` renders as part of the decision detail, read-only.
- The `count_verifier_wait` toggle reads the current value from the daemon config and writes it back through the config endpoint; absent field falls back to the `true` default.
- State label, reason line and settings label are localized in EN, DE, ES, FR and JA.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible.

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state open --search 'verifier wait lane'` on 2026-10-29 returned nothing, so no earlier issue was commented on.

Filed: https://github.com/dmz006/datawatch-app/issues/301 (2026-10-29, via `gh issue create -R dmz006/datawatch-app --title "[sync] Lane fill verifier-wait lanes: new REST state and fields, MCP, CLI, comm, PWA lane view, count_verifier_wait setting and three locale keys" --body-file <scratch draft>`). Filed exactly once.
