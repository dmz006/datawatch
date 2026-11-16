# feat(i18n): add the held-Automaton keys for operator hold parity (datawatch v9.177.0)

Parent release: datawatch v9.177.0

## What changed

An operator can put an Automaton on hold (paused, not blocked): a new Held status with a reason, an optional condition (recorded, never evaluated) and an optional look-again date. Held Automata show in an Inbox group, expire into a ready-to-review state, and can be released or extended.

## Screens

- Automata list and detail: a Held status chip, and Hold, Release and Extend actions with a dialog for reason, condition and look-again date.
- Inbox: a Held Automata group (the same card that lists work waiting on a decision), with an empty and a load-failed state.
- Progress card: a Held column.

## Settings to add to the mobile app

None.

## REST contract from routes added

- `/api/autonomous/held`
- `/api/autonomous/held/migrate`
- `/api/autonomous/prds/{id}/hold`
- `/api/autonomous/prds/{id}/release_hold`

## New locale keys with English values

| Key | English |
|---|---|
| `automata_action_extend` | Extend |
| `automata_action_extend_tip` | Extend the hold of this held Automaton |
| `automata_action_hold` | Hold |
| `automata_action_hold_tip` | Put this Automaton on hold |
| `automata_action_release` | Release |
| `automata_action_release_tip` | Release this held Automaton |
| `automata_status_held` | Held |
| `automation_help_held_automaton` | Help: Putting an Automaton on hold |
| `automation_inbox_help` | The work that has stopped and is waiting: Automata waiting on a decision, a hold, or blocked on something outside. |
| `automation_inbox_title` | Inbox |
| `automaton_hold_action` | Hold |
| `automaton_hold_at` | Held at |
| `automaton_hold_by` | Held by |
| `automaton_hold_condition_label` | Condition |
| `automaton_hold_condition_prompt` | Condition (optional — recorded, never evaluated) |
| `automaton_hold_expired` | Hold expired — ready to review |
| `automaton_hold_extend` | Extend |
| `automaton_hold_failed` | Hold action failed: %1$s |
| `automaton_hold_no_date` | No look-again date set |
| `automaton_hold_reason_label` | Reason |
| `automaton_hold_reason_prompt` | Hold reason (required) |
| `automaton_hold_reason_required` | A reason is required for the hold |
| `automaton_hold_release` | Release |
| `automaton_hold_release_ask` | Release this held Automaton now? |
| `automaton_hold_toast_extended` | Hold extended |
| `automaton_hold_toast_released` | Hold released |
| `automaton_hold_toast_set` | Automaton is on hold |
| `automaton_hold_tooltip_extend` | Extend this hold (change reason, condition or date) |
| `automaton_hold_tooltip_hold` | Put this Automaton on hold (paused, not blocked) |
| `automaton_hold_tooltip_release` | Release this held Automaton now |
| `automaton_hold_until_label` | Look again |
| `automaton_hold_until_prompt` | Look at again (optional — leave empty for no date) |
| `held_automata_inbox_empty` | No Automata are on hold |
| `held_automata_inbox_failed` | Couldn't load held Automata: %1$s |
| `held_automata_inbox_title` | Held Automata |
| `progress_card_col_held` | Held |

## Screens

- `internal/server/web/app-automation-subtabs.test.js`
- `internal/server/web/app-automation-tab-registry.test.js`
- `internal/server/web/app-hold.test.js`
- `internal/server/web/app-lifecycle-subscriptions.test.js`
- `internal/server/web/app-notify-events.test.js`
- `internal/server/web/app-progress-card.test.js`
- `internal/server/web/app.js`
- `internal/server/web/openapi.yaml`
- `internal/server/web/style.css`
