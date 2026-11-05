# Classification verdicts: final gate counts

Counts only. No transcripts.

| Item | Passed | Failed | Skipped |
|---|---|---|---|
| Daemon binary build (discarded output) | 1 | 0 | 0 |
| Go packages (classifier, autonomous, inference, server, mcp, router) | 10 | 0 | 0 |
| Go tests | 9544 | 0 | 11 |
| Repo checks | 10 | 0 | 0 |
| Fixtures replayed (`internal/classifier/testdata`) | 4 | 0 | 0 |

Test runs used `env -u TMUX -u TMUX_TMPDIR`. No daemon started, no `gh`, no version bump.

Repo checks run: no-unscoped-tmux, test-daemon-cleanup, no-internal-refs, go-toolchain-pins, no-identity-wipe, no-direct-ignore-writes, no-local-leaks, no-production-service-control, howto-coverage, docs-sync.

Gate result: clean.
