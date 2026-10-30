Issue: dmz006/datawatch-app#307

# [sync] Supervisor watch: new REST read GET /api/autonomous/supervisor-watch, MCP, CLI, comm, PWA card, eight autonomous.automation.supervisor_watch config keys and 29 locale keys in five bundles

## Body of the issue

The daemon now runs a supervisor watch. On each coordinator poll, with no model call, it records the findings the supervisor watch script produces today, and raises them through the existing alert and notify channels. It ships in shadow mode first: shadow reports and records only and never acts. This adds one new read contract, `GET /api/autonomous/supervisor-watch`, which the PWA renders as a read-only Supervisor watch card on the Automaton view. This issue records the exact endpoint, every response field, the config keys and the locale keys. Capability parity is required; implementation parity is not.

### New REST route

`GET /api/autonomous/supervisor-watch` is read-only and takes no query parameters. Any other method answers 405. It answers 503 (`autonomous disabled`) when Automata is disabled; the client hides the card in that case and never shows a stale state. A federated peer needs the autonomous read capability (`federation.CapAutonomousRead`), otherwise 403.

Body:

```json
{
  "mode": "off | shadow | on",
  "last_poll_at": "2026-01-01T00:00:00Z",
  "findings": [ ... ],
  "comparison": { "last": null, "agreed_days": 0, "only_watch": [], "only_script": [] }
}
```

| Field | Type | Notes |
|---|---|---|
| `mode` | string | The watch mode in effect: `off`, `shadow` or `on`. An unset mode reads `shadow` (reports only, never acts) |
| `last_poll_at` | string, date-time | The instant the coordinator last ran a supervisor watch pass; the zero time (`0001-01-01...`) when it never has |
| `findings` | array | The stored findings, open first, then most recently seen first; an empty array (never null) when none are stored |
| `comparison` | object | The comparison against the script's record file, see below |

#### `findings[]` fields

| Field | Type | Notes |
|---|---|---|
| `kind` | string | One of the stable kinds: `tmux_restart`, `daemon_inactive`, `model_not_resident`, `task_failed`, `task_review_needed`, `automaton_not_merged`, `idle_lane_eligible_work`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`, `main_checkout_dirty`, `ci_red`, `ci_stuck` |
| `subject` | string | The stable id the finding is about: an Automaton id, a node name, a lane name, a CI run id, or the main checkout path. `kind` plus `subject` is the finding's identity |
| `text` | string, omitted when empty | The one-line operator text for the finding; fall back to `subject` when absent |
| `first_seen_at` | string, date-time | When the finding was first raised; preserved across refreshes |
| `last_seen_at` | string, date-time | When the finding was last re-raised or touched |
| `would_alert` | boolean, omitted when false | Shadow mode marked the finding alert-due without raising an alert; always absent in `on` mode |
| `alerted_at` | string, date-time, omitted until set | When the alert for the finding was raised (`on` mode); absent until it is |
| `resolved` | boolean | The finding was closed; a later sighting re-opens it in place |

#### `comparison` fields

| Field | Type | Notes |
|---|---|---|
| `last` | object or `null` | The most recent recorded comparison; `null` until one was recorded (an unavailable script record is never recorded) |
| `agreed_days` | integer | Calendar days whose comparisons all agreed; a day with no comparisons does not count |
| `only_watch` | array of strings | The last comparison's `kind/subject` entries the watch raised that the script did not; empty array when none |
| `only_script` | array of strings | The last comparison's `kind/subject` entries the script raised that the watch did not; empty array when none |

`comparison.last` fields: `at` (date-time, when the pass ran the comparison), `watch_kinds` and `script_kinds` (arrays of `kind/subject` strings, sorted, de-duplicated), `only_watch` and `only_script` (arrays of strings, omitted when empty), `agreed` (boolean, the two sides raised exactly the same set).

Everything is additive: unknown fields are ignored, and an absent optional field renders as today.

### MCP

- New read-only tool `supervisor_watch_list` returns the same object as `GET /api/autonomous/supervisor-watch`. It takes no arguments; an argument is refused rather than ignored.

### CLI

- `datawatch autonomous supervisor-watch` prints the mode, the last poll, one line per finding (kind, state, last seen, the operator line) and the comparison summary. States are `open`, `would alert`, `alerted` and `resolved`. `--json` prints the raw response.

### Comm channel

- `autonomous supervisor-watch` (also `autonomous supervisor_watch`) replies with the same lines as the CLI. It is a read; the alerts the watch raises reach chat through the existing alert fan-out.

### PWA card (reference render)

The Supervisor watch card sits on the Automaton view (Chains sub-tab, element `#automationSupervisorWatchCard`, `role="status"`). It polls `GET /api/autonomous/supervisor-watch` every 30 000 ms, with one interval only (cleared before it is set and when the view is left), and is hidden when the endpoint is unavailable. It shows, top to bottom:

1. Title "Supervisor watch".
2. "Mode: <label>" where the label is `supervisor_watch_mode_off` / `_shadow` / `_on`; an unknown mode shows its raw value; an empty mode shows shadow.
3. The last poll: "Last poll never" for the zero time, otherwise "Last poll just now" (under 5 s), "N s. ago", "N m. ago", "N h. ago" or "N d. ago".
4. "No open findings", or "Open findings: N" followed by up to 20 open (not resolved) findings, each as the `kind` in code style, the `text` (falling back to `subject`) and "(would alert)" when `would_alert` is set; then "and N more open findings" when more than 20 are open.
5. The comparison line: "Comparison: no supervisor script record compared yet" when `last` is null; "Comparison: agrees with the supervisor script (N days of full agreement)" when `last.agreed`; otherwise "Comparison: differs from the supervisor script. Only the watch: <entries>. Only the script: <entries>. (N days of full agreement)" with "nothing" for an empty side.

### Config keys (autonomous.automation.supervisor_watch)

Read and written through `GET` / `PUT /api/config` under `autonomous.automation.supervisor_watch`. The daemon applies every default in one place, so a client shows the effective value when a key is unset and never writes a default of its own. Every write path validates: a bad mode or a value out of range is refused.

| Key | Type | Default | Notes |
|---|---|---|---|
| `mode` | select | `shadow` (empty reads shadow) | `off`, `shadow` or `on`; anything else is refused |
| `alert_minutes` | integer | 30 when unset | Minutes a finding stays open before one alert; `0` turns the alert off; negative is refused |
| `silent_minutes` | integer | 60 when unset | Minutes an Automaton worktree may stay silent before a `worktree_silent` finding; `0` is kept; negative is refused |
| `poll_seconds` | integer | 60 when unset (0) | Coordinator poll interval the staleness window is measured against; minimum 60, a lower non-zero value is refused |
| `script_output_path` | text | empty | Repository-relative path of the supervisor script record file; empty means no comparison |
| `tmux_restart_baseline` | text | empty | Baseline the `tmux_restart` finding compares against; empty means no baseline |
| `main_checkout` | text | empty | Path of the main checkout; empty means `main_checkout_dirty` is not checked |
| `working_model` | text | empty | Model every compute node should keep resident; empty means `model_not_resident` is not checked |

The PWA lists all eight in the Automata settings block, labelled by the `settings_supervisor_watch_*` keys below.

### New locale keys

There are 29 new keys, present in all five PWA bundles (en, de, es, fr, ja). EN values:

| Key | EN value |
|---|---|
| `supervisor_watch_title` | Supervisor watch |
| `supervisor_watch_mode` | Mode: %1$s |
| `supervisor_watch_mode_off` | off |
| `supervisor_watch_mode_shadow` | shadow (reports only, never acts) |
| `supervisor_watch_mode_on` | on |
| `supervisor_watch_last_poll` | Last poll %1$s |
| `supervisor_watch_last_poll_never` | Last poll never |
| `supervisor_watch_age_just_now` | just now |
| `supervisor_watch_age_seconds` | %1$d s. ago |
| `supervisor_watch_age_minutes` | %1$d m. ago |
| `supervisor_watch_age_hours` | %1$d h. ago |
| `supervisor_watch_age_days` | %1$d d. ago |
| `supervisor_watch_findings` | Open findings: %1$d |
| `supervisor_watch_no_findings` | No open findings |
| `supervisor_watch_more` | and %1$d more open findings |
| `supervisor_watch_would_alert` | (would alert) |
| `supervisor_watch_unknown` | unknown |
| `supervisor_watch_comparison_none` | Comparison: no supervisor script record compared yet |
| `supervisor_watch_comparison_agreed` | Comparison: agrees with the supervisor script (%1$d days of full agreement) |
| `supervisor_watch_comparison_differs` | Comparison: differs from the supervisor script. Only the watch: %1$s. Only the script: %2$s. (%3$d days of full agreement) |
| `supervisor_watch_comparison_nothing` | nothing |
| `settings_supervisor_watch_mode` | Supervisor watch mode (off, shadow or on; default shadow) |
| `settings_supervisor_watch_alert_minutes` | Supervisor watch alert (minutes a finding stays open before one alert, 0 = off, default 30) |
| `settings_supervisor_watch_silent_minutes` | Supervisor watch silence window (minutes an Automaton worktree may stay silent, default 60) |
| `settings_supervisor_watch_poll_seconds` | Supervisor watch poll interval (seconds, min 60, default 60) |
| `settings_supervisor_watch_script_output_path` | Supervisor watch script record path (repository-relative path of the supervisor script record file, empty = no comparison) |
| `settings_supervisor_watch_tmux_restart_baseline` | Supervisor watch tmux restart baseline (empty = no baseline) |
| `settings_supervisor_watch_main_checkout` | Supervisor watch main checkout (repository path, empty = dirty main checkout not checked) |
| `settings_supervisor_watch_working_model` | Supervisor watch working model (model every compute node should keep resident, empty = not checked) |

The de, es, fr and ja bundles carry the same English text as a placeholder for now. Please send translations of the 29 keys into DE, ES, FR and JA (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- The app reads `GET /api/autonomous/supervisor-watch` and renders mode, last poll, open findings (kind, text, would-alert mark) and the comparison line, matching the PWA.
- The view is hidden, with no stale state, when the endpoint answers 503, 403 or fails.
- `would_alert` findings show the mark; resolved findings are not counted as open; a null `comparison.last` reads as no comparison yet; the zero `last_poll_at` reads as never.
- The eight config keys read their value from the daemon config and write it back through the config endpoint; an absent key shows the default in the table.
- Labels, the age text and the comparison text are localized in EN, DE, ES, FR and JA.
- Unknown or absent fields render exactly as today, so the app stays forward-compatible.
