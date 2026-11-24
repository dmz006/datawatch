# Finished work that is waiting to land is missing from the Android and iPhone apps

**Status:** draft, not filed. File it against datawatch-app when the datawatch release that carries it ships.

## Affected area

The daemon now keeps finished Automaton work from sitting unmerged: a guard on archive and cancel, a sweep that gives work older than a day an owner and an open question, a daily digest, and a weekly audit. The PWA carries the digest, the run buttons, the setting and the decision prompt.

- `GET /api/autonomous/finished-work` returns `{mode, generated_at, last_sweep?, last_digest?, last_audit?, audit_record_id?, waiting[]}`; each row is `{automaton, title, status, cause, action, branch, ahead, owner, since, question_id?, reask_at?}`. `cause` is one of `finished_not_merged`, `code_done_rest_pending`, `reask_due`, `branch_gone_unlanded`, `stuck_no_activity`. Needs `autonomous:read`.
- `POST /api/autonomous/finished-work/run` body `{run: "sweep"|"audit"}` runs one bounded pass or the audit; the reply is the pass or the audit result. Needs `autonomous:write`.
- `DELETE /api/autonomous/prds/{id}?decision=keep|drop&reason=...` and `POST /api/autonomous/prds/{id}/cancel` or `.../archive` with `{decision, reason}`: with the setting on, a cancel or archive of an Automaton whose branch holds commits not on the default branch is refused with HTTP 400 and a message that contains `keep or drop decision`; the same call with a decision succeeds. `keep` is asked again after 14 days.
- The questions the sweep raises are ordinary operator questions with `asked_by` `finished-work` and the options `land_by_hand`, `leave_alone`, `drop`; the apps' existing question screen already answers them.
- Setting `autonomous.finished_work` (`on` default, `shadow`, `off`).
- PWA: Automata, Housekeeping sub-tab, Housekeeping repairs card, section **Finished work waiting to land** with **Run sweep now** and **Run audit now**; Settings row for the setting; a prompt on a refused cancel or archive asks for keep or drop and a reason.
- Locale keys: `settings_finished_work`, `finished_work_title`, `finished_work_purpose`, `finished_work_none`, `finished_work_row`, `finished_work_question`, `finished_work_run_sweep`, `finished_work_run_audit`, `finished_work_run_failed`, `finished_work_run_done`, `finished_work_decision_prompt`, `finished_work_reason_prompt`, `finished_work_cause_*` in every file under `internal/server/web/locales`.

## Current behavior

The daemon and the PWA carry all of it. The apps cannot show the digest, run the sweep or audit, or answer a refused cancel or archive with a decision; they show the 400 text.

## Wanted

- A list of finished work waiting to land that follows `GET /api/autonomous/finished-work`, with owner and open question per row, and the two run actions.
- On a 400 whose text contains `keep or drop decision`, a prompt for keep or drop and a required reason, then the same call with the decision.
- The same five locales.

## Parity surface

REST, MCP, CLI, comm channel, YAML/config and PWA are done. Android and iPhone are this draft.
