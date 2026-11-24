# [sync] Finished, not merged: the "Branch not merged" badge on Automaton views

Draft only. No issue has been filed on any tracker.

## Affected area

The Automaton list, the Automaton detail, the project cards and the project timeline in the datawatch-app mobile clients (Android and iOS). The daemon returns the field on every surface; the PWA shows the badge.

## What the daemon does

Every read of an Automaton derives a read-only `merge_state` field. It is never stored and it never hides a record. The only value is `finished_not_merged`: the record is `completed` or `archived`, it has a work branch, and that branch has commits the base branch does not have (a rebased or squashed landing counts as landed). An active record, a record with no branch, a merged, empty or dropped branch, and a comparison that cannot be made all leave the field absent. The git comparison is cached for one minute per record.

Where the field is returned: `GET /api/autonomous/prds` (each row), `GET /api/autonomous/prds/{id}`, the project roll-up (`GET /api/projects/{name}/rollup`, one `merge_state` per Automaton line) and the project timeline Automaton rows. The archive guard works from the same fact: the supervisor loop does not archive a finished Automaton while its work is unmerged, and the operator's own archive is always honored.

## What the PWA does

- A small amber badge "Branch not merged" (locale key `prd_merge_state_badge`) on the Automaton card in the list and in the detail header, on the project cards and on the Gantt row, shown only when `merge_state` is `finished_not_merged`. Nothing is shown for an absent field.
- Next to the badge a small help link "?" (locale key `merge_state_help_link` for its title) that opens the definition *Automaton merge state*. It is hidden when the operator turned the documentation links off.
- The badge tooltip says the branch work is not merged into the base branch.

Locale keys in all five locales (en, de, es, fr, ja): `prd_merge_state_badge`, `merge_state_help_link`.

## Expected behavior in the apps

1. Show the same badge on the list card, the detail header, the project cards and the timeline row when `merge_state` is `finished_not_merged`; render nothing for an absent or any other value (a future value must not show as unmerged).
2. Treat the field as read-only. Do not send it back in any write.
3. Offer the Re-review action next to it (see the re-review parity draft).
4. Reuse the two locale strings.

## Compatibility

No migration is needed. Older apps ignore the field. The project roll-up reflects a hand merge within about two minutes (one minute for the roll-up body plus one for the per-record comparison), not only on the next Automaton write.

## Tests

Daemon side: the merge state view tests, the roll-up and timeline tests, and the roll-up refresh test that lands a branch by hand and reads the line again. PWA side: node tests for the badge and the locale keys.
