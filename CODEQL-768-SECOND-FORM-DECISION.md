# Decision: second barrier form for `internal/server/discussion_folder.go`

- Date: 2026-11-08
- Task: Try the second barrier form — CodeQL `go/path-injection` follow-up (alert 768)
- Required input: `.datawatch/reports/codeql-go-scan.md`

## Finding

`docs search` is not applicable; this is a task-gated decision. The report
file `.datawatch/reports/codeql-go-scan.md` does not exist in this scope, and
no `reports/` directory exists under `.datawatch/` (contents of `.datawatch/`:
`skills/`, `verify/`, `tree.lock`).

## Decision

Per the task rule — "If it shows no go/path-injection result in
`internal/server/bl332_discussion_scope.go`, `internal/server/bl332_discussion_sync.go`
or `internal/server/bl333_file_service.go`, make no change and say so" — the
absent report shows no such result, so **no code change was made**.
`internal/server/discussion_folder.go` is left exactly as committed (first
barrier form only: cleaned-path prefix check).

## Verification

- `ls .datawatch` + `find .datawatch -type f` — report absent (only
  `verify/discussion-folder-path-second-form.sh`, `skills/web-search-guidance/SKILL.md`,
  `tree.lock`).
- `bash .datawatch/verify/discussion-folder-path-second-form.sh` →
  `second barrier form check: ok` (gofmt clean, `go build ./internal/server/`,
  `TestDiscussionFolderPath|TestValidateDiscussionID|TestDiscussionRoutesRejectTraversalIDs` pass).
- No alerts dismissed; `scripts/codeql-local.sh`, workflow files and scanner
  configuration untouched.
