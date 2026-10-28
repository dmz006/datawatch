# feat(settings): add stuck_running_seconds setting parity

Parent release: datawatch 9.29.2 (the next patch above main at the time of writing).

## What changed in datawatch

A daemon watchdog now catches an Automaton that sits in `running` with every task finished and no active session. After `autonomous.stuck_running_seconds` the daemon re-runs the completion rollup once. If the Automaton is still stuck after a second full bound it moves to `blocked`, with a recorded decision, an audit entry and a status event. The bound is a new setting on the Automata card of the PWA Settings page.

## Setting to add to the mobile app

| Item | Value |
|---|---|
| Config key | `autonomous.stuck_running_seconds` |
| REST field | `stuck_running_seconds` under `autonomous` in `GET`/`PUT /api/config` |
| Label | Stuck Automaton timeout (seconds) |
| Type | whole number, seconds |
| Default | 900 (shown as the placeholder; 0 or empty means use 900) |
| Range | 0 or more; no upper bound (0 = default) |
| Placement | Automata settings card, next to the other watchdog bounds (worker idle nudge, orphan grace) |

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `settings_stuck_running_seconds` | Stuck Automaton timeout (seconds) | Stuck run timeout (seconds) |
| `settings_stuck_running_seconds_help` | How long an Automaton may stay running with every task finished and no live session before the watchdog re-checks it once, then blocks it. 0 uses the default (900). | How long a running item may stay running with every task finished and no live session before the watchdog re-checks it once, then blocks it. 0 uses the default (900). |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization Rule requires. The placeholder avoids the word "Automaton" because the Terminology Rule guard (`TestLocales_AutomatonNeverUntranslatedOrMistranslated`) refuses that English word in those bundles. Please send translations of both keys for DE, ES, FR and JA through the Compose Multiplatform pipeline, using the app's own word for "Automaton"; the PWA bundles will then mirror them in place of the placeholders.

## Acceptance

- The field appears in the app's Automata settings, reads the current value from the daemon config and writes it back through the config endpoint.
- Label and help text are localized in EN, DE, ES, FR and JA.
- An Automaton that the watchdog moves to `blocked` already shows as blocked through the existing status event; no new status value is added.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/290 (2026-10-28, via `gh issue create --repo dmz006/datawatch-app`).
