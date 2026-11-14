# Autonomous priority by kind + defect intake (e3db0001)

- **Date:** 2026-11-03 (plan retrofitted by the rules-review remediation pass; implementation landed on this branch in the e3db0001 task commits before this date)
- **Version:** v9.67.0 (datawatch version at the time of planning); the work is recorded under `## [Unreleased]` in `CHANGELOG.md` and ships in the next release.
- **PRD:** e3db0001 (branch `automaton/e3db0001`, 47 files changed)

## Scope

- `internal/autonomous` — kind → priority resolution and `priority_source` recording; defect-intake offer + outcome record
- `internal/server` (REST) — `GET`/`PUT` `/api/autonomous/config`, `PUT /api/config`, `POST /api/autonomous/prds/{id}/set_priority`, `GET /api/autonomous/prds/{id}` fields; parity + validation tests
- `internal/mcp` — `autonomous_config_get`/`autonomous_config_set`, `autonomous_prd_set_priority`, `autonomous_prd_get` parity test
- CLI (`cmd/datawatch`) — `datawatch autonomous config-get`/`config-set` rows
- comm channel router — `configure autonomous.priority_by_kind=…`, `configure autonomous.defect_intake_automation=…`
- `internal/server/web` — priority badge with source + tooltip, the two Settings fields, 9 new locale keys in all five bundles + guards (`v5280_locales_test.go`, `v8_73_33_automata_terminology_test.go`, `app-priority-source.test.js`)
- Docs — `docs/howto/autonomous-priority-by-kind.md`, `docs/operations.md`, `docs/config-reference.yaml`, `docs/commands.md`, `docs/api-mcp-mapping.md`, `README.md`, `docs/howto/README.md`
- Evidence — `docs/testing-tracker.md` section, `docs/plans/parity-issues/priority-source-and-automata-settings.md`, `scripts/test-stories/pwa/TS-841.mjs`, release-smoke section 79b
- Tests — `priority_by_kind_test.go`, `priority_by_kind_parse_test.go`, `defect_intake_test.go`, `config/priority_by_kind_test.go`, `mcp/priority_by_kind_parity_test.go`, `router/priority_by_kind_comm_test.go`, `server/priority_by_kind_parity_test.go`

## Phases

1. **Resolve the capacity-queue priority from the kind table and record `priority_source`** (`049b26c9a` + `Merge 0bb5a8a24`). Status: **Done** — shipped on this branch; the `explicit` / `kind` / `default` rule set the priority is recorded on the Automaton and session records.
2. **Offer kind-bug defect filings to the named Automation and record the outcome** — Status: **Done** — `offered` / `not_configured` / `skipped` / `error` recorded as `defect_intake` on the Automaton; empty setting records `not_configured` and offers nothing (never guessed); intake never fails or blocks the filing.
3. **REST, MCP, CLI and comm surfaces for `priority_by_kind`, `defect_intake_automation` and the per-Automaton pin** (`e42add151` + `Merge bea32783a`) — Status: **Done** — integer-only validation (refused, nothing stored), empty clears back to the shipped defaults, `set_priority` records `priority_source: explicit`.
4. **Web UI priority-source badge + the two Settings fields, five locales** (`ab22bba93` + `Merge 9099b2885`) — Status: **Done** — 9 new locale keys in en/de/es/fr/ja, locale guards extended (must-have keys + terminology-guard exemption for the two defect-intake keys, both with a recorded rationale).
5. **Operator documentation** (`ff78f6cbe` + `Merge a5c8d45df`) — Status: **Done** — how-to, `docs/operations.md` section, `docs/config-reference.yaml`, `docs/commands.md`, `docs/api-mcp-mapping.md` rows, README line, howto index row.
6. **Rules-review remediation (this pass)** — Status: **Done** — locale bundle duplicates removed (de/es/fr/ja at 3360 keys, 0 duplicates), German typo fixed, three wrong-word locale values translated per the translation table, testing-tracker section appended, parity-issue draft written, TS-841 story + `priority-source` group + release-smoke section 79b added, CHANGELOG reuse audit recorded, plan doc (this file) written.

## Parity surface

| Surface | Status | Note |
|---|---|---|
| `REST` | **Included** | `GET`/`PUT /api/autonomous/config`, `PUT /api/config`, `POST /api/autonomous/prds/{id}/set_priority`, `GET /api/autonomous/prds/{id}` (`priority`, `priority_source`, `defect_intake`) |
| `MCP` | **Included** | `autonomous_config_get` / `autonomous_config_set` (the two key arguments), `autonomous_prd_set_priority`, `autonomous_prd_get` fields; parity test `internal/mcp/priority_by_kind_parity_test.go` |
| `CLI` | **Included** | `datawatch autonomous config-get` / `config-set` for both keys; `datawatch config set autonomous.priority_by_kind …`. The per-Automaton pin intentionally has **no CLI subcommand** — the record-level pin is an operator decision per Automaton and the CLI already carries the table and the general `config set` path; `datawatch session priority <id> <n>` stays session-only and is documented as such |
| `comm channel` | **Included** | `configure autonomous.priority_by_kind=…` and `configure autonomous.defect_intake_automation=…`; both keys are listed in the `configure` help output |
| `YAML/config` | **Included** | Both keys ride the standard config file + `PUT /api/config` apply path (validate first, apply at once, no restart), recorded in `docs/config-reference.yaml` |
| `PWA` | **Included** | Priority badge with source tooltip on the Automaton and capacity-waiting views; the two Settings fields (Automata tab); all nine new keys in all five locale bundles; e2e story TS-841 + release-smoke section 79b |
| `Android` | **Excluded — reason logged** | The datawatch-app client reads the same REST/MCP contract already shipped in phase 3; the badge and the two settings fields are a UI addition owed to the native clients. Drafted in `docs/plans/parity-issues/priority-source-and-automata-settings.md` (issue body complete: REST fields, config fields, locale keys with EN values, MCP/CLI/comm/PWA render spec, acceptance criteria). **Not yet filed** — this remediation pass runs under the no-gh constraint; the file is prepared to open against dmz006/datawatch-app as-is |
| `iPhone/iOS` | **Excluded — reason logged** | Same as Android — capability parity owed through the same datawatch-app issue; no separate iOS surface for this feature (the SwiftUI client shares the Android codebase per `docs/parity-status.md`) |

## Phase Completion Checklist (phase 6, this pass)

- [x] Functionality implemented per the phase's own description — remediation is evidence + docs + guards, all applied.
- [x] Unit tests added/passing for anything new — guards extended (`TestLocales_CommonNavKeysPresent` must-have + 9 keys; `isAutomationObjectKey` exemption with rationale); all touched Go packages and `node --test internal/server/web/app-priority-source.test.js` green, run 2026-11-03.
- [x] Live/smoke test — TS-841 story (settings rows render + write/read/refuse/clear contract) wired into release-smoke section 79b; live-curl rows in the testing-tracker section are recorded as owed and discharge when release-smoke 79b runs under a live daemon.
- [x] `go test ./...` and `node --test internal/server/web/*.test.js` green, run 2026-11-03; the final gate re-runs the full matrix after this plan is written.
- [x] General documentation checklist — `CHANGELOG.md` (reuse audit added this pass), `docs/config-reference.yaml`, `docs/operations.md` (section added this pass), `README.md` (line added this pass), doc index in `README.md`/`docs/README.md`/`docs/howto/README.md` (row already present), `docs/testing-tracker.md` (section added this pass), no leaked tracker IDs, this plan's `Parity surface` section accurate.
- [x] Conditional docs — `docs/howto/autonomous-priority-by-kind.md` (new operator workflow; added in phase 5) and the operations section (this pass). Glossary: N/A with reason — the feature adds settings on existing concepts (priority, queue, Automation record), no new operator-facing *concept*; the how-to covers the workflow.
- [x] Mobile-Parity Rule — datawatch-app issue: drafted in `docs/plans/parity-issues/priority-source-and-automata-settings.md`, **not yet filed** (no-gh constraint of this pass; recorded as the one outstanding action, and the Parity surface table carries the exclusion reason).
- [x] Localization Rule — all five bundles at 3360 keys, 0 duplicates; `TestLocales_AllAppJSKeysExistInEnglishBundle` passes.
- [x] Version bumped in `cmd/datawatch/main.go` and `internal/server/api.go` — N/A with reason: this remediation pass fixes docs/tests/locale bundles of already-merged work; no shippable behaviour change of its own, and the e3db0001 work ships under the next release's version bump (currently `9.67.0` on main).
- [x] This plan doc's phase-status lines updated — all phases Done; shipped version to be noted in the release entry when the next version tag lands.
