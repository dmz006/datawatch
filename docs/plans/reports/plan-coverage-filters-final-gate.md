# Final gate — plan-coverage filters

Feature: deterministic (non-LLM) finding filters on the plan-requirement
coverage gate. Two filters, both on by default, both enforced at
decompose-time compute only (never at the approve/ack check), with the
finding-set the report was computed from pinned on the stored record so a
later config change cannot silently re-interpret it.

- `plan_coverage_suppress_created_files` — rule-3 structural findings
  about files the plan itself creates are withheld.
- `plan_coverage_scoped_requirements` (+ `plan_coverage_requirement_sections`,
  `plan_coverage_min_requirement_chars`) — requirement extraction is scoped
  to the named spec sections at a minimum token length; tokens that stop
  counting as standalone requirements are counted, not dropped.

Both filters appear on every config surface (API config, autonomous config,
MCP `autonomous_config_set`, CLI `config set`, comm `configure`), and the
stored record carries the suppressed per-category counts plus the pinned
filter set. The operator reads both on the CLI `prd-coverage` read and on
every surface that announces plan coverage (the Automaton's `detail=full`
PRD read), and MCP reads stay read-only by construction.

## Commands run

| Command | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./cmd/datawatch ./internal/...` | pass |
| `go test ./...` | pass (67/67 packages ok) |
| `node --test internal/server/web/*.test.js` | pass (1759 pass, 0 fail) |
| `make lint` (pre-checks + golangci-lint run ./...) | pass (0 issues) |
| `gofmt -l` on the branch .go files | clean (no output) |
| `bash scripts/check-no-internal-refs.sh` | pass |
| `bash scripts/check-howto-coverage.sh` | pass |
| `bash scripts/check-curated-howtos.sh` | pass |
| `bash scripts/check-docs-sync.sh` | pass |

## Fixes made during the gate

- `cmd/datawatch/cli_autonomous.go` — `prd-coverage` help text now names
  the shared `detail=full` contract the surface-parity test pins (the same
  record is part of the Automaton's `detail=full` read); four unchecked
  `fmt.Fprintf`/`fmt.Fprintln` error returns flagged by golangci-lint
  (errcheck) are now discarded explicitly (they print to the command's
  stdout, which the tests already capture).

## Acceptance matrix

| # | Acceptance | Verification |
|---|---|---|
| 1 | Both filter keys exist in `internal/config` with on-defaults and land in Get/Set/persistence | `internal/config/plan_coverage_filters_test.go` — pass |
| 2 | Config validation: bad mode/ratio/bools/sections list/min-chars rejected on both config surfaces; ratio clamps to 1.0 | `TestPlanCoverage_SurfacesParity/REST config surface` (internal/server) — pass |
| 3 | Created-file findings about plan-created files are withheld; genuine findings still fail | created-file filter tests in `internal/autonomous` + gate test — pass |
| 4 | Scoped extraction: out-of-section tokens aren't requirements; in-scope requirement still extracted | scoped-requirements tests in `internal/autonomous` — pass |
| 5 | Suppressed per-category counts + pinned filter set stored on the PRD record; fingerprint excludes filter fields so an ack stays valid across a filter-only change | `TestPlanCoverage_SuppressedCountsOnRecord` (internal/autonomous) — pass |
| 6 | Approve gate unaffected by the filters (they only filter findings, never loosen the ratio); ack flow unchanged | `TestPlanCoverage_SurfacesParity/REST report + ack surface` — pass |
| 7 | CLI `autonomous prd-coverage <id>` sends the plan_coverage read, prints suppressed counts + filters, keeps the no-report message on a null record, `--json` passes raw through, help says Automaton (never PRD) | `TestPlanCoverageFilters_CLI*` (cmd/datawatch) — pass |
| 8 | MCP `autonomous_config_set` accepts the filter keys; MCP reads stay read-only; MCP PRD coverage read announces the suppressed counts on the `detail=full` path | `internal/mcp/plan_coverage_filters_mcp_test.go` + `TestPlanCoverage_SurfacesParity/MCP surface` — pass |
| 9 | Comm `configure` accepts the filter keys with the same validation | `internal/router/plan_coverage_filters_comm_test.go` + `TestPlanCoverage_SurfacesParity/comm surface` — pass |
| 10 | Surface parity: one shared contract pinned across REST, MCP, CLI, comm; a rename or path drift on any surface fails it | `TestPlanCoverage_SurfacesParity` (internal/server) — pass |
| 11 | PWA/config surface shows both filter keys with their defaults | app.js config surface + `node --test` — pass |
