# [sync] search before create: overlap warnings, overlap read route and two settings

## Body sent with the issue

The datawatch daemon now checks for overlapping work when an Automaton is created. This adds one field to a create response, one new read route, two config keys, and a read-only warnings block in the PWA. The app needs matching behavior.

## Contract changes

### 1. Create response: `overlap_warnings`

`POST /api/autonomous/prds` keeps its existing request and response. When the config key `autonomous.automation.create_search_mode` is `on`, the response object gains one top-level field:

```json
{
  "id": "…",
  "…": "all existing Automaton fields, unchanged",
  "overlap_warnings": [
    {
      "kind": "automaton",
      "id": "ab12cd34",
      "status": "running",
      "score": 0.72,
      "shared_terms": ["search", "create", "overlap"],
      "reason": "One sentence of operator text.",
      "read_hint": "How to read the underlying item."
    }
  ]
}
```

- `kind` is one of `automaton`, `registry`, `branch`, `unavailable`.
- `id` and `status` are set only for `automaton`. `row` (integer) is set only for `registry`. `branch` (string) is set only for `branch`.
- `score` is a number rounded to two decimals. `shared_terms` has at most five strings and may be absent.
- `reason` is always present. `read_hint` may be absent.
- The array is present but empty when the check ran and found nothing.
- In `off` and `shadow` modes the field is **absent**. The create itself always succeeds; the warnings never block it and never change the stored Automaton.

### 2. New read route: `GET /api/autonomous/overlap`

Runs the same check without creating anything. It writes nothing and works in every mode.

Query parameters:

| Parameter | Required | Meaning |
|---|---|---|
| `title` | one of `title` or `spec` | Title of the work about to be created |
| `spec` | one of `title` or `spec` | Spec of the work about to be created |
| `limit` | no | Most warnings returned, whole number 1 to 50, default 10 |

Response `200`:

```json
{
  "warnings": [ { "kind": "automaton", "id": "…", "status": "…", "score": 0.72, "reason": "…" } ],
  "threshold": 0.5,
  "mode": "shadow"
}
```

- `warnings` uses the same item shape as the create response, best match first, at most `limit` items.
- `threshold` is the match threshold in force. `mode` is `off`, `shadow` or `on`.
- `400` when both `title` and `spec` are blank or `limit` is not a whole number from 1 to 50. `500` when the search fails. `503` when autonomous is disabled. A federated peer needs the autonomous read capability.

### 3. Config keys

Both are read and written through the existing `GET` and `PUT /api/config`.

| Key | Type | Default | Rule |
|---|---|---|---|
| `autonomous.automation.create_search_mode` | string | `shadow` | `off`, `shadow` or `on`. Any other value is a `400`. |
| `autonomous.automation.create_search_threshold` | number | `0.5` | Above 0 and at most 1. Any other value is a `400`. |

`GET /api/config` shows the effective value when the key is unset.

## English text

Warnings block (PWA locale keys, English):

| Key | Text |
|---|---|
| `overlap_warnings_title` | Possible overlap with existing work |
| `overlap_warning_automaton` | Automaton |
| `overlap_warning_registry` | Registry row |
| `overlap_warning_branch` | Unmerged branch |
| `overlap_warning_read_hint` | Read this item before you start new work. Nothing here was changed. |

Settings (PWA locale keys, English):

| Key | Text |
|---|---|
| `settings_automation_create_search_mode` | Automaton create search (off, shadow records overlaps without showing them, on shows them, default shadow) |
| `settings_automation_create_search_threshold` | Automaton create search threshold (similarity above 0 and at most 1, default 0.5) |

## PWA behavior to match

After a successful create, when `overlap_warnings` is a non-empty array, the PWA shows a dismissible block titled with `overlap_warnings_title`. Each warning of kind `automaton`, `registry` or `branch` is one line: the kind label, then the item (Automaton id with its status in brackets, the registry row number, or the branch name), then the score to two decimals, then the read hint text. Warnings of any other kind, including `unavailable`, are skipped. The block has one control, which closes it. Nothing in it changes state.

The two config keys appear in Settings under the Automaton group: `create_search_mode` as a select with `off`, `shadow`, `on` (default `shadow`), and `create_search_threshold` as a number field from 0.05 to 1 in steps of 0.05 (default 0.5).

## Acceptance criteria for mobile clients

- [ ] After a create, show `overlap_warnings` read-only. Each line shows the kind label, the item, and the score. The block can be dismissed and offers no action that changes state.
- [ ] Ignore the field when it is absent, and show no block when the array is empty. A create without the field behaves exactly as before.
- [ ] Skip warnings with an unknown kind, including `unavailable`, without an error.
- [ ] Offer `GET /api/autonomous/overlap` in mobile search: send `title` and/or `spec` (and optionally `limit`), show the returned warnings with the same read-only lines, and show `mode` and `threshold` where useful. Handle `400`, `500` and `503` with a plain message.
- [ ] Show `autonomous.automation.create_search_mode` and `autonomous.automation.create_search_threshold` in settings with the English text above, read and written through `/api/config`, with the defaults and ranges above. Show the daemon's `400` message when a value is refused.
- [ ] Add the seven English strings above to the app's string resources; other locales can follow the PWA translations.

## Filing record

Issue: https://github.com/dmz006/datawatch-app/issues/305

Issue number: 305

Title as filed: [sync] search before create: overlap warnings, overlap read route and two settings

Filed 2026-10-30 with `gh issue create -R dmz006/datawatch-app`. The body above is exactly the body filed.
