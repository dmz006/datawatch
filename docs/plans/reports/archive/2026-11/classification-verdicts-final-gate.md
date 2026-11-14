# Classification verdicts: final gate counts

Counts only. No transcripts.

| Item | Passed | Failed |
|---|---|---|
| Daemon binary build (discarded output) | 1 | 0 |
| Go packages (classifier, autonomous, inference, server, mcp, router) | 10 | 0 |
| Go tests | 9538 | 0 |
| Repo checks | 9 | 1 |
| Fixtures replayed (`internal/classifier/testdata`) | 4 | 0 |

Test runs used `env -u TMUX -u TMUX_TMPDIR`. No daemon started, no `gh`, no version bump.

Repo checks run: no-unscoped-tmux, test-daemon-cleanup, no-internal-refs, go-toolchain-pins, no-identity-wipe, no-direct-ignore-writes, no-local-leaks, no-production-service-control, howto-coverage, docs-sync.

Failed check: no-local-leaks (1 address finding, a private-range test address literal in `cmd/datawatch/classify_runtime_wiring_test.go`). This gate changes no source files, so it is reported, not fixed.

Gate result: not clean (1 check failed).
