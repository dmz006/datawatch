**Title:** [sync] feat(automaton): model request progress
**Issue:** https://github.com/dmz006/datawatch-app/issues/324
**Number:** 324

## Issue text
Parent release: datawatch next release

## What changed in datawatch

When an Automaton worker or verifier session waits on a model that runs on a registered compute node, datawatch now observes that in-flight model request, so a hung request can be told from a slow one. The daemon polls the node's running-models signal (and, when the node reports it, the GPU load) and sorts each request into one of four states:

- `generating`: the node shows the model working on the request.
- `idle`: the node is reachable but shows no sign of the model working on it.
- `unreachable`: the node could not be read at the last poll.
- `unknown`: no reading has been taken yet (before the first sample).

It is observation only. The daemon makes no model call and does nothing about a stuck request: it never ends a worker, never retries on another backend and never unloads a model. The watch reports, raises one alert and one non-blocking question at the threshold, and records the operator's answer.

### New REST route

`GET /api/autonomous/request-watch` (read-only; the same read access as the other `/api/autonomous/*` reads). Query parameters, both optional:

| Parameter | Notes |
|---|---|
| `session` | a session ID; only its observation is listed |
| `automaton` | an Automaton ID; only the observations of its tasks are listed |

An ID that matches nothing gives an empty list, not an error. Errors: `405` for any method other than `GET`, `503` when the watch is not wired in the daemon.

Response body:

| Field | Type | Notes |
|---|---|---|
| `enabled` | boolean | `autonomous.request_watch.enabled` in effect (default `true`); when `false`, `observations` is empty |
| `poll_seconds` | integer | seconds between samples (default `30`) |
| `no_progress_minutes` | integer | minutes without progress before a request is over the threshold (default `30`) |
| `observations` | array | one entry per watched session, ordered by session ID; never `null` |

Each entry of `observations[]` (the same object the `model_request` field carries, below):

| Field | Type | Notes |
|---|---|---|
| `session_id` | string | the worker or verifier session |
| `automaton_id` | string | the owning Automaton |
| `task_id` | string | the task the session works on |
| `role` | string | `worker` or `verifier` |
| `node` | string | the compute node the model runs on |
| `model` | string | the model the request is waiting on |
| `state` | string | `generating`, `idle`, `unreachable` or `unknown` |
| `last_change` | string, omitted when empty | RFC 3339 time the state last changed |
| `no_progress_seconds` | integer | seconds without progress (0 while `generating`) |
| `last_poll` | string, omitted when empty | RFC 3339 time of the last sample |
| `over_threshold` | boolean | true once `no_progress_seconds` reaches `no_progress_minutes` |
| `error` | string, omitted when empty | why the node could not be read |
| `summary` | string | plain text, below |

`summary` is plain text the app can show as is: `waiting on a model request: generating`; `waiting on a model request: no progress for N minutes` (also `1 minute` and `less than a minute`); for an unreachable node `waiting on a model request: node unreachable, no progress for N minutes`; and `waiting on a model request: no reading yet` while the state is `unknown`.

### New `model_request` object on session and task JSON

The same observation object is attached, only when the watch holds one, to:

- every session in the session list and session detail JSON (`GET /api/sessions`, `GET /api/sessions/{id}` and the other session views): `model_request`;
- every task row of an Automaton: `GET /api/autonomous/prds/{id}` (in each story's task list) and its task listing (`GET /api/autonomous/prds/{id}?detail=tasks`): `model_request`. A task with both a worker and a verifier observation carries the most recently polled one, which is the request in flight.

The field is filled at response time and never stored. It is absent when there is no observation. A task's `status` is unchanged by it. Unknown or absent fields must render as today, so the app stays forward-compatible with older daemons.

### Settings: `autonomous.request_watch`

`GET` and `PUT /api/config` serve the block under `autonomous.request_watch`; `GET` always returns all eleven keys with defaults filled in. `PUT` takes dotted keys (`autonomous.request_watch.<key>`), checks every key before applying any, and one bad value refuses the whole `PUT` with a message that names the key and the valid values. The block applies live, with no restart.

| Key | Type | Default | Valid values |
|---|---|---|---|
| `enabled` | boolean | `true` | `true`, `false` |
| `poll_seconds` | integer | `30` | whole number, minimum 1 |
| `no_progress_minutes` | integer | `30` | whole number, minimum 1 |
| `observe_workers` | boolean | `true` | `true`, `false` |
| `observe_verifiers` | boolean | `true` | `true`, `false` |
| `use_gpu_signal` | boolean | `true` | `true`, `false` |
| `gpu_busy_percent` | integer | `10` | 1 to 100 |
| `unreachable_counts` | boolean | `true` | `true`, `false` |
| `raise_alert` | boolean | `true` | `true`, `false` |
| `raise_question` | boolean | `true` | `true`, `false` |
| `remediation_options` | string array | unset, which reads as all four | JSON array or comma list of `keep_waiting`, `end_worker`, `retry_other_backend`, `unload_reload_model`; `keep_waiting` is always offered and is always the default answer; an explicit empty list offers only `keep_waiting`; `null` restores the default |

### MCP, CLI and comm channel

- MCP: new read-only tool `autonomous_request_watch` returns the same object as the REST route. Arguments: `session` and `automaton`, both optional.
- CLI: `datawatch autonomous request-watch [--session ID] [--automaton ID] [--json]`; `--json` prints the REST body untouched.
- Comm channel verb: `autonomous request-watch`.
- The settings travel through the existing config tools (`config_set` with `autonomous.request_watch.*` keys).

### The alert and the non-blocking question

When a request has made no progress for `no_progress_minutes` (once per episode, identified by the session, the node and the time the episode began):

- **Alert** (`raise_alert`): one `warn` alert titled "Automaton model request is not progressing". The body names the Automaton, the task, the role, the node and the model, the minutes without progress against the limit, and the ways to act.
- **Question** (`raise_question`, when the operator question channel is on and at least two options are offered): one operator question, asked by `request-watch`, context `task`, label `decision`, with `task_id` and the Automaton ID set. It is **not blocking**: it does not park or change the task, because parking a running worker would itself end the worker. Its four choices, with the default `keep_waiting`:

| Option ID | Label | Meaning |
|---|---|---|
| `keep_waiting` | Keep waiting | Nothing changes and a slow request may still finish. Always offered; the default answer. |
| `end_worker` | End only the stuck worker | Leaves the model on the node alone. |
| `retry_other_backend` | Retry on a stronger backend | Runs the work again on another backend. |
| `unload_reload_model` | Unload and reload the model on the node | Clears a model that may be stuck on the node. |

  Answering **only records the choice**: the answer is written to the Automaton's decision log as a `request_watch_answer` entry that names the chosen option and states that the daemon performed no remediation. Nothing is ended, retried or unloaded; the operator acts by hand. The question is withdrawn on its own when the request recovers or the session ends.

When no question is asked, the alert lists the options in plain words so the alert alone carries the choices.

The question uses the existing operator question routes: `GET /api/questions?asker=request-watch` lists the open ones and `POST /api/questions/{id}/answer` with `{"chosen_option_id": "<option ID>"}` answers one. Mobile already renders operator questions; this adds no new answer route.

### Web app (PWA) reference render

- **Automata model request progress card** on the Automation Overview, beside the uncommitted-work card, polled every 10 seconds from `GET /api/autonomous/request-watch`. A header with a small help link, a read-only note, then one row per observation (up to 50, then "N more not shown"): session, role, task, node, the state chip, and the error line when `error` is set. An empty list shows the empty text; a failed read shows the loader's error line built from `request_watch_failed`.
- **State chip** from `model_request` on each task row of the Automaton detail, each session row in the session list and the session detail header. The chip shows `summary`; the node and model are its tooltip. A request with `over_threshold` true uses the warning style. No `model_request` means no chip.
- **Settings fields** in a "Model request watch" group in the Automata settings: the eleven keys in the table above, `remediation_options` edited as comma-separated text and sent as an array.

### New locale keys

`%1$s` is a positional placeholder, kept as written. The `de`, `es`, `fr` and `ja` PWA bundles carry the English text as a placeholder until the translations arrive.

| Key | EN value |
|---|---|
| `settings_request_watch_group` | Model request watch |
| `settings_request_watch_enabled` | Model request watch (on by default) |
| `settings_request_watch_enabled_hint` | Watches the in-flight model request of a worker or verifier whose model runs on a registered compute node, so a hung request can be told from a slow one. Observation only: it never ends a worker or retries anything. On by default. |
| `settings_request_watch_poll_seconds` | Model request watch poll interval (seconds, whole number, minimum 1, default 30) |
| `settings_request_watch_poll_seconds_hint` | How often the daemon samples each watched request. Whole number, minimum 1, default 30. |
| `settings_request_watch_no_progress_minutes` | Model request watch no-progress threshold (minutes, whole number, minimum 1, default 30) |
| `settings_request_watch_no_progress_minutes_hint` | Minutes without progress before a request is flagged as over the threshold. Whole number, minimum 1, default 30. |
| `settings_request_watch_observe_workers` | Watch worker requests (on by default) |
| `settings_request_watch_observe_workers_hint` | Observe the model request of each worker session. On by default. |
| `settings_request_watch_observe_verifiers` | Watch verifier requests (on by default) |
| `settings_request_watch_observe_verifiers_hint` | Observe the model request of each verifier session. On by default. |
| `settings_request_watch_use_gpu_signal` | Use the compute node GPU signal (on by default) |
| `settings_request_watch_use_gpu_signal_hint` | Read the GPU load of the compute node as progress evidence when it is reported. On by default. |
| `settings_request_watch_gpu_busy_percent` | GPU busy threshold (percent, 1 to 100, default 10) |
| `settings_request_watch_gpu_busy_percent_hint` | A GPU load at or above this percent counts as the model generating. Accepted values 1 to 100, default 10. |
| `settings_request_watch_unreachable_counts` | Count an unreachable node as no progress (on by default) |
| `settings_request_watch_unreachable_counts_hint` | While the compute node cannot be reached the time counts toward the no-progress threshold. On by default. |
| `settings_request_watch_raise_alert` | Raise an alert at the threshold (on by default) |
| `settings_request_watch_raise_alert_hint` | Raise an alert when a request has made no progress for the threshold. On by default. |
| `settings_request_watch_raise_question` | Raise a question at the threshold (on by default) |
| `settings_request_watch_raise_question_hint` | Raise a question that offers the remediation options. The question does not block or change the task; answering it only records the choice. On by default. |
| `settings_request_watch_remediation_options` | Remediation options offered in the question (comma separated, default all four) |
| `settings_request_watch_remediation_options_hint` | Options are keep_waiting, end_worker, retry_other_backend and unload_reload_model. Keep waiting is always offered and is always the default answer. Empty offers only keep waiting. |
| `request_watch_card_title` | Automata model request progress |
| `request_watch_help_link` | About model request progress |
| `request_watch_read_only` | Read-only. The daemon observes each model request of an Automaton worker or verifier and never ends or retries a worker. |
| `request_watch_empty` | No Automaton worker or verifier is waiting on an observed model request. |
| `request_watch_failed` | Could not load the Automata model request progress: %1$s |
| `request_watch_generating` | Waiting on a model request: generating |
| `request_watch_no_progress` | Waiting on a model request: no progress for %1$s minutes |
| `request_watch_no_progress_one` | Waiting on a model request: no progress for 1 minute |
| `request_watch_no_progress_under_minute` | Waiting on a model request: no progress for under a minute |
| `request_watch_unreachable` | Waiting on a model request: node unreachable |
| `request_watch_waiting` | Waiting on a model request |
| `request_watch_chip_tip` | Model request on %1$s |
| `request_watch_chip_tip_plain` | Waiting on a model request |
| `request_watch_role_worker` | Worker |
| `request_watch_role_verifier` | Verifier |
| `request_watch_row_session` | Session %1$s |
| `request_watch_row_task` | Task %1$s |
| `request_watch_row_node` | Node %1$s |
| `request_watch_row_error` | Error: %1$s |
| `request_watch_more` | %1$s more not shown |

Please send translations of the 43 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

Capability parity, not implementation parity: Android and iPhone show the same information and the same question answer path, in each app's own idiom.

- The Automata overview shows a model request progress card from `GET /api/autonomous/request-watch`: per observation the session, role, task, node, the state with its `summary`, and the `error` line when set; an empty `observations` list shows the empty text; a failed read shows an error line built from `request_watch_failed`; the card refreshes on a short poll and tolerates a daemon that answers `503` (or has no such route) by hiding the card or showing the error line. It is read-only.
- A state chip built from `model_request` shows on each Automaton task row, each session row and the session detail header, with the node and model as its tooltip or long-press text, and a warning style when `over_threshold` is true. No `model_request` means no chip.
- The question raised at the threshold appears in the app's existing operator question list with its four choices (`keep_waiting`, `end_worker`, `retry_other_backend`, `unload_reload_model`, as many as the settings offer, `keep_waiting` the default). It is not blocking: the task is not shown as parked. Answering goes through `POST /api/questions/{id}/answer` with `chosen_option_id`, exactly as the PWA does, and the app makes clear that the answer only records the choice and performs nothing.
- The eleven settings read their current values from the daemon config and write them back through the config endpoint; an absent field falls back to the default in the table; `remediation_options` is edited as comma-separated text and sent as an array (an empty field is sent as an empty array, which offers only `keep_waiting`); a refused `PUT` shows the daemon's message.
- Every label, hint, chip, row and empty or error text is localized in EN, DE, ES, FR and JA, and every user-visible string says Automaton or Automata.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible with older daemons.

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state all --search 'model request progress'` on 2026-11-01 returned no issue for this feature, so no earlier issue was commented on.

Filed: https://github.com/dmz006/datawatch-app/issues/324 (2026-11-01, via `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file -`, with the body cut from the "Issue text" section of this file by `sed`). Filed exactly once.
