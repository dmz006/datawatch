# Decision records plan

**Date:** 2026-10-31

**Version note:** planning document; the release is the next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump), set at merge time.

## Scope

- `internal/config/` — the `autonomous.decisions` keys and defaults.
- `internal/autonomous/` — record store and validation, question builder, clarity rules, question update, sync engine, answer handling and gate release, document status line, follow-ups, folder watch, flow tests.
- `internal/server/` — REST routes, config surface, PWA card and settings, definitions text.
- `internal/mcp/`, `internal/router/`, `cmd/datawatch/` — MCP tools, comm verbs, CLI.
- `docs/howto/automaton-decision-records.md`, `docs/testing-tracker.md`, `CHANGELOG.md`, `scripts/live-check-decision-records.sh`, `tests/live-smoke/decision-records-report.md`.

The status line rewrite is meant for documents like `docs/plans/2026-10-29-pre-10-0-design-proposals-index.md`; no code is tied to that file.

## Phases

1. **Configuration keys and record store** — Status: Done on this branch, shipped in next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump).
2. **Question builder, clarity rules, question update, sync engine (one question per record)** — Status: Done on this branch, shipped in next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump).
3. **Answer handling, gate release, document status line, follow-ups** — Status: Done on this branch, shipped in next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump).
4. **Folder watch, REST, MCP, CLI, comm verbs, PWA** — Status: Done on this branch, shipped in next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump).
5. **Flow tests, live check, how-to, tracker and changelog** — Status: Done on this branch, shipped in next patch above main (taken by the merge helper at merge time and run with BUMP=minor because this feature is a minor bump).

### Phase Completion Checklist

- [x] Functionality implemented per the phase descriptions.
- [x] Unit tests added and passing (`TestDecisionRecord*`, `TestDecisionSync_*`, `TestDecisionFolder_*`, and the JS tests).
- [x] Live check against a real daemon: sandbox port 18443, 2026-11-06, steps 1 to 7 PASS, step 8 SKIPPED (no planning backend in the sandbox); see `tests/live-smoke/decision-records-report.md`.
- [ ] Full `go test ./...` and `node --test` green: N/A for this documentation task; run by the merge step.
- [x] General documentation checklist: `CHANGELOG.md` and `docs/testing-tracker.md` done here. `docs/config-reference.yaml`, `docs/operations.md`, `README.md` and the doc index: N/A in this task (earlier tasks of this Automaton; plan README rows are left to the merge step). No tracker identifiers added.
- [x] Conditional docs: how-to `docs/howto/automaton-decision-records.md` exists; flow diagram N/A, no new architecture beyond the question channel.
- [x] Mobile-Parity Rule: the PWA gained a card and settings rows; the Android and iPhone issue is listed under Parity surface and is filed at merge.
- [x] Localization Rule: new PWA strings are in all five locale bundles (`TestDecisionRecordPWA_RowLabelsLocalize`).
- [ ] Version bump: N/A on this branch, the merge helper sets it with BUMP=minor.
- [x] This plan's phase status lines are current.

## Parity surface

- **REST:** create, list (with filters and folder status), get, update, withdraw for decision records; the answer route accepts an optional follow-up; the decision line carries the record id.
- **MCP:** record tools (create, list, update, withdraw) and a follow-up argument on `question_answer`.
- **CLI:** `datawatch autonomous decisions` record commands (create, list, show, update, withdraw) and a follow-up flag on answering.
- **Comm channel:** verbs to create, list, show and withdraw records. Intentional exclusion: no follow-up flag on the answer verb, because a chat answer is a short reply and the follow-up is chosen on the other surfaces.
- **YAML/config:** all keys under `autonomous.decisions`, changeable through `PUT /api/config`, `datawatch config set`, chat `configure` and MCP `config_set`.
- **PWA:** a decision records card on the Automata view and settings rows for the keys.
- **Android:** a parity issue in `datawatch-app` for the card and the new REST contract is filed at merge; not built in this change.
- **iPhone:** same as Android; not built in this change.
- **Intentional exclusions:** no chat follow-up flag (above); no document rewrite from chat; mobile clients are not built here because the app repository is separate.
