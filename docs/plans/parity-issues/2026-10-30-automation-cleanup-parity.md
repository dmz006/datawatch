# [sync] Automaton cleanup status: one new read endpoint, four config keys, cleanup status card and disk alert (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

Datawatch now cleans up after finished Automata in two passes and reports what each pass did. The **worktrees pass** removes finished Automaton work branches and worktrees that are safe to delete (all their commits are on `main`, no uncommitted changes) once they are older than the retention window. The **temp-file sweep** removes the daemon's own session scratch files that no live session needs any more. The default mode is `report`: both passes only count what they would remove and remove nothing. An operator who sets the mode to `on` lets the passes delete, at most a set number of entries per pass; `off` turns both passes off.

A disk guard sits beside the passes. It reads the usage of the disk holding the Automaton worktree base and of the disk holding the daemon session temp directory, and raises one rate-limited alert (at most once an hour) while either is at or above the configured percent. The disk check runs in every mode, `off` included; the alert only tells the operator to free space and never deletes anything by itself.

A new read-only status endpoint shows the last run of each pass, the disk readings and the alert state.

## Changed REST contract

### `GET /api/autonomous/cleanup`

Read-only. Only `GET` is served (any other method is a 405), and a federated peer needs the autonomous read capability (403 without it). While Automata are disabled it returns 503. The status is read from the daemon's in-memory watch, so a read never scans the disk.

```json
{
  "worktrees": {
    "mode": "off | report | on (empty string before the pass has ever run)",
    "last_run": "RFC 3339 time (key absent until the pass has run)",
    "removed": 0,
    "would_remove": 0,
    "skipped": 0,
    "failed": 0,
    "bytes_freed": 0,
    "last_error": "string (key absent when the last run failed nothing)",
    "owner_removed": 0,
    "net_removed": 0,
    "owner_bytes_freed": 0,
    "net_bytes_freed": 0
  },
  "temp": { "...": "same shape as worktrees" },
  "worktree_disk": { "path": "string", "percent": 0, "alert": false },
  "temp_disk": { "path": "string", "percent": 0, "alert": false },
  "disk_alert_percent": 85,
  "disk_alert": false,
  "last_disk_alert_at": "RFC 3339 time (key absent until the alert has fired)"
}
```

The daemon omits empty values rather than sending `null`: `last_run`, `last_error`, `worktree_disk`, `temp_disk`, the disk `percent` and `last_disk_alert_at` are **absent keys** when they have no value, never `null`. A client must treat an absent key (and, to be safe, a `null`) as "no value".

- `worktrees` is the finished-worktrees pass, `temp` the daemon temp-file sweep. Both always appear. A pass that has not run yet has no `last_run` key, an empty `mode` and all counts 0.
- `mode` is the mode the pass last ran with. In `report` mode `would_remove` counts what would be removed and `removed` stays 0.
- `owner_*` counts the entries belonging to this daemon's own finished Automata; `net_*` counts everything else the same pass removed (the safety net).
- `worktree_disk` and `temp_disk` come from the disk check, not from the passes. The disk check runs when the daemon starts and on every coordinator poll, in every mode, so each reading appears after the first check, whether or not its pass has run. `temp_disk` is therefore present on any daemon that has done its first check. `worktree_disk` stays absent for as long as no worktree base repository is configured. `percent` can be absent when the filesystem could not be measured; show "not available for `<path>`" then, not 0%. `alert` is true when that reading is at or above `disk_alert_percent`.
- `disk_alert_percent` is the threshold in force (default 85). `disk_alert` is true whenever either disk reading is at or above that threshold, as of the last disk check. It does not reflect the one-hour rate limit: the daemon's decision whether to raise the alert notification now is internal and is not sent on the wire. `last_disk_alert_at` is the last time the alert notification actually fired, and the key is absent until it has fired. While `disk_alert` stays true, `last_disk_alert_at` advances at most once an hour.
- Status codes: 200 with the status, 401 for bad auth, 403 (peer lacks the read capability), 405 (not `GET`), 503 (Automata disabled, or the status is unavailable).

## Automaton JSON field changes

None. No existing Automaton, Automation or triage object gains a field or a status value.

## New config keys

All four keys sit under `autonomous.automation` in the daemon config and are returned by the existing config read. They are written through `PUT /api/autonomous/config`, in its `automation` object (`cleanup_mode`, `cleanup_retention_hours`, `cleanup_max_removals_per_pass`, `disk_alert_percent`), and they appear in the Automaton settings screen. A key that was never set is omitted from the config read, so the app must show the default for it.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `cleanup_mode` | string: `off`, `report`, `on` | `report` | `off` runs neither pass. `report` records what the passes would remove and removes nothing. `on` lets the passes remove. An unset key reads `report`. A write with an empty value or any word outside the three is rejected with a 400, so the app must send one of the three words. |
| `cleanup_retention_hours` | whole number >= 1 | `24` | Hours a finished Automaton worktree is kept before the worktrees pass may remove it. Unset reads 24. A write of 0, a negative number or a fraction is rejected with a 400. |
| `cleanup_max_removals_per_pass` | whole number >= 1 | `10` | Most entries one pass may remove. Unset reads 10. A write of 0, a negative number or a fraction is rejected with a 400. |
| `disk_alert_percent` | whole number 1 to 100 | `85` | Percent of the disk in use at or above which the disk alert is raised. Unset reads 85. A write outside 1 to 100, or a fraction, is rejected with a 400. |

Unlike `merge_pending_alert_minutes`, there is no value of 0 that means off for the three numbers: 0 is refused on write. To stop cleanup, set `cleanup_mode` to `off`.

## PWA changes

### Cleanup status card

- The **Overview** sub-tab of the Automation screen gets an **Automata cleanup status** card, drawn from `GET /api/autonomous/cleanup` and refreshed every 30 seconds. It sits beside the Automaton safe-to-delete report card.
- The card says it is a read-only status of the two cleanup passes and that the mode and limits are changed in the Automata settings. It offers no action: nothing in it removes, cleans or runs a pass.
- It has two pass sections, **Automaton worktrees pass** and **Automata temp file sweep**. Each shows the mode line, then either "The Automata cleanup has not run yet." (when `last_run` is absent) or the last run time, the counts line (removed, would remove, skipped, failed) and the bytes freed; then the owner-versus-safety-net split, and the last error (in the error colour) or "No Automata cleanup error."
- A **disk use** section follows, with a badge **Automata disk alert** (error colour) while `disk_alert` is true and **Automata disk ok** (success colour) otherwise. The badge tooltip, and a line under it, show the time of the last alert when `last_disk_alert_at` is set. Under the badge are one line per disk reading (path and percent used, with the line in the error colour and bold when that reading's `alert` is true), the threshold line, or "No Automata disk reading yet." when neither reading exists yet. A reading with no `percent` reads "Automata disk use not available for `<path>`".
- A failed read reads "Could not load the Automata cleanup status: `<message>`". Every value is escaped before it is drawn, and long paths and messages wrap rather than scroll.

### Settings

The Automaton settings screen has four new fields: **Automaton cleanup** (a select of off, report and on, default report), **Automaton cleanup retention** (whole hours, minimum 1, default 24), **Automaton cleanup limit** (whole number, minimum 1, default 10) and **Automata disk alert** (whole percent, 1 to 100, default 85).

## New locale keys

| Key | EN |
|---|---|
| `settings_automation_cleanup_mode` | Automaton cleanup (off, report or on; default report) |
| `settings_automation_cleanup_retention_hours` | Automaton cleanup retention (hours a finished Automaton worktree is kept before cleanup, whole number, default 24) |
| `settings_automation_cleanup_max_removals_per_pass` | Automaton cleanup limit (most Automaton entries removed in one pass, whole number, default 10) |
| `settings_automation_disk_alert_percent` | Automata disk alert (percent of the disk in use that raises an alert, 1-100, default 85) |
| `cleanup_card_title` | Automata cleanup status |
| `cleanup_read_only` | Read-only status of the two Automata cleanup passes. Change the mode and limits in the Automata settings. |
| `cleanup_pass_worktrees` | Automaton worktrees pass |
| `cleanup_pass_temp` | Automata temp file sweep |
| `cleanup_mode_line` | Automata cleanup mode: %1$s |
| `cleanup_last_run` | Last Automata cleanup run: %1$s |
| `cleanup_never_run` | The Automata cleanup has not run yet. |
| `cleanup_counts` | Automata cleanup: removed %1$d, would remove %2$d, skipped %3$d, failed %4$d |
| `cleanup_bytes_freed` | Automata cleanup bytes freed: %1$s |
| `cleanup_split` | Automata cleanup removed by the owner: %1$d, by the safety net: %2$d |
| `cleanup_last_error` | Last Automata cleanup error: %1$s |
| `cleanup_error_none` | No Automata cleanup error. |
| `cleanup_disk_heading` | Automata disk use |
| `cleanup_disk_worktrees` | Automaton worktree disk %1$s: %2$s used |
| `cleanup_disk_temp` | Automata temp disk %1$s: %2$s used |
| `cleanup_disk_unreadable` | Automata disk use not available for %1$s |
| `cleanup_disk_none` | No Automata disk reading yet. |
| `cleanup_disk_threshold` | Automata disk alert threshold: %1$s |
| `cleanup_disk_alert_badge` | Automata disk alert |
| `cleanup_disk_ok_badge` | Automata disk ok |
| `cleanup_disk_alert_last` | Last Automata disk alert: %1$s |
| `cleanup_failed` | Could not load the Automata cleanup status: %1$s |

The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive through the localization pipeline; please send translations of these keys for DE, ES, FR and JA, and the PWA bundles will then mirror them in place of the placeholders. Please add the same keys, with the English values, to the app's own string resources.

## Other surfaces (for context, no app work)

The same read is available over MCP (`autonomous_cleanup_status`, read-only), the CLI (`datawatch autonomous cleanup`, with `--json` for the raw body) and the chat channels (`autonomous cleanup`, which also posts the disk alert text when the alert is active). The disk alert reads: "Automaton cleanup: the disk holding the Automaton worktree base `<path>` is at `<n>`% (threshold `<m>`%). Removing the Automaton worktrees and session temp files that live there would free space." (the temp disk reads "the disk holding the daemon session temp dir"; both are named when both are hot).

## Where to look in datawatch

- `internal/server/cleanup.go`: the route handler and the strict write checks for the four config keys.
- `internal/server/autonomous.go`: where `PUT /api/autonomous/config` applies those checks.
- `internal/autonomous/cleanup_status.go`: the status model, the disk readings and the rate-limited alert.
- `internal/config/config.go`: the four config keys, their defaults and the `Effective*` readers.
- `docs/api/openapi.yaml`: `/api/autonomous/cleanup` and the `CleanupStatus`, `CleanupPassStatus` and `CleanupDiskCheck` schemas.
- `internal/server/web/app.js`: `cleanupCardHtml` and `_automationCleanupLoader`.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.

## Acceptance (mobile)

- The app reads `GET /api/autonomous/cleanup` with the same auth as its other Automata reads and handles 401, 403, 405 and 503 without crashing; a 503 shows the failed state ("Could not load the Automata cleanup status: ..."), not an empty card.
- The Automation screen has an **Automata cleanup status** card with the **Automaton worktrees pass** and **Automata temp file sweep** sections, the mode, last run, counts, bytes freed, owner-versus-safety-net split and last error rows described above, and a read-only note. The card offers no remove, clean-up or run action.
- A pass with no `last_run` key (or a `null`) shows "The Automata cleanup has not run yet." and no counts; a pass in `report` mode shows `would_remove` and a `removed` of 0 without any error.
- The disk section shows the **Automata disk alert** badge while `disk_alert` is true and **Automata disk ok** otherwise; missing `worktree_disk` and `temp_disk` show "No Automata disk reading yet."; a reading without `percent` shows the not-available line instead of 0%; an absent (or `null`) `last_disk_alert_at` shows no last-alert line; an absent `last_error` shows "No Automata cleanup error.".
- On a phone-width screen the card fits without horizontal scrolling: long paths and error messages wrap, and the badge stays on one line.
- The four settings fields read and write `cleanup_mode`, `cleanup_retention_hours`, `cleanup_max_removals_per_pass` and `disk_alert_percent`; a key missing from the config read shows its default (report, 24, 10, 85); the app never sends a mode outside off, report and on (nor an empty mode), a retention or limit below 1, or a disk alert percent outside 1 to 100, because the daemon answers those with a 400.
- A failed or slow cleanup read does not crash the screen or show zeros: it shows the failed text above, or keeps the last good card marked as stale, and the app polls the status no faster than every 30 seconds.
- Every new string says Automaton or Automata (all 26 locale keys listed above do), and the 26 locale keys above exist in all of the app's languages (EN, DE, ES, FR, JA), with the English text as the placeholder in DE, ES, FR and JA until translations land.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/308 (datawatch-app#308).

The issue title is the heading of this file, and the issue body is the text above from "Parent release" through "Acceptance (mobile)". The `[sync]` title prefix follows the operator's written spec for this issue; the mobile-parity rule names `feat(area)` as the prefix, so a later parity issue should use that form.
