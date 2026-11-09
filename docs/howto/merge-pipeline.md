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
| `action` | A built-in: `rebase_onto_main`, `check_branch_versions`, `merge_branches`, `set_release_version`, `insert_changelog_heading`, `commit_release_bump`, `tag_release`, `validate_release_bump`, `push_release` or `deploy_latest`. |
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

## One merge at a time: the merge lock

Two merge runs must never ship into the same base repository at once. Before a
run starts, it takes a lock on its repository path; the lock is released when
the run ends, whatever the result.

- **A second run is refused.** Its run record is marked failed, and its reason
  names the run that holds the lock, for example `another merge holds this
  repository: run <id> (Automaton <id>)`. Nothing is merged, and the holder is
  not touched. Wait for the holding run to finish, then start again.
- **The held lock is visible.** While a merge runs, the merge status shows the
  lock: `Merge lock held: run <id> for Automaton <id> (branches …) since <time>`.
  The same information is in the `merge_lock` field of `GET /api/autonomous/merge`
  (absent when nothing is held).
- **The lock survives a restart.** It is kept in `merge-locks.json` in the daemon
  store directory. After a daemon restart the lock still shows as held. A run
  that is resumed takes its own lock back, so it is not refused by its own
  earlier lock.
- **A broken lock file refuses merges; it never lets them run unlocked.** If
  `merge-locks.json` is unreadable or corrupt, every merge run is refused with a
  message that the lock store is unavailable. Repair the file or restore it from
  a backup in the daemon store directory, then start the run again. Deleting the
  file clears all locks, so only do that when you are sure no merge is running.

## The built-in pipeline

When the file and the record are both empty for a project (or
`fallback_on_bad_file` is `default`), the built-in pipeline is in effect.
In order:

1. `rebase-onto-main` (`rebase_onto_main`) — bring each branch onto main, fetching main from the remote (if any) first. A branch already on main is left alone; the rebase of a conflicting branch stops the run with that branch untouched.
2. `check-branch-versions` (`check_branch_versions`) — run the project's merge-version check on every branch, nothing merged until each one passes.
3. `check-internal-refs` (`check_internal_refs`) — present only if the project's Makefile defines that target.
4. `merge-branches` (`merge_branches`) — merge each branch in order.
5. `set-release-version` (`set_release_version`) — set the release version in every version file, after the merge.
6. `add-changelog-heading` (`insert_changelog_heading`) — add the changelog heading for that version.
7. `commit-release-bump` (`commit_release_bump`) — one commit of the version files and the changelog.
8. `tag-release` (`tag_release`) — tag that commit with the new version.
9. `validate-release-bump` (`validate_release_bump`) — before anything leaves the machine, run the release-bump validator over the bump the earlier steps made (see [The release-bump validator](#the-release-bump-validator) below). A failing check fails this step and names each check that failed, so the push never runs. The result is kept on the run record as `validator`.
10. `push-release` (`push_release`) — push main and the tag; fast-forward only.
11. `deploy-latest` (`deploy_latest`) — run the deploy command; a failure does not fail the run (`on_failure: continue`).

### The release-bump validator

The `validate-release-bump` step runs after the bump commit and the release
tag exist, and before the push. It only reads: it never writes a file, a tag
or a remote. Every check runs, even after one fails, so the result names each
check that failed. The five checks, in the order they run:

| Check name | What it requires |
|---|---|
| `version_variables_equal` | Every file in `version_files` (by default `cmd/datawatch/main.go` and `internal/server/api.go`) carries the same `X.Y.Z` in its `var Version = "..."` line. A file with no such line fails the check. |
| `changelog_heading_matches` | The first `## vX.Y.Z` heading in `CHANGELOG.md` is that version. |
| `one_step_above_last_tag` | The version is exactly one patch, minor or major step above the newest local `vX.Y.Z` tag. The run's own release tag is not counted as the previous release. |
| `tag_not_taken` | The tag `vX.Y.Z` is not on `origin`, and any local tag of that name points at HEAD. If `origin` cannot be queried, the check fails. |
| `readme_line_matches` | The first `Current release:` line in `README.md` names that version. |

The run record keeps the whole result under `validator`: `pass`, `version`,
`checked_at`, and one entry per check with its `name`, `pass` and `detail`.
A run that stopped before this step has no `validator` field.

The same checks can be run by hand, read-only, from a repository checkout with
`bash scripts/verify-release-bump.sh`. It prints one `PASS` or `FAIL` line per
check, in the order above, and exits 0 only when all five pass. Set
`VERIFY_RELEASE_REMOTE` to query a remote other than `origin`; if that remote is
not configured, only local tags are checked and the tag line says so.

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

**The release size is patch today, and there is no setting for it.** The
daemon merges the next patch release above main; there is no setting for
another size, and no approval mode for a size yet. The phase-4 design
records both as decided — sizes patch/minor/major, an approval mode for
each with defaults patch `auto`, minor `auto`, major `ask`, overridable per
project, and the release size resolving in layers
([plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md),
"Phase 4 design as decided (2026-11-07)") — but none of it is a key you can
set yet. Until it lands, deciding what ships as minor or major stays a
manual operator call.

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
| `version_files` | none; a pipeline that bumps the version fails that step until you name the files that carry the version line |

Set them with `datawatch config set`, the `autonomous_config_set` MCP tool (its
`merge_pipeline_*` arguments), the `configure` chat verb, or Settings >
Automation > Merge pipeline in the PWA. Each help link there opens the matching
entry in [the definitions](../datawatch-definitions.md#merge-pipeline).
