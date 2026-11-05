# C1410001 final gate — documentation close-out

Date: 2026-11-05. All checks run in this worktree with `TMUX`/`TMUX_TMPDIR` unset for the test commands, no daemon started, no model pulled, no version bumped, no behavior changed (documentation + one verification script only; the two pre-existing web test failures below predate this task — `git status` shows no `internal/` file touched).

## Checks actually run

| Check | Result |
|---|---|
| `make check-leaks` | PASS — 0 hits in all four classes (address 0, home 0, email 0, local 0; local-pattern class skipped, no `.leak-patterns.local`) |
| `make lint` (8 audits + golangci-lint) | PASS — unscoped-tmux kill, test-daemon cleanup, production-service-control, Go toolchain pins (`>= 1.26.10`), internal refs, identity wipe, generated-ignore files, leaks: all OK; `golangci-lint run ./...` = **0 issues** |
| `go test ./... -count=1` (full suite, `env -u TMUX -u TMUX_TMPDIR`) | PASS — **88 packages ok, 0 failed** (198s); feature-touched packages re-run `-v`: **229 tests pass, 0 fail** (config 10, autonomous 143, server 52, mcp 12, cmd 3, router 9) |
| Full node suite (`node --test` in `internal/server/web`) | **FAIL — 2028 pass, 2 fail.** Both failures are `app-requirements-report.test.js`: "a satisfied report shows the three parts…" and "missing items are listed; an empty report still renders its parts". Both assert the card renders `Automation backends` / `No Automation backend minimums in the report yet.`, while the card renders `Requirement backends` / `No requirement backend minimums in the report yet.` A wording-switch mismatch between the pre-existing test and the pre-existing card, in two files neither of which this task touches. |
| Howto coverage audit (`scripts/check-howto-coverage.sh`) | PASS — every `docs/howto/*.md` has `exec_steps` or is LLM-only |
| `.datawatch/verify/review-fix-docs.sh` | PASS — **34/34 doc-entry checks**: config-reference (2 key families), implementation (section, 3 core files, 3 routes), mcp (4 tools), cursor-mcp (4 tools), README (report + consent + howto link), operations (both subsections, both key families, verbatim refusal wording, complete-replace semantics), testing-tracker (both sections, real test names), both howto guides present |

## Documentation close-out (what this task did)

- `docs/config-reference.yaml` — `autonomous.requirements.min_ollama.<capability>` and `autonomous.model_pull_consent.models/publishers` documented with default-deny semantics, publisher matching (case-insensitive, `:tag`-ignoring), validation rules (a bad key rejects the whole PUT), all write surfaces, and a commented-out example policy. No live keys added.
- `docs/implementation.md` — new section "Automation requirements and the model pull gate": capability table, Ollama minimum floors (built-in → operator override), report behavior (a report, not a gate; unparseable versions fail closed), the consent engine, the two pull paths that enforce it, every surface, and the test files.
- `docs/mcp.md` / `docs/cursor-mcp.md` — the four tools (`automation_requirements_report`, `automation_model_pull_consent_get`, `automation_model_pull_consent_set`, `compute_node_backend_versions`).
- `README.md` — "Choose where AI runs" covers the requirements report and the model-pull gate, with howto links.
- `docs/operations.md` — "Automation requirements report" and "Model pull consent" subsections: config keys, all write surfaces, the verbatim refusal wording, and the complete-replace read-modify-write discipline on every surface.
- `docs/testing-tracker.md` — two sections with the real test names and test commands per surface; live-validation left unmarked until the smoke task.

## Surface parity record (the 3 routes this feature added)

| # | REST route | MCP tool | CLI | comm verb |
|---|---|---|---|---|
| 1 | `GET /api/automation/requirements-report` | `automation_requirements_report` | `datawatch automation requirements-report` | `requirements-report` |
| 2 | `GET/PUT /api/automation/model-consent` (the set is a complete-replace; PUT audited as `model_pull_consent_changed`) | `automation_model_pull_consent_get` / `automation_model_pull_consent_set` | `datawatch automation model-consent get` / `set` | `model-pull-consent-get` / `model-pull-consent-set` |
| 3 | `GET /api/compute/nodes/{name}/versions` | `compute_node_backend_versions` | `datawatch compute node versions <name>` | `node-versions <name>` |

All three routes are present in the config-reference, implementation, mcp, cursor-mcp, operations and testing-tracker documentation above (verified by the 34-check script).

## Gate

- Task checks (all documentation close-out requirements + verify script + lint + leak + full Go suite + howto coverage): **PASS.**
- Full node suite: **2 of 2030 fail**, both pre-existing and unrelated to this task (wording mismatch in `app-requirements-report.test.js`; neither file is in this task's diff). Reported here rather than hidden — the earlier "counts-only" gate claimed a clean node run in that file, which the run does not support.
