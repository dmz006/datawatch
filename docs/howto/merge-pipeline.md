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
| `action` | A built-in: `rebase_onto_main`, `check_branch_versions`, `merge_branches`, `set_release_version`, `insert_changelog_heading`, `set_readme_release_line`, `commit_release_bump`, `tag_release`, `validate_release_bump`, `push_release` or `deploy_latest`. |
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

Release approval decides what happens to a prepared release before it leaves the
machine: the tag, the push and the deploy. The daemon's merge run prepares the
release in every mode — the branches are merged, the version is bumped, the
changelog is written and the commit is made. The mode decides who ships it.

### The three modes

| Mode | What the daemon does |
|---|---|
| `auto` | Runs the checks. When they pass, it ships the release and reports afterwards. Nobody is asked first. |
| `ask` | Prepares everything, raises one decision with the merge plan, and ships only after a yes. |
| `manual` | Prepares the release and reports. A person ships it. |

In every mode a failed check holds the release for a person to look at. It is
never shipped on a failed check, and "auto" never overrides that. See
[The release gate](#the-release-gate).

### The three sizes

Each size of release has its own mode, under one key per size: `patch`, `minor`
and `major`. Which size a given release is (patch, minor or major) is decided
elsewhere, by the release's own versioning. This setting does not choose the
size; it only says what to do with a release of each size.

### The defaults

| Size | Default mode |
|---|---|
| `patch` | `auto` |
| `minor` | `auto` |
| `major` | `ask` |

A major release is never shipped quietly. A release whose size cannot be read
resolves to `ask`, never to shipping.

### Resolution order

For each size, the mode in effect is the first one that is set:

1. the project's override, under `autonomous.merge_pipeline.projects.<project>.release_approval`;
2. the global value, under `autonomous.merge_pipeline.release_approval`;
3. the default in the table above.

A size left unset inherits. Sending `null` for a size clears the override and
the size inherits again.

### Set the mode on each surface

Each surface sets the same keys. A mode is only `auto`, `ask` or `manual`.

- **config.yaml** — the global modes, in the `autonomous.merge_pipeline` block:

  ```yaml
  autonomous:
    merge_pipeline:
      release_approval:
        patch: auto
        minor: auto
        major: ask
  ```

- **Web UI** — Settings > Automation, in the Merge pipeline group, has one field per size. Set the Major field to `ask` to change the global default. For one project, the Merge pipeline card has a Release approval select per size, and choosing a mode there saves that project's override.
- **REST API** — `PUT /api/config` with the dotted key:

  ```
  PUT /api/config
  {"autonomous.merge_pipeline.release_approval.major": "ask"}
  ```

- **Comm channel** — the `configure` verb:

  ```
  configure autonomous.merge_pipeline.release_approval.major=ask
  ```

- **MCP** — `autonomous_config_set` with the size argument. Add `merge_pipeline_project` to set it for one project only:

  ```
  autonomous_config_set(merge_pipeline_release_approval_minor="manual", merge_pipeline_project="myproject")
  ```

- **Command line** — `datawatch config set`:

  ```
  datawatch config set autonomous.merge_pipeline.release_approval.major ask
  ```

A value outside the three modes is refused on every surface, with the key named,
and nothing is stored. The same goes for a size that is not `patch`, `minor` or
`major`.

### Read the mode in effect

Read the modes that apply to a project, with the inheritance already resolved:

- **Merge pipeline view** — the Merge pipeline card in Settings > Automation shows the mode in effect for each size.
- **Command line and chat** — `datawatch autonomous merge-pipeline show [--project P]`, or the chat verb `autonomous merge-pipeline`. The text answer carries a `Release approval: patch …, minor …, major …` line.
- **MCP** — `autonomous_merge_pipeline_get`. Its answer carries `release_approval_in_effect`, the resolved mode for each size.
- **REST API** — `GET /api/autonomous/merge-pipeline?project=NAME` carries the same `release_approval_in_effect`. `GET /api/config` returns the effective `release_approval` block for the global settings.

For one project, the mode in effect is what the merge run will act on. Read it
before you change a size, because the project's own override wins over the
global value.

A project can also have its own mode for one size. For example, to leave the
defaults in place and make one project ship minor releases manually:

```yaml
autonomous:
  merge_pipeline:
    projects:
      myproject:
        release_approval:
          minor: manual
```

### Answer the decision

In `ask` mode the daemon raises one decision for the release, once per project,
Automaton and size. It appears as the question "Ship this release?", with the
merge plan in it: the project, the Automaton, the size, the version the release
would get, the branches it merges, the checks and their results, and the steps
that would leave the machine, in the order they would run.

Answer it with **yes (ship it)** to let the run ship the release, or **no (do not
ship)** to hold it. "No" is the default, so an unanswered decision never ships.
Answer it where questions are answered: the question list in the web UI, the chat
channel, or the `question_answer` MCP tool. In the question list it is a decision
raised by the daemon's release approval for the project, not a question another
person asked, and it has no route of its own — the ordinary question answer is the
only answer path, so nothing release-specific has to be called. A later answer
replaces an earlier one,
and only the current answer counts. A run that is re-run for the same release
finds the decision already raised rather than raising a second one.

### Changing a mode

Moving a size toward `auto` — above all `major`, which is `ask` by default — is a
decision to make deliberately and record with its evidence. In `auto` the daemon
ships that size on its own once the checks pass, and a failed check still holds
the release for a person in every mode.

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

## The merge lock: one merge per repository

Two merge runs must never ship into the same base repository at once. Before a
run starts, it takes a lock on its repository path; the lock is held for the
whole run, including the deploy step, and is released when the run ends,
whatever the result. The glossary entry is [Merge lock](../datawatch-definitions.md#merge-lock).

- **A second run is refused.** Its run record is marked failed, and its reason
  names the run that holds the lock, for example `another merge holds this
  repository: run <id> (Automaton <id>)`. Nothing is merged, and the holder is
  not touched. Wait for the holding run to finish, then start again.
- **The held lock is visible.** While a merge runs, the merge status shows the
  lock: `Merge lock held: run <id> for Automaton <id> (branches …) since <time>`.
  The same information is in the `merge_lock` field of `GET /api/autonomous/merge`
  (absent when nothing is held).
- **The lock survives a restart.** It is kept in `merge-locks.json` in the daemon
  store directory. After a daemon restart the lock still shows as held, with
  the same run, Automaton and branches it had before.
- **After a restart the same Automaton takes its lock back.** When the Automaton
  that held the lock starts its merge again for the same branches (the case
  after a daemon restart), the lock is handed back to it and the run is not
  refused by its own earlier lock. A different Automaton, or the same one with
  different branches, is refused like any other second run.
- **A broken lock file refuses merges; it never lets them run unlocked.** If
  `merge-locks.json` is unreadable or corrupt, every merge run is refused with a
  message that the lock store is unavailable. Repair the file or restore it from
  a backup in the daemon store directory, then start the run again. Deleting the
  file clears all locks, so only do that when you are sure no merge is running.

### Reading the lock

The lock is one of three things, and the surfaces show which:

| State | What it means | Where you see it |
|---|---|---|
| Held | A run is merging into the repository now. | `Merge lock held: run <id> for Automaton <id> (branches …) since <time>` on the merge status; `merge_lock` on `GET /api/autonomous/merge`. |
| Refused run | A run was started while the lock was held. Its record is `failed` and its reason names the holder. | The run's entry in `pipeline_runs` on the merge status. |
| Not held | No run holds the repository; the `merge_lock` field is absent. | Absence of the field and of the held line. |

The surfaces that show the lock and the last validator result are listed in
[Where to read the lock and the last result](#where-to-read-the-lock-and-the-last-result).

## The order inside the lock

Once a run holds the lock, it works through these steps in order. The
step names are the built-in ones listed in [The built-in pipeline](#the-built-in-pipeline) below.

1. **Rebase onto the latest main.** Main is fetched first when the project has
   a remote, and each branch is rebased onto it. A branch that conflicts stops
   the run with that branch untouched.
2. **Check and merge the branches.** `check-branch-versions` must pass for every
   branch before anything merges; then `merge-branches` merges them in order.
3. **Compute the release version after the merge.** The version is main's
   version as it stands after the merge, plus the release size. The release
   size the version step applies is patch today (the resolved release size is
   stored but not yet applied; see [Choose the release size](#choose-the-release-size)),
   so the next patch above main is used. The version is then fixed for the run.
4. **Make the release bump.** The version files, the changelog heading, the
   README line and one bump commit, then the release tag on that commit.
5. **Validate the bump.** The [release validator](#the-release-bump-validator)
   runs read-only before anything leaves the machine. A failing check stops the
   run here, so nothing is pushed.
6. **Push, fast-forward only.** The push refuses unless the remote's main is an
   ancestor of the local head. It never forces.
7. **Deploy.** A failure here does not fail the run.

The lock is released after the last step, whatever the result.

### Redo when main moved

If the push is refused because the remote's main moved while the run worked,
the run redoes itself rather than failing:

- its own merge commits and release bump are undone, and the run's local tag is
  deleted;
- local main is fast-forwarded to the remote's main (a diverged local main stops
  the redo and fails the run);
- the steps run again from the rebase step, so the release version is computed
  again from the moved main.

A run redoes at most **three times**. A fourth refusal fails the run with the
reason `main kept moving`. Each redo appears in the run record as a step named
`main-moved-redo-<n>`.

## Where to read the lock and the last result

Both the lock and the last release-validator result are on the merge status.
Read it on whichever surface you have:

| Surface | How to read it | Lock | Last validator result |
|---|---|---|---|
| PWA | The Merge pipeline card | The `Merge lock held: …` line | The validator line: pass or fail, the version, when it ran, and each failing check under it |
| REST | `GET /api/autonomous/merge` | `merge_lock` (absent when nothing is held) | `last_validator` (absent when no run has one) |
| MCP | `autonomous_merge` tool (read-only) | `merge_lock` | `last_validator` |
| Comm channel | `autonomous merge` | The `Merge lock held` line | The validator line |
| CLI | `datawatch autonomous merge` | The `Merge lock held` line | The validator line |

`last_validator` is the result of the newest run that reached the validate step,
with `pass`, `version`, `checked_at` and one entry per check (`name`, `pass`,
`detail`). A run that stopped before that step, or whose release steps were all
skipped, has none and is passed over, so the field shows the newest run that
has one, or is absent when no run has one.

Reading the status never changes anything: it merges nothing, deletes nothing
and pushes nothing.

## The built-in pipeline

When the file and the record are both empty for a project (or
`fallback_on_bad_file` is `default`), the built-in pipeline is in effect.
In order:

1. `rebase-onto-main` (`rebase_onto_main`) — bring each branch onto main, fetching main from the remote (if any) first. A branch already on main is left alone; the rebase of a conflicting branch stops the run with that branch untouched.
2. `check-branch-versions` (`check_branch_versions`) — run the project's merge-version check on every branch, nothing merged until each one passes.
3. `check-internal-refs` (`check_internal_refs`) — present only if the project's Makefile defines that target.
4. `merge-branches` (`merge_branches`) — merge each branch in order. A branch already contained in main is left in place and named as such in the step note. When every branch is already in main, the run has nothing new to release: the release steps 5 through 11 are each recorded as `skipped` (note `nothing new was released: every branch was already merged into main`), nothing is pushed, and the deploy step still runs.
5. `set-release-version` (`set_release_version`) — set the release version in every version file, after the merge. A file that already carries the run's version is left alone.
6. `add-changelog-heading` (`insert_changelog_heading`) — add the changelog heading for that version. A changelog that already has a heading for it is left alone.
7. `set-readme-release-line` (`set_readme_release_line`) — rewrite the version and date on the README's `Current release:` line to this release and today's date (UTC). The line must carry the shape `vX.Y.Z (YYYY-MM-DD)` — a line like `**Current release: v1.2.3 (2026-11-08).**` — or the step has nothing to rewrite and the run stops, naming the file and the missing shape. The rest of the line and file is left alone. A line that already names the version is left alone too. A README with no such line stops the run and names the file.
8. `commit-release-bump` (`commit_release_bump`) — one commit of the version files, the changelog and the README; with nothing staged no commit is made.
9. `tag-release` (`tag_release`) — tag that commit with the new version. A tag already on that commit is left alone. A tag of that name pointing at another commit fails the step, naming the tag and both commits — the daemon never moves a release tag, so resolving it is an operator call.
10. `validate-release-bump` (`validate_release_bump`) — before anything leaves the machine, run the release-bump validator over the bump the earlier steps made (see [The release-bump validator](#the-release-bump-validator) below). A failing check fails this step and names each check that failed, so the push never runs. The result is kept on the run record as `validator`.
11. `push-release` (`push_release`) — push main and the tag; fast-forward only.
12. `deploy-latest` (`deploy_latest`) — run the deploy command; a failure does not fail the run (`on_failure: continue`).

### Why every merge step is safe to run twice

**A step run twice changes nothing.** Each merge step checks what is already
done before it writes, so a second run never adds a second version bump, a
second changelog heading or a second commit. The release steps are safe to run twice: a step that finds its work already done — the version files already carry the version, the changelog already has the heading, the README line already names it, nothing is staged, the tag is already on that commit — succeeds as a no-op and leaves the files, the commit history and the tag list exactly as the earlier run left them. The one exception is a release tag someone moved to another commit: the tag step then fails and names both commits, and it is the operator who resolves the tag and re-runs.

### The release-bump validator

The `validate-release-bump` step runs after the bump commit and the release
tag exist, and before the push. The step calls `ValidateReleaseBump` in
[internal/mergepipeline/release_validator.go](../../internal/mergepipeline/release_validator.go),
which runs the five checks below and returns the result the run record keeps.
It only reads: it never writes a file, a tag or a remote. The glossary entry is
[Release validator](../datawatch-definitions.md#release-validator). Every check runs, even after one fails, so the result names each
check that failed. The five checks, in the order they run:

| Check name | What it requires |
|---|---|
| `version_variables_equal` | Every file in `version_files` carries the same `X.Y.Z` in its `var Version = "..."` line. A file with no such line fails the check. `version_files` has no default: with an empty list this check fails rather than guessing (see [Settings](#settings)). |
| `changelog_heading_matches` | The first `## vX.Y.Z` heading in `CHANGELOG.md` is that version. |
| `one_step_above_last_tag` | The version is exactly one patch, minor or major step above the newest local `vX.Y.Z` tag. The run's own release tag is not counted as the previous release. |
| `tag_not_taken` | The tag `vX.Y.Z` is not on `origin`, and any local tag of that name points at HEAD. If `origin` cannot be queried, the check fails. |
| `readme_line_matches` | The first `Current release:` line in `README.md` names that version. The check looks at the version only, not the date: a line that names the right version without the `(YYYY-MM-DD)` date still passes here — it is the `set-readme-release-line` step, not this validator, that requires the shape. |

The run record keeps the whole result under `validator`: `pass`, `version`,
`checked_at`, and one entry per check with its `name`, `pass` and `detail`.
A run that stopped before this step has no `validator` field; a run whose release steps are all `skipped` (nothing new to release) has none either.

The same checks can be run by hand, read-only, from a repository checkout with
`bash scripts/verify-release-bump.sh`. It prints one `PASS` or `FAIL` line per
check, in the order above, and exits 0 only when all five pass. Set
`VERIFY_RELEASE_REMOTE` to query a remote other than `origin`; if that remote is
not configured, only local tags are checked and the tag line says so.

Reading the script's output:

- Each line starts with `PASS` or `FAIL`, then the check name, a colon and a
  detail, for example `PASS one_step_above_last_tag: v… is one step above the
  newest tag v…`. A `FAIL` line's detail says what to change.
- The five lines come in the table's order, and every check runs even after one
  fails, so a single run shows all the problems.
- The exit status is `0` only when all five lines are `PASS`; `1` means at least
  one check failed. Use the status in a script; read the lines for the reason.
- The check names are the same as the run record's `validator` checks, so a
  `FAIL` line here matches a failed entry in the run record.

The script does not read `version_files`. It always checks `cmd/datawatch/main.go`
and `internal/server/api.go`, so on a project whose version lives elsewhere it
can disagree with the validator step. Use it as a check on this repository, not
as a substitute for the pipeline step.

**For operator review:** the built-in pipeline's `version_files` default is
empty, so a fresh project that runs the built-in pipeline fails at
`set-release-version` until its version files are named. Whether the built-in
default should name files, and whether the script should read the setting, are
open decisions.

**For operator review too:** the `set-readme-release-line` step needs the
`Current release:` line to already carry the `vX.Y.Z (YYYY-MM-DD)` shape —
for example `**Current release: v1.2.3 (2026-11-08).**`. If the project's
README line has no date, the step stops the run until the line is fixed; a
line that only names a version still passes the `readme_line_matches`
validator, so the two surfaces do not agree. Review the README line before the
first run of the built-in pipeline.

A run whose release tag has been moved to another commit fails the `tag-release` step (the daemon never moves a release tag); resolving the tag — or re-running after it is dealt with — is an operator call.

## How the release version is chosen

A branch ships into main: the release version is not a number the branch
carries, it is resolved at merge time.

- **Before anything merges**, the `check-branch-versions` step runs the
  project's merge-version check (`scripts/check-merge-version.sh`) for each
  branch, against the version on main (M). A branch passes if it left the
  version as it was at the merge base, or if it already carries M with one
  more patch. One that sets some other version of its own is refused and
  nothing merges, and the two version files must agree on main and on the
  branch wherever they are read.
- **After the branches have merged**, the `set-release-version` step computes
  the release version from main as it stands at that point — the next patch
  release above M — and writes it into every `version_files` entry. It runs
  exactly once per run, so a run that redoes from the rebase step sets the
  version from the moved main.
- The version is then fixed for the run: the `add-changelog-heading` step
  adds a changelog heading for it, the `tag-release` step names it in the tag,
  and the run record keeps it as `release_version`.

**The release size the version step applies is patch today.** The daemon merges
the next patch release above main. The release size and the approval mode for
each size are settings now (see [Choose the release size](#choose-the-release-size)
and [Release approval modes](#release-approval-modes)), but the version step
does not yet apply a resolved size; until it does, deciding what ships as
minor or major stays a manual operator call.

## When main moves before the push

Another machine can land commits on the remote's main while a run is working.
The push step is fast-forward-only: it lists and fetches the remote's branch
first, and refuses to push unless that head is an ancestor of the local head,
so a push can only move the remote forward. The refusal says why — it names
the head of the branch on the remote and the local head, and says that
nothing was pushed. The run does not fail for that alone. It undoes its own
work and starts again:

- The run's local merge commits and release bump are undone: local main is
  reset to the head it had when the lock was taken (or, after an earlier redo,
  the head that redo synced to). The run's local release tag is deleted.
- Local main is fast-forwarded to the remote's main. A local main that cannot
  fast-forward (the histories have diverged) stops the redo and fails the run.
- The steps run again from the rebase step, so the release version is computed
  from the moved main, and the changelog heading and bump commit are made once.
- Each redo is a run step named `main-moved-redo-<n>`, with what it reset and
  synced to. A run allows three redos. A fourth refusal fails the run with the
  reason `main kept moving`.

A redo refuses to reset local main when any commit above the base is not one
the run made, for example a commit someone made on local main during the run.
The run then fails and nothing is reset.

A redo can also end with nothing new to release: if main moved so that the
run's branches are already contained in it, the release steps are recorded as
`skipped` as above and the run finishes without a push.

## Move from shadow to on

A release size's merges are compared with the merges a person makes by hand
before the daemon ships that size on its own. The comparison records are the
evidence for the switch. This section says what that evidence must show, how
to read it on each surface, how a switch is recorded, what falls back after a
switch, and which parts are built now and which are still to come.

### The bar

Every release size (`patch`, `minor` and `major`) uses the same bar:

- at least 10 comparison records where the daemon's plan is equal to or better
  than the manual merge,
- at least 2 of those comparisons with conflicts,
- at least 2 of those comparisons with PWA changes,
- no comparison with a blocking defect.

The bar is fixed in this build. It cannot be changed in configuration yet.

### Read it on each surface

Each read gives one size's counts against the bar, the items still missing and
a Ready or Not ready flag. Ready means the bar is met. It does not change the
size's mode. Add a project to keep one project's comparisons only.

| Surface | How to read it |
|---|---|
| CLI | `datawatch merge-pipeline readiness --size patch\|minor\|major [--project P]` |
| REST | `GET /api/autonomous/release-readiness?size=patch\|minor\|major` with an optional `project` parameter |
| MCP | the `autonomous_release_readiness` tool, with `size` (required) and an optional `project` |
| Comm verb | `autonomous merge-pipeline release-readiness <patch\|minor\|major> [project]` |
| PWA | the Release readiness card on the Automation Setup sub-tab. It draws a size only once that size has at least one comparison record |

### Mode changes are recorded

Each release size has a release approval mode, set globally or for one project.
The modes are `auto`, `ask` and `manual`. Moving a size to on means setting its
mode to `auto`.

Every change to a size's mode is recorded as a decision that holds:

- who changed it: the requester and the surface the change came from (REST,
  MCP, CLI or comm),
- the old mode and the new mode,
- the evidence: that size's readiness counts at the time of the change.

Each change also emits a `release_mode_changed` audit event, which shows in
Events. Setting a size to the mode it already has is a no-op. Nothing is
recorded and no event is emitted.

### After a switch to on: falling back to ask (not built yet)

The intended design is that, after a size is set to `auto`, the daemon falls
back to `ask` for that size and raises an alert when:

- a validator fails,
- a release CI run goes red within the window, or
- a merge needed a manual fix.

None of this is in the build yet. The release gate does hold a run when one of
its release checks fails, in any mode. It does not change the size's mode and
it does not raise an alert. Nothing detects a red release CI within the window
or a manual fix. Until that work lands, a size set to `auto` stays `auto` after
a failure. Watch the card and Events after each switch.

### What is built and what is still to come

Built now:

- the bar and the readiness read on the CLI, REST, MCP, comm verb and PWA card,
- the decision record and the `release_mode_changed` audit event for each mode
  change made through the REST, MCP and comm surfaces,
- the release gate: a failed release check holds the run in any mode, `manual`
  holds for a person, and `ask` raises one decision question and ships only
  after a yes.

Still to come:

- the writer of the comparison records. No code in this build writes them yet,
  so the readiness read counts zero until that writer is wired,
- a configurable bar,
- the fall-back to `ask` and its alert.

The mode setting has no shadow value. A size's mode is `auto`, `ask` or
`manual`, so the switch is the change from one of the other two to `auto`.

### To move a size

1. Check the card, or `datawatch merge-pipeline readiness --size S`, until the
   size shows Ready. Read any blocking defect it reports first.
2. Set that size's mode to `auto` in the merge pipeline card or the Autonomous
   settings. Do this one size at a time. The change records its own evidence.
3. Watch Events and the release gate for the next releases of that size.

To move a size back, set its mode to `ask` or `manual`. That change is recorded
the same way. Keep the major size on `ask` unless a person decides otherwise.

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
| `release_approval` | patch `auto`, minor `auto`, major `ask` | one mode per release size, each `auto`, `ask` or `manual`; a project's `release_approval` takes precedence over the global one |

A `release_approval` mode outside `auto`, `ask` and `manual` is refused the same way. A `release_cadence` other than `per_merge`, a `version_scheme` other than
`semver`, a `major_trigger` outside the three, or a `release_size_map` value
outside `patch`, `minor`, `major`, `none` is refused on every write surface
with the key named, and nothing is written.

Set them with `datawatch config set`, the `autonomous_config_set` MCP tool (its
`merge_pipeline_*` arguments, and `merge_pipeline_release_approval_patch|minor|major` for a mode, with `merge_pipeline_project` for one project), the `configure` chat verb (a mode outside the three is refused with the key named before it is stored), or Settings >
Automation > Merge pipeline in the PWA — the card's one-select-per-size section writes that project's override, and the settings page's one-per-size fields write the global value. The four release size keys each have a `?`
link there that opens the [Release size](../datawatch-definitions.md#release-size)
entry in the definitions. The other keys open their own entries for the pipeline
in the same file.
