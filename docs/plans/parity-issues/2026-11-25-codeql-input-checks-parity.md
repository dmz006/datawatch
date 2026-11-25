# Parity draft: stricter input checks from the code scanning fixes

Draft only. No issue is filed.

## What changed in the daemon

- `POST /api/supervisor-setup` answers 400 for a schedule name that holds a
  path separator, `..` or a control character.
- `POST /api/autonomous/branch-reap/restore` and
  `POST /api/autonomous/remote-reap/restore` answer 400 for a value that is not
  a valid git branch name (before: 500 or 404).

## What the mobile client needs

No new field, setting or screen. A client that offers supervisor set-up or a
branch restore should show the 400 message as it is, like any other refusal.

The web app change removes an unused path that wrote a translation as markup.
Nothing visible changes, so there is nothing to mirror.
