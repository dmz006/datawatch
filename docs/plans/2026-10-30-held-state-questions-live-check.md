# 2026-10-30 — Held-state questions live check (sandbox daemon `hsq-live-20261101`)

Live-check of the howto `docs/howto/automaton-held-state-questions.md`, run on a **sandbox
daemon** — never the production daemon.

## Sandbox setup

- Built fresh (per task): `env -u TMUX -u TMUX_TMPDIR go build -o datawatch ./cmd/datawatch/`
  → v9.42.1, 146.3 MB.
- Sandbox daemon **`hsq-live-20261101`**: `./datawatch serve` on loopback port **38655**,
  data dir `/tmp/hsq-live-20261101/data/`, dedicated `HOME=/tmp/hsq-live-20261101/home`
  (claude creds copied in), `TMUX` unset, own token. PID **431140**. No `td_install_traps`
  in the tool shell; stopped by exact PID with SIGTERM.
- Five fixtures created via the daemon API (PRDs + one automation `96622465`
  "livecheck-pipeline"), each landing one of the five held states:
  - `ff7b0001` — end-to-end control PRD (decompose → approve → run → **complete**).
  - `438b0001` — **revisions_asked** (via `POST .../request_revision`).
  - `55970001` — **review_held / blocked** (worker failed verification → failure-escalation hold).
  - `44370001` — **awaiting_decision** (decision-gate hold on task `0b7f0002`).
  - `44720001` — **pending_insert** (agent actor + `agent_insert_policy=block` → held pending
    operator approval, pending insert `234ba23f`).
- Held-mode switched with `PUT /api/config` `{"autonomous.questions.held_mode":"on"}` → `{"status":"ok"}`.

## Results (one row per check)

| # | Check | Result | Observed |
|---|-------|--------|----------|
| 1 | Shadow baseline: held states land, **no questions raised**, notes recorded | **PASS** | `GET /api/autonomous/held-questions` in `shadow` mode returned `open: []` for all 5 held states; held-state TriageNotes/decision entries present (e.g. `operator_decision` "Automaton … is held because a revision of its spec was requested", `question_id` recorded for the shadow note). |
| 2 | Exact question count at first tick in on mode (one per item, no dupes at any later tick) | **PASS** | At 03:51:28, exactly **5 open** questions — one per fixture — with the correct dedupe identity (states `blocked`, `review_held`, `revision_requested`, `awaiting_decision`, `pending_insert`); state did not spawn more than one per item across subsequent ticks. |
| 3 | Question content: right link targets, options, recommendation, rationale, default hold | **PASS** | Each question carries the entity ids as link targets (e.g. `prd_id 438b0001`, `automation_id 96622465`, `task_id 0b7f0002`), `session_id: "held"`, `source: "held"`, `source_line_sha256` dedupe key; 3–5 options (review: `retry, retry_stronger, change_spec, cancel, hold`; revision: `change_spec, resume, hold`; decision: `resume, cancel, hold`); `recommendation.rationale` present (e.g. "Content failures point at the spec, so fixing the spec before any retry is the right first move"); `default: "hold"` on every state; `label: "decision"` / risk normal. No raw URL in the stored text — the PWA resolves the ids into the linked screen. |
| 4 | Daemon API `GET /api/autonomous/held-questions` lists them | **PASS** | Returned `mode`, `states` (all 5), and the 5 open questions with `question_id`, `state`, `prd_id`/`task_id`/`automation_id`, `recommended_option`, `default`, `created_at`; also exposes `shadow` notes section. |
| 5 | CLI `datawatch question held` (same list; held-mode shown) | **PASS** | `HOME=<sandbox> DATAWATCH_TOKEN=<token> env -u TMUX ./datawatch question held -u http://127.0.0.1:38655` listed the 5 questions with their recommended options and mode `on`. |
| 6 | State still held while question is open; answering the recommended option makes the daemon run the action | **FAIL** (routing path) | Answer for the review-held question (recommended `change_spec`) was **accepted and recorded** (HTTP 200/201, `operator_answer` decision entries for `a-18e38e070b88663ab38d`, `a-18e38e46e66e65600479`, `a-18e38e72c8c838a53d08` incl. a `supersedes` answer) but `routeHeldAnswer` **never ran** in 3 attempts; the state stayed held and the question was re-raised at the next pass. **Root cause**: the submit-time question-change hook (`SetChangeHook` at `cmd/datawatch/main.go:6656` → `Manager.RecordQuestionChange`) writes an `operator_answer` decision entry for the question id when the answer is *submitted*; the question-tick router's idempotence probe `heldAnswerAlreadyRecorded` (`internal/autonomous/held_question_answer.go:343`) then finds that same entry and refuses to route. Unit tests pass because they never wire the change hook (no submit-time entry exists in the tests). **The underlying action is sound**: the same manager action invoked directly — `POST /api/autonomous/prds/55970001/resolve_task_review` with the changed spec — returned HTTP 200, replaced the spec, re-ran the worker, and `RESULT.txt` came back exactly `A`/`B` (verification proceeding). So the operator-answered *action* works; only the question→action routing in this build (v9.42.1) is broken. |
| 7 | Stale/withdrawn questions can't be re-answered; refusal names why | **PASS** | Answering the withdrawn pending-insert question: `HTTP 409 "question is not open: the question is withdrawn and accepts no answer"`. Re-answering an already-answered question without `supersedes`: `HTTP 409 "answer must name the answer it supersedes: the question is answered by "a-18e38e72c8c838a53d08", so the answer must name that id as supersedes"`. |
| 8 | Clearing the state another way withdraws the question automatically | **PASS** | Operator `approve_insert` (member `234ba23f`, actor `operator`) cleared the pending-insert hold; at the next held pass the question `q-18e38de457025afed80b` was withdrawn — decision entry `{"kind":"operator_decision","actor":"daemon","note":"held state pending_insert for 234ba23f cleared","question_id":"q-18e38de457025afed80b","decision_state":"withdrawn"}`; question dropped from the open list. Same behavior observed for the blocked-state question after the review was resolved directly (03:59:28, "held state blocked for 55970001 cleared"). |
| 9 | Alerts still fire in on mode | **PASS** | Held-state alerts observed: `error "Automaton task needs review"` (PRD-C task hold, 03:45:34) and `warn "Automaton task waiting for a decision"` (PRD-D decision gate, 03:44:10); alerting kept firing in the on-mode window (session lifecycle alerts 03:59:19, 04:00:42). 30 alerts in the sandbox store total. |
| 10 | Cleanup: everything created is removed; daemon stopped by PID | **PASS** | All 5 PRDs `cancelled` (HTTP 200 each), automation `96622465` activate→cancel (final status `cancelled`), daemon PID 431140 SIGTERM → terminated, `/tmp/hsq-live-20261101` removed (workdirs included), build binaries removed, `git status` clean. |

## Notes / discrepancies found

1. **Check 6 FAIL is a production-code bug in v9.42.1**, not a howto inaccuracy: the howto's
   promise ("answering the recommended option makes the daemon run the action") does not hold
   when the submit-time question-change hook is wired (as in a real daemon). The fix is to make
   the tick router's idempotence probe distinguish the submit-time record (write `answer_id` =
   the *accepted* answer's id at routing time, or mark the submit-time record with a distinct
   kind) or to skip probe entries that carry the same `question_id + answer_id` pair as the
   pending answer. The manager action itself and the direct operator API are correct.
2. `plan_coverage_check=block` pinned onto a PRD at decompose time blocked `approve` from
   `revisions_asked`; `plan_coverage/acknowledge` is only allowed from `needs_review` — a
   chicken-and-egg for a fixture PRD (worked around by using PRD-E's pending-insert state for
   the withdrawal check instead).
3. `approve_insert` requires a real operator actor (`operator`), not an arbitrary
   `operator-*` string; the pending insert id is the *member* id (`234ba23f`), not the PRD id.
4. Draft automations can't be cancelled — activate then cancel.

## Addendum (fix of the check 6 FAIL)

The check 6 routing bug was fixed in this branch: `heldAnswerAlreadyRecorded`
(`internal/autonomous/held_question_answer.go`) now matches only the routed
record — the entry the answer router writes, which always carries a
`Result` — and deliberately does not match the submit-time `operator_answer`
entry the question-change hook records on the PRD's decisions (which carries
no `Result`). The regression test
`TestHeldQuestionAnswerRoutedDespiteSubmitHook`
(`internal/autonomous/held_question_answer_test.go`) wires the daemon's
change hook (`QuestionStore.SetChangeHook` → `Manager.RecordQuestionChange`)
exactly as `cmd/datawatch/main.go` does and proves the action runs exactly once even
with the submit-time entry present; idempotence on re-routing still holds
via the routed record. `go test ./internal/autonomous/ -count=1` passes.

A live re-run of this check's answer step (the single FAIL above) on a
rebuilt sandbox daemon is still pending; until it passes, the held-question
rows in `docs/testing-tracker.md` keep **Validated = No** for the
answer-routing path.

## Cleanup confirmation

- Daemon `hsq-live-20261101` (PID 431140) stopped by SIGTERM; port 38655 free.
- Fixtures removed via API first (5 PRDs cancelled, automation cancelled), then the sandbox
  data dir, temp workdirs, HOME copy, and both build binaries deleted; `git status` clean.
- No tracked files modified other than this report. No production daemon was touched.
