Issue: dmz006/datawatch-app#393

Issue URL: https://github.com/dmz006/datawatch-app/issues/393

# [sync] usage calculator: auto-lanes settings and status

## Body of the issue

The daemon can now apply the usage calculator's lane advice to the Claude lanes it runs. The companion apps should show the setting and the status, and must not break on the new fields. This follows the calculator issue [#286](https://github.com/dmz006/datawatch-app/issues/286).

## Request shapes

`GET /api/usage/autolanes` — read-only, no parameters. Settings are written through the normal config path:

```json
{"key": "usage.calculator.auto_lanes", "value": true}
```

| Key | Type | Notes |
|---|---|---|
| `usage.calculator.auto_lanes` | boolean | default false |
| `usage.calculator.auto_lanes_max` | integer 1 to 8 | operator maximum, default 2 |
| `usage.calculator.auto_lanes_special_priority` | integer 0 or above | default 1 |

## Response shapes

The status carries: enabled, the policy in force, the operator maximum, the cap the daemon applies now (absent when no capped backend), what a fresh advice would do to that cap, the settings' last change, and the recent decisions. Unknown or absent fields must render as today.

## Android, iOS and Wear implications

- **Android:** add the toggle and the maximum to the usage settings, and a status line ("Auto-lanes on/off, current cap N"). Treat a missing cap as "no capped backend".
- **iOS:** the same contract. The iPhone app has no calculator screen yet (tracked in #286), so this joins that work.
- **Wear:** no change.

## Locale keys that need Android translations

The PWA bundles already carry de, es, fr and ja text; Android should check its translations against the `settings_usage_calculator_auto_lanes*` and `settings_usage_calculator_autolanes_*` keys. Use the app's own word for Automaton in operator text; never "PRD".

## Acceptance (mobile)

- The toggle and maximum read and write the keys above; a refused value shows the daemon's message.
- The status line shows on/off and the cap, or the no-capped-backend line.
- An unknown status field never raises an error.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | Changed | New status read |
| MCP | Changed | `usage_autolanes_status` |
| CLI | Changed | `usage auto-lanes` |
| Comm channel | Not touched | Existing config verbs |
| YAML/config | Changed | Three keys |
| PWA | Changed | Controls and status line |
| Android | Pending, issue filed ([#393](https://github.com/dmz006/datawatch-app/issues/393)) | This text |
| iPhone | Pending, issue filed ([#393](https://github.com/dmz006/datawatch-app/issues/393)) | Joins the calculator work |

## Filing record

Filed 2026-11-07 as [datawatch-app#393](https://github.com/dmz006/datawatch-app/issues/393) (the body there is this text from the Body of the issue heading on). The related calculator issue is [#286](https://github.com/dmz006/datawatch-app/issues/286); the iPhone app work joins it.