# Alert fix verification report

Recorded 2026-10-26. End-to-end check of the code-scanning alert work on this branch (path-injection, certificate-check and JS sanitization alerts, plus the CodeQL Analyze workflow).

## Results

| Check | Result |
|-------|--------|
| `go build -o datawatch ./cmd/datawatch/` | pass |
| `go vet` for session, autonomous, gitguard, pipeline, server | pass, no output |
| `go test` for the same five packages | pass |
| `python3 scripts/check_accepted_risks.py` | pass: 182 entries valid; 2 pre-existing escalation notices (CVE-2025-26625, CVE-2026-7210) need operator review and are unrelated to this branch |
| `bash scripts/check-no-unscoped-tmux.sh` | pass |
| `bash scripts/check-no-internal-refs.sh` | pass |

## Decisions

Every alert in `docs/plans/path-injection-alert-decisions.md` (path-injection 658, 675-677, 681, 687-690, 695, 696; certificate and JS alerts 630, 631, 664, 665, 671-674, 685, 694, 697-699 and the Go disabled-certificate alert) carries a fix or dismiss decision with a reason. The sixteen dismissals each have a `code-scanning/<number>` entry in `security/accepted-risks.yml`, which the schema check accepts.

## Regression tests for fixes

- 695, 696, 658: `internal/session/project_dir_test.go`, `internal/gitguard/contain_test.go`
- 676, 677: `TestCleanTempPacksRefusesSymlinkEscape` in `internal/gitguard/policy_test.go`
- 681: `TestRunTestsRefusesUnsafeProjectDir` in `internal/pipeline/quality_test.go`
- Go disabled certificate check: `internal/server/node_usage_tls_test.go`, `cmd/datawatch/compute_node_config_test.go`
- 697, 698: the story scripts `TS-821.mjs` and `TS-824.mjs` are themselves the tests; the fix is in them. No separate unit test exists.

## Tracker IDs and versions in the diff

`git diff main...HEAD` was searched for BL###, B###, GH### and release version numbers in added lines and in file names. No file, function or test name carries one. Alert numbers appear only in decision docs and the accepted-risks registry, where they identify external alerts.

## Failures

None in the requested checks. Not fixed, out of scope: the two escalation notices above.
