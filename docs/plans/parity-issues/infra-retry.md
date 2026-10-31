Issue: dmz006/datawatch-app#319

# [sync] Infrastructure retry: new GET /api/autonomous/infra-retries view, task infra_retries and infra_retry_count fields, autonomous.infra_retry settings block, MCP, CLI, comm, PWA card and badge, 31 locale keys

## Body of the issue

When the machinery around a task fails (a session that could not start, a failed storage write, a session killed before the task arrived, a daemon restart), the Automaton now retries the task after a delay instead of counting the failure against it. When the tries are spent the task is held for the operator and an operator question is raised. The daemon gained a read view of this state, two task fields, and an `autonomous.infra_retry` settings block. This records the exact contract the Android and iPhone apps should render, the settings to expose, and the locale keys for the labels.

### New REST route

`GET /api/autonomous/infra-retries` (read-only; needs the same read access as the other `/api/autonomous/*` reads). Query parameters, all optional:

| Parameter | Notes |
|---|---|
| `automaton` | an Automaton ID; only its tasks are listed |
| `limit` | whole number from 1 to 1000, default 50; anything else is a `400` |

Response body:

| Field | Type | Notes |
|---|---|---|
| `enabled` | boolean | the policy in effect (default `true`) |
| `max_tries` | integer | tries a task gets before the exhausted action applies (default `3`) |
| `backoff_seconds` | integer array | delay in seconds before each retry, never `null` (default `[60,300,540]`); the last entry repeats when there are more tries than entries; `[]` means no delay |
| `categories` | string array | failure classes treated as transient, never `null` (default all four: `spawn_error`, `storage_write`, `session_killed_before_delivery`, `daemon_restart`); `[]` means none |
| `exhausted_action` | string | `hold` (default) or `count` |
| `retry_dependents` | boolean | default `true` |
| `raise_question` | boolean | default `true` |
| `tasks` | array | tasks that were retried or are held, most recently retried first; never `null` |
| `count` | integer | number of entries in `tasks` |

The seven policy fields are the same values `GET /api/config` serves under `autonomous.infra_retry`, with defaults filled in.

Each entry of `tasks[]`:

| Field | Type | Notes |
|---|---|---|
| `automaton_id` | string | the owning Automaton |
| `automaton_title` | string | |
| `task_id` | string | |
| `task_title` | string | |
| `status` | string | the task status; a held task is `review_needed` |
| `retry_count` | integer | consecutive infrastructure retries in the current incident |
| `max_tries` | integer | the tries limit in effect, to render "Retry n of max" |
| `retries` | array | the retry history, oldest first, never `null` |
| `held` | boolean | true while the task waits for the operator because its retries ran out |
| `held_cause` | string, omitted when empty | why it is held |
| `question_id` | string, omitted when none | the operator question open for the hold |

Each entry of `retries[]`:

| Field | Type | Notes |
|---|---|---|
| `number` | integer | which retry it was |
| `class` | string | one of the four failure classes |
| `reason` | string, omitted when empty | the failure text |
| `at` | RFC 3339 time | when the retry was decided |
| `delay_seconds` | integer | the back-off delay applied |
| `next_at` | RFC 3339 time | when the retry is due; may be the zero time |

Errors: `503` when the Automaton system is disabled, `501` when the daemon cannot serve the read, `400` for a bad `limit`. Unknown fields should be ignored, as before.

### New task fields

On every task in the existing Automaton and task views (`GET /api/autonomous/prds`, `GET /api/autonomous/prds/{id}`), both omitted when empty:

| Field | Type | Notes |
|---|---|---|
| `infra_retries` | array | the task's infrastructure retry history, oldest first, the newest 50 kept; each entry has `number`, `class`, `reason`, `at`, `delay_seconds`, `next_at` and `source` (an internal marker the app may ignore) |
| `infra_retry_count` | integer | consecutive infrastructure retries in the current incident; resets when the task gets a fair attempt, while `infra_retries` stays |

A task held after its retries ran out has `status` `review_needed` and a `failure_reason` whose `source` is `infra-retry-hold` and whose `detail` is the hold cause.

### New settings block

`GET` and `PUT /api/config`, under `autonomous.infra_retry`. `GET` always returns all seven keys with defaults filled in. `PUT` takes dotted keys (`autonomous.infra_retry.<key>`) and checks every key before applying any, so one bad value refuses the whole `PUT` with a message naming the key and the valid values.

| Key | Type | Default | Valid values |
|---|---|---|---|
| `enabled` | boolean | `true` | `true`, `false` |
| `max_tries` | integer | `3` | whole number from 1 to 1000 |
| `backoff_seconds` | integer array | `[60,300,540]` | JSON array or comma list of whole seconds, each 0 to 604800, at most 1000 entries; empty means no delay; `null` restores the default |
| `categories` | string array | all four classes | JSON array or comma list of `spawn_error`, `storage_write`, `session_killed_before_delivery`, `daemon_restart`; empty retries none; `null` restores the default |
| `exhausted_action` | string | `hold` | `hold` or `count` |
| `retry_dependents` | boolean | `true` | `true`, `false` |
| `raise_question` | boolean | `true` | `true`, `false` |

### MCP

- New read-only tool `autonomous_infra_retries` returns the same object as `GET /api/autonomous/infra-retries`. Arguments: `automaton` (an Automaton ID) and `limit` (1 to 1000, default 50), both optional.
- The settings travel through the existing config tools: `config_set` with `autonomous.infra_retry.*` keys, and `autonomous_config_set` gained the arguments `infra_retry_enabled`, `infra_retry_max_tries`, `infra_retry_backoff_seconds`, `infra_retry_categories`, `infra_retry_exhausted_action`, `infra_retry_retry_dependents`, `infra_retry_raise_question`.

### CLI

- `datawatch autonomous infra-retries [automaton-id] [--limit N] [--json]` prints a policy line (`Infrastructure retry: on, 3 tries, back-off 60s/300s/540s, hold when used up`) and then one block per task (`Automaton <id> "<title>", task <id> "<title>": <status>, retry <n> of <max>`, one `retry <n>: <class>: <reason> (<time>)` line per retry, and for a held task `held after infrastructure retries: <cause>` plus `waiting on question <id>` when one is open). `--json` prints the REST body untouched.
- `datawatch config set autonomous.infra_retry.<key> <value>` sets a key.

### Comm channel

- `autonomous infra-retries [automaton-id] [limit]` replies with the same text as the CLI, newest task first, default 10 tasks, cut to fit one chat message with closing lines that say what was left out.
- `autonomous config-set` and `configure` set the keys through the existing config path.

### PWA (reference render)

- **Infrastructure retries card** on the Automation Overview, beside the lane fill card, polled every 10 seconds. A header, then a policy line, then one block per task (up to 20):
  - policy line: "Automatic retry is on: a task gets N tries, then it is held for the operator." (`exhausted_action` `hold`), the "then the failure counts against the task" variant for `count`, or "Automatic retry is off: an infrastructure failure counts as an ordinary failure." when `enabled` is false;
  - per task: the Automaton and task titles, a "Retry n of max" badge, then the last 3 retries, each with its class, reason, time and next try;
  - a held task adds a "Held for the operator" badge, the cause line, and an "Open the question" button when `question_id` is present;
  - an empty `tasks` list shows the empty text; a failed read shows the loader's error line.
- **Task row badge** in the Automaton detail: a "Retry n of max" badge when `infra_retry_count` (or the newest `infra_retries` entry) is above zero, using `max_tries` from the view above; a "Held for the operator" badge when the task is held (`failure_reason.source` is `infra-retry-hold`), with the cause as its tooltip, and the cause line in the expanded task body.
- **Settings fields** in the Automata settings block, after the lane fill settings: the seven keys in the table below.

### Settings to add to the mobile app

| Config key | Label (English) | Type | Default | Control |
|---|---|---|---|---|
| `autonomous.infra_retry.enabled` | Infrastructure retry (retry a task when the machinery around it failed; on by default) | boolean | `true` | toggle |
| `autonomous.infra_retry.max_tries` | Infrastructure retry tries per task (whole number, minimum 1, default 3) | integer | `3` | number, minimum 1 |
| `autonomous.infra_retry.backoff_seconds` | Infrastructure retry delays (seconds, comma separated, default 60, 300, 540) | integer array | `[60,300,540]` | comma-separated text, sent as an array |
| `autonomous.infra_retry.categories` | Infrastructure retry failure classes (comma separated, default all four) | string array | all four | comma-separated text, sent as an array |
| `autonomous.infra_retry.exhausted_action` | Infrastructure retry when the tries are spent (hold or count, default hold) | string | `hold` | select: Hold the task / Count as a failure |
| `autonomous.infra_retry.retry_dependents` | Infrastructure retry also retries blocked dependents (on by default) | boolean | `true` | toggle |
| `autonomous.infra_retry.raise_question` | Infrastructure retry raises a question when the tries are spent (on by default) | boolean | `true` | toggle |

### New locale keys

Placement of every key above is under "PWA (reference render)". The `de`, `es`, `fr` and `ja` PWA bundles carry the English text as a placeholder until the translations arrive. `%1$s` and `%2$s` are positional placeholders, kept as written.

| Key | EN value | Context |
|---|---|---|
| `infra_retry_card_title` | Infrastructure retries | card header |
| `infra_retry_empty` | No task has been retried or held because of an infrastructure failure. | card, no tasks |
| `infra_retry_failed` | Could not load the infrastructure retries: %1$s | card, failed read; `%1$s` is the error |
| `infra_retry_policy_on_hold` | Automatic retry is on: a task gets %1$s tries, then it is held for the operator. | policy line; `%1$s` is `max_tries` |
| `infra_retry_policy_on_count` | Automatic retry is on: a task gets %1$s tries, then the failure counts against the task. | policy line |
| `infra_retry_policy_off` | Automatic retry is off: an infrastructure failure counts as an ordinary failure. | policy line |
| `infra_retry_count_of` | Retry %1$s of %2$s | badge on card and task row |
| `infra_retry_count` | Retry %1$s | badge when the limit is not known |
| `infra_retry_class` | Class: %1$s | retry line |
| `infra_retry_reason` | Reason: %1$s | retry line |
| `infra_retry_time` | At %1$s | retry line |
| `infra_retry_next` | Next try %1$s | retry line |
| `infra_retry_held_label` | Held for the operator | badge on a held task |
| `infra_retry_held_cause` | Cause: %1$s | cause line and badge tooltip |
| `infra_retry_open_question` | Open the question | button on a held task with a question |
| `settings_infra_retry_enabled` | Infrastructure retry (retry a task when the machinery around it failed; on by default) | settings label |
| `settings_infra_retry_enabled_hint` | A spawn error, a failed storage write, a session killed before the task arrived or a daemon restart is retried after a delay instead of counting as a failed attempt. | settings hint |
| `settings_infra_retry_max_tries` | Infrastructure retry tries per task (whole number, minimum 1, default 3) | settings label |
| `settings_infra_retry_max_tries_hint` | How many tries a task gets before the action below applies. | settings hint |
| `settings_infra_retry_backoff_seconds` | Infrastructure retry delays (seconds, comma separated, default 60, 300, 540) | settings label |
| `settings_infra_retry_backoff_seconds_hint` | The delay before each retry. The last entry repeats when a task has more tries than entries. Empty means no delay. | settings hint |
| `settings_infra_retry_categories` | Infrastructure retry failure classes (comma separated, default all four) | settings label |
| `settings_infra_retry_categories_hint` | Classes are spawn_error, storage_write, session_killed_before_delivery and daemon_restart. Empty retries none. | settings hint; the class names stay untranslated |
| `settings_infra_retry_exhausted_action` | Infrastructure retry when the tries are spent (hold or count, default hold) | settings label |
| `settings_infra_retry_exhausted_action_hint` | Hold keeps the task for the operator. Count books the failure as an ordinary failed attempt. | settings hint |
| `settings_infra_retry_exhausted_hold` | Hold the task | select option |
| `settings_infra_retry_exhausted_count` | Count as a failure | select option |
| `settings_infra_retry_retry_dependents` | Infrastructure retry also retries blocked dependents (on by default) | settings label |
| `settings_infra_retry_retry_dependents_hint` | Tasks that were blocked behind the failed task are retried with it. | settings hint |
| `settings_infra_retry_raise_question` | Infrastructure retry raises a question when the tries are spent (on by default) | settings label |
| `settings_infra_retry_raise_question_hint` | The question asks the operator what to do with the held task. | settings hint |

Please send translations of the 31 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- The Automation Overview shows an infrastructure retries card from `GET /api/autonomous/infra-retries`: the policy line for `enabled` and `exhausted_action`, and per task the titles, the "Retry n of max" badge and the last retries with class, reason, time and next try, newest task first.
- A held task shows the "Held for the operator" badge and the cause, and the "Open the question" button opens the operator question named by `question_id`; without `question_id` there is no button.
- An empty `tasks` list shows the empty text; a failed read shows an error line built from `infra_retry_failed`; the card refreshes on a short poll and tolerates a daemon that answers `501` or `503` by hiding the card or showing the error line.
- The Automaton detail task rows show the "Retry n of max" badge from `infra_retry_count` and the held badge from `failure_reason.source` `infra-retry-hold`, with the cause in the expanded body.
- The seven settings read their current values from the daemon config and write them back through the config endpoint; an absent field falls back to the default in the table; the two list fields are edited as comma-separated text and sent as arrays; a refused `PUT` shows the daemon's message.
- Every label, hint, badge, policy line and button is localized in EN, DE, ES, FR and JA, with every user-visible string saying Automaton or Automata.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible with older daemons.

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state all --search 'infrastructure retry'` on 2026-10-31 returned nothing, so no earlier issue was commented on.

Filed: https://github.com/dmz006/datawatch-app/issues/319 (2026-10-31, via `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file <scratch draft>`). Filed exactly once.
