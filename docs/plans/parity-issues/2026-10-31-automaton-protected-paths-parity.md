**Title:** feat(i18n): add 39 protected paths keys (protected_paths_*, settings_automation_protected_paths*, rules_review_source_*) for protected paths parity (datawatch v9.104.0)
**Issue:** https://github.com/dmz006/datawatch-app/issues/381
**Number:** 381

## Issue text
Parent release: datawatch next release

## What changed in datawatch

Some files, such as the project rules file, the supervisor rules document, CODEOWNERS and the workflows directory, must not change inside an Automaton task without the operator. A new **protected-paths gate** now runs once per task attempt, right after the verification result is stored: it compares the task directory's working tree (committed changes, uncommitted edits to tracked files and new untracked files) with the base commit, and matches the changed paths against the protected-path list. If the task changed a protected path the Automaton has not authorised in advance, the **protected-paths action** decides what happens:

- `hold` (default): the task is parked in the `blocked` state, the hold is recorded on the task, and one operator question is raised. Nothing changes until the operator answers approve, reject or change the spec.
- `warn`: the finding is recorded in the Automaton decision log and raised as an alert, and the task completes. No hold object is written and no question is raised.
- `block`: the task is parked in the `blocked` state with no question: the operator authorises the paths with the Automaton's authorised paths, or edits the task, then resets it.
- `off`: no check.

A second setting, the **rules review source**, sets where the rules review reads the project rules from: `base_commit` (default) is the rules as committed on the base the task branched from, so a task cannot change the rubric it is judged by; `worktree` is the rules file in the task's working tree.

The effective protected list is the explicit list when one is set, else the default list: the effective project rules file (`AGENT.md` when none is set), `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`, `CODEOWNERS` and `.github/workflows/`. Each entry is a repository-relative exact file, a directory (trailing slash) or a glob using `* ? [ ]` (an entry ending in `/**` is the same as the directory form).

An **Automaton can authorise protected paths in advance**: its tasks may change those paths without an operator decision; such a change is recorded in the Automaton decision log and raised as an alert, and the gate only acts on the paths the Automaton did not authorise.

A new read-only route reports the action, the rules source, the effective protected list, every current hold and every Automaton that authorises some. A new action route sets an Automaton's authorised paths. The PWA gained an **Automata protected paths** card on the Overview sub-tab of the Automation screen, a chip on a held task, a banner on an Automaton whose tasks are held, an **Authorised paths** editor on the Automaton screen, and a **Protected paths** settings group. The Android and iPhone apps must give the operator the same information and the same way to answer the question. Capability parity is required; implementation parity is not.

## Changed REST contract

### `GET /api/autonomous/protected-paths`

Read-only. Only `GET` is served (any other method is a 405 with an `Allow: GET` header). While Automata are disabled it answers 503 (`autonomous disabled`); a daemon whose Automaton manager cannot report the status also answers 503. A federated peer needs the autonomous read capability (403 without it); bad auth is a 401. The status is built from the stored Automata alone and makes no git call, so a read is cheap and never disturbs a running worktree.

Optional query parameter:

- `automaton` - an Automaton id. Narrows `held` and `authorized` to that one Automaton. An id that matches nothing is a 200 with empty `held` and `authorized` lists, never a 404.

Body:

```json
{
  "action": "hold | warn | block | off",
  "rules_source": "base_commit | worktree",
  "paths": ["path/inside/the/repository"],
  "held": [
    {
      "automaton_id": "string",
      "automaton_title": "string",
      "task_id": "string",
      "task_title": "string",
      "paths": ["path/inside/the/repository"],
      "action": "string",
      "reason": "string (key absent when empty)",
      "question_id": "string (key absent when no question was raised)",
      "since": "RFC 3339 time",
      "base_commit": "string (key absent when empty)",
      "awaiting_spec_change": false
    }
  ],
  "authorized": [
    {
      "automaton_id": "string",
      "automaton_title": "string",
      "paths": ["path/inside/the/repository"]
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `action` | The protected-paths action in effect now (`autonomous.automation.protected_paths_action`). An unset or unrecognized setting reads `hold`. |
| `rules_source` | Where the rules review reads the project rules from, in effect now (`autonomous.automation.rules_review_source`). An unset or unrecognized setting reads `base_commit`. |
| `paths` | The effective protected list (`autonomous.automation.protected_paths`; the default list when none is set). Always a list, never `null`. |
| `held` | One entry per task held because its commit changed protected paths. Always a list, never `null`; empty when nothing is held or the `automaton` filter matches nothing. Sorted by Automaton id then task id. |
| `held[].automaton_id` | The held task's Automaton. Always present. |
| `held[].automaton_title` | The Automaton title; its id when it has no title. Always present. |
| `held[].task_id`, `held[].task_title` | The held task and its title. Always present. |
| `held[].paths` | The changed protected paths, sorted. Always a list, never `null`. Can be long: cap what you draw (the PWA caps at 20 and then "and N more"). |
| `held[].action` | The protected-paths action in force when the hold was raised. Always present. |
| `held[].reason` | Why the hold was raised. Absent when empty. |
| `held[].question_id` | The operator question that waits for an answer. Absent when the daemon had no question store wired, in which case the hold has no question to open. |
| `held[].since` | When the hold was raised. Always present. |
| `held[].base_commit` | The commit the task started from, the state a rejection puts the paths back to. Absent when empty. |
| `held[].awaiting_spec_change` | True once the operator chose to change the task instructions and the task waits for that edit. |
| `authorized` | One entry per Automaton that authorises protected paths in advance. Always a list, never `null`; empty when none do or the `automaton` filter matches nothing. Sorted by Automaton id. |
| `authorized[].automaton_id`, `authorized[].automaton_title` | The Automaton and its title (its id when it has no title). Always present. |
| `authorized[].paths` | The authorised protected paths, sorted. Always a list, never `null`. |

The daemon omits empty values rather than sending `null`: `reason`, `question_id` and `base_commit` are **absent keys** when they have no value. A client must treat an absent key (and, to be safe, a `null`) as "no value". Status codes: 200 with the status, 401, 403 (peer lacks the read capability), 405, 503.

### `POST /api/autonomous/prds/{id}/set_authorized_paths`

Sets the protected paths this Automaton's tasks may change without an operator decision. Body:

```json
{
  "paths": ["path/inside/the/repository"]
}
```

`paths` is required. Each entry is a repository-relative exact file, a directory (trailing slash) or a glob; entries are trimmed, cleaned and de-duplicated. An empty list clears the authorisation. An entry that is absolute, has a `..` segment or is longer than 256 bytes, or a list of more than 100 entries, is refused with a 400 that names it. The change is recorded on the decision timeline and audited. A federated peer needs the autonomous write capability (403 without it). Status codes: 200 with the updated Automaton (which carries `authorized_paths` when any are set), 400 (a refused entry), 403, 404 (unknown Automaton id), 405.

## Automaton and task JSON field changes

The Automaton JSON gains one optional field, the task JSON two. All are **absent** (the key is omitted, never `null`) when they have no value, and removed again when the hold is resolved.

| Field | Where | Meaning |
|---|---|---|
| `authorized_paths` | Automaton | The protected paths this Automaton's tasks may change without an operator decision. Absent when none are set; an empty list never appears. |
| `protected_path_hold` | task | The hold, while the task is held. Absent when the task is not held; removed when the hold is resolved. |
| `approved_protected_paths` | task | The protected paths the operator approved for this task, after an `approve` answer. Absent when none were approved. The approval covers this task only; the Automaton's `authorized_paths` are never touched. |

`protected_path_hold` fields:

| Field | Meaning |
|---|---|
| `paths` | The changed protected paths. Always a list, never `null`. |
| `action` | The protected-paths action in force when the hold was raised. |
| `reason` | Why the hold was raised. Optional; absent when empty. |
| `question_id` | The question raised for the hold. Optional; absent when no question store was wired. |
| `base_commit` | The commit the task started from, the state a rejection puts the paths back to. Optional; absent when empty. |
| `since` | When the hold was raised. |
| `awaiting_spec_change` | True once the operator chose to change the task instructions. Optional; absent while false. |

A held task carries status `blocked` and an `error` that starts with `protected_path:` followed by up to five of the paths. Under the `block` action the task also reads `blocked`, but its `error` starts with `protected_path_blocked:` and no question is raised. While the hold awaits a spec change, the `error` reads "protected_path: change the task instructions, then reset the task". No new status value is added: a held or blocked task reads `blocked`.

## New config keys

All three keys sit under `autonomous.automation` in the daemon config, are returned by the existing config read, are written through `PUT /api/autonomous/config` in its `automation` object (or `PUT /api/config` as `autonomous.automation.<key>`), and take effect without a restart. A key that was never set is omitted from the config read, so the app must show the default.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `protected_paths` | JSON array of repository-relative strings; the `PUT /api/config` surface also accepts a comma separated string | the default list (`AGENT.md` when no rules file is set, `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`, `CODEOWNERS`, `.github/workflows/`) | The paths a task commit must not touch without the operator. An empty list reads the default list. |
| `protected_paths_action` | string: `hold`, `warn`, `block`, `off` | `hold` | What happens when a task commit touches a protected path. An unset key reads `hold`. |
| `rules_review_source` | string: `base_commit`, `worktree` | `base_commit` | Where the rules review reads the project rules from. An unset key reads `base_commit`. |

All three are strict on write: `protected_paths_action` must be one of its four words, `rules_review_source` one of its two, and every entry of `protected_paths` must be repository-relative (not absolute, no `..` segment, at most 256 bytes) with at most 100 entries; a refused value rejects the whole write with a 400 that names the allowed words or the offending entry. An absent key keeps the current value. The app must send exactly the allowed words and clean entries.

## The question

Each hold raises one operator question in the existing questions store (`GET /api/questions`, `GET /api/questions/{id}`, answered with `POST /api/questions/{id}/answer` and `chosen_option_id`), the same store and calls the app already uses for other Automaton questions. The question is a system question (`asked_by` is `autonomous`). Its fields:

- `label`: `protected_paths`. Use this to recognise the question.
- `context`: `task`; the question carries `task_id`. The wire field `prd_id` is the existing name for the Automaton scope and holds the held Automaton's id. Do not show those words to the operator.
- `text`: names the Automaton and the task, lists the changed protected paths (at most 20, then "and N more") and says in one sentence what each answer does. It ends "Nothing changes until you answer."
- `options` (the ids to send as `chosen_option_id`):
  - `approve` - "Approve this change": keeps the change and the task completes; the approval covers only these paths for this task.
  - `reject` - "Reject it": puts the listed paths back to their state before the task, in a new commit (history is never rewritten), and runs the task again.
  - `change_spec` - "Change the spec": keeps the task blocked so you can edit its instructions, then reset it.
- `default`: `hold`. This is **not** an option id: it means keep the hold as it is, and nothing changes until the operator answers `approve`, `reject` or `change_spec`. An answer with any other id leaves the hold as it is.

When the question is answered, the daemon acts on the hold, the `protected_path_hold` object disappears from the task (and `approved_protected_paths` appears on an `approve`) and the Automaton runs on. Only the question the hold itself names is acted on: an answer to a different or stale question is recorded and ignored. `reject` reverts the change to the listed paths, so the app should make that clear when it asks for confirmation.

## PWA changes (the contract to match)

- **Card.** The Overview sub-tab of the Automation screen has an **Automata protected paths** card, drawn from `GET /api/autonomous/protected-paths` and refreshed every 30 seconds. It has a small help link, a read-only note, an action line and a rules-source line (each shows the word with its explanation; a value this version does not know is shown as it came), the protected paths in effect (expandable, capped at 20 with "and N more", or "No path is protected."), then either "No Automaton task is held on protected paths." or one row per `held` entry. Each row reads "Task `<title or id>` of Automaton `<name>` is held on protected paths" and has an expandable list of the changed protected paths (capped at 20 with "and N more"), the reason, "Held since `<time>`", the "Waiting for the task instructions to change" line when `awaiting_spec_change` is true, and an **Open the question** button when `question_id` is present. One line per `authorized` entry reads "Automaton `<name>` authorises: `<paths>`". A failed read reads "Could not load the Automata protected-paths status: `<message>`". Every value is escaped before it is drawn, and long paths wrap rather than scroll.
- **Held task.** A task whose JSON has `protected_path_hold` shows a warning chip "Protected paths (`<file count>`)" (tooltip: "This task is held: its commit changed protected paths") and, in its body, the changed protected paths, the reason and the **Open the question** button.
- **Held Automaton.** An Automaton whose tasks are held on protected paths shows a banner "This Automaton is held: `<n>` of its tasks changed protected paths".
- **Authorised paths.** The Automaton screen has an **Authorised paths** section: a hint ("Protected paths this Automaton's tasks may change without a decision. Comma-separated and repository-relative; an empty field clears the list."), the current list (or "No path is authorised for this Automaton."), a comma-separated field (placeholder "docs/plans/, AGENT.md") and a **Save authorised paths** button that calls `POST /api/autonomous/prds/{id}/set_authorized_paths`, confirming "Authorised paths saved" or "Could not save the authorised paths: `<message>`".
- **Settings.** The Automaton settings screen has a group **Protected paths** with three fields: the protected paths (comma-separated, repository-relative; empty uses the default list), the action select (hold, warn, block, off; default hold) and the rules review source select (base_commit, worktree; default base_commit).

## New locale keys

All 39 keys are present in the PWA bundles. The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive through the localization pipeline; please send translations of these keys for DE, ES, FR and JA, and the PWA bundles will then mirror them in place of the placeholders. Please add the same keys, with the English values, to the app's own string resources.

| Key | EN |
|---|---|
| `protected_paths_card_title` | Automata protected paths |
| `protected_paths_help_link` | About protected paths |
| `protected_paths_read_only` | Read-only status. Change the protected paths, the action and the rules review source in the Automata settings; answer a held task through its question. |
| `protected_paths_action_line` | Protected-paths action: %1$s |
| `protected_paths_source_line` | Rules review source: %1$s |
| `protected_paths_list_line` | Protected paths in effect (%1$s) |
| `protected_paths_list_none` | No path is protected. |
| `protected_paths_action_hold` | hold (park the task until you answer) |
| `protected_paths_action_warn` | warn (record it and continue) |
| `protected_paths_action_block` | block (refuse the commit) |
| `protected_paths_action_off` | off (no check) |
| `rules_review_source_base_commit` | base commit (the rules as committed before the task) |
| `rules_review_source_worktree` | worktree (the rules file in the task working tree) |
| `protected_paths_empty` | No Automaton task is held on protected paths. |
| `protected_paths_held_task` | Task %1$s of Automaton %2$s is held on protected paths |
| `protected_paths_held_automaton` | Automaton %1$s has tasks held on protected paths |
| `protected_paths_paths` | Changed protected paths (%1$s) |
| `protected_paths_more` | and %1$s more |
| `protected_paths_reason` | Reason: %1$s |
| `protected_paths_since` | Held since %1$s |
| `protected_paths_awaiting_spec_change` | Waiting for the task instructions to change |
| `protected_paths_open_question` | Open the question |
| `protected_paths_authorised_line` | Automaton %1$s authorises: %2$s |
| `protected_paths_failed` | Could not load the Automata protected-paths status: %1$s |
| `protected_paths_chip` | Protected paths (%1$s) |
| `protected_paths_chip_tip` | This task is held: its commit changed protected paths |
| `protected_paths_banner` | This Automaton is held: %1$s of its tasks changed protected paths |
| `protected_paths_authorised_title` | Authorised paths |
| `protected_paths_authorised_hint` | Protected paths this Automaton's tasks may change without a decision. Comma-separated and repository-relative; an empty field clears the list. |
| `protected_paths_authorised_none` | No path is authorised for this Automaton. |
| `protected_paths_authorised_placeholder` | docs/plans/, AGENT.md |
| `protected_paths_authorised_label` | Authorised paths, comma-separated |
| `protected_paths_authorised_save` | Save authorised paths |
| `protected_paths_authorised_saved` | Authorised paths saved |
| `protected_paths_authorised_failed` | Could not save the authorised paths: %1$s |
| `settings_automation_protected_paths_group` | Protected paths |
| `settings_automation_protected_paths` | Automaton protected paths (comma-separated, repository-relative; empty uses the default list) |
| `settings_automation_protected_paths_action` | Automaton protected-paths action (hold, warn, block or off; default hold) |
| `settings_automation_rules_review_source` | Automaton rules review source (base_commit or worktree; default base_commit) |

## Other surfaces (for context, no app work)

The same read is available over MCP (`autonomous_protected_paths`, read-only, and `autonomous_prd_set_authorized_paths`), the CLI (`datawatch autonomous protected-paths [--automaton ID] [--json]` and `datawatch autonomous authorize-paths <automaton-id> [--path P ...] [--clear]`) and the chat channels (`autonomous protected-paths [automaton-id]` and the authorise verb). None of them answers the question; the answer always goes through the question store.

## Where to look in datawatch

- `internal/server/protected_paths.go`: the `GET /api/autonomous/protected-paths` handler and the strict write checks for the three config keys.
- `internal/server/autonomous.go`: the `set_authorized_paths` action case behind `POST /api/autonomous/prds/{id}/set_authorized_paths`.
- `internal/autonomous/protected_paths_gate.go`: the gate, once per worker attempt after verification, from the base commit.
- `internal/autonomous/protected_paths_hold.go`: the hold, the warning, the block and the question (label, option ids, default).
- `internal/autonomous/protected_paths_answer.go`: what `approve`, `reject` and `change_spec` each do.
- `internal/autonomous/protected_paths_status.go`: the status model (`ProtectedPathsStatus`, `ProtectedPathsHeldEntry`, `ProtectedPathsAuthorizedEntry`) and how it is built.
- `internal/autonomous/models.go`: the `ProtectedPathHold` struct and the `authorized_paths`, `protected_path_hold` and `approved_protected_paths` fields.
- `internal/config/config.go`: the three config keys, their defaults, `DefaultProtectedPaths` and the `Effective*` readers.
- `docs/api/openapi.yaml`: `/api/autonomous/protected-paths`, `/api/autonomous/prds/{id}/set_authorized_paths` and the `ProtectedPathsStatus`, `ProtectedPathsHeldEntry`, `ProtectedPathsAuthorizedEntry` and `ProtectedPathHold` schemas.
- `internal/server/web/app.js`: `protectedPathsCardHtml`, `protectedPathsChipHtml`, `protectedPathsBannerHtml`, `protectedPathsAuthorisedHtml` and the settings fields.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.

## Acceptance (mobile)

Both the Android and the iPhone app:

- Read `GET /api/autonomous/protected-paths` with the same auth as their other Automata reads, poll it no faster than every 30 seconds, and handle 401, 403, 405 and 503 without crashing. A 503 or a failed read shows "Could not load the Automata protected-paths status: ..." (or keeps the last good card marked as stale), never an empty or zero card.
- Show an **Automata protected paths** card on the Automation screen with the action line, the rules-source line, the protected paths in effect, either the empty text or one row per `held` entry with the task wording, the changed protected paths list, the reason, the held-since time, the awaiting-spec-change state and, when `question_id` is present, a control that opens that question, and one line per `authorized` entry naming the Automaton and its authorised paths.
- Treat absent keys (and `null`) as "no value": no `reason`, `question_id` or `base_commit` shows no such line; an empty `held` or `authorized` shows the empty text; `paths` is drawn as an empty list when absent. Do not draw an unbounded path list: cap it (the PWA caps at 20, then "and N more"), and let long paths wrap on a phone-width screen with no horizontal scrolling.
- Support the `automaton` query filter (or use it when showing the holds of one Automaton) and show an empty state, not an error, when it matches nothing.
- Show a chip on a task whose JSON carries `protected_path_hold` (with the file count) and a banner on an Automaton whose tasks are held; show nothing when the key is absent. Do not treat a held task as failed: it is `blocked` and waiting for an answer. A `protected_path_blocked:` error is a blocked task with no question: show it as blocked and point to the authorised paths or the task edit.
- Let the operator answer the question: show the three options with the ids `approve`, `reject` and `change_spec`, send the chosen option id through the existing questions answer call, never send `hold` as a chosen option (it is only the default), and ask for confirmation before `reject` because it puts the listed paths back to their state before the task. After an answer, the next read no longer lists the hold, and the chip or banner disappears.
- Show the **Authorised paths** section on the Automaton screen and let the operator save or clear the list through `POST /api/autonomous/prds/{id}/set_authorized_paths` with `{"paths": [...]}`: an empty list clears the authorisation, an unknown Automaton is a 404, and a refused entry is a 400 that names it. Send only repository-relative entries, never an absolute path or one with a `..` segment, because the daemon answers those with a 400 and changes nothing.
- Show the **Protected paths** settings group: the protected paths (comma-separated, repository-relative; empty uses the default list), the action (`hold`, `warn`, `block`, `off`; default `hold`) and the rules review source (`base_commit`, `worktree`; default `base_commit`). A key missing from the config read shows its default. The app never sends an empty value or a word outside the allowed set, because the daemon answers those with a 400 and changes nothing.
- Add all 39 locale keys above, with the English values, to the app's own string resources in all of its languages (EN, DE, ES, FR, JA), with the English text as the placeholder in DE, ES, FR and JA until translations land. Every new string says Automaton or Automata.

Capability parity is required; implementation parity is not: each platform uses its idiomatic screens and delivery.

The issue text above is the exact body as posted.

## Corrections posted as comments

- Edited at merge (v9.104.0): title and parent release set; the card moved to the Housekeeping sub-tab of the Automation screen. URL: https://github.com/dmz006/datawatch-app/issues/381
