# Web UI load performance

**Date:** 2026-10-23
**Version:** v9.5.105
**Status:** In progress. Phase 1 (measurement) is done; Phases 2 to 5 are planned.
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

## Phases

| Phase | Name | Status |
|---|---|---|
| 1 | Seed, endpoint measurement, tab timing script, baseline | Done (not yet shipped in a tagged version) |
| 2 | Slim list payloads: sessions and Automata lists carry summary fields only, full record by id | Planned |
| 3 | Response compression and conditional requests for bodies over a size threshold | Planned |
| 4 | Load only what the visible tab needs; paginate or window the long lists in the UI | Planned |
| 5 | Re-measure against the baseline and set regression budgets | Planned |

### Phase 1: Seed and measurement (Done)

- [x] Deterministic seed helper `SeedPerfServer`, independent of live data.
- [x] `TestPerfSeed*` prove the seeded counts and heavy-Automaton sizes.
- [x] `TestEndpointMeasure*` record raw bytes, gzipped bytes and server time per
  endpoint, log a table, and write JSON to `PERF_REPORT_OUT`.
- [x] `scripts/perf-measure-tabs.mjs` measures Sessions, Automata, Automation,
  Observer and Settings, with an optional slow-4G profile.
- [x] Baseline recorded above.
- [ ] Live run of `scripts/perf-measure-tabs.mjs` against a daemon on seeded data, with
  its numbers added to the Baseline. N/A until a seeded daemon is available.
- [ ] Phase Completion Checklist items for docs and version: N/A, test and tooling
  only, no user-visible change, no version bump of its own.

### Phase 2: Slim list payloads (Planned)

- Return summary fields in the list responses and keep the full record on the by-id
  route. `/api/sessions` already has a `?view=summary` mode; the Automata list has none.
- Move the UI to the summary form and fetch the full record when a card is opened.
- Done when the Phase 1 measurement shows both lists under their budget in Phase 5.
- Checklist: [ ] feature, [ ] tests, [ ] live test on a real daemon, [ ] full test
  suites, [ ] docs checklist, [ ] conditional docs, [ ] Mobile-Parity Rule (the list
  contract changes), [ ] Localization Rule (N/A unless new UI text), [ ] version bump.

### Phase 3: Compression and conditional requests (Planned)

- Gzip responses over a size threshold; skip tiny bodies.
- Add validators so an unchanged list answers 304.
- Checklist: same items as Phase 2.

### Phase 4: Load by visible tab (Planned)

- Stop fetching data for tabs the user has not opened; page or window the long lists.
- Checklist: same items as Phase 2.

### Phase 5: Re-measure and budgets (Planned)

- Re-run both tools, fill a before/after table here, and add a test that fails when a
  load-path endpoint exceeds its byte budget on the seeded data.
- Checklist: same items as Phase 2.

## Parity surface

REST contract changes in Phase 2 affect mobile clients that parse `/api/sessions` and
`/api/autonomous/prds`. A `datawatch-app` issue is required then.
