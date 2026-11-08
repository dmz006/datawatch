# Release size and milestones

The merge pipeline decides how far each merge moves the version. You set the
rules once per project in **Settings > Automation > Merge pipeline**. Each
field has a `?` link that opens this page.

## The settings

| Setting | What it does | Values |
|---|---|---|
| Release size by kind | Maps an Automaton kind to the step its merge adds. | Rows of kind and size; `patch`, `minor`, `none` take effect, `major` is accepted but never used for a kind row (majors are explicit or a milestone). |
| Major release trigger | When a major step is raised. | `manual` (default), `milestone`, `never` |
| Release cadence | How often a release is cut. | `per_merge` (available); `batch` and `manual` are refused for now. |
| Version scheme | How version numbers are written. | `semver` (available). |

The built-in size map is `feature` = minor and `bug` = patch. A row you add
either adds a kind or overrides one of these. A kind you do not list keeps the
built-in size; a kind that no layer names steps no version at all.

## Majors are never inferred

A major release is not worked out from a diff or from a kind. It applies only
when you choose it:

- put an explicit `release_size: major` on the Automaton itself — it applies
  under any major trigger, even `never`; or
- let the milestone trigger (below) raise it.

A `major` row in the size map is stored but never used: a kind whose row says
`major` falls back to the built-in for that kind (feature is minor, bug is
patch), or to no version step for any other kind.

Separately from the size, an Automaton marked `breaking_change` is reported as
one whose merge must go through the major approval mode, whatever step it
lands on. That is how a breaking change stays visible to you even when it
ships as a patch.

## Milestones

An Automation is the arc that groups the Automata building one larger feature.
Mark an Automation as a `milestone` when it is a major feature being grown out
of the minors and patches that land under it. With the trigger set to
`milestone`, a major step is raised when such a marked Automation is complete
(every member has finished). Set the trigger to `manual` to raise a major only
when you name it, or to `never` so that a milestone never raises one either.

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

## Status in this release

The keys and the record flags are stored, and the daemon resolves the size per
Automaton — a pure function of the Automaton's `release_size` and
`breaking_change`, the arc's `milestone` mark and the project settings, the
same answer on every surface. What is not wired yet: the merge runner's
version-bump step that consumes the resolved size, and the release approval
modes the `breaking_change` flag points at — both land in the work that
follows this one. Until then, nothing set here changes what a ship bumps.
