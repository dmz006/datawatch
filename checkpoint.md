# Checkpoint — docs set for "Make a daemon restart safe" (verifier/judge resume + script pickup)

Time: 2026-11-09 07:36
Status: **COMPLETE** (this run supersedes the 02:09 checkpoint, which recorded the
pre-fix boot path; the fixes it flagged are now in this tree's code and the docs
below describe the post-change behaviour.)

## What changed in this run (all four docs, 126 insertions / 13 deletions)

### docs/howto/restart-with-work-running.md
- Intro: planning is now one of the cases with "no setting to choose" beside it
  — a verifying task, an in-flight guardrail judgment and the verification /
  final-gate script are all picked back up by the restarted daemon instead of
  being redone.
- *Current status*: notes the boot path, verifier/judge re-attach and
  detached-script pickup are all in this tree, and that the restart-matrix
  `planning` row + its release-smoke run are still open and no live
  deploy-restart has confirmed any of it (points at the *Still to review*
  notes in operations.md).
- New section *Verifying, judging, and verification scripts*: the three
  no-setting cases in the daemon's own boot order (re-attach live verifier →
  consume its leftover verdict → re-verify once; same shape for guardrail
  judgments; script: wait on a live run / consume an undelivered exit code /
  run once, retiring any undelivered run from an earlier attempt first),
  timeout enforced from the recorded start time, and the delta versus the
  pre-fix audit (`plans/restart-matrix-findings.md`).

### docs/howto/autonomous-recovery.md
- "What happens to Automata when the daemon restarts": planning was previously
  the only case; a fourth case is now named (verifying task, in-flight
  judgment, running script — all picked back up, none with a setting).
- New *A task still verifying at restart* subsection under *Restarting the
  daemon while a task is in progress*: the boot order (live session → verdict
  on disk → live script → script exit code on disk → old behaviour), the
  judgment analogue, what the operator sees (no double verifier/judge, no
  re-run) and the "still owed a live check" flag.
- *See also*: added `howto/restart-with-work-running`.

### docs/operations.md
- Restart-safety table: the `verifying` row now reads **Fixed in this tree**
  (verifier adopted — verdict read, on-disk verdict consumed, verification
  runs only where nothing usable survives; matrix state `verify-session`
  pending re-run), and a new table row *Task's verification or final-gate
  script running* covers the detached run + durable record + boot pickup
  (matrix states `verify-script`, `verify-script-mid`, `final-gate` pending
  re-run).
- *What still does not survive* → *What the audit left as an operator choice,
  and how this tree resolves it*: the two items (daemon-run scripts,
  un-adopted verifier) are both resolved in this tree; guardrail judgments
  named as the same shape; matrix rows / smoke skips flagged as pre-fix
  measurements until re-run on this tree.
- *Tasks that were verifying*: now says the daemon reads the live or on-disk
  verdict instead of re-running.
- *KillMode*: verification scripts are no longer daemon children; under
  `KillMode=process` a recorded live script run (pid + start time matching its
  record) is waited on, not re-run.
- Release-smoke paragraph: all four states (`verify-script`, `verify-script-mid`,
  `final-gate`, `verify-session`) stay skipped with the pre-fix readings until
  the matrix states implement and re-run the fix.
- *Still to review (operator)* item 3 rewritten: fixed in this tree and no
  longer an open choice; still owed = re-run the four matrix states on a
  build from this tree, update (or drop) the release-smoke skip reasons, and
  confirm the in-use build (no double script run, no second verifier).

### docs/datawatch-definitions.md (glossary)
- New *Verifying restart behavior*: the boot order for a `verifying` task
  (re-attach live session → consume on-disk verdict → re-verify once / reset
  to `pending`), the judgment analogue, order-before-task-resume, live check
  still owed, links to the two howtos.
- New *Verification script run record*: detached runs + durable record under
  the data dir (`verify-runs`), boot pickup order (wait / consume / retire
  undelivered / run once), timeout from recorded start time, the delta versus
  `restart-matrix-findings.md`, links to plan + howto.

## What I verified (source of truth = code in this tree)

- Boot order: `ReconcileVerifiers` then `ReconcileJudges` are wired before the
  task executor is re-launched (`internal/autonomous/api.go`, host hooks
  `SetVerifierReattachFn` / `SetVerifierResultReader`).
- Verifying resume outcomes in `internal/autonomous/verifying_status.go`
  (`ResolveVerifyingResume`): reattach (live `VerifierSessionID`) > consume
  (parseable persisted verdict) > the recorded-decisions/reset rules,
  matching the docs' ordering (attach → verdict on disk → re-verify once).
- `ConsumeVerifierResult` in `internal/autonomous/manager.go`: idempotent —
  pass completes, fail records + requeues; matches "a fail goes through the
  normal retry path".
- Judge resume: same attach / consume / re-judge-once shape
  (`internal/autonomous/judge_resume.go`, `judge_reconcile.go`).
- Script: detached run + durable record (pid, start time, output, exit code,
  delivered flag) under the data dir; boot pickup `resumeVerificationScriptRun`
  waits on the live process, consumes an undelivered exit code, retires an
  undelivered unfinished run from an earlier attempt before running fresh
  (`internal/autonomous/verification_script_exec.go`,
  `verification_script_resume.go`, `verification_script_record*.go`);
  `holdVerifyingForScriptRun` keeps the task in `verifying` while the run is
  alive. Timeout computed from the recorded start time.
- Pre-fix audit wording quoted verbatim from
  `docs/plans/restart-matrix-findings.md` ("daemon children, rerun from
  scratch, OPERATOR PICK"); the `RM_PICK_STATES` skips in
  `scripts/release-smoke.sh` still carry the pre-fix skip reasons — the docs
  flag both as pending re-run on this tree rather than claiming they changed.
- Cross-links: all relative `.md` links in the four files resolve (0 broken);
  anchors used (`#verifying-judging-and-verification-scripts`,
  `#restarting-the-daemon-while-a-task-is-in-progress`) match live headings.

## Operator-review items flagged (carried into operations.md *Still to review*)

1. Live restart check on the build in use for all three no-setting cases
   (verifier adoption, judgment resume, script pickup) — not yet confirmed
   live.
2. Re-run `scripts/restart-matrix.sh --only
   verify-script,verify-script-mid,final-gate,verify-session` on a build
   cut from this tree.
3. Update the `scripts/release-smoke.sh` skip reasons (or drop the skips) so
   the smoke records the new behaviour instead of the pre-fix readings.
4. The `planning` matrix state / smoke skip stays until implemented and
   re-run (pre-existing, unchanged).
