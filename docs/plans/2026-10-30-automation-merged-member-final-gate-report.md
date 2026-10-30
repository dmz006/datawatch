# Automaton merged-member policy: final gate report

Date: 2026-10-30. Branch: `automaton/46df0001`. All commands were run from the repository root with
`env -u TMUX -u TMUX_TMPDIR` where a Go toolchain was involved.

## Commands

| # | Command | Result |
|---|---|---|
| 1 | `go build -o datawatch ./cmd/datawatch/` | PASS |
| 2 | `go vet ./internal/autonomous ./internal/config ./internal/federation ./internal/server ./internal/mcp ./internal/router ./cmd/datawatch` (every package the branch touches) | PASS, no findings |
| 3 | `go test ./internal/autonomous ./internal/config ./internal/server ./internal/mcp ./internal/router ./cmd/datawatch` | PASS, all six packages `ok` |
| 3b | Re-run of the five packages that first came from the test cache, with `-count=1` (`autonomous`, `config`, `mcp`, `router`, `cmd/datawatch`) | PASS, all `ok` |
| 4 | `bash scripts/check-no-internal-refs.sh` | PASS: no internal refs in user-facing surfaces |
| 5 | `bash scripts/check-docs-sync.sh` | PASS: `docs/` and `internal/server/web/docs` in sync |

No test was skipped and no failure had to be fixed or worked around.

## Checklist (confirmed by reading the code and docs)

- **Every behaviour choice is a config key reachable from every surface: PASS.** The one choice is
  `autonomous.automation.merged_member_policy` (`completed`, `manual`, `block`; empty reads as `completed`).
  - YAML: `internal/config/config.go` field with a `yaml` tag, a default of `completed`, and load-time
    validation; documented in `docs/config-reference.yaml`.
  - REST: `GET`/`PUT /api/config` in `internal/server/api.go` and the Automata config object in
    `internal/server/autonomous.go`; read routes for the decisions in `internal/server/merged_member.go`.
  - MCP: `autonomous_config_set` in `internal/mcp/autonomous.go`, plus the read tool `automation_merged_members`.
  - CLI: `datawatch config set` for the key, and `datawatch autonomous merged-members` in
    `cmd/datawatch/cli_merged_member.go`.
  - Comm channel: `configure` in `internal/router/router.go`, and the verb `autonomous merged-members` in
    `internal/router/merged_member_comm.go`.
  - PWA: a settings select in `internal/server/web/app.js`, a decision view on the Automation detail, and a
    help link. Each surface has its own `merged_member_policy_config_test.go` or equivalent test.
- **The proof never hard-codes a branch name: PASS.** A grep for `"main"`, `"master"` and `origin/main|master`
  across the non-test `internal/autonomous/*merged_member*.go` files finds nothing. The default branch is read
  from the project, and the tests exercise it with a scripted git runner.
- **Every new string says Automaton or Automata: PASS.** The new alert, status text, settings label and
  locale values use "Automaton" and "Automation"; the existing terminology test
  (`v8_73_33_automata_terminology_test.go`) was extended for the new strings and passes.
- **The five locale bundles carry the same new keys: PASS.** `de`, `en`, `es`, `fr` and `ja` each add
  the same eleven keys (the sorted key lists hash identically across the five files).
- **CHANGELOG has no tracker IDs and no version number: PASS.** Two entries were added under the unreleased
  `### Added` section; a scan of the added lines for tracker IDs, issue numbers and version strings finds none.
- **The parity issue number is recorded: PASS.** The mobile app parity issue is #310
  (`https://github.com/dmz006/datawatch-app/issues/310`), recorded in
  `docs/plans/parity-issues/2026-10-30-automation-merged-member-parity.md`, with a matching row in
  `docs/parity-status.md`.
- **No version bump: PASS.** The diff against `main` does not touch the `Version` variables in
  `internal/server/api.go` or `cmd/datawatch/main.go`. The minor bump happens at merge time.

## Related record

The sandbox live check for this feature is written up in
`docs/plans/2026-10-30-automation-merged-member-live-check.md`.

## Outcome

All five commands passed and every checklist item is confirmed. The gate is clean.
