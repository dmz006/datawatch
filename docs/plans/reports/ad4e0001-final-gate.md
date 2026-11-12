# ad4e0001 — merge pipeline final gate (counts only)

Gate script exit: **0 (ok)** — the two gate bugs are fixed (node v24 spec-reporter count
extraction; `./internal/router/` missing from the package list).

| Check | Result |
|---|---|
| Go tests — server package | pass (pass=10 fail=0) |
| Go tests — mcp package | pass (pass=17 fail=0) |
| Go tests — CLI package | pass (pass=13 fail=0) |
| Go tests — comm (`internal/router`) | pass (pass=9 fail=0) |
| Node merge-pipeline tests | pass (pass=30 fail=0, node v24) |
| gofmt on changed files | pass (0 findings) |
| golangci-lint | pass (0 findings) |
| No version bump | pass (0 version-line changes) |
| No CHANGELOG change | pass (0 changes) |

Verbatim count lines:

```
go ./internal/server/ pass=10 fail=0
go ./internal/mcp/ pass=17 fail=0
go ./cmd/datawatch/ pass=13 fail=0
go ./internal/router/ pass=9 fail=0
node app-merge-pipeline pass=30 fail=0
gofmt findings on changed files: 0 (no Go files changed)
golangci-lint findings: 0
version line changes: 0, CHANGELOG changes: 0
merge-pipeline-final-gate: ok
```

79 automated checks pass across the five surfaces (10 server, 17 MCP, 13 CLI, 9 comm/
`internal/router`, 30 Node). Beyond the gate:

- `go test ./... -count=1` exits 0 — 88 packages `ok`, zero FAIL lines.
- `make lint` exits 0 — golangci-lint 0 issues, all repo checks green.
- `make build` exits 0 — binary built at version 9.70.3.
- `node --check internal/server/web/app.js` clean.
- The four `merge_pipeline_no_project_*` keys are in the `TestLocales_CommonNavKeysPresent`
  `mustHave` slice (locale parity guard) and in all five bundles (en, de, es, fr, ja).
- `scripts/release-smoke.sh` §93 covers the `project_required` refusal end-to-end against an
  isolated daemon: PASS with the branch binary (400 with `code: project_required` naming
  `worktree_base_repo`). It deliberately FAILs against a pre-branch binary — the structured
  body is new behaviour this branch introduces, and that is exactly what the section checks.
- PWA e2e story TS-847 (`scripts/test-stories/TS-847.sh` +
  `scripts/test-stories/pwa/merge-pipeline-card.mjs`) covers the card empty state and
  preselect at phone width (390x844), with no horizontal overflow.
- `datawatch-app#330` (the merge-pipeline sync issue) is commented with the new
  `project_required` contract for the native apps.

Note: the gate's node count extraction handles both the TAP reporter (`# pass N`) and the
node v24 spec reporter (`ℹ pass N`) summary lines; `./internal/router/` is part of the
checked packages. The node suite itself passes 30/0 under node v24.

## Verification re-run after the rules-review follow-up (2026-11-04)

The post-merge tree had a real inconsistency the review caught: `scripts/release-smoke.sh`
carries 95 top-level `H "NN."` section headers (the branch added section 93, merge pipeline
`project_required`), while `docs/testing.md` still said 94 — so `TestSmokeSectionCountDocMatches`
(`tests/ci/release_smoke_usage_test.go`) failed and `go test ./...` was red. Fixed in this
follow-up:

- `docs/testing.md` now states 95 and enumerates the sections it gained (Claude Code
  interstitial prompt 92, merge pipeline `project_required` 93, lane fill eligibility 94).
- The pre-existing duplicate section number is resolved: the lane fill smoke section was
  numbered 92 alongside the Claude Code interstitial prompt section 92 (both matched by
  `SMOKE_ONLY=92`), so lane fill is renumbered 92 → 94. Live references updated to 94 with
  a note of the original number: `docs/testing-tracker.md` (lane fill smoke row, run recipe
  `SMOKE_ONLY=94`), `docs/testing.md` (lane fill results row), and the lane fill gate report
  (its quoted past commit tokens are untouched). `scripts/release-smoke.sh` is `bash -n`
  clean; the section bodies are unchanged, so its behaviour is the 5 of 5 recorded earlier.
- Re-run after the fix, all green:
  - `go test ./... -count=1`: exit 0, no FAIL lines (11071 tests across the package set,
    including `TestSmokeSectionCountDocMatches` pass).
  - `git grep` for duplicate `H "NN."` top-level numbers: the only remaining duplicate is the
    pre-existing section 50 pair, out of this branch's scope.
  - Gate (`bash .datawatch/verify/merge-pipeline-final-gate.sh`,
    (`DW_SKIP_TIDY_CHECK=1`): exit 0, `merge-pipeline-final-gate: ok`, same 79-check count
    lines (10/17/13/9 Go, 30 Node, 0 findings on gofmt/lint).
  - `node --test internal/server/web/app-merge-pipeline.test.js`: 30 pass, 0 fail.
  - `node --check internal/server/web/app.js`: clean.
- `datawatch-app#330` gained a second comment (2026-11-04) enumerating the four
  `merge_pipeline_no_project_*` locale keys with their English values, per the review.
