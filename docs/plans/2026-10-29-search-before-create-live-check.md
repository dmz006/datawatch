# Search before create: sandbox live check

Sandbox: yes
Sandbox URL: https://127.0.0.1:28443 (TLS). The plain HTTP listener on 127.0.0.1:28080 redirects to it.
Sandbox port: 28443 (TLS) and 28080 (HTTP), loopback only.

Ports 18080 and 18443 were already in use by another process before this run, so the check did not use them. The production daemon and its port were not used at any point.

## Setup

- Binary: `.datawatch/live-check/datawatch`, built from this tree with `go build -o .datawatch/live-check/datawatch ./cmd/datawatch/`.
- Config: `.datawatch/live-check/config.yaml`, derived from `testdata/datawatch.yaml`. Every path is under `.datawatch/live-check/`. The sections that bind extra ports or reach outside hosts are off (MCP SSE, DNS channel, webhook, ntfy, Open WebUI, vision, whisper, compute nodes, memory, council, ollama). `autonomous.automation.create_search_mode` starts at `shadow`. Allowed project roots are limited to `.datawatch/live-check/workspace`.
- Driver: `.datawatch/live-check/run-live-check.sh`. It starts the daemon with `td_start` from `scripts/test-daemon-lib.sh` under `td_install_traps`, with `TD_STATE_DIR` set to `.datawatch/live-check/state`. It ends the daemon with `td_stop_one`. The trap runs `td_stop_all` on any other exit path. Nothing is stopped by name.
- Evidence: `.datawatch/live-check/evidence/` (one `.cmd`, `.code` or `.rc`, and `.body` or `.out` file per step, plus `checks.tsv` and `run.log`). Those files are scratch and are not tracked.

## Commands and exit codes

| # | Command | Exit code |
|---|---|---|
| 1 | `go build -o .datawatch/live-check/datawatch ./cmd/datawatch/` | 0 |
| 2 | `bash .datawatch/live-check/run-live-check.sh` (run 2, final; drives steps 3 to 17 below) | 0 |
| 3 | `datawatch --config .datawatch/live-check/config.yaml --url https://127.0.0.1:28443 autonomous overlap --spec <spec 1>` | 0 |
| 4 | same as 3, with `--json` | 0 |
| 5 | `DW_LIVE_OVERLAP_SMOKE=https://127.0.0.1:28443 go test -count=1 -v -run 'TestOverlapSmokeLive$' ./internal/server/` | 0 |
| 6 | `gofmt -l internal/server/overlap_live_smoke_test.go` (no output) | 0 |
| 7 | `go vet ./internal/server/` | 0 |
| 8 | `go test -count=1 -v -run TestOverlapSmoke ./internal/server/` (in process, no daemon) | 0 |

Run 1 of the driver also exited 0. It ran before step 5 was added to the driver, and it used different Automaton IDs. Run 2 is the one reported here. Before run 2 the sandbox config key was set back to `shadow`, because the daemon's own `PUT /api/config` in run 1 had left it at `on`.

The two specs were:

- Spec 1: `Rotate the sandbox audit archive nightly, compress each dated copy with gzip, and keep only the newest fourteen archives on the lane storage volume.`
- Spec 2: the same text with `Please.` added at the end.

## Observed responses (run 2)

Each line is the status code and a trimmed body. Automaton IDs are as the daemon issued them in this run.

| Step | Request | HTTP | Observed |
|---|---|---|---|
| 1 | `GET /api/config` | 200 | `autonomous.automation.create_search_mode` is `shadow` at start |
| 2 | `POST /api/autonomous/prds` (spec 1, `project_dir` in the sandbox workspace) | 200 | `{"id":"13560001","status":"draft"}`. No `overlap_warnings` key in shadow mode |
| 3 | `POST /api/autonomous/prds` (spec 2) | 200 | `{"id":"33f90001","status":"draft"}`. No `overlap_warnings` key in shadow mode |
| 4 | `GET /api/audit?action=automaton_create_search&limit=50` | 200 | Entry with `subject` `33f90001`, `details.mode` `shadow`, and warnings `[{"kind":"automaton","id":"13560001","score":1}, {"kind":"unavailable",...}]`. The first Automaton is named |
| 5 | `PUT /api/config` `{"autonomous.automation.create_search_mode":"on"}` | 200 | Accepted |
| 6 | `GET /api/config` | 200 | `create_search_mode` is `on`, `create_search_threshold` is `0.5` |
| 7 | `PUT /api/config` `{"...create_search_mode":"bogus","...create_search_threshold":0.7}` | 400 | `autonomous.automation.create_search_mode must be "off", "shadow" or "on", got "bogus"` |
| 8 | `GET /api/config` (after step 7) | 200 | Mode still `on`, and the valid threshold in the refused patch was not applied |
| 9 | `POST /api/autonomous/prds` (spec 1, mode on) | 200 | `{"id":"55480001","status":"draft","overlap_warnings":[{"kind":"automaton","id":"13560001","score":1,...}, {"kind":"automaton","id":"33f90001","score":1,...}, {"kind":"unavailable","score":0,...}]}` |
| 10 | `GET /api/autonomous/overlap?spec=<spec 1>` | 200 | `{"mode":"on","threshold":0.5,"warnings":[{"kind":"automaton","id":"13560001","score":1,...}, {"kind":"automaton","id":"33f90001","score":1,...}, {"kind":"automaton","id":"55480001","score":1,...}]}` |
| 11 | CLI `autonomous overlap --spec <spec 1>` | exit 0 | Three lines, one per Automaton: `automaton 13560001 status draft score 1.00: Automaton 13560001 already describes this plan.` and the same for `33f90001` and `55480001` |
| 12 | CLI `autonomous overlap --spec <spec 1> --json` | exit 0 | Three automaton warnings, same IDs and scores |
| 13 | `go test -run 'TestOverlapSmokeLive$'` against the sandbox | exit 0 | `sandbox create_search_mode = on`; `sandbox overlap read: mode on, threshold 0.5, 3 warnings`; `--- PASS: TestOverlapSmokeLive` |

The warning for `13560001` in the step 9 reply and in the step 10 read is the same object: the driver compares the two JSON objects with sorted keys, and they match.

## Checks

The driver recorded 31 checks in run 2. All passed, and none failed. They cover:

- the daemon answers, and the mode is `shadow` at start
- the first create is a draft, and its reply has no `overlap_warnings` in shadow mode
- the second create has no `overlap_warnings` in shadow mode, and its shadow audit entry names the first
- `PUT /api/config` sets `on`, and a bad mode with a valid threshold is refused with 400 and changes nothing
- the third create's `overlap_warnings` names the first
- the read route names the first, reports `on` and threshold `0.5`, and returns the same warning object as the create reply
- the CLI exits 0 in text and JSON modes, and its output names the first
- `TestOverlapSmokeLive` passes against the sandbox and is not skipped
- each created Automaton is deleted with HTTP 200 and then returns 404; none is listed afterward
- the port closes and the daemon's PID is gone after `td_stop_one`

## Cleanup result

- Every Automaton the run created was deleted with `DELETE /api/autonomous/prds/{id}?hard=true`: `13560001`, `33f90001` and `55480001`. Each delete returned 200, and each follow-up `GET` returned 404.
- `GET /api/autonomous/prds` after the deletes (HTTP 200) lists none of the three.
- The Automata from run 1 (`c8040001`, `e51d0001`, `05af0001`) were deleted the same way in run 1, with the same results.
- The daemon was ended with `td_stop_one` (exit 0). Port 28443 closed, and the recorded PID was gone.

## Notes and deviations

- The create route takes `spec` and `project_dir`, not a title. The stored Automaton has no title at create, so the search text is the spec alone.
- The sandbox project directory has no registry file, so the registry source reports `unavailable` in the create replies and audit entries. The read route passes no project directory, so it never reports that source. This difference is what the code does, not a fault of the check.
- The daemon's own `PUT /api/config` rewrites `config.yaml` in the sandbox directory. That rewritten file is the sandbox state after the run.
- The daemon logged that its proxy sandbox listener on 127.0.0.1:8444 could not bind, because the port was already in use. That listener is not part of this check, and the check did not depend on it.

## Tracker coverage

Rows of `docs/testing-tracker.md` whose Validated column this check changed, because the behaviour the row describes was observed live:

- Overlap config keys (REST): `GET` and `PUT /api/config`, the 400 for a bad value, and a refused whole patch that changed nothing.
- Create route: shadow mode (no warnings in the reply, one audit entry that names the first) and mode `on` (warnings in the reply).
- Read route: the three-field reply (`warnings`, `threshold`, `mode`) and the same warning as the create reply.
- CLI `datawatch autonomous overlap`: the text and JSON output for the spec, exit 0.

Behaviours in those rows that were not observed live, and are covered only by the Go unit tests: the create route with mode `off` and a failing or panicking search; the read route's 400 for blank input and a bad limit, its 500 for a failed search, its 503 with no manager, and its refusal of other methods; the CLI's local refusals and its pass-through of daemon errors.

Not observed by this check, and left at Validated `No`: the comm verb, the MCP tool, and the PWA warnings block and Settings fields. The row for the live check on a running daemon, which includes the PWA, stays `No`.

Result: PASS
