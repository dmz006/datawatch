# Project link and project membership order

- **Date:** 2026-11-12
- **Scope:** Make the Projects page and every project roll-up place an Automaton by an explicit project link first, add the controls to set or clear that link on every surface, group Automata that no step places, and link existing Automata with a reported backfill.
- **Related:** [docs/plans/2026-11-06-automata-pages-redesign.md](2026-11-06-automata-pages-redesign.md) (the Projects sub-tab)

An Automaton belongs to the first project that matches, in this order: its own project link, the project of the Automation holding it, the project of its parent Automaton, then a directory match as the fallback (a directory under a worktree maps to its repository). A link to a different project always wins over a directory match. An Automaton no step places is listed once in the **Automata without a project** group, which is in no project's total, so nothing is invisible.

## Scope

Files and packages affected:

- `internal/autonomous` — membership resolution and its source, `project_link_set.go` (set or clear, decision-log entry), `project_link_backfill.go` (dry-run report, apply, idempotence, done marker), `api.go`.
- `internal/server` — `autonomous_project_link.go` (REST handlers), `autonomous_view.go` (`project` and `project_source` on the read reply), roll-up `without_project` group, route registration in `server.go`, `docs/api/openapi.yaml` and `internal/server/web/openapi.yaml`.
- `internal/mcp` — `autonomous_project_link.go` (`autonomous_prd_set_project`, `autonomous_project_link_backfill`), tool registration and the registered-tool-names golden file.
- `cmd/datawatch` — `cli_autonomous_project_link.go` (`prd-set-project`, `project-link-backfill`), wiring in `cli_autonomous.go`.
- `internal/router` — comm verbs `automaton project` and `automaton project-backfill` in `commands.go` and `sx2_parity.go`.
- `internal/server/web` — `app.js` (Project field in Automaton settings, the group on the project page) and locale bundles `locales/{en,de,es,fr,ja}.json`.
- `docs/howto/projects-tab.md`, `docs/datawatch-definitions.md`, `docs/api-mcp-mapping.md`, `docs/commands.md`, `docs/howto/README.md`.

One configuration field is added: `autonomous.project_link_backfill_apply` (default `false`, a dry run at start).

## Phases

### Phase 1 — Membership order in every roll-up — Done

Link, then Automation, then parent, then directory; a link beats a directory match; an unknown link name is ignored; templates are never counted. The same decision feeds the project page, arcs, Gantt, History, progress snapshots, cost grouping and the Events project filter.

- [x] Functionality implemented and tests passing.
- [x] Docs: [howto/projects-tab.md](../howto/projects-tab.md#which-automata-belong-to-a-project).

### Phase 2 — The group for Automata without a project — Done

`without_project` in the roll-up response; each Automaton appears once with its own tasks, points and throughput; the group is outside every project's total.

- [x] Functionality implemented and tests passing.
- [x] Docs: howto and the *Automaton without a project* definition.

### Phase 3 — Set or clear the link — Done

REST `POST /api/autonomous/prds/{id}/set_project`, MCP `autonomous_prd_set_project`, CLI `datawatch autonomous prd-set-project`, comm verb `automaton project <id> <name|clear>`. An unknown name is refused with the configured names; each change writes a decision-log entry; no session is touched. The read reply reports `project` and `project_source`.

- [x] Functionality implemented on REST, MCP, CLI and comm channel, with a parity test.
- [x] OpenAPI and MCP tool listing updated.

### Phase 4 — The PWA control — Done

The Project field in Automaton settings (shows the source, **Save project**, **Clear link**) and the group on the project page with a hint to set a link.

- [x] Functionality implemented; five locale bundles updated.
- [x] Web tests added.

### Phase 5 — Backfill — Done

A dry-run report first (entries with project, source and result; scanned, matched, applied, skipped, failed). It links existing Automata whose Automation, parent or repository directory identify the datawatch project, never changes an existing link, skips templates, writes one `project_link_backfill` decision-log entry per link, and is idempotent. REST `POST` and `GET /api/autonomous/project-link-backfill`, MCP `autonomous_project_link_backfill`, CLI `datawatch autonomous project-link-backfill [--apply]`, comm verb `automaton project-backfill [apply]`. At start it runs as a dry run unless `autonomous.project_link_backfill_apply` is `true`.

- [x] Functionality implemented and tests passing.
- [x] Docs: howto, definitions, mapping and commands.

### Phase 6 — Documentation set — Done

Projects how-to (front matter topics and MCP steps for search, membership order, link controls, backfill), definitions (*Project membership*, *Project link*, *Automaton without a project*, *Project membership source*, *Project link backfill*), `docs/api-mcp-mapping.md` and `docs/commands.md` rows, and the how-to index line.

- [x] Docs checklist complete.

### Phase 7 — Mobile parity — Planned

The Android and iPhone apps need the Project field and the group, and must ignore `project_source` and `without_project` safely. Tracked in the datawatch-app repository.

## Parity surface

| Surface | Status | Note |
|---|---|---|
| REST | Touched | `set_project`, `project-link-backfill` (`POST`, `GET`), and `project` / `project_source` on the read reply |
| MCP | Touched | `autonomous_prd_set_project`, `autonomous_project_link_backfill` |
| CLI | Touched | `prd-set-project`, `project-link-backfill [--apply]` |
| Comm channel | Touched | `automaton project`, `automaton project-backfill` |
| YAML / config | Touched | `autonomous.project_link_backfill_apply` (default `false`) |
| PWA | Touched | Project field in Automaton settings; group on the project page |
| Android | Excluded from this change | Mobile parity is tracked in the datawatch-app repository |
| iPhone | Excluded from this change | Mobile parity is tracked in the datawatch-app repository |
