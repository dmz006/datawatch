# Registry rows and findings as datawatch records

- Date: 2026-11-12
- Version when written: v9.149.0
- Scope: step one of moving the plans registry into datawatch. A registry row and a finding become records the daemon keeps, reachable from REST, MCP, the CLI, the comm channel and the web app, with a one-time import of the old files and an optional write-through at ship time.
- Design: [plans and backlog in datawatch](2026-11-10-plans-and-backlog-in-datawatch.md) holds the operator decisions this follows. The Projects page that shows the records is described in [the Automata pages redesign](2026-11-06-automata-pages-redesign.md).
- Release: shipped in the next patch above main. No version number is claimed here; the release path adds it.
- How-to: [plan records](../howto/plan-records.md).

## Out of scope

- Design documents. They stay as files in `docs/plans/`.
- Handover plans. They stay as files.
- Other projects. The records carry a `project` field and the import takes any registered alias, but only the datawatch project is imported and documented here.
- The cut-over and any cleanup of the plans folder. Those are later steps the operator decides; nothing here says when or whether they happen.
- Mobile parity (see the parity section).

## Phases

### Phase 1 — Record model and store (Done)

- [x] New package `internal/planrecords` with kind `registry_row` (states open, planned, in_progress, blocked, shipped, done, deferred, cancelled) and kind `finding` (open, triaged, fixed, wontfix, duplicate).
- [x] Short readable id unique in the installation, a uid that never changes, the old row number kept as `legacy_number`.
- [x] Title is the human handle and is shown first; unique within a project (case-insensitive, trimmed), repeatable across projects; a rename keeps the old title as an alias.
- [x] References resolve by id, uid, `row <n>`, exact title, older title or unambiguous title prefix; an ambiguous title returns the matches.
- [x] One JSON file per record under `plan-records/<kind>/<uid>.json`, written through a temp file and an atomic rename; files are the source of truth.
- [x] Records are never deleted; closing is a state change and sets `closed_at`.

### Phase 2 — Secure mode sealing (Done)

- [x] Under secure mode each record is sealed with the shared per-record file sealing (`internal/secfile`).
- [x] Only `kind`, `project`, `state`, `id`, `uid`, `legacy_number`, `created_at`, `updated_at` and `closed_at` stay readable; named in the code comment and in the docs.

### Phase 3 — Audit events (Done)

- [x] Every write emits `plan_record_created` or `plan_record_updated`; a real import emits a `plan_record_imported` summary.

### Phase 4 — REST, configuration and audit wiring (Done)

- [x] Routes under `/api/plan-records`: `registry-rows`, `findings`, `search`, `import`; no delete route.
- [x] Config block `autonomous.plan_records` with `enabled` (default true), `write_through` (default false) and `import_conflict_policy` (suffix, rename or skip; default suffix); changes take effect at once.

### Phase 5 — One-time import (Done)

- [x] Reads the numbered rows of `docs/plans/README.md` and the findings files; never writes them, so they remain the read-only archive.
- [x] Dry run first with the full conflict preview; idempotent by project, kind and `legacy_number` or `legacy_source`.
- [x] Conflict policies: suffix (the record is flagged `needs_review`), rename, skip; a run never stops half done.
- [x] Refuses an unregistered project alias and a directory outside the allowed project roots.

### Phase 6 — MCP, CLI and comm channel (Done)

- [x] MCP tools `plan_record_create`, `plan_record_get`, `plan_record_list`, `plan_record_update`, `plan_record_search`, `plan_record_import`.
- [x] CLI group `datawatch plan-record` (alias `plan-records`); `import` previews unless `--dry-run=false`.
- [x] Comm verb `plan-record` / `plan-records`.

### Phase 7 — Web app (Done)

- [x] Plan records section on the project page in the Projects sub-tab: Registry rows and Findings views, filter, search, detail panel, new record, import with a preview, needs-review mark.
- [x] Settings rows for the three keys and locale strings.

### Phase 8 — Write-through at ship time (Done)

- [x] With `write_through` on, the merge pipeline's `registry_row` post-ship hook moves the matching row to `shipped` (or creates one) as the actor `merge-pipeline` and does not edit the README; a failed write fails the hook with a redacted message.
- [x] With it off, the hook is unchanged.

### Phase 9 — Documentation (Done)

- [x] The how-to, the definitions entry, the MCP tool tables, the configuration table, the route mapping, the encryption coverage line, the operations note and the supervisor rules paragraph.

### Phase 10 — Operator review of the imported records (Planned)

- [ ] Run the import against the datawatch project, look through the records marked needs review and settle each one.
- [ ] Decide whether to turn write-through on. The setting stays off until the operator does.

### Phase 11 — Mobile parity in datawatch-app (Planned)

- [ ] Plan records view in the Android and iPhone apps, tracked in the datawatch-app repository.

## Parity surface

| Surface | Status | Notes |
|---|---|---|
| REST | Touched | `/api/plan-records/registry-rows`, `/findings`, `/search`, `/import`; OpenAPI updated. |
| MCP | Touched | Six `plan_record_*` tools; federated peers need `autonomous:read` or `autonomous:write`. |
| CLI | Touched | `datawatch plan-record list\|get\|create\|update\|search\|import`. |
| comm channel | Touched | `autonomous plan-record(s)` verb with list, get, create, update, search and import. |
| YAML/config | Touched | `autonomous.plan_records.enabled`, `write_through`, `import_conflict_policy`. |
| PWA | Touched | Plan records section on the Projects page, with settings rows and locale strings. |
| Android | Excluded from this change | Tracked in datawatch-app; the mobile apps follow the web app in a separate change set once the record shape is settled. |
| iPhone | Excluded from this change | Tracked in datawatch-app, same reason as Android. |
