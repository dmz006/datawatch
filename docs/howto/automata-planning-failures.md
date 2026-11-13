---
docs:
  index: true
  topics: [autonomous, automata, planning, planning failure, spec check, spec lint, self-correction, fixed release number, tracker id, private address, worktree path, housekeeping, repairs, question, alert, troubleshooting]
exec_steps:
  - tool: autonomous_prd_list
    description: List the Automata; a row with a planning_failure object failed to plan
    args: {}
    read_only: true
  - tool: housekeeping_repairs
    description: Read the planning repair group, with what it would do or did
    args: {}
    read_only: true
---

# How-to: Planning failures, the spec check and self-correction

A draft Automaton should never sit with zero stories and no explanation.
This page covers the three parts that prevent that: the **spec check** that
warns you about risky wording when you create or revise an Automaton, the
**self-correction** the daemon makes on its own when the only problem is a
fixed release number, and the **planning failure** record, alert and
question that show you what is left. For the decompose rule pack and the
standard tasks, see [automaton-decompose-lint.md](automaton-decompose-lint.md);
for the wider planning flow, see [autonomous-planning.md](autonomous-planning.md).

## The spec check on create and revise

When you create an Automaton, or ask for a revision of one, the daemon lints
the spec text and returns the findings with the reply. It is a warning only:
it never blocks the create or the revision, and a clean spec returns an empty
list. It uses no model and touches no session.

Each finding has:

| Field | Meaning |
|---|---|
| `rule` | Which check fired (table below) |
| `phrase` | The exact text in your spec that tripped it |
| `severity` | `warning` (the daemon can reword it itself) or `error` (only you can fix it) |
| `correctable` | `true` when the daemon can reword it |
| `suggestion` | The suggested rewording |

On a revision the text checked is the Automaton's spec plus the revision note.

### What is flagged, and why

The planner copies spec wording into task titles and task text, and the
decompose then rejects some of it. The spec check finds those phrases first.

| Rule | Flagged phrase | Severity | Suggested rewording | Why |
|---|---|---|---|---|
| `release-version-fixed` | a fixed three-part release number in release-flavoured text (for example a "version" followed by a number) | warning | "the next patch above main" (or "the release" when the word "version" or "release" introduced it) | A release number fixed in a task goes stale. The version is chosen at merge time as the next patch above main. |
| `names_tracker_id` | a tracker id | warning | remove it and describe the work itself | A task that names a tracking item by id means nothing to the worker and leaks into docs. |
| `system-binary-path` | an absolute path to a system binary | warning | the command name, resolved at runtime with `command -v` | A real system path differs per machine. |
| `private-address` | a private, loopback or link-local IPv4 address | error | a configured host name or "the target host" | A task must not carry an address that belongs to one network. |
| `worktree-path` | a path into a task worktree directory | error | a repo-relative path | A task never names its own worktree directory, and a home path leaks an operator's directory. |

A finding reads the same on every surface:

```
warning [release-version-fixed] "<the word version and the number>": replace '<the word version and the number>' with 'the release'; the version is taken at merge time as the next patch above main (the daemon can fix this wording itself)
```

### Where the spec check is shown

| Surface | Create | Revise |
|---|---|---|
| REST | `POST /api/autonomous/prds` reply carries a `spec_check` array | `POST /api/autonomous/prds/{id}/request_revision` reply carries a `spec_check` array |
| MCP | `autonomous_prd_create`: the JSON reply plus a second text item with the lines | `autonomous_prd_request_revision`: the same |
| CLI | `datawatch autonomous prd-create <spec>` prints `spec check: ...` lines after the reply | `datawatch autonomous prd-request-revision <id> --note ...` does the same |
| Chat | `autonomous create <spec>` appends "Spec check: n finding(s)" and the lines | `autonomous request-revision <id> [note]` does the same |
| Web form | The new-Automaton form shows a **Spec check** block under the form after you create | The block also shows after you ask for a revision; close it with the × |

```sh
datawatch autonomous prd-create "Ship the change as <the word version, then a three-part number> and call /usr/local/bin/tool"
# prints the new Automaton, then:
# spec check: warning [release-version-fixed] "<the word version and the number>": ...
# spec check: warning [system-binary-path] "/usr/local/bin/tool": ...
```

## What the daemon fixes by itself

When a plan comes back from the planner and the **only** content failure is a
fixed release number (`release-version-fixed`), the daemon does not send the
plan back. It rewrites each fixed number in a release story's title or
description, or a release task's title or text:

- a number introduced by "version" or "release" is replaced together with that
  word by "the release";
- a bare number becomes "the next patch above main".

It does this only when every other plan check passes on the corrected plan.
Each change is written to the Automaton's decision log as one `auto_correct`
line listing every replacement as `old phrase -> new phrase` with its story and
task, and the recorded failure is cleared as a success clears it. The version
itself is still chosen at merge time (see
[merge-pipeline.md](merge-pipeline.md#how-the-release-version-is-chosen)).

**What stays a failure.** Everything else takes the ordinary path (retry within
the budget, then a recorded failure): a tracker id, a system path, a private
address, a worktree path, any other plan check, or a release number in a plan
that also fails another check. A private address or a worktree path is never
reworded by the daemon, because only you know what should replace it.

## Where a planning failure is shown

When planning gives up, the Automaton records one planning failure:

| Field | Meaning |
|---|---|
| `reason` | What went wrong, in words |
| `category` | The failure category (for example `content`) |
| `source` | The rule that tripped, using the same names as the spec check |
| `phrases` | The exact offending phrases (at most 20) |
| `retryable` | Whether retrying may clear it |
| `retry_counts` | Retries spent per category |
| `retry_budget` | The retry budget of this failure's category |
| `occurred_at` | When it happened |

An Automaton with no planning failure has no `planning_failure` object.

| Surface | Where |
|---|---|
| Web detail view | A **Planning failed** block: reason, category, rule, phrases and retries against the budget, with the pending question under it |
| Web list | A red **Planning failed** badge on the Automaton's row; hover for the reason |
| REST | `planning_failure` on `GET /api/autonomous/prds/{id}` and on each row of `GET /api/autonomous/prds` |
| MCP | `autonomous_prd_get` and `autonomous_prd_list` return the same object |
| CLI | `datawatch autonomous prd-get <id>` prints a **Planning failed** section (reason, category, rule, phrases, retries); `datawatch autonomous prd-list` prints one `planning failed:` line per failed Automaton |
| Chat | `autonomous get <id>` adds a "Planning failed: ..." line with the phrases; `autonomous list` adds one line per failed Automaton |

A retry of a content failure with the same spec reproduces it, so do not simply
plan again: reword the phrases the record names.

## The alert and the question

A **non-retryable** failure raises **one alert**: a warning titled "Automaton
could not be planned: <title>" whose body gives the category, the rule, the
reason and the phrases. It is raised once per recorded failure; a daemon
restart does not raise it again. A retryable failure raises nothing until its
budget is spent.

When the daemon can reword **every** spec check finding, it also raises **one
question** on the ordinary question store, asked by `planning` for that
Automaton, with two options:

- **Fix the wording and plan again.** The spec is rewritten with the
  suggestions (a fixed release number becomes the neutral wording, a tracker id
  is removed, a system path becomes the command name), applied as a revision,
  and the Automaton is planned again in the background.
- **Leave it** (the default). Nothing changes; the failure stays on the
  Automaton.

If any finding cannot be reworded (a private address, a worktree path), there
is the alert and no question. The question is raised once per failure and not
again while an open question carries the same Automaton, rule and phrases. The
answer is recorded on the decision log as `planning_failure_answer`.

Answer it from the Automaton's detail view, from the Questions list, with the
`question_answer` MCP tool, or with `datawatch question answer <id> --option
fix_wording` (or `--option leave_it`).

## The planning housekeeping group

The housekeeping repair group `autonomous.housekeeping_repairs.planning` takes
the *Fix the wording and plan again* answer for you. It looks for a **draft**
Automaton with **no stories** whose recorded failure is **not retryable**.

| Mode | What it does |
|---|---|
| `report` (default) | Records what it would do; changes nothing |
| `on` | After the grace period, if every spec check finding can be reworded, rewords the spec, applies it as a revision and plans again; if one cannot, it skips the draft as unsafe, raises one alert and leaves the spec alone |
| `off` | Does nothing for this group |

The grace is `autonomous.housekeeping_repairs.grace_seconds` (default 300).
Read what the group would do or did with `housekeeping_repairs` (MCP),
`datawatch autonomous housekeeping-repairs`, `autonomous housekeeping-repairs`
(chat) or `GET /api/autonomous/housekeeping-repairs`. Turn it to `on` only if
you accept the daemon rewording a spec without you; undo by editing the spec
back from the detail view. See
[automaton-housekeeping-repairs.md](automaton-housekeeping-repairs.md#planning-repair).

## Troubleshooting

**The Automaton is a draft with zero stories.** Open the detail view or run
`datawatch autonomous prd-get <id>` and read the **Planning failed** block. If
there is none, planning has not failed: check that it is still running.

**The failure names a phrase but I do not see it in the spec.** The phrase may
have come from the planner's own wording of a task. Reword the spec so the
planner has no reason to repeat it, then plan again.

**There is an alert but no question.** The failure is not one the daemon can
reword (a private address, a worktree path, or a check other than the spec
check). Edit the spec yourself and ask for a revision.

**I chose Fix the wording and nothing changed.** The spec had nothing the check
could reword; the refusal is on the decision log. Edit the spec by hand.

**The spec check showed a finding but the Automaton was created.** That is
intended: the spec check is a warning and never blocks.

**The same failure repeats after I plan again.** A content failure is
reproduced by the same spec. Reword the named phrases first.

**The planning group did nothing.** Check its mode: `report` only records. Check
the draft has no stories and a non-retryable failure, and that the grace has
passed.

## Operator review

Things this change leaves for you to decide or confirm:

- The planning housekeeping group is `report` by default. Decide whether to set
  it to `on`, which lets the daemon reword a spec without asking you.
- The Android and iPhone apps do not yet show the **Planning failed** block or
  the spec check; parity is tracked in datawatch-app.
- The spec check never blocks. If your process needs a hard stop on a private
  address or a worktree path, that is not provided.
- The release-number self-correction rewrites the stored plan, not your spec.
  Read the `auto_correct` line on the decision log the first few times.

## Related

- [Decompose lint](automaton-decompose-lint.md): the rule pack, the planner's never-put list and section 6a
- [Autonomous planning](autonomous-planning.md): the create, plan, review and run loop
- [Housekeeping repairs](automaton-housekeeping-repairs.md): every repair group and its modes
- [Merge pipeline](merge-pipeline.md): how the release version is chosen
