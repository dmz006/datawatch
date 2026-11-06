---
docs:
  index: true
  topics: [merge, pipeline, ship, hooks, parity, registry, automata, autonomous]
exec_params:
  - {name: project, required: false, description: "Project name; leave empty for the daemon-wide values"}
exec_steps:
  - tool: autonomous_merge_pipeline_get
    description: Read the pipeline in effect for the project, where it came from and why; the post_ship block shows whether it is on, how many hooks are defined and the names of the ones that would run
    args: {project: "{{params.project}}"}
    read_only: true
---
# How-to: Post-ship hooks

Post-ship hooks are the small steps a project runs after the ship is done —
after merge, version bump, tag, push and deploy have all succeeded. They finish
what a project does by hand today: updating its plans registry row, filing a
parity issue in its companion repository, tagging the release where the project
wants it tagged, or notifying people.

They attach to the [merge pipeline](merge-pipeline.md): a project that lists no
hooks, or leaves them disabled, ships exactly as it does today. Hooks run only
after the pipeline's push step has succeeded. A hook that fails never blocks or
fails the ship — the ship is done — its failure is recorded on the hook's step
with its detail redacted, the run keeps the result the pipeline's steps set,
and the later hooks still run.

## Enabling

Post-ship hooks are off by default. Two switches gate each hook:

1. The card's own switch, `autonomous.merge_pipeline.post_ship.enabled` — off by
   default, it turns the whole block off.
2. Each hook's own switch in the list — a listed hook does nothing until you
   switch it on; a disabled hook produces no step in the run at all.

Nothing turns on by being mentioned: a built-in whose target setting is empty
fails its step with the missing setting named, and the daemon never guesses a
registry file, a repository or a message body.

## The hook list

The hook list is one array, `autonomous.merge_pipeline.post_ship.hooks`, in run
order. Each entry has:

| Field | Meaning |
|---|---|
| `name` | A label for the hook, shown in the run record. |
| `builtin` | One of the four built-in actions, or empty for a command hook. |
| `command` | Any shell command line, or empty for a built-in hook. Give this or `builtin`, never both. |
| `enabled` | The hook's own switch. Off by default. |

In the PWA (Settings > Automation > Post-ship hooks) you add hooks one at a
time, set each to a built-in or a command, order them with the up and down
buttons and toggle each one, then press **Save hooks** to send the whole list.
The daemon re-checks every rule on the way in — the name, exactly one of
built-in or command, a target for an enabled built-in, and the maximum of 20
hooks — and its error message comes back as a toast if the list is not
acceptable.

The four built-ins:

| Built-in | What it does |
|---|---|
| `registry_row` | Finds the project's row in the registry file and marks its status cell shipped, with the version. |
| `parity_issue` | Opens a parity issue in the companion repository, titled by the title template, with the notify template as its body. |
| `tag` | Tags the release `vVERSION`; skips rather than fails when the tag already exists. |
| `notify` | Sends the rendered message through the daemon's notifier (its alert channel); the step fails rather than drops the message when no notifier is wired. |

## The built-in targets

Every target a built-in touches is a setting. The `post_ship` block is **global**:
one list and one set of targets for all projects, so unlike the other merge
pipeline keys it has no per-project (`projects.<project>.`) form. The
`{project}` and `{version}` placeholders in a template are substituted at run
time with the project name and the shipped version; any other `{placeholder}`
is left as written.

| Key | Used by | Meaning |
|---|---|---|
| `registry_file` | `registry_row` | The registry file, relative to the project, whose row is updated. May contain `{project}`. |
| `registry_row_pattern` | `registry_row` | The text that must match exactly one row of that file; the row's status cell is what gets updated. May contain `{project}` and `{version}`. |
| `companion_repository` | `parity_issue` | The owner/name the issue is filed in — the companion, never the project that was merged. |
| `issue_title_template` | `parity_issue` | The issue's title. May contain `{project}` and `{version}`. |
| `notify_template` | `notify` | The message body. Also the parity issue's body when set — without it the issue gets a default one. May contain `{project}` and `{version}`. |

All keys live under `autonomous.merge_pipeline.post_ship` and can be set with
`datawatch config set` (for example `datawatch config set
autonomous.merge_pipeline.post_ship.enabled true`), the same `PUT /api/config`
the card writes to, or the card itself in the PWA. The card's help links open
the sections of this page.

## Reading the results

Every enabled hook is one step in the merge run record — kind `post_ship_hook`,
with its name, start and end, result and a short account of what it did. The
run records show in the PWA's merge status view and come back from
`GET /api/autonomous/merge` (the `pipeline_runs` field). A failed hook's step carries its redacted
error, so the missing setting or the reason for the failure is readable in
the record; a hook that skips (an already-existing tag) reads as a success
with the reason.

## See also

- [How-to: Merge pipeline](merge-pipeline.md) — the pipeline the hooks attach to.
- Settings > Automation > Post-ship hooks — the card in the PWA.
