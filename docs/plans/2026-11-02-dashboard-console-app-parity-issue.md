# Datawatch App — operator console parity brief (mobile)

**Proposed issue title:** `feat(dashboard): operator console parity`

Draft — **do not file yet**. This is the parity brief for the next
`dmz006/datawatch-app` pass. The datawatch PWA ships the operator console;
the companion app (Android + iOS) does not render any of it yet, and it must
not break on the new config keys.

## What shipped on the PWA

The Dashboard view carries up to five **console cards** —
**Questions waiting**, **Held & blocked**, **Lanes**, **Usage**, **CI &
release** — the one-glance "what needs my attention right now" summary of
the daemon. Each card opens the PWA anchor of the section it rolls up
(`#questions`, `#held-questions`, `#lanes`, `#usage`, `#releases`) and
carries a help link to the operator console how-to.

- The summary body comes from `GET /api/dashboard/console` (read-only,
  `dashboard:read` capability for federated reads).
- Five sections, fixed order: `questions_waiting`, `held_blocked`, `lanes`,
  `usage`, `ci_release`. Each section reports `available`, `count`,
  `summary` and `link`.
- A section gated off by a switch, or whose backing subsystem this daemon
  does not run, returns `available: false` with a `reason` naming the key —
  the read itself never 5xxs.
- `dashboard.console.enabled: false` drops the console cards from the board;
  a stored layout is rewritten once to add them back when the console is
  switched on again.
- Locale keys (`dash_console_questions`, `dash_console_held`,
  `dash_console_lanes`, `dash_console_usage`,
  `dash_console_ci_release`, plus the `*_link` variants) are present in all
  five PWA bundles.

## The seven config keys

All booleans; every one reads its default, **true**, while absent; an
explicit `false` sticks through save/load and YAML round trips.

| Key | Gates |
|---|---|
| `dashboard.console.enabled` | the console as a whole |
| `dashboard.console.show_questions` | the `questions_waiting` section |
| `dashboard.console.show_held_blocked` | the `held_blocked` section |
| `dashboard.console.show_lanes` | the `lanes` section |
| `dashboard.console.show_usage` | the `usage` section |
| `dashboard.console.show_ci_release` | the `ci_release` section |
| `dashboard.hide_inactive_cards` | dashboard feature cards with no live data stay hidden from the grid and the Add-Card picker (default true) |

## Endpoints and surfaces (datawatch side)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/dashboard/console` | the five-section summary (read-only) |
| GET/PUT | `/api/config` | read/save the seven `dashboard.*` switches |
| MCP | `dashboard_console_get` | the same body as the REST route |
| CLI | `datawatch dashboard console` | one line per section; `--json` for the raw body |
| CLI | `datawatch dashboard console set <key> <value>` | set a switch (same `PUT /api/config` path) |
| Chat | `dashboard console` | the one-liner; `configure dashboard.console.enabled=false` |

Example body:

```jsonc
{
  "generated_at": "2026-11-02T10:15:00Z",
  "questions_waiting": { "available": true,  "count": 2, "summary": "2 open (1 blocking)", "link": "#questions" },
  "held_blocked":      { "available": true,  "count": 1, "summary": "1 Automaton held",    "link": "#held-questions" },
  "lanes":             { "available": true,  "count": 1, "summary": "1 lane filling",       "link": "#lanes" },
  "usage":             { "available": true,  "count": 0, "summary": "under the pace line",  "link": "#usage" },
  "ci_release":        { "available": false, "reason": "hidden by the dashboard.console.show_ci_release switch — set it to true to show this section", "link": "#settings" }
}
```

## Acceptance criteria (for the app)

1. **A console section on the dashboard-equivalent screen** that lists the
   five sections in the endpoint's order, each with its count, summary line
   and a tap-through to where the operator acts on it (the app's equivalent
   of `#questions`, `#held-questions`, `#lanes`, `#usage`, `#releases`).
2. **`available: false` sections render as an off row with the reason**
   (which names the switch), never as an error state; the read never 5xxs,
   so a missing subsystem must not blank the screen.
3. **The seven switches parse** out of `GET /api/config` (absent keys read
   true) and, if the app gets a settings row for them, save an explicit
   false without dropping it.
4. **The app does not break on the `dashboard` config subtree** it does not
   understand: existing screens must render identically before and after
   the key exists.
5. **Both Android and iOS** carry the same section (one shared screen if
   the app can); locale strings in both apps.
6. A help link to the operator console how-to where the app already links
   PWA how-tos.

## References

- datawatch how-to: [`docs/howto/dashboard-operator-console.md`](../howto/dashboard-operator-console.md)
- datawatch parity row: [`docs/parity-status.md`](../parity-status.md)
- datawatch testing tracker section: "Operator console" in [`docs/testing-tracker.md`](../testing-tracker.md)
- After filing: record the `app#…` number in the datawatch parity row and
  link this note from it, matching the other `[sync]` parity notes.
