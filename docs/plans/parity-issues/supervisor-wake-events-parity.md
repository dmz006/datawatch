Parent release: datawatch v9.159.0

## What changed in datawatch

The supervisor can now wake on Automaton lifecycle events instead of waiting for its next scheduled check (off by default, the schedule stays as the fallback). Lifecycle events are subscribable from more places: the web subscription form has an event picker with a last-day preview, a daemon can read a federated peer's lifecycle events, and an MCP client can subscribe to the lifecycle events resource. A forwarded event now names the peer it came from.

## New config keys

Returned and accepted by `GET` and `PUT /api/config` as dotted keys. The first four are shown in Settings > Automata under "Supervisor wake"; the last two are in the matching Settings groups.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.supervisor_loop.wake.enabled` | bool | `false` | An event on the list starts a supervisor pass at once. |
| `autonomous.supervisor_loop.wake.kinds` | list of event kinds | blocked, review needed, failed task or final gate, judge verdicts, all tasks done or cancelled, stuck running, long capacity wait, burn guard rung change | Which lifecycle kinds wake a pass. |
| `autonomous.supervisor_loop.wake.coalesce_seconds` | int 0 to 3600 | `30` | Events inside this window fold into one pass. |
| `autonomous.supervisor_loop.wake.fallback_interval_minutes` | int 1 to 1440 | `20` | Minutes between scheduled passes while the wake is on. |
| `autonomous.notify.forwarding.peer_events` | bool | `false` | Lets this daemon read a peer's lifecycle events. |
| `mcp.resource_subscriptions` | bool | `false` (restart needed) | Offers MCP resource subscriptions for the lifecycle events resource. |

## New endpoints

- `GET /api/supervisor-loop/wake`: settings in effect, last event wake and last scheduled check with counts (drives the status line on the Settings card).
- `GET /api/autonomous/event-kinds` (optional `group`): the lifecycle event kinds, grouped Automaton, Task, Review, Capacity, Budget, for the event picker.
- Peer event subscriber control and read (MCP `autonomous_peer_events`, `autonomous_peer_events_control`; chat `autonomous peer-events`).
- `POST /api/autonomous/subscriptions/preview` now counts lifecycle kinds.

## What the mobile app needs

- A Supervisor wake settings group with the switch, kind list, quiet window, fallback interval and the status line from `GET /api/supervisor-loop/wake`; the two other keys as toggles.
- The subscription form's event picker reading `GET /api/autonomous/event-kinds`, with the last-day preview from the preview endpoint.
- Show the `peer` field on a forwarded event.
- All new strings in the five locales (about 30 were added to the web locale files).
