---
docs:
  index: true
  topics: [prd, autonomous, review, rules, auto-fix, attention, needs_review]
exec_steps:
  - tool: autonomous_attention_list
    description: List the Automata that need attention; an item held by the completion review carries rules_review_outcome (action, findings, retries)
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including auto_fix_retries (how many automatic fix tasks each Automaton gets)
    args: {}
    read_only: true
---

# Automata completion review: automatic fix and hold

When an Automaton's work is done, the **completion review** judges it
against the operator's rules before it is reported finished. This
how-to is for operators and supervisors: what happens when the review
blocks, where to see it, and how to fix it.

## What the completion review is

It is the last check on an Automaton. It reads the finished work and
reports findings: the problems it found. A block whose problem is with
the work itself (a missing test, a rule not met, a doc not updated) is
a **content** block. Content blocks get an automatic fix. Other
categories (for example an infrastructure fault) stay blocked and are
handled by their own rules.

## The three outcomes

1. **A fix task is created and runs.** The findings become one
   corrective task. It is added to the Automaton, approved and run. When
   it finishes the completion review runs again. If it passes, the
   Automaton completes and the outcome is cleared.
2. **The fix task could not be created.** The Automaton is set to
   `needs_review` (not blocked, not running). The reason is written to
   the decisions (`autonomous_decisions`). No retry is used up, because
   a retry is counted only after the fix task exists.
3. **The retries are spent.** The review still blocks after
   `autonomous.auto_fix_retries` fix tasks, or auto-fix is off. The
   Automaton is set to `needs_review` with the findings attached.

Holding in `needs_review` (instead of blocking) keeps the work lanes
filling: a held Automaton is not picked as runnable, and other Automata
keep starting. The total retry ceiling
(`autonomous.retry_ceiling_total`) applies on top.

## Where to see it

An Automaton held by the completion review carries
`rules_review_outcome` with `action`, `findings` and the retries spent
of the budget.

- **Web:** the attention list, and the Automaton view, show the action
  and the findings.
- **REST:** `GET /api/autonomous/attention` (and
  `GET /api/autonomous/prds/{id}`).
- **MCP:** `autonomous_attention_list`.
- **CLI:** `datawatch autonomous attention` prints an action line
  (`fix task created`, `fix task failed`, `retries spent` or
  `auto-fix off`) and each finding as a bullet.
- **Comm channel:** `autonomous attention` prints the same; up to five
  findings and `+N more`.

## How to fix it

1. Read the findings from any surface above.
2. Add a task that addresses them with the normal add-task call
   (`autonomous_prd_add_task`, `POST /api/autonomous/prds/{id}/add_task`,
   or the Add task button). A parked Automaton accepts it.
3. Approve the task (`autonomous_prd_approve_task`).
4. Run the Automaton. When the task is done, the completion review runs
   again. A pass clears the outcome; a new block holds it again with
   the new findings.

This manual path does not use the retry budget.

## Change the retry count

`autonomous.auto_fix_retries` is the number of automatic fix tasks per
Automaton. The default is `1`. Set it to `0` to turn auto-fix off: the
Automaton is then held in `needs_review` at the first block.

| Method | How |
|---|---|
| YAML | `config.yaml` → `autonomous.auto_fix_retries: 2` |
| CLI | `datawatch config set autonomous.auto_fix_retries 2` |
| Web UI | Settings → Automata → auto-fix retries |
| REST | `PUT /api/config` with `{"autonomous.auto_fix_retries": 2}` |
| Comm channel | `configure autonomous.auto_fix_retries=2` |

MCP: `autonomous_config_set` with `auto_fix_retries: 2`.

## Finished, not merged: the re-review that clears a stale block

A finished Automaton can end with an open completion review while its work
branch still carries commits the default branch does not have: no verdict on
record yet, the last verdict a block, or a pass judged on an older review
head (a stale pass after the work moved on). Every read derives
`merge_state` `finished_not_merged` for it — the amber "Branch not merged"
badge in the list and detail views, the Projects cards and the Gantt row —
and the archive guard holds it there: the supervisor loop does not archive a
completed Automaton while its work is unmerged (a rebased or squashed
landing counts), an automated, non-operator archive of it is refused naming
the branch and the commits it still carries, and your own archive is always
honored as the explicit "abandon the work" decision. An Automaton that was
wrongly archived before that guard existed is restored by the one-time
[unarchive repair](automaton-housekeeping-repairs.md#unarchive-repair-one-time).

To settle the open review, the **re-review** (re-judgment) action re-runs
the completion rules review as a fresh judgment. It never resets an
Automaton — no task goes back to `pending`, no story reopens — and every
task must already be `completed` or `cancelled`, with at least one
`completed` (a cancelled task is skipped, not a blocker).

**Who it applies to.** A `blocked` Automaton, one held for review whose head
has moved, or a `completed` or `archived` Automaton in state
`finished_not_merged` whose review is still open to re-running. A finished
Automaton whose work is already on the default branch, or whose latest pass
covers the current review head, is refused — the refusal names the states
re-review applies to.

**What the verdict resolves.** A **pass** clears the stale block: a blocked
Automaton is lifted to `completed`, and a completed or archived Automaton
keeps its finished status and simply loses the stale block. A **block**
flips the record back to `blocked` with the new reason. **No verdict** (the
judge failed or returned nothing) keeps the Automaton's status — a blocked
one keeps its previous blocked reason — and an error is returned so you see
the re-review produced no judgment. In every case the outcome is written to
the Automaton's decision log before any status change.

**A fresh judgment, or a replay.** It is a fresh review whenever the review
head or the rules text the judge reads has changed since the stored verdict
— that is how the fix you committed by hand (or the auto-fix merge) gets
judged. The stored verdict is replayed as-is only when the head and the
rules text are both unchanged, the verdict recorded a rules fingerprint and
the per-commit verdict cache is on ([the per-commit verdict
cache](judge-policy.md#the-per-commit-verdict-cache)). Every re-review records a
`guardrail_rejudge` decision carrying the commit it judged
(`judged_commit_sha`) and whether the stored verdict was reused
(`verdict_reused`), so asking again and again leaves one verdict per review
that actually ran. The compact reply carries a plain-language `message`
saying which of the two it was.

**The auto-fix merge triggers the re-review on its own.** When the auto-fix
task above completes and merges back into the Automaton branch, the merge
itself runs exactly one re-review of the new head: a pass clears the stale
block (a finished record keeps its finished status) and a block re-blocks
with the new reason. You run no re-judge for that one; the decision log
shows it with the merge and the new verdict next to each other.

| Surface | Call |
|---|---|
| REST | `POST /api/autonomous/prds/{id}/rejudge` — with `?view=compact` the reply carries the plain-language `message` |
| MCP | `autonomous_prd_rejudge(id)` — the same compact reply |
| CLI | `datawatch autonomous prd-rejudge <id>` (alias `datawatch autonomous rejudge <id>`; `--backend <name>` pins the judgment to that judge backend, the Reroute form) |
| Comm | `autonomous rejudge <id> [backend=<name>]` — the same reply from the messaging channel |
| Web app | The **Re-review** button on the Automaton's detail header (shown for `blocked`, `completed` and `archived` records) sends the same request and shows the same `message` in a six-second notice |

Reading the verdict afterwards is the same as for a block: the latest entry
in the Automaton's verdict list on its detail page (or in
`autonomous_prd_get`) with its issues, and the `guardrail_rejudge` /
`guardrail_verdict` entries in its decision log. The full reference for the
re-review, including the fingerprint and replay rules, is in
[Re-judging the completion guardrail](autonomous-recovery.md#re-judging-the-completion-guardrail-rejudge).

See also [Failure escalation](failure-escalation.md) and
[Automata capacity](automata-capacity.md). The decision record is
[the completion review plan](../plans/2026-11-04-completion-review-autofix.md).
