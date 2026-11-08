# Release size and milestones

The merge pipeline decides how far each merge moves the version. You set the
rules once per project in **Settings > Automation > Merge pipeline**. Each
field has a `?` link that opens this page.

## The settings

| Setting | What it does | Values |
|---|---|---|
| Release size by kind | Maps an Automaton kind to the step its merge adds. | Rows of kind and size; sizes are `patch`, `minor`, `major`, `none`. |
| Major release trigger | When a major step is raised. | `manual` (default), `milestone`, `never` |
| Release cadence | How often a release is cut. | `per_merge` (available); `batch` and `manual` are refused for now. |
| Version scheme | How version numbers are written. | `semver` (available). |

The built-in size map is `feature` = minor and `bug` = patch. A row you add
either adds a kind or overrides one of these. A kind you do not list keeps the
built-in size, and a kind with no size has no entry.

## Majors are never inferred

A major release is not worked out from a diff or from a kind. It applies only
when you choose it:

- put `major` on a kind in the size map, or
- use the milestone trigger (below).

## Milestones

An Automation is the arc that groups the Automata building one larger feature.
With the trigger set to `milestone`, a major step is raised when the Automation
an Automaton belongs to is complete. Set it to `manual` to raise a major only
when you name it, or to `never` to never raise one.

## Cadence and scheme

`per_merge` cuts a release for every merge. Choosing `batch` or `manual` is
accepted in the list so you can see them, but saving either one is refused and
the daemon's reason is shown beside the field. The same applies to any version
scheme other than `semver`.

## Saving

- Major trigger, release cadence and version scheme save when you change them.
  A refused value is shown with the reason, and the control goes back to the
  value in effect.
- Release size by kind saves with its own **Save release sizes** button. A row
  with no kind, or a kind listed twice, is refused before anything is sent.

The same values can be set in the config file under
`autonomous.merge_pipeline` (see the configuration reference) and through the
config tools.
