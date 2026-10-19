# Task 4046 — prd-create / template-instantiate project_dir CLI fix

## Context
CLI `newAutonomousPRDCreateCmd` and `newAutonomousTemplateInstantiateCmd` (cmd/datawatch/cli_autonomous.go) previously substituted `os.Getwd()` when `--project-dir` was omitted, silently binding new PRDs to the daemon caller's cwd. The daemon is supposed to own the default: worktree mode when `autonomous.worktree_base_repo` is configured.

## Change
- Removed `if projectDir == "" { projectDir, _ = os.Getwd() }` from both RunE bodies.
- `project_dir` is now sent as `""` when the flag is omitted; explicit `--project-dir` still forces that directory.
- `--project-dir` flag help text in both commands now reads: "Project directory; omit to let the daemon apply its default (worktree mode) instead of assuming the current directory" (no more "default: cwd").
- New test: `cmd/datawatch/prd_create_project_dir_test.go` — harness mirrors `runSetLLMCmd` (httptest server, `t.Setenv("HOME", t.TempDir())`, `writeReloadTestConfig` with `server:\n  port: <port>`), two cases: omitted flag → body `project_dir == ""` (not cwd); explicit flag → body equals the given dir.

## Scope notes
- Daemon handler (internal/server/autonomous.go) untouched — it already accepts empty project_dir.
- Other `--dir`/cwd defaults (project-summary, session new) intentionally left alone.

## Acceptance (all verified)
- `go build ./...` — clean
- `go vet ./cmd/datawatch/` — clean
- `go test -count=1 ./cmd/datawatch/` — 303 passed
- `go test -run TestPRDCreate -v` — both new tests PASS
