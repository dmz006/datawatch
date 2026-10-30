# Operator question clarity — final gate report

Date: 2026-10-30

Scope: the change that makes operator questions easier to answer — a visible
trade-off line on every option, a context link in the answer dialog, a short
preview of each open question in the queue, and three quality modes (`off`,
`warn`, `refuse`) that check a question when it is raised.

## Result

PASS

## What the gate checks

| Check | Result |
|---|---|
| The change set includes this report | Pass |
| `gofmt` over the changed Go files only | Pass |
| `go build` of `./cmd/datawatch/` | Pass |
| Package tests: `internal/autonomous`, `internal/server`, `internal/mcp`, `internal/router`, `internal/config`, `cmd/datawatch` | Pass |
| Node tests: every `internal/server/web/app-questions-*.test.js` file | Pass |
| `scripts/check-no-internal-refs.sh` (no internal tracker identifiers on operator-facing surfaces) | Pass |

## Coverage by surface

| Surface | Where it lives | Tests |
|---|---|---|
| Quality check and the three modes | `internal/autonomous` | `TestQuestionQuality_*` |
| Context reference (proposal, Automaton, task, registry row) | `internal/autonomous`, `internal/server` | `TestQuestionContextRef*` |
| REST: raise with trade-offs, quality read, `context_ref` on reads | `internal/server` | `TestQuestionQuality_*`, `TestQuestionContextRef*` |
| Config keys: `quality_mode`, `quality_min_text_chars`, `quality_tracker_id_pattern`, `queue_preview_lines` | `internal/config`, `internal/server` | `TestQuestionQuality_Config*` |
| MCP: trade-offs on `question_raise`, the `question_quality` tool | `internal/mcp` | `TestQuestionRaiseTradeoffs_MCP`, `TestQuestionQuality_MCP` |
| CLI: `--tradeoff`, `question quality`, answer help | `cmd/datawatch` | `TestQuestionRaiseTradeoff`, `TestQuestionQuality_CLI`, `TestQuestionAnswerHelp`, `TestQuestionGroupHasTheQualityCommand` |
| Chat: quality verb, answer wording, trade-offs on show | `internal/router` | `TestQuestionQuality_Comm`, `TestQuestionAnswerHelp_Comm`, `TestQuestionShowTradeoffs_Comm` |
| PWA: answer dialog | `internal/server/web` | `app-questions-clarity.test.js`, `app-questions-context-link.test.js` |
| PWA: queue preview | `internal/server/web` | `app-questions-queue-preview.test.js` |

## Documentation

- `docs/testing-tracker.md` has a row for each test family above, with the exact
  test names so each row can be found by name.
- `CHANGELOG.md` has a plain-language entry in the unreleased section.
- `docs/parity-status.md` has the surface rows; the mobile row is pending its
  issue number.
- `docs/howto/operator-questions-answering.md` and `docs/datawatch-definitions.md`
  describe the new behaviour and settings.

## Not done here

- The version variables are not touched; the minor bump happens at merge time.
- No live daemon check was run; every row in the tracker says so.
