# Parity issue draft — merge shadow: the plan's new findings and gate vocabulary (Phase 4, part of finishing review and merge by the daemon)

**Not filed. Not yet fileable as its own issue — this vocabulary belongs in
the open shadow drafts that will be filed.** This is a file-write-only task
(template: `parity_issue_draft`): nothing below runs a hosting CLI (no `gh`),
files nothing on any tracker, and creates nothing on any host. The sibling
drafts `shadow-merge-plan-parity.md` (the plan record itself),
`merge-shadow-read-parity.md` (the five read-only surfaces) and
`merge-shadow-settings-parity.md` (the three settings) are still open; this
draft records what this step added to the record they already contract, so the
vocabulary is not left out of the issue the clients eventually get.

**Corrected 2026-11-25** to match the main branch: there is no load-based gate
status (a test gate that fails and then passes on rerun is `intermittent`), the
replay files named below are the ones in the tree, and the finding codes are
split into those a stored plan carries today and those it does not.

## Parent work

"Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)." Operator, 2026-11-07, quoted: "b finsih now we need to be
using it to debug it and monitor its right." The step covered by this draft
added the plan's own checks and the gate-side disciplines, beside the shadow
merge plan the earlier step landed: the unmerged-task-branch check
(`internal/mergeshadow/checks_unmerged_task.go`, replayed by
`replay_unmerged_task_branch_test.go` and
`testdata/mergeshadow/unmerged-task-branch/scenario.json`), the stray-root-file
check (`root_hygiene.go`, replayed by `replay_root_stray_file_test.go` and
`testdata/mergeshadow/root-stray-file/scenario.json`), the failed test gate
rerun (`gate_rerun.go`, replayed by `replay_intermittent_failure_test.go` and
`testdata/mergeshadow/intermittent-failure/fake-test.sh`), the lint version pin
(`lint_pin.go`, replayed by `replay_lint_version_pin_test.go` and
`testdata/mergeshadow/lint-version-pin/ci.yaml`), and the broken-plan-link
replay (`replay_broken_plan_link_test.go` with
`testdata/mergeshadow/broken-plan-link/docs/plans/README.md`). Each is the
replay of an event the phase's first tests meet again: a task branch never
merged back, a stray file riding into the root, a test that fails once and then
passes, a lint that differs between local and CI, and a plan registry row that
links to a missing plan. The operator-visible record is the "The plan's checks
and gates" section of `docs/howto/merge-shadow.md`, the "What you still review"
list on the same page, and the four entries in
`docs/datawatch-definitions.md` (Unmerged task branch check, Stray root file
check, Intermittent gate, Lint version pin).

## Affected area

The findings-and-gates vocabulary of the shadow merge plan record — the part of
the contract the Android and iPhone clients will parse when they render a
stored plan or its read-only surfaces: REST `GET /api/autonomous/merge/shadow`
(the stored plans, with each finding's code and blocking flag and each gate's
status and detail), the `autonomous_merge_shadow_status` MCP tool, the CLI
`autonomous merge-shadow status` and `compare <automaton-id>`, the comm-verb
summary, and the PWA **Merge shadow** card — which shows the comparison, not
the plan, and therefore renders none of this vocabulary at all today. What
this step added to the record's vocabulary: three new blocking finding codes —
`unmerged_task_branch` (an Automaton task branch named after the work branch
holding commits the work branch does not), `stray_root_file` (a file the branch
adds at the repository root that the base ref does not track), and
`lint_version_mismatch` (the local golangci-lint differing from the version the
trial merge's CI workflow pins). The rerun of a failed `test` gate adds one gate
status, `intermittent` (the gate failed first and passed on rerun: not a
failure; the gate's detail reads `intermittent: passed on rerun` and carries the
names of the failing tests, when the output gives them, and the first five lines
of the failing run). A client will meet the gate statuses `pass`, `fail`,
`timeout`, `skipped`, `intermittent`, and no other. The finding codes a stored
plan carries today are `trial_merge_failed`, `conflict_unresolved`, `gate_fail`,
`gate_timeout`, `changelog_missing` and `unmerged_task_branch`. Four more codes
are defined but do not reach the findings of a plan the daemon stores today:
`stray_root_file` (the plan run does not call that check), `review_stale` (a
recorded review verdict that does not name the branch head; the daemon passes no
verdict into the plan run), and `lint_version_mismatch` and
`lint_tool_unavailable` (they are set on the lint gate's result, which the
stored plan reduces to status, duration and detail, so the plan shows a failed
`lint` gate and a `gate_fail` finding). Also in
scope for the contract: a plan blocked by `unmerged_task_branch` records the
trial merge and *every gate as skipped with the reason*, and the
`check-plans-links` gate's output reports only the count of broken plan targets
— the checker never names them. What is *not* in scope: no new settings, no new
locale keys, no new endpoint, no new affordance — the three
`autonomous.merge_shadow.*` settings and the seven PWA locale keys are the
earlier step's contract (`merge-shadow-settings-parity.md`), and the plan
remains a record: `applied` stuck at `false`, no apply, no approval, no ack.

## Current behavior

Server side, within the shadow bound: the unmerged-task-branch check runs
before the trial merge, the failed-test-gate rerun and the lint version pin run
inside the gates, and a gate that fails lands as a gate failure — recorded in
the stored plan, read from the five surfaces above, and documented in the howto
and the definitions entries. The stray-root-file check exists and has its
replay, but the plan run does not call it today, and the trial merge runs the
build, lint and test gates only (`check-plans-links` is not one of them).
Nothing is touched: the trial worktree stays
throwaway, uncommitted and removed, and no gate outcome blocks, retries or
fails the Automaton. Client side, nothing is mirrored yet, so the gap is
latent rather than live: Android and iPhone do not read the shadow state, do
not render a Merge shadow card, and the PWA card itself shows only the
comparison. The moment a client renders a stored plan — the pending
`shadow-merge-plan-parity.md` / `merge-shadow-read-parity.md` issue — three
misreads are waiting for it. First, a client that models gates as
`pass`/`fail`/`timeout`/`skipped` will classify `intermittent` as an unknown
or a failing status, when it is deliberately *non-blocking*: the test gate
failed, passed on rerun, and the daemon filed a defect record for the operator —
a real defect to fix, but not a failed gate. Second, a plan blocked by
`unmerged_task_branch` arrives with every gate recorded `skipped` — a client
that counts `skipped` as "gates did not run, nothing to show" hides exactly the
blocking finding that explains the skips, and one that treats them as evidence
overstates the run. Third, findings are not gate rows: each carries a code, a
blocking flag and the daemon's own message, and a client that checks codes
against a fixed list will misread any code added later. None of this is wrong
in the record; what is missing is the client-side vocabulary, which today simply
does not exist anywhere.

## Expected behavior

Capability parity, not implementation parity, and only inside the read-only
bound: a client that renders a stored plan renders the full gate status set —
`pass`, `fail`, `timeout`, `skipped`, `intermittent` — with
`intermittent` presented as passed on rerun (the gate detail's failing lines
shown, explicitly not a failed gate) rather than a failure or an unknown; renders findings by their
code and blocking flag as free text with the daemon's own message, not against
a hard-coded known-code list, so a new code reads correctly without a client
change each time one lands; and reads a plan whose gates are all
`skipped` as "blocked before the trial merge — the finding says why," not as
an empty gate run. A client that only shows the comparison — the PWA card's
present shape — has nothing new to do: the comparison vocabulary is unchanged,
and the card rule (hidden until enabled and at least one comparison) still
holds. What must *not* be expected of a client: any reaction to a finding —
no unmerge button for an unmerged task branch, no delete for a stray root
file, no "rerun the gate" for an `intermittent` gate, no lint-version fixer —
because the server never acts on any of them, the plan is a record not a
queue, and a client with those affordances would be ahead of the server. The
supervisor relationship is unchanged on every platform: the manual merge
remains the source of truth, and these findings are evidence the operator
reviews, per the howto's "What you still review" list.

## Suggested next step

Do not file this as a separate issue. Fold the vocabulary into the pending
`shadow-merge-plan-parity.md` and `merge-shadow-read-parity.md` drafts —
add the finding codes and the `intermittent` status to the affected-area lists, the three
misreads to the expected-behavior section, and a one-line acceptance that a
plan rendering treats `intermittent` as non-blocking and all-`skipped` gates
as the blocked-plan signal — so the single issue the operator files carries
the whole plan contract, including this step's codes. Keep this file as the
standing record of what the vocabulary is and why, and update it when the
approval modes per release size land (that step's draft must name any pending
plan state a client would render, because none exists today). Replace this
status line with the issue reference the day the operator files the merged
issue.

## Filing record

Not filed. Nothing has been run on a tracker and nothing was created on any
host: file write only, per the template and the session boundary.
