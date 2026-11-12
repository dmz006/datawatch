# [sync] Automaton merge-pending state: two new read endpoints, two config keys, merge badge and safe-to-delete report (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

Datawatch now checks whether a finished Automaton's work branch has been merged into `main`. The check is off by default. When an operator turns it on (or into shadow mode), a finished Automaton whose branch still has commits that are not on `main` is marked **merge pending**, and one alert is raised after the branch has sat unmerged for the configured number of minutes. A read-only **safe-to-delete** report lists which Automata work branches and worktrees can be cleaned up and which must stay. Nothing in this feature merges, deletes or pushes; cleaning up a branch stays a separate step the operator approves.

## Changed REST contract

Both new endpoints are read-only. Only `GET` is served (any other method is a 405), and a federated peer needs the autonomous read capability (403 without it). While Automata are disabled both return 503.

### `GET /api/autonomous/merge`

Returns the Automaton merge status:

```json
{
  "mode": "off | shadow | on",
  "alert_minutes": 30,
  "automata": [
    {
      "automaton_id": "string",
      "name": "string (the Automaton title, or its id when it has none)",
      "branch": "string (the Automaton work branch)",
      "state": "merged | pending | needs_look | empty | unknown",
      "commits_ahead": 0,
      "pending_since": "RFC 3339 time, or null",
      "alerted_at": "RFC 3339 time, or null",
      "would_alert": true
    }
  ],
  "shadow_decisions": [
    {
      "at": "RFC 3339 time",
      "automaton_id": "string",
      "name": "string",
      "branch": "string",
      "state": "merged | pending | needs_look | empty | unknown",
      "commits_ahead": 0,
      "would_alert": false,
      "repeat_count": 1,
      "last_seen_at": "RFC 3339 time"
    }
  ]
}
```

- `automata` has one entry per completed Automaton that has a work branch, sorted by `automaton_id`. `would_alert` is present only in shadow mode.
- `state` is read from the daemon's in-memory comparison, so a read never runs git; an Automaton the daemon has not compared yet reads `unknown`.
- `pending_since` and `alerted_at` are `null` when they do not apply. `alerted_at` stays `null` in shadow mode, which never alerts.
- While `mode` is `off`, both `automata` and `shadow_decisions` are empty arrays. `shadow_decisions` is also empty outside shadow mode, and is newest first.

### `GET /api/autonomous/safe-to-delete`

Returns the safe-to-delete report for Automata work branches and worktrees:

```json
{
  "safe": [
    { "branch": "automaton/<id>", "worktree": "path or empty", "reason": "string", "commits": 0 }
  ],
  "held": [
    { "branch": "automaton/<id>", "worktree": "path or empty", "state": "pending | needs_look | unknown | merged | empty", "dirty": true, "note": "string" }
  ],
  "held_dirty": 0
}
```

- `safe` lists branches whose commits are all on `main` (or that have none). `held` lists the ones that must stay: still merge pending, needing a look, not comparable, or with a worktree that has uncommitted changes. `held_dirty` counts the branches that are safe by state but held for uncommitted changes.
- `dirty` and `note` are omitted when empty or false.
- Status codes: 200 with the report, 500 when the branches cannot be listed, 503 when Automata are disabled or no worktree base repository is configured for Automata.

## Automaton JSON field changes

The Automaton object itself gains no new field. The existing optional `triage` record (the one the triage badge and the needs-attention card already read) gains one status value and two optional fields:

- `triage.status` can now also be `merge_pending`, next to `failed` and `blocked`. A client that lists triage records must tolerate the new value; it is written only for a finished Automaton whose branch is not merged yet.
- `triage.branch` (string) and `triage.commits_ahead` (integer) are set only on a `merge_pending` record: the work branch and how many of its commits are not on `main`. Both are omitted on `failed` and `blocked` records.

## New config keys

Both keys sit under `autonomous.automation` in the daemon config, are returned and accepted by the existing config endpoints (`GET` and `PUT` `/api/config`, and `autonomous.automation.*` in the Automata config object as `merge_pending_mode` and `merge_pending_alert_minutes`), and appear in the Automaton settings screen.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `merge_pending_mode` | string: `off`, `shadow`, `on` | `off` | `off` does nothing. `shadow` records what the check would do and reports it through the two read endpoints, but raises no alert and writes no triage record. `on` marks finished Automata merge pending and raises the alert. A key that is absent from the config reads as `off`. Over the wire an empty string is not accepted: `PUT /api/config` and the Automata config object reject an empty value, and any other value outside the three, with a 400, so the app must send one of the three words. |
| `merge_pending_alert_minutes` | whole number >= 0 | `30` | Minutes a finished Automaton branch may sit unmerged before one merge-pending alert is raised. `0` turns the alert off and survives a round trip; unset reads the default 30; a negative value is rejected with a 400. |

## PWA changes

### Merge badge

- A **merge badge** now appears on a finished Automaton's detail view, below the progress area, and on its Automation card in the list. It is drawn from `GET /api/autonomous/merge` and only for the states `pending` and `needs_look`; the states `merged`, `empty` and `unknown` draw no badge.
- On an Automation card the badge shows the member Automaton that most needs the operator: a member that needs a look wins over one that is merge pending.
- The badge label is **Automaton merge pending** (`pending`) or **Automaton needs a look** (`needs_look`), coloured warning and error. Its tooltip reads "Automaton branch `<branch>`, `<n>` commits not on main".
- The badge paints at once from the last good read and again when a fresh read lands. A failed read keeps the last good result, so a flaky poll never makes a badge blink off. Reads are shared and not repeated within 5 seconds.

### Safe-to-delete report view

- The **Overview** sub-tab of the Automation screen gets an **Automaton safe-to-delete report** card, drawn from `GET /api/autonomous/safe-to-delete` and refreshed every 60 seconds (the read runs git, so it polls slowly).
- The card says it is a read-only report of Automaton branches and offers no action: nothing in it deletes, merges or pushes.
- It has two sections, **Automaton branches safe to delete (n)** and **Automaton branches held (n)**. A safe row shows the branch, the "Automaton worktree: `<path>`" line, the reason and "Automaton commits not on main: n". A held row shows the branch, the worktree line, "Automaton branch state: `<state label>`", a warning line "The Automaton worktree has uncommitted changes." when `dirty` is set, and the `note`.
- Empty sections read "No Automaton branch is safe to delete." and "No Automaton branch is held."; a failed read reads "Could not load the Automaton safe-to-delete report: `<message>`".
- Every value is escaped before it is drawn, and long branch and worktree paths wrap rather than scroll.

### Settings

The Automaton settings screen has two new fields: **Automaton merge-pending check** (a select of off, shadow and on) and **Automaton merge-pending alert** (a whole number of minutes, 0 = off, default 30).

## New locale keys

| Key | EN |
|---|---|
| `settings_automation_merge_pending_mode` | Automaton merge-pending check (off, shadow or on; default off) |
| `settings_automation_merge_pending_alert_minutes` | Automaton merge-pending alert (minutes a finished Automaton branch may sit unmerged before an alert, 0 = off, default 30) |
| `merge_state_pending` | Automaton merge pending |
| `merge_state_needs_look` | Automaton needs a look |
| `merge_state_merged` | Automaton merged |
| `merge_state_empty` | Automaton branch empty |
| `merge_state_unknown` | Automaton state unknown |
| `merge_badge_title` | Automaton branch %1$s, %2$d commits not on main |
| `safe_delete_card_title` | Automaton safe-to-delete report |
| `safe_delete_read_only` | Read-only report of Automaton branches. Nothing here deletes, merges or pushes; cleaning up a branch is a separate step the operator approves. |
| `safe_delete_safe_heading` | Automaton branches safe to delete (%1$d) |
| `safe_delete_held_heading` | Automaton branches held (%1$d) |
| `safe_delete_safe_empty` | No Automaton branch is safe to delete. |
| `safe_delete_held_empty` | No Automaton branch is held. |
| `safe_delete_worktree` | Automaton worktree: %1$s |
| `safe_delete_commits` | Automaton commits not on main: %1$d |
| `safe_delete_state` | Automaton branch state: %1$s |
| `safe_delete_dirty` | The Automaton worktree has uncommitted changes. |
| `safe_delete_failed` | Could not load the Automaton safe-to-delete report: %1$s |

The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive through the localization pipeline; please send translations of these keys for DE, ES, FR and JA, and the PWA bundles will then mirror them in place of the placeholders. Please add the same keys, with the English values, to the app's own string resources.

## Other surfaces (for context, no app work)

The same two reads are available over MCP (`autonomous_merge`, `autonomous_safe_to_delete`), the CLI (`datawatch autonomous merge`, `datawatch autonomous safe-to-delete`) and the chat channels (`autonomous merge`, `autonomous safe-to-delete`). The merge-pending alert reads: "Automaton `<name>` merge pending", with a body naming the Automaton, its work branch, how many commits are not on `main`, how long it has been waiting, and the Automation it belongs to when it is a member of one.

## Where to look in datawatch

- `internal/server/merge.go`: the two route handlers and the list of merge REST operations.
- `internal/autonomous/merge_status.go`, `internal/autonomous/merge_safe_delete.go`: the response models.
- `internal/autonomous/triage_record.go`: the `merge_pending` triage status and the `branch` and `commits_ahead` fields.
- `internal/config/config.go`: the two config keys, their defaults and validation.
- `docs/api/openapi.yaml`: `/api/autonomous/merge`, `/api/autonomous/safe-to-delete` and the `MergeStatus`, `MergeAutomaton`, `MergeShadowDecision` and `SafeDeleteReport` schemas.
- `internal/server/web/app.js`: `mergeBadgeHtml`, `mergeBadgeForAutomation`, `loadMergeStatus`, `safeDeleteCardHtml` and `_automationSafeDeleteLoader`.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.

## Acceptance (mobile)

- The app reads `GET /api/autonomous/merge` and `GET /api/autonomous/safe-to-delete` with the same auth as its other Automata reads, and handles 401, 403, 405 and 503 without crashing; a 503 on the safe-to-delete read (Automata disabled, or no worktree base repository) shows the failed state, not an empty report.
- A finished Automaton in state `pending` shows an **Automaton merge pending** badge and one in state `needs_look` shows an **Automaton needs a look** badge, on its detail screen and on its Automation card in the list; `merged`, `empty` and `unknown` show no badge. On an Automation card, `needs_look` wins over `pending`.
- The badge tooltip or long-press text names the branch and the number of commits not on `main`.
- While `mode` is `off`, no badge appears and nothing breaks on the empty `automata` and `shadow_decisions` lists.
- A failed or slow merge read keeps the last good badges instead of clearing them, and merge reads are not repeated more than once every 5 seconds.
- The Automation screen has an **Automaton safe-to-delete report** card with **Automaton branches safe to delete (n)** and **Automaton branches held (n)** sections, the worktree, commits, state and dirty-worktree warning rows described above, the empty-section texts, and a read-only note. The card offers no delete, merge or push action.
- On a phone-width screen the card and the badge fit without horizontal scrolling: long branch names and worktree paths wrap, and the badge stays on one line.
- The two settings fields (**Automaton merge-pending check** and **Automaton merge-pending alert**) read and write `merge_pending_mode` and `merge_pending_alert_minutes`; an alert value of 0 is kept as 0, not reset to 30, and a mode outside off, shadow and on (and an empty mode) is never sent, because the daemon answers those with a 400.
- A `triage.status` of `merge_pending`, with `triage.branch` and `triage.commits_ahead`, does not break the existing triage badge or needs-attention list; an unknown status value is shown as plain text instead of failing.
- Every new string says Automaton or Automata (all 19 locale keys listed above do), and the 19 locale keys above exist in all of the app's languages (EN, DE, ES, FR, JA), with the English text as the placeholder in DE, ES, FR and JA until translations land.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/304 (datawatch-app#304).

The issue title is the heading of this file, and the issue body is the text above from "Parent release" through "Acceptance (mobile)". The `[sync]` title prefix follows the operator's written spec for this issue; the mobile-parity rule names `feat(area)` as the prefix, so a later parity issue should use that form.
