# UI-prompt feature — final gate report

Branch `automaton/e8890001` (HEAD `3096d0129`), verified read-only on 2026-11-01.
Full gate log: `ui-prompt-final-gate.log` (repo root).

## Gate: PASS — 0 failures

`.datawatch/verify/ui-prompt-final-gate.sh` ran to completion (script ends with
`exit $fail`; the log contains zero `FAIL:` lines and every `run` step shows its
success output). Steps, with their results:

| Step | Result |
|---|---|
| `go test -count=1 ./internal/session/ ./internal/config/ ./internal/server/ ./internal/router/ ./cmd/datawatch/ ./internal/mcp/` (ui_prompt targets) | `ok` in every package (log lines 7–21) |
| `go test -count=1 ./internal/session/ ./internal/audit/ ./internal/stats/ ./internal/metrics/ ./internal/server/ ./cmd/datawatch/` (ui-prompt targets) | `ok` in every package (log lines 26–31) |
| `go test -count=1 ./internal/session/ ./internal/autonomous/ ./internal/config/ ./internal/audit/ ./internal/stats/ ./internal/metrics/ ./internal/server/ ./cmd/datawatch/` (interstitial targets) | `ok` (log lines 42–44) |
| `gofmt -l` on all branch-changed `.go` files | clean |
| `node --test internal/server/web/app-ui-prompt-settings.test.js` (per-task script) | 10 pass / 0 fail (log lines 59–70) |
| `internal/session` ui_prompt + escalation tests | `ok` 4.434s (log line 42) |
| `bash -n` on verify scripts, shellcheck `-S error` where present | pass |
| PWA settings test (main script) | **10 pass / 0 fail** (log lines 114–122: "tests 10, pass 10, fail 0") |
| `go test -count=1 ./...` (full tree) | `ok` in **all** packages incl. session 182.7s, router 118.0s, autonomous 61.0s, cmd/datawatch 106.8s (log lines 95–108) |
| `bash scripts/check-no-unscoped-tmux.sh` | OK: no unscoped tmux kills |
| `bash scripts/check-test-daemon-cleanup.sh` | OK: no daemon cleanup violations |
| `bash scripts/check-no-internal-refs.sh` | ✓ PASS: no internal refs in user-facing surfaces |
| `bash scripts/check-howto-coverage.sh` | ✓ PASS: every howto authored or LLM-only |
| `bash scripts/check-no-production-service-control.sh` | OK: no production service-control hits |
| `golangci-lint run ./internal/session/... ./internal/config/... ./internal/audit/... ./internal/stats/... ./internal/server/... ./cmd/datawatch/...` | **0 issues** (last log line) |

**Failures: none.** Nothing to report verbatim.

## Claim 1 — dismissed with Esc only, never Enter/newline/CR/C-m/digits — CONFIRMED

- `internal/session/interstitial_prompt.go:93–98`: `interstitialSafeKeys` is a
  closed map; only `autocompact-threshold` carries a safe key, and it is **Esc**.
  `interstitialSafeKey` (line 108): "a missing kind or an empty key is the
  refusal". Nothing else in the codebase supplies a key for a known kind.
- `internal/session/ui_prompt_dismiss.go:21–85` (`uiPromptSafeDismissKey`): the
  guard independently **rejects `enter`, `c-m`, `\x1b[13~`, newline, CR** and any
  key with a digit in its name, and accepts only the classifier's closed allow-list.
- `internal/session/ui_prompt_dismiss.go:104,107`: `uiPromptKeySend` uses
  `sendKeyEsc` (raw `Esc` via `SendKeyRaw`) — no literal/newline is ever sent.
- 400k-fixtures test: `internal/session/ui_prompt_dismiss_test.go:197–252`
  (`TestUIPromptDismiss_400kFixture`) — parks the exact pane
  `❯ Autocompact at 400k / Keep auto — Esc (not now)`, classifies the kind, and
  asserts the guard **refuses `enter`** ("enter is not the safe dismiss key for
  any known kind") while only `esc` gets sent; `TestUIPromptDismiss_EscCleared`
  (line 254) shows the cleared outcome with the audit line
  "Dismissed with Esc (prompt: autocompact-threshold, attempt 1)".

## Claim 2 — one question, names the session, shows the screen — CONFIRMED

- `internal/session/ui_prompt_escalate.go` (trigger block): fires once per
  occurrence, only after the attempts cap with the prompt still visible, and
  withdraws on clear (`prompt_cleared`) and session end (`session_ended`).
- `internal/autonomous/ui_prompt_escalation.go:76–115` (`uiPromptEscalationText`):
  the question text carries "Session: <full id> (name …)" and a neutralised
  "Screen (last N non-empty lines …)" excerpt; options are exactly `ack`/`hold`,
  default `hold` (lines 117–121).
- Tests: `internal/autonomous/ui_prompt_escalate_test.go:102`
  (`TestUIPromptEscalate_DaemonRaisesTheOperatorQuestion`) asserts one open
  question, session link, and that the text contains the screen excerpt lines;
  `:158` proves a duplicate occurrence raises **no second** question; `:205`
  proves the fallback alert "names the session";
  `internal/session/ui_prompt_escalate_test.go:92` proves
  "fires exactly once (trigger attempts_exhausted)" and
  `:300` that a failed hook stays one-shot.

## Claim 3 — four settings round-trip on all surfaces, PWA/help/definitions — CONFIRMED

Keys: `session.dismiss_ui_prompts`, `session.ui_prompt_dismiss_delay_seconds`,
`session.ui_prompt_clear_bound_seconds`, `session.ui_prompt_max_attempts`
(`internal/config/config.go:1803–1819`, defaults 5 / 30 / 2, validators at
1844–1864).

- **YAML**: `internal/config/ui_prompt_settings_test.go:123`
  `TestUIPromptSettings_RoundTrip` + `:157` LoadFromFile.
- **REST**: `internal/server/ui_prompt_settings_test.go:59`
  `TestUIPromptSettings_ConfigAPI`; API read/write at `internal/server/api.go:5901–5902, 8493–8531`;
  bad-value 400 with the key named at `api.go:10053–10115`.
- **comm configure**: `internal/router/ui_prompt_configure_test.go:13,50`.
- **MCP**: `internal/mcp/ui_prompt_config_set_test.go:113,143`.
- **CLI**: `cmd/datawatch/ui_prompt_config_set_test.go:105,134,164`.
- **PWA**: `internal/server/web/app-ui-prompt-settings.test.js` — 10 tests:
  Sessions card renders the four settings "with values, help and docs link"
  (line 87), each row carries `href="/diagrams.html#docs/datawatch-definitions.md#..."`
  (line 90, `hits == 4`); writes go through one `PUT /api/config` per change;
  locale keys present in EN/DE/ES/FR/JA (line 168: "all five locale bundles
  carry the eight keys").
- **Definitions entry**: `docs/datawatch-definitions.md:513–520` — the four keys
  in a table with defaults (on / 5 / 30 / 2) and the help sentences naming all
  surfaces (`configure`, `PUT /api/config`, `config_set`, `datawatch config set`,
  PWA).

## Claim 4 — audit entries, timeline lines, stats count — CONFIRMED

- **Audit**: `internal/audit/events.go:193–220` — `ui_prompt_dismissed`,
  `ui_prompt_not_cleared`, `ui_prompt_refused` and the escalation
  `ui_prompt_escalation_raised/refused/withdrawn`. Test
  `internal/audit/ui_prompt_dismiss_test.go`
  (`TestUIPromptDismiss_AuditActionsVerify`) asserts fixed CEF signatures 62/63/64
  with actor `autonomous`, session slot and kind/key/option details — and the
  400k-guard refusal audit at `ui_prompt_dismiss_test.go` (session pkg) line 45
  (`ui_prompt_refused`).
- **Timeline**: `internal/session/ui_prompt_dismiss.go:96–100` —
  "Dismissed with Esc (prompt: autocompact-threshold, attempt 1)" and
  "The key did not clear the prompt (autocompact-threshold, attempt 1)";
  asserted verbatim in the tests (e.g. `ui_prompt_dismiss_test.go:44` on
  refusal; `TestUIPromptDismiss_EscCleared` timeline check).
- **Stats**: `internal/stats/collector.go:220–226` `UIPromptStats`
  (dismissed / not_cleared / refused + by_kind), JSON
  `"ui_prompts,omitempty"` (line 146; `ui_prompts.json` shape in test
  `internal/stats/ui_prompt_dismiss_test.go`: "totals = dismissed=3
  not_cleared=1 refused=2", omitted when no getter); Prometheus counter
  `datawatch_ui_prompt_dismissals_total{kind,outcome}` in
  `internal/metrics/collector.go:111–143` with its test.
- **Release smoke**: `scripts/release-smoke.sh:6181+` section 92 verifies
  defaults (true/5/30/2), a good round-trip, and refusals (0 / -8 / 1.5 → 400)
  **only against the script's own isolated daemon** (line 6205).

## Claim 5 — version strings unchanged — CONFIRMED (branch-level)

- `cmd/datawatch/main.go:122` and `internal/server/api.go:204` both read
  `var Version = "9.49.1"`.
- `git diff $(git merge-base HEAD main) HEAD -- cmd/datawatch/main.go internal/server/api.go`
  contains **no** `+/-var Version` line — the branch never touched the version
  lines; they are byte-identical to the branch base (verified with
  `git show 02f6f444bd:…`, both files "9.49.1").
- Nuance (not a failure): current `main` later advanced to "9.56.3" via other
  merged branches (`364137389`, `11419e2c7`, `44fb0db08` bump commits), so the
  worktree's 9.49.1 differs from *today's* main. The rule's intent — this branch
  did not bump or otherwise change the version — holds exactly.

## Claim 6 — parity issue body file present, not filed — CONFIRMED

- File: `docs/plans/parity-issues/claude-interstitial-prompt-dismissal.md`
  (7498 bytes), with the body delimited by `<!-- body-start -->` /
  `<!-- body-end -->`. The post-marker section is titled "Filed issue":
  "The supervisor files this issue at the merge; it is **not filed from the
  branch**." and "**Issue: not yet filed.**" The file's own provenance header
  says the text between the markers is the issue body and the sections after the
  end marker record the filed issue (recorded as none). The docs gate script
  `.datawatch/verify/ui-prompt-docs.sh:18,22` checks exactly this file passed.

## Claim 7 — tests use FakeTmux / t.TempDir only — CONFIRMED

- All session-package tests build on `newTestManagerWithFake`
  (`internal/session/bl89_fake_tmux_test.go`), which wires an in-memory
  `FakeTmux`; `internal/session/ui_prompt_dismiss_test.go:66–69` drives panes
  through `fake.Pane[session]` only. No test in `ui_prompt_*_test.go` or
  `interstitial_prompt_test.go` calls `tmux` directly, spawns a daemon on the
  live data directory, or touches `~/.datawatch` (grep for `UserHomeDir`,
  `exec.Command.*tmux`, `TmuxManager{` across these files: no hits; the single
  `/home/<user>/work/app` string is a literal label inside a test event struct in
  `internal/autonomous/ui_prompt_escalate_test.go:29`, never used as a path).
  Question-store tests use `t.TempDir()` exclusively.
- Repo audits passed in the gate: `check-no-unscoped-tmux.sh` (OK),
  `check-test-daemon-cleanup.sh` (OK: no daemon start without cleanup, no
  end-by-name, Go tests carry `t.Cleanup`), and the release smoke's ui_prompt
  section runs only against its own isolated daemon (skips when an external
  `DW_BASE` would point at live settings).

## Summary

Gate: **PASS (0 failures, 0 issues)**. All seven claims confirmed against the
code and its tests; the parity issue body is recorded as not-yet-filed; no test
touches the real data directory or a real tmux session; the branch made no
version change. No tracked files were modified by this verification.
