# Final-gate + 7-claims verification — e8890001 (ui-prompt)

- **Run:** `env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/ui-prompt-final-gate.sh 2>&1 | tee ui-prompt-final-gate.log` — 2026-11-01 20:08:54–20:15:13 local (~390 s).
- **Gate result: PASS — 0 failures.** Log `ui-prompt-final-gate.log` (144 lines): `grep -ciE "^FAIL| FAIL:"` → **0**; `grep -c "^ok"` → **30** Go packages ok; node `--test` twice: `pass 10 / fail 0` (log L65–66, L122–123); `golangci-lint` → `0 issues.` (log L144); every `scripts/check-*.sh` audit printed its OK/✓ PASS line (log L137–143); shellcheck clean (L87–88); `skipped 0` (L68, L125).
- **No fixes attempted; no tracked file changed.** This file (plus the untracked gate log) is the output artifact.

## 7-claim verdicts

| # | Claim | Verdict |
|---|-------|---------|
| 1 | Dismissal sends Esc only — never Enter/newline/CR/`c-m`/digits | CONFIRMED |
| 2 | Operator escalation: one question, names the session, shows the screen | CONFIRMED |
| 3 | Four `session.ui_prompt*` settings round-trip on all surfaces + PWA/help/definitions | CONFIRMED |
| 4 | Audit entries, timeline lines, stats counters | CONFIRMED |
| 5 | Version strings unchanged on the branch | CONFIRMED |
| 6 | **Parity issue body file present, not filed** | **FAILED (see below — filed as datawatch-app#301, still OPEN)** |
| 7 | Tests use FakeTmux / `t.TempDir()` only; no unscoped tmux; no leftover daemons | CONFIRMED |

## Claim 6 — FAILED — verbatim evidence

**Claim under review:** the parity issue body file exists and is *not filed* against any remote.

**File (verbatim):** `tmp-evidence/parity-issue-verifier-wait-body.md` — exists, 70 lines, 5219 bytes.
- Line 1 (verbatim, beginning): `Lane fill learned a new lane state. A Claude lane whose Automaton has running work with every piece blocked waiting for a verifier slot is now reported as `waiting_verifier`, and the lane fill planner counts it as available under the new `autonomous.lane_fill.count_verifier_wait` setting (on by default). The app already renders the lane fill status view; this records the exact contract to render, the setting to expose, and the locale keys for the labels.`
- Line 70 (last, verbatim): `- Unknown or absent new fields render exactly as today, so the app stays forward-compatible.`

**Filing status (verbatim, `gh issue view 301 --repo dmz006/datawatch-app --json number,title,state,createdAt,author`):**
```json
{"author":{"id":"MDQ6VXNlcjU0ODg3NQ==","is_bot":false,"login":"dmz006","name":"David M. Zendzian"},"createdAt":"2026-10-29T22:12:16Z","number":301,"state":"OPEN","title":"[sync] Lane fill verifier-wait lanes: new REST state and fields, MCP, CLI, comm, PWA lane view, count_verifier_wait setting and three locale keys"}
```

**Verbatim identity (body diff, only a trailing newline differs):**
```
$ diff <(gh issue view 301 --repo dmz006/datawatch-app --json body -q .body) tmp-evidence/parity-issue-verifier-wait-body.md
71d70
<
```

**Verdict: FAILED.** The body is *not* in a "not filed" state: it is filed against `dmz006/datawatch-app` as **issue #301**, state **OPEN** (created 2026-10-29T22:12:16Z), whose stored body is byte-identical to the file (modulo the trailing newline).

**Where the prior attempt's report went wrong (verbatim context):** the tracked report `ui-prompt-final-gate-report.md` (committed in `689e9ec37`), line 134–136:

```
## Claim 6 — parity issue body file present, not filed — CONFIRMED
- File: `docs/plans/parity-issues/claude-interstitial-prompt-dismissal.md`
```

That file (`docs/plans/parity-issues/claude-interstitial-prompt-dismissal.md`, line 87: "Issue: not yet filed.") is the ui-prompt feature's *own* parity record and is indeed not filed (gh search `interstitial` / `autocompact` issues on `dmz006/datawatch-app` → `[]`). But that is not the file the claim cites here: for the flagged parity body file `tmp-evidence/parity-issue-verifier-wait-body.md`, the "exists, not filed" claim is false — it is filed as datawatch-app#301 (OPEN).

## Confirmed claims — evidence

**1.** `internal/session/interstitial_prompt.go:301` (`interstitialSafeKeys`) — closed map; only `esc` is safe (`IsSafeInterstitialKey`, L309). `internal/session/ui_prompt_dismiss.go:219` (`dismissKeyLiteral`) rejects `enter`/`return`/`c-m`/newline/CR/TAB/SPACE/digits/sketch rune-scan; only `esc` maps to `\x1b`. Tests on FakeTmux: `ui_prompt_dismiss_test.go:188` `TestUIPromptDismiss_400kAutocompactFixtureAutoDismissal`, `:439` `TestUIPromptDismiss_GuardRejectsDangerousKeys`.

**2.** `internal/autonomous/ui_prompt_escalation.go:91` (`RaiseUIPromptEscalation`), `:131` (`WithdrawUIPromptEscalation`), `:257` (`uiPromptEscalationText`): one question per run with "Session: <id>", last-40 non-empty screen lines, `ack`/`hold` options, default `hold`; deduped; withdrawn on clear.

**3.** `internal/config/config.go:1803,1809,1814,1819` — `session.dismiss_ui_prompts` (default true), `ui_prompt_dismiss_delay_seconds` (5), `ui_prompt_clear_bound_seconds` (30), `ui_prompt_max_attempts` (2), validated (`:2151,2161,2171`); round-trip tests `internal/config/ui_prompt_settings_test.go`; PWA rows `internal/server/web/app.js:16343-16346`; definitions `docs/datawatch-definitions.md:509-522`; REST/MCP/CLI/comm surfaces covered by gate tests (all `ok`).

**4.** `internal/audit/events.go:193-216` (audit event kinds), `internal/stats/collector.go:146` (`ui_prompts` counters), `internal/metrics/prometheus.go:194` (`datawatch_ui_prompt_dismissals_total`).

**5.** Merge-base `02f6f444` and HEAD both carry `var Version = "9.49.1"` in `cmd/datawatch/main.go:122` and `internal/server/api.go:204`; `git diff 02f6f44..HEAD -- cmd/datawatch/main.go internal/server/api.go` touches no `Version` line; no new versioned CHANGELOG heading on the branch.

**7.** Tmux-touching commands wrapped with `env -u TMUX -u TMUX_TMPDIR` in the gate script (`ui-prompt-final-gate.sh` `run()`); all dismissal tests use `FakeTmux`; autonomous tests use `t.TempDir()`; gate log shows `OK: no unscoped tmux` and `OK: no daemon cleanup violations` (L137–138); `check-no-internal-refs` ✓ PASS, `check-howto-coverage` ✓ PASS (L139–141).

## Output artifacts
- `ui-prompt-claims-verify-report.md` (this file — new/untracked; the prior report `ui-prompt-final-gate-report.md` is tracked and left unmodified).
- `ui-prompt-final-gate.log` (gate run, untracked; `*.log` gitignored).
