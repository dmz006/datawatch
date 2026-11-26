# Mobile parity draft: isolated merge runs (datawatch-app)

Draft only; no issue filed.

## What changed in datawatch
- Every daemon merge run is isolated. A run record gains `automaton_id`, `shadow` and `push_plan`
  (`ref`, `old`, `new`, `commits`), a step `update-local-main`, and the validator check
  `one_step_above_base`.
- A merge pipeline definition gains `tag_sizes` on a `tag_release` step and a top-level
  `conflict_rules` list (`path`, `rule`: `keep_main`, `keep_both` or `ask`).
- No new endpoint, setting, MCP tool, CLI command or chat verb.

## Wanted in the mobile client
- Show the new step and check names in the merge run steps; they are plain strings today, so
  nothing breaks without a change.
- A pipeline editor, if the app has one, must keep unknown fields of the definition when it saves
  (it must not drop `conflict_rules` or `tag_sizes`).
- Show `push_plan` and a shadow marker once a surface starts shadow runs.
