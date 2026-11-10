# Automata Events tab

- **Date:** 2026-11-09
- **Version at planning time:** v9.119.0
- **Design record:** [docs/plans/2026-11-06-automata-pages-redesign.md](2026-11-06-automata-pages-redesign.md) (decision 2: the Events sub-tab sits before Templates; one stream with kind chips and a project filter; a status that is "off", "none" or "never" is not shown at all — it becomes an alert, an Events entry, or both)

The Events tab is one scrollable, searchable stream of what Automata and Automations are doing: watch notices, supervisor and lane decisions, infrastructure retries, Automata checks, cleanup, alerts and errors. It absorbs the log-shaped cards on the other Automata sub-tabs (supervisor watch, recent decisions, infrastructure retries, Automata checks, safe-to-delete report, cleanup status) and replaces the always-visible repo CI status line with alert/feed entries when the watch is off.

## Scope

Files and packages affected:

- `internal/server` — `automata_event_log.go` (record, merge order, cursor paging), `automata_event_log_watch_sources.go` (supervisor watch, lane decisions), `automata_event_log_retry_check_sources.go` (infrastructure retries, Automata checks), `automata_event_log_maintenance_sources.go` (safe-to-delete report, cleanup status), `automata_event_log_classify.go` (error classification, quiet handling), `automata_event_log_watch_off.go` (alerts + feed entries for off statuses), `automata_event_log_handler.go` (REST handler), route registration in `server.go`, plus their `_test.go` files and `automata_events_locale_test.go`, `v8_73_33_automata_terminology_test.go` (Automaton/Automata wording guard).
- `cmd/datawatch` — `cli_autonomous_event_log.go` (+ test), wiring in `cli_autonomous.go`.
- `internal/router` — `autonomous_event_log.go` (comm verb, + test), `commands.go`, `sx2_parity.go`.
- `internal/server/web` — `app.js` (sub-tab, stream rows, filters, moved cards, repo CI line removal), `app-automata-events-*.test.js` and the touched `app-*.test.js` suites, locale bundles `locales/{en,de,es,fr,ja}.json`, `openapi.yaml`.
- `docs/howto/automata-events.md`, `CHANGELOG.md`.

No configuration field is added; `docs/config-reference.yaml` is untouched.

## Phases

### Phase 1 — Event log record and merge order — Done (shipped with the Events tab)

`internal/server/automata_event_log.go`: the `AutomataEvent` record (stable id; kind; severity info/warn/error; UTC time; project, Automation, Automaton owner; one-line summary; reason; next action; quiet flag), the kind vocabulary `watch, decision, retry, check, cleanup, alert, error`, newest-first merging of several event slices, and the opaque `before=` cursor for paging backwards.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`automata_event_log_test.go`, classification, source and handler tests).
- [x] `go test ./...` and `node --test internal/server/web/*.test.js` green at merge.
- [x] Live/smoke test: N/A — in-memory read model, no persistence; covered by the handler and PWA tests below.
- [x] Docs checklist: covered by Phase 10.
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: N/A (no user-facing string in this phase).
- [x] Version: part of the Events tab release, no standalone tag.
- [x] This plan's phase status updated.

### Phase 2 — The event sources — Done

- Supervisor watch and lane decision records: `automata_event_log_watch_sources.go`.
- Infrastructure retries and Automata checks: `automata_event_log_retry_check_sources.go`.
- Safe-to-delete report and cleanup status: `automata_event_log_maintenance_sources.go`.

Each source renders its records into `AutomataEvent` rows (time, kind, owner, summary, reason, next action where one exists).

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`*_sources_test.go` for each of the three source files).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — pure rendering of existing records.
- [x] Docs checklist: covered by Phase 10.
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: N/A (labels ship in Phase 8/10).
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 3 — Error classification and quiet handling — Done

`automata_event_log_classify.go`: error entries are classified with what happened, the reason, and a next step that points at the place the operator fixes it; routine notices are marked quiet (one muted line with its reason, no action needed).

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`automata_event_log_classify_test.go`).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — classification is pure; rendered and exercised by the PWA error-row tests.
- [x] Docs checklist: covered by Phase 10 (the how-to explains quiet and error rows).
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: strings keyed in Phase 10.
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 4 — Off statuses become alerts and feed entries — Done

`automata_event_log_watch_off.go`: a watch or loop that is off emits an alert and an Events entry when it has something to say (a failing or pending run, a poll that has not happened) instead of a permanent status strip.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`automata_event_log_watch_off_test.go`).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — feed/alert emission is covered by the existing alert and feed tests plus the handler tests.
- [x] Docs checklist: covered by Phase 10.
- [x] Mobile-Parity Rule: alerts already reach every client; tracked in Phase 11.
- [x] Localization Rule: strings keyed in Phase 10.
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 5 — REST read endpoint — Done

`GET /api/autonomous/event-log` (`automata_event_log_handler.go`, registered in `server.go`): newest first, with `kind=` (repeatable), `project=`, `q=` (summary/reason text) and cursor paging via `limit=` + `before=`/`next_cursor`. Read-only; the route is listed in `openapi.yaml`.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`automata_event_log_handler_test.go`).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — the PWA and the CLI/comm verbs exercise the same handler.
- [x] Docs checklist: endpoint documented in `openapi.yaml`; CHANGELOG entry in Phase 10.
- [x] Mobile-Parity Rule: endpoint is platform-neutral; PWA change tracked in Phase 11.
- [x] Localization Rule: N/A (no new user-facing string).
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 6 — CLI command — Done

`datawatch autonomous event-log` (`cli_autonomous_event_log.go`, wired in `cli_autonomous.go`): `kind`, `project`, `q`, `limit` and `before` flags over the same endpoint, `--json` available.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`cli_autonomous_event_log_test.go`).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — flag-to-query mapping covered by the CLI test.
- [x] Docs checklist: CHANGELOG entry in Phase 10; CLI is named in the how-to.
- [x] Mobile-Parity Rule: N/A (operator-interface surface).
- [x] Localization Rule: N/A (command help text).
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 7 — Comm channel verb — Done

`autonomous event-log [kind=…] [project=…] [q=…] [limit=1..10] [before=<cursor>]` (`internal/router/autonomous_event_log.go`, registered in `commands.go`, named in `sx2_parity.go`): renders the matching events newest first and ends with the `before=` cursor for the next page.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`internal/router/autonomous_event_log_test.go`).
- [x] `go test ./...` green at merge.
- [x] Live/smoke test: N/A — routing and rendering covered by the router tests.
- [x] Docs checklist: CHANGELOG entry in Phase 10.
- [x] Mobile-Parity Rule: N/A (operator-interface surface).
- [x] Localization Rule: N/A (reply text built from event content).
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 8 — The Events sub-tab in the PWA — Done

`internal/server/web/app.js`: the Automata view's sub-tabs become Automation, Automata, **Events**, Templates. The stream shows time, kind, project and the owner (Automation and/or Automaton) per row; a row expands to its detail, reason and next action; kind chips toggle kinds, the project dropdown narrows to one project (All projects clears it), the debounced search matches summary and reason, and Load more follows `next_cursor`. Error entries render with what happened, the reason and a Next action button that opens the place to fix it; quiet entries render as one muted line.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (`app-automata-events-{subtab,stream-rows,errors,filters,moved-decisions,moved-maintenance,repo-ci-line,card-help}.test.js`).
- [x] `node --test internal/server/web/*.test.js` and `go test ./...` green at merge.
- [x] Live/smoke test: N/A — the stream reads the handler tested in Phase 5.
- [x] Docs checklist: how-to + CHANGELOG in Phase 10.
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: keys in all five bundles (Phase 10), guarded by `automata_events_locale_test.go`.
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 9 — Moved cards and the repo CI line — Done

The supervisor watch, recent decisions, infrastructure retries, Automata checks, safe-to-delete report and cleanup status panels move into the Events stream (Phase 8); each former card keeps a **?** link that opens the how-to. The always-visible "repo CI" status line under the sub-tab bar is removed: when the watch is off it shows nothing, and anything it would have said arrives as an alert, an Events entry, or both (Phase 4).

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (moved-card and repo CI line suites above; touched card suites updated).
- [x] `node --test internal/server/web/*.test.js` green at merge.
- [x] Live/smoke test: N/A — reads the same sources as the stream.
- [x] Docs checklist: the how-to's "Moved cards" section (Phase 10).
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: keys in all five bundles (Phase 10).
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 10 — Locale, help, openapi and docs — Done

New locale keys in all five bundles (`en`, `de`, `es`, `fr`, `ja`), guarded by `internal/server/automata_events_locale_test.go`; per-card **?** help anchors; the `openapi.yaml` route entry; the how-to `docs/howto/automata-events.md` (reading the stream, filtering, the moved cards); the `CHANGELOG.md` entry.

Checklist:

- [x] Functionality implemented per the phase's own description.
- [x] Unit tests added/passing (locale guard test).
- [x] `go test ./...` and `node --test internal/server/web/*.test.js` green at merge.
- [x] Live/smoke test: N/A (docs and locale wiring only).
- [x] Docs checklist: `CHANGELOG.md` entry present; `docs/config-reference.yaml` N/A (no new config field); `docs/operations.md` N/A (no deployment/security change); `README.md` + doc index — how-to is indexed; `docs/testing-tracker.md` N/A (read-only merge of existing records, contract covered by the endpoint tests); no leaked tracker IDs (checked); this plan's Parity surface section accurate.
- [x] Mobile-Parity Rule: tracked in Phase 11.
- [x] Localization Rule: every new `t('key')` has an entry in all five bundles.
- [x] Version: part of the Events tab release.
- [x] This plan's phase status updated.

### Phase 11 — Mobile parity tracking — Planned

The PWA changes in Phases 8–10 (new sub-tab, stream and filters, moved cards, removed repo CI line, new locale keys) are operator-visible, so the Mobile-Parity Rule requires the Android/iPhone clients (the `datawatch-app` repository) to follow: an app issue carrying the visible changes, the locale keys and acceptance criteria, or a comment on an existing redesign issue.

Checklist:

- [ ] `datawatch-app` issue filed or existing issue commented (per Mobile-Parity Rule "Action per change").
- [ ] Issue linked from the release notes.
- [ ] Other items: N/A (tracking only, no code).
- [x] This plan's phase status updated when the issue is filed.

## Parity surface

| Surface | Status | Detail / reason |
|---|---|---|
| REST | Touched | `GET /api/autonomous/event-log` — newest first, `kind=` (repeatable), `project=`, `q=` and cursor paging (`limit=` + `before=`/`next_cursor`); read-only; listed in `openapi.yaml`. |
| MCP | Excluded | The event log is a read-only operator-facing stream; no LLM tool needs to browse it and no status/stats tool exists for it, so no MCP tool is added. Operator access stays on the operator-facing surfaces (REST, CLI, comm, PWA). |
| CLI | Touched | `datawatch autonomous event-log` with `kind`, `project`, `q`, `limit` and `before` flags over the same endpoint (`--json` available). |
| Comm channel | Touched | `autonomous event-log [kind=…] [project=…] [q=…] [limit=1..10] [before=<cursor>]` over the same endpoint, with the next-page cursor in the reply. |
| YAML/config | Excluded | The Events change has no configuration setting: the stream, its kinds, filters and paging are fixed behavior, so there is nothing to add to `config.yaml` and no config round-trip to keep in parity. |
| PWA | Touched | The Events sub-tab (before Templates), stream rows with detail/reason/next action, kind chips, project filter, search, Load more, the moved log-shaped cards, the removed repo CI status line, and the new locale keys in all five bundles. |
| Android | Excluded from this change, tracked | No Android code lives in this repository; the PWA is the reference implementation. Tracked for the `datawatch-app` repository per the Mobile-Parity Rule (Phase 11): the Events tab, filters, moved cards and locale keys. |
| iPhone | Excluded from this change, tracked | Same as Android; the Compose-Multiplatform app is the single mobile target (Phase 11). |
