# feat(i18n): add the unlanded-work count and alert keys for parity (datawatch v9.178.0)

Parent release: datawatch v9.178.0

## What changed

Automaton work that never reached main is now counted and reported. The daemon counts Automaton branches that are not on main by state (landed, pending, live, unlanded) and by age, names the oldest pending branch, and raises one alert a day when the pending count is above a threshold.

## Settings to add to the mobile app

- `unlanded_alert_enabled`
- `unlanded_alert_threshold`

## REST contract from routes added

None.

## New locale keys with English values

| Key | English |
|---|---|
| `settings_unlanded_alert_enabled` | Daily pending-branch alert |
| `settings_unlanded_alert_enabled_help` | Once per calendar day, raise one alert naming the oldest pending Automaton branch and its age when more branches wait for cleanup than the threshold. |
| `settings_unlanded_alert_threshold` | Pending-branch alert threshold (default 3) |
| `settings_unlanded_alert_threshold_help` | A whole number, 0 or more. The daily alert fires when the number of pending Automaton branches is above this. 0 alerts on any pending branch. |
| `unlanded_counts_by_age` | By age: |
| `unlanded_counts_by_state` | By state: |
| `unlanded_counts_landed` | %1$s landed |
| `unlanded_counts_live` | %1$s live |
| `unlanded_counts_older` | %1$s older |
| `unlanded_counts_oldest` | Oldest pending: %1$s (%2$s), %3$s |
| `unlanded_counts_over_threshold` | Above alert threshold: %1$s pending (threshold %2$s) |
| `unlanded_counts_pending` | %1$s pending |
| `unlanded_counts_under_one_day` | %1$s under a day |
| `unlanded_counts_under_one_week` | %1$s under a week |
| `unlanded_counts_unlanded` | %1$s unlanded |

## Screens

- Housekeeping card (Settings, Automata): a new "Unlanded work counts" block showing counts by state and by age, the oldest pending branch with its age, and an above-threshold line when the pending count exceeds the threshold.
- Settings, Automata: two new settings, a daily pending-branch alert switch and its threshold (a whole number, 0 or more; 0 turns the alert off).
