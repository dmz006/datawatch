# The attempt cap settings, task count, hold and force reset are missing from the Android and iPhone apps

**Status:** draft (file only; no issue filed). Drafted 2026-11-23 against the datawatch tree on branch
`prep/attempts`. Operator checklist: `docs/howto/attempt-cap.md`.

## Affected area

The daemon now counts every attempt a task is started for, in all, across cancels, resets, review
retries and daemon restarts, makes later attempts wait, and holds the task for the operator at a cap.
Only the operator's force reset clears the count.

- Settings under `autonomous.attempt_cap`:
  - `autonomous.attempt_cap.enabled` — switch (unset reads on).
  - `autonomous.attempt_cap.max_total` — whole number 1 to 1000 (unset reads 6).
  - `autonomous.attempt_cap.free_attempts` — whole number 1 to 1000 (unset reads 3).
  - `autonomous.attempt_cap.backoff_minutes` — list of whole minutes 0 to 10080 (unset reads 5, 15, 45;
    an empty list means no wait; `null` clears it to the default).
- Project policy field `attempt_cap` (whole number 1 or more), in the project policy editor and in
  `GET /api/projects/{name}/policy` (effective value with its source).
- Task fields on every task view (`GET /api/autonomous/prds/{id}`, `.../tasks`, and the review list):
  `attempts_total`, `attempt_cap_held`, `attempt_not_before`.
- The operator's force reset: `POST /api/autonomous/prds/{id}/reset_task` with `force: true` and
  `actor: operator`. It clears the count and the hold; a reset made as anything else keeps the count.
- Alert "Automaton task reached its attempt cap" and the operator question with the options
  `force_reset` and `cancel` (asked by `attempt-cap-hold`).
- Where they live in the PWA: the **Attempt cap** group in the Automata settings list and the
  `attempt_cap` row of `PROJECT_POLICY_GROUPS` in `internal/server/web/app.js`; the task badges
  (`window.attemptCapTaskBadgesHtml`) and the held task's **Force reset** button (`prdForceResetTask`).
- Locale keys, in every file under `internal/server/web/locales` (English placeholders in de, es, fr and
  ja until the mobile translations land): `attempt_cap_count`, `attempt_cap_count_of`,
  `attempt_cap_held_label`, `attempt_cap_next`, `attempt_cap_force_reset`,
  `attempt_cap_force_reset_title`, `attempt_cap_force_reset_confirm`, `attempt_cap_force_reset_ok`,
  `attempt_cap_force_reset_fail`, `settings_attempt_cap_group`, `settings_attempt_cap_enabled(_hint)`,
  `settings_attempt_cap_max_total(_hint)`, `settings_attempt_cap_free_attempts(_hint)`,
  `settings_attempt_cap_backoff_minutes(_hint)` and `project_policy_attempt_cap`.

## Current behavior

The daemon and the PWA carry all of it. The Android and iPhone apps show no attempt count on a task, do
not say that a task is held at the cap or waiting for its next attempt, have no Attempt cap group in
Settings, and have no project policy field for the cap. A task held at the cap cannot be released from a
phone: a plain retry is refused with a message naming the cap, and the apps have no force reset.

## Expected behavior

Capability parity with the PWA; each app uses its own idiomatic controls.

- A task row and the task detail show "Attempts started: n" (or "n of cap" when the cap is known), a
  "Held at the attempt cap" badge while `attempt_cap_held` is true, and "Next attempt not before <time>"
  while `attempt_not_before` is in the future.
- A task held at the cap offers **Force reset** after a confirmation. It sends
  `POST /api/autonomous/prds/{id}/reset_task` with `{"task_id": ..., "actor": "operator", "force": true}`
  and shows the daemon's text on a refusal. The ordinary Retry action stays as it is and shows the
  daemon's refusal for a task held at the cap.
- The cap question appears in the apps' questions list like any other and its answers
  (`force_reset`, `cancel`) are sent unchanged.
- Settings shows an Attempt cap group with the four settings, labels and help text from the
  `settings_attempt_cap_*` keys; a save sends the dotted key to `PUT /api/config` and shows the daemon's
  400 text unchanged. The backoff list is a comma list of whole minutes.
- The project policy editor shows **Attempt cap** (whole number 1 or more) with the value and source from
  the effective policy.

## Acceptance

- [ ] A task shows its attempt count, the hold badge and the wait, in every shipped language.
- [ ] A task held at the cap offers Force reset, after a confirmation, and the task runs again with its
      count at zero.
- [ ] A Retry of a task held at the cap shows the daemon's refusal and changes nothing.
- [ ] The Attempt cap settings group reads and writes the four keys through `GET` and `PUT /api/config`;
      an unset value shows its default; an empty backoff list is kept as empty.
- [ ] A refused value shows the daemon's 400 text unchanged and leaves the stored value as it was.
- [ ] A save of one setting does not change the others.
- [ ] The project policy editor carries `attempt_cap` with its source.
- [ ] The cap alert and question are shown once per hold.
- [ ] Android and iPhone show the same labels, defaults and ranges as the PWA.
