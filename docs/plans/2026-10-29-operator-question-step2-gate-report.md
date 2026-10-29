# Operator question channel — Step 2 gate verification report

Date: 2026-10-29 · Session: johnnyjohnny-5d3b · Branch: `automaton/e0830001`

Scope of verification: the Step 2 deliverable of the operator question channel
(announce routes: WebSocket event, comm messaging, alert, audited question
events, polled GET, the opt-in per-question session return line, the three
notification toggles reachable from every config surface, the five locale
bundles, the documentation set, and the datawatch-app parity record). Step 1
(channels, config switch, store, REST) already shipped and is re-verified here
only to the extent the tests touch it.

Operator decisions applied throughout: questions never expire by a clock;
the return path default is **event plus polled GET**, and the per-question
session return line is opt-in.

## Commands run

All shell commands ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux server
command was run, no process was killed, and no daemon was restarted (the
"live flip" tests below assert exactly that the switch is read per request).

| # | Command | Result |
|---|---------|--------|
| 1 | `env -u TMUX -u TMUX_TMPDIR go build ./internal/... ./cmd/...` | **PASS** (exit 0, no output) |
| 2 | `env -u TMUX -u TMUX_TMPDIR go vet ./internal/autonomous/ ./internal/server/ ./internal/router/ ./internal/mcp/ ./internal/config/` | **PASS** (exit 0, no output) |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./internal/autonomous/` | **PASS** — 127 `--- PASS` lines (top-level + subtests), 0 failures, `ok ... 0.316s` |
| 4 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./internal/server/` | **PASS** — 75 `--- PASS` lines, 0 failures, `ok ... 3.307s` |
| 5 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./internal/router/` | **PASS** — 7 `--- PASS` lines, 0 failures, `ok ... 0.022s` |
| 6 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./internal/mcp/` | **PASS** — 3 `--- PASS` lines, 0 failures, `ok ... 0.008s` |
| 7 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./internal/config/` | **PASS** — 8 `--- PASS` lines, 0 failures, `ok ... 0.002s` |
| 8 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run Question ./cmd/datawatch/` | **PASS** — 5 `--- PASS` lines, 0 failures, `ok ... 0.119s` |
| 9 | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run 'TestLocales' -v ./internal/server/` (locale guard) | **PASS** — 7/7 top-level tests: `TestLocales_AllPresent`, `TestLocales_ParityWithEnglish`, `TestLocales_CommonNavKeysPresent`, `TestLocales_AllAppJSKeysExistInEnglishBundle`, `TestLocales_AutomatonNeverUntranslatedOrMistranslated`, `TestLocales_AutomationKeyExemptionIsNarrow`, `TestLocales_PRDNeverUserFacing`; `ok ... 0.096s` |
| 10 | `env -u TMUX -u TMUX_TMPDIR bash scripts/check-docs-sync.sh` | **FAIL on first run, then PASS** — see "Docs sync" below |

Every command above is green in its final state. One honest incident to record:

### Docs sync (command 10)

- First run: **FAILED**, exit 1, with:
  `ERROR: embedded PWA docs drift from docs/` and
  `>f+++++++++ plans/parity-issues/operator-question-step2-notifications.md`.
- Diagnosis (faithfully): the parity record was committed under `docs/` in
  the previous task, but the generated embedded copy under
  `internal/server/web/docs/` (gitignored, regenerated at `make build` /
  `make sync-docs` time) was stale. The script's own message says to run
  `make sync-docs` and re-stage.
- Action taken: `env -u TMUX -u TMUX_TMPDIR make sync-docs` (exit 0). This
  only regenerates the gitignored embedded copy from `docs/` — it changes no
  source file and touches nothing outside this work's documents.
- Second run: **PASS** — `docs/ and internal/server/web/docs in sync.`, exit 0.

No other command failed; nothing else was fixed.

## Scope checklist

Every item below is proven by a committed test (all passing under commands 3–8
or 9 above) and/or a committed file.

| Scope item | Proven by |
|------------|-----------|
| **WebSocket** | `internal/server/question_raise_dispatch.go:70` — `s.hub.BroadcastQuestionRaised(QuestionRaisedData{...})` on raise; PWA consumes it in `internal/server/web/app.js` (`case 'question_raised'`, line 1295). Tests: `TestQuestionRaiseNotify_HubAndRouterEachCalledOnce` (`internal/server/question_raise_notify_test.go`), `TestQuestionStep2SwitchOn_LiveFlip_FansOutAndEmits` (`internal/server/question_step2_switch_off_test.go`). |
| **Messaging (comm)** | `internal/server/question_raise_dispatch.go:80-87` — messaging group dispatch, gated on `EffectiveNotifyMessaging()`; message text built in `internal/router/question_notify.go`. Tests: `TestQuestionRaiseNotify_MessagingToggleOffSuppressesRoute`, `TestQuestionRaiseNotify_RouterReceivesExactlyOne`, `TestQuestionRaiseNotify_TextNamesIDNumberedOptionsAndAnswerRoutes`, `TestQuestionRaiseNotify_TextWithoutOptions`. |
| **Alert** | `internal/server/question_raise_dispatch.go:88-114` (alert gate at line 94, high-priority questions only) — `alertStore.Add(alerts.LevelWarn, ...)` + `s.hub.BroadcastAlert(a)`, high-priority questions only, gated on `EffectiveNotifyAlert()`. Tests: `TestQuestionRaiseNotify_AlertOnlyWhenHighPriority`, `TestQuestionRaiseNotify_AlertToggleOffSuppressesAlert`. |
| **Question events (audit / pending-events)** | `internal/autonomous/question_events.go` (event emission for answer/withdraw/supersede); `internal/server/question_events_emit_test.go`: `TestQuestionEventEmitAnswer`, `..._Withdraw`, `..._SupersedingAnswer`, `..._MarkerPreventsDuplicatesAfterReopen`, `..._CrashRepairedOnce`, `TestQuestionEventEmitSwitchOff`. `internal/server/web/app.js:26580` includes `question.answered` / `question.withdrawn` in the pending-events kinds. |
| **Polled GET (default return path)** | `internal/server/questions.go` (QuestionsAPI list/get/answers) + `internal/server/question_polled_get_test.go`: `TestQuestionPolledGetOpenNoAnswer`, `..._AnsweredShowsAnswer`, `..._SupersededShowsLatest`, `..._WithdrawnState`, `TestQuestionPolledGetEventsReadReturnsQuestionAnswered`, `TestQuestionPolledGetSwitchOffReturns503`. This is the default path per the operator decision: event plus polled GET require no opt-in. |
| **Return path line (opt-in per question)** | `internal/autonomous/question_return_path.go` + `internal/autonomous/question_return_path_test.go`: `TestQuestionReturnPathMarkerPrefix`, `..._JSONFields`, `..._MultilineQuoteStaysOneLine`, `TestQuestionReturnPathGoneSessionLeavesAnswerQueued`, `TestQuestionReturnPathOffSendsNothing` (line not sent when off), `TestQuestionReturnPathSupersedingAnswerSendsNewLine`. Opt-in shape pinned by `TestQuestionPriorityReturnPathReturnPathRequiresSession` (`internal/autonomous/questions_step2_fields_test.go`). Default-on surface toggle `autonomous.questions.return_path`: `AutonomousQuestionsConfig.EffectiveReturnPath()` (`internal/config/config.go:3178`), `TestQuestionNotifyConfig_ExplicitFalseSurvivesRoundTrip/return_path`. |
| **Toggles on every surface** | Config core: `config.AutonomousQuestionsConfig` + `Effective*` helpers (`internal/config/config.go:3135-3181`), read live per request — tests `TestQuestionsConfig_DisabledByDefault`, `TestQuestionsConfig_YAMLRoundTripKeepsBothValues`, `TestQuestionNotifyConfig_DefaultsOnWhenKeyAbsent`, `TestQuestionNotifyConfig_ExplicitFalseSurvivesRoundTrip` (messaging/alert/return_path), `TestQuestionNotifyConfig_EffectiveHelpersNilIsOn`. PWA Settings card: `internal/server/web/app.js:16382-16385` (all four keys: `enabled`, `notify_messaging`, `notify_alert`, `return_path`). REST config: `TestQuestionNotifyConfig_RESTGetDefaultsOn`, `..._RESTPutThenGetRoundTrip`, `..._RESTNonBooleanRejected`. MCP: `TestQuestionNotifyConfigSurface_SetToggles` (`internal/mcp/question_notify_config_surface_test.go`). CLI: `TestQuestionNotifyConfigSurface_SetToggles`, `..._GetReadsToggles`, `TestQuestionsCLIConfigSetTogglesChannel` (`cmd/datawatch/`). Comm channel: `TestQuestionNotifyConfigSurface_ConfigureApplyPath`, `..._MentionedInConfigureHelpAndList` (`internal/router/`). 503-when-off: `TestQuestionRaiseNotify_SwitchOffSuppressesEverything`, `TestQuestionStep2SwitchOff_FireNothing` (proving "when the switch is off nothing in this work may fire"). |
| **Locale keys** | `settings_automata_questions_enabled`, `settings_automata_questions_notify_messaging`, `settings_automata_questions_notify_alert`, `settings_automata_questions_return_path` present in all five bundles (`internal/server/web/locales/{en,de,fr,es,ja}.json` — each gains the keys in the settings-card commit). Tests: `TestQuestionsSettings_LocaleKeyInAllBundles`, `TestQuestionNotifySettingsLocale` (`internal/server/questions_settings_locale_test.go`), plus the locale guards in row 9 above (parity, no untranslated terms, PRD never user-facing). |
| **Docs** | `docs/howto/operator-questions-notifications.md` (524 lines, new), `docs/api/autonomous.md`, `docs/api/openapi.yaml` (`/api/questions`, `questionRaise`, Questions tag), `docs/config-reference.yaml` (autonomous.questions block with all four keys), `docs/implementation.md`, `docs/operations.md`, `docs/parity-status.md`, `docs/testing-tracker.md` (66 lines added), `docs/api/ask.md`, `docs/api-mcp-mapping.md`, `docs/howto/README.md`, `CHANGELOG.md`. `scripts/check-docs-sync.sh` final state: PASS. |
| **Parity record** | `docs/plans/parity-issues/operator-question-step2-notifications.md` (153 lines; datawatch-app issue 283 filed and verified open per its commit). This is the file that caused the stale-embedded-copy failure in command 10, resolved via `make sync-docs`. |

## Constraint compliance (this session)

- No tmux server commands; every shell command under `env -u TMUX -u TMUX_TMPDIR`.
- No `pkill` / `kill` by name; no daemon restart.
- No tracker IDs (BL-style numbers) introduced in this report or touched code; no PRD-wording in user-facing text (enforced by `TestLocales_PRDNeverUserFacing`, `TestLocales_AutomatonNeverUntranslatedOrMistranslated` — passing).
- The throttle detector list is never named here; where relevant it is only "the list entries".
- When the channel switch is off: nothing fires — pinned by `TestQuestionRaiseNotify_SwitchOffSuppressesEverything` and `TestQuestionStep2SwitchOff_FireNothing`; REST endpoints refuse with 503 — pinned by `TestQuestionPolledGetSwitchOffReturns503` and the Step 1 `questions_config_switch` tests.
- The only remediation performed was `make sync-docs`, the exact remedy the failing check printed; it regenerates a gitignored artifact from already-committed docs and changes no source.

## Verdict

Gate **PASS**: build 0, vet 0, `go test -run Question` green across all six
packages (225 PASS lines total, 0 failures), locale guards 7/7, docs sync in
sync after regeneration. All ten scope items carry committed proof.
