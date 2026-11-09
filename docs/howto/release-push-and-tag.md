# Release: push and tag

When an Automaton finishes a release, the daemon can publish it to the remote
and tag it. Three settings control this. They sit next to the git settings in
**Settings > Sessions**, and each one has a `?` help entry that opens this page.

## Settings

| Setting | Web control | Default | What it does |
|---|---|---|---|
| `autonomous.push_and_tag` | Push and tag finished releases (switch) | off | On: the daemon pushes the release commit and creates its tag. Off: the release stays local. |
| `autonomous.release_path` | Release path (select) | `direct_push` | `direct_push` pushes straight to the branch. `pull_request` opens a pull request instead. |
| `git.release_ci_window_minutes` | CI window (minutes) | 60 | Whole minutes the daemon waits for CI on a release before it stops waiting. Must be greater than 0. |

The same keys can be set from the REST config endpoints, the
`datawatch config set` command, and the comm channel `configure` verb.

## Last-release status

When a release record exists, the release block shows a line such as
`Last release: v1.2.3`. With no release record, that line is not shown.

## Choosing a release path

- Use `direct_push` when the branch is yours to update and CI gates run after the
  push.
- Use `pull_request` when the branch is protected. The release then lands only
  after the pull request is merged.

## Related

- [Automata overview](automation-tab.md)
- [Autonomous planning](autonomous-planning.md)
