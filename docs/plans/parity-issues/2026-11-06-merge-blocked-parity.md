# feat(automata): Automaton merge-blocked state: task merge_block, merge_blocked triage record, merge retry interval setting (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

When a finished task's work cannot be merged into the Automaton branch yet (for example the Automaton worktree has uncommitted changes), datawatch now records the reason on the task instead of leaving it only in the daemon log. While a merge is blocked the daemon retries on a timer and holds the next task. The Automaton card and the Triage card show the block, its reason and the next action. A new setting, `autonomous.parallel.merge_retry_seconds`, sets how long the daemon waits between retries.

## Changed REST contract

No new endpoint. Existing Automaton reads (`GET /api/autonomous/prds`, `GET /api/autonomous/prds/{id}`) and triage reads carry the new fields.

### Task field: `merge_block`

`Task.merge_block` is an optional object, omitted when nothing blocks the task's merge:

```json
{
  "kind": "string (for example dirty_worktree)",
  "reason": "string",
  "action": "string (the next action for the operator)",
  "files": ["string"],
  "since": "RFC 3339 time",
  "last_retry_at": "RFC 3339 time",
  "attempts": 0
}
```

Every field is optional and omitted when empty or zero. A client must tolerate a `kind` it does not know and a missing `reason` or `action`.

### Triage record: status `merge_blocked` and `merge_blocks`

- `triage.status` can now also be `merge_blocked`, next to `failed`, `blocked` and `merge_pending`. It is written for an Automaton that is still running and has at least one finished task whose merge is blocked, and only when the Automaton has no failed or blocked record of its own.
- `triage.merge_blocks` (array, omitted when empty) lists each blocked task. When the Automaton already has a `failed` or `blocked` record, the same list rides on that record.

```json
{
  "task_id": "string",
  "task_title": "string",
  "reason": "string",
  "action": "string",
  "since": "RFC 3339 time",
  "attempts": 0
}
```

`task_title`, `reason`, `action`, `since` and `attempts` are omitted when empty. A client that lists triage records must tolerate the new status; an unknown status shows as plain text.

## New config key

Under `autonomous.parallel` in the daemon config, returned and accepted by `GET` and `PUT /api/config` and the Automata config object, and shown in the Automaton settings screen.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `merge_retry_seconds` | whole number >= 0 | `60` | Seconds between timed retries of a pending task merge. `0` turns the timed retry off and must survive a round trip; unset reads 60; a negative value is rejected with a 400. |

## PWA changes

- The Automaton card shows a **merge blocked** status when a task has a `merge_block`: the reason, how long it has been blocked, and the next action, plus a help link.
- The Triage card lists a `merge_blocked` record as a selectable item; opening it shows the blocked tasks (title, why, blocked for, next action).
- The Settings page has a field for the task merge retry interval.

## New locale keys

| Key | EN |
|---|---|
| `settings_parallel_merge_retry_seconds` | Task merge retry interval (seconds between retries of a pending task merge; 0 = off; default 60) |
| `settings_parallel_merge_retry_seconds_help` | How long to wait before the daemon tries again to merge a task branch whose merge is still pending. While a merge is pending the next task is held. 0 turns the timed retry off. |
| `merge_blocked_status` | merge blocked |
| `merge_blocked_card_title` | Automaton waiting to merge |
| `merge_blocked_what` | This Automaton has finished work that cannot merge yet, so it is waiting to merge. |
| `merge_blocked_line` | Merge blocked: %1$s. Next: %2$s |
| `merge_blocked_why` | Why: %1$s |
| `merge_blocked_how_long` | Blocked for: %1$s |
| `merge_blocked_next_action` | Next action: %1$s |
| `merge_blocked_reason_unknown` | reason not recorded |
| `merge_blocked_action_unknown` | no action recorded |
| `merge_blocked_age_now` | less than a minute |
| `merge_blocked_age_minutes` | %1$s min |
| `merge_blocked_age_hours` | %1$s h |
| `merge_blocked_age_days` | %1$s d |
| `merge_blocked_help_link` | What does a blocked merge mean? Open the Automaton merge help |

The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive; please send translations for those four languages. Add the same keys, with the English values, to the app's own string resources.

## Where to look in datawatch

- `internal/autonomous/models.go`: `Task.MergeBlock` and the `MergeBlock` type.
- `internal/autonomous/merge_block.go`, `internal/autonomous/merge_retry.go`: block detection and the timed retry.
- `internal/autonomous/triage_record.go`: `merge_blocked` status, `TriageMergeBlock`, `merge_blocks`.
- `internal/config/config.go`: `merge_retry_seconds`, its default and validation.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.

## Acceptance (mobile)

- An Automaton with a task `merge_block` shows a merge blocked state with the reason and the next action; a missing reason or action shows the "not recorded" text, not a blank.
- The blocked time is shown as less than a minute, minutes, hours or days from `since`.
- A `merge_blocked` triage record is a selectable item in the Triage list and lists every entry of `merge_blocks`. An unknown triage status or merge block `kind` does not break the screen.
- The settings field reads and writes `merge_retry_seconds`; 0 stays 0 (not reset to 60) and a negative value is never sent.
- All 16 locale keys above exist in EN, DE, ES, FR and JA, with English as the placeholder in DE, ES, FR and JA.
- On a phone-width screen the new rows wrap without horizontal scrolling.
