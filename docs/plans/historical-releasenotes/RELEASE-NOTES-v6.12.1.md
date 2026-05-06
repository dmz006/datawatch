# Release Notes — v6.12.1

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.1

### Summary — close BL268-273 + BL275-276 + automata smoke

Closes the v6.12.0 deferral list and the new operator-filed items (BL275 stale Council text, BL276 Council personas).

### Closed

- **BL273** — daemon-side `/docs/` static file server. Mirror canonical `docs/datawatch-definitions.md` into `internal/server/web/docs/` via `scripts/sync-docs-to-webfs.sh`; `release-smoke.sh` invokes `--check` mode at the prelude. Help icons + Settings/About now stay in-app via `/diagrams.html` (the existing markdown renderer, retitled "Documentation" with the definitions doc as default landing).
- **BL269** — Automata help button now opens `/diagrams.html#docs/datawatch-definitions.md#automata`.
- **BL270** — `.select-bar-fixed` z-index 350 + box-shadow + auto-hide of `.new-session-fab` while bar is up. Bar always sits above bottom nav and FAB.
- **BL271** — New-automaton wizard: prominent "Start from template" strip at top (operator: "should appear first, before the free-form fields"); footer cleaned up to Cancel + Launch only.
- **BL272** — `.settings-section` inner padding + border + `+ .settings-section { margin-top }` normalized so all listed cards (Observer + Settings General/Plugins/Comms/LLM/Agents/Automate) share consistent visual rhythm.
- **BL275** — dropped "v6.11.0 ships the framework with stubbed responses" from the Council card intro (council shipped real LLM debate plumbing).
- **BL276** — added 4 new default personas: `platform-engineer`, `network-engineer`, `data-architect`, `privacy`. Total goes 6 → 10. New `.seeded` marker file + additive seeding so future default personas are written to disk on next start without re-creating ones the operator deliberately deleted. Council card now exposes "View / edit personas" button → modal listing every loaded persona's name, role, system_prompt + on-disk path.
- **About page** — operator: "the about page has too much; it should be just a hyperlink on 'system documentation & diagrams'". Replaced multi-line block with a single hyperlink. Diagrams.html viewer now defaults to the central definitions doc + adds it to the sidebar as a top-level "Manual" entry.

### Added

- **BL268 partial** — fleshed out 4 more `datawatch-definitions.md` sections (Launch Automation form, Automaton detail, Settings → General, Settings → Automate). The remaining card prose stays tracked as BL268.
- **`scripts/sync-docs-to-webfs.sh`** — mirrors `docs/datawatch-definitions.md` into the embedded web FS so the daemon can serve it at `/docs/`. `--check` mode for release-smoke prelude.
- **release-smoke.sh check 18** — Automata flow: PRD CRUD lifecycle on the default path (no LLM cost); execute step gated behind `DW_MAJOR=1` (consumes API quota — major-release-only).

### Fixed

- **`README.md`** — restored the missing parts of the Daniel Keys Moran acknowledgement: the *"The DataWatch sees everything."* tagline + the multi-source book-purchase paragraph (Amazon / Internet Archive / blog email request). Operator: "the attribution to daniel keys moran is incomplete... fix it".
- **Council `internal/council/council.go`** — `loadOrSeed` now does additive seeding via a `.seeded` marker so adding new defaults in future releases doesn't require manual operator action.

### Tests

- Updated `TestDefaultPersonasSixCanonicalRoles` → `TestDefaultPersonasTenCanonicalRoles` (count 6 → 10, 4 new names asserted).
- Existing 1804 go tests continue to pass.
