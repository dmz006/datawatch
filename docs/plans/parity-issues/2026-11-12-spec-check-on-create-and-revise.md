# [sync] spec check: a spec_check array on the Automaton create and revise replies

## Body sent with the issue

The datawatch daemon now lints an Automaton's spec text when the Automaton is created and when a revision is requested. The findings come back on the reply. The check never blocks the create. The app needs to show the findings.

## Contract change

`POST /api/autonomous/prds` and `POST /api/autonomous/prds/{id}/request_revision` keep their requests. Each reply gains one top-level field, always present:

```json
{
  "id": "…",
  "…": "all existing Automaton fields, unchanged",
  "spec_check": [
    {
      "rule": "names_tracker_id",
      "phrase": "BL123",
      "severity": "warning",
      "correctable": true,
      "suggestion": "remove 'BL123' and describe the work itself instead of naming a tracking item by id"
    }
  ]
}
```

- `rule` is one of `release-version-fixed`, `names_tracker_id`, `system-binary-path`, `private-address`, `worktree-path`.
- `phrase` is the exact text found; `suggestion` is the rewording.
- `severity` is `warning` (the daemon can fix the wording itself; `correctable` is true) or `error` (the operator must reword it).
- The array is empty when the spec is clean.

## App behavior

After the create or the revise call returns, show a warning block listing each finding: the severity, the phrase, the rule and the suggestion, and for a correctable finding the note that the daemon can fix the wording itself. Show nothing for an empty array. The block never blocks navigation.

## Strings (PWA locale keys, EN values)

- `spec_check_title`: "Spec check: wording the planner may copy into task titles"
- `spec_check_severity_warning`: "warning"
- `spec_check_severity_error`: "error"
- `spec_check_fixable`: "The daemon can fix this wording itself."
- `spec_check_suggestion`: "Suggested rewording"

The de, es, fr and ja values are in the daemon's locale files.
