# Usage calculator lane stamp: sandbox live check

**Date:** 2026-10-30

Feature: the usage calculator no longer takes a Claude lane count from the person typing a
reading. The daemon stamps each reading with the Claude lanes the ledger sees running
(`lanes`, with `lanes_source`), the advice reports `lanes_running_now` apart from the
what-if, and `GET /api/usage/advice?lanes=N` is a what-if that is never saved.

This is a live smoke test. It changed no code. Its only repository output is this report.
Everything ran against a sandbox daemon: never the production daemon, the production config,
the systemd datawatch unit or port 8443.

## Parity surface

| Surface | Touched or excluded | Detail |
|---|---|---|
| REST | Touched | `POST /api/usage/readings` ignores a caller `lanes` and stamps `lanes` and `lanes_source`; `GET /api/usage/advice` returns `lanes_running_now`, `lanes_stamped` and `what_if_lanes`; OpenAPI updated (`docs/api/openapi.yaml`, `internal/server/web/openapi.yaml`). Steps 1 to 4 exercise this surface live. |
| MCP | Touched | `usage_reading_add` takes `lanes` as deprecated and ignored; `usage_advice` `lanes` is the what-if; `usage_readings` describes `lanes_source`. Covered by unit tests; not driven live in this check. |
| CLI | Touched | `datawatch usage reading add --lanes` is deprecated and not sent; `datawatch usage advice --lanes` is the what-if. Covered by unit tests; not driven live in this check. |
| Comm channel | Touched | `usage reading <percent> lanes=N` accepts and drops the lane value; `usage advice lanes=N` is the what-if (the advice text carries the `Reply: usage advice lanes=3` hint). The advice text a comm reply carries was read live in steps 3 and 4 (the `comm` field). |
| YAML/config | Excluded | No config key was added, renamed or removed. The existing `usage.calculator.*` keys are unchanged. |
| PWA | Touched | The reading form lost its lanes input; the calculator card gained the running-now line and the what-if control; locale keys added to all five bundles; story TS-840. The live PWA story was not run here (step 5, not run). |
| Android | Excluded from this change, tracked | The lane stamp changes the response shape the mobile app reads; this is filed as `dmz006/datawatch-app#315` (`docs/plans/parity-issues/2026-10-30-usage-lanes-stamp.md`). No Android code lives in this repository. |
| iPhone | Excluded from this change, tracked | Same issue, `dmz006/datawatch-app#315`. No iPhone code lives in this repository. |

## Sandbox

| Item | Value |
|---|---|
| Daemon binary | `/home/dmz/workspace/supervisor-scratch/b3270001-live/datawatch`, built with `env -u TMUX -u TMUX_TMPDIR go build -o <that path> ./cmd/datawatch/` (outside the repository; daemon reported version 9.42.2) |
| Config file | `testdata/datawatch.yaml` is the sandbox config; the daemon ran from its rewritten copy `/home/dmz/workspace/supervisor-scratch/b3270001-live/sandbox-datawatch.yaml`, produced with the same `sed` rewrite `scripts/run-tests.sh` uses: `data_dir` and `default_project_dir` moved to `/home/dmz/workspace/supervisor-scratch/b3270001-live/data` (outside the repository), listeners on `127.0.0.1`, ports `28080` (http), `28443` (https), `29090` (MCP SSE), `28053` (DNS channel), `28081` (webhook), `28180` (ntfy). The ports are not the template's `18xxx` set because those were already held by another sandbox that this check did not touch. |
| Base URL | `https://127.0.0.1:28443` (the https base URL the config sets; self-signed certificate, `curl -k`; no bearer token, `token: ""`) |
| Daemon pid | `2512852`, recorded by `td_start` in `scripts/test-daemon-lib.sh` (marker: the config path, confirmed in `/proc/2512852/cmdline`) |
| Stopped | `td_stop_one 2512852` returned 0; `/proc/2512852` no longer exists; ports 28443 and 28080 closed; the daemon log ends with `Shutting down... (signal=terminated)`. No process was killed by name. |
| Shell | Every command ran under `env -u TMUX -u TMUX_TMPDIR`. |

Health after start: `GET https://127.0.0.1:28443/api/health` answered `"status":"ok"`, `"version":"9.42.2"`,
`"hostname":"dw-e2e-test"`, with no auth required.

The daemon log also carried one unrelated line: `proxy sandbox listener: could not bind 127.0.0.1:8444 ... address already in use`.
That listener has a fixed port outside the sandbox config; it only means `/remote/` was unavailable in this sandbox. It did
not touch or depend on port 8443, and no step used it.

The sandbox started with an empty readings series and no Claude lease (`GET /api/usage/readings` returned
`{"readings":[],"extra_credit_points":0,...,"is_keeper":true}`).

## Steps

Clock at the start of step 1: `2026-10-31T01:25:32Z` (server local time `2026-10-30 21:25 EDT`).

### Step 1: reading with a caller lanes value and no `at`

Request:

```
POST https://127.0.0.1:28443/api/usage/readings
Content-Type: application/json
{"percent":40,"lanes":2}
```

Response, `HTTP/2 200`:

```
{"ok":true,"reading":{"id":"f40e9e8fedf4766a","at":"2026-10-31T01:25:32.927485317Z","percent":40,"lanes":0,"lanes_source":"live","source":""}}
```

Fields seen: `lanes` = 0, `lanes_source` = `live`. Expected: lanes 0 and `live`, because a fresh sandbox has no
Claude lease, so the caller's 2 did not change the stamp. **PASS**

### Step 2: reading with an `at` two hours before the clock

Request (`at` = clock minus two hours, `2026-10-30T23:25:38Z`):

```
POST https://127.0.0.1:28443/api/usage/readings
Content-Type: application/json
{"percent":41,"at":"2026-10-30T23:25:38Z"}
```

Response, `HTTP 200`:

```
{"ok":true,"reading":{"id":"65219bb8e7a272ae","at":"2026-10-30T23:25:38Z","percent":41,"lanes":0,"lanes_source":"current","source":""}}
```

Fields seen: `lanes` = 0, `lanes_source` = `current`. Expected: `current`, because no history exists before the daemon
started. **PASS**

### Step 3: plain advice

Request:

```
GET https://127.0.0.1:28443/api/usage/advice
```

Response, `HTTP 200`. Fields seen in `advice`: `lanes_running_now` = 0, `lanes_stamped` = 0, `lanes` = 0,
`what_if_lanes` absent. The `cli` text carries `Claude lanes running now (ledger count): 0` and
`Lanes stamped on the newest reading: 0`. Expected: `lanes_running_now` 0. **PASS**

Note: the same text's `advice:` line reads `Within budget: 3 lanes end the week at 44.4%`. That is the policy's
recommendation (policy `use-all`), not a lane count in use, and it is separate from the figures above.

### Step 4: what-if, then plain again

Request:

```
GET https://127.0.0.1:28443/api/usage/advice?lanes=2
```

Response, `HTTP 200`. Fields seen in `advice`: `lanes` = 2, `what_if_lanes` = 2, `lanes_running_now` = 0,
`lanes_stamped` = 0. The `cli` text carries the line `What-if lanes (not saved): 2` followed by `lanes 2 running`,
and still carries `Claude lanes running now (ledger count): 0`. Expected: the chosen lane count is 2 and the text
names it as the what-if. **PASS**

Then a plain request again:

```
GET https://127.0.0.1:28443/api/usage/advice
```

Response, `HTTP 200`. Fields seen: `lanes` = 0, `lanes_running_now` = 0, `lanes_stamped` = 0, `what_if_lanes`
absent, no `What-if lanes` line in the text. The stamped default is back and the what-if left nothing behind.
**PASS**

### Step 5: the new PWA story (TS-840)

**Not run.** Reasons, all checked here:

- Playwright is not installed for the story. `scripts/test-stories/pwa/lib.mjs` imports `playwright`; from that
  directory `require.resolve('playwright')` fails and `import('playwright')` fails with `ERR_MODULE_NOT_FOUND`.
  `scripts/test-stories/pwa/node_modules` is a symlink whose target does not exist (dangling), although
  `package.json` lists Playwright.
- Installing it was not done: `scripts/e2e-pwa.sh` would run `npm ci` through that dangling link, which writes
  outside the project directory and needs the network. This check writes only its report.
- `scripts/run-tests.sh --stories=TS-840` could not select the story in any case: that runner only globs
  `scripts/test-stories/TS-*.sh`, and the new story is `scripts/test-stories/pwa/TS-840.mjs`, which is run by
  `scripts/e2e-pwa.sh` (or `run-all.mjs`). No `TS-840.sh` exists.

The story's own assertions are therefore unverified live. The PWA code it covers is covered by the unit tests
`internal/server/web/app-usage-calculator-lanes.test.js` and `app-usage-calculator-frame.test.js`.

## Result

| Step | Outcome |
|---|---|
| 1. Reading with `lanes` 2, no `at`: `lanes` 0, `lanes_source` `live` | PASS |
| 2. Reading with `at` two hours back: `lanes_source` `current` | PASS |
| 3. `GET /api/usage/advice`: `lanes_running_now` 0 | PASS |
| 4. `?lanes=2` names 2 as the what-if; plain GET shows the stamped default again | PASS |
| 5. PWA story TS-840 | not run (Playwright not installed; see above) |

Steps failed: none. Step 5 was not run, so the PWA story remains unverified live; this is recorded as not run, not
as a pass. Sandbox daemon 2512852 was stopped by its recorded pid and confirmed gone.

Result: PASS
