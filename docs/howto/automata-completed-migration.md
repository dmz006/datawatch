---
docs:
  index: true
  topics: [autonomous, prd, automation, completed, cancelled, migration, merged, shipped]
exec_params:
  - {name: id, required: false, description: "Restrict the run to one Automaton or Automation (single-record mode also accepts blocked or failed Automata)"}
  - {name: dry_run, required: false, description: "List what would change without writing: true or false (default false)"}
  - {name: limit, required: false, description: "Most records one run changes, oldest first; 0 = no limit (default 0)"}
exec_steps:
  - tool: autonomous_completed_migrate
    description: Move cancelled Automata (and Automations) whose work is already merged to Completed; run with dry_run first
    args:
      id: "{{params.id}}"
      dry_run: "{{params.dry_run}}"
      limit: "{{params.limit}}"
    read_only: false
  - tool: autonomous_shipped_counts
    description: Read the per-status counts afterwards to see the cancelled bucket shrink
    args: {}
    read_only: true
---

# How-to: Show finished Automata as Completed

## 1. When you need it

An Automaton whose branch merged to the default branch can still end up
`cancelled` — for example it was cancelled after its merge landed. Such an
Automaton did its work, so it should read **Completed**. The **completed
migration** moves it there:

- Source status: `cancelled` in a bulk run. With `id=`, a `blocked` or
  `failed` Automaton (Automations: `blocked`) is also accepted.
- Evidence required: the record carries a **shipped record**, or the
  read-only merge proof finds its branch (`automaton/<id>`) merged on the
  default branch. A cancelled record with neither is **left cancelled**.
- Completion time: the shipped record's merge time, or the merge commit's
  commit time — **never the time you ran the migration**.
- An Automation moves when every non-skipped member is merged or completed.
- Each changed record gets a `completed_migration` decision with the reason
  and commit, so the history shows why the status changed.

It never starts, stops, kills or signals a session, and no git command
writes.

## 2. Run it — dry run first

```
$ datawatch autonomous completed-migrate --dry-run
$ datawatch autonomous completed-migrate --limit 20
$ datawatch autonomous completed-migrate --id a1b2c3d4
```

| Surface | Call |
|---------|------|
| CLI | `datawatch autonomous completed-migrate [--dry-run] [--limit N] [--id <id>]` |
| REST | `POST /api/autonomous/completed/migrate?dry_run=true&limit=N&id=<id>` (query or JSON body; query wins) |
| MCP | `autonomous_completed_migrate` (`dry_run`, `limit`, `id`) |
| Comm | `autonomous completed-migrate dry_run=true limit=N id=<id>` |

Records are taken oldest first, so repeated `limit` runs make progress.
Bad `dry_run` or `limit` values are a 400. A real run while a **final gate
is running** is refused with a **409** — retry when the gate finishes (a dry
run still reports, with `busy: true`).

## 3. Reading the report

`considered`, `changed`, `would_complete` (dry run) or `completed` (real run;
each item has title, kind, from, to, `completed_at`, commit and reason),
`already_completed`, `left_cancelled`, `left_other` (single-id mode),
`deferred` (past `limit`) and `errors`. Re-running is safe: records already
moved count under `already_completed`.

## 4. Relationship to the shipped status

[The shipped status](automata-shipped-status.md) records *that work landed*
and never touches a status. Every view — the web app chip and badges, the
counts, the lists and the text output — already **displays** a record that
carries a shipped record as **Completed**, even while its stored status is
still `cancelled`; there is no separate Shipped label or bucket. The
migration is the one thing that changes the *stored* status of a
merged-but-cancelled record, and it leaves the shipped fields alone. After it
the record is `completed` in the store too (and still carries its release,
merge time and merge commit). A cancelled record whose work never merged is
untouched and still reads Cancelled.

## 5. For the operator to review

- The display change is not gated by the migration: counts and chips already
  show merged-but-cancelled work as Completed. The migration only aligns the
  stored status, and it needs a shipped record or a merge proof.
- Run `--dry-run` and read each `reason` before the real run; the migration
  is not reversible from the surfaces (no un-complete verb).
- It is a manual action: no setting makes it run on its own.
- Mobile companion apps do not yet have a surface for it, and they still use
  the old Shipped label and bucket until they pick up the Completed display
  (parity tracked in datawatch-app).

## See also

- [The shipped status](automata-shipped-status.md)
- [Merged-member policy](automation-merged-member.md)
- [Completion review and auto-fix](automata-completion-review-autofix.md)
- [Definitions: Completed migration](../datawatch-definitions.md#completed-migration)
