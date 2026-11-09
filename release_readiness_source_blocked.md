# Blocked: adapt comparison records into release readiness

**Task:** add `internal/autonomous/release_readiness_source.go` with
`ReleaseReadinessRecordsFromComparisons`, mapping the shadow merge
comparison records into `[]ReleaseComparisonRecord`, plus one test per
mapped field.

**Resolution: blocked on the shadow merge comparison work (PRD
`d2f10001`).** The comparison record type and its store do not exist in
this worktree's `HEAD`, and per the task spec the correct action is to
stop and report blocked rather than invent a store.

## What was established

1. The release readiness scaffolding this task builds on **is** in the
   tree: `internal/autonomous/release_readiness.go` defines
   `ReleaseComparisonRecord` (ID, Size, PlanEqualOrBetter,
   HasConflicts, HasPWAChanges, BlockingDefect), the readiness bar and
   `ComputeReleaseReadiness`, with its test file. That is the work of the
   two preceding tasks (readiness bar/computation, one unit test per
   readiness condition), both already merged into this worktree.

2. The **shadow merge comparison record** the task says to map from does
   not exist anywhere in `HEAD`. Exhaustive greps confirm it:

   - `type .*Comparison` in `internal/autonomous`: only
     `SupervisorWatchComparison` (the supervisor watch oracle
     cross-check — no release size, verdict, conflicts, PWA or defect
     fields) and `SupervisorWatchComparisonView`.
   - `ReleaseComparisonRecord`: defined and used only in
     `release_readiness.go` and its test.
   - `EqualOrBetter` / `HasPWA` / `PWAChange`: no matches outside
     `release_readiness*`.
   - The only "shadow" record stores in this tree's `internal/autonomous`
     are the merge-**pending** shadow decisions
     (`MergeShadowDecision` + `mergeShadowRecords`,
     `merge_shadow_record.go`, `merge_shadow_decisions.json`) and the
     supervisor watch comparisons. The merge-shadow record carries
     state / commits-ahead / would-alert only — none of the five fields
     the task maps (release size, equal-or-better verdict, conflict
     count, PWA-change flag, blocking defect) exist on it, so it is not
     the described source either.
   - There is no `internal/mergeshadow` package in this tree (no
     `mergeshadow` import anywhere under `.`).

3. The described source exists **only on the unmerged branch
   `automaton/d2f10001`** (verified with `git grep` on that ref; the tip
   commit is **not** an ancestor of this worktree's `HEAD`):

   - `internal/mergeshadow/compare.go` — `Comparison` (AutomatonID,
     `ReleaseSize`, Matched/Different/NotCompared,
     `Differences []Difference`), `Difference` with `Severity` (major /
     minor), `Compare(plan, landed, parityIssuePresent, manualGates)`.
   - `internal/mergeshadow/compare_store.go` — `SaveComparison` /
     `LoadComparison` under `<dataDir>/merge-comparisons/<id>.json`.
   - `internal/mergeshadow/defect_candidate.go` — `DefectCandidate`
     (a `Difference` kept as a defect-queue candidate, `Severity`,
     `ReleaseSize`).
   - `internal/mergeshadow/plan.go` — `MergePlan` with
     `Conflicts []Conflict`; `internal/mergeshadow/derive.go` — PWA
     change detection (`genartifacts.PWAChanged`,
     `ParityIssueNeeded`).
   - The autonomy handover plan
     (`docs/plans/2026-11-03-autonomy-handover-plan.md`, the Phase 4
     order) lists `d2f10001` — "shadow merge and comparison with the
     replay tests" — explicitly **before** `b3ed0001` — "release
     readiness and the audit of mode changes".

4. Even once `d2f10001` lands, two of the task's five mapping rules do
   not read straight off a single comparison record: `HasConflicts`
   (conflict count > 0) comes from the **plan** (`MergePlan.Conflicts`),
   and `HasPWAChanges` (PWA-change flag) comes from the **derivation**
   (`ParityIssueNeeded`) — the `Comparison` itself carries only
   `ReleaseSize`, the matched/different verdict and the
   severity-graded differences. The adapter will need the plan, the
   comparison and the defect candidates joined per Automaton, not just
   one stored record. This is a spec ambiguity worth tightening when the
   task is re-run against the dependency: "equal-or-better verdict" is
   reasonably `Different == 0`, "blocking defect" is reasonably a
   major-severity difference/candidate, but "conflict count" and the
   "PWA-change flag" are plan- and derive-side fields.

## Why this is the right call, not a failure of approach

The task's own stop rule applies verbatim: "If no comparison record
store exists in the tree, stop and report this task as blocked on the
shadow merge comparison work; do not invent a store." Writing an adapter
against a fabricated `mergeshadow`-shaped type would compile against
nothing in this tree, and copying the unmerged `d2f10001` package into
this worktree would be merging another PRD's unfinished work without the
supervisor's merge ordering — that is a release-management decision, not
a task-level one.

## What unblocks it

1. Land PRD `d2f10001` (shadow merge and comparison) into `main` — it is
   the declared predecessor in the handover plan's build order.
2. Re-run this task in a worktree whose base already contains
   `internal/mergeshadow`; the adapter and its one-test-per-field test
   then have a real store to read from.
3. While that is open, have the decomposer confirm which stored object
   carries each of the five fields (comparison vs plan vs defect
   candidate) so the mapping rules in the task text match the landed
   types.
