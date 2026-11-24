# [sync] Burn guard pace policy: shadow and enforce, a pace block on the status (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

The burn guard now has a hard line beside its ladder. Each interval, for every guarded Claude entry, the **pace policy** decides from the readings as they stand whether new work should be held: the current window is at the operator's pause line (`window_stop_percent`, 85 by default) or the week is more than `week_tolerance` points ahead of the weekly pace line. The `usage.burn_guard.mode` setting, which already existed on every surface, now does something:

- `off` decides nothing.
- `shadow` (the default, and what an unset value reads) computes the decision and records it with its evidence as `would_hold` or `would_release`; admission is never touched.
- `on` holds new worker admission for that entry and lifts the hold when the reading drops or the window or the week resets. Each change raises an alert.

Running work always finishes, its verifier and judge are not held, and operator and supervisor sessions are never held. Leaving `on` releases the hold at once.

No new route, tool, CLI command or setting key was added. The existing status route grows a `pace` block, and the web app, the dashboard and the chat reply draw it.

## Changed REST contract

### `GET /api/usage/burn-guard` (new `pace` key)

```json
{
  "pace": {
    "mode": "off | shadow | on",
    "enforcing": false,
    "held": false,
    "entries": [
      {
        "llm": "claude-code",
        "held": false,
        "would_hold": true,
        "rule": "window_pause_line | weekly_pace_line (key absent when nothing is held)",
        "since": "RFC 3339 time (key absent unless a real hold is in force)",
        "last": { "...": "a decision, below" }
      }
    ],
    "log": [ { "...": "a decision, newest first, at most 100" } ]
  }
}
```

A decision: `at`, `entry`, `mode`, `hold` (the verdict, whatever the mode), `rule`, `action` (`none`, `hold`, `release`, `would_hold`, `would_release`), `held_after`, `reason`, and the evidence: `week_read_at`, `week_percent`, `week_points_since`, `week_estimated`, `week_line`, `week_tolerance`, `week_reset`, `window_read_at`, `window_percent`, `window_points_since`, `window_estimated`, `window_stop_percent`, `window_start`, `window_end`, `window_reset`, `reading_age_minutes`, `stale`. Time keys with no value and `rule` are absent, never `null`. `pace` itself is absent on a daemon older than this release; a client must treat an absent `pace` as "no pace policy".

## What the mobile app should do

- Burn guard card: add a block under the existing state line. Show the mode (`off`, `shadow` as "records only", `on` as "enforcing"). For each entry with `held` true show "new work is held" with the rule and `since`; for `would_hold` true in shadow show "would hold, nothing is held". When no entry is held say so.
- Show the five newest `log` entries: time, action, rule, reason. The reason already carries the numbers.
- The `mode` setting already has a select in the app's settings; its help text changes (off decides nothing, shadow records, on holds).
- Strings: the web app's locale keys `burn_guard_pace_*` (en, de, es, fr, ja) are the reference wording.

## Gaps

- The stale-reading flag is shown in the decision's reason only; there is no separate "send a fresh reading" prompt yet.
- The hold covers new worker admission. Planner sessions are not held, and a supervisor class with its own reserved buffer is a separate piece of work.
