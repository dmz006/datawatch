# Release Notes — v6.12.3

Released: 2026-05-06
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.3

### Fixed

- **`internal/session/manager.go` `processOutputLine` structured-channel branch** — operator-debugged against live session a95f after another restart: "session list shows session a95f as waiting input with a last response visible; but it is clearly still running... session list isn't updating". Root cause: this branch returned at line 4156 BEFORE reaching the v6.12.2 LCE-bump path at line 4357, so claude-code MCP / opencode-acp sessions never bumped `LastChannelEventAt` from log-file output. Gap watcher kept them in WaitingInput indefinitely. Now bumps LCE at the top of the structured-channel branch (positive evidence of activity → resets watcher gap and flips back to Running if drifted).

### Tests

- 525 targeted tests pass (`./internal/session/...` + `./internal/server/...`) — the only packages this patch touches.
- Full regression suite (1804) + smoke not run for this patch per the patch-test rule (patches test only impacted packages; minor/major releases run full regression + smoke).
