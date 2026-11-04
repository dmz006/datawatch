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
- PWA e2e story TS-844 (`scripts/test-stories/TS-844.sh` +
  `scripts/test-stories/pwa/merge-pipeline-card.mjs`) covers the card empty state and
  preselect at phone width (390x844), with no horizontal overflow.
- `datawatch-app#330` (the merge-pipeline sync issue) is commented with the new
  `project_required` contract for the native apps.

Note: the gate's node count extraction handles both the TAP reporter (`# pass N`) and the
node v24 spec reporter (`ℹ pass N`) summary lines; `./internal/router/` is part of the
checked packages. The node suite itself passes 30/0 under node v24.
