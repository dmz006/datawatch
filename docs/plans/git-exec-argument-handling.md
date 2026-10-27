# Git exec argument handling

Scope: exec.Command("git", ...) in internal/session (runGit, runGitEnv, gitOutput, Tracker.git).

## Data flow

- Fixed binary "git" everywhere; no shell is involved.
- Constant arguments: init, config, diff, ls-files, status, rev-parse, add, rm.
- commit: the message or file path is the value of -m / -F, so it is consumed as that flag's value and cannot become an option.
- Tracker.git: config key/value pairs come from fixed tables in this package.
- Variable, can start with '-' if unchecked:
  - TagCheckpoint tag: "datawatch-<kind>-<sessionID>"; the prefix keeps a leading '-' out, but the session id could carry spaces or control characters.
  - Rollback tag: "datawatch-pre-<sessionID>", same reasoning.
  - PushBranch remoteName, branch and originURL: caller supplied (cmd/datawatch main, agents post-session hook); originURL passes unchanged through injectTokenIntoHTTPS when it is not http(s), so a value such as "--upload-pack=x" could reach set-url and push.

## Decision

- Add validators in internal/session/git_args.go: reject empty, leading '-', NUL, newline and other control characters; refs follow check-ref-format rules; remote names also reject '/'.
- Apply them in TagCheckpoint, Rollback and PushBranch before any exec.
- Constant-argument call sites need no change.
- Regression tests in internal/session/git_args_test.go cover option-like values and normal values against temp repos.
