# Judge size threshold — end-to-end verification (b2050001)

Date: 2026-10-27. Branch `automaton/b2050001`, HEAD `36d9522a`. No code was changed.

## Verdict

- **Tested: yes.** Build, vet, the full unit suites, the four guard scripts and lint all pass.
- **Validated: no.** No live connection to a daemon carrying the feature was observed. The only
  reachable sandbox runs a stale build (`8.0.0-e2e`) that does not have
  `judge_size_threshold_lines`. The smoke story skipped and the setting round trip could not be
  completed. Do not treat the feature as validated.

## Results

| # | Command | Exit | Result |
|---|---------|------|--------|
| 1 | `go build -o <tmpdir>/datawatch ./cmd/datawatch/` (tmpdir outside the repo) | 0 | built |
| 2 | `go vet ./internal/config/ ./internal/autonomous/ ./internal/mcp/ ./internal/router/ ./internal/server/ ./cmd/datawatch/` | 0 | clean |
| 3 | `go test -count=1` on the same six packages (full, unfiltered) | 0 | all `ok`: config 1.8s, autonomous 25.6s, mcp 1.9s, router 88.1s, server 57.7s, cmd/datawatch 91.6s |
| 4 | `bash scripts/check-no-unscoped-tmux.sh` | 0 | OK |
| 5 | `bash scripts/check-no-production-service-control.sh` | 0 | OK |
| 6 | `bash scripts/check-no-internal-refs.sh` | 0 | PASS |
| 7 | `bash scripts/check-howto-coverage.sh` | 0 | PASS |
| 8 | `make lint` (golangci-lint is installed at `~/go/bin/golangci-lint`) | 0 | `0 issues.` |
| 9 | `scripts/run-tests.sh --stories=TS-825` with `DW_BASE=https://127.0.0.1:18443` | 0 | **SKIPPED, not passed**: `0 passed 0 failed 1 skipped` |
| 10 | Setting round trip on the sandbox | — | **Not confirmed** (see below) |

Tests ran with `env -u TMUX`. An earlier run of the same command (before `-count=1`) was served
from the Go test cache; the numbers above are from the uncached run.

## Smoke (TS-825) — did not run

`GET https://127.0.0.1:18443/api/health` returned `{"hostname":"dw-e2e-test","version":"8.0.0-e2e"}`.
The sandbox is reachable but predates this feature. `GET /api/config` lists no
`autonomous.judge_size_threshold_lines`, and the story reported
`SKIP [TS-825] GET /api/autonomous/config has no judge_size_threshold_lines on this daemon`.
The exit code 0 reflects a skip, not a pass.

To get a real Validated result, redeploy the sandbox from this branch (the binary from row 1)
and rerun `scripts/run-tests.sh --stories=TS-825`.

`run-tests.sh` creates its work directory next to the repository (outside the allowed write
scope), so it was run from a symlink farm in the session scratchpad. The farm was deleted
afterwards.

## Setting round trip — failed against the stale sandbox

- `PUT /api/config {"autonomous.judge_size_threshold_lines":0}` → HTTP 500,
  `save failed: write config /etc/datawatch/config.yaml: ... read-only file system`.
  Nothing was changed, so there was nothing to restore to 400; the key does not exist there.
- The GET-returns-0 step and the restore-to-400 step were therefore not reached.

## Side effect on the sandbox (my mistake, restored)

I probed `PUT /api/autonomous/config {"judge_size_threshold_lines":0}`. The stale daemon
treats that PUT as a full replace, not a merge, and answered 200. The object was reduced to
`{"enabled":false,"scan":{...}}`, dropping `poll_interval_seconds`, `max_parallel_tasks`,
`planning_backend`, `max_recursion_depth` and `auto_approve_children`.

I restored the object from the GET captured immediately before the PUT. `GET
/api/autonomous/config` now returns the identical pre-PUT body:

```
{"enabled":true,"poll_interval_seconds":30,"max_parallel_tasks":3,"planning_backend":"ollama","max_recursion_depth":5,"auto_approve_children":true,"scan":{"enabled":false,"sast_enabled":false,"secrets_enabled":false,"deps_enabled":false}}
```

Caveats: this was only compared through that one endpoint. I did not record `GET /api/config`'s
`autonomous` block (which shows `max_parallel_tasks` and `poll_interval_seconds` as 0) before the
PUT, so I cannot say whether that view changed. The sandbox is a disposable e2e daemon, but the
operator may want to restart or redeploy it anyway.

## Suggested follow-up (not done — out of scope)

The full-replace behavior of `PUT /api/autonomous/config` on the stale build is worth checking
on the current build, since a partial PUT of one key must not wipe the other settings.
