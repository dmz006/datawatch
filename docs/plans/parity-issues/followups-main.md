# Parity: operator retry, existing-task seeding and raw-key guard (datawatch-app)

**Status:** draft (file only; no issue filed). Drafted 2026-11-24 against branch `prep/followups-main`
(on main 9.184.0). Related: `docs/plans/parity-issues/attempt-cap.md`, `docs/plans/parity-issues/pause-new-work.md`.

## Affected area

1. **Operator retry starts at once.** When the operator retries or resets a task (review retry, plain reset,
   force reset), the attempt back-off that was owed is dropped; the retry still counts toward the cap.
   Daemon-side; the task views already carry `attempt_not_before`, which now clears on an operator retry.
   The app needs no change except that a task's "next attempt not before" line disappears after the operator
   retries it.
2. **Existing tasks are counted from their history, and tasks over the cap are held with one question per
   Automaton.** Once, at the first start after upgrade, every task's `attempts_total` becomes its recorded
   attempt count and unfinished tasks over the cap become `review_needed` with `attempt_cap_held: true`.
   The operator sees one alert, "Automaton tasks held at the attempt cap", and ONE question per Automaton
   (asked by `attempt-cap-seed`, context `prd`, no task id) with the options `force_reset` ("Clear the counts
   and run them again", operator only) and `keep_held` (the default). The app must render that question like
   any operator question (it has no task id) and must not assume a cap question always names a task.
3. **Raw locale keys.** The PWA drew some strings before its locale bundle had loaded. No API or locale key
   changed. For the app: if it draws any text from a bundle that loads asynchronously, make sure a missing
   entry falls back to the English text and the screen is redrawn when the bundle is ready.

## Current behavior

The daemon and the PWA carry items 1 to 3. The Android and iPhone apps show tasks and questions from the
same endpoints and need no new field.

## Expected behavior

- A task the operator retried shows no wait, however long the earlier back-off was.
- The grouped question appears in the question list of the Automaton and answers through the existing
  question answer call; a `force_reset` answer from the operator clears every held task of the Automaton.
- No screen shows a raw key such as `pause_badge_paused`.

## Surfaces

| Surface | Change |
|---|---|
| REST / MCP / CLI / comm | none new; `reset_task`, the question list and the answer call are unchanged |
| PWA | `tOr` and `onI18nReady` guards; header pause badge, alert pill, error toast; `navigate()` waits for the bundle |
| Android / iPhone | item 2: render a question with no task id; item 3: check bundle fallback |
