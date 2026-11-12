# Web UI load performance

**Date:** 2026-10-23
**Version:** v9.5.105
**Status:** Done 2026-10-23: Phases 1 to 5 are complete (measurement, slim
summary list endpoints with paging + ETag, gzip + conditional requests, load by
visible tab with loading/cached/stale/failed states with Retry, seeded live-style
re-measure + byte-budget tests). Docs (`docs/howto/list-endpoints-and-paging.md`),
the testing-tracker section and this close-out landed in PRD `791a0001`. No
version bump; all changes ship with the daemon's next release alongside the
`3cda0001` code.
**Scope:** `internal/server/` (list endpoints behind `/api/sessions`,
`/api/autonomous/prds`, `/api/automations`, `/api/alerts`, response compression),
`internal/server/web/app.js` (what the UI fetches on load and per tab),
`internal/server/perf_seed_test.go`, `scripts/perf-measure-tabs.mjs`.

## Problem

On a daemon with a lot of history the web UI is slow to show anything, most of all
over a mobile link. The list endpoints the UI calls on load return whole records,
including large per-record fields (session `task` text, story and task payloads,
verifier text) that the list views never display.

## Tools

- **Seed and endpoint measurement:** `internal/server/perf_seed_test.go`.
  `SeedPerfServer(t, dir)` builds a deterministic, live-data-independent Server with
  3000 sessions (150 carry a 128 KB `task`; 120 have an output log and transcript on
  disk), 300 Automata (six carry 7, 8, 9, 10, 11 and 13 MB of story data: task specs,
  errors, checkpoint refs, output tails and verifier text), 20 Automations and 500
  alerts. `MeasureLoadEndpoints(t, srv)` returns raw bytes, gzipped bytes and server
  time per endpoint. Run it with:

  ```bash
  PERF_REPORT_OUT=perf-report.json go test ./internal/server/ -run 'TestPerfSeed|TestEndpointMeasure' -v -count=1
  ```

- **Per-tab time to first content:** `scripts/perf-measure-tabs.mjs`, against any
  daemon (`TEST_HTTP`, `TEST_TOKEN`), with an optional `--slow-4g` pass that throttles
  through the Chrome DevTools Protocol. It prints JSON. Point it at a daemon started on
  seeded data to reproduce the browser-side numbers.

## Baseline

Measured on the seeded data above, in-process (`httptest`, no network), 2026-10-23.
Gzip is Go's default level. Server time is the handler's wall time for one request
and varies a little between runs.

| Endpoint | Path | Status | Raw bytes | Gzip bytes | Gzip ratio | Server ms |
|---|---|---|---|---|---|---|
| sessions | `/api/sessions` | 200 | 35,883,863 | 11,888,196 | 33% | 58.8 |
| autonomous prds | `/api/autonomous/prds` | 200 | 82,960,495 | 27,870,783 | 34% | 124.2 |
| automations | `/api/automations` | 200 | 9,230 | 1,250 | 14% | 0.3 |
| capacity | `/api/capacity` | 200 | 201 | 162 | 81% | 10.3 |
| alerts | `/api/alerts` | 200 | 218,550 | 63,272 | 29% | 0.5 |
| observer stats | `/api/observer/stats` | 200 | 54 | 79 | 146% | 0.0 |
| observer envelopes | `/api/observer/envelopes` | 200 | 10,376 | 862 | 8% | 0.2 |
| config | `/api/config` | 200 | 8,222 | 2,682 | 33% | 0.1 |
| llms | `/api/llms` | 200 | 12 | 37 | 308% | 0.0 |

Reading the numbers:

- Two endpoints are the whole problem. `/api/autonomous/prds` (83 MB raw, 28 MB
  gzipped) and `/api/sessions` (36 MB raw, 12 MB gzipped) are about 99.9% of the bytes
  the load path moves. Everything else together is under 0.3 MB.
- Server time is not the bottleneck (about 60 ms and 120 ms). Transfer and the
  browser's JSON parse are. At the slow-4G preset (1.6 Mbps down) 28 MB gzipped is
  about 140 s of transfer for the Automata list alone.
- Gzip only gets the lists to about a third of their size. Cutting the fields the
  list views do not show is the larger win; compression is the smaller one.
- The observer stats and llms bodies are tiny, so gzip overhead makes them larger.
  Compression should skip small bodies.

## Root cause of oversized Automata

An Automaton is one JSON object (`autonomous.PRD`, `internal/autonomous/models.go`)
holding `Story[]`, each with `Tasks[]`. Everything is stored whole and returned whole,
so the record's size is the sum of the text its tasks carry. Nothing in the record is a
byte-bounded log; the 13 MB case is many tasks times several text fields each.

Per-task size measured on the seeded data (`perfBuildAutomaton`; the six heavy Automata
have 8 stories x 12 tasks = 96 tasks; each of the five heavy fields below gets about a
fifth of the per-task budget):

| Automaton (story data) | Bytes per heavy field, all 96 tasks | Per task, per field | Rest of the record (ids, titles, timestamps) |
|---|---|---|---|
| 7 MB | 1,467,936 | 15,291 | 174,226 |
| 8 MB | 1,677,696 | 17,476 | 192,680 |
| 9 MB | 1,887,360 | 19,660 | 211,695 |
| 10 MB | 2,097,120 | 21,845 | 230,432 |
| 11 MB | 2,306,784 | 24,029 | 248,888 |
| 13 MB | 2,726,208 | 28,398 | 286,736 |

For the 13 MB case, 13,918,160 bytes of story JSON are 5 x 2.73 MB of text plus 0.29 MB
of structure: 98% is text.

Field by field, what grows and what the code bounds:

| Field | Where | Bounded by the code? | Notes |
|---|---|---|---|
| `Task.Spec` | `models.go` Task | No | Written by the decomposer or an operator edit; one copy per task, grows with how much context a spec carries. |
| `Task.Error` | Task | No | Set on each failed attempt from the failure text; a long traceback or tool output lands here whole. Overwritten, not appended, but unbounded in length. |
| `Task.StartFailureTail` | Task | By line count only (last 20 non-empty lines) | A line can be long, so 20 lines is not a byte bound. |
| `Task.AttemptRefs` | `attempt_record.go`, `attempt_snapshot.go` | Entries are short git ref names | `refs/datawatch/attempts/<task>/<n>`, about 40 bytes each, one per killed attempt. The snapshot content lives in git, not in the record. The seed stores payload text here as a stand-in for the heavy attempt data. |
| `Task.Verification.RawJudge` | `VerificationResult` | Yes, 16 KiB at write | Truncated by the verifier before it is stored. Still 16 KiB x tasks. |
| `Task.Verification.Summary` / `Issues` | `VerificationResult` | No | Free text from the verifier. |
| `Task.Verdicts[].RawJudge` / `Summary` / `Issues` | `GuardrailVerdict` | RawJudge 16 KiB | One verdict per guardrail per task and story. |
| `Task.PriorAttemptHint` | Task | No | Copy of the outgoing attempt's error and verification, per reset; single-use. |
| `Story.Description`, `PRD.Spec`, `PRD.MemoryReport` | Story, PRD | No | One per story or Automaton; small next to the per-task fields. |
| `PRD.Decisions[].Note` | PRD | No | Appended on every decision; grows with age. Not capped in this change; the summary carries only `decision_count`. |
| Checkpoints (`CHECKPOINT.md`) | `checkpoint_done.go` | n/a | A file in the worker's directory that `checkpointSaysAllDone` reads. It is never copied into the Automaton record. |
| Learnings (`autonomous.Learning`) | `models.go` | n/a | Saved to datawatch memory, not stored on the PRD. |

So the unbounded growth is task text, multiplied by the task count and by retries
(each retry rewrites `Error`, appends to `AttemptRefs`, `AttemptCommitSHAs`, `NudgeEvents`
and `DeliveryFailureEvents`). The only byte bound anywhere is the 16 KiB on judge text,
which is itself 1.5 MB at 96 tasks.

The list is not affected by these fields: `BuildSummaryView` and `BuildSummaryRow`
copy scalars and counts only, and `TestPRDDetailCapListSummaryDoesNotReadHeavyFields`
proves none of the heavy text reaches the list body.

### Fix: cap the by-id view, keep the store whole

- `GET /api/autonomous/prds/{id}` caps each text field above at
  `autonomous.detail_text_cap_bytes` (default 4096; 0 = default; negative = off). A cut
  field ends with `…[truncated: kept N of M bytes]`, and the record carries a `truncated`
  list of `{field, bytes, kept, ref}`, where `ref` is the URL of the full text.
- `?full=1` returns the whole record, as before.
- New sub-resource `GET /api/autonomous/prds/{id}/outputs[?task=<id>]` and
  `GET /api/autonomous/prds/{id}/tasks/{task_id}/outputs` return the heavy fields uncapped.
- The cap works on a copy; the store is never modified. `edit_task` refuses a spec that
  still carries a truncation marker so a capped copy cannot overwrite the stored text.
- Effect on the 13 MB seeded Automaton at the 4096 default: the five heavy fields drop
  from 28,398 to at most 4,096 + 40 bytes each, so the by-id reply falls from about
  13.9 MB to about 2.1 MB. At a 16 KiB cap it was still 8.1 MB, which is why the default
  is 4096.
- Tests: `TestPRDDetailCap*` in `internal/server/prd_detail_cap_test.go`.

## After the fix

Same seeded data, same in-process method, first page as the web UI requests it on
load (`TestPerfBudgetFirstPage`, 2026-10-23). The baseline column is the Baseline
table above.

| Endpoint | Path (after) | Baseline raw | After raw | After gzip | Reduction (raw) |
|---|---|---|---|---|---|
| sessions | `/api/sessions?view=summary&limit=50` | 35,883,863 | 16,352 | 2,045 | 99.95% |
| autonomous prds | `/api/autonomous/prds` | 82,960,495 | 57,411 | 3,476 | 99.93% |
| automations | `/api/automations` | 9,230 | 9,229 | 1,246 | 0% |
| alerts | `/api/alerts?limit=50` | 218,550 | 21,952 | 6,845 | 90% |

The first-page Automata reply is 57 KB (about 3.5 KB gzipped), down from 83 MB, so at
the slow-4G rate the transfer is well under a second instead of about 140 s. The
explicit full forms (`/api/sessions`, `/api/autonomous/prds?full=1`) still return the
complete objects, at the baseline sizes, for clients that ask for them.

Server time for the Automata list fell from 124 ms to about 1 ms
(`TestEndpointMeasureLoadPath`). The live browser pass of
`scripts/perf-measure-tabs.mjs` is not recorded: no seeded daemon was available.

## Result against the live-style data set

2026-10-23, against the seeded live-style data set (3000 sessions, 300 Automata,
20 Automations, 500 alerts), served as a real HTTP daemon by the new blocking test
`TestSeededLocalServerServe` (`internal/server/perf_serve_test.go`, `PERF_SERVE_ADDR`),
not in-process. The endpoint rows are `TestEndpointMeasureLiveStyleBeforeAfter`
(`internal/server/perf_live_style_measure_test.go`); the tab rows are the per-tab
time-to-first-content driver (`pwa-tab-ttfb.mjs`) running the same classifier and
profiles as `scripts/perf-measure-tabs.mjs` (cold context per profile, CDP slow-4g/
fast throttling, Playwright over system Chromium).

### Endpoints: old full form vs first page

| Endpoint | Before path | Before raw (bytes) | Before gzip (bytes) | Before Server ms | After path | After raw (bytes) | After gzip (bytes) | After Server ms |
|---|---|---|---|---|---|---|---|---|
| Sessions | `/api/sessions` | 35,886,983 | 11,886,946 | 62.8 | `/api/sessions?view=summary&limit=50` | 16,352 | 2,045 | 1.3 |
| Automata | `/api/autonomous/prds?full=1` | 82,960,499 | 27,870,448 | 123.5 | `/api/autonomous/prds` | 57,410 | 3,466 | 1.2 |
| Automation | `/api/automations` | 9,233 | 1,249 | 0.3 | `/api/automations` | 9,233 | 1,249 | 0.1 |
| Alerts | `/api/alerts` | 218,530 | 63,258 | 0.4 | `/api/alerts?limit=50` | 21,951 | 6,839 | 0.1 |

The first page is 2,195x smaller for Sessions and 1,445x smaller for Automata;
the gzip overhead does not change the conclusion. The explicit full forms still
return the complete objects (guard: `TestPerfBudgetFullFormStillComplete`).

### Tabs: time to first content, ms (budget in parentheses)

| Tab | slow-4g (budget 3000) | fast (budget 1500) |
|---|---|---|
| Sessions | 221 (3000) | 208 (1500) |
| Automata | 282 (3000) | 40 (1500) |
| Automation | 164 (3000) | 6 (1500) |
| Observer | 649 (3000)* | 20 (1500)* |
| Settings | 16 (3000) | 13 (1500) |

Every tab reaches its first content well under its budget on both profiles.
\* The Observer tab's System Statistics cards call `/api/stats` and
`/api/schedules`, which the seeded Server does not wire up, so the tab's final
state is the pre-existing "stats unavailable" failed state; its first content
(the loading skeleton) is still what is timed. Not a PWA defect.

### Comparison with the 2026-10-23 live baseline

Live baseline on the production daemon (2026-10-23): PRD list 339 MB, session
list 32 MB, tab load 15-40 s. This fixture is a smaller but same-shape data set
(300 Automata; live history is larger), and it reproduces the failure mode at
baseline scale in the Sessions full list: 35,886,983 bytes raw (35.9 MB, 11.9 MB
gzipped) versus the live 32 MB. After the fix, the same tabs load in 0.02-0.65 s
of first content on a simulated slow-4g link instead of 15-40 s: the fix removes
the list payloads from the first-content path rather than the live scale being
special.

### Budget status

- 256 KB (262,144 bytes) uncompressed per first-page endpoint: Sessions 16,352 B,
  Automata 57,410 B, Automations 9,233 B, Alerts 21,951 B - all pass
  (`TestPerfBudgetFirstPage`).
- `PERF_TAB_BUDGET_MS` 3000 slow-4g / 1500 fast per tab: all five tabs pass the
  measurement above.
- ETag conditional repeat returns 304 with an empty body on all four list endpoints
  (`TestPerfBudgetConditionalRepeat`); `?full=1` remains complete
  (`TestPerfBudgetFullFormStillComplete`).
- Small fixes already in place with code and tests: summary view + `limit` on the
  two heavy lists, `?full=1` escape hatch on the detail/list paths, ETag on all
  list endpoints (alerts gained one for the guard). Nothing new was needed to
  hold the budgets.
- Follow-ups (no code in this pass): the Observer tab depends on `/api/stats` and
  `/api/schedules`, which 503 until the stats store is enabled (daemon behaviour
  unchanged); a totals cap on the full `/api/alerts` list (218.5 KB baseline) is a
  Phase 4 candidate.

## Budget guard

- `internal/server/perf_budget_test.go`, `TestPerfBudget*`:
  - `TestPerfBudgetFirstPage` fails when a first-page body exceeds 262,144 bytes
    uncompressed.
  - `TestPerfBudgetConditionalRepeat` fails when a repeat with `If-None-Match` does not
    return 304 with an empty body (sessions, Automata, Automations, alerts all carry an
    ETag; alerts gained one for this guard).
  - `TestPerfBudgetFullFormStillComplete` fails when `?full=1` / the plain sessions list
    stops returning complete objects.
- `scripts/perf-budget.sh` runs the Go test and, when `TEST_HTTP` and `TEST_TOKEN` are
  set and `node` is on the path, `scripts/perf-measure-tabs.mjs --slow-4g-only`, failing
  when a tab's time to first content exceeds `PERF_TAB_BUDGET_MS` (default 3000). It
  exits non-zero on any failure.

## Phases

| Phase | Name | Status |
|---|---|---|
| 1 | Seed, endpoint measurement, tab timing script, baseline | Done (shipped in v9.5.109) |
| 2 | Slim list payloads: sessions and Automata lists carry summary fields only, full record by id | Done (shipped in v9.5.109) |
| 3 | Response compression and conditional requests for bodies over a size threshold | Done (shipped in v9.5.109) |
| 4 | Load only what the visible tab needs; paginate or window the long lists in the UI | Done (shipped in v9.5.109) |
| 5 | Re-measure against the baseline and set regression budgets | Done (budget guard and after-fix numbers above) |

### Phase 1: Seed and measurement (Done)

- [x] Deterministic seed helper `SeedPerfServer`, independent of live data.
- [x] `TestPerfSeed*` prove the seeded counts and heavy-Automaton sizes.
- [x] `TestEndpointMeasure*` record raw bytes, gzipped bytes and server time per
  endpoint, log a table, and write JSON to `PERF_REPORT_OUT`.
- [x] `scripts/perf-measure-tabs.mjs` measures Sessions, Automata, Automation,
  Observer and Settings, with an optional slow-4G profile.
- [x] Baseline recorded above.
- [x] Live run of `scripts/perf-measure-tabs.mjs` against a daemon on seeded data, with
  its numbers added to the Baseline. Recorded under
  "Result against the live-style data set" using a seeded HTTP daemon
  (`TestSeededLocalServerServe`), 2026-10-23.
- [x] Phase Completion Checklist items for docs and version: N/A, test and
  tooling only, no user-visible change, no version bump of its own (the docs for
  the full feature landed in this PRD's close-out).

### Phase 2: Slim list payloads (Done)

- Return summary fields in the list responses and keep the full record on the by-id
  route. `/api/sessions` already has a `?view=summary` mode; the Automata list has none.
- Move the UI to the summary form and fetch the full record when a card is opened.
- Done when the Phase 1 measurement shows both lists under their budget in Phase 5.
- Checklist: [ ] feature, [ ] tests, [ ] live test on a real daemon, [ ] full test
  suites, [ ] docs checklist, [ ] conditional docs, [ ] Mobile-Parity Rule (the list
  contract changes), [ ] Localization Rule (N/A unless new UI text), [ ] version bump.

### Phase 3: Compression and conditional requests (Done)

- Gzip responses over a size threshold; skip tiny bodies.
- Add validators so an unchanged list answers 304.
- Checklist: same items as Phase 2.

### Phase 4: Load by visible tab (Done)

- Stop fetching data for tabs the user has not opened; page or window the long lists.
- Checklist: same items as Phase 2.

### Phase 5: Re-measure and budgets (Done)

- Re-run both tools, fill a before/after table here, and add a test that fails when a
  load-path endpoint exceeds its byte budget on the seeded data.
- Checklist: same items as Phase 2.

## Parity surface

REST contract changes in Phase 2 affect mobile clients that parse `/api/sessions` and
`/api/autonomous/prds`. A `datawatch-app` issue is required then.

## Live-update connection review

Scope: the WebSocket client in `internal/server/web/app.js` (backoff, watchdog, wake,
resync), the decompose `EventSource`, `sw.js` and `internal/server/ws.go` /
`api.go`. Measured first with `internal/server/web/live-update.test.js` (fake clock,
fake socket, fake visibility/online events; 14 tests, run with `node --test`).

Already sound, kept (tests assert it): full-jitter exponential backoff capped at 30 s;
a flapping socket does not reset the backoff, a socket stable for 15 s does; a quiet
but healthy socket is never closed (client pings every 15 s, the server answers each
ping with a `pong` frame, so the 35 s stale limit only trips on a dead path); no
reconnect while the page is hidden; one socket for a simultaneous visible + online.
The service worker never intercepts `/api/*` or `/ws`. The server sends a protocol
ping every 30 s with a 60 s read deadline and a pong handler. The decompose
`EventSource` closes while hidden, retries with capped backoff and then polls.
Low-power behaviour (no hidden-tab reconnects) is unchanged.

Found by the measurement and fixed:

| Finding | Fix |
|---|---|
| A superseded socket's late `close` (watchdog close, server switch, wake while CLOSING) nulled `state.ws`, set `connected=false`, stopped the new socket's watchdog and scheduled another reconnect, so the next wake opened a duplicate socket. | `open`/`message`/`close` handlers ignore events from any socket that is not `state.ws`. |
| Returning from background reset the activity clock, hiding a socket killed while asleep for another 35-50 s. | `_wsWake` pings and marks the socket unproven; no frame within `WS_WAKE_GRACE_MS` (5 s) closes it and the normal reconnect runs. |
| After a long outage the backoff counter stayed high, so a reconnect that failed right after the network returned waited up to 30 s. | Wake (online / visible) with no open socket restarts the counter at 0 for the immediate attempt. |
| Federated-server resync fetched the full unpaged `/api/sessions`. | Both resync paths use the summary form (`/api/sessions?view=summary&limit=50`). |
| List loaders (Automata, Automation, Alerts) kept polling every 30 s on a hidden phone tab and did not refresh on return or after a socket reconnect. | Hidden pages skip the poll; `refreshIfOverdue()` runs on visible and after a reconnect. Polling at `pollMs` continues while the socket is down. |

Not changed: server timings in `ws.go`/`api.go` and `sw.js` needed no change.

## Follow-up

Left open after done (2026-10-23), to be picked up when the Automata tab UI work
lands:

- Re-measure the production daemon after release (this pass measured against the
  seeded live-style daemon plus the recorded baseline; the live numbers above are
  the budget source, not the production re-run).
- The slow-network group (TS-802 to TS-810) is verified by the seeded daemon and by
  release smoke section 79 on the smoke daemon; an operator device pass (phone on a
  real mobile link) is optional follow-up.
- `datawatch-app` parity issue for the REST contract changes (summary form, paging,
  ETag) once Phase 2 code ships, per the Parity surface note above.
