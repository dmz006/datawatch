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

## Choose the release size

Every ship steps the project's version by one: `patch`, `minor` or `major` —
or steps no version at all (`none`). How much a merge steps is its
**release size**, and the daemon resolves it for each Automaton, most
specific layer first. The resolution is stored and readable now; the merge's
version step that applies it is wired in the work that follows, so until then
a set size does not change what a ship bumps (see the note at the top of
[release size and milestones](release-size-and-milestones.md)):

| Layer | What decides it |
|---|---|
| Explicit | The Automaton's own `release_size` (`patch`, `minor`, `major` or `none`). An explicit major applies under any `major_trigger`. |
| Milestone | The Automaton's Automation (arc) is marked `milestone`, its members are complete, and the project's `major_trigger` is `milestone`: the release steps major. |
| Kind map | The `release_size_map` entry for the Automaton's kind (the daemon labels Automata `feature`, `bug` or another kind of its own). A `major` entry is stored but never used: a major is never inferred from a kind. |
| Default | kind `feature` steps minor, kind `bug` steps patch, any other kind steps no version. |

The resolution never reads the diff and never infers a major: a major step
comes from an explicit `release_size: major`, or from a completed
`milestone` arc while `major_trigger` is `milestone`, or it does not
happen.

Separately from the size, an Automaton marked `breaking_change` is reported
as one whose merge must go through the major approval mode, whatever step it
lands on. That is how a breaking change stays visible to you even when it
ships as a patch.

The size and the flags are set on the records: `release_size` and
`breaking_change` on the Automaton record, `milestone` on the Automation
record; an unset record keeps the old behaviour. The keys the size is
resolved through are the release keys of `autonomous.merge_pipeline`, listed
in the settings table below.

> **Status** — the keys and flags are stored and resolved by the daemon in
> this release. The merge runner's version-bump step consuming the resolved
> size, and the release approval modes the `breaking_change` flag points at,
> are wired in the work that follows this one; until then, nothing you set
> here changes what a ship bumps.

## After the ship: post-ship hooks

Once the pipeline's push step succeeds, the daemon can run the post-ship
hooks a project has configured: mark its registry row shipped, file a parity
issue, tag the release or send a notification. The hooks are off by default
and a failed hook never blocks or fails the ship — the ship is already done.
The operator guide is [How-to: Post-ship hooks](post-ship-hooks.md).

## Settings

All keys are under `autonomous.merge_pipeline` and each can be overridden per
project under `autonomous.merge_pipeline.projects.<project>.<key>`.

| Key | Default | Meaning |
|---|---|---|
| `file_name` | `.datawatch/merge-pipeline.yaml` | the pipeline file looked for in each project |
| `source_order` | `file, record` | the sources tried, first to last |
| `fallback_on_bad_file` | `record` | what is used when the file exists but cannot be used |
| `conflict_policy` | `mechanical_and_model` | what happens when a merge conflicts |
| `conflict_model` | the built-in default model name | the model a policy that needs one asks for the rest of a conflict |
| `version_files` | none; a pipeline that bumps the version fails that step until you name the files that carry the version line | the project files the release-version step sets |
| `release_size_map` | `feature: minor`, `bug: patch` (built-in) | the kind-to-size entries of the release size; an entry adds to or overrides the built-in |
| `major_trigger` | `manual` | `manual`: a major is an explicit `release_size: major` only; `milestone`: a completed `milestone` Automation as well; `never`: a milestone never steps a major |
| `release_cadence` | `per_merge` | one release step per merged Automaton; `batch` and `manual` are named values that are refused as not available yet |
| `version_scheme` | `semver` | versions are numbered major.minor.patch; any other name is refused as not available yet |

A `release_cadence` other than `per_merge`, a `version_scheme` other than
`semver`, a `major_trigger` outside the three, or a `release_size_map` value
outside `patch`, `minor`, `major`, `none` is refused on every write surface
with the key named, and nothing is written.

Set them with `datawatch config set`, the `autonomous_config_set` MCP tool (its
`merge_pipeline_*` arguments), the `configure` chat verb, or Settings >
Automation > Merge pipeline in the PWA. The four release keys each have a `?`
link there that opens the [Release size](../datawatch-definitions.md#release-size)
entry in the definitions. The other keys open their own entries for the pipeline
in the same file.
