# Final gate — plan coverage filters (BL488)

Date: 2026-10-31

Branch `prd/2026-10-30-bl488-plan-coverage-filter` (worktree `/home/<user>/.datawatch/prd-worktrees/e58f6807`), merge base `a187351cf748c242d6876d40b79a360ada26e784`. Every command below was run from the repository root with `env -u TMUX -u TMUX_TMPDIR` in front.

## Commands (final results)

- `env -u TMUX -u TMUX_TMPDIR go build -o .scratch-gate/datawatch ./cmd/datawatch/` — PASS (0, binary built; scratch dir created inside the project boundary and deleted afterwards)
- `env -u TMUX -u TMUX_TMPDIR go build ./...` — PASS (0, no output)
- `env -u TMUX -u TMUX_TMPDIR go vet ./internal/config ./internal/autonomous ./internal/server ./internal/mcp ./internal/router ./internal/federation ./cmd/datawatch` — PASS (0, no output)
- `env -u TMUX -u TMUX_TMPDIR go test ./...` — PASS (0, 83 packages ok, 0 failing packages)
- `env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/*.test.js` — PASS (0, tests 1759, pass 1759, fail 0)
- `make lint` — PASS (0, golangci-lint "0 issues"; internal-refs check "✓ PASS: no internal refs in user-facing surfaces"; ignore-file audit OK)
- `env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-internal-refs.sh` — PASS (0)
- `env -u TMUX -u TMUX_TMPDIR bash scripts/check-howto-coverage.sh` — PASS (0)
- `env -u TMUX -u TMUX_TMPDIR bash scripts/check-curated-howtos.sh` — PASS (0)
- `env -u TMUX -u TMUX_TMPDIR bash scripts/check-docs-sync.sh` — PASS (0)
- `gofmt -l` over every `.go` file changed in the branch — PASS (0, no files listed)
- `bash .datawatch/verify/final-gate.sh` — PASS (0, "verification passed")

No command was skipped and nothing was hidden.

## Acceptance matrix (evidence for every operator requirement)

**1. Created files are not findings, and a path whose parent folder cannot be created stays a finding.**
`TestPlanCoverageFilters_CreatedFilesSuppression` (`internal/autonomous/plan_coverage_created_findings_test.go:66`): subtest 1 "created file in title, spec verb, and files list gives no finding and Suppressed 1" through subtest 3, with subtest 6 "options off reproduces the old findings" and subtests 7–9 keeping findings for reference-only, negated, and duplicate paths. Subtest 4 (`:133`) "uncreatable parent and no task creates it keeps the finding, Suppressed 0" and subtest 5 "another task creating the parent directory clears the finding" cover the parent-folder case; `TestPlanCoverageFilters_GateUncreatableFile` (`plan_coverage_filters_gate_test.go:242`, "an uncreatable referenced file must still fail the record") covers it at the gate level. How-to: "the file's parent folder has to exist or be created" (`docs/howto/plan-coverage-filters.md:67`).

**2. Stray words and short phrases are not requirements; only the scope and constraint sentences count; the minimum length is configurable.**
`TestPlanCoverageFilters_RequirementsScopedSuppressesStrayTokens` and `TestPlanCoverageFilters_RequirementsNoRecognisedHeadingUsesWholeSpec` (`plan_coverage_scope_report_test.go:47,122`); `TestPlanCoverageFilters_ScopeFixtureBodiesOnly`, `_ScopeCommaQualifierAndParenthetical`, `_ScopeSectionNamesCaseInsensitive`, `_ScopeExcludesNonScopedTokens`, `_ScopeMinCharsDropsShortKeepsLong`, `_ScopeUnscopedExtractionUnchanged` (`plan_coverage_scope_test.go:26-175`); the configurable minimum is `autonomous.plan_coverage_min_requirement_chars` with `0` meaning the built-in default 30 (`TestPlanCoverageFilters_ConfigMinChars`, `internal/config/plan_coverage_filters_test.go:70`; negative or textual values rejected in `TestPlanCoverageFilters_ConfigInvalidValues`, `internal/server/plan_coverage_filters_config_test.go:154`).

**3. The settings come on by default and the plan shows how many findings each filter withheld.**
Defaults: `TestPlanCoverageFilters_ManagerDefaults` (`plan_coverage_filter_settings_test.go:18`, unset reads both filters true, sections `Scope, Constraints`, minimum 30) and `TestPlanCoverageFilters_ConfigGetDefaults` (`internal/server/plan_coverage_filters_config_test.go:79`). Suppressed counts on the plan: `record.suppressed.created_files` / `.requirement_tokens` asserted in `TestPlanCoverageFilters_RequirementsScopedSuppressesStrayTokens` and rendered by `TestPlanCoverageFilters_CLISummaryShowsCountsAndFilters` (`cmd/datawatch/plan_coverage_filters_cli_test.go:80`, summary line `Suppressed by the plan coverage filters: 2 created-file finding(s), 1 requirement finding(s)`), `TestPlanCoverageFilters_CommSummaryHoldsCountsAndAck` (`internal/router/plan_coverage_filters_comm_test.go:126`), and the PWA panel test at `app-plan-coverage-filters.test.js:32`; live check #5 of the sandbox report shows the same counts over HTTP.

**4. The acknowledgement path still works for a genuine finding.**
`TestPlanCoverageFilters_GateAckThenApprove` (`plan_coverage_filters_gate_test.go:316`: genuine missing requirement → blocked → acknowledge by fingerprint → approved) and `TestPlanCoverageFilters_GateGenuineMissingRequirement` (`:229`); `TestPlanCoverageFilters_GateFingerprintIgnoresSuppressedAndFilters` (`:355`) keeps a finding-level acknowledgement valid when only the filters change.

**5. The settings are configurable from YAML config, REST, MCP, the CLI, chat, and the PWA.**
YAML: `docs/config-reference.yaml` entries plus `TestPlanCoverageFilters_ConfigRoundTripYAML` / `_ConfigRoundTripJSON` (`internal/config/plan_coverage_filters_test.go:103,127`). REST: `TestPlanCoverageFilters_ConfigRoundTrip`, `_ConfigPartialPutKeepsExplicitFalse`, `_ConfigAutonomousEndpointRoundTrip` (`internal/server/plan_coverage_filters_config_test.go:96,131,178`). MCP: `TestPlanCoverageFilters_MCP_ConfigSet_ForwardsAll` / `_ABsentParamsNotSent` (`internal/mcp/plan_coverage_filters_mcp_test.go:78,119`). CLI: `datawatch config set autonomous.plan_coverage_*` shown in the how-to (`docs/howto/plan-coverage-filters.md` § CLI). Chat: `configure` help/listing in `TestPlanCoverageFilters_ConfigureHelpAndListMention` (`internal/router/plan_coverage_filters_configure_test.go:16`). PWA: settings panel fields in `internal/server/web/app-plan-coverage-filters.test.js:107` ("the four plan coverage filter settings fields exist with the right keys, types and defaults"). Live check #3 exercises the REST round-trip end to end.

**6. REST read.**
`TestPlanCoverageFilters_ReadWithRecord` (`GET /api/autonomous/prds/{id}/plan_coverage` → `{ "record": … }`) and `_ReadLiveNotPinned`, `_ReadNoRecord`, `_ReadNotFound`, `_ReadMethodNotAllowed`, `_ReadNoTrackerID` (`internal/server/plan_coverage_filters_read_test.go:123-249`); OpenAPI route documented in `docs/api/openapi.yaml` and `internal/server/web/openapi.yaml`; live check #5.

**7. MCP tool.**
`autonomous_prd_plan_coverage_get`: `TestPlanCoverageFilters_MCP_GetTool_ProxiesRoute`, `_MCP_GetTool_MissingID`, `_MCP_GetTool_ReadCapability` (`internal/mcp/plan_coverage_filters_mcp_test.go:22,46,151`); live check #7 (tool call returns the same record as the REST read).

**8. CLI read.**
`datawatch autonomous prd-coverage <plan>`: `TestPlanCoverageFilters_CLIRequestIsThePlanCoverageRead`, `_CLISummaryShowsCountsAndFilters`, `_CLIJSONPrintsRawBody`, `_CLINullRecordKeepsNoReportMessage`, `_CLIHelpSaysAutomatonNotPRD` (`cmd/datawatch/plan_coverage_filters_cli_test.go:63-160`); live check #7 (CLI output matches the REST body).

**9. Chat verb.**
`autonomous plan-coverage <id>` (alias `prd-coverage`): `TestPlanCoverageFilters_CommHitsReadPath`, `_CommAliasReachesPath`, `_CommSummaryHoldsCountsAndAck`, `_CommNoAckLine`, `_CommUsageWithoutID`, `_CommNullRecordLine`, `_CommRelaysDaemonError`, `_CommHelpListsVerb`, `_CommToleratesRecordWithoutFindingsKey` (`internal/router/plan_coverage_filters_comm_test.go:82-249`); live check #7 (chat line matches the REST body).

**10. PWA view.**
Coverage panel with per-filter suppressed line: tests in `internal/server/web/app-plan-coverage-filters.test.js` (suppressed line with both labels, zero-count line, absent-suppressed shape, definitions help link, `DOCS_LINKS_BY_KEY` registration, settings fields); all green in the 1759-test node run.

**11. Parity issue filed with its number.**
`docs/plans/parity-issues/plan-coverage-filters.md` — "Issue: #322", URL `https://github.com/dmz006/datawatch-app/issues/322`, status "filed (unactioned)"; also listed in `docs/parity-status.md`.

**12. The five locale bundles.**
`internal/server/web/locales/{en,de,es,fr,ja}.json` each carry the 9 new `settings_plan_coverage_*` keys (verified: en 9, de 9, es 9, fr 9, ja 9); en has the operator-facing help text "How the plan coverage check is filtered"; de/es/fr/ja carry placeholder strings per the Localization Rule.

**13. Operator-facing wording says Automaton, never PRD.**
`TestPlanCoverageFilters_CLIHelpSaysAutomatonNotPRD` passes (`cmd/datawatch/plan_coverage_filters_cli_test.go:156`); `internal/server/web/locales/en.json` help strings read "Read the plan coverage report of an Automaton (plan) with its filters and suppressed findings."; the how-to, OpenAPI descriptions, changelog entry and test-tracker rows all use Automaton/Automata; `scripts/check-no-internal-refs.sh` reports no internal wording in user-facing surfaces.

**14. Go tests use fakes and cover every decision path, with one test per named failure mode.**
Decision-path tests (fakes: a fake decompose manager, a fake disk statter, a fixed clock, in-memory trackers): `TestPlanCoverageFilters_ManagerDefaults`, `_ManagerExplicitValues`, `_ManagerConfigJSONRoundTrip`, `_CreatedFilesSuppression`, `_Scope*` (7 tests), `_Requirements*` (5 tests), `_GateCreatedFiles`, `_GateStrayTokens`, `_GateGenuineMissingRequirement`, `_GateUncreatableFile`, `_GateFiltersOffReproduceFindings`, `_GatePinnedFilters`, `_GateAckThenApprove`, `_GateFingerprintIgnoresSuppressedAndFilters`, `_Config*` (server, 6 tests), `_Read*` (server, 7 tests), `_MCP_*` (5 tests), `_CLI*` (5 tests), `_Comm*` (9 tests), `_Configure*` (1 test). Named failure modes each have their own test: uncreatable file (`_GateUncreatableFile`), no record / plan not decomposed (`_ReadNoRecord`, `_CLINullRecordKeepsNoReportMessage`, `_CommNullRecordLine`), not found (`_ReadNotFound`), method not allowed (`_ReadMethodNotAllowed`), live-not-pinned (`_ReadLiveNotPinned`), missing tracker id (`_ReadNoTrackerID`), missing MCP id (`_MCP_GetTool_MissingID`), invalid values (`_ConfigInvalidValues`, `_ConfigAutonomousEndpointInvalidValues`).

**15. Live check exists and passed.**
`docs/plans/reports/plan-coverage-filters-live-check.md` (Date: 2026-10-31) — checks 1–8 all PASS on an ephemeral datawatch daemon (`/tmp/dw-gate-<pid>`), loopback-only listeners: build, config defaults, config round-trip + invalid-value rejections, REST read, CLI + chat + MCP agreement, filters-off reproduction, no-residue cleanup; data dir pruned after.

**16. The docs set is complete.**
How-to: `docs/howto/plan-coverage-filters.md`. Config reference: `docs/config-reference.yaml` (4 keys, default 30 minimum documented). OpenAPI: `docs/api/openapi.yaml` + `internal/server/web/openapi.yaml` (route, 4 put keys, response envelope). Testing tracker: `docs/testing-tracker.md` section `## Plan coverage filters — unreleased` (9 rows: all Tested Yes; Validated Yes for the four surfaces the sandbox report covers). Glossary: `docs/datawatch-definitions.md:1579` "### Plan coverage filters". Changelog: `CHANGELOG.md` Unreleased `### Added` entry. Parity: issue #322 file + status table row.

**17. No version number and no version heading on this branch.**
The changelog entry sits under the existing Unreleased Added heading with no `v9.x.y` string; `docs/plans/parity-issues/plan-coverage-filters.md` says "Parent release: the next patch above main at merge time" (no number); the how-to, config reference, OpenAPI and test-tracker rows introduce no version numbers (the only version strings in branch-touched docs are the pre-existing toolchain notes "Go 1.27.2" / "Node.js v24.15.0" in the test-tracker header, which name the test toolchain, not a datawatch release).

## Fixes made in-run (disclosed)

`internal/session/manager.go` — `parseClaudeClockTime` computed the "next occurrence" by adding a flat 24 hours, which shifts the wall-clock hour across a DST transition; on 2026-10-31 16:42 EDT, `TestParseRateLimitResetTime_ModernResetMarker` failed (expected 14:00, got 13:00 EST). The fix rolls to the next calendar day in local time with `time.Date`, preserving the hour, and does not change any other path. This is the only non-feature file changed; it was required for `go test ./...` to pass honestly.

## Bottom line

All 12 gate commands PASS (exit 0) on the final run of 2026-10-31; every acceptance item above is evidenced by a named test, a doc file, the parity issue number, or the live-check report; the one out-of-scope fix is disclosed. The feature is ready to merge.
