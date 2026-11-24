# [sync] Judge timeout hold replay, not judged review state and a time limit that grows with the change

Draft only. No issue has been filed on any tracker.

## Affected area

Every client that shows an Automaton's review state, edits the autonomous settings or offers the Retry and Reroute actions: the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel.

## What changed in the daemon

- **Replay of a judge timeout hold.** An Automaton in `needs_review` whose newest block is a judge timeout hold (reason `judge_timeout_hold`) and whose tasks are all completed or cancelled now accepts a rejudge (`POST /api/autonomous/prds/{id}/rejudge`, `autonomous_prd_rejudge`) and a run (`POST /api/autonomous/prds/{id}/run`, `prd-run`). It needs no new commit; the review runs fresh on the same head and is never served from the verdict cache. Another timeout keeps the Automaton held. The PWA Retry and Reroute buttons on the timed out badge therefore succeed instead of being refused.
- **Honest review state.** `review_state` gains `judged` (false for a timed out or pending review, true for blocked and passed) and `backends` (each backend tried with `timed_out`, `no_slot` or `could_not_start`). The `cause` of a timed out review starts "not judged:". The comm line reads `review timed out, not judged: <cause> -> <action>`.
- **A timed out judge keeps its evidence.** The attempt records `session_id` and `last_output` (the last 4 KB); the held verdict records `judge_session_id` and `raw_judge`.
- **Time limit.** The limit counts from the moment the judge gets its slot; the wait for a slot is recorded separately and never counted, and a judge that runs past the limit is ended at it.
- **New setting** `autonomous.judge_timeout_per_thousand_lines_seconds` (whole number 0 to 3600, default 600): extra limit per 1000 changed lines above `autonomous.judge_size_threshold_lines`, at most four times the base limit; 0 adds nothing. A large rules review is split by file group and each group gets its own scaled limit. Accepted in YAML, `PUT /api/config`, `datawatch config set`, comm `configure`, MCP `autonomous_config_set` (`judge_timeout_per_thousand_lines_seconds`).

## What the PWA already does

- Settings, autonomous: the number field "Judge extra time limit per 1000 changed lines (sec, 0=none)" (minimum 0, step 1, placeholder 600) with a help link to the judge policy how-to.
- New locale keys in en, de, es, fr and ja: `settings_judge_timeout_per_thousand` and `settings_judge_timeout_per_thousand_help`.

## Expected behavior in the apps

1. Settings: add the same field to the autonomous section; reject negative, non-whole and above-3600 values before sending and show the daemon's error text.
2. Review state: if the app renders `review_state`, show "not judged" for `timed_out` (use `judged` and `backends`, do not infer it from the cause text).
3. Retry and Reroute on a timed out review now succeed on the same head; do not hide them for a timeout hold.

## Compatibility

New fields are additive; an older app ignores `judged` and `backends`. An older daemon does not accept the new setting.
