# Final gate — parallel tasks inside one Automaton (v9.57.0)

Date: 2026-11-02 (UTC)

Branch `automaton/3ebd0001` (worktree `/home/<user>/.datawatch/prd-worktrees/3ebd0001`), merge base `3aa19ca34376580736dd38cadeb4ef36dbcf4456` (merge base of `origin/main`). Every command below was run from the repository root. Lint: the full `make lint` target includes `golangci-lint run ./...`; per the gate spec's explicit alternative ("make lint **or** scripts/check-docs-sync.sh, check-howto-coverage.sh, check-curated-howtos.sh, check-no-internal-refs.sh, check-version-bump.sh") the five named scripts were run instead.

## Commands (final results)

- `go build -o datawatch ./cmd/datawatch/` — **PASS** (exit 0, no output; binary at the repo root is gitignored via `/datawatch`)
- `go vet ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./internal/config/` — **PASS** (exit 0, no output)
- `go test ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./internal/config/` — **PASS** (exit 0):
  ```
  ok  	github.com/dmz006/datawatch/internal/autonomous	62.905s
  ok  	github.com/dmz006/datawatch/internal/server	67.555s
  ok  	github.com/dmz006/datawatch/internal/mcp	1.961s
  ok  	github.com/dmz006/datawatch/internal/router	134.873s
  ok  	github.com/dmz006/datawatch/internal/config	(cached)
  ```
- `bash scripts/check-docs-sync.sh` — **FAIL** (exit 1):
  ```
  ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage.
  >f+++++++++ plans/2026-11-01-parallel-worktree-live-smoke-report.md
  >f+++++++++ plans/reports/archive/2026-11/parallel-tasks-datawatch-app-parity-issue.md
  >f+++++++++ plans/reports/archive/2026-11/parallel-tasks-live-smoke.md
  ```
  Three branch-added `docs/plans/` files are missing from the embedded PWA docs mirror (`make sync-docs` would fix it). Per the gate spec, no other file was edited to make the check pass; the failure is recorded as-is.
- `bash scripts/check-howto-coverage.sh` — **PASS** (exit 0):
  ```
  ==> Howto coverage audit (every docs/howto/*.md must be authored or LLM-only)
  ✓ PASS: every howto under docs/howto/ has exec_steps or is LLM-only.
  ```
- `bash scripts/check-curated-howtos.sh` — **PASS** (exit 0):
  ```
  ==> Curated howto exec_steps audit (23 howtos)
  ✓ PASS: 23 curated howtos all have exec_steps; every tool: references a registered MCP tool.
  ```
- `bash scripts/check-no-internal-refs.sh` — **PASS** (exit 0):
  ```
  ==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)
  ✓ PASS: no internal refs in user-facing surfaces.
  ```
- `bash scripts/check-version-bump.sh` — **PASS** (exit 0):
  ```
  ==> Version check: 9.57.0 (both files match) [VERSION_CHECK_MODE=range]
      base: 3aa19ca34376580736dd38cadeb4ef36dbcf4456 (resolved via merge-base of origin/main), version at base: 9.56.0
  ✓ PASS: range 9.56.0 -> 9.57.0 (base: 3aa19ca34376580736dd38cadeb4ef36dbcf4456, via merge-base of origin/main)
  ```
- New app tests: `node --test internal/server/web/app-parallel-metrics.test.js internal/server/web/app-parallel-task-merge.test.js internal/server/web/app-task-read-only.test.js` — **PASS** (exit 0; tests 17, pass 17, fail 0, cancelled 0, skipped 0). These are exactly the `app-*.test.js` files added by this branch.

## Verification items

**Both Version strings match.** `internal/server/api.go:205` and `cmd/datawatch/main.go:123` both read `var Version = "9.57.0"` — PASS (also confirmed by `check-version-bump.sh`, "both files match").

**Every new setting has a help link entry and a definitions entry.** The new settings are the four `autonomous.parallel.*` keys (`read_only_tasks`, `worktree_per_task`, `merge_conflict_policy`, `merge_conflict_model`; `internal/config/config.go:6199-6213`).

- Help links: the help link table in `internal/server/web/app.js:9007-9010` maps all four keys to the glossary — `'autonomous.parallel.read_only_tasks': DOCS_DEFINITIONS + '#read-only-tasks'`, `'autonomous.parallel.worktree_per_task'`, `'autonomous.parallel.merge_conflict_policy'` and `'autonomous.parallel.merge_conflict_model'` to `DOCS_DEFINITIONS + '#parallel-task-worktrees'`. Both target sections exist in `docs/datawatch-definitions.md` (`### Parallel task worktrees` at line 1986, `### Read-only tasks` at line 1996). The PWA tests assert "the group and every setting have a help link table entry that opens the glossary section" — PASS.
- Definitions: `docs/datawatch-definitions.md` documents `worktree_per_task` (line 1988), `merge_conflict_policy` (line 1992), `merge_conflict_model` (line 1994) and `read_only_tasks` (line 1998). All four keys also appear in `docs/config-reference.yaml`, `docs/howto/automaton-parallel-tasks.md` and `docs/api/openapi.yaml` — PASS.

**The parity issue draft exists.** `docs/plans/reports/archive/2026-11/parallel-tasks-datawatch-app-parity-issue.md` — "datawatch-app parity issue: parallel tasks inside one Automaton (v9.57.0)", "Ready for the merge step to file against `dmz006/datawatch-app`. Not filed from this branch." — PASS.

## Verdict

**FAIL (1 of 10 checks).** All Go build/vet/test commands, four of the five lint scripts, the three new node test files (17/17), the version match, the help-link and definitions coverage, and the parity draft all pass. The single failure is `scripts/check-docs-sync.sh`: the embedded PWA docs mirror is missing the three branch-added `docs/plans/` files (the live-smoke report, the parity issue draft, and the live-smoke writeup). The fix is `make sync-docs` plus re-staging the embedded assets, which is outside this gate's "commit that file only" boundary and therefore left for the operator.
