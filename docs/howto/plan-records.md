---
docs:
  index: true
  topics: [autonomous, plan-records, registry-rows, findings, projects, import]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the plan records settings (master switch, write-through and import conflict policy)
    args: {section: "autonomous"}
    read_only: true
  - tool: plan_record_list
    description: List the plan records, newest first
    args: {}
    read_only: true
---
# How-to: plan records, registry rows and findings kept by datawatch

A **plan record** is one item of the plans registry (a *registry row*) or one
problem raised by an Automaton or an operator (a *finding*), kept by the daemon
instead of as a numbered line in `docs/plans/README.md` or an entry in a
findings file. Operators and Automata read and change the same records through
the same surfaces. Terms are in
[datawatch-definitions.md](../datawatch-definitions.md#plan-records).

This guide covers what a record holds, how to name one, the five surfaces,
importing the old files, what happens when an Automaton ships, the settings,
and how to turn it off.

## What a record holds

| Field | Meaning |
|---|---|
| `title` | The human name. Unique inside one project (case-insensitive, trimmed). |
| `id` | A short readable id, unique in the installation. |
| `uid` | An identifier that never changes. |
| `kind` | `registry_row` or `finding`. |
| `project` | The registered project alias the record belongs to. Fixed when the record is created. |
| `state` | Registry rows: `open`, `planned`, `in_progress`, `blocked`, `shipped`, `done`, `deferred`, `cancelled`. Findings: `open`, `triaged`, `fixed`, `wontfix`, `duplicate`. |
| `body` | The notes. |
| `legacy_number` | The old registry row number, when the record came from the registry file. |
| `shipped_by`, `shipped_version` | The Automaton id and version that shipped a row. |
| `raised_by` | The Automaton id that raised a finding. |
| `links`, `aliases` | Links, and older titles that still find the record. |
| `needs_review`, `review_reason` | Set by an import when a conflict was resolved for you. |

Records are never deleted. Closing one is a state change, and a closed record
still resolves.

## Naming a record

Every surface takes a **reference**. It is tried in this order: id, uid,
`row <number>` (`row-<number>` in a URL), exact title, an older title, an
unambiguous title prefix. A reference that matches more than one record is not
guessed: REST answers 409, the CLI exits non-zero, and each lists the
candidates so you can repeat the call with an id.

## The surfaces

| Surface | How |
|---|---|
| REST | `GET` and `POST /api/plan-records/registry-rows` and `/api/plan-records/findings`; `GET` and `PUT` on `.../{ref}`; `GET /api/plan-records/search?q=`; `POST /api/plan-records/import[?dry_run=true]`. List filters: `project`, `state`, `legacy_number`, `limit` (default 50, most 500), `offset`. There is no delete route. Reads need `autonomous:read`, writes `autonomous:write`. |
| MCP | `plan_record_list`, `plan_record_get`, `plan_record_create`, `plan_record_update`, `plan_record_search`, `plan_record_import`. |
| CLI | `datawatch plan-record list\|get\|create\|update\|search\|import` (alias `plan-records`). `import` previews by default; add `--dry-run=false` to write. |
| Chat | `autonomous plan-records [registry-row\|finding] [project=P] [state=S]`, `autonomous plan-record get <ref>`, `create <json>`, `update <ref> <json>`, `search <text>`, `import [dry-run] [project=P] [source=S] [policy=P]`. Lists and searches reply with the ten best lines, title first. |
| PWA | The **Plan records** section of the project page in the Projects sub-tab: Registry rows and Findings views, a state filter, a search box, a detail panel to change the title, state, notes and the linked Automaton, **New record**, and **Import archive…** with a preview before anything is written. The section hides itself when plan records are off. |

Each created or changed record writes one `plan_record_created` or
`plan_record_updated` audit event; a real import writes one
`plan_record_imported` summary event.

## Creating and changing records

A new record needs a `kind`, a registered `project` alias and a `title`. The
daemon gives it its own id (never the old row number) and a uid, sets the
dates, and starts it in state `open`. A title already used in the same project
is refused; the same title in another project is fine. Project, kind and
`legacy_number` cannot be changed afterwards. Renaming a record keeps the old
title as an older title that still finds it.

```sh
# CLI: a registry row, then a finding
datawatch plan-record create --kind registry-row --project datawatch \
  --title "Plan records in the registry" --state planned --body "Keep rows in datawatch."
datawatch plan-record create --kind finding --project datawatch \
  --title "Import skipped a row" --raised-by <automaton-id>

# CLI: change by title, id or row number; flags you leave out are untouched
datawatch plan-record update "Plan records in the registry" --state in_progress
datawatch plan-record update "row 412" --title "Plan records, step one" --state shipped
```

On REST send the same fields as JSON: `POST /api/plan-records/registry-rows`
with `{"project":"datawatch","title":"...","state":"planned"}`, then
`PUT /api/plan-records/registry-rows/{ref}` with only the fields to change.
Over MCP use `plan_record_create` and `plan_record_update`
(`{"kind":"registry-row","ref":"row 412","state":"shipped"}`). In chat send
`autonomous plan-record create <json>` or `autonomous plan-record update <ref>
<json>`. In the web app use **New record** or open a record in the detail
panel.

## Finding a record

Use whatever you remember. `datawatch plan-record get "row 412"`,
`get plan-records-in-the-registry` (an id), `get "Plan records in"` (a title
prefix) and `get "Old title"` (an older title) all reach the same record when
the match is unique. When two projects use the same title, add `--project`
(`project` on the other surfaces) to choose. `datawatch plan-record search
<words>` looks through titles, older titles and notes, with title hits first,
and `list` filters by `project`, `state` or `legacy_number`.

## Importing the old files

The import reads the numbered rows of `docs/plans/README.md` and the findings
files (`docs/plans/**/*findings*.md` by default; the REST body field
`findings_glob` overrides it) of a registered project and creates one record
each.

1. Preview first: `datawatch plan-record import --project datawatch`, REST
   `POST /api/plan-records/import?dry_run=true`, or **Preview import** in the PWA.
   Read the counts (to create, unchanged, changed, conflicts, skipped) and each
   conflict.
2. Apply: `datawatch plan-record import --project datawatch --dry-run=false`,
   REST without `dry_run`, or **Apply import**.

The import only reads: the files stay in place as the archive. It is
idempotent, keyed by project, kind and `legacy_number` (rows) or
`legacy_source` (findings), so a second run creates nothing; when a source
changed since the first run it reports the record as changed and updates it
only when nobody edited the record in between. The project must be a
registered alias (an unknown one is refused with a message to register it
first) whose directory sits under an allowed project root. The default
project is `datawatch`; `source` is `registry`, `findings` or `all`.

The **conflict policy** applies to every conflict in a run: `suffix` (default)
appends ` (row <n>)` or ` (<file>)` and flags the record `needs_review`,
`rename` takes a new title from the caller, `skip` leaves the record out.
After an import, look through the records for the **Needs review** mark (`needs_review` true) and check each one.

## What happens when an Automaton ships

The merge pipeline's `registry_row` post-ship hook normally edits the registry
file. With **write-through** on, it records the shipped row as a plan record
instead and never touches the README:

- It looks for an existing registry row in the Automaton's project: first by
  the row number its row pattern carries, then by the pattern text and the
  Automaton title (exact title or an older title).
- A match is moved to `shipped` and gets `shipped_by` (the Automaton id) and
  `shipped_version`. No match creates a new registry row titled by the
  Automaton. The change is made by the actor `merge-pipeline`.
- A failed write fails the hook with a redacted message. It never falls back
  to editing the README.
- With write-through off (the default), or when no record store is wired, the
  hook behaves exactly as before.

**The supervisor.** With write-through on, a supervisor or any other Automaton
that needs to add or change a row does it through the `plan_record_*` tools:
`plan_record_search` or `plan_record_get` to find the row by title, id or
`row <n>`, then `plan_record_update` (or `plan_record_create` when there is
none) to set the state, the notes and `shipped_by`. It does not edit
`docs/plans/README.md`. With write-through off, the README stays the place to
edit and the tools are only for reading and for records you keep on purpose.
Only the operator turns the setting on or off.

Write-through changes only the registry-row hook. It does not write record
changes back into the plans files, and the files are not updated when a record
is edited by hand.

## Settings

All under `autonomous.plan_records.`; change them with `PUT /api/config`,
`datawatch config set`, the chat `configure` verb or the MCP `config_set`
tool. They take effect at once, and a bad value is refused. In the PWA they
are in **Settings → Automata**.

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `true` | Master switch. While `false` every `/api/plan-records` route answers 404 and the PWA section hides. |
| `write_through` | `false` | Make the `registry_row` post-ship hook record the shipped row as a plan record. |
| `import_conflict_policy` | `suffix` | `suffix`, `rename` or `skip`; used when an import names no policy. |

## Storage and security

One JSON file per record under `<data dir>/plan-records/<kind>/<uid>.json`,
written through a temporary file and an atomic rename; the index is rebuilt
from the files when the store opens. With secure mode on, each record is
sealed with the same file sealing the other per-record data files use.
Exactly these fields stay readable without the key, so records can still be
filtered: `kind`, `project`, `state`, `id`, `uid`, `legacy_number`, and the
dates `created_at`, `updated_at` and `closed_at`. Everything else (title,
notes, older titles, links, Automaton ids, review fields, legacy source and
shipped version) is only inside the sealed payload. Without secure mode the
files are plain JSON. See [encryption](../encryption.md).

## Turning it off, and rollback

Set `autonomous.plan_records.enabled` to `false` (or leave `write_through` off
to keep the registry file as the hook's target). Records are kept on disk.
Removing the `plan-records` directory under the data dir discards every
record; the original files are never changed by an import, so re-importing
rebuilds them. One caveat: rows shipped by write-through while it was on exist
only as records, so copy them back into the registry file by hand before you
remove the directory.

## Still for the operator to review

Nothing here turns on by itself. These are yours to settle:

- **Records marked `needs_review`.** After the first real import, look through
  the records an import flagged and settle each one (rename, merge or accept).
- **Write-through.** `autonomous.plan_records.write_through` stays off until
  you turn it on. Decide when the registry hook should write records instead of
  editing `docs/plans/README.md`.
- **The old files.** `docs/plans/README.md` and the findings files stay in
  place as the read-only archive. Whether and when to clean them up is a later
  step you decide.
- **Mobile apps.** Android and iPhone do not show plan records yet; that is
  tracked in the datawatch-app repository.

## If something looks wrong

- **404 from a plan record route, or the PWA section is missing.** Plan records
  are off: set `autonomous.plan_records.enabled` to `true`.
- **409 on a reference.** More than one record matches; repeat with an id.
- **Import refuses the project.** Register the alias first (Projects sub-tab or
  `datawatch projects upsert`), and check its directory is under an allowed
  project root.
- **A shipped row did not appear.** Check `write_through` is on and the hook
  result in the merge run; with it off the hook edits the README as before.
