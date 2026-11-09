---
docs:
  index: true
  topics: [merge, pipeline, conflict, release, ship, automata, autonomous]
exec_params:
  - {name: project, required: false, description: "Project name; leave empty for the merge base repository"}
exec_steps:
  - tool: autonomous_merge_pipeline_get
    description: Read the pipeline in effect for the project, where it came from and why, the conflict policy and model, the release approval modes in effect, and any per-project overrides
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
what is in effect, how merge conflicts are handled and who approves a release
before it ships.

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
another source was used, the steps in order, the conflict policy and model. The
text answers (CLI, chat, MCP) also name the release approval modes in effect for
the project as a `Release approval: patch …, minor …, major …` line, and the
JSON body of `GET /api/autonomous/merge-pipeline` carries them as
`release_approval_in_effect`. The PWA card shows the same three modes under
Release approval, one select per size, and a change there is saved as that
project's override (see [Release approval modes](#release-approval-modes)).

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

## Release approval modes

`autonomous.merge_pipeline.release_approval` decides, for each size of release, who has to approve it before the release ships. There is one key per size — `patch`, `minor` and `major` — and each takes one of three modes:

| Mode | What happens |
|---|---|
| `auto` | With the pipeline's checks passing, the release ships without waiting for anyone; a failed check holds it for a person to look. |
| `ask` | The operator is asked before it ships, and it ships only on their yes. |
| `manual` | The release is held for the operator to ship; nothing is asked. |

The defaults are patch `auto`, minor `auto`, major `ask`: a major release is never shipped quietly, and a release whose size cannot be read resolves to `ask` rather than being shipped. The daemon's merge run honours this setting through the release gate ([The release gate](#the-release-gate)). A mode is only `auto`, `ask` or `manual` — anything else is refused with the key named rather than stored, wherever it is set. Over MCP, `autonomous_config_set` takes `merge_pipeline_release_approval_patch`, `merge_pipeline_release_approval_minor` and `merge_pipeline_release_approval_major` (each written as `autonomous.merge_pipeline.release_approval.<size>`), and `merge_pipeline_project` with them sets the mode for that project only; `config_set` takes the same dotted keys.

You can see where the inheritance lands for a project: the read answer carries `release_approval_in_effect` with the resolved mode per size (the project's value, else the global one, else the default above), its text answers (`show` on the command line, the chat verb and the MCP tool's text) add a `Release approval: patch …, minor …, major …` line, and `GET /api/config` reads the block's effective `release_approval` back with the same per-size resolution. The PWA merge pipeline card shows the mode in effect for each size, and choosing a mode there writes that project's override, the same way the card's conflict policy and model work. The settings page (Settings > Automation, the Merge pipeline group) carries one global field per size; choosing a mode there writes the global `autonomous.merge_pipeline.release_approval.<size>`.

Each size inherits when unset, in this order — a project's value in `autonomous.merge_pipeline.projects.<project>.release_approval`, then the global `release_approval`, then the defaults above. For example, to leave the defaults in place and make one project ship minor releases manually:

```yaml
autonomous:
  merge_pipeline:
    projects:
      myproject:
        release_approval:
          minor: manual
```

Set or read a mode with `datawatch config set autonomous.merge_pipeline.release_approval.major ask`, `PUT /api/config` with `{"autonomous.merge_pipeline.release_approval.major": "ask"}`, MCP `config_set`, or the comm channel `configure autonomous.merge_pipeline.release_approval.major=ask`; sending `null` for a size clears it back to inheriting. Moving a size toward `auto` — above all `major`, which is the operator's by default — is a decision to make deliberately and record with its evidence: the daemon's merge run honours these modes, so `auto` means it ships that size on its own — with the checks passing; a failed check holds the release for a person in every mode.

## The release gate

When the daemon's merge run prepares a release, a release gate stands between the prepared work and anything that leaves the machine. It applies to every leaving step — the `tag_release`, `push_release` and `deploy_latest` actions and the tag, deploy and post-ship-hook steps — and decides at most once per run, before the first of them runs; the decision is cached and applies to every later leaving step, so the run cannot answer the push differently from the tag.

What the gate keeps back is only the leaving steps. The preparation — merging the branches into the local main, the version bump, the changelog, the commit — lands as usual; holding the release means the tag is not made, nothing is pushed and the deploy does not run.

The checks are the ground rule, and the mode decides the rest:

- **A failed check never ships, in any mode, and never asks.** A "yes" against failed checks could only be honoured by shipping them, so the leaving steps are skipped and the run holds for a person to look at what failed.
- **`auto`** — with the checks passing, the leaving steps run as before and nothing is asked.
- **`ask`** — the gate raises one yes/no question, ending "Ship this release?", with the plan in it: the project, the Automaton, the size, the version it would get, the branches it merges, the checks and their results, and the leaving steps in the order they would run. The options are "yes (ship it)" and "no (do not ship)", and "no" is the default. The run ships only when the question is answered yes, and then its first leaving step records that it was approved by the operator, with the question id. A re-run for the same project, Automaton and size finds the question already raised rather than raising a second one, and a changed mind replaces the earlier answer — only the current one counts.
- **`manual`** — the leaving steps are held for a person and no question is raised; the operator ships the release.

The gate never guesses: if the `ask` mode cannot raise its question, the leaving step fails and the run stops short of the ship.

Nothing in the run's record hides the hold. The steps the gate keeps back are still listed, skipped with the gate's note — the mode, the size (or "not read") and the reason — in the run's steps, in `pipeline_runs` of `GET /api/autonomous/merge` and the PWA's merge status view, beside the steps that did run.

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
| `release_approval` | patch `auto`, minor `auto`, major `ask`; one mode per key, each `auto`, `ask` or `manual`; a project's `release_approval` takes precedence over the global one |
| `version_files` | none; a pipeline that bumps the version fails that step until you name the files that carry the version line |

Set them with `datawatch config set`, the `autonomous_config_set` MCP tool (its `merge_pipeline_*` arguments, and `merge_pipeline_release_approval_patch|minor|major` for a mode, with `merge_pipeline_project` for one project), the `configure` chat verb (a mode outside the three is refused with the key named before it is stored), or Settings > Automation > Merge pipeline in the PWA — the card's one-select-per-size section writes that project's override, and the settings page's one-per-size fields write the global value. Each help link there opens the matching
entry in [the definitions](../datawatch-definitions.md#merge-pipeline).
