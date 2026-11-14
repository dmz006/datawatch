# Unlanded branch review, 2026-11-06

Operator request, quoted: "so yes c a one time review too need to get worktrees fixed" and "i'm looking forward to your
review of worktree hopefully some good work in there to use!!"

Scope: the 77 branches that `scripts/branch-audit.py` class "other" (not landed by subject, not a live record). First
pass: `git cherry` against main (patch-id): 76 of 77 have no commit equivalent on main, 1 has all of them. Second pass:
four read-only reviewers, 19 or 20 branches each, checking what the branch was for and whether main has the
capability. Their tables are in `docs/plans/reports/unlanded-branch-review/` (`result-chunk1..4.md`, plus the pass-1
data). Nothing was deleted or changed by the review.

## Result

| Class | Branches |
|---|---|
| SUPERSEDED (the capability is on main in another shape) | 9 |
| DROPPED (replaced by another design, with a registry row) | 3 |
| JUNK (smoke or scratch files) | 4 |
| USEFUL-UNLANDED | 61 |

The 61 are not lost work. They fall into eight feature families that are the Automata in flight right now, plus three
small orphans. Most of the 61 are per-task sub-branches that are ancestors of their parent `automaton/<id>` branch,
so the parent already carries the same commits and the sub-branch is a redundant copy.

| Family | State | Note |
|---|---|---|
| `61c60001` archive grace for cancelled Automata | running | not on main; applies almost cleanly; fixes the loop-archive defect (row 363); land first |
| `c4880001` read-only task recovery | running | applies easily (74 behind); unblocks `718a0001` and `a12f0001` |
| `718a0001` measured verifier headroom | failed on a wedged gate | about 4,450 lines; 94 behind; remove the stray completion marker file before landing |
| `a12f0001` sessions tiles, grouping, saved views | failed on a wedged gate | about 4,000 lines; 1,044 behind; heavy `app.js` conflicts, needs a port |
| `6aca0001` protected paths and rules from the base commit | running | about 10,900 lines in 62 files; 1,709 behind; needs a port at merge |
| `ee660001` decision records | blocked, repaired today | about 11,900 lines; 1,756 behind; see the finding below |
| `ec310001` observer pull for federated peers | blocked by the rules review | no registry row names it: the operator should confirm it is wanted |
| `d5d10001` pending items registry | blocked | 4 commits, about 2,400 lines; row 361 says it was meant to run again |

Small orphans worth recovering (not part of a live family):

- `4e190001`: the question-raise notification wait (`questionNotifyGate`, `WaitQuestionNotifies`, about 130 lines and a
  test) is not on main; the rest of that deflake round is. Port that part.
- `42950001--tasks-auto-fix-rules-review-findings-attempt-1-f7900001`: a `.In(time.Local)` zone fix in
  `usage_backoff_wiring.go` plus three test files, matching the row 302 follow-up. Cherry-pick the code and tests.
- `dc6c597b`: the `autonomous:task:` session-name prefix (about 36 lines). Registry row 6 says it is fixed, but main
  still builds the name as `"autonomous:" + req.Title`.

Safe to delete once the operator agrees (nothing useful in them): the 9 superseded (`28510001`, `68c40001`, `2713dbb9`,
`597a5120`, `da660001`, `6ac20001`, `3ebd0001`, `5154c783`, `77190001`), the 3 dropped (`85d00001`, `1f850001`, and
`b20f0001` once `718a0001` lands) and the 4 junk (`47c7eb15`, `f07d6046`, `3b518952`, `e6443290`). The redundant
sub-branches of a live parent go when the parent merges, not before.

## What the review found beyond the list

1. **A task-merge defect (registry row 375).** `ee660001`'s seven completed task branches were never merged into the
   Automaton branch: a stale uncommitted edit in the Automaton worktree made the merge refuse, silently, from
   2026-11-05 21:11 on. The how-to task failed three times because the code it had to document was not in its tree.
   Repaired by hand today (stale edit saved to scratch, seven merges clean, build passes, task retried).
   A scan of every live Automaton found no other stuck merge except `ec310001` task `746c0001` (pending, worktree
   clean), still to look at.
2. **Two long-lived Automata are far behind main** (`6aca0001` 1,709 and `ee660001` 1,756 commits). Their final merge
   will need a merge helper and probably a port of `openapi.yaml` and `app.js`.
3. **The cleanup is not at fault** (see the redesign doc): the gap is that nothing resolves archived-but-unlanded work.
   The Housekeeping "Unlanded work" action list (decision 6) is built from this review.

## Cleanup done 2026-11-06 10:18 (operator approved: "a - yes delete and recover the 3")

Operator answer, quoted: "a - yes delete and recover the 3" (option A of question 8).

- Deleted 15 branches and their worktrees (all had an archived record, none live): the 9 superseded (`28510001`,
  `68c40001`, `2713dbb9`, `597a5120`, `da660001`, `6ac20001`, `3ebd0001`, `5154c783`, `77190001`), the dropped `85d00001`
  and `1f850001`, and the 4 junk (`47c7eb15`, `f07d6046`, `3b518952`, `e6443290`). `b20f0001` is kept until `718a0001`
  lands.
- Safety copies in `supervisor-scratch/review/`: `deleted-branches-2026-11-06.bundle` (every deleted branch, verified)
  and, for `28510001`, whose worktree held uncommitted edits (a 17-line diff and three drift-mode test files) that were
  already superseded: `28510001-uncommitted.patch` and `28510001-untracked-tests.tgz`.
- Recovery of the three orphans (`42950001`, `dc6c597b`, `4e190001`) is with a merge helper; the result is recorded in
  the registry when it ships.
- The remaining branches are the sub-branches of live Automata and wait for their parents to merge.
