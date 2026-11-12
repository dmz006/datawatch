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

See also [Failure escalation](failure-escalation.md) and
[Automata capacity](automata-capacity.md). The decision record is
[the completion review plan](../plans/2026-11-04-completion-review-autofix.md).
