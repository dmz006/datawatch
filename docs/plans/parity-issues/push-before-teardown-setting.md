# The kill-time save push setting is missing from the Android and iPhone apps

**Status:** draft, file only. No issue is filed; the supervisor files it at release.

## Affected area

One new daemon setting, `git.push_before_teardown` (boolean, default off). When a
session is killed, the daemon pushes the commits the session made itself that are
on no remote yet. Commits that another local branch also holds (a task branch cut
from a local main that is ahead of the remote) are never pushed, and the session
record says so. Before this setting the save push ran on every kill.

Where it lives:

- Config: `GitConfig.PushBeforeTeardown` in internal/config/config.go, key
  `git.push_before_teardown`, read live on every kill.
- Read and write: `GET /api/config` (the `git` block carries `push_before_teardown`)
  and `PUT /api/config` with the dotted key. The MCP `config_set` tool, the
  `datawatch config set` command and the `configure` chat verb reach it through
  the same path.
- PWA: Settings > General > Sessions card, toggle "Push a killed session's own
  commits", with a help line; locale keys `settings_git_push_before_teardown` and
  `settings_git_push_before_teardown_help` in every file under
  internal/server/web/locales.
- Session record: `push_state` is `not_pushed` and `push_warning` reads
  `not pushed: N of the M unpushed commit(s) on <branch> are not this session's own
  (another local branch holds them); push them yourself` when the push is refused.
- The same change writes `ended_at` and `end_note` on every session end (kill,
  natural complete or fail), so a session card can show how and when a session
  ended, not only for finalised records.

## What the apps need

- A toggle on the Settings > Sessions page for `git.push_before_teardown`, off by
  default, saving through `PUT /api/config`, with the help text above.
- Show `push_warning` on an ended session when it starts with `not pushed`.
- Show `ended_at` and `end_note` on an ended session detail.

## Surfaces

| Surface | State |
|---|---|
| Config file, REST, MCP, CLI, chat, PWA | done (daemon) |
| Android | open |
| iPhone | open |
| Federation | none: the setting is per daemon |
