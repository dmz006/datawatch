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

## 1. The nine habits

Every script the planner writes — and every one you write by hand — follows
these. The planner's prompt carries the first four as a shared helper text, and
the rest are enforced by the decompose-lint rules and the final-gate plan check
when the plan comes in — a finding feeds the planner a retry with the text to
fix.

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
6. **Exempt the generated artefacts from a file-set check.** The daemon
   writes the per-Automaton artefacts — the changelog, tracker-rows and
   registry-row drafts (`docs/plans/drafts/`), the parity issue and its hash
   file (`docs/plans/parity-issues/`) and the gate report
   (`docs/plans/reports/`) — into the cumulative tree, so a
   changed-file-set check must exempt those paths or compare against a
   cumulative list that already names them.
7. **Write the final-gate re-run into a whole-set claim.** A script that
   asserts a finite whole set of changed files must still pass when the
   final gate re-runs it after every standard task has landed: assert only
   the files this task adds, or guard the claim so it skips on the gate's
   re-run.
8. **Match flags against the real binary, not memory.** Every long option a
   script invokes (`command … --flag`) must be listed as a whole option in
   that command's own help (`command --help`); short flags and a command
   that does not resolve or answer a bounded help probe are out of scope.
9. **Anchor every heading and output-format assertion in the task spec.** A
   script that greps a markdown heading, or the gate phrases "Final answer"
   / "Verification summary", needs the task spec to state that heading or
   phrase — verbatim or in equivalent wording, the same words in any order: a
   "gates section" in the spec anchors a grep of
   `## Gates`, and a spec naming `## Gates` anchors a grep of `Gates`.
10. **Write bash, and run it with bash.** Start every script with
   `#!/usr/bin/env bash`. The daemon and the planner run the script with
   `bash`, so `[[ ]]`, arrays, process substitution and `set -o pipefail` are
   fine there; under an `sh` shebang, or when a spec line runs the script with
   `sh`, they break (`verify_script_sh_for_bash`).
11. **Keep one shared allowed-file list.** Build the allowed list once, from
   the task's own files plus the standard set (changelog, README, docs,
   locales, tests), cumulative across stories, or source the shared helper.
   A short literal list that leaves out the task's own files or the standard
   set rejects correct work (`verify_script_rejects_own_files`).
12. **No hex-colour patterns, and no pipe into `grep -q`.** A tracker-id
   pattern made of digits alone (`#[0-9]{3,}`) also matches CSS hex colours
   such as `#123456`, so put a tracker word before the `#` or require the
   match not to follow a `#` (`verify_script_hex_pattern`). `grep -q` exits
   at the first match and the producer gets SIGPIPE, which fails the pipeline
   under `pipefail`; capture the output first, then run `grep -q` on the
   variable or file (`verify_script_pipe_grep_q`).
13. **A story's gate script covers that story only.** A script that closes a
   story runs only that story's own scripts, never another story's. Only the
   single final gate runs the checks of the whole Automaton, and a script
   must not require output that only a task it does not depend on creates
   (`verify_script_needs_later_task_output`).

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
check is plain task verification, not a gate, and a script that closes a story
runs only that story's own scripts.

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
skipped by the rules below that read a script; the warn rule
`unexecutable_verification_script` keeps covering it.

| Rule id | Severity | What it means | How to fix it |
|---|---|---|---|
| `verify_script_task_files_only` | block | The script compares only against the task's own files (a literal list equal to them, or a file-listing diff of HEAD, the tree or `main`) and never against the merge base. | Compare against `git merge-base HEAD main` — directly, or through a sourced helper — and compare the result with the cumulative allowed list. |
| `verify_script_docs_without_sync` | block | The script checks docs, the embedded docs copy or how-to coverage but never runs `make sync-docs`. | Add `make sync-docs` as the script's first step, or source a helper that runs it. |
| `artefact_files_allowed` | block | Runs a changed-file-set check that does not exempt the generated artefacts the daemon writes per Automaton (the changelog draft, the parity issue draft and its hash file, the gate report) — the final gate re-runs the script against the cumulative tree, where exactly those files exist. | Exempt `docs/plans/drafts/`, `docs/plans/parity-issues/`, `docs/plans/reports/` and `CHANGELOG.md` from the unexpected-file computation — in the check itself or in the sourced helper. |
| `final_gate_artefact_allowed` | block | Asserts the whole changed-file set against a finite list with no guard for the final gate re-running it; the gate runs after every standard task has landed, so a finite whole-set claim can only fail. | Assert only the files this task adds, or guard the claim so it skips when the final gate re-runs the script. |
| `script_invokes_known_flag` | block | Invokes a command with a long flag (`--flag`) the command's own help does not list as a whole option — a flag invented from memory. | Check each command and flag against the real binary's help (`command --help`) before accepting the script; short flags and a command that does not resolve or answer a bounded help probe are out of scope. |
| `check_matches_spec_text` | block | Asserts a markdown heading, or the exact gate phrases "Final answer" / "Verification summary", that the task's own spec does not state, verbatim or in equivalent wording — the check is anchored to an assumption about output shape, not to the spec. | State the heading or phrase the script checks in the spec — the same words are enough, in any phrasing (a "gates section" in the spec anchors a grep of `## Gates`). |
| `verify_script_artefact_files` | block | The script flags changed files the task did not plan, without exempting `docs/plans/drafts/`, `docs/plans/parity-issues/`, `docs/plans/reports/` and `CHANGELOG.md` (stricter shape check than `artefact_files_allowed`). | Exempt those paths from the "unexpected files" check, or check only the files the task adds. |
| `verify_script_final_gate_rerun` | block | A script asserts the changed-file set against a finite allow-list with no guard the lint recognizes for the final gate rerunning it on the cumulative tree. | Guard it with an env var (`DATAWATCH_*`, `SKIP_*`, `FINAL*`), a `--final` argument, or a check limited to files the task adds. |
| `verify_script_unknown_flag` | block | The script passes a long flag to a command (or one of its subcommands) the script invokes that the command's own `--help` output does not list — a flag the installed binary does not have cannot be asserted. | Use only flags the command actually accepts (check with `<command> --help`), or rephrase the check so it does not depend on that option. Commands the environment cannot find are not checked. |
| `verify_script_text_not_in_spec` | block | The script greps or compares against a markdown heading (a `# …` line) or a multi-word output phrase the task spec does not state, verbatim or in equivalent wording. | State the heading or the format being checked in the task spec — the same words are enough, in any order. |
| `verify_script_rejects_own_files` | block | The allowed-file list is built only from literals that omit a file the task itself plans, or the standard allowed set (changelog, README, docs, locales, tests), so the script rejects the task's correct work. | List the task's own files and the standard set, or source the shared allowed list instead of a literal one. |
| `verify_script_needs_later_task_output` | block | The script requires a file or a phrase (a path test, a grep target) that only a task it does not depend on creates. | Add the `depends_on` edge to the creating task, or move the check into that task's own script. |
| `verify_script_sh_for_bash` | block | The script uses `[[ ]]`, arrays, process substitution or `pipefail` under an sh shebang, or a spec or script line runs a bash script with `sh`. | Use `#!/usr/bin/env bash` and run the script with `bash`. |
| `verify_script_pipe_grep_q` | block | The script pipes into `grep -q`; grep exits at the first match and the producer gets SIGPIPE, which fails the pipeline under `pipefail`. | Capture the output first (`out=$(cmd)`, then `grep -q pat <<<"$out"`), or drop `-q` and send the output to `/dev/null`. |
| `verify_script_hex_pattern` | block | A grep pattern matches a bare `#<digits>` run, which also matches CSS hex colours (`#123456`) as tracker ids. | Put a tracker word before the `#` (`(issue\|fixes\|closes)\s+#[0-9]{3,}`), require the match not to follow a `#`, or use the BL/GH-prefixed id forms. |
| `final_gate_without_script` | block | A `final_gate` task has no verification script, no `verification_script_reason` and no usable report path. | Give the gate a script under `.datawatch/verify/`, or a report path under `.datawatch/reports/`, or state why it has none in `verification_script_reason`. |
| `verify_script_syntax` | block | `<shell> -n` reports a syntax error in the script. | Fix the syntax at the reported line. |
| `verify_script_shell_mismatch` | block | A helper is called with a shell other than the one its shebang names. | Call the helper with the shell its shebang names, or run it directly. |
| `verify_script_pipefail_grep_q` | block | With `pipefail` on, a pipeline ends in a quiet grep and can read as a false no-match. | Write the output to a file, then run `grep -q` on the file. |
| `verify_script_allowed_files` | block | The literal allowed-file list leaves out a path the plan names (a story or task title, spec, file list or verification script). | Make the allowed list cumulative over the whole plan. |
| `fixed_calendar_date_in_test` | warn | The task asks for a test that uses a fixed calendar date, which passes now and fails later. | Take times from the current time with an offset, or an injected clock. |

**Operator note.** The rows fall in three groups:

- The first eleven (`verify_script_task_files_only`,
  `verify_script_docs_without_sync`, `artefact_files_allowed`,
  `final_gate_artefact_allowed`, `script_invokes_known_flag`,
  `check_matches_spec_text`, `final_gate_without_script`,
  `verify_script_artefact_files`, `verify_script_final_gate_rerun`,
  `verify_script_unknown_flag`, `verify_script_text_not_in_spec`) are rules in
  the rule pack, severity `block`: in `enforce` mode they feed the planner a
  retry with the findings as guidance; in `shadow` mode they are recorded on
  the Automaton and nothing is retried. An operator rule with the same id can
  replace or disable them.
- The five script-content rules `verify_script_rejects_own_files`,
  `verify_script_needs_later_task_output`, `verify_script_sh_for_bash`,
  `verify_script_pipe_grep_q` and `verify_script_hex_pattern` are also rule-pack
  rules at `block`, and quote the offending line.
- The next four (`verify_script_syntax`, `verify_script_shell_mismatch`,
  `verify_script_pipefail_grep_q`, `verify_script_allowed_files`) are the
  preflight, not rule-pack rules (details in
  [section 5a](#5a-preflight-checks-for-planner-written-scripts)). They run on
  every decompose and when an edit sets a task's script, always at `block`,
  and their findings appear on the Automaton before any task starts.
- `fixed_calendar_date_in_test` is a rule-pack rule at `warn`: it is recorded
  and never triggers a retry.

See
[the decompose-lint how-to](automaton-decompose-lint.md#3-the-rule-pack-that-ships)
for the whole rule pack.

### A script that does not exist yet

The warn rule `unexecutable_verification_script` reports a missing script file
and states which of two cases applies to the Automaton task:

- **The daemon will write the verbatim block.** The task spec says
  `Write it VERBATIM to <path>` followed by a fenced script block, and `<path>`
  is the task's verification script. The daemon writes that block to the path
  before the task starts, so the finding is informational.
- **The task creates it and it is checked after the task writes it.** The spec
  tells the worker to create the script (for example `Create the script at
  <path>`) and carries no verbatim block. The file is absent at start; the
  verification after the task runs the written file and fails if it was never
  written.

When neither applies, the finding keeps its plain warning that the script does
not exist: add a verbatim block, tell the worker to create the script, or list
the script in the task's generated files.

**What happens at task start.** The pre-start verifier no longer refuses a task
only because its script is missing when the spec says how the file comes to
exist:

1. **Verbatim block.** Before the check, the daemon writes the block to the
   script path with mode `0755` and records a `verifier_materialize` decision
   on the Automaton. It never overwrites a file that already exists, and it
   refuses an absolute path, a `..` path or a path that is not the task's
   verification script. This also rebuilds the script on a restart after
   `.datawatch/verify/` was removed.
2. **Task creates it.** With no verbatim block and a spec that tells the worker
   to create the script (`Write it to <path>`, `Create the script at <path>`,
   `touch <path>`, `chmod +x <path>`), the missing file is accepted, a
   `verifier_prestart_task_created` decision is recorded and the task starts.
   The path must be the task's own script (full path or its base name) and
   directly follow the instruction on the same line.
3. **Neither.** The task is still refused (or, in `shadow` mode, recorded) as
   before: the script does not exist.

Only a **missing** script is accepted this way. A script that exists but is not
executable, has no shebang or lies outside the project is still a problem. The
check after the task runs the written file and fails the task if it was never
written, so accepting the start loses no safety. With
`autonomous.checks.pre_start_verifier` set to `off` none of this runs.

## 5a. Preflight checks for planner-written scripts

Verification and final-gate scripts written by the planner used to fail on
their first real run for reasons that had nothing to do with the task. The
preflight checks catch four of those causes by reading the script text, never
by running the script. Each finding names the script (repo-relative), the line
(0 when no line applies), the rule id and a message.

| Rule id | What it catches | How to fix it |
|---|---|---|
| `verify_script_syntax` | `<shell> -n` reports a syntax error in a script whose shebang names `bash`, `sh`, `dash`, `ksh` or `zsh`. At most one finding per script. A script whose interpreter is not installed is not flagged. | Fix the syntax at the reported line. |
| `verify_script_shell_mismatch` | The script calls a helper with one shell (for example `sh helper.sh`) while the helper's shebang names another (`#!/usr/bin/env bash`). Helpers that cannot be read, or have no shell shebang, are skipped. | Call the helper with the shell its shebang names, or run it directly. |
| `verify_script_pipefail_grep_q` | The script enables `pipefail` (`set -o pipefail`, `set -euo pipefail`) and pipes into a quiet grep (`-q`, `-qE`, `-Eq`, `--quiet`, also after `\|&`). grep exits at the first match, the upstream command gets a broken pipe, and the check reads as a false no-match. | Write the pipeline's output to a file, then test the file with `grep -q`. |
| `verify_script_allowed_files` | The script's literal allowed-file list leaves out a path the **whole plan** names: a story or task title, a spec, a planned or generated file list, or a verification script. A Go file the plan names that already has a `_test.go` sibling in the tree drags that test file in with it. Files named by the built-in standard tasks count as covered (a templated path is cut to its static folder), and a folder exemption covers everything under it; glob entries (`*`) are allowed in the list. A script that names no literal path builds its list at run time and is not checked. | Make the list cumulative over the whole plan (see [section 2](#2-the-cumulative-allowed-file-helper)). |

**Where they run.** The preflight runs in two places, and every finding it
raises has severity `block`:

- **Every decompose.** Each decompose-lint pass also runs the preflight over
  the planned stories and adds its findings to the pass. In `enforce` mode they
  drive the retry like any other `block` finding; in `shadow` mode they are
  recorded only. With decompose lint `off`, the preflight still runs once and,
  when it finds anything, records those findings on the Automaton (with mode
  `off`); nothing is retried.
- **A task edit that sets a script.** When you set a task's
  `verification_script` with the edit tools ([section 6](#6-set-a-tasks-script-or-reason-with-the-edit-tools)),
  the preflight runs for that one script. Its findings replace any earlier
  finding for the same script and rule id in the Automaton's decompose-lint
  findings; they are reported, never enforced, and never refuse the edit.

The findings are read on the same surfaces as other decompose-lint findings
(`autonomous_decompose_lint_findings`, the findings REST/CLI paths and the PWA
findings list). The preflight has no configuration setting of its own and is
not a rule in the rule pack, so an operator rule cannot disable or re-grade it.
The read-only dry run (`autonomous_decompose_lint_dry_run`) does **not** run
the preflight; it only runs the rule pack.

## 5b. Allowlist coverage and phrase wording

If your final gate restricts changed files, derive its allowlist from every path
the task specs and the investigation name, plus the existing tests those changes
need and the generated changelog and parity draft files. Do not guess a
directory list. The rule `verify_script_allowed_files` fails when a spec names a
file the allowlist rejects; globs and generated-folder prefixes count as coverage.
See [Final gate allowlist](../datawatch-definitions.md#final-gate-allowlist).

Worked example: a spec names `docs/howto/x.md`; the gate allows only `internal/`.
Add `docs/howto/x.md` (or `docs/howto/*`) to the allowlist.

The phrase checks compare meaning, not exact wording: reordered, re-worded,
plural or tense variants of the same key words count as stated in the spec. A
script that checks "tests passed" is fine when the spec says "tests must pass".
Details: [decompose lint](automaton-decompose-lint.md#3a-final-gate-allowlist-and-meaning-based-phrase-checks).

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

## 7. The verification lint gate

A task's verification once passed a branch that the repository's own `make lint`
rejected: the script checked the task's behavior, and nothing ran the linter over
the code it changed. The lint gate closes that gap.

- **What it lints.** It lists the files the branch changed against its merge base
  with `main` (untracked files included), keeps the Go files, and lints only their
  package directories (`./<dir>`). Files outside Go packages are ignored.
- **No Go change, no lint.** When the branch touches no Go package the gate passes
  without starting the linter.
- **Findings fail the gate.** The linter runs as `golangci-lint run <packages>` from
  the project directory. A non-zero exit fails the gate, and the linter output is
  kept with the result, trimmed and capped at 8 KiB.
- **A linter that cannot run is never a pass.** If `golangci-lint` is not on `PATH`
  or cannot start, the gate returns an error instead of a result; treat it as an
  infrastructure fault, not as a clean branch.

**When it runs.** The gate runs during verification of every task and every story,
right after the verification script passes and before any guardrail runs. A script
that fails never reaches the gate. The gate is skipped (counts as a pass) when the
unit has no project directory or the directory is not a git work tree.

**What a failure looks like.** A failing gate records a `lint-gate` verdict with
outcome `block` and severity `high`, sets the unit's failure reason to category
`content`, source `lint-gate`, not retryable, and appends the message (starting
`lint gate failed`) to the script output you see on the task. No guardrail runs
after it. Both linter findings and a linter that cannot start produce this block;
a missing `golangci-lint` is not retried as infrastructure. Fix the findings (or
install the linter on the worker's node) and reset the task.

The gate has no setting of its own; the linter command is fixed and injectable only
in tests. See **Verification lint gate** in [the definitions](../datawatch-definitions.md#shadow-merge-checks), listed beside the merge lint checks.

## Still to review (operator)

These points are decisions or checks the docs cannot settle for you:

- **Block severity is the shipped default.** All the verification-script
  stability rules are `block` (`verify_script_task_files_only`,
  `verify_script_docs_without_sync`, `artefact_files_allowed`,
  `final_gate_artefact_allowed`, `script_invokes_known_flag`,
  `check_matches_spec_text`, `final_gate_without_script`,
  `verify_script_artefact_files`, `verify_script_final_gate_rerun`,
  `verify_script_unknown_flag` and `verify_script_text_not_in_spec`): in `enforce`
  mode a script that breaks one sends the plan back to
  the planner (up to `max_retries`) before any task runs. Decide whether you
  want that, or want `shadow` first to see how often your planners trip them;
  an operator rule with the same id replaces the built-in.
- **Existing scripts are not rewritten.** The rules read scripts as they stand
  in the project. A script already checked in that compares only against its
  own task's files will now be flagged on the next decompose; fix or exempt it.
- **Allowlist rule fires on more existing scripts.** `verify_script_allowed_files`
  now also fails when a spec names a path the literal allowlist rejects, and the
  phrase checks accept reworded or inflected wording. A checked-in gate script
  whose allowlist misses a spec-named path (or a sibling `_test.go` the plan
  touches) will be flagged on the next decompose; add the path or a covering
  glob. Also confirm the looser phrase matching does not let through a checked
  phrase that the spec never really states.
- **Live planner output is not re-measured here.** These pages describe the
  prompt text and rules in the tree; whether a real planner now writes stable
  scripts on first try has not been confirmed against a running daemon.
- **Lint gate blocks on a missing linter.** The gate now runs after every passing
  task and story script. A node without `golangci-lint` on `PATH` blocks every Go
  task as a non-retryable content failure, not an infrastructure retry. Confirm
  each worker node has the linter, or decide whether that case should be retried.
- **No off switch.** The gate cannot be turned off per Automaton or globally; a
  repository with pre-existing findings in a touched package blocks until fixed.
- **Lint version.** The gate uses whatever `golangci-lint` is on `PATH`; it does not
  check the version CI pins (the shadow merge's lint version pin does that).
- **Spec wording decides the case.** The task-created case is matched on
  phrases in the spec text. A spec that words the instruction differently is
  still refused at start; confirm your planner's wording on a dry run before
  relying on it.
- **Preflight severity is fixed at `block`.** The four `verify_script_*`
  preflight checks in [section 5a](#5a-preflight-checks-for-planner-written-scripts)
  now run on every decompose and on a script edit, with no setting to turn
  them off or lower them. In `enforce` mode they can send a plan back to the
  planner. Review whether that is wanted, and note the dry run does not show
  them — only a real decompose or an edit does.
- **Fixed calendar dates in tests are a warning only.** The new
  `fixed_calendar_date_in_test` rule (see the
  [rule pack](automaton-decompose-lint.md#3-the-rule-pack-that-ships)) flags a
  test-writing task whose spec holds a `YYYY-MM-DD` date, but never blocks. It
  matches any such date, including ones that are not test fixtures; decide
  whether to raise or disable it with an operator rule of the same id.
- **Mobile apps.** The task editor fields are described for the web app only;
  parity for the Android and iPhone apps is tracked outside this tree.

## See also

- [Decompose lint, standard tasks and guidance](automaton-decompose-lint.md)
- [Daemon task modes and the script gate](automaton-daemon-task-modes.md)
- [Verification script and exemption reason](../datawatch-definitions.md#verification-script-and-exemption-reason)
