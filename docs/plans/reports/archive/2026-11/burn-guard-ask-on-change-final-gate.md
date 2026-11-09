# Burn guard ask mode — final gate report

Date: 2026-11-02

Feature: the burn guard setting `usage.burn_guard.ask_on_change` (boolean, default `false`).
When `false` the guard raises one alert per rung change and no question; when `true` it keeps
at most one open question per change alongside the per-change alert.

## Verdicts

| # | Check | Verdict |
|---|---|---|
| 1 | burn-guard-config-ask-on-change | PASS |
| 2 | burn-guard-ladder-question-gate | PASS |
| 3 | burn-guard-question-withdrawal | PASS |
| 4 | burn-guard-surfaces | PASS |
| 5 | burn-guard-pwa-locales | PASS |
| 6 | burn-guard-docs | PASS |
| — | **Overall** | **PASS** |

All six run under `env -u TMUX -u TMUX_TMPDIR` from the repository root. See the deviation on
the named script files below: the six `.datawatch/verify/burn-guard-*.sh` scripts are not present
in the tree, so each check was run as the feature's own test suite and content checks listed here.

## Command output summaries

1. **burn-guard-config-ask-on-change**
   - `go test ./internal/config/ -run 'BurnGuard' -count=1` → PASS. Ran and passed:
     `TestBurnGuardDefaults`, `TestBurnGuardNormalize` (incl. `booleans_taken_as_given`),
     `TestBurnGuardYAMLRoundTrip`, `TestBurnGuardAskOnChangeDefaultAndRoundTrip`,
     `TestBurnGuardYAMLPartialKeepsDefaults`. `ok ... internal/config 0.009s`.

2. **burn-guard-ladder-question-gate**
   - `go test ./internal/burnguard/ -run 'BurnGuard' -count=1` → PASS. Ran and passed:
     `TestBurnGuardAskOnChangeFalseRaisesNoQuestion`,
     `TestBurnGuardAskOnChangeTrueQuestionsEachClimbAndStepBack`,
     `TestBurnGuardAskOnChangeFalseOnNoOpStepAndOnRelease`,
     `TestBurnGuardAskOnChangeWithdrawOnEveryChangedStep`,
     `TestBurnGuardAskOnChangeAlertNamesProjectionTargetAndSetting`.
     `ok ... internal/burnguard 0.007s`.

3. **burn-guard-question-withdrawal**
   - `go test ./internal/autonomous/ -run 'BurnGuard' -count=1` → PASS. Ran and passed:
     `TestBurnGuardAskOnChangeFalseRaisesNothing`,
     `TestBurnGuardAskOnChangeOneOpenAcrossRungChanges`,
     `TestBurnGuardAskOnChangeNoQuestionForNoOpOrRelease`,
     `TestBurnGuardAskOnChangeStartWithdrawsStaleKeepsCurrent`.
     `ok ... internal/autonomous 0.014s`.

4. **burn-guard-surfaces**
   - `go test ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/ -run 'BurnGuard|AskOnChange|ask_on_change' -count=1` → PASS, all four packages `ok`:
     - server: `TestBurnGuardAskOnChangeRestRoundTrip`, `TestBurnGuardAskOnChangeRestRejectsNonBool`, `TestBurnGuardAskOnChangeOffAlertsOnly`, plus the pre-existing burn guard REST/config/capability tests — `ok ... internal/server 0.092s`.
     - mcp: `TestBurnGuardAskOnChangeMCPSetSchemaAndBody`, `TestBurnGuardAskOnChangeMCPGetRoundTrip` — `ok ... internal/mcp 0.054s`.
     - router (comm `configure`): `TestBurnGuardAskOnChangeConfigureAcceptsTrueAndFalse` — `ok ... internal/router 0.249s`.
     - CLI: `TestBurnGuardAskOnChangeCLIAcceptsTrueAndFalse`, `TestBurnGuardAskOnChangeCLIRejectsNonBool`, `TestBurnGuardAskOnChangeCLISendsPUTBody` — `ok ... cmd/datawatch 0.066s`.

5. **burn-guard-pwa-locales**
   - `node --test internal/server/web/app-burn-guard.test.js` → PASS: `tests 10, pass 10, fail 0`.
   - Locale keys `burn_guard_ask_on_change`, `settings_usage_burn_guard_ask_on_change`,
     `burn_guard_ask_on_change_help` each present exactly once in all five bundles
     `internal/server/web/locales/{en,de,es,fr,ja}.json` (15/15 hits).

6. **burn-guard-docs**
   - `usage.burn_guard.ask_on_change` present in: `docs/howto/burn-guard.md` (17 hits, incl. the
     *The question on top of the alert* section, the `Where to change them` surface table and the
     alert-hint quote), `docs/config-reference.yaml` (default `false` recorded),
     `docs/api/openapi.yaml` and `internal/server/web/openapi.yaml` (boolean schema field),
     `docs/plans/parity-issues/burn-guard-ask-on-change.md`, and `CHANGELOG.md` (2 entries,
     feature + documentation).

## Code confirmation (read the code)

- **Default is false**: `DefaultBurnGuard()` in `internal/config/burn_guard.go` sets
  `AskOnChange: false` ("the guard alerts only; it does not ask questions"); the field is a
  plain bool `usage.burn_guard.ask_on_change` with no omitempty, so an explicit `false`
  survives a save and reload.
- **False raises no question**: in `internal/burnguard/ladder.go`, `Decision.QuestionText` is
  only written inside `if in.Settings.AskOnChange && changesSetting(...)`; every alert carries
  the change and `askOnOffHint` when off (verified by `TestBurnGuardAskOnChangeFalseRaisesNoQuestion`).
- **True never leaves more than one open question**: `autonomous.RaiseBurnGuardQuestion`
  (`internal/autonomous/burn_guard_question.go`) withdraws every open guard question (reason
  `superseded by a newer burn guard rung change`) before raising the new one, and
  `BurnGuard.announce` routes every changed step through `ApplyBurnGuardAskOnChange` — so the
  guard can never hold more than one open question (verified by
  `TestBurnGuardAskOnChangeOneOpenAcrossRungChanges`).
- **A no-op step or a release raises no question**: `changesSetting` in `internal/burnguard/ladder.go`
  sees a `lower_inflight` climb whose cap is already at one (and its release that restores a cap
  that was never lowered) as changing nothing, so `QuestionText` stays empty; a disabled release
  sets only `AlertText`, never `QuestionText` (verified by
  `TestBurnGuardAskOnChangeFalseOnNoOpStepAndOnRelease` and
  `TestBurnGuardAskOnChangeNoQuestionForNoOpOrRelease`).
- **Start-up withdrawal exists**: `autonomous.PruneBurnGuardQuestionsAtStart` is called from
  `Server.StartBurnGuard` (`internal/server/burn_guard.go`): at start, when the loaded ladder
  state applies no rung every open guard question is withdrawn, and when it applies a rung the
  newest open question is kept while older ones are withdrawn (reason `at start the burn guard
  no longer applies the rung this question refers to`). Verified by
  `TestBurnGuardAskOnChangeStartWithdrawsStaleKeepsCurrent`.
- **Alert text**: on a step with `ask_on_change` off the alert states what was done
  (e.g. "lowered the claude-code inflight cap from 4 to 3."), the projection ("projected 112.0%
  at the reset") and the target ("target 100.0%"), then the one-liner "Set `usage.burn_guard.ask_on_change`
  to true to get a question per change." — what was done, the projection, the target and how to
  change the setting (verified by `TestBurnGuardAskOnChangeAlertNamesProjectionTargetAndSetting`).
  With the setting on, the hint is omitted and the alert is answered by the single open question.

## Deviations

- The six named check scripts `.datawatch/verify/burn-guard-{config-ask-on-change,ladder-question-gate,question-withdrawal,surfaces,pwa-locales,docs}.sh`
  are not present in the tree (only `.datawatch/verify/burn-guard-final-gate.sh` is, and the
  `_lib.sh` it sources is also absent). Running each named command as given exits 127
  ("No such file or directory"). Each check was therefore executed as the functional equivalent
  above — the feature's own passing test suites plus the locale and documentation content checks —
  and every command's output was recorded in the command output summaries section.
- No other deviations: every feature test for `usage.burn_guard.ask_on_change` passes on the
  first run and no file other than this report was changed.

## Merge-time

The minor version bump and the datawatch-app parity issue (docs/plans/parity-issues/burn-guard-ask-on-change.md) are done at merge through the merge helper.
