# Lane fill eligibility: final gate report

**Date:** 2026-11-01
**Commit under test:** `459322a52` (branch `automaton/a5db0001`, worktree clean at the start of the gate)
**Toolchain:** go1.27.2 linux/amd64, node v24.15.0

Feature: lane fill now says why it refuses a queued Automaton (seven stable codes in
`eligibility_reason`), takes its lane counts from the in-flight caps of the enabled LLM entries
(`lane_counts`), has a mode for each lane kind (`gpu_mode`, `claude_mode`, shown as `modes`), and
reads pool evidence from a registered host with an untrusted certificate with a message that names the
node and the fix. The default mode stays `shadow` for both lane kinds: nothing starts work on its own
unless the operator sets a mode to `on`.

This is the final gate. It changed no code. Its repository output is this report, plus the live check
report that the script rewrites on every run. Every command ran under `env -u TMUX -u TMUX_TMPDIR`, from
the repository root, after the last code commit, one at a time.

**Gate rule:** the result is `Result: PASS` only if every recorded command exited 0 and every live check
passed. Nothing below exited non-zero.

**Result: PASS**

| Step | Command | Exit | Outcome |
|---|---|---|---|
| 1 | go build | 0 | daemon binary built |
| 1 | go vet (six packages) | 0 | no findings |
| 1 | gofmt -l (38 changed Go files) | 0 | nothing listed |
| 2 | go test | 0 | 83 packages passed, 0 failed, 12 without test files |
| 2 | node --test | 0 | 1849 tests passed, 0 failed |
| 3 | Live check | 0 | 8 of 8 checks passed |
| 4 | check-no-internal-refs.sh, check-no-unscoped-tmux.sh | 0 | both clean |

## 1. go build, go vet, gofmt

```
env -u TMUX -u TMUX_TMPDIR go build -o datawatch ./cmd/datawatch/
```

Exit code 0, no output. The binary landed at `datawatch` in the repository root, which `.gitignore`
covers (`/datawatch`); it was removed after the gate so the working tree holds only the report changes.

```
env -u TMUX -u TMUX_TMPDIR go vet ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/ ./internal/config/
```

Exit code 0, no output.

`gofmt -l` ran only on the 38 Go files the branch changed against its merge base with `main` (the list
came from `git diff --name-only <merge base> HEAD`, filtered to `.go`). Exit code 0, nothing listed. No
whole-package format was run.

## 2. go test and node --test

```
env -u TMUX -u TMUX_TMPDIR go test ./... -count=1
```

The multiplexer variables were unset for the run. Exit code 0 after 183 seconds. The tally over the
output: **83 packages `ok`, 0 packages failed, 12 packages with no test files** (95 packages in all). The
packages this feature touches:

```
ok  	github.com/dmz006/datawatch/cmd/datawatch	112.092s
ok  	github.com/dmz006/datawatch/internal/autonomous	61.394s
ok  	github.com/dmz006/datawatch/internal/config	1.905s
ok  	github.com/dmz006/datawatch/internal/mcp	1.998s
ok  	github.com/dmz006/datawatch/internal/router	125.069s
ok  	github.com/dmz006/datawatch/internal/server	66.782s
ok  	github.com/dmz006/datawatch/tests/ci	67.936s
```

Nothing failed, so there is no unrelated or pre-existing failure to name and no fix was needed. (An
intermittent timing failure in the `tests/ci` signal test showed up in an earlier gate of a different
feature; it passed in this run.)

```
env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/*.test.js
```

Exit code 0. Last lines:

```
ℹ tests 1849
ℹ suites 0
ℹ pass 1849
ℹ fail 0
ℹ cancelled 0
ℹ skipped 0
ℹ todo 0
```

The two scripts that the verification script also runs, `scripts/check-no-internal-refs.sh` and
`scripts/check-no-unscoped-tmux.sh`, both exited 0 ("no internal refs in user-facing surfaces", "no
unscoped tmux kills").

## 3. Live check

```
env -u TMUX -u TMUX_TMPDIR bash scripts/lane-fill-eligibility-live-check.sh
```

Exit code 0, run against the final code (source revision `459322a52`), started 15:33:11 local time,
189 seconds. Its report, rewritten by this run, is
[`2026-10-31-lane-fill-eligibility-live-check.md`](2026-10-31-lane-fill-eligibility-live-check.md).
It drives a sandbox daemon (never the operator's daemon, never the operator's multiplexer) against a
throwaway project whose default branch is `trunk`. **Overall: PASS, every check passed.**

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Defaults on a fresh config, the round trip and a bad mode | PASS | 5 of 5 |
| 2 | Refusals in shadow mode, one for each rule | PASS | 12 of 12 |
| 3 | The same refusals on every read surface | PASS | 3 of 3 |
| 4 | Lane counts follow the LLM in-flight caps | PASS | 3 of 3 |
| 5 | The per-lane-kind switch: gpu_mode on starts GPU work only | PASS | 7 of 7 |
| 6 | Pool evidence with an untrusted certificate | PASS | 4 of 4 |
| 7 | Shadow comparison | PASS | 4 of 4 |
| 8 | Cleanup | PASS | 3 of 3 |

What it proved against a real daemon: each of the seven rules (stale, held, sequenced after an
unfinished Automaton, proposal not confirmed, proposal missing, base branch already carries the work,
worktree not registered) produces a `blocked` decision with the right `eligibility_reason`; the one fully
eligible control is the only `start_prd` decision; the same refusals read the same over REST, MCP, the CLI
(text and `--json`) and the chat verb; the Claude lane count follows an LLM cap of 1 and falls back to the
configured 3 when `lane_counts_from_caps` is off; `gpu_mode` on starts the control on a GPU lane while a
Claude-lane decision stays unexecuted and no refused Automaton starts; and a node with an untrusted
certificate gives an evidence error that names the node and the fix without showing the address.

## 4. Parity

**datawatch-app issue:** `dmz006/datawatch-app#301`. An open issue for the lane fill status view already
existed, so no new issue was filed; the sync text was posted as a comment on it
(`https://github.com/dmz006/datawatch-app/issues/301#issuecomment-6562807953`). The record is
`docs/plans/parity-issues/2026-10-31-lane-fill-eligibility.md`, "Filing record".

| Surface | What it carries | Test that covers it |
|---|---|---|
| Config keys (seven under `autonomous.lane_fill`) | defaults, validation, explicit `false` and `0` survive a save, an empty mode inherits | `TestLaneFillNewKeyDefaults`, `TestLaneFillEffectiveModeForKind`, `TestLaneFillKindModeValidation`, `TestLaneFillStaleAfterHoursZeroSurvivesRoundTrips` (`internal/config/config_lane_fill_test.go`); `TestLaneFillNewKeysPutThenGetRoundTrip`, `TestLaneFillBadGPUModeIs400`, `TestLaneFillNewKeysNamedInConfigureUsage` (`internal/server/lane_fill_kind_config_test.go`) |
| REST read, `GET /api/autonomous/lane-fill` | `eligibility_reason`, `lane_counts`, `modes`; OpenAPI names | `TestLaneFillReasonsEveryCodeSurvivesTheRoute`, `TestLaneFillReasonsCarryLaneCountsAndModes`, `TestLaneFillReasonsOpenAPINames` (`internal/server/lane_fill_reasons_rest_test.go`) |
| MCP tool `autonomous_lane_fill_status` | the refusal reason in the result, the tool description | `TestLaneFillStatusShowsTheRefusalReason`, `TestLaneFillStatusDescriptionMentionsReasonsAndLaneCounts` (`internal/mcp/lane_fill_reasons_test.go`) |
| CLI `datawatch autonomous lane-fill` | reason code before the text, lane counts line, modes line, raw JSON | `TestLaneFillReasonsCLIShowsReasonLaneCountsAndModes`, `TestLaneFillReasonsCLIJSONReturnsTheRawBody` (`cmd/datawatch/lane_fill_reasons_cli_test.go`); text rendering in `TestLaneFillTextReasonCodeComesBeforeTheReasonText`, `TestLaneFillTextLaneCountsLine` (`internal/autonomous/lane_fill_text_reasons_test.go`) |
| Comm verb `autonomous lane-fill` | the same reply with reason, lane counts and modes | `TestLaneFillComm_ReplyShowsReasonLaneCountsAndModes` (`internal/router/lane_fill_reasons_comm_test.go`) |
| PWA view | refusal chip, unknown code shows no chip, escaped hostile text, lane counts line, per-kind modes line, settings rows | `internal/server/web/app-lane-fill.test.js`: "a decision with no eligibility reason shows no chip", "a decision with an unknown eligibility code shows no chip", "a hostile refusal text and a hostile eligibility code are escaped", "the lane counts line shows the effective count, the configured count and the source for both lane types", "the per-kind modes line shows when a kind differs from the base mode", "the autonomous settings card declares the eligibility, lane count and per-kind mode fields with their defaults" |
| Locale keys in five bundles (en, de, es, fr, ja) | every `lane_fill_` and `settings_lane_fill_` key in all five, real translations | "every lane_fill_ and settings_lane_fill_ key exists in all five bundles with the same set" and "every lane key is a real translation in de, es, fr and ja, not an English copy" (`app-lane-fill.test.js`); `TestLocales_AllAppJSKeysExistInEnglishBundle` (`internal/server/v8_73_25_locale_key_coverage_test.go`); live check 3 reads the served bundles |
| Supervisor watch (not a user surface, but it reads the status) | a refused Automaton stops counting as eligible work | `TestLaneFillEligibQueuedEligibleExcludesRefused` (`internal/autonomous/lane_fill_reasons_test.go`), `TestSupervisorWatchWorkIdleLaneIgnoresStaleQueuedWork`; live check 7 |

No Android or iPhone code lives in this repository; both are tracked by the issue above.

## 5. Shadow

A default configuration executes nothing and reads `shadow` for both lane kinds, so the first release
changes no behaviour.

- **Unit evidence.** `TestLaneFillKindModeDefaultConfigExecutesNothing`
  (`internal/autonomous/lane_fill_kind_mode_test.go`) builds the coordinator from `DefaultConfig()` with an
  approved GPU Automaton, an approved Claude Automaton and a draft, polls, and asserts that `mode`,
  `modes.gpu` and `modes.claude` all read `shadow`, that decisions were recorded, that every decision has
  `executed: false` and mode `shadow`, and that the fake starter received **zero calls**.
  `TestLaneFillShadowRecordsAndStartsNothing` (`internal/autonomous/lane_fill_coordinator_test.go`) pins the
  same for the base mode, `TestLaneFillKindModeDefaultsOfNewKeys` (`lane_fill_config_kind_test.go`) pins the
  defaults of the new keys, and `TestLaneFillSmokeRestartShadowThenOn` covers a restart in shadow and then on.
- **Live evidence, check 1.** On a fresh config the status reads
  `{"mode":"shadow","modes":{"gpu":"shadow","claude":"shadow"}}`; the generated config sets only the poll
  interval, no mode, and both `gpu_mode` and `claude_mode` read empty (inherit).
- **Live evidence, check 2.** With every kind of refusal and one eligible control queued, "nothing was
  started in shadow mode": executed decisions 0, no Automaton running, the worker never ran. Every decision
  in the status reads `executed: false`.
- **The switch only moves when the operator moves it.** Check 5 sets `gpu_mode` to `on` explicitly: the
  control then starts on a GPU lane, the Claude-lane decision stays `executed: false`, none of the refused
  Automata start, and setting `gpu_mode` back to `shadow` stops further starts at the next poll
  (`TestLaneFillSwitchBackToShadowStopsExecution`, `TestLaneFillSwitchRefusedAutomatonNeverExecutesWithGPUOn`).

## 6. Phase Completion Checklist (AGENT.md)

- [x] Functionality implemented per the description: done (seven rules, lane counts, per-lane-kind mode, pool evidence fix, all read surfaces).
- [x] Unit tests added and passing: done (listed in the Parity table; full suite green).
- [x] Live or smoke test against a real running daemon: done (the live check, 8 of 8 checks, sandbox daemon).
- [x] `go test ./...` and `node --test internal/server/web/*.test.js` both green: done (section 2 above).
- [x] General documentation checklist, eight items:
  - [x] `CHANGELOG.md`: done (one Added entry, no version number, no tracker ids).
  - [x] `docs/config-reference.yaml`: done (the seven new keys with defaults).
  - [x] `docs/operations.md`: done (the switch-over and the refusal reasons).
  - [x] `README.md`: N/A, the lane fill how-to is already linked from the README; no new top-level concept.
  - [x] Doc index in `README.md` and `docs/README.md`: N/A, `docs/howto/automation-lane-fill.md` is already indexed in both and no new document was added.
  - [x] `docs/testing-tracker.md`: done (a section for this feature).
  - [x] No leaked tracker ids: done (`scripts/check-no-internal-refs.sh` clean).
  - [x] The plan's own Parity surface section still accurate: done (`docs/parity-status.md`, `docs/api-mcp-mapping.md` and the supervisor duties proposal's Step 10 status were updated).
- [x] Conditional docs: `docs/datawatch-definitions.md` done (glossary entries); `docs/howto/automation-lane-fill.md` done (reasons, lane counts, the day-long comparison, the one-kind-at-a-time switch); `docs/flow/*.md` N/A (no new data flow, lane fill has no flow diagram and this adds no new component).
- [x] Mobile-Parity Rule: done (datawatch-app issue #301 commented, see section 4).
- [x] Localization Rule: done (every new `t('key')` has an entry in all five bundles, covered by the tests in section 4).
- [x] Version bumped in `cmd/datawatch/main.go` and `internal/server/api.go`: **deferred to merge time by instruction** (the version is set at merge; none was written anywhere in this work).
- [x] The plan's phase-status line updated: done (the supervisor duties proposal reads "Built, ships in shadow mode", with the version set at merge).

**Open items:** none of the checklist items is open. Two things are honestly not done and are not
checklist items: the one-day comparison on the operator's live daemon has not been run (the proposal says
so), and the version bump waits for merge time.

## 7. Shadow comparison

Before turning `gpu_mode` to `on`, the operator leaves both kinds on shadow for about a day while the
supervisor still fills lanes by hand, reads the status at the end (`datawatch autonomous lane-fill
--limit 1000 --json`), and compares every `blocked` decision and its `eligibility_reason` with what the
supervisor's own checks (the daemon-side supervisor watch and `scripts/supervisor-watch.py`) said in the
same window: a refusal the supervisor would also have made is good, a refusal it would not have made, or a
start the planner proposed that the supervisor would have left alone, is a difference to fix or accept in
writing. The `agreed_days` figure in `GET /api/autonomous/supervisor-watch` counts the days the watch and
the script agreed. Only when every difference is explained does the operator set `gpu_mode` to `on` (Claude
lanes later, after a look at the usage pace); the steps are in `docs/howto/automation-lane-fill.md`. On the
sandbox state, the live check's comparison (check 7) showed that lane fill refuses seven kinds of Automaton
that the supervisor watch script's rule still counts as queued and runnable (it has no stale, proposal,
base-branch or worktree rule, and only a 72 hour window and a fixed ignore list); the daemon-side watch
raises `idle_lane_eligible_work` only for the idle lanes of a kind that lane fill counts eligible work for
(the GPU lanes, never a Claude lane), and once lane fill refuses the control as stale the finding clears.
The script itself is written for the operator's own daemon and was applied to the sandbox state as a
module, not run as a program; every difference is listed in the live check report.
