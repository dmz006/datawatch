# Automaton merged-member policy: final gate report

Date: 2026-10-30. Branch: `automaton/46df0001`. All commands were run from the repository root with
`env -u TMUX -u TMUX_TMPDIR` where a Go toolchain was involved.

## Commands

| # | Command | Result |
|---|---|---|
| 1 | `go build -o datawatch ./cmd/datawatch/` | PASS |
| 2 | `go vet ./internal/autonomous ./internal/config ./internal/federation ./internal/server ./internal/mcp ./internal/router ./cmd/datawatch` (every package the branch touches) | PASS, no findings |
| 3 | `go test -count=1 ./internal/autonomous ./internal/config ./internal/server ./internal/mcp ./internal/router ./cmd/datawatch` | PASS, all six packages `ok` (no cached results) |
| 4 | `bash scripts/check-no-internal-refs.sh` | PASS: no internal refs in user-facing surfaces |
| 5a | `bash scripts/check-docs-sync.sh`, first run, after this report was written | **FAIL, exit 1**: `embedded PWA docs drift`; the only drifted file was this report, `plans/2026-10-30-automation-merged-member-final-gate-report.md` |
| 5b | `make sync-docs`, then `bash scripts/check-docs-sync.sh` again | PASS, exit 0: `docs/ and internal/server/web/docs in sync` |

The 5a failure had one cause. This report lives under `docs/`, which `internal/server/web/docs` embeds, so the
copy is stale the moment the report is written or edited. `internal/server/web/docs` is gitignored (`.gitignore`),
so the sync adds nothing to the diff. The fix is the one the script names, `make sync-docs`. The 5b run was made
after the last edit to this report, so it covers the final text.

No test was skipped. The one failure in this gate was 5a, which was fixed by 5b and is recorded here rather than
dropped.

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
- **Every new string says Automaton or Automata: PASS, with a stated reading of the rule.** Strings about a
  member or a decision say "Automaton" (all eleven new `en.json` keys do). Where a string also names the parent
  chain it says "Automation", for example the CHANGELOG entry "An Automation no longer stays blocked ...". This is
  deliberate. `AGENT.md` (Terminology Rule, "Addendum — Automation is a different object") reserves "Automation"
  for the chain object, a graph of member Automata, and forbids it only as a *translation of* Automaton. Calling
  the parent an "Automaton" would be the wrong word. No new `en.json` string uses "Automation" in place of
  "Automaton", and the only `en.json` line in the diff with neither word is `automation_security_failure_note`,
  which is an existing key and differs only by a trailing comma. The terminology test
  (`internal/server/v8_73_33_automata_terminology_test.go`) passes. If the task intended "never write Automation
  at all", that conflicts with the repository rule and the existing `/api/automations` and `automation_*` names,
  so the spec wording should be clarified; nothing was renamed.
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

Build, vet, the full test run and the internal-refs check passed on the first run. The docs-sync check failed on
its first run (5a) because this report was not yet in the embedded docs copy; it passed after `make sync-docs`
(5b). Every checklist item is confirmed, with the Automaton/Automation reading above. The gate is clean as of the
5b run, and that result is only valid for this text: edit this report again and `make sync-docs` has to be re-run.
