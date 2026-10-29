# Decision gate — final gate report

Date: 2026-10-29 · Branch: `automaton/f1f20053` · Tree: commit `3927cc7aa` plus two uncommitted edits made by this task
(the README sentence described under item 4 below, and this report).

This is the Phase Completion Checklist from `AGENT.md`, run for the operator decision line and decision gate as a
whole. Every command ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux server command was run, nothing was killed by
name, no version was bumped. The feature itself is described in
[the live-check report](2026-10-29-decision-gate-live-check.md), the
[operator how-to](../howto/automaton-operator-decisions.md) and the
`## Decision gate` section of [`docs/operations.md`](../operations.md).

## Result in one paragraph

Build, vet, gofmt, the Go suite, the leak audit, the how-to audit and the leftover-daemon check pass. Two things
did not pass first time and are recorded exactly below: the embedded-docs sync check failed on drift (fixed with the
repo's own `make sync-docs`, now passing), and one node test failed intermittently (3 of 13 full runs; the last
six runs were green; the likely cause (inferred from reading it, not reproduced) is a fixed 25 ms wait in a test file this branch does not touch, not a gate
fault). The live check passed all four of its steps, with the limits stated in the live-check section.

## Checks

| # | Command | Result |
|---|---|---|
| 1 | `go build ./...` | exit 0, no output |
| 2 | `go vet ./cmd/datawatch ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server` | exit 0, no output |
| 3 | `gofmt -l` on the 37 changed `.go` files (`git diff --name-only main...HEAD`, filtered to `.go`) | exit 0, no files listed |
| 4 | `go test ./...` | exit 0: 82 packages `ok`, 12 `[no test files]`, no `FAIL` line. 80 of the 82 were `(cached)`; see 4b |
| 4b | `go test -count=1 ./cmd/datawatch ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server` (uncached, the seven packages the feature changed) | exit 0: `cmd/datawatch` 111.776s, `internal/autonomous` 28.888s, `internal/config` 1.841s, `internal/federation` 0.050s, `internal/mcp` 1.925s, `internal/router` 93.707s, `internal/server` 67.778s, all `ok` |
| 5 | `node --test internal/server/web/*.test.js` | final run: 1495 tests, 1495 pass, 0 fail, exit 0. Earlier failures: see "Failure 1" |
| 6 | `bash scripts/check-no-internal-refs.sh` | exit 0: `PASS: no internal refs in user-facing surfaces.` Run twice, the second time after the README edit |
| 7 | `bash scripts/check-docs-sync.sh` | first run exit 1 (drift); after `make sync-docs`, exit 0. See "Failure 2" |
| 8 | `bash scripts/check-howto-coverage.sh` | exit 0: `PASS: every howto under docs/howto/ has exec_steps or is LLM-only.` |
| 9 | `bash scripts/check-no-leftover-test-daemons.sh snapshot <file>` before the Go run, then `check <file>` after all runs | snapshot listed 12 live test daemons already running (not started by this task); check exit 0: `OK: no leftover test daemons` |
| 10 | Locale guard tests: `go test -count=1 -v -run Locale ./internal/server` | exit 0, 15 tests `PASS` (list below) |

Locale guard tests that ran and passed: `TestLocales_AllPresent`, `TestLocales_ParityWithEnglish`,
`TestLocales_CommonNavKeysPresent`, `TestLocales_AllAppJSKeysExistInEnglishBundle`,
`TestLocales_AutomatonNeverUntranslatedOrMistranslated`, `TestLocales_AutomationKeyExemptionIsNarrow`,
`TestLocales_PRDNeverUserFacing`, `TestQuestionsSettings_LocaleKeyInAllBundles`, `TestQuestionNotifySettingsLocale`,
`TestAllowedProjectRoots_SettingsCardAndLocales`, `TestJudgeConfigLocaleParity`,
`TestListenerAuth_LocaleKeys_AllBundles`, `TestMCPPublicURLLocaleParity`, `TestRateCeilingLocaleParity`,
`TestProxySandboxMux_ServesLocalesUnauthenticated`.

Audit Logging Rule tests: **N/A.** The branch adds no audit event. `git diff main...HEAD -- '*.go'` contains no added
line that mentions audit. The new `operator_decision`, `operator_answer` and `decision_gate_shadow` entries go on the
Automaton's own decision log (the `decisions` array on the Automaton), which is not the operator audit trail, so the
JSON-lines and CEF round-trip tests the rule asks for do not apply.

## Failures and fixes, recorded exactly

### Failure 1 — one node test fails intermittently

First full run, 19:07:19:

```
✖ settings modal: explicit list renders non-inherit with checked rows only (57.526746ms)
  AssertionError [ERR_ASSERTION]: modal element should be appended
      at TestContext.<anonymous> (.../internal/server/web/app-notify-events.test.js:278:10)
    actual: false
    expected: true
ℹ tests 1495
ℹ pass 1494
ℹ fail 1
exit=1
```

Thirteen full runs in all on this branch: 3 failed, 10 passed. Where the failing test's name was captured (the first run
and one run of an eight-run loop) it was this same test; the third failing run's name was not captured, only
`ℹ fail 1`. Run alone, `app-notify-events.test.js` passed 3 of 3 (17 tests). The test opens a settings modal and then
waits a fixed `await sleep(25)` for an asynchronous mount before asserting the modal exists
(`internal/server/web/app-notify-events.test.js`, around lines 253-278), so it can miss under load when node runs
test files in parallel. `git diff main...HEAD --stat` shows that file is not touched by this branch. I could not
compare against `main`: a copy of `main`'s web folder extracted outside the tree failed two unrelated tests because it
lacks files they read from the repository root, so that comparison was discarded and proves nothing either way.
Not fixed here (outside this feature). The last six runs, including the closing one above, were green.

### Failure 2 — embedded docs drift

`bash scripts/check-docs-sync.sh`, first run:

```
ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage.
>f.st...... testing-tracker.md
>f+++++++++ plans/2026-10-29-decision-gate-live-check.md
exit=1
```

Cause: the feature's tracker section and live-check report had not been copied into `internal/server/web/docs/`.
Fix: `make sync-docs` (exit 0). That folder is git-ignored (`.gitignore` line 42), so the sync changed no tracked
file. After it, the check printed `docs/ and internal/server/web/docs in sync.` and exited 0. This report was added
afterwards, so the sync and the check were run once more with it in place; the result is in the last section.

### Run notes (not feature failures)

- My first `go test ./...` was lost when its background shell was reaped before it finished; its output has no
  final line, so it is not counted. The result in row 4 is from a second, complete run, then row 4b repeated the
  seven changed packages uncached.
- A first batch of script invocations wrote to an unset output path and did not run (`Permission denied`); they
  were re-run properly and only the re-runs are reported. `check-no-leftover-test-daemons.sh` run with no mode
  prints its usage and exits 2; it needs `snapshot` then `check`, which is what row 9 is.
- Scratch outputs for this task were kept in `.datawatch/final-gate/` (git-ignored) and removed at the end. An
  earlier set of copies of the first outputs was written to the session scratch folder before I confined it.

## Phase Completion Checklist

- [x] **Functionality implemented per the phase description.** The hold, the release on answer, the shadow mode, the
  decision line on REST, MCP, CLI, comm and PWA, and the two config keys on every config surface; commits
  `178400988` to `3927cc7aa`. Live-checked, see below.
- [x] **Unit tests added and passing.** `internal/autonomous/decision_gate_test.go`,
  `decision_gate_executor_test.go`, `decision_line_test.go`, `decision_log_test.go`;
  `internal/server/decisions_rest_test.go`; `internal/mcp/decision_tools_test.go`;
  `internal/router/decisions_comm_test.go`; `cmd/datawatch/cli_decisions_test.go`;
  `internal/config/decision_gate_config_test.go`; one `decision_gate_config_surface_test.go` each in
  `cmd/datawatch`, `internal/mcp`, `internal/router` and `internal/server`;
  `internal/server/web/app-decisions.test.js`. All pass uncached (row 4b) and in the full run.
- [x] **Live test against a real running daemon.** Done on 2026-10-29 against a sandbox `datawatch v9.34.3`; see the
  live-check section.
- [x] **`go test ./...` and `node --test internal/server/web/*.test.js` green.** Go: green. Node: green on the last
  six runs but intermittent across thirteen (Failure 1); checked with that caveat stated, not hidden.
- [x] **General documentation checklist (8 items):**
  1. `CHANGELOG.md` — [x] one `[Unreleased]` entry added by this branch.
  2. `docs/config-reference.yaml` — [x] 28 lines for the two keys.
  3. `docs/operations.md` — [x] new `## Decision gate` section (40 lines: holds, shadow mode, alert, config, where to see it).
  4. `README.md` — [x] the branch had not touched it; **found missing by this gate and added**: one sentence in the
     operator-question paragraph describing the gate and decision read, linking the how-to and naming the parity issue.
  5. Documentation index — [x] `docs/README.md` (how-to row updated) and `docs/howto/README.md` (new row); the new
     how-to is also linked from `README.md` by the sentence in item 4. The live-check and this report are plan
     documents under `docs/plans/`, which are not individually indexed.
  6. `docs/testing-tracker.md` — [x] section "Operator decision line, decision gate and config keys — unreleased".
  7. No leaked tracker IDs — [x] `scripts/check-no-internal-refs.sh` passes (README included, after the edit).
  8. Parity surface section — [x] the `## Parity surface` section of this report.
- [x] **Conditional docs.** `docs/datawatch-definitions.md` — [x] two new entries (*Operator decision*, *Decision
  gate*). `docs/howto/automaton-operator-decisions.md` — [x] new; `check-howto-coverage.sh` passes.
  `docs/flow/*.md` — **N/A**: the gate is one added check on the existing task-start path and one release on the
  existing answer path, with no new component; the sequence is written up in `docs/operations.md` and the how-to.
- [x] **Mobile-Parity Rule.** `datawatch-app` issue **302**, filed:
  https://github.com/dmz006/datawatch-app/issues/302 ("[sync] Automaton operator decision line and design-point
  gate"). The exact filed text is kept in `docs/plans/parity-issues/operator-decision-gate.md`; it covers Android and
  iPhone/iOS together.
- [x] **Localization Rule.** 11 new locale keys (listed in issue 302) are in all five bundles
  (`de`, `en`, `es`, `fr`, `ja`); `TestLocales_AllAppJSKeysExistInEnglishBundle` and the other locale guards pass.
  The non-English values are placeholders, as the issue says.
- [x] **Version bump** — **not bumped, by this task's instruction.** `git diff main...HEAD` shows no `Version` line
  changed in `cmd/datawatch/main.go` or `internal/server/api.go`. This work is not a tagged unit yet; the release
  step belongs to whoever merges it.
- [x] **Plan-doc phase-status line** — **N/A**: no plan document for this feature exists under `docs/plans/` (a
  search for the feature found only the live-check report, the supervisor rules and this report), so there is no
  status line to update.

## Parity surface

Full set: `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone`. Nothing is excluded.

| Surface | Covered by | Where it is tested |
|---|---|---|
| REST | `GET /api/autonomous/decisions` (`prd_id`, `state`, `limit`; federation cap `autonomous:read`), the `waiting_decision` task status, `PUT /api/config` for both keys; `internal/server/decisions.go`, `docs/api/autonomous.md`, `docs/api/openapi.yaml`, `internal/server/web/openapi.yaml` | `internal/server/decisions_rest_test.go`, `internal/server/decision_gate_config_surface_test.go`; live check (a)-(d) |
| MCP | tool `autonomous_decisions` (read-only, capability mapped in `internal/federation/mcp_tool_caps.go`), `config_set`; `docs/mcp.md`, `docs/api-mcp-mapping.md` | `internal/mcp/decision_tools_test.go`, `internal/mcp/decision_gate_config_surface_test.go`; live check (b) over stdio |
| CLI | `datawatch autonomous decisions [id] [--state] [--limit] [--json]`, `datawatch config set/get`; `docs/commands.md` | `cmd/datawatch/cli_decisions_test.go`, `cmd/datawatch/decision_gate_config_surface_test.go`; live check (b), (c) |
| comm channel | verb `autonomous decisions [id]`, `configure` for both keys | `internal/router/decisions_comm_test.go`, `internal/router/decision_gate_config_surface_test.go`; live check (b), (c) on a daemon with no `server.token` only (Finding 1 below) |
| YAML/config | `autonomous.questions.decision_gate`, `autonomous.questions.decision_gate_mode`; `internal/config/config.go`, `docs/config-reference.yaml` | `internal/config/decision_gate_config_test.go`; live check (a), (b) set the mode through the API |
| PWA | Decisions card on the Automaton detail, `waiting_decision` label and glyph, the Automata settings card (toggle and select), 11 locale keys in 5 bundles | `internal/server/web/app-decisions.test.js`, locale guards. **Not driven in a browser** (live-check "Not covered") |
| Android | datawatch-app issue 302, Android acceptance list | issue filed; implementation is the app's work |
| iPhone | datawatch-app issue 302, iPhone/iOS acceptance list | issue filed; implementation is the app's work |

`docs/parity-status.md` carries two new rows for the decision line and the gate.

## Live-check result

From [the live-check report](2026-10-29-decision-gate-live-check.md): sandbox `datawatch v9.34.3`, built from this
worktree, production daemon untouched, scratch folder deleted afterwards.

- (a) Shadow mode, gate on — **PASS**. The task was not held; one `decision_gate_shadow` log entry; decision line `open`.
- (b) Mode `on` — **PASS**. Task `waiting_decision` with its reason; the same reason read on REST, MCP, CLI and comm.
- (c) Answer with a quote — **PASS**. The task left the hold and reached its start attempt 51 ms after the answer with
  no `run` call; the decision line showed `answered` with the quote verbatim (SHA-256 matched).
- (d) Withdrawn decision and unknown id — **PASS**. Both tasks stayed `waiting_decision` through an answer attempt
  (409, 404), an unrelated answer and a 36 s wait.

Stated limits, kept from that report:

- **Finding 1 (not fixed, outside this feature).** The comm loopback read sends no `Authorization` header, so with
  `server.token` set every comm verb built on it, this one included, returns `HTTP 401: unauthorized`. Step (b) is a
  PASS for the default empty-token configuration only.
- The backend part of (c), a released task running a worker to completion, was **NOT RUN**: the sandbox has no tmux or
  worker backend, and the start attempt failed with `create tmux session: exit status 1`.
- The Web UI was not driven; the tracker's Web UI rows and the MCP, CLI, comm and config-key rows keep Validated "No".
- Not exercised live: the gate switched off, the two-reason join, `limit` and the 400/403/503 cases of the read
  route, a second shadow scan for dedup.

## Open items for the operator

1. Finding 1 above (comm verbs are refused when `server.token` is set).
2. The intermittent node test (`app-notify-events.test.js`, fixed 25 ms wait).
3. The real worker-backend run of a released task, and a browser pass over the Decisions card.
4. The gate ships in `shadow`; nothing is held until an operator sets `autonomous.questions.decision_gate_mode` to `on`.
5. `docs/plans/2026-10-29-decision-gate-live-check.md` has no `## Parity surface` heading, which `AGENT.md` asks of
   plan documents; this report has one. Left as is.

## Final verification after this report was written

`make sync-docs` then `bash scripts/check-docs-sync.sh`: exit 0, `docs/ and internal/server/web/docs in sync.`
(run with this report present). `bash scripts/check-no-internal-refs.sh`: exit 0. `bash scripts/check-howto-coverage.sh`:
exit 0.
