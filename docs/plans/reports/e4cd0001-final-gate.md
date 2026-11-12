# Final Gate: Stats Installer Archive-Name Defect

- Date: 2026-11-03 (re-verified; originally 2026-11-02)
- Task: e4cd0001 final gate
- Verification: final-gate verification script (full run)

## Result

PASS. 8 checks passed, 0 failed. (The original 2026-11-02 run failed on
docs-sync; that failure was fixed upstream and this gate was re-run — every
check re-executed on 2026-11-03 and all now pass.)

## Checks

| Check | Result |
|---|---|
| installer-syntax | pass |
| installer-shellcheck | pass |
| archive-name-tests (Go) | pass |
| docs-curated-howtos | pass |
| docs-howto-coverage | pass |
| docs-sync | pass |
| no-version-bump | pass |
| local-archive install (tarball under GoReleaser asset name, redirected HOME) | pass |

## Totals

- Passed: 8
- Failed: 0

## Re-verification (2026-11-03)

- docs-sync: `scripts/check-docs-sync.sh` — "docs/ and
  internal/server/web/docs in sync.", exit 0. (The original failure no longer
  reproduces on this branch head.)
- installer-syntax: `bash -n install/install-stats.sh` — clean.
- installer-shellcheck: `shellcheck install/install-stats.sh` — clean.
- archive-name-tests: `go test ./tests/ci/` — 79 tests pass, including the
  installer/goreleaser archive-name cross-check.
- docs-curated-howtos: `scripts/check-curated-howtos.sh` — PASS (23 howtos).
- docs-howto-coverage: `scripts/check-howto-coverage.sh` — PASS.
- no-version-bump: `var Version = "9.67.0"` unchanged in both
  `cmd/datawatch/main.go` and `internal/server/api.go` across
  `df8a850f3..HEAD`.
- local-archive install: `go build ./cmd/datawatch-stats/` into a
  `datawatch-stats_9.67.0_linux_amd64.tar.gz` tarball;
  `HOME=<tmp> bash install/install-stats.sh --datawatch <url> --name
  gate-check --archive <tarball>` installed the binary to
  `<HOME>/.local/bin/datawatch-stats` (runs, `--version` answers) with no
  download and no version fetch.

No daemon started, no gh usage, no version variable changed, no installer or
source logic edited (only this report was re-verified; the CHANGELOG
catch-up entry and the AGENT.md commit-type note landed separately under the
rules-review fix for this branch).
