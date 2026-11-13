---
docs:
  index: true
  topics: [automaton, verification script, verification scripts, final gate, allowed files, merge base, sync-docs, decompose lint, script exemption reason, edit task]
exec_params:
  - {name: prd_id, required: true, description: "The id of the Automaton whose plan you are checking or fixing"}
  - {name: task_id, required: false, description: "The id of the task whose verification script or exemption reason you are setting"}
  - {name: verification_script, required: false, description: "One repo-relative script file under .datawatch/verify/ (never a command string); leave empty to keep the current one"}
  - {name: verification_script_reason, required: false, description: "Why the task has no verification script; leave empty to keep the current one"}
exec_steps:
  - tool: autonomous_decompose_lint_dry_run
    description: Lint the Automaton's stored plan read-only with the current rule pack and see every finding, each naming its script and rule id — nothing is stored or re-decomposed
    args:
      id: "{{params.prd_id}}"
    read_only: true
  - tool: autonomous_decompose_lint_findings
    description: Read the last recorded decompose-lint pass for the Automaton (the findings the planner's own run produced)
    args:
      id: "{{params.prd_id}}"
    read_only: true
  - tool: autonomous_prd_get
    description: Read the Automaton to find the task id and see each task's current verification script and reason
    args:
      id: "{{params.prd_id}}"
    read_only: true
  - tool: autonomous_prd_edit_task
    description: Set one task's verification script, or the reason it has none; an omitted field stays unchanged
    args:
      id: "{{params.prd_id}}"
      task_id: "{{params.task_id}}"
      verification_script: "{{params.verification_script}}"
      verification_script_reason: "{{params.verification_script_reason}}"
    read_only: false
---

# How-to: write verification scripts that stay stable

A verification script is the one repo-relative script file that checks a task
(see [Verification script and exemption reason](../datawatch-definitions.md#verification-script-and-exemption-reason)).
The planner writes one under `.datawatch/verify/` for each file-changing task
when it plans an Automaton. A script that works the day it is written but
fails once other tasks of the same Automaton land is the commonest cause of a
task that fails for no reason in its own files. This guide says how to write
scripts that do not drift, what the decompose lint flags, and how to fix a
plan that has the problem.

## 1. The five habits

Every script the planner writes — and every one you write by hand — follows
these. The planner is given the same rules in its prompt.

1. **Run `make sync-docs` first, in any docs check.** A script that checks
   the docs, the embedded docs copy (`internal/server/web/docs`) or how-to
   coverage (`scripts/check-docs-sync.sh`, `scripts/check-howto-coverage.sh`)
   must run `make sync-docs` itself as its first step. Otherwise it compares
   against an embedded copy that is stale the moment a source doc changes.
2. **Use the cumulative allowed-file helper.** A script that checks "nothing
   outside the allowed files changed" must build its allowed list from
   everything the whole Automaton may touch — see
   [the cumulative allowed-file helper](#2-the-cumulative-allowed-file-helper).
   Never build it from one task's own file list.
3. **Compare against the merge base.** Compare against
   `git merge-base HEAD main`, never against the working tree, the current
   `main`, or one task's file list — see
   [section 3](#3-compare-against-the-merge-base).
4. **Never run a formatter over a whole folder.** Run `gofmt` (or the
   equivalent) only on the changed files the script lists. A package-wide
   run fails the task on an unrelated file that was already unformatted.
5. **One final gate for the Automaton, at the end.** Story-level checks are
   plain task verification; there is no per-story gate — see
   [section 4](#4-one-final-gate-per-automaton).

## 2. The cumulative allowed-file helper

The **cumulative allowed-file helper** is the shared piece a script sources
(or copies) to decide which changed files are acceptable. It allows, from the
start of the Automaton:

- every file any story of the Automaton may touch (the union of all tasks'
  planned files, not just the current task's);
- the files the standard tasks generate — the changelog draft, the parity
  issue draft and its hash file, the docs set, and the registry plan files;
- the Go files `gofmt` may rewrite, and the docs files.

Because the list is cumulative, a task that runs early and a task that runs
last both pass against the same list: an earlier task's output is never
"unexpected" for a later one. A helper that is sourced one level deep is read
by the lint together with the script, so putting the merge-base comparison or
the `make sync-docs` call in the helper is enough.

## 3. Compare against the merge base

List the files the Automaton changed so far with:

```bash
base="$(git merge-base HEAD main)"
{ git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u
```

That is the changed set since the branch left `main`, plus new untracked
files. Compare *that* against the cumulative allowed list. Three comparisons
look right and are not:

| Comparison | Why it drifts |
|---|---|
| a literal list equal to the task's own files | fails as soon as an earlier task adds a file that is not in this task's list |
| `git diff --name-only` / `git status` of the tree alone | depends on what has been committed so far, so the answer changes as tasks land |
| a diff against the current `main` | moves when `main` moves, so a clean branch starts to fail |

## 4. One final gate per Automaton

An Automaton has exactly **one** final gate: a task of kind `final_gate`, as
the **last** task of the **last** story, depending on every other task. The
standard-task pack appends it once there, and a planner-written `final_gate`
on any earlier story is rejected by the plan check at decompose time (and any
that remains is dropped when the standard tasks are applied). A story-level
check is plain task verification, not a gate.

The final gate **always carries a verification script or an explicit
exemption reason**: a **final gate exemption reason** is the stated reason a
final gate gives (in `verification_script_reason`) for having no script of its
own — for example "the closing check is the project's configured gate
command". With neither a script nor a reason (and no usable report path) the
plan is flagged by `final_gate_without_script`.

When the daemon runs the gate it resolves the checks in strict order: the
gate task's own script under `.datawatch/verify/`, else the project gate
command, else **every** other task's script across **every story** of the
Automaton, in story order, until the first failure. The details are in
[the script gate](automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session).

## 5. How the lint reports a finding, and how to fix each rule

Decompose lint reads each task's script from the project directory and names
the finding with **the script path and the rule id**, for example
`verify_script_task_files_only: .datawatch/verify/<name>.sh …`. A script that
does not exist yet (or is a command string, or lies outside the project) is
skipped by the three rules below; the warn rule
`unexecutable_verification_script` keeps covering it.

| Rule id | Severity | What it means | How to fix it |
|---|---|---|---|
| `verify_script_task_files_only` | block | The script compares only against the task's own files (a literal list equal to them, or a file-listing diff of HEAD, the tree or `main`) and never against the merge base. | Compare against `git merge-base HEAD main` — directly, or through a sourced helper — and compare the result with the cumulative allowed list. |
| `verify_script_docs_without_sync` | block | The script checks docs, the embedded docs copy or how-to coverage but never runs `make sync-docs`. | Add `make sync-docs` as the script's first step, or source a helper that runs it. |
| `final_gate_without_script` | block | A `final_gate` task has no verification script, no `verification_script_reason` and no usable report path. | Give the gate a script under `.datawatch/verify/`, or a report path under `.datawatch/reports/`, or state why it has none in `verification_script_reason`. |

All three are `block`-severity: in `enforce` mode they feed the planner a retry
with the findings as guidance; in `shadow` mode they are recorded on the
Automaton and nothing is retried. See
[the decompose-lint how-to](automaton-decompose-lint.md#3-the-rule-pack-that-ships)
for the whole rule pack.

## 6. Set a task's script or reason with the edit tools

On a task that is still editable (the Automaton is in `needs_review`,
`revisions_asked` or `cancelled`, or the task is failed, blocked or held on a
`blocked` Automaton; a completed task is immutable) you can set the script or
the exemption reason without a re-decompose. The script path must be one clean
repo-relative file — not a command string, an absolute path, a `..` path or the
project root — that already exists in the project; it is also added to the
task's generated files. An omitted field is left unchanged.

| Surface | How |
|---|---|
| REST | `POST /api/autonomous/prds/{id}/edit_task` with `{task_id, verification_script, verification_script_reason}` |
| MCP | `autonomous_prd_edit_task` with `verification_script` / `verification_script_reason` |
| CLI | `datawatch autonomous prd-edit-task <prd-id> --task <task-id> --verification-script <path> --verification-script-reason <text>` |
| Comm channel | `autonomous edit-task <prd> <task> verification_script=<path> verification_script_reason="<text>"` |
| PWA | the **Verification script** and **Script exemption reason** fields of the task editor |

A typical fix after a lint finding:

1. Run a dry run (`datawatch autonomous lint dry-run <prd-id>`) and read each
   finding's script and rule id.
2. Edit the script file in the project so it follows the habits above.
3. If the task needs no script, set `verification_script_reason` instead.
4. Dry-run again; the finding disappears when the rule no longer matches.

## See also

- [Decompose lint, standard tasks and guidance](automaton-decompose-lint.md)
- [Daemon task modes and the script gate](automaton-daemon-task-modes.md)
- [Verification script and exemption reason](../datawatch-definitions.md#verification-script-and-exemption-reason)
