**Title:** [sync] feat(automaton): uncommitted-work hold and state
**Issue:** https://github.com/dmz006/datawatch-app/issues/320
**Number:** 320

## Issue text
Parent release: datawatch next release

## What changed in datawatch

After a task finishes in an Automaton that commits its own work, datawatch now checks that the Automaton worktree is clean. When files that git still sees as uncommitted are left behind (the task commit failed, or files remain after it), the **uncommitted-work policy** decides what happens:

- `hold` (default): the task is parked in the `blocked` state, the hold is recorded on the task, and one operator question is raised. Nothing changes until the operator answers.
- `warn`: the finding is recorded in the Automaton decision log and raised as an alert, and the task completes. No hold object is written and no question is raised.
- `off`: no check and no commit attempt.

A second check runs before an Automaton is reported finished and always covers the whole worktree. Files that no single task owns put the **Automaton** itself on hold (kind `worktree`); the Automaton then carries the hold object and one question is raised.

The **uncommitted-work scope** sets which files the per-task check looks at: `task_files` (default) is the files the task itself changed, `worktree` is every uncommitted file in the worktree.

A new read-only route reports the policy, the scope and every current hold. The PWA gained an **Automata uncommitted work** card on the Overview sub-tab of the Automation screen, a chip and file list on a held task, a banner on a held Automaton, and two settings fields. The Android and iPhone apps must give the operator the same information and the same way to answer the question. Capability parity is required; implementation parity is not.

## Changed REST contract

### `GET /api/autonomous/uncommitted-work`

Read-only. Only `GET` is served (any other method is a 405 with an `Allow: GET` header). While Automata are disabled it answers 503 (`autonomous disabled`); a daemon whose Automaton manager cannot report the status also answers 503. A federated peer needs the autonomous read capability (403 without it); bad auth is a 401. The status is built from the stored Automata alone and makes no git call, so a read is cheap and never disturbs a running worktree.

Optional query parameter:

- `automaton` - an Automaton id. Narrows `held` to that one Automaton. An id that matches nothing is a 200 with an empty `held` list, never a 404.

Body:

```json
{
  "policy": "hold | warn | off",
  "scope": "task_files | worktree",
  "held": [
    {
      "automaton_id": "string",
      "name": "string",
      "task_id": "string (key absent for a worktree hold)",
      "task_title": "string (key absent for a worktree hold)",
      "kind": "task | worktree",
      "files": ["path/inside/the/worktree"],
      "outside_scope": ["path/inside/the/worktree"],
      "reason": "string (key absent when empty)",
      "question_id": "string (key absent when no question was raised)",
      "held_at": "RFC 3339 time"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `policy` | The uncommitted-work policy in effect now (`autonomous.automation.uncommitted_work_policy`). An unset or unrecognized setting reads `hold`. |
| `scope` | The scope of the per-task check in effect now (`autonomous.automation.uncommitted_work_scope`). An unset or unrecognized setting reads `task_files`. |
| `held` | One entry per held task and per held Automaton. Always a list, never `null`; empty when nothing is held or the `automaton` filter matches nothing. |
| `held[].automaton_id` | The held Automaton. Always present. |
| `held[].name` | The Automaton title; its id when it has no title. Always present. |
| `held[].task_id`, `held[].task_title` | The held task. Absent for a `worktree` hold. |
| `held[].kind` | `task` for a hold on one task, `worktree` for files no single task owns. |
| `held[].files` | The held paths, sorted. Always a list, never `null`. Can be long: cap what you draw (the PWA shows 50 and then "and N more"). |
| `held[].outside_scope` | The subset of `files` that the task did not declare. Always a list, never `null`. |
| `held[].reason` | Why the hold was raised. Absent when empty. |
| `held[].question_id` | The operator question that waits for an answer. Absent when the daemon had no question store wired, in which case the hold has no question to open. |
| `held[].held_at` | When the hold was raised. |

The daemon omits empty values rather than sending `null`: `task_id`, `task_title`, `reason` and `question_id` are **absent keys** when they have no value. A client must treat an absent key (and, to be safe, a `null`) as "no value". Status codes: 200 with the status, 401, 403 (peer lacks the read capability), 405, 503.

## Automaton and task JSON field changes

A new optional object `uncommitted_work` appears on both the Automaton JSON and each task JSON. It is **absent** (the key is omitted, never `null`) when no hold is recorded, and it is removed again when the hold is resolved. On a task it is set while the task is held; on an Automaton it is set while the Automaton is held on files no task owns. A `warn` finding does not write it.

| Field | Meaning |
|---|---|
| `kind` | `task` (a hold on one task) or `worktree` (files no single task owns). |
| `policy` | The policy in force when the hold was raised. Optional. |
| `files` | The held paths. Optional; treat absent as an empty list. |
| `outside_scope` | The held paths the task did not declare. Optional; treat absent as an empty list. |
| `reason` | Why the hold was raised. Optional. |
| `question_id` | The question raised for the hold. Optional; absent when no question store was wired. |
| `held_at` | When the hold was raised. |

A held task also carries status `blocked` and an `error` that starts with `uncommitted_work:` followed by up to five of the paths. No new status value is added: a held task reads `blocked`, and a held Automaton also reads `blocked`: the check runs as the Automaton is about to finish, and the roll-up then commits `blocked` instead of `completed`, with a plain-sentence `blocked_reason` (the cause, up to five of the files, and what to do). A held Automaton is therefore never `completed` and never listed as merge ready.

## New config keys

Both keys sit under `autonomous.automation` in the daemon config, are returned by the existing config read, are written through `PUT /api/autonomous/config` in its `automation` object (or `PUT /api/config` as `autonomous.automation.<key>`), and take effect without a restart. A key that was never set is omitted from the config read, so the app must show the default.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `uncommitted_work_policy` | string: `hold`, `warn`, `off` | `hold` | What the per-task check does with work that is still uncommitted after verification. An unset key reads `hold`. |
| `uncommitted_work_scope` | string: `task_files`, `worktree` | `task_files` | Which files the per-task check covers. An unset key reads `task_files`. The check before an Automaton is reported finished always covers the whole worktree whatever this says. |

Both are strict on write: a present key must be one of its allowed words, and an empty value, a non-string or any other word rejects the whole write with a 400 that names the allowed words. An absent key keeps the current value. The app must send exactly one of the allowed words.

## The question

Each hold raises one operator question in the existing questions store (`GET /api/questions`, `GET /api/questions/{id}`, answered with `POST /api/questions/{id}/answer` and `chosen_option_id`), the same store and calls the app already uses for other Automaton questions. The question is a system question (`asked_by` is `autonomous`). Its fields:

- `label`: `uncommitted_work`. Use this to recognise the question.
- `context`: `task` for a task hold (the question also carries `task_id`) and `prd` for a worktree hold (no `task_id`). The wire value `prd` and the wire field `prd_id` are the existing names for the Automaton scope; `prd_id` holds the held Automaton's id in both cases. Do not show those words to the operator.
- `text`: names the Automaton, the task (when there is one), the cause and the files, and marks files outside the task's declared files. It ends "Nothing changes until you answer."
- `options` (the ids to send as `chosen_option_id`):
  - `commit` - "Commit them": stages exactly the listed files and commits them to the Automaton branch.
  - `discard` - "Discard them": removes the listed changes from the worktree. They cannot be recovered.
  - `retry` - "Retry the task" for a task hold, "Check again" for a worktree hold: looks at the worktree again and runs the check once more.
- `default`: `hold`. This is **not** an option id: it means keep the hold as it is, and nothing changes until the operator answers `commit`, `discard` or `retry`. An answer with any other id leaves the hold as it is.

When the question is answered with `commit`, `discard` or `retry`, the daemon acts on the hold and, once it is resolved, the `uncommitted_work` object disappears from the task or Automaton and the Automaton runs on. If the hold names a different question (a stale one), the answer is ignored. `discard` is destructive, so the app should make that clear when it asks for confirmation.

## PWA changes (the contract to match)

- **Card.** The Overview sub-tab of the Automation screen has an **Automata uncommitted work** card, drawn from `GET /api/autonomous/uncommitted-work` and refreshed every 30 seconds, beside the Automata cleanup status card. It has a small help link, a read-only note, a policy line and a scope line (each shows the word with its explanation; a value this version does not know is shown as it came), then either "No Automaton is held on uncommitted work." or one row per `held` entry. A task row reads "Task `<title or id>` of Automaton `<name>` is held on uncommitted work"; a worktree row reads "Automaton `<name>` is held on uncommitted work in its worktree". Each row has an expandable list of the uncommitted files and one of the files outside the declared files (capped at 50 with "and N more"), the reason, "Held since `<time>`" and an **Open the question** button when `question_id` is present. A failed read reads "Could not load the Automata uncommitted-work status: `<message>`". Every value is escaped before it is drawn, and long paths wrap rather than scroll.
- **Held task.** A task whose JSON has `uncommitted_work` shows a warning chip "Uncommitted work (`<file count>`)" (tooltip: the reason, or the chip tip text) and, in its body, the file lists, the reason and the **Open the question** button.
- **Held Automaton.** An Automaton whose own JSON has `uncommitted_work` shows a banner "This Automaton is held: its worktree has `<n>` uncommitted files that no task owns", with the file lists, the reason and the **Open the question** button.
- **Settings.** The Automaton settings screen has a group **Uncommitted-work check** with two selects: policy (hold, warn, off; default hold) and scope (task_files, worktree; default task_files).

## New locale keys

All 26 keys are present in the PWA bundles. The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive through the localization pipeline; please send translations of these keys for DE, ES, FR and JA, and the PWA bundles will then mirror them in place of the placeholders. Please add the same keys, with the English values, to the app's own string resources.

| Key | EN |
|---|---|
| `uncommitted_work_card_title` | Automata uncommitted work |
| `uncommitted_work_help_link` | About the uncommitted-work check |
| `uncommitted_work_read_only` | Read-only status. Change the policy and scope in the Automata settings; answer a held Automaton through its question. |
| `uncommitted_work_policy_line` | Uncommitted-work policy: %1$s |
| `uncommitted_work_scope_line` | Uncommitted-work scope: %1$s |
| `uncommitted_work_policy_hold` | hold (park the task until you answer) |
| `uncommitted_work_policy_warn` | warn (record it and continue) |
| `uncommitted_work_policy_off` | off (no check) |
| `uncommitted_work_scope_task_files` | task files (the files the task changed) |
| `uncommitted_work_scope_worktree` | worktree (every uncommitted file) |
| `uncommitted_work_empty` | No Automaton is held on uncommitted work. |
| `uncommitted_work_held_task` | Task %1$s of Automaton %2$s is held on uncommitted work |
| `uncommitted_work_held_automaton` | Automaton %1$s is held on uncommitted work in its worktree |
| `uncommitted_work_files` | Uncommitted files (%1$s) |
| `uncommitted_work_more_files` | and %1$s more |
| `uncommitted_work_outside_scope` | Outside the declared files (%1$s) |
| `uncommitted_work_reason` | Reason: %1$s |
| `uncommitted_work_held_since` | Held since %1$s |
| `uncommitted_work_open_question` | Open the question |
| `uncommitted_work_failed` | Could not load the Automata uncommitted-work status: %1$s |
| `uncommitted_work_chip` | Uncommitted work (%1$s) |
| `uncommitted_work_chip_tip` | This task is held: files it left are still uncommitted in the Automaton worktree |
| `uncommitted_work_banner` | This Automaton is held: its worktree has %1$s uncommitted files that no task owns |
| `settings_automation_uncommitted_work_group` | Uncommitted-work check |
| `settings_automation_uncommitted_work_policy` | Automaton uncommitted-work policy (hold, warn or off; default hold) |
| `settings_automation_uncommitted_work_scope` | Automaton uncommitted-work scope (task_files or worktree; default task_files) |

## Other surfaces (for context, no app work)

The same read is available over MCP (`autonomous_uncommitted_work`, read-only), the CLI (`datawatch autonomous uncommitted-work [--automaton ID] [--json]`) and the chat channels (`autonomous uncommitted-work [automaton-id]`). None of them commits or discards anything; the answer always goes through the question.

## Where to look in datawatch

- `internal/server/uncommitted_work.go`: the route handler and the strict write checks for the two config keys.
- `internal/autonomous/uncommitted_work_status.go`: the status model (`UncommittedWorkStatus`, `UncommittedWorkEntry`) and how it is built.
- `internal/autonomous/models.go`: the `UncommittedWorkHold` struct and the `uncommitted_work` field on the Automaton and the task.
- `internal/autonomous/uncommitted_work_hold.go`: the hold, the warning and the question (label, option ids, default).
- `internal/autonomous/uncommitted_work_answer.go`: what each answer does.
- `internal/config/config.go`: the two config keys, their defaults and the `Effective*` readers.
- `docs/api/openapi.yaml`: `/api/autonomous/uncommitted-work` and the `UncommittedWorkStatus` and `UncommittedWorkEntry` schemas.
- `internal/server/web/app.js`: `uncommittedWorkCardHtml`, `uncommittedWorkChipHtml`, `uncommittedWorkTaskHtml`, `uncommittedWorkBannerHtml` and `_automationUncommittedWorkLoader`.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.

## Acceptance (mobile)

Both the Android and the iPhone app:

- Read `GET /api/autonomous/uncommitted-work` with the same auth as their other Automata reads, poll it no faster than every 30 seconds, and handle 401, 403, 405 and 503 without crashing. A 503 or a failed read shows "Could not load the Automata uncommitted-work status: ..." (or keeps the last good card marked as stale), never an empty or zero card.
- Show an **Automata uncommitted work** card on the Automation screen with the policy line, the scope line and either the empty text or one row per `held` entry, with the task or worktree wording, the file list, the list of files outside the declared files, the reason, the held-since time and, when `question_id` is present, a control that opens that question.
- Treat absent keys (and `null`) as "no value": no `task_id`, `task_title`, `reason` or `question_id` shows no such line; an empty `held` shows the empty text; `files` and `outside_scope` are drawn as empty lists when absent. Do not draw an unbounded file list: cap it (the PWA caps at 50, then "and N more"), and let long paths wrap on a phone-width screen with no horizontal scrolling.
- Support the `automaton` query filter (or use it when showing the holds of one Automaton) and show an empty state, not an error, when it matches nothing.
- Show a chip on a task whose JSON carries `uncommitted_work` (with the file count and the reason) and a banner on an Automaton whose own JSON carries it; show nothing when the key is absent. Do not treat the task as failed: it is `blocked` and waiting for an answer.
- Let the operator answer the question: show the three options with the ids `commit`, `discard` and `retry`, send the chosen option id through the existing questions answer call, never send `hold` as a chosen option (it is only the default), and ask for confirmation before `discard` because it cannot be undone. After an answer, the next read no longer lists the hold, and the chip or banner disappears.
- Show the **Uncommitted-work check** settings group: policy (`hold`, `warn`, `off`; default `hold`) and scope (`task_files`, `worktree`; default `task_files`). A key missing from the config read shows its default. The app never sends an empty value or a word outside the allowed set, because the daemon answers those with a 400 and changes nothing.
- Add all 26 locale keys above, with the English values, to the app's own string resources in all of its languages (EN, DE, ES, FR, JA), with the English text as the placeholder in DE, ES, FR and JA until translations land. Every new string says Automaton or Automata.

Capability parity is required; implementation parity is not: each platform uses its idiomatic screens and delivery.

## Correction comment posted

The issue body as first posted said a held Automaton "keeps its existing status". That was wrong, and the text above is the corrected wording. This correction was posted as a comment on the issue (https://github.com/dmz006/datawatch-app/issues/320#issuecomment-6539494079):

Correction to the issue text: it says a held Automaton "keeps its existing status". That is wrong.

When the whole-worktree check holds an Automaton as it is about to finish, the daemon sets the Automaton status to `blocked` (not `completed`) and fills `blocked_reason` with a plain sentence: the cause, up to five of the files, and what to do. A held Automaton is therefore never `completed` and is never listed as merge ready. No new status value is added: both a held task and a held Automaton read `blocked`.

Client impact: when `uncommitted_work` is present on an Automaton, expect `status` = `blocked` and show `blocked_reason` as the explanation. The acceptance criteria are otherwise unchanged.
