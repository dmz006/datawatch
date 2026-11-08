# Mobile parity issue draft — object tag filter allocation fix

- **Status:** draft only. No issue has been filed on any tracker.
- **Target repo:** dmz006/datawatch-app
- **Suggested title:** chore(parity): no mobile change needed for the object tag filter allocation fix — confirm tag filter behavior only

## Affected area

The Automata object tag filter in the server (`internal/autonomous/object_tag_filter.go`, with tests in `internal/autonomous/object_tag_filter_test.go`). This is the code that decides whether an Automaton, an Automation or a task matches a tag selection with the any-match or all-match rule. This work is part of the fix for the high-severity code scanning alerts that made the CodeQL alert gate fail.

## Current behavior

`MatchesObjectTags` sized its lookup set from the combined length of the system tags and the managed tag ids. Code scanning flagged that size sum as a possible allocation overflow. The matching result was never wrong. No request field, response field, route or screen is involved.

## Expected behavior

The lookup set is now created without a size hint, which removes the flagged sum. Matching is unchanged: an empty selection matches every object, any-match needs one requested tag, and all-match needs every requested tag. New tests in `object_tag_filter_test.go` cover these rules. The API contract is identical, so the mobile client needs no code change and no parity gap is left open or closed by this work.

## Suggested next step

Do not open a feature issue. If the mobile client filters Automata lists by tag on its own, check that its any-match and all-match behavior agrees with the rules above, and note the result on the next parity review. Otherwise close this draft as not applicable.
