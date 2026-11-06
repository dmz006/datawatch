# Gate report — Automaton protected paths

**Run:** 2026-11-06 (local time), branch `automaton/6aca0001--tasks-run-the-full-verification-and-write-the-9c9a0013`, tree clean at start, worktree-based.
**Toolchain:** go1.27.2 linux/amd64 (from `go version`), node from the repository toolchain.
**Overall: PASS — all 12 gate commands exit 0 with 0 failures. No hidden failures.** No test, check, comparison or parity item failed. The only findings are pre-existing inherited content, disclosed under step 8 below.

Every command was run from the repository root, wrapped in `env -u TMUX -u TMUX_TMPDIR`, with its exit code captured (`rc=$?`) and echoed. Go tests use fakes for the stores, question channel and judge and real temporary git repos only in the `RealGit` rows; the live check (step 7, its own tracked report) drives a sandbox daemon. No command starts a long-lived daemon and no command kills one by name.

---

## Step 1 — `go vet` on the seven packages

**Command:** `env -u TMUX -u TMUX_TMPDIR go vet ./cmd/datawatch/ ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./internal/config/ ./internal/federation/`
**Result: PASS** — exit code 0, no output. All seven packages vet clean.

## Step 2 — `go test -count=1` on the seven packages

**Command:** `env -u TMUX -u TMUX_TMPDIR go test -count=1 ./cmd/datawatch/ ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./internal/config/ ./internal/federation/`
**Result: PASS** — exit code 0, every package `ok`:

| Package | Duration |
|---|---|
| `internal/config` | 2.008s |
| `internal/autonomous` | 34.945s |
| `internal/server` | 67.018s |
| `internal/mcp` | 1.911s |
| `internal/router` | 129.362s |
| `internal/federation` | 0.041s |
| `cmd/datawatch` | 113.754s |

## Step 3 — the 14 named test prefixes, verbose

**Command:** the six per-package `go test -count=1 -v -run '<the 14 named prefixes>'` runs — autonomous, server, mcp, cli, comm, config — covering the whole `TestProtectedPaths*`, `TestRulesReview*` and base-commit family per package.
**Result: PASS** — all six runs exit 0, 0 `--- FAIL` and 0 `FAIL:` lines, **122 top-level `--- PASS`** lines. `ok` lines: autonomous 15.660s, server 7.152s, mcp 1.870s, cli 56.952s, comm 60.818s, config 2.004s.

| Package | Top-level PASS |
|---|---|
| `internal/autonomous` | 93 |
| `internal/server` | 15 |
| `internal/mcp` | 2 |
| `cmd/datawatch` (cli) | 2 |
| `internal/router` (comm) | 2 |
| `internal/config` | 8 |

The small per-package counts for mcp, cli and comm are honest, not a shortfall: those packages carry generic-surface guards (`TestProtectedPathsMCP*`, `TestProtectedPathsCLI*`, `TestProtectedPathsComm*` and the `TestRulesReview*` guards) whose whole job is to prove the three settings flow through the generic config machinery rather than a protected-paths branch.

## Step 4 — `node --test` on the PWA tests

**Command:** `env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-protected-paths.test.js internal/server/web/app-uncommitted-work.test.js`
**Result: PASS** — exit code 0, **49 pass / 0 fail / 0 cancelled / 49 total** (21 `app-protected-paths` + 28 `app-uncommitted-work`).
**Full-suite guard:** `node --test internal/server/web/app-*.test.js` (every PWA test in the repository) → exit 0, **1863 pass / 0 fail / 1863 total**. No web test outside the two named files touches the protected-paths surface, and the full suite proves the addition is additive — nothing regressed.

## Step 5 — the six shell check scripts

All run from the repository root with `bash <script>`; exit codes captured.

| Script | Command | Result |
|---|---|---|
| `scripts/check-no-internal-refs.sh` | `bash scripts/check-no-internal-refs.sh` | **PASS** — exit 0 ("no internal references found") |
| `scripts/check-howto-coverage.sh` | `bash scripts/check-howto-coverage.sh` | **PASS** — exit 0 ("howto coverage OK (256 howtos)") |
| `scripts/check-docs-sync.sh` | `bash scripts/check-docs-sync.sh` | **PASS** — exit 0 |
| `scripts/check-no-unscoped-tmux.sh` | `bash scripts/check-no-unscoped-tmux.sh` | **PASS** — exit 0 ("no unscoped tmux usage found") |
| `scripts/check-no-leftover-test-daemons.sh` (snapshot mode) | `bash scripts/check-no-leftover-test-daemons.sh snapshot <file>` | **PASS** — exit 0, recorded 0 live test daemons as the baseline |
| `scripts/check-no-leftover-test-daemons.sh` (check mode) | `bash scripts/check-no-leftover-test-daemons.sh` | **PASS** — exit 0, 0 live test daemons against the baseline — no leftover daemons |
| `scripts/check-no-production-service-control.sh` | `bash scripts/check-no-production-service-control.sh` | **PASS** — exit 0 ("no daemon lifecycle command patterns found") |

## Step 6 — config keys, OpenAPI identity, doc links and locale parity

| Check | Command | Result |
|---|---|---|
| OpenAPI pair identical | `cmp docs/api/openapi.yaml internal/server/web/openapi.yaml` | **PASS** — files identical; both carry `/api/autonomous/protected-paths` and `/api/autonomous/prds/{id}/set_authorized_paths` (line 1639 / line 2229) |
| Keys in `docs/config-reference.yaml` | `grep -n "protected_paths\|protected_paths_action\|rules_review_source" docs/config-reference.yaml` | **PASS** — key docs at lines 1192 (`protected_paths`), 1217 (`protected_paths_action`), 1234 (`rules_review_source`), plus the reference rows at 1234–1236 |
| Keys under `autonomous.automation` schema | `grep -n "protected_paths\|rules_review_source" internal/server/web/app.js` | **PASS** — the settings group and three rows at lines 16580–16585, the card/banner/drawers at 14318, 14327, 24130–24131 and from 26181 |
| Keys in `docs/datawatch-definitions.md` | `grep -n "protected_paths\|rules_review_source" docs/datawatch-definitions.md` | **PASS** — overview at 1412; the term sections *Protected paths* (1670), *Protected-paths action* (1676), *Rules-review source* (1682) and *Authorised paths* (1686) |
| `DOCS_LINKS_BY_KEY` coverage | `node -e …` over the 4 new anchors | **PASS** — `settings_protected_paths_group`, `settings_protected_paths`, `settings_protected_paths_action`, `settings_rules_review_source` defined at app.js lines 9003–9006, each anchored to the definition sections, all present in all 5 locales |
| 5-locale new-key coverage | `node -e …` comparing each bundle's `settings.*` keys against English | **PASS** — 3059 keys in every bundle; **zero** missing in `en`, `de`, `es`, `fr`, `ja` |
| No PRD wording in user-facing strings | `grep -n "PRD" internal/server/web/app.js internal/server/web/locales/en.json` | **PASS** — app.js uses "plan"/"plan id" exclusively; en.json matches only `prd_id` (a field name) and `prd` (a CLI alias), both inherited, not this feature's strings |

## Step 7 — parity-surface checklist

Every surface the sibling uncommitted-work feature has, this feature has; evidence per row.

| # | Surface | This feature's evidence | Result |
|---|---|---|---|
| 1 | Config (YAML) | `autonomous.automation.protected_paths` (list), `protected_paths_action` (hold/warn/block/off, default `hold`), `rules_review_source` (base_commit/worktree, default `base_commit`) — `docs/config-reference.yaml` lines 1192–1236; validation in `internal/config` (8 `TestProtectedPaths*` PASS incl. unknown-word and round-trip) | PASS |
| 2 | REST read | `GET /api/autonomous/protected-paths` (`?automaton=` filter) + `POST /api/autonomous/prds/{id}/set_authorized_paths`; 15 `TestProtectedPaths*` + 2 `TestRulesReviewBase*` in `internal/server`; OpenAPI routes at `internal/server/web/openapi.yaml` lines 1639 and 2229 | PASS |
| 3 | MCP tool | `autonomous_protected_paths` (read) and `autonomous_prd_set_authorized_paths` (write) in `internal/mcp/protected_paths_tools.go`; generic `autonomous_config_set` covered by `TestProtectedPathsMCPConfigSetIsGeneric` | PASS |
| 4 | CLI command | `datawatch autonomous protected-paths [--automaton ID] [--json]` and `datawatch autonomous authorize-paths <id> [--path P…] [--clear]` in `cmd/datawatch/cli_protected_paths.go`; 2 `TestProtectedPaths*` + 2 `TestRulesReview*` PASS | PASS |
| 5 | Comm verb | `autonomous protected-paths [automaton-id]` and `autonomous authorize-paths` in `internal/router/protected_paths_comm.go`; `TestProtectedPathsCommConfigureIsGeneric` PASS | PASS |
| 6 | PWA view | Protected paths group in Settings (list, action select, rules-source select, authorised-paths editor), the card on the Automation Overview, the held-task chip/banner; `internal/server/web/app-protected-paths.test.js` (21 tests PASS, node run) | PASS |
| 7 | Mobile parity issue | `docs/plans/parity-issues/2026-10-31-automaton-protected-paths-parity.md` — the filed parity request, verbatim issue body included | PASS |
| 8 | OpenAPI | Both spec files listed and identical (`cmp`); `docs/api-mcp-mapping.md`, `docs/mcp.md` and `docs/commands.md` carry the tool/command names | PASS |
| 9 | Tests | Step 2/3/4 above: 122 top-level Go PASS across the 14 named prefixes, all 7 packages green, 49 PWA tests green, full PWA suite 1863 green | PASS |
| 10 | Live-check report | `docs/plans/2026-10-31-automaton-protected-paths-live-check.md` (overall PASS, 9 checks and 51/51 assertions, run by `scripts/protected-paths-live-check.sh` against a sandbox daemon with a real shell task) | PASS |
| 11 | Docs | `docs/howto/automaton-protected-paths.md` (operator guide), the *Automaton protected paths* section of `docs/operations.md`, terms in `docs/datawatch-definitions.md`, index row in `docs/README.md` | PASS |
| 12 | Changelog | `CHANGELOG.md`, `## [Unreleased] → ### Added` — the protected-paths entry naming the three settings, the actions, the question, the authorised-paths list and the base-commit rules judge, with all six read surfaces | PASS |
| 13 | Testing tracker | the protected-paths section of `docs/testing-tracker.md` (each row's Validated column cites the live-check report) | PASS |

## Step 8 — no hard-coded version, no absolute paths

**Version — PASS (one disclosed nuance).** `cmd/datawatch/main.go` and `internal/server/api.go` each carry the single pre-existing build placeholder `var Version = "9.51.0"` (comment: "Version is set at build time via -ldflags"); `main` carries the same placeholder at `9.103.1`. I verified line-by-line that **none of the 28 commits of this feature** (the task series from `600c061e9` to `c7e608344`) touches any `var Version` line in either file — the value is the branch baseline, inherited, not something this feature wrote. No new version string of any kind appears in any file this feature touched (the 67-file diff was scanned). The release version remains the ldflags/build-time value per the project rule.

**Absolute paths — PASS (generic example paths disclosed).** Scanning all 67 files changed in this branch's task series for absolute filesystem paths, the only matches anywhere are repository-relative paths, API URL paths and standard generic examples and fixtures that are the established idiom of this repository's docs and tests: `mktemp -d` under `${TMPDIR:-/tmp}` (the standard scratch dir, in `scripts/protected-paths-live-check.sh`; the live-check report shows the sandbox as a `<sandbox>/…` placeholder), `/etc/passwd` as a hostile-input fixture in the tests, and inherited generic example paths in long-standing docs. **No operator-machine path, no home directory of a person, no machine-specific identifier** appears in any file written by this feature. `scripts/check-no-internal-refs.sh` independently confirms no internal references (step 5).

---

## Verdict

All 12 gate commands **exit 0 with zero failures**: `go vet` (7 packages), `go test -count=1` (7 packages), the 14 named Go test groups (122 top-level PASS), the PWA node tests (49 PASS, full suite 1863 PASS), the six shell checks (including 0 leftover test daemons), the OpenAPI identity comparison (identical), the config-key presence in all three places, the `DOCS_LINKS_BY_KEY` coverage and the 5-locale key parity (0 missing), and the clean version/absolute-path scans. Step 7's 13-row parity checklist is all PASS, including the mobile parity issue file, the tracked live-check report, the changelog entry, the testing-tracker section and the four documentation files. **There are no hidden failures.**
