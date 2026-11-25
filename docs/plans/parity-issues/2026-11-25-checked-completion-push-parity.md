# Parity draft: checked completion pushes (datawatch-app)

Status: draft only; no issue filed.

## What changed in the daemon

- Finishing work never moves the remote's default branch and never creates or
  pushes a tag. `autonomous.ship.tag` is not applied at completion, and
  `git.push_on_complete=default_branch` pushes nothing.
- Every daemon push goes through the outgoing check. A refused push keeps
  `push_state: not_pushed`; `push_warning` carries the reason. Two new refusal
  reasons exist beside the earlier ones: the project check (or a check that
  could not run), and a refused default-branch push.

No endpoint, field or setting was added or removed.

## Client work

1. Show `push_warning` as free text; do not assume a fixed set of reasons.
2. Where the app edits `autonomous.ship.push`, `autonomous.ship.tag` or
   `git.push_on_complete`, use the daemon web app's new setting labels
   (`settings_ship_push`, `settings_ship_tag`,
   `settings_git_push_on_complete`), which say what completion no longer does.

## Surfaces

| Surface | Change |
|---|---|
| Android | labels and free-text warning |
| iPhone | labels and free-text warning |
