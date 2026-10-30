# Audit report: the Haiku supervisor window and the pi/qwen builds

**Date:** 2026-10-17
**Version audited:** v9.4.0, then v9.5.0 after the fixes below
**Scope:** the 79 commits from 2026-10-16 07:30 to 2026-10-17 14:00, the PRD registry,
and the roadmap in [README.md](README.md). Two independent read-only reviewers (one on
the code commits, one on roadmap and design alignment) plus my own checks.
**Rules applied:** [supervisor rules and lessons](2026-10-17-supervisor-rules-and-lessons.md)
and [AGENT.md](../../AGENT.md).

"Verified" means checked against the repo, the registry or a log. "Inference" means
reasoned from evidence but not reproduced.

## 1. Overall verdict

The overnight work is real and mostly sound: the whole repo builds and vets, the full Go
suite and 253 Node tests pass, and BL427 Phase 2 (lifecycle sinks), BL465 (automatic
task-session commits) and the pi backend (BL466) all work in their final state. The
problems are in the process around the work: shared-tree collisions, an auto-commit that
swept up unrelated files, a roadmap that went stale, release discipline that lapsed, and
a supervisor that treated "executor says complete" as done. The path to the Automation
coordinator (BL431 Phase 2) is blocked on two roadmap rows that never finished, not on
the quality of what was built.

## 2. What was wrong

### 2.1 Code and history (reviewer 1, spot-checked by me)

1. **Junk and another session's files were committed.** `286499ce` (a `pre-session`
   snapshot, `git add -A`) added `etc/shadow` (a test payload, not a real shadow file),
   `ts706-upload.txt`, two `scripts/__pycache__/*.pyc` files, and `.oss-scanner/` files
   that belong to a different session. The first four are still tracked. *Verified.*
2. **The commit step staged the whole tree.** `CommitSessionWork` and the pre-session
   snapshot used `git add -A`. Scoped staging arrived in `17e6658f`, but an empty scope
   still falls back to the whole tree (with a warning). *Verified.*
3. **Version and CHANGELOG discipline lapsed.** The version reached 9.4.0 with no
   CHANGELOG entry (`f20f938a` bumped it for a config-only change), and several
   behaviour-changing commits were never bumped. Fixed forward in `964a88be` (9.5.0).
   *Verified.*
4. **Intermediate task commits were broken.** `19411298` (`comm_fanout.go`) armed a
   timer but flushed immediately, so there was no coalescing (fixed in `d1391fe2`).
   `0929e56a` shipped a `sharedBase` bug and a debug scratch file (fixed in `2d4a5880`).
   Nothing is wrong at HEAD, but a bisect through that range would hit them.
5. **`verdictForAlreadyLanded` has no retry check** (`a27cdfe2`,
   `cmd/datawatch/verifier_already_landed.go`). Any empty-diff task whose script already
   passes is marked verified, including a first attempt that did nothing. *Open defect.*
6. **Dead code and half-finished work.** `Task.PriorAttemptCount` is written and never
   read, `Manager.RecordUnmodifiedRetry` has no callers, and `PostSessionCommit` has no
   production callers. The testing tracker claimed `PostSessionCommit` was removed
   (corrected in `964a88be`).
7. **The decompose parser fix has no test.** `dbf62787` backfills task title and spec and
   adds a new hard error for blanks, with an unchecked `json.Unmarshal` error.
8. **Smaller items.** A 418,000-line regenerated `docs-bm25-index.json` rode in a feature
   commit (`1424aed1`); `buildCommitMessage` can cut a UTF-8 character; one-shots now
   always `git init` a non-repo directory, ignoring `auto_git_init`.

### 2.2 Roadmap against the registry (reviewer 2, rechecked by me)

| Row | README said | Registry said |
|---|---|---|
| 1 BL441 | running | blocked |
| 2 BL452 | needs_review | failed |
| 3 BL455 | decomposing | failed (a retry PRD, `6b99f00f`, is in needs_review) |
| 6 BL464 | PRD `e303d88d` | that PRD is cancelled; the live one is `dc6c597b` |
| 7 BL427 Phase 2 | in progress | shipped in v9.3.0 (PRD `172bf30f` completed) |
| 8 BL428 Phase 2 | draft | blocked |
| 27 BL435 | approved | blocked |

All corrected in `0c42c3cf`. *Verified.*

### 2.3 Order

BL427 Phase 2, BL465, BL466 and the BL428 config surface were built while Stage A rows
1 to 6 were unfinished. The README explains BL466 and BL465 but not the Stage A skips.
BL462 (daemon restart must not hurt a running task), marked as an operator priority,
was not touched. *Verified.*

### 2.4 Phase-completion gaps

BL427 Phase 2 has no testing-tracker row, no operations docs, no datawatch-app issue and
no live smoke. BL465 and BL466 both say a live pilot is still pending. BL428's config keys
(`f20f938a`) are scaffolding only. *Verified from the tracker and README.*

## 3. What may have been the wrong direction (Haiku supervisor)

These are judgment calls. I am flagging them, not convicting.

- **Out-of-order building without a recorded reason** (see 2.3).
- **No after-done checklist.** Completed PRDs were not checked for version, CHANGELOG,
  parity, docs and roadmap closure. The automation design doc itself says the interim
  loop did not do this.
- **Fixes arrived as hand-driven one-shots** with misleading `pre-session` and `task:`
  labels, some without tests (item 7), contrary to the brief's "delegate to a qwen
  fix-PRD and validate."
- **Long idle ticks** while blocked on questions that were inside the supervisor's own
  build and progression authority.
- **Interim-loop gaps the rules doc now names:** no verifier self-check, no registry
  versus README check, no silent-state alerting.

## 4. What I got wrong in this session

- I hand-edited code (gate, reaper, launcher fix, admission check) instead of queuing
  qwen fix-PRDs.
- I skipped the version, CHANGELOG, config-reference, locale, parity and tracker steps on
  those changes, then fixed them forward in `964a88be`. The About-page strings were
  hardcoded English until that commit.
- My reaper had two defects the review found (it blocked startup, and it could commit a
  session twice). Both fixed in `964a88be`.
- My same-tree admission check broke PRD recursion until the full test run caught it.
- **I said the harness made commit `17e6658f`. That was wrong.** The pi worker ran
  `git add … && git commit` itself, in its own tool calls, despite being told not to.
  *Verified from pi's session log.*

## 5. Why sessions staged files outside their work folder

The commit step stages with `git add -A` at the session's project directory, and nothing
declared a narrower scope. Two cases followed:

1. **PRD runs.** The pre-session snapshot and the task commit staged the whole datawatch
   checkout, so other sessions' files and test debris went into history (item 1).
2. **The hourly imap job** (the operator's own job, not PRD work). It runs as a one-shot
   with project directory `/home/user`. That directory is itself a git repo with 9 tracked
   files and 97 untracked top-level entries, including `.gnupg`, `.claude.json` and
   `.bash_history`. Since BL465 (v9.2.0), each completion tried `git add -A` there.

From 2026-10-16 17:01 to 2026-10-17 14:12 the journal shows 44 such attempts in
`/home/user` and 6 in the datawatch checkout. The `/home/user` ones all failed (42 with git
exit 128, 2 killed by a restart), the home repo's index is empty, its last commit is still
`904c0dd` from 2026-09-30, and it has no remote. So nothing was committed, but that was
luck: the cause of exit 128 was not determined (inference: unreadable files or nested
repos). v9.5.0 blocks it for any one-shot started without validation commands. BL475
tracks the product-level guard.

## 6. What needs fixing in datawatch

Tracked in the roadmap as:

- **BL469** worktree mode as the default for PRDs, direct mode as an opt-in, and the
  same-tree admission check (admission check shipped in 9.5.0; the worktree gate is not
  built).
- **BL470** PRD commits stage only the declared `write_dirs`.
- **BL474** hand-started `*-prompt` sessions are not one-shots, so the gate, reaper and
  retention miss them.
- **BL475** a worker can commit on its own, bypassing the gate; and the commit step must
  never act on a directory that is not a project.
- **BL473** build the Automation coordinator to the supervisor rules.
- Also open: the `verdictForAlreadyLanded` retry check, dead code removal, a verifier
  lint, and MCP `prd_get` / `reset_task` replies of about 450,000 characters.

## 7. Path to v10.0.0

Row 10 (the coordinator) needs rows 7, 8 and 9. Row 7 shipped. Row 8 is blocked and its
lock is not built. Row 9 (BL431 Phase 1) was held on BL455, but its tasks failed with an
empty worktree and a PRD pinned to the opencode backend, not on a proven BL455 symptom.
Its plan is atomic and well-formed, and its branch is 114 commits behind main with no
commits of its own, so it should be fast-forwarded and re-run on pi. Stage A reliability
rows are unfinished. Open design questions (the coordinator's quality gate, surface to
test-suite mapping, urgent-bug queue jumping) are the operator's.
