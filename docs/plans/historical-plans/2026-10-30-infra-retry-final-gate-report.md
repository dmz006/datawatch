# Infrastructure retry — final gate report

Date: 2026-10-31 · Branch: `automaton/ced90001` · Tree: commit `d39520de4` plus two uncommitted changes made by this
task (the plan checklist update described under item 14 below, and this report).

This is the Phase Completion Checklist from `AGENT.md`, run for the infrastructure retry feature as a whole. Every
command ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux server command was run, nothing was killed by name or by
anything else, and no version was bumped. The feature is described in [the plan](2026-10-30-infra-retry.md), the
[live-check report](2026-10-30-infra-retry-live-check.md), the
[operator how-to](../howto/infrastructure-retry.md) and the `## Infrastructure retry` part of
[`docs/operations.md`](../operations.md).

## Result in one paragraph

Build, vet, gofmt, the full Go suite (83 packages, none cached), the seven changed packages uncached, the locale
guards, the leak audit, the docs-sync check, the how-to audit, both tmux audits, the version and changelog checks, the
word check and the parity-issue check all pass. Two things did not pass and are recorded exactly below, neither one a
fault in this feature. First, the full node suite has one deterministic failure,
`app-merged-member.test.js`, which fails identically on the merge base before any commit of this branch (a field-order
assertion between two other features' settings fields); I did not fix it. Second, the leftover-daemon guard failed three times in a row
on a test daemon that is not in my snapshot; it belonged to another worktree's live-check script, which started it after
my snapshot. I did not end it, it ended by 02:14:54, and the guard then passed against the original snapshot. The live check (separate report) passed 7 of 7 steps, with the limits stated in the
last section.

## Checks

| # | Command | Result |
|---|---|---|
| 1 | `go build ./...` | exit 0, no output |
| 2 | `go vet ./cmd/datawatch ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server` | exit 0, no output |
| 3 | `gofmt -l` on the 52 changed `.go` files (`git diff --name-only main...HEAD -- '*.go'`, existing files only) | exit 0, no files listed |
| 4 | `go test ./...` | exit 0 in 186 s: 83 packages `ok`, 12 `[no test files]`, 0 cached, no `FAIL` line |
| 4b | `go test -count=1 ./cmd/datawatch ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server` | exit 0 in 119 s: `cmd/datawatch` 106.811s, `internal/autonomous` 58.801s, `internal/config` 1.829s, `internal/federation` 0.042s, `internal/mcp` 1.974s, `internal/router` 117.844s, `internal/server` 66.891s, all `ok` |
| 5 | `node --test internal/server/web/*.test.js` | **exit 1, 3 runs, same result each time**: 1781 tests, 1780 pass, 1 fail. See "Failure 1". The feature's own file passes: `node --test internal/server/web/app-infra-retry.test.js` is part of the run and is not the failing file |
| 6 | `bash scripts/check-no-internal-refs.sh` | exit 0: `PASS: no internal refs in user-facing surfaces.` |
| 7 | `bash scripts/check-docs-sync.sh` | exit 0: `docs/ and internal/server/web/docs in sync.` (no drift on the first run, so `make sync-docs` was not needed then; it was run once after the plan and this report were edited, see the last section) |
| 8 | `bash scripts/check-howto-coverage.sh` | exit 0: `PASS: every howto under docs/howto/ has exec_steps or is LLM-only.` |
| 9 | `bash scripts/check-no-unscoped-tmux.sh` | exit 0: `OK: no unscoped tmux kills` |
| 10 | `bash scripts/check-test-daemon-cleanup.sh` | exit 0: `OK: no daemon cleanup violations` |
| 11 | `go test -count=1 -v -run Locale ./internal/server` | exit 0, 19 top-level tests `PASS` (list below) |
| 12 | `bash scripts/check-no-leftover-test-daemons.sh snapshot <file>` before the Go run, then `check <file>` after all runs | snapshot: 13 live test daemons, none started by this task. Check at 02:11, 02:12:25 and 02:12:51: **exit 1**, one daemon not in the snapshot, started by another worktree's script. Check at 02:14:56: **exit 0**, `OK: no leftover test daemons`. See "Failure 2" |
| 13 | grep of the added lines in the changed user-visible files for the word `PRD` | 0 in `app.js`, the five locale bundles, `openapi.yaml`, the how-to, `docs/operations.md`, `README.md`, `docs/config-reference.yaml`, `docs/datawatch-definitions.md`, `docs/mcp.md`, `docs/api-mcp-mapping.md`, `docs/parity-status.md`, `CHANGELOG.md`, `docs/README.md`, `docs/howto/README.md`, `docs/cursor-mcp.md`, `docs/implementation.md`. 2 in `docs/testing-tracker.md`, both inside test names and test descriptions (`TestInfraRetryText_NeverSaysPRD`, "no new string says PRD"), not in any string a user reads. 0 added Go lines carry the word inside a string literal. The locale guard `TestLocales_PRDNeverUserFacing` passes (row 11) |
| 14 | `git diff main...HEAD -- cmd/datawatch/main.go internal/server/api.go` filtered for `var Version`, and `git diff main...HEAD -- CHANGELOG.md` filtered for `^+## v` | 0 changed `var Version` lines (`main.go` gains one `InfraRetry:` field in the autonomous config copy; `api.go` gains the config read and the checked write of the new block, 13 lines added and 1 reformatted); 0 added `## v` headings (the one added line is a bullet in the existing unreleased section) |
| 15 | `head -1 docs/plans/parity-issues/infra-retry.md` | `Issue: dmz006/datawatch-app#319` |
| 16 | `bash .datawatch/verify/infra-retry-final-gate.sh` | see "Verify script" below |

Locale guard tests that ran and passed: `TestLocales_AllPresent`, `TestLocales_ParityWithEnglish`,
`TestLocales_CommonNavKeysPresent`, `TestLocales_AllAppJSKeysExistInEnglishBundle`,
`TestLocales_AutomatonNeverUntranslatedOrMistranslated`, `TestLocales_AutomationKeyExemptionIsNarrow`,
`TestLocales_PRDNeverUserFacing`, `TestLocalesSecurityFailureKeysInAllBundles`,
`TestLocalesSecurityFailureNoPRDWording`, `TestLocalesSecurityFailureNoOrphanKeys`,
`TestQuestionsSettings_LocaleKeyInAllBundles`, `TestQuestionNotifySettingsLocale`,
`TestQuestionQuality_SettingsLocaleKeys`, `TestAllowedProjectRoots_SettingsCardAndLocales`,
`TestJudgeConfigLocaleParity`, `TestListenerAuth_LocaleKeys_AllBundles`, `TestMCPPublicURLLocaleParity`,
`TestRateCeilingLocaleParity`, `TestProxySandboxMux_ServesLocalesUnauthenticated`.

Audit Logging Rule tests: **N/A.** The feature adds no operator audit event. Retries, the hold and the dependent
recovery are written to the Automaton's own decision log (the `decisions` array on the Automaton), which is not the
operator audit trail, so the JSON-lines and CEF round-trip tests the rule asks for do not apply.

## Failures, recorded exactly

### Failure 1 — one node test fails, and it fails before this branch

`node --test internal/server/web/*.test.js`, first run (02:11:26), then twice more (02:12:08 and 02:12:10), then the
failing file alone twice. Every one of the five runs failed the same way:

```
✖ GENERAL_CONFIG_FIELDS: autonomous.automation.merged_member_policy is a completed, manual or block select defaulting to completed
  AssertionError [ERR_ASSERTION]: the field sits next to the merge-pending fields

  94 !== 90

      at TestContext.<anonymous> (.../internal/server/web/app-merged-member.test.js:78:10)
    actual: 94
    expected: 90
ℹ tests 1781
ℹ pass 1780
ℹ fail 1
exit=1
```

The test asserts that the settings field `autonomous.automation.merged_member_policy` sits directly after
`autonomous.automation.merge_pending_alert_minutes` in `GENERAL_CONFIG_FIELDS`. In `internal/server/web/app.js` the
cleanup fields (`cleanup_mode` and the three after it, up to `disk_alert_percent`) sit between those two keys. The
assertion compares the index of `merged_member_policy` (actual, 94) with the index of `merge_pending_alert_minutes`
plus one (expected, 90), so four fields sit in between.

Why this is not this feature, with the evidence:

- **It fails at the merge base.** I extracted `git merge-base main HEAD` (`b4ab1513f`) with `git archive` into a folder
  under this project, and ran `node --test internal/server/web/app-merged-member.test.js` there. Result: exit 1,
  13 tests, 12 pass, 1 fail, the same assertion, `94 !== 90`. The extract was removed afterwards.
- **The test file and the order are not touched by this branch.** `app-merged-member.test.js` has the same content at the
  merge base, on this branch and on `main`. This branch's only edit inside `GENERAL_CONFIG_FIELDS` adds the seven
  `autonomous.infra_retry.*` fields after the existing merged-member field, so it cannot move the two keys the
  assertion compares.
- **`main` already has the two keys next to each other** (`merge_pending_alert_minutes` then
  `merged_member_policy`, with the cleanup fields after them), so a merge will bring the fix.

I did not fix it here. It is outside this feature, and the fix is to reorder a block of `app.js` that `main` has already
reordered, which would only make the merge conflict. The checklist item for the node suite is therefore **not ticked
clean**; see the checklist.

### Failure 2 — the leftover-daemon guard reports a daemon that is not this task's

`bash scripts/check-no-leftover-test-daemons.sh check .datawatch/final-gate/daemons.before`, exit 1 (first check, 02:11:31):

```
  ✗ leftover test daemon: pid=3532336 age=1m 10s config=/tmp/dw-uncommitted-live.eKWQbg/config.yaml tcp_listening=35931,58697,58323,50033,53613
FAIL: 1 leftover test daemon(s) not in the baseline .datawatch/final-gate/daemons.before (live test daemons now: 13)
```

Evidence that it is not mine: its command is the `bin/datawatch` of a different worktree
(`prd-worktrees/83250001`), its working directory is that worktree, and its parent is
`bash scripts/uncommitted-work-live-check.sh`, a script this branch does not contain. It started about
02:10, after my 02:05 snapshot, while my runs were in progress. The count stayed at 13, so one daemon from the baseline must have
exited in the meantime. None of the tests I ran starts a daemon on a temporary config (the cleanup audit in
row 10 passes), and the live check for this feature ended its own daemon, planner stand-in and private tmux server
(step g of the live-check report, which also ran this guard). Re-checked at 02:12:25 and 02:12:51: same daemon, still running, exit 1 both times.
I did not end it, since it is another job's process and I may not kill by name or touch other jobs' processes.

Outcome: at 02:14:54 `ps` no longer listed pid 3532336, and the final check (02:14:56) exited 0 with
`OK: no leftover test daemons`. So nothing from this task is left running, but the guard's failures earlier in the run
are real and are kept here rather than hidden.

### Run notes (not feature failures)

- Scratch outputs for this task were kept in `.datawatch/final-gate/` (git-ignored) and removed at the end, except the
  leftover-daemon snapshot, which is removed with them.
- `go test ./...` ran detached in the background and was waited for; its output ends with its own exit line.
- `pgrep` used for a look at test daemons matched this task's own shell once (a `cwd` of this worktree with a mangled
  config argument); that was my own command line, not a daemon, and nothing was signalled.

## Phase Completion Checklist

- [x] **1. Functionality implemented per each phase's description.** Phases 1 to 14 of the plan are Done: the
  configuration keys, the classifier, the records and back-off, the executor wiring, the hold and question, the
  dependent recovery, the REST read and config keys, the MCP tool, the CLI command, the comm verb, the PWA and its
  locale keys, the parity record and the documentation. 23 task commits on the branch, `f40b2c247` to `d39520de4`.
- [x] **2. Unit tests added and passing.** `internal/autonomous/infra_failure_test.go`, `infra_retry_test.go`,
  `infra_retry_executor_test.go`, `infra_retry_hold_test.go`, `infra_retry_dependents_test.go`,
  `infra_retry_held_cause_test.go`, `infra_retry_text_test.go`, `infra_retry_view_test.go`, `launch_failure_test.go`;
  `internal/config/infra_retry_test.go`; `internal/server/infra_retry_test.go`, `infra_retry_config_test.go`;
  `internal/mcp/infra_retry_tools_test.go`; `internal/router/infra_retry_comm_test.go`;
  `cmd/datawatch/cli_infra_retry_test.go`; `internal/server/web/app-infra-retry.test.js`. All pass uncached (row 4b)
  and in the full run (row 4).
- [x] **3. Live check against a real running daemon.** Run on 2026-10-31 against a sandbox daemon, 7 of 7 steps passed
  (see the last section). The PWA in a browser, MCP, comm and the non-REST config surfaces were not driven live, so
  their Validated cells in the tracker stay No.
- [ ] **4. `go test ./...` and `node --test internal/server/web/*.test.js` both green.** Go: green. Node: **not green**,
  1 failure in a file this branch does not touch and that fails at the merge base too (Failure 1). Left unticked on
  purpose.
- [x] **5. General documentation checklist (8 items):**
  1. `CHANGELOG.md` — [x] one bullet added to the unreleased section, no versioned heading.
  2. `docs/config-reference.yaml` — [x] 57 added lines for the seven `autonomous.infra_retry.*` keys.
  3. `docs/operations.md` — [x] 27 added lines.
  4. `README.md` — [x] one line changed.
  5. Documentation index — [x] `docs/README.md` (1 line) and `docs/howto/README.md` (1 line, the new how-to).
  6. `docs/testing-tracker.md` — [x] 29 added lines, one row per surface.
  7. No leaked tracker IDs — [x] `scripts/check-no-internal-refs.sh` passes (row 6).
  8. Parity surface — [x] the `## Parity surface` section of the plan, and `docs/parity-status.md` (1 line).
- [x] **6. Conditional docs.** `docs/datawatch-definitions.md` — [x] Infrastructure retry section, 29 added lines.
  `docs/howto/infrastructure-retry.md` — [x] new, 304 lines; `check-howto-coverage.sh` passes. `docs/flow/*.md` —
  **N/A**: no new data flow beyond the existing attempt loop and review path, as the plan says.
- [x] **7. Mobile-Parity Rule.** One sync issue on the `datawatch-app` repository, `dmz006/datawatch-app#319`; the
  filed text is kept in `docs/plans/parity-issues/infra-retry.md`, whose first line is `Issue: dmz006/datawatch-app#319`
  (row 15). `docs/parity-status.md` carries the row.
- [x] **8. Localization Rule.** 31 new keys in all five bundles (`de`, `en`, `es`, `fr`, `ja`);
  `TestLocales_AllAppJSKeysExistInEnglishBundle` and the other locale guards pass (row 11). The non-English values
  carry the English text until translations arrive, as the parity issue says.
- [x] **9. Version left for merge.** Not bumped, by this task's instruction: `var Version` is unchanged in
  `cmd/datawatch/main.go` and `internal/server/api.go`, and there is no new `## v` heading (row 14). The merge helper
  sets the version.
- [x] **10. Plan status.** The plan's phase-status line already read "all phases Done"; its two stale checklist lines
  (live check "pending", the full run "is the branch's final gate") are updated by this task to point at the live-check report and this
  report, and it does not name a shipping version, on purpose.

The rest of the checklist maps to the checks above in this order: unit tests (2) to rows 4 and 4b, live check (3) to
the live-check report, full suites (4) to rows 4 and 5, documentation (5, 6) to rows 6 to 8, parity (7) to row 15,
localization (8) to row 11, version (9) to row 14, plan status (10) to the plan edit.

## Parity surface

Full set: `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone`. Nothing is excluded.

| Surface | Covered by | Where it is tested |
|---|---|---|
| REST | `GET /api/autonomous/infra-retries` and the seven `autonomous.infra_retry.*` keys on `GET`/`PUT /api/config`; `internal/server/infra_retry.go`, `internal/server/web/openapi.yaml` | `internal/server/infra_retry_test.go`, `infra_retry_config_test.go`; live check (a), (f) |
| MCP | tool `autonomous_infra_retries` and the `autonomous_config_set` arguments; capability mapped in `internal/federation/mcp_tool_caps.go`; `docs/mcp.md`, `docs/api-mcp-mapping.md` | `internal/mcp/infra_retry_tools_test.go`; live check (f) |
| CLI | `datawatch autonomous infra-retries [id] [--limit] [--json]`, `datawatch config set` | `cmd/datawatch/cli_infra_retry_test.go`; live check (f) |
| comm channel | verb `autonomous infra-retries`, `configure` for the keys | `internal/router/infra_retry_comm_test.go`; live check (f) |
| YAML/config | `autonomous.infra_retry.*`; `internal/config/infra_retry.go`, `docs/config-reference.yaml` | `internal/config/infra_retry_test.go`; live check (a) |
| PWA | the Infrastructure retries card, the task badge and held label, the seven settings fields, 31 locale keys in 5 bundles | `internal/server/web/app-infra-retry.test.js`, the locale guards. Not driven in a browser |
| Android | `datawatch-app#319`, Android acceptance list | issue filed; implementation is the app's work |
| iPhone | `datawatch-app#319`, iPhone/iOS acceptance list | issue filed; implementation is the app's work |

## Live-check result

From [the live-check report](2026-10-30-infra-retry-live-check.md): sandbox `datawatch v9.47.1`, built from this
worktree, production daemon untouched, scratch folder deleted afterwards. All seven steps passed:

- (a) config round trip, (b) a transient failure retries, (c) repeated failure holds the task, (d) dependents recover,
  (e) a non-transient failure is not retried, (f) REST, MCP, CLI and comm agree, (g) cleanup.

Stated limits, kept from that report:

- The worker is a stand-in, so a retried task was shown to leave the retry loop and spawn a live worker, not to run to
  completion.
- The PWA card and badges, `config set` on the CLI, `configure` on comm and `autonomous_config_set` on MCP were not
  driven live; their rows in the testing tracker keep Validated "No".

## Verify script

`bash .datawatch/verify/infra-retry-final-gate.sh`, run at 02:13:59 after the plan edit and `make sync-docs`: exit 0 in
53 s, last line `done (fail=0)`. It checked the report exists with a `## Checks` heading and no failed-result marker,
gofmt on the changed `.go` files, no `var Version` change, `go build ./...`, the `InfraRetry|InfraFailure|InfraBackoff`
tests in the seven packages (all `ok`), `node --test internal/server/web/app-infra-retry.test.js` (29 tests, 29 pass),
and the leak, docs-sync, how-to, tmux and daemon-cleanup scripts (all pass). It does not run the full node suite, which is
why Failure 1 does not show there.

## Open items for the operator

1. `app-merged-member.test.js` fails at the merge base and on this branch (Failure 1); `main` already has the field order
   it asserts, so the merge should clear it. Confirm by running the node suite after the merge.
2. Nothing to do for Failure 2: the other job's daemon has ended and the guard is clean. If the guard fails again in a
   later run on a busy host, look at the config path and the parent script before blaming the branch under test.
