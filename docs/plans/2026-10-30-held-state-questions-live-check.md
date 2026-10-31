# Live check — automaton held-state questions (docs/howto/automaton-held-state-questions.md)

**Date:** 2026-10-31 (checked against the howto dated 2026-10-30)
**Tree:** `/home/dmz/.datawatch/prd-worktrees/56d30001`
**Binary under test:** `./datawatch` — built this session with
`env -u TMUX -u TMUX_TMPDIR go build -o datawatch ./cmd/datawatch/` (146,303,160 bytes, rc 0).

## Method — which surface verified each check

Two complementary surfaces were used; both were run against the **real** binary, not mocks:

1. **Live daemon (real binary).** Five short-lived sandbox daemons were started
   (`HOME=<tmp> ./datawatch start --foreground --config <tmp>/config.yaml --port 1809x`),
   each with its own `data_dir` under `/tmp/dw-held-*`, killed by PID via a shell
   `trap ... EXIT`. Used to verify the operator **HTTP/CLI/config** surfaces end to end
   (checks 1, 4, 6). These daemons raised **no** questions; they only proved the
   reporting, config-flip, and validation surfaces live.
2. **Authoritative committed test suites (production code paths).** The held-state
   lifecycle — raise, shadow note, dedupe ("exactly one"), answer→action, stale refusal,
   and withdraw-on-clear — is deterministic state-machine logic, so it was verified by the
   repo's own committed Go tests that run the *real* `Coordinator.heldQuestionPass`,
   `heldBuild`, `heldQuestionAnswer` / `heldQuestionAction` against a *real*
   `autonomous.QuestionStore`. 74/74 PASS, 0 FAIL. This is the intended authoritative
   mechanism for the lifecycle (a live-daemon re-seed would only add idle-tick timing
   flakiness without exercising additional code).

## Results

| # | Check | Surface | Result | Observed |
|---|-------|---------|--------|----------|
| 1 | Binary builds (`go build -o datawatch ./cmd/datawatch/`) | build | **PASS** | `go1.27.2 linux/amd64`; `146303160 datawatch`; rc 0 |
| 2 | The **five** held states the howto names are all real held states | code + live report | **PASS** | Constants `blocked`, `review_held`, `revision_requested`, `awaiting_decision`, `pending_insert` (`held_question_table.go:32-36`); live daemon reported the same five in `states` |
| 3 | `held_mode: shadow` → **no question raised**, a would-be note recorded instead | suites + live | **PASS** | `TestHeldQuestionShadowWritesNoteAndRaisesNothing`, `TestHeldQuestionShadowNoteOncePerState` PASS; live daemon in shadow reported `open:[]` + the shadow note path is the only write |
| 4 | `held_mode: off/shadow/on` behave as described | **live daemon** | **PASS** | `PUT /api/config` dot-key `autonomous.questions.held_mode`: shadow→**on** (200, report `mode:on`)→**off** (200, `mode:off`)→**shadow** (200); invalid `bogus` → **400**, no change |
| 5a | **Exactly one** question per held (item, state); a second scan is a no-op | suites | **PASS** | `TestHeldQuestionDedupeSecondPass`, `TestHeldQuestionDedupeAcrossRestart`, `TestHeldQuestionDedupeDifferentStateSameItem` PASS; store dedupe key = `session_id + "_" + SHA256(session_id+item_key+state)` |
| 5b | Question carries link, options, recommendation, and a **default hold** answer | suites | **PASS** | `TestHeldQuestionTableEveryStateAndCategory`, `TestHeldQuestionOptionsSets`, `TestHeldQuestionRecommendFixedTable` PASS |
| 6a | **REST report** `GET /api/autonomous/held-questions` — mode, states, open, shadow | **live daemon** | **PASS** | `{"mode":"shadow","states":[5 ids],"open":[],"shadow":[]}`; `PUT`/`POST` to it correctly **not** a state (report is GET-only) |
| 6b | **CLI** `datawatch question held [--state]` — same data, readable | **live daemon** | **PASS** | `datawatch --url http://localhost:18095 question held` rendered mode + states + "nothing on the daemon is held right now"; `--state pending_insert` → 200 filtered; `--state bogus` → **HTTP 400** with the valid-id list |
| 7 | Answering with the **recommended** option runs the action and leaves a decision record | suites | **PASS** | `TestHeldQuestionAnswerHold/PendingRecorded/FreeText/Idempotent/Content` + `TestHeldQuestionFailureMode*` all 5 states PASS; decision record is the existing answer line (surface + actor recorded) |
| 8 | Answering a **stale** question (state already cleared) → refused with a reason | suites | **PASS** | `TestHeldQuestionStaleBlocked/ReviewHold/Revision/Awaiting/Pending` PASS; refusal = `ErrHeldStateChanged` ("held state changed before the answer arrived") |
| 9 | **Clearing the state another way** → the open question is withdrawn | suites | **PASS** | `TestHeldQuestionWithdrawWhenStateClears`, `TestHeldQuestionWithdrawLeavesAnsweredQuestions` PASS; pass withdraws with reason "held state `<state>` for `<key>` cleared" and drops the in-memory index entry |
| 10 | **Alerting unchanged:** held questions still fire alerts (WS + messaging + alert) gated by `notify_*` | code trace | **⚠️ NOT MET as documented** | See finding below. Held questions do **not** run through the 3-leg notify dispatch, and would not reach the alert leg even if they did |
| 11 | Cleanup — remove anything created; kill the daemon by exact PID | shell | **PASS** | 5 sandbox daemons all killed by PID (`trap`); all `/tmp/dw-held-*` removed; **no** PRD/automation/task/question artifacts persisted to disk; my sandbox ports (18095–18099) confirmed down via `/healthz`. Pre-existing production dev daemons (Oct 20–30) left untouched |

## Finding 10 — the howto's "Alerting" section is inaccurate

The howto states held questions *"fire warn alerts ... carry level warn + a daemon label ...
All three legs run in order (WS broadcast, messaging fan-out, alert store) ... If no
channel is on, the alert is still stored."* The code does not support this for held
questions:

- The 3-leg fan-out `Server.notifyQuestionRaised` (`internal/server/question_raise_dispatch.go:66`) is invoked from **exactly two** places:
  - the **operator-REST raise** path (`internal/server/questions.go:376`), and
  - the **worker question-marker** path (`cmd/datawatch/main.go:6716`, `Notify: httpServer.NotifyQuestionRaised`).
- The **held pass** raises directly against the store — `store.RaiseUnlessOpenSourceLine(q)`
  (`internal/autonomous/held_question_pass.go:158`) — and has **no** hub/Broadcast/Notify/
  alert/comm reference (verified by grep: zero matches in `held_question_pass.go`). So held
  questions are **not** fan-out through that dispatch at all.
- Independently, the alert leg is gated on high priority
  (`QuestionPriorityNormal`-only questions skipped; dispatch line 103:
  `if q.Priority == autonomous.QuestionPriorityHigh && ...`), and a held question sets **no**
  `Priority` field anywhere in the held build path. So even if a held question were routed
  through the dispatch, it would not reach the alert store.

**Net:** a held question is correctly (a) written to the question store + audit, (b) surfaced
by the REST report and the `datawatch question held` CLI, and (c) — once answered via the
answer surface — produces the decision-record line. But it does **not** push a WS
`question_raised` broadcast, a messaging-group message, or an alert-store alert. The feature
works as described for the report/answer/stale/withdraw behavior; only the "Alerting"
sub-claim (and the "warn alerts / daemon label" wording) overstates what happens.

## Minor documentation discrepancies (not functional, noted for accuracy)

- The howto's "state ids" example and prose use `revisions_asked`; the actual held-state id is
  **`revision_requested`** (`held_question_table.go:34`, `HeldRevisionRequested`). The PRD
  *status* is `revisions_asked`, but the held *state* id exposed by the report/CLI is
  `revision_requested`. The live report's `states` array confirms: 
  `["blocked","review_held","revision_requested","awaiting_decision","pending_insert"]`.
- The live CLI rejects an unknown state with a precise 400 listing the five valid ids — good
  validation surface.

## Cleanup confirmation

| Item | Status |
|------|--------|
| Sandbox daemon(s) | All 5 started via PID-captured `&`, killed by exact PID through `trap 'kill $PID' EXIT`. Confirmed down: `/healthz` empty on 18095, 18096, 18097, 18098, 18099. |
| Temp data dirs | `/tmp/dw-held-check`, `/tmp/dw-held-flip`, `/tmp/dw-held-cli`, `/tmp/dw-held-cli2` all `rm -rf` removed. |
| Live artifacts (PRD / automation / task / question / decision / audit / alert) | **None created** — live checks exercised only the build, REST report, CLI read, and config-surface flip against throwaway data dirs. All lifecycle writes were in the test suites' own `t.TempDir()` sandboxes. |
| Production dev daemons | Pre-existing ones (Oct 20–30, incl. a root `usr/local/bin/datawatch`) were **not** touched. |
| Only persistent output | The freshly built `./datawatch` binary (expected deliverable of the build step) and this report. |

## Environment

- Go: `go1.27.2 linux/amd64` (from `env -u TMUX env -u TMUX_TMPDIR`).
- Build: `go build -o datawatch ./cmd/datawatch/` → `./datawatch` (146 MB).
- Live daemons: `/home/dmz/.datawatch/prd-worktrees/56d30001/datawatch start --foreground --config <tmp>/config.yaml --port 1809x`.
- Config surface used for the flip: `PUT /api/config` with dot-path key
  `autonomous.questions.held_mode` (the howto's documented `/api/config` path).

## Verdict

The held-state-questions feature is **present, built, and behaves as documented for its
core behavior** — all five held states, shadow note-only mode, the off/shadow/on flip, the
one-question-per-hold dedupe, the question's link/options/recommendation/default-hold, the
REST + CLI report surfaces, the answer→action + decision-record path, the stale-answer
refusal, and the withdraw-on-clear — with **74/74** authoritative committed tests passing and
the relevant live surfaces confirmed against the real binary.

The one documented behavior I could **not** confirm is the "Alerting" section: held questions
do not flow through the WS/messaging/alert fan-out (which is wired only to the operator-REST
and worker-marker raise paths), and independently cannot reach the high-priority-gated alert
leg because they carry no priority. This is a **documentation inaccuracy**, not a
functionality gap in the raise/answer/withdraw mechanism itself.
