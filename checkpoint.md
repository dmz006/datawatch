# Checkpoint — docs set for "Make a daemon restart safe" (verifier/judge resume + script pickup)

## Run 2 — verification pass, 2026-11-10 20:45
Status: **COMPLETE** — audit found one new fact the 07:36 docs run did not
have: the restart matrix cannot be run at all against a build of this tree.

### New finding (empirical, this machine)

- Built `/tmp/datawatch` from this tree (v9.118.3). Ran
  `scripts/restart-matrix.sh --strict --only worker-running` (one of the
  eight enforced must-survive states): **FAIL at `state_reached`** —
  "could not create the PRD".
- Root cause: the matrix sandbox config (`rm_write_config` in
  `scripts/restart-matrix/lib.sh`) does not set `server.allowed_project_roots`
  — only the compute-node variant (`rm_write_config_pi`) does, and its comment
  there says exactly why ("the sandbox dir is not under the default roots, so
  name it"). The daemon's default allowed roots are the data-dir worktree
  base + `~/.workspace`-style default + configured roots; the sandbox project
  dir (`$TMPDIR/rm.*/project`) is under neither, so
  `POST /api/autonomous/prds` answers
  `project_dir refused: project directory is outside the allowed project
  roots` (guard since `7182f10db`, 10-26 — an ancestor of the 11-09 fix
  commit, so the guard was in the tree when the 11-09 "matrix passes" runs
  were made; those runs evidently used a binary predating the guard).
- Positive control: same sandbox with one line added
  (`allowed_project_roots: ["/tmp/rm-manual"]`) → PRD creation succeeds
  (id returned, status draft). Diagnosis confirmed.
- Consequence: **every** matrix state that creates an Automaton fails at
  creation on a build of this tree — including the eight enforced states the
  release smoke records as passing. The 11-09 smoke "ok" lines were logged
  output of a binary that predates the guard, not a property of this tree.

### Docs updated this run (3 files, in scope)

- `docs/howto/restart-with-work-running.md` — *Current status*: replaced
  "the `planning` row and its release-smoke run are still open" with the
  matrix-cannot-run finding, the exact refusal text, the 2026-11-10
  confirmation (v9.118.3, `worker-running` FAIL at `state_reached`, positive
  control), and the one-line fix location (`rm_write_config`). All
  "verified on a sandbox daemon" claims are now flagged as stale until the
  harness fix lands.
- `docs/datawatch-definitions.md` — *Verifying restart behavior* and
  *Verification script run record* entries: the "still owed a live check /
  re-run on a build from this tree" notes now say the re-run is not possible
  yet and point at the *Current status* note.
- `docs/howto/autonomous-recovery.md` — *A task still verifying at restart*
  close: same flag added.
- `docs/howto/README.md` — index line checked, no claim to correct,
  unchanged.

### Operator-review items (supersedes the list below)

1. **Fix the matrix harness**: add
   `allowed_project_roots: ["$RM_SB"]` to `rm_write_config` in
   `scripts/restart-matrix/lib.sh` (one line; already proven sufficient by
   the positive control) — then every matrix state is runnable again.
2. Re-run the full matrix (all 14 states) on a build cut from this tree
   after the harness fix; only then do the "verified on a sandbox daemon"
   rows and the five release-smoke skips (whose reasons still record
   pre-fix readings) mean what they say.
3. Live restart check on the build in use for the three no-setting cases
   (verifier adoption, judgment resume, script pickup) — still not done.

---

## Run 1 — 2026-11-09 07:36
Status: COMPLETE (docs written; superseded by Run 2 on the matrix status).

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

### docs/datawatch-definitions.md
- New entries: **Verifying restart behavior** (task in `verifying` state:
  worker/session/result kept, no retry, no second worker; boot order:
  live verifier re-attach → on-disk verdict → fallback re-verify once or
  reset; judgment analogue; before any task state is resumed) and
  **Verification script run record** (detached run, durable `verify-runs`
  record; boot pickup order; timeout from recorded start; effect versus the
  pre-fix audit), plus the existing planning-policy entries cross-linked to
  both.

### docs/howto/README.md
- Index row for `restart-with-work-running` updated to name the verifying /
  judging / verification-script pickup.

### Verification done
- Re-read the four final files in full; behaviour described matches the code
  paths (`verifying_status.go` decision order, `verify-runs` boot pickup,
  `planning_reattach.go`).
- Confirmed the docs do not claim the pre-fix audit outcomes as current:
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
