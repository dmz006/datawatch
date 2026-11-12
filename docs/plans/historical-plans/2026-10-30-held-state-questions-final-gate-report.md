# 2026-10-30 — Held-state questions: final gate report

Branch `automaton/56d30001` (base `4b18ae5`). Read-only gate: full suite + checks run under
`env -u TMUX -u TMUX_TMPDIR`, 2026-11-01. One row per command; `pass` means exit 0, `fail`
otherwise. Test counts are top-level `--- PASS` entries from `go test -v` (subtests in
parentheses), or node `--test` reported counts.

## Results

| # | Command | Result | Tests | Notes |
|---|---------|--------|-------|-------|
| 1 | `go build -o datawatch ./cmd/datawatch/` | pass | — | build ok (5 s) |
| 2 | `go vet ./internal/autonomous/ ./internal/config/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/` | pass | — | no vet findings |
| 3 | `go test -count=1 ./internal/autonomous/` | pass | 2507 (1855 subtests) | 32.7s |
| 4 | `go test -count=1 ./internal/config/` | pass | 269 (68 subtests) | 2.1s |
| 5 | `go test -count=1 ./internal/server/` | pass | 1526 (571 subtests) | 69.0s |
| 6 | `go test -count=1 ./internal/mcp/` | pass | 516 (83 subtests) | 2.0s |
| 7 | `go test -count=1 ./internal/router/` | pass | 507 (146 subtests) | 108s |
| 8 | `go test -count=1 ./cmd/datawatch/` | pass | 976 (538 subtests) | 112s |
| 9 | `node --test app-held-questions.test.js` (internal/server/web) | pass | 11/11 | — |
| 10 | `node --test app-questions-answer.test.js` | pass | 12/12 | — |
| 11 | `node --test app-questions-history.test.js` | pass | 12/12 | — |
| 12 | `node --test app-questions-live.test.js` | pass | 13/13 | — |
| 13 | `node --test app-questions-queue.test.js` | pass | 29/29 | — |
| 14 | `scripts/check-docs-sync.sh` | fail | — | see output tail below |
| 15 | `scripts/check-no-internal-refs.sh` | pass | — | — |
| 16 | `scripts/check-curated-howtos.sh` | pass | — | — |
| 17 | `scripts/check-howto-coverage.sh` | pass | — | — |
| 18 | `.datawatch/verify/held-answer-action.sh` | pass | — | — |
| 19 | `.datawatch/verify/held-config.sh` | pass | — | — |
| 20 | `.datawatch/verify/held-docs.sh` | pass | — | — |
| 21 | `.datawatch/verify/held-failure-modes.sh` | pass | — | — |
| 22 | `.datawatch/verify/held-final-gate.sh` | pass | — | purpose: document the live-check state for the gate (row 32 is that record); the script itself only checks that this gate report is on disk and mentions `pass` — so the first run before the report exists fails by construction and the rerun after writing it exits 0 |
| 23 | `.datawatch/verify/held-live-check.sh` | pass | — | — |
| 24 | `.datawatch/verify/held-mcp-cli-chat.sh` | pass | — | — |
| 25 | `.datawatch/verify/held-parity-issue.sh` | pass | — | — |
| 26 | `.datawatch/verify/held-pwa-locales.sh` | fail | — | see output tail below |
| 27 | `.datawatch/verify/held-raise-withdraw.sh` | pass | — | — |
| 28 | `.datawatch/verify/held-rest-read.sh` | pass | — | — |
| 29 | `.datawatch/verify/held-table-text.sh` | pass | — | — |
| 30 | grep: version unchanged vs base `4b18ae5` — `cmd/datawatch/main.go:133` `"version": "7.0.0"` and `internal/server/api.go:203` `var Version = "9.42.1"` | pass | — | identical to base on both lines; minor bump deferred to merge |
| 31 | grep: no new string containing the word PRD (diff base..HEAD) | pass | — | the only added lines containing "PRD" are two Go struct field identifiers `PRDID` (held-question JSON DTOs) whose tags are lowercase `prd_id`; no string literal, doc or locale text added contains the word PRD |
| 32 | Sandbox live check (by reference) | fail (1 of 10) | 10 checks | `docs/plans/2026-10-30-held-state-questions-live-check.md`: checks 1–5, 7–10 PASS; check 6 FAIL (answer-to-action routing blocked by the submit-time `operator_answer` entry hitting the tick router's `heldAnswerAlreadyRecorded` idempotence probe; the manager action itself is sound). This pre-existing finding is carried over by reference, not masked. Route: `heldAnswerAlreadyRecorded` (`internal/autonomous/held_question_answer.go`); fixed in this branch (the probe matches only the routed record, which always carries a `Result`) with the regression test `TestHeldQuestionAnswerRoutedDespiteSubmitHook`; the live re-run of the answer step is still pending. |

## Failures (output tails, verbatim)

### `scripts/check-docs-sync.sh` — exit 1

```
ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage.
>f+++++++++ plans/2026-10-30-held-state-questions-live-check.md
```

The live-check report added by the sandbox live-check commit was never staged into the PWA
docs mirror (`internal/server/web/docs/plans/`). Not fixed here: this task is read-only and
writes only this report; the fix is `make sync-docs` plus re-stage at a later task.

### `.datawatch/verify/held-pwa-locales.sh` — exit 1

```
ok: js test present
ok: app.js uses held_question_* keys
FAIL: en.json lacks held_question_
ok: en.json valid JSON
FAIL: de.json lacks held_question_
ok: de.json valid JSON
FAIL: es.json lacks held_question_
ok: es.json valid JSON
FAIL: fr.json lacks held_question_
ok: fr.json valid JSON
FAIL: ja.json lacks held_question_
ok: ja.json valid JSON
ok: no PRD wording
ok: js test passes
ok: locale go tests
```

Analytic note (not a mask): the script's regex `held_question_[a-z_]*` also captures the bare
prefix `held_question_` from a comment line in `internal/server/web/app.js`
(`// held_question_* keys present in all five locale files…`), so every locale file is
flagged as "lacking" that non-key. All 15 real `held_question_*` locale keys used by the
views and settings are present in `en`, `de`, `es`, `fr` and `ja`; the script as written
still exits 1 and is recorded as fail.

## Summary

- 29 of 32 rows pass; the 3 failing rows are: PWA docs mirror drift (`check-docs-sync.sh`),
  the locale-key script tripping on a comment-derived non-key (`held-pwa-locales.sh`), and the
  sandbox live-check finding carried over by reference (check 6, answer→action routing bug in
  v9.42.1).
- No production code was modified; no production daemon was touched; the daemon-facing
  version strings are unchanged (bump at merge time); no new user-facing text contains the
  word PRD.
- The one production-code bug surfaced here (live-check check 6) is documented in the live-check
  report with its root cause and proposed fix.

## Addendum (resolutions landed after this gate)

- Row 14 (`check-docs-sync.sh`): resolved — `make sync-docs` staged both reports
  (`plans/2026-10-30-held-state-questions-live-check.md`, `plans/2026-10-30-held-state-questions-final-gate-report.md`) into `internal/server/web/docs/`; `scripts/check-docs-sync.sh` now exits 0.
- Row 26 (`held-pwa-locales.sh`): resolved — the extraction now requires at least one key letter after the prefix (`grep -oE 'held_question_[a-z]+[a-z_]*'`), so the bare prefix in the `app.js` comment is no longer matched; the script exits 0.
- Row 32 (live-check check 6): the routing bug is fixed in this branch — `heldAnswerAlreadyRecorded` now matches only the routed record (the entry that carries a `Result`), while the submit-time question-change-hook entry never blocks the first routing; re-routing stays idempotent through the routed record. Regression test `TestHeldQuestionAnswerRoutedDespiteSubmitHook` wires the daemon's change hook exactly as `cmd/datawatch/main.go` does. `go test ./internal/autonomous/ -count=1` passes. The live re-run of the check 6 answer step on a rebuilt daemon is still pending; until it passes, the held-question rows in `docs/testing-tracker.md` keep Validated = No for the answer-routing path.
