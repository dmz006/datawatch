# Parity draft (datawatch-app): Automata tag repair

Draft only. No issue is filed.

## What changed in the daemon
- `POST /api/autonomous/tags/repair` — body `{dry_run, copies}`; a dry run unless `dry_run` is `false`.
  The reply lists each tag ID that has no record with `restore`, `merge` or `unresolved`.
- Tag records are no longer dropped by a restart, another tag edit or a peer sync.
- Registry tags can carry `local: true` (a tag this daemon wrote itself, such as an auto tag).
  It appears in `GET /api/autonomous/tags/registry`; clients may ignore it.

## Mobile client
- No screen is required: the repair is a one-time maintenance action, run from the CLI.
- Check that the client ignores unknown tag fields.
