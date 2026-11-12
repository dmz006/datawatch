# [sync] Identity backups, empty-write refusal and restore

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

<!-- body-start -->
## Daemon version

Lands in the next patch release above the one on main when the change merges. The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

The saved operator identity can no longer be lost silently. The daemon keeps a timestamped backup before every identity write, keeps the newest few (`identity.backups_to_keep`), and refuses a write that would replace saved fields with an empty identity unless it is the explicit reset. There is a new way to bring a backup back (Restore) and a new explicit Reset. The PWA's old **Reset** button, which only reloaded the form from the daemon, is now called **Revert**. The REST contract changed in five places: a `409` refusal on the existing write routes, `GET /api/identity/backups`, `POST /api/identity/restore`, `POST /api/identity/reset`, and one new config key. The apps need to match the PWA.

## REST contract

### Refusal: HTTP 409 on `PUT`, `PATCH` and `POST /api/identity`

A write whose fields are all empty, sent over an identity that has saved content, is refused and nothing is written:

```json
{ "error": "refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset",
  "code": "identity_wipe_refused",
  "fields_kept": ["role", "current_focus"] }
```

`fields_kept` names the saved fields that have content (`role`, `north_star_goals`, `current_projects`, `values`, `current_focus`, `context_notes`). Show the `error` sentence as written and keep what the person typed; do not retry on their behalf. Tell this 409 apart from any other by `code`.

### List: `GET /api/identity/backups`

Reply `200`, newest first:

```json
{ "backups": [ { "name": "identity-20261030T084512.381Z.yaml", "created_at": "2026-10-30T08:45:12.381Z", "size_bytes": 412, "role": "Platform engineer", "field_count": 5 } ],
  "keep": 10 }
```

`backups` is an empty array when there are none. `keep` is the effective retention limit. `name` is the value to send to restore.

### Restore: `POST /api/identity/restore`

Request: `{ "backup": "<name from the list, or \"latest\">" }`. The current identity is backed up first, so a restore can itself be undone.

| Status | Meaning |
|---|---|
| 200 | The restored identity (same shape as `GET /api/identity`). |
| 400 | Bad JSON, or `backup` missing or empty. Plain-text body. |
| 404 | Unknown or invalid backup name. Body `{"error": "…"}`. |
| 422 | The backup holds no operator content, so restoring it would empty the identity. Body `{"error": "…"}`. |

### Reset: `POST /api/identity/reset`

No body. Clears the identity on purpose, keeping a backup of what it replaced, and answers `200` with the now-empty identity. This is the only route that clears a saved identity.

### Config key

`identity.backups_to_keep`: a whole number, default 10, minimum 1. Read and written through `GET`/`PUT /api/config`. A bad value (zero, negative, not a whole number) rejects the whole `PUT` with nothing changed; a good one applies at once with no restart.

Locale keys the PWA added (English values in `internal/server/web/locales/en.json` of the datawatch repository; the other four locales carry them too): `identity_reset_confirm`, `identity_reset_done_toast`, `identity_restore_title`, `identity_restore_none`, `identity_restore_confirm`, `identity_restore_done_toast`, `identity_restore_row_fields`, `identity_backups_keep_saved_toast`, `identity_backups_keep_invalid`, `settings_identity_backups_to_keep`.

## What the mobile identity screen should show

- **Restore choice.** A **Restore** action opens a list of the backups from `GET /api/identity/backups`, newest first. Each row shows the time (`created_at`, in the person's local time zone), the role, and the field count ("5 fields filled in"). With no backups it says "No identity backups yet." Choosing a row asks for confirmation ("Restore the identity saved on <time>? The current identity is backed up first, so this can be undone."), then calls restore and reloads the form from the reply. A `404` or `422` shows the daemon's sentence.
- **Explicit Reset.** A **Reset** button that clears the identity on purpose. It asks first and the confirmation says a backup is kept: "Clear the saved identity? A backup is kept, and Restore can bring it back." It then calls `POST /api/identity/reset` and shows "Identity cleared; a backup was kept".
- **Revert.** The old PWA Reset, which only reloaded the form from the daemon and threw away unsaved edits, is now called **Revert**. If the app has an equivalent, give it that name so the two words are not confused.
- **A refused save keeps the typed text.** When a save comes back `409` with `code: identity_wipe_refused`, show the daemon's `error` sentence, keep every field as typed, and do not clear the form.
- **Backups-to-keep field.** A number field "Identity backups to keep (whole number, minimum 1, default 10)" with a help link, saved through `/api/config`. A value under 1 or not whole is refused in the field with "Use a whole number of 1 or more", and a saved value says "Backup limit saved".

## Acceptance criteria

### Android

- [ ] Restore lists the backups newest first with time, role and field count, and shows the empty state when there are none.
- [ ] Restore asks for confirmation naming the backup time, then reloads the form from the `200` reply; `404` and `422` show the daemon's sentence.
- [ ] Reset asks for confirmation that says a backup is kept, calls `POST /api/identity/reset`, and confirms with the toast text.
- [ ] The form's reload-from-daemon action is named Revert, not Reset.
- [ ] A `409` with `code: identity_wipe_refused` shows the daemon's sentence and keeps the typed text.
- [ ] The backups-to-keep field reads and writes `identity.backups_to_keep` through `/api/config`, refuses a value under 1, and has its help link.
- [ ] The new locale keys exist in all five locales.
- [ ] Layout holds at 360 dp width with a long role name in a restore row.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the restore rows, the confirmation text and the refusal sentence readable without horizontal scroll.
- [ ] VoiceOver reads each restore row with its time, role and field count, and reads Reset and Revert as distinct buttons.
<!-- body-end -->

## Parity surface

Every surface the change could reach, and whether it changes. Where it does not change, the reason is given.

| Surface | Changes? | Detail or reason |
|---|---|---|
| REST | Yes | A `409` (`code: identity_wipe_refused`, `fields_kept`) on `PUT`, `PATCH` and `POST /api/identity` for an empty write over saved content, `GET /api/identity/backups`, `POST /api/identity/restore` (200/400/404/422) and `POST /api/identity/reset`. |
| MCP | Yes | New tools `identity_backups`, `restore_identity` and `reset_identity`; `set_identity` reports the refusal and names `reset_identity`. |
| CLI | Yes | `datawatch identity backups [--json]`, `datawatch identity restore [--backup <name>]` and `datawatch identity reset`; a refused write prints the daemon's sentence. |
| Comm channel | Yes | The verbs `identity backups` and `identity restore <name\|latest>`. There is no reset verb, by design: a clear never happens from chat, and the verb points at the CLI, the PWA or the MCP tool. |
| YAML/config | Yes | One key, `identity.backups_to_keep` (default 10, minimum 1), read and written through `/api/config`, MCP `config_set`, `datawatch config set` and YAML. |
| PWA | Yes | The identity section gains the Restore choice, the explicit Reset with its confirmation, the renamed Revert, the refused-save message that keeps the typed text, and the backups-to-keep field with its help link. |
| Android | Yes, not built | Needs the identity screen changes above. Tracked in the issue filed at the merge. |
| iPhone | Yes, not built | The same as Android. Tracked in the issue filed at the merge. |

## Filed issue

The supervisor files this issue at the merge; it is not filed from the branch. The issue body is the text between the two body markers above.

- Issue: not yet filed.
