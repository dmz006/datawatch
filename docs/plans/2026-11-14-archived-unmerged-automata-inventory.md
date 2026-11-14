# Archived and finished Automata that never reached main: inventory and plan

Date: 2026-11-14. Base: `main` at f8ab0020d. Scope: read-only investigation, then this plan.
No merge, reset, cancel, archive or approve command was run.

## 1. The question

How many Automata are archived or otherwise finished but never merged into main, how much finished
work is that, and what would it take to land or consciously drop each one. The operator's concern is
that finished work was lost because nothing deterministic merged it, and that datawatch itself, not a
supervisor session, should own landing.

## 2. How the numbers were produced

- `scripts/branch-audit.py` (compares commit subjects with main and joins the Automaton status):
  156 unlanded, 0 pending, 12 live, 14 landed.
- Per branch: `git rev-list --left-right --count main...<branch>` (ahead, behind), `git diff
  --shortstat main...<branch>`, `git cherry main <branch>` (patch-equal commits), `git merge-tree
  --write-tree main <branch>` (would a merge conflict), and a per-file blob comparison against main.
- Task branches were compared with their parent Automaton branch with `git cherry`.
- Feature checks: grep and `git log --grep` on main for what each branch asks for, plus whether the new
  files the branch adds exist on main.
- The REST list at `/api/autonomous/prds` shows 81 records (29 archived); the other branches belong to
  records already removed, which the audit reports as "no Automaton record".
- Scratch data: `/home/dmz/workspace/supervisor-scratch` (`res4.json`, `audit.txt`).

Line counts below are per-branch diffs against the merge base; they overlap where several branches
edit the same shared files (CHANGELOG, operations docs, `app.js`), so totals are upper bounds.

## 3. What the 156 actually are

The audit's "156 archived branches" is two populations plus a status mix:

| Population | Branches | Note |
|---|---:|---|
| Automaton work branches (`automaton/<id>`) | 28 | The real work items |
| Task sub-branches (`automaton/<id>--tasks-...`) | 128 | 117 are fully contained in their parent branch |

Status of the 28 Automaton branches: 20 archived, 5 needs_review, 1 failed, 1 blocked, 1 cancelled.
Only 20 are literally archived; the other 8 are finished or stuck in a state the audit also treats as
"archived with work not on main". The audit label should be read as "terminal and not landed".

The 117 contained task branches carry no extra work: every commit is patch-equal to a commit on the
parent branch (task merges into the parent already happened). They are clutter, not risk, and fold into
their parent's decision.

## 4. Classification (28 Automata, then the 11 task outliers)

Classes: (a) finished and valuable, merge needed; (b) already on main by another route; (c) partial or
failed; (d) intentionally dropped / nothing to keep.

| Class | Automata | Commits ahead | Files (upper bound) | Lines |
|---|---:|---:|---:|---|
| (a) merge needed | 20 | 617 | 959 | +59,346 / -1,986 |
| (b) superseded | 4 | 58 | 71 | +5,131 / -89 |
| (c) partial or failed | 2 | 28 | 15 | +4,870 / -6 |
| (d) drop | 2 | 2 | 3 | +35 / -2 |
| Total | 28 | 705 | 1,048 | +69,382 / -2,083 |

Class (a) is about 85 percent of the lines. Behind-main distance across the 20: median about 160
commits, worst 5,852. Merge dry-run (`merge-tree`): 5 merge clean, 15 conflict.

### 4.1 Class (a): finished and valuable, merge needed (20)

State is commits ahead / commits behind main / files / lines / merge dry-run. "Evidence" is what the
grep on main found; in every row the branch adds source files that do not exist on main.

| Automaton | Title | Status | Ahead | Behind | Files | Lines | Merge | Evidence and notes |
|---|---|---|---:|---:|---:|---|---|---|
| 72bc0001 | Installable packs for standard tasks and Automaton templates | archived | 111 | 2827 | 152 | +10280/-119 | conflict (40 files) | 106 new files not on main; the largest item; operator already decided to rebase it |
| 7bd50001 | Git hosts: settings, readiness, GitLab | needs_review | 104 | 3548 | 106 | +10524/-85 | conflict (48 files) | host runner, config, CLI, PWA page absent; main has only the fake-host CI test and a decision doc; review verdict pending |
| 72d10109 | Pi stall reporting, fast failure | archived | 49 | 1375 | 39 | +2115/-17 | conflict (4) | 18 new files absent |
| 72d1017e | Finalise session records whose process is gone | archived | 44 | 1903 | 50 | +3073/-26 | conflict (18) | finaliser and its setting absent |
| 90d60001 | Deploy after ship (post-ship deploy, Deploy card) | archived | 40 | 5852 | 62 | +4357/-45 | conflict (58) | the deploy package and CLI are absent; main only has the unrelated supervisor-loop step of the same name |
| 13f00001 | Empty system_tags array on summaries, testing tracker | archived | 33 | 3593 | 13 | +625/-20 | conflict (10) | small, old |
| 18980001 | Actionable Automata Inbox | needs_review | 26 | 345 | 43 | +3025/-178 | conflict (10) | 6 new files absent |
| 14310001 | Stale browser state cannot break the Sessions view | needs_review | 14 | 861 | 20 | +1695/-83 | conflict (12) | 3 new files absent |
| 2b340001 | Route each task to a lane at admission | archived | 21 | 37 | 22 | +1224/-93 | clean | no routing at admission on main; ready now |
| 2b580001 | Make capacity fall-through work | archived | 18 | 7 | 45 | +1764/-41 | clean | main has the inert first version (setting exists, a bug record was filed); this is the fix; ready now |
| 33160001 | Consistent phone page layout across views | archived | 12 | 164 | 10 | +833/-25 | conflict (6) | |
| 6e980001 | Automata Resources, Progress and costs, Housekeeping | archived | 18 | 345 | 31 | +1103/-370 | conflict (18) | |
| 71660001 | Plan status roll-up and plan rename/edit | archived | 16 | 167 | 52 | +3796/-76 | conflict (4) | 15 new files absent |
| 8b7a0001 | Core automation stability 1: automatic gate repair | archived | 20 | 85 | 64 | +5287/-10 | clean | 33 new files absent; the daemon repair is not on main; ready now |
| 91e40001 | Projects page phone fit and grouped empty lines | archived | 18 | 8 | 16 | +480/-24 | clean | ready now |
| a1a30001 | Trace retention follows the session | archived | 15 | 378 | 27 | +648/-46 | conflict (2) | |
| d3460001 | Usage back-off must not block verification (review reserve) | archived | 17 | 100 | 59 | +2546/-179 | clean | review reserve absent on main; ready now |
| e5700001 | Core automation stability 2: archive only after merge | needs_review | 23 | 85 | 127 | +4491/-472 | conflict (2) | the fix for this very problem, stuck in rules review; conflict is two files |
| 8d130001 | Build-host toolchain freshness check | archived | 10 | 163 | 14 | +1223/-2 | conflict (2) | |
| 652c0001 | Make the lifecycle queue-overflow test deterministic | needs_review | 8 | 99 | 7 | +257/-75 | conflict (2) | the test files exist on main in older form |

Task-level extras in class (a): the design record on branch `d1130001--tasks-investigate-why-a-judge-or-verifier-pool-21880002`
(162 lines, one new file, not on main), and the two task branches that run slightly past their
parents (`13f00001--tasks-update-the-testing-tracker-...`, `18980001--tasks-auto-fix-rules-review-findings-...`);
both follow their parent's decision.

Five are ready to land today with a clean merge: 2b340001, 2b580001, 8b7a0001, 91e40001, d3460001.
Fifteen need a rebase or conflict resolution first; four of those (e5700001, 8d130001, 652c0001,
a1a30001) conflict in only two files.

### 4.2 Class (b): already on main by another route (4)

| Automaton | Title | Evidence |
|---|---|---|
| 57e90001 | PWA tests for the persisted server choice | the implementation and test merged through two task merges on main; the test file exists on main |
| ac690001 | Agent mail between peers: design pass | the design record landed on main as its own docs commit |
| 4e190001 | Deflake round five | 2 of its 4 added files are byte-identical on main; the rest is a report |
| b20f0001 | Verifier cap above 1 requires a measured result | 9 of its 16 added files are identical on main; recheck the 7 remaining files before deleting |

Task outlier in this class: `42950001--tasks-auto-fix-rules-review-findings-...` (both added files
identical on main).

### 4.3 Class (c): partial or failed (2, plus 2 task outliers)

| Automaton | Status | Ahead/behind | Notes |
|---|---|---|---|
| 38870001 | failed | 12 / 2 | Fix startup crash from a typed-nil model; the diff has a fix and two tests (153-line typed-nil test absent from main). Merges clean. Needs a human look at why it failed, then likely lands. |
| 70900001 | blocked | 16 / 4511 | Configuration parity audit and CI gate; report and baseline exemptions, no source overlap with main. Gate partly exists on main under another route; decide whether the audit report is still wanted. |

Task outliers: `70900001--tasks-wire-the-configuration-parity-...` (follows its parent) and
`9f8f0001--tasks-auto-fix-rules-review-findings-...` (3 files, 8 lines; review and drop or land).

### 4.4 Class (d): intentionally dropped or nothing to keep (2)

| Automaton | Why |
|---|---|
| 718a0001 | Single commit adding a stray completion marker file |
| d2f10001 | Single commit that only rewrites two source comments (removes tracker ids) |

Both are safe to record as dropped and delete. No operator value is lost.

### 4.5 Not counted: live and landed

12 branches belong to running Automata (their merge is not due yet); 14 are fully landed and only
need branch deletion (the existing safe-to-delete report covers them). Five more task sub-branches
belong to running parents (`3e5c0001`, `31e40001`, `5c0c0001`, `89e70001`, `d0460001`).

### 4.6 Totals at stake

- Work that needs a landing decision: 20 Automata in class (a), 617 commits, up to 959 files,
  about 59 thousand lines added, plus 2 in class (c) (28 commits).
- Work that needs only a deletion record: 4 in class (b), 2 in class (d), 117 contained task branches.
- Biggest single risks by size: packs (152 files), git hosts (106), deploy after ship (62),
  automatic gate repair (64), archive-before-merge fix itself (127 files).

## 5. Root causes: how finished work gets archived before merge

All references are to the tree at the base commit above.

1. **The close step archives completed and rejected Automata in the same pass.** In
   `closeFinishedRecords` (`internal/autonomous/supervisor_loop_steps.go`) only a cancelled record gets
   a grace and the branch check (`archiveKeepsBranch`, added by the earlier cancel-keeps-branch fix).
   A completed or rejected record goes straight to `Manager.Archive`, whatever its branch holds. This
   is the main hole: finished and archived are one step, merge is not in it.
2. **The merge-pending watcher is blind to most of the lifecycle.** The pass in the coordinator
   (`automation_coordinator.go`, `merge_pending_pass.go`) only looks at `Status == completed` with a
   branch. By the time it runs, step 1 has usually archived the record. It also runs in `shadow`
   mode here (`autonomous.automation.merge_pending_mode`): it writes shadow decisions and never alerts.
   `needs_review`, `blocked` and `failed` records go to the triage pass, which alerts on the
   failure, not on unmerged work.
3. **`Manager.Archive` has no branch guard at all** (`manager.go`). The operator, REST, MCP and
   supervisor sessions can archive any terminal record. Archive then also removes the worktree
   (`cleanupOrphanedWorktree`), so the only trace left is a branch ref nobody reviews.
4. **Nothing owns landing.** The daemon has the pieces (merge pipeline record, `autonomous_merge`,
   merge lock, conflict policy, safe-to-delete report, an unlanded-work list with keep, archive and
   delete actions), but each is on-demand. No daemon pass takes a ready branch from finished to merged;
   a supervisor session has to remember to do it, and that is the step that was lost.
5. **Branches rot.** There is no automatic rebase. Behind-main distance reaches thousands of commits for
   the older branches, so a merge that would have been trivial becomes conflict work (15 of 20 conflict
   today). Delay turns "merge" into "rebuild".
6. **Review gates stall finished work.** Five Automata sit in `needs_review`, including the one
   that fixes this problem. A record in review state is neither merged nor visible as unmerged.
7. **The audit is a script, not a daemon feature.** `scripts/branch-audit.py` takes minutes, needs a
   person to run it, and counts task sub-branches as separate findings (128 of 156), which buries the 28
   real items.
8. **Task branches are never reaped after their merge into the parent**, inflating every count.

## 6. Features that would have prevented it deterministically

| Feature | Closes root cause | Exists today |
|---|---|---|
| Never archive (or delete the worktree of) a record whose branch has commits not on base; a keep or drop landing record is required instead | 1, 3 | Partly: cancelled only (earlier fix); the archive-before-merge Automaton (e5700001) is the complete version and is stuck in review |
| Merge-ready sweep that includes archived, needs_review, blocked and failed records | 2, 6 | e5700001 includes archived; shadow mode still on |
| Per-branch landing record: landed, pending, kept, dropped with who, when and why | 1, 3, 4 | No (keep records exist for unlanded work only) |
| Merge-pending mode on, with the existing 30-minute alert | 2 | Built; set to shadow |
| Automatic rebase queue (serial, local, mechanical conflict policy, human queue for the rest) | 5 | No (merge pipeline has conflict policy for the merge step only) |
| Merge lane in the daemon: rebased, gate green, then the existing merge pipeline and release approval | 4 | No (pipeline exists; trigger is manual) |
| Reap contained task branches after the parent lands | 8 | Safe-to-delete report lists them; no daemon action |
| Unlanded-work counts, per class, on every surface and in an alert | 7 | List exists (`unlanded_manager.go`); no counts card, no daily alert |
| Completed means landed, for kinds that have a branch | 1 | No |

## 7. Implementation plan

Ordering principle: stop the leak first (steps 1 to 3), then drain the backlog while the leak is
closed (step 6 starts after 2), then automate the rest (steps 4, 5, 7, 8). Effort is in tasks, the
unit the planner decomposes into. No release numbers or per-task backends here; those follow the
normal planning rules.

### 7.1 Automata to file

| Step | Automaton (one line) | Outcome and how to verify | Depends on | Tasks |
|---|---|---|---|---|
| 1 | Finish the archive-before-merge Automaton (e5700001): rebase onto main, clear the review findings, land | `Manager.Archive` and the close step refuse a record whose branch has commits not on base, for completed, rejected and cancelled; tests cover each path and the explicit keep and drop exits; operator archive via REST returns a refusal naming the branch | none | 3 (rebase, review fixes, land) |
| 2 | Landing record per branch | Every Automaton branch has a landing state (pending, landed, kept, dropped) with actor, time and reason, written by merge, keep and drop actions; archive requires a state other than pending; shown by REST, CLI, MCP, chat and PWA; verify with a table-driven test over all four states and the five surface reads | 1 | 6 |
| 3 | Merge-pending sweep covers every terminal state | Pass includes archived, needs_review, blocked and failed records with a branch; mode switch from shadow to on after the shadow window; verify with a fake git runner (one test per state) and one alert per episode | 1 | 3 |
| 4 | Automatic rebase queue | One branch at a time, under the existing merge lock, merge main into the branch with the configured mechanical conflict policy, never force-push, conflicting branches go to a visible needs-human list; verify with fake git: clean, mechanical conflict, real conflict, concurrent run refused | 2, 3 | 8 |
| 5 | Merge lane | A branch whose landing state is pending, rebased, final gate green and review passed is merged by the existing merge pipeline with release approval respected; landing record set to landed; verify with fake pipeline: ready, gate red, review pending, approval ask | 2, 4 | 8 |
| 6 | Backlog drain (one-time, uses steps 2 and 5 where available; otherwise the existing manual merge action with a recorded landing state) | Each class (a) Automaton is landed or recorded dropped in the order in 7.3; verify with the audit: unlanded count reaches 0 and `datawatch autonomous unlanded` lists nothing | 2 (hard), 5 (soft) | 20 landings, 4 to 6 rebase tasks for the large ones |
| 7 | Reap contained task branches and landed Automaton branches | After the parent's landing record is landed or dropped, task sub-branches that are patch-contained are deleted with a backup ref; verify with safe-to-delete report empty of them and a restore test | 2 | 3 |
| 8 | Unlanded-work counts in the daemon | Count per class and age on REST, CLI, MCP, chat and PWA, plus one daily alert above a configured count; `scripts/branch-audit.py` becomes a thin caller or is removed; verify with a parity test and an alert test | 2 | 5 |
| 9 | Completed means landed (only if the operator says yes to question 9) | A record with a branch does not enter completed until its landing state is landed or kept or dropped; verify with a state-machine test | 2, 5 | 4 |

Totals: about 40 tasks for the new code, plus the 20 landings in step 6.

### 7.2 What is deliberately not in the plan

- No new Automaton for the 117 contained task branches; step 7 handles them as a class.
- No re-run of class (a) work: all of it is finished and reviewed except where noted; the cost is
  rebase and conflict resolution, not re-implementation.
- No change to `AGENT.md` or `CLAUDE.md` rules from this plan.

### 7.3 Order for the backlog drain (step 6)

1. The five clean merges, smallest first: 91e40001, 2b580001, 2b340001, d3460001, 8b7a0001.
   Each needs the gate run on the merged result, not on the branch alone.
2. The archive-before-merge Automaton itself (step 1).
3. Small conflicts of two files each: 8d130001, 652c0001, a1a30001, 38870001 (class c, clean merge,
   after a human reads why it failed).
4. Medium conflicts: 14310001, 18980001, 33160001, 6e980001, 71660001, 13f00001.
5. Large and old, one at a time, each its own rebase Automaton: 72d10109, 72d1017e, 90d60001,
   72bc0001 (packs), 7bd50001 (git hosts). These touch the shared files most; land one, rebase the
   next, never two in flight.
6. Class (b) and (d): record dropped with the reason in section 4, delete the branches with backup refs.
7. Class (c) 70900001: operator decision (question 5).

## 8. Questions for the operator

One line each: question, options, recommendation.

1. Land all 20 class (a) Automata? Options: all, all but some, re-review first. Recommend all, in the order
   in 7.3, after each passes the gate on the merged result.
2. Packs (72bc0001): rebase the branch or re-run it? Options: rebase, re-run on current main. Recommend
   rebase, as already decided; 106 new files merge cleanly in principle, only 40 files conflict.
3. Git hosts (7bd50001) is in review: finish the review then land, or hold? Recommend finish the review
   first; it is the largest unreviewed item.
4. Record classes (b) and (d) as dropped with the reasons above and delete the branches with backup refs?
   Recommend yes; recheck the 7 remaining files of b20f0001 first.
5. Configuration parity audit (70900001, blocked): land the report, re-plan, or drop? Recommend drop the
   report if the gate on main already enforces parity; otherwise land.
6. Should archiving be refused outright while the branch is ahead, or warn? Options: refuse, warn with
   confirm. Recommend refuse, with explicit keep and drop actions as the only exits.
7. Merge-pending mode: when to move from shadow to on? Recommend after seven days of shadow decisions that
   show no false positives, keeping the 30-minute alert.
8. May the daemon merge into main without you? Options: never, auto for clean, gate-green, review-passed
   patch and minor changes with major always asking (matches the current release approval), always auto.
   Recommend the middle option.
9. Should a record with a branch only count as completed after it is landed, kept or dropped? Recommend
   yes; it removes the whole class of problem rather than guarding it.
10. Should the automatic rebase stay local (no push, no force)? Recommend yes, local only.
11. Keep task sub-branches after the parent lands? Recommend delete with a backup ref, restorable.
12. Priority against the other core work: the leak fix (steps 1 to 3) first, drain next, automation
    (steps 4, 5) after. Recommend that order; the rebase queue is the most expensive item and is not on
    the critical path for stopping the loss.

## 9. Verification of this document

- Counts reproduce with `python3 scripts/branch-audit.py` (156/0/12/14 at the base commit) and with the
  per-branch commands in section 2.
- Classes (a) and (b) rest on the file-level evidence in 4.1 and 4.2; the feature greps on main
  returned no match for the packs, git hosts, finaliser, deploy package, route-at-admission, review
  reserve, gate repair, inbox, trace retention and toolchain check.
- Open caveat: 7bd50001, 70900001 and b20f0001 overlap partly with work that reached main by other
  routes (git host observability and fake-host CI test, configuration gate, verifier cap files). The
  numbers above treat them conservatively; a rebase will show the exact overlap.
