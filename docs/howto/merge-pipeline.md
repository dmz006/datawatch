---
docs:
  index: true
  topics: [merge, pipeline, conflict, release, ship, automata, autonomous]
exec_params:
  - {name: project, required: false, description: "Project name; leave empty for the merge base repository"}
exec_steps:
  - tool: autonomous_merge_pipeline_get
    description: Read the pipeline in effect for the project, where it came from and why, the conflict policy and model, and any per-project overrides
    args: {project: "{{params.project}}"}
    read_only: true
  - tool: autonomous_merge_pipeline_validate
    description: Check the project's repository pipeline file without saving anything; every problem is named
    args: {project: "{{params.project}}", file: "true"}
    read_only: true
---
# How-to: Merge pipeline

The merge pipeline is the ordered list of steps that takes finished branches to
shipped: checks, merge, version bump, changelog, tag, push and deploy. Every
project has one. You can keep it in a file in the project's own repository,
keep it as a record the daemon holds, or do neither and use the built-in
pipeline. This guide shows how to define it each way, which one wins, how to see
what is in effect, and how merge conflicts are handled.

This how-to belongs to the tutorial and onboarding material. The
[onboarding report](onboarding-report.md) points at it when a project still uses
the built-in pipeline.

## What a pipeline looks like

A pipeline is a list of steps. Each step has a `name`, a `kind` and either a
shell `command` or a built-in `action`.

```yaml
steps:
  - name: test
    kind: test
    command: make test
    on_failure: stop
  - name: lint
    kind: lint
    command: make lint
    requires_make_target: lint
  - name: merge-branches
    kind: ship
    action: merge_branches
  - name: deploy
    kind: deploy
    action: deploy_latest
    on_failure: continue
```

| Field | Meaning |
|---|---|
| `name` | A label for the step, shown in the run record. |
| `kind` | `test`, `lint`, `version_bump`, `changelog`, `ship`, `tag`, `deploy`, `post_ship_hook` or `custom`. |
| `command` | A shell command line. Give this or `action`, never both. |
| `action` | A built-in: `check_branch_versions`, `merge_branches`, `set_release_version`, `insert_changelog_heading`, `commit_release_bump`, `tag_release`, `push_release` or `deploy_latest`. |
| `dir` | A folder inside the project to run in. Empty means the project root. |
| `on_failure` | `stop` (the default) ends the run at this step; `continue` records the failure and runs the next step. |
| `requires_make_target` | Run the step only if the project's Makefile defines that target. |
| `timeout` | How long the step may run. Empty uses the runner's default. |

A pipeline that does not validate is refused with every problem named, so you
fix them all in one pass.

## Define a pipeline in a repository file

Commit a file named `.datawatch/merge-pipeline.yaml` in the project (the name is
a setting, see below). The pipeline then travels with the code and is reviewed
like any other change.

1. Write the steps as shown above.
2. Check the file before you rely on it:
   - CLI: `datawatch autonomous merge-pipeline validate --repo-file`
   - MCP: `autonomous_merge_pipeline_validate` with `file` set to `true`
   - Chat: `autonomous merge-pipeline validate file`
3. Read what is in effect (next section).

## Define a pipeline as a daemon record

Use a record when the repository cannot hold the file, or when you want one
pipeline kept by the daemon.

- CLI: `datawatch autonomous merge-pipeline set-record --from-file pipeline.yaml`
  (use `-` to read standard input)
- MCP: `autonomous_merge_pipeline_set_record` with `yaml` or `definition`
- Chat: `autonomous merge-pipeline set-record <pipeline as YAML or JSON>`
- PWA: Settings > Automation > Merge pipeline > Edit the daemon record

To remove it: `datawatch autonomous merge-pipeline clear-record`, the same tool
with `clear` set to `true`, or Remove record in the PWA.

## No project yet

Nothing above needs a registered project: a read without a project name uses
the merge base repository, and the pipeline resolves and runs inside it. When
neither a registered project nor a merge base repository is set, the read
answers HTTP 400 with the code `project_required` (a JSON body with
`code: "project_required"`).

In the PWA you never see a raw error: in Settings > Automation, the Merge
pipeline card shows an empty state instead. To fix it, choose a project in
the card's project field (or pass the project argument of the API call or
the MCP tool, or `--project` on the CLI), or set the merge base repository
with the `worktree_base_repo` setting (`autonomous.worktree_base_repo`) —
the empty state links to this section. That setting is a repository path the
operator points at the daemon's working root, so it has no PWA settings
field; every other surface can set it:

| Surface | How |
|---|---|
| YAML | `autonomous.worktree_base_repo: /path/to/base-repo` in `~/.datawatch/config.yaml` |
| CLI | `datawatch config set autonomous.worktree_base_repo /path/to/base-repo` |
| Web UI | N/A — no settings field (a repository path, not a settings entry); the empty state links here |
| REST API | `PUT /api/config` with `{"autonomous.worktree_base_repo": "/path/to/base-repo"}` |
| Comm channel | `configure autonomous.worktree_base_repo=/path/to/base-repo` |

Once it is set, the reads above — `datawatch autonomous merge-pipeline show`,
`autonomous_merge_pipeline_get`, `autonomous merge-pipeline` and the card —
resolve the pipeline in the base repository without a project name, and the
card's empty state becomes the pipeline itself.

## Which one wins, and how to see it

The sources are tried in the order set by `autonomous.merge_pipeline.source_order`.
By default that is the repository file first, then the daemon record. The
built-in pipeline always applies last, so a project with neither still has a
pipeline.

If the repository file exists but cannot be used (it is unreadable, too large,
not valid YAML or fails validation), `autonomous.merge_pipeline.fallback_on_bad_file`
decides: `record` (the default) uses the daemon record, `default` uses the
built-in pipeline, `none` uses nothing and the merge run does not start. The
problem is always reported, whichever you choose.

To see the result:

- CLI: `datawatch autonomous merge-pipeline show [--project P]`
- MCP: `autonomous_merge_pipeline_get`
- Chat: `autonomous merge-pipeline`
- PWA: Settings > Automation > Merge pipeline

The answer names the source in effect (project file, daemon record or built-in
default), the reason in plain language, any problem found with the file even if
another source was used, the steps in order, and the conflict policy and model.

## Choose the conflict policy

When merging a branch conflicts, `autonomous.merge_pipeline.conflict_policy`
says what happens:

| Policy | What happens |
|---|---|
| `stop_and_ask` | The merge is aborted, the project is unchanged and you are asked. |
| `mechanical` | Conflicts where both sides only added lines are resolved automatically. If anything else is left, the merge is aborted and you are asked. |
| `mechanical_and_model` (the default) | The mechanical step runs first, then a model proposes the rest. A proposal is used only if it passes the test step. Otherwise you are asked. |

A policy that needs a model falls back to `stop_and_ask` when no model is set or
the model cannot be reached; the show output says so.

## Choose the conflict model

`autonomous.merge_pipeline.conflict_model` names an entry in your LLM registry.
Leave it unset to use the built-in default name; set it to an empty string for
no model. The setting never adds, edits or removes a registry entry, so make
sure the name you give exists. If you run a model of your own, name that one.

## How a model proposal is tested before use

Your project is never touched while a model is working. For each conflict the
daemon:

1. Redoes the merge in a throwaway scratch worktree and resolves the additive
   hunks mechanically.
2. Sends the remaining conflicting hunks, as data, to the conflict model and
   accepts only a reply that is exactly one resolution for every hunk, with no
   conflict markers left.
3. Applies the proposal in the scratch tree and runs the pipeline's test step
   there.
4. Only if the test step passes does it commit the merge and move the project to
   it. On any failure, timeout, oversized prompt or unreadable reply, the scratch
   tree is thrown away, the project is unchanged and you are asked.

## After the ship: post-ship hooks

Once the pipeline's push step succeeds, the daemon can run the post-ship
hooks a project has configured: mark its registry row shipped, file a parity
issue, tag the release or send a notification. The hooks are off by default
and a failed hook never blocks or fails the ship — the ship is already done.
The operator guide is [How-to: Post-ship hooks](post-ship-hooks.md).

## The shadow merge plan

Phase 4 of the autonomy handover (review and merge by the daemon) moves the merge from "the supervisor works it out by hand" to "the daemon works it out and only then acts", using the same shadow-first rule as every other handover: the daemon plans the merge, the supervisor does the real one, and the two are compared — every difference is a defect. The plan and the shadow comparison that grades it against the merge that actually landed both exist as of this step; the rest of the phase lands separately.

### What a plan says

One shadow merge plan per Automaton, describing what its merge would do:

| Field | Meaning |
|---|---|
| Release size | `patch`, `minor`, `major` or `none`. |
| Planned version | The version the bump step would set. Empty when there is none. |
| Changelog heading and body | The entry drawn from the Automaton's own changelog entry task, or empty when it has none — nothing is invented. |
| Changelog source | The task the entry came from, or `missing` when no task carries a well-formed one. |
| README line, registry row, parity issue | The mechanical release edits the merge would make. Empty when there are none. |
| Conflicts | Each conflicting path with its file class, the resolution and who made it. |
| Gates | Each lint/test gate with its status, how long it took and its detail. |
| Findings | Each problem with a code, a message and whether it blocks. |

### What a plan never does

- It is never applied. Every stored plan has `applied` set to `false`, and the store refuses a plan marked applied — a shadow plan is a record, not a queue.
- It touches nothing in your project. The trial merge that finds the conflicts runs in a throwaway worktree outside the project's tree, does the merge without committing it, and the scratch tree is removed on every exit path. No branch, tag or ref is created or moved, and nothing is pushed.
- It invents nothing. The changelog text comes only from the Automaton's own changelog entry task when its output has the exact shape `## [version] - YYYY-MM-DD` with a non-empty body. Without one, the plan carries empty heading and body and the source `missing`.

### Where plans live

One plan per Automaton, as `merge-plans/<id>.json` under the daemon's data directory, written atomically (a temporary file renamed into place, so a crash never leaves a torn plan). Reads list the newest first, and a project's retention prunes the oldest plans beyond the most recent N (N is at least 1).

### How the shadow comparison runs

When the merge has landed and the shipped record has been written for the Automaton, the daemon starts the shadow comparison in the background. The ship path never waits on it: the comparison runs under a two-minute budget, and when that budget is spent, or the comparison fails for any other reason (an unresolvable base ref included), whatever it left behind is logged and dropped. The ship already happened and nothing about it changes.

- **Nothing without a plan.** Without a stored plan to compare — an Automaton that finished before reaching the plan step, or whose plan was pruned by retention — the comparison does no work at all: no git reads, no comparison record, no files written.
- **Read from the ref, never the tree.** The landed state is read from the default branch's ref — the plan's base ref, or `main` when the plan has none — with `git show <ref>:<path>`, so the comparison sees what landed, not your working tree, and uncommitted changes cannot skew it. It reads the first `## ` heading of `CHANGELOG.md`, the `var Version` line of every version file the pipeline names (a file without the line reads as `no-version`), the `Current release` line of `README.md` whose version matches the plan, and the registry table row that names both this project and its Automaton — from `.datawatch/mergeshadow/registry.md` at the project root when present, else the pipeline's post-ship registry file with its `{project}` placeholder filled in, else `docs/plans/README.md`.
- **Not found is a fact, not an error.** A file or line that is absent reads as *not found* and feeds the comparison — a planned artefact found nowhere is the input to a difference, not a failure of the read.
- **Differences are graded.** The plan and the landed state are compared field by field — version (every version file), changelog heading, README line, registry row, the parity-issue flag, and gate results. A difference is **major** when a version is wrong or missing, a planned heading, README line or registry row landed nowhere, or a gate result or parity-issue expectation is unmet; **minor** when the heading, README line or registry row drifted in wording.
- **The comparison is stored.** One comparison per Automaton, as `merge-comparisons/<id>.json` under the daemon's data directory, written atomically: every matched, differing and not-compared field with its planned and landed value, plus the difference count by severity.
- **Each difference leaves a defect candidate.** Every difference is also recorded as a defect candidate on the Automaton: its severity, field, both values and the comparison id, with the status `candidate` and a content-derived identity key, stored under `defect-candidates/` in the data directory. The store is idempotent by content — re-running the comparison on an unchanged plan and landed state adds nothing — and a candidate is not yet a defect: filing it into the defect queue is a separate job.

**Two lines still read from the plan itself, and the record says so.** The parity-issue line compares the plan's flag against the plan's flag today, because the landed-side look-up of a filed parity issue is not wired yet — that line reports *matched* without anything being checked. Gate lines compare against the gate results the plan itself recorded in its trial run — the supervisor's manual gate record slot is there for when one is supplied later — and a gate with an expectation but no result at all becomes a *missing gate* difference, never a silent failure. Both lines are visible in the record rather than hidden by it.

### What is still yours to do

As of this step the plan, the comparison and the defect candidates are internal records: none has a read on any operator surface yet (no REST route, MCP tool, CLI verb or PWA card), and the candidates are not yet filed into the defect queue. The rest of the phase — the release approval modes per size of release (each `automatic`, `ask` or `manual`), the daemon's own push and tag under a release path, conflict resolution by file class with the confirmation counter — lands in the same phase and is documented as it does. What you still review: every difference the comparison grades (each one names its field, both values and its severity), the two lines that still read from the plan itself (parity-issue and gates, above), and whether the evidence bar below has been met before the supervisor steps out of the loop. Until the shadow comparison meets the evidence bar (ten merges where the daemon's plan equals the manual one or is better, including at least two with conflicts and two with PWA changes, no blocking comparison defect), the merge stays with the supervisor. The phase's modes, defaults and exit criteria are in [the autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4.

## Settings

All keys are under `autonomous.merge_pipeline` and each can be overridden per
project under `autonomous.merge_pipeline.projects.<project>.<key>`.

| Key | Default |
|---|---|
| `file_name` | `.datawatch/merge-pipeline.yaml` |
| `source_order` | `file, record` |
| `fallback_on_bad_file` | `record` |
| `conflict_policy` | `mechanical_and_model` |
| `conflict_model` | the built-in default model name |
| `version_files` | none; a pipeline that bumps the version fails that step until you name the files that carry the version line |

Set them with `datawatch config set`, the `autonomous_config_set` MCP tool (its
`merge_pipeline_*` arguments), the `configure` chat verb, or Settings >
Automation > Merge pipeline in the PWA. Each help link there opens the matching
entry in [the definitions](../datawatch-definitions.md#merge-pipeline).
