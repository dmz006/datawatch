<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/408 (release v9.114.0). -->
# datawatch-app parity issue draft: failed-attempt cost settings (READY TO FILE, NOT FILED)

Date: 2026-11-08
Parent work: "Make a failed task attempt cost as little as possible. Measured 2026-11-08: a task is tried up to three times and each attempt starts a full worker context."
Status: draft only. Nothing has been filed. No hosting CLI was run, and no issue exists on any tracker. The supervisor files it at merge time and records the issue URL back into this file.

## Affected area

The daemon now has four attempt-cost settings that decide how cheaply a failed Automaton task is retried:

- `autonomous.first_attempt.<kind>.backend` and `autonomous.first_attempt.<kind>.model`, one pair for each task kind (`implementation`, `docs`, `note`, `final_gate`), which choose the worker for a task's first attempt.
- `autonomous.first_attempt_escalate_after_failures`, which sets how many failed attempts pass before the first-attempt pair stops applying (default 1, range 0 to 20).
- `autonomous.retry_resume_max_context_tokens`, which sets the largest prior context a retry may resume instead of starting a new session (default 150000, range 0 to 2000000, where 0 means always start fresh).

The same change adds three new review reasons that can hold a task without another attempt, a `resumed` and `resume_reason` record on every attempt, and a `backend_reason` on every attempt. Those reasons appear in the Automaton review panel and in the status output that the apps display.

## Current behavior

On the daemon side, the four settings are readable and writable on every operator surface: config.yaml, the web Settings page (under Automata, then Autonomous), REST `GET` and `PUT /api/config`, the comm channel `configure` command, the MCP `autonomous_config_get` and `autonomous_config_set` tools, and `datawatch config set`. Every write goes through one shared validator, and a refused write saves nothing.

On the mobile side, nothing is filed yet. This draft has not confirmed the Android and iOS state. Unless those clients already carry these fields, they do not show the four settings, the three new review reasons, or the `backend_reason`, `resumed` and `resume_reason` fields on attempts, and their Automaton review view would show a held task with no explanation of why a retry was or was not resumed.

The PWA settings card and the locale strings are in place in all five bundles (en, de, es, fr, ja). The help link for these settings points at the failed-attempt-cost how-to page.

## Expected behavior

The Android and iOS clients should reach the same capability as the PWA, using their own idiomatic controls, with no change to the daemon:

- Settings: show the first-attempt backend and model for each of the four task kinds, the escalate-after-failures count and the resume context limit. Each value should be validated by the daemon on save, and a refused value should show the daemon's message beside the field and keep the typed value for correction.
- Review and attempt view: show each attempt's worker and why it was chosen, whether the attempt resumed the previous session and the reason it did or did not, and the "No retry" reason on a task held for review.
- Help: link the settings to the same glossary and how-to anchors the PWA uses.
- Strings: use the English strings already in the datawatch bundle, and request DE, ES, FR and JA translations through the usual Compose Multiplatform pipeline. Do not machine-translate ad hoc.

## Suggested next step

File one `feat(parity):` issue on datawatch-app, titled along the lines of `feat(parity): failed-attempt cost settings and attempt record for the Android and iOS clients`. The body should contain:

1. The four setting keys with their types, ranges and defaults, copied from the howto's settings table.
2. The three new review reasons (no files changed for a task that writes nothing, verification script missing, and the same failure summary twice) and the alert title, *Automaton task needs review (no retry)*.
3. The attempt-record fields listed above, with a note that they are omitted when not recorded.
4. The English keys to copy from `internal/server/web/locales/en.json` (the `settings_attempt_cost_*` and `review_attempt_*` groups), and the request for the four translations.
5. A link to `docs/howto/failed-attempt-cost.md`, and to the glossary entries *First-attempt worker (attempt cost)*, *Retry resume (attempt cost)* and *No-retry hold* in `docs/datawatch-definitions.md`.

Record the issue URL and number in this file once it is filed. Until then, the Mobile-Parity status for this feature stays open.

## Parity surface

- **REST:** included. `GET` and `PUT /api/config` carry the four keys, and `GET /api/autonomous/status` carries the review reasons. Validation errors come back as HTTP 400 with the daemon's message.
- **MCP:** included. `autonomous_config_get` and `autonomous_config_set` carry the four keys, and the Automaton read tools carry the attempt record.
- **CLI:** included. `datawatch config set` writes each key, and `datawatch autonomous status` prints the review reasons.
- **comm channel:** included. `configure <key>=<value>` writes each key, and a refused write replies with the same validator message.
- **YAML/config:** included. The four keys are in config.yaml under the `autonomous` block and are documented in `docs/config-reference.yaml`.
- **PWA:** included. The Settings card under Automata, then Autonomous, plus the Automaton review panel and the help link, are in place.
- **Android:** not yet covered. This draft is the request for it.
- **iPhone/iOS:** not yet covered. This draft is the request for it, with the same content.

Each excluded surface has a stated reason above. Android and iOS are not excluded; they are pending this issue.
