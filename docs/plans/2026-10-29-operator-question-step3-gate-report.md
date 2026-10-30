# Guardrail gate report (rules-review) — operator-question Step 3

Date: 2026-10-29
Scope guardrail: "rules-review" over the operator-question channel work — every question raised on the Automaton, and every surface an answer can come from (see `docs/plans/parity-issues/operator-question-step3-surfaces.md`).
Input: the worktree at `origin/main` (v9.33.0) plus this iteration's step (the answering surface): `app.js` + `app.css`, the five `i18n_*.json` files and `questions_locale_test.go` (the locale guard), `internal/mcp/questions_tools.go` (+ test), `cmd/datawatch/cli_questions.go` (+ test), `internal/server/web/automation-question-stories.js` (the PWA browser story) + `internal/server/web/test-sandbox.js` (the story suite), `docs/howto/operator-questions-answering.md` + `docs/howto/operator-questions-notifications.md`, `docs/plans/2026-10-29-operator-question-step3-research.md`, the Step 3 section of `docs/testing-tracker.md`, the Unreleased section of `CHANGELOG.md` and the Current release section of `README.md`.

**Guardrail token (for the final commit message):** `rules: Configuration-Accessibility (B6), Monitoring-And-Observability (B7), Reuse-And-Expand-Audit (B9), Skills-Hook-Audit (B10), Mobile-Parity (B4), Locale-Guard (B3), Terminology, General-Doc-Checklist item 4 (README)` · `changelog: added` · `readme: updated (Current release)` · `tracker: "Operator question — answering it (Step 3)" (docs/testing-tracker.md)` · `b4: datawatch-app#283 filed and verified open (queue view, answer sheet, settings toggle — this iteration completes it)` · `b6: queue-card disabled state translated in all five locales (settings_automata_questions_enabled + the card strings, non-empty, no PRD-word or Automation-word in any user-visible label, round-trip, app.js presence)` · `b7: monitoring N/A per item with reasons (this step is the answering surface on top of the notification primitives — no new subsystem to gauge; the queue and every state change stay visible through GET /api/questions and the audit log, per the B7 note in the Step 3 section of docs/testing-tracker.md)` · `b9: reuse-audit — the card and dialog reuse the Automation view's existing loader, i18n and poll machinery; the PWA story reuses the browser-story harness and the test-sandbox; no second store or REST shape` · `b10: skills-hook: N/A (no session/Automaton/agent/comm-channel/plugin path added)` · `id-check: clean` · `leak-check: clean` · `make lint: pass (golangci + web lint, both clean)` · `test: pass (go test ./... + node --test ./internal/server/web/*.test.js, both clean)` · `version: 9.33.2 (both files)`.

## Surfaces shipped (the exact names, cross-checked against the code)

- **REST (the one surface):** `POST /api/questions`, `GET /api/questions` (filters `state`, `context`, `asker`, `text`, `limit`), `GET /api/questions/{id}`, `POST /api/questions/{id}/answer`, `POST /api/questions/{id}/withdraw`, and `GET /api/questions/{id}/answers` (the answer log). All of them refuse with a 503 that names `autonomous.questions.enabled` while the channel switch is off.
- **MCP:** `question_raise`, `question_list`, `question_get`, `question_answer`, `question_withdraw` — one per REST route, so a remote client answers without the CLI or a browser. Raises behind `autonomous:write`, reads behind `autonomous:read`.
- **CLI (`datawatch question <subcommand>`):** `list [--state <state>] [--context <ctx>] [--asker <who>] [--text <search>] [--limit n] [--json]`, `show <id>`, `answer <id> (--option <option id> and/or --text <verbatim words>) [--supersedes <answer id>]`, `withdraw <id> --reason <text>`, `raise --text <text> --option id=label [--option id=label …] [--default <option id or hold>]`.
- **comm channel:** `question list`, `question show <id>`, `question answer <id> <option number or id> [quote ...]` — plus the bare-reply rule (when exactly one question is open, a plain reply whose first word is one of its option numbers answers it; with two or more open, the reply is refused and the open questions are listed; with none open, it is a normal message). No raise or withdraw comm verb yet — those stay on the other surfaces.
- **PWA:** the Automation view's question queue card (open questions in daemon order, with the high-priority marker, and the translated disabled state while the switch is off), the question dialog (options, recommendation with reasoning, default, free-text box), and the history list with each question's answer log — all in the five locales the PWA ships.
- **config surface:** `autonomous.questions.enabled` plus `notify_messaging`, `notify_alert` and `return_path` — settable from the PWA Settings toggle, REST config, MCP `config_set`, `datawatch config set`, and the comm channel; flipping the channel on is live on the next request, no restart.

## Surfaces skipped by design, and why

- **Android / iPhone app screens** — the PWA queue card and answering dialog cover the mobile surface; `datawatch-app#283` (filed and verified open) is already tracking the native app side, so no separate parity item here.
- **A dedicated stats surface** — n/a per item, with reasons: see the Step 3 section of `docs/testing-tracker.md` (the queue remains fully queryable through the existing `GET /api/questions` and the audit log; no new `SystemStats` field, stats tool, Monitor card or Prometheus counter is warranted).

## Docs guard

- `docs/howto/operator-questions-answering.md` (this surface, end to end; both it and the notifications howto are indexed in `docs/README.md` and `docs/howto/README.md`).
- `docs/howto/operator-questions-notifications.md` (Step 2's announce legs).
- `CHANGELOG.md` Unreleased — the answering surface, verbatim quote, the history and the answer log, the CLI/MCP/comm wording, all six routes, the browser story and the guardrail token.
- `README.md` — the Current release line (the v9.34.0 incoming entry) and the step-by-step walkthrough with the real names above.
- `docs/operations.md` → "Operator questions on the Automaton" — the surfaces table, the answer log, and the bare-reply rule.
- `docs/testing-tracker.md` — the Step 3 section (queue card / dialog / history live check, the browser story, the CLI and MCP round trips, the locale guard, and the B7 n/a note with reasons).
- `docs/datawatch-definitions.md` — **Question**, **Answer** and **Answer log** entries.
- `docs/mcp.md` — the five `question_*` tools, each with its REST proxy, its parameters and the 503-off behaviour.
- `docs/commands.md` — the `datawatch question` CLI group and the five `question_*` MCP tools alongside the other autonomous surfaces.
- `docs/cursor-mcp.md` — the five tools in the selected table.
- Lint: `make lint` and `golangci-lint run` clean, including the question-tool, CLI and story files touched this iteration.
- Tests: `go test ./...` and `node --test ./internal/server/web/*.test.js` both clean, including the new locale guard.
- PWA browser story: the answering path (switch-off disabled state, queue card, option / free-form answer, supersession) end to end with headless Chromium; recorded in the testing tracker.

## Verdict

**pass (11/11)** — all eleven rules-review items are closed above; no remaining item is "N/A without saying why" and no user-visible copy carries the word "PRD" or the word "Automation".
