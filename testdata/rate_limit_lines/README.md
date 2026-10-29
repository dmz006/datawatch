# `testdata/rate_limit_lines/` — observed per-backend rate-limit lines

Real, verbatim error lines each allowed backend prints when throttled, captured
**from in-tree sources only** (release notes, `CHANGELOG.md`, session tests with
operator-repro comments, testdata). Every line below was extracted by copying
the exact source text; none was invented or paraphrased. If no in-tree source
shows a real line for an allowed backend, **no fixture is created** and the row
below records the absence and why.

These fixtures are the **source of truth** for the built-in per-backend table in
`internal/session/rate_limit_lines.go`: `builtinRateLimitPhrases` may only
contain phrases that appear verbatim (case-insensitively) in a line recorded
here, and each recorded line must be detected by the anchored matcher for its
family. `internal/session/rate_limit_lines_test.go::TestBuiltinRateLimitFixtureDetection`
enforces both directions — every fixture line is detected by
`matchesBuiltinRateLimitLines`, and every table phrase occurs verbatim in at
least one recorded line. (Most phrases *lead* their line; `out of extra
usage` occurs inside the `You're out of extra usage …` line and leads lines
that start directly with it, e.g. `Out of extra usage · resets …`.)

Byte notes: files end with a single trailing `\n` (POSIX text fixture). `⎿` is
U+237F (`e2 9f bf`); `·` is U+00B7 (`c2 b7`); `∙` is U+22C5 (`e2 88 c5`).
Apostrophes in the in-tree sources are plain ASCII `'`; the detection table is
byte-exact otherwise (matches are case-insensitive but no glyph is
normalized), so a fixture and its table phrase must carry the same glyph.

## Fixtures

| # | Fixture | Verbatim line | Source (file:line) | Provenance |
|---|---|---|---|---|
| 1 | `claude-code.txt` | `⎿  You've hit your limit · resets 10pm (America/New_York)` | `internal/session/ratelimit_parser_test.go:87` (BL185 comment: "BL185 — operator repro 2026-04-26", comment block 85–91); identical fixture text at `internal/session/ratelimit_parser_test.go:94` | Operator-reported claude-code rate-limit pane line (BL185). Includes the TUI output gutter glyph `⎿` + two spaces that claude-code prints before secondary lines, plus the U+00B7 middle dot. Extracted verbatim from line 87. |
| 2 | `claude-code.txt` | `5-hour limit reached ∙ resets 2pm` | `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.27.4.md:41` (section "Modern claude-code rate-limit pattern fix"; the `∙` is U+22C5 as printed there) | Operator-reported claude-code "5-hour" dialog from the v5.27.4 modern-phasings fix ("session limit reach filter and auto scheduled continue command doesn't work"). Extracted verbatim from the release note's quoted line. |
| 3 | `claude-code.txt` | `Weekly limit reached` | `RELEASE-NOTES-v5.27.4.md:42` (same section) | Same incident: weekly-limit dialog phrasing that the v5.27.3-and-earlier pattern list did not cover. |
| 4 | `claude-code.txt` | `Hit weekly limit` | `RELEASE-NOTES-v5.27.4.md:43` (same section) | Same incident. |
| 5 | `claude-code.txt` | `Opus limit reached` | `RELEASE-NOTES-v5.27.4.md:44` (same section, printed as "Opus limit reached" / "Sonnet limit reached") | Same incident: model-specific limit phrasings. |
| 6 | `claude-code.txt` | `Sonnet limit reached` | `RELEASE-NOTES-v5.27.4.md:44` (same section) | Same incident: model-specific limit phrasings. |
| 7 | `claude-code.txt` | `You're out of extra usage · resets 11:50am (America/New_York)` | `CHANGELOG.md:14569` (BL262 operator-reported line); also `docs/plans/historical-releasenotes/RELEASE-NOTES-v6.11.2.md:56` and `RELEASE-NOTES-v6.11.3.md:14`; `internal/session/ratelimit_parser_test.go:113-128` includes the `⎿`-prefixed form | BL262 extra-usage exhaustion line. |
| 8 | `claude-code.txt` | `Approaching usage limit` | pre-change tree (merge-base `4f7d91309`): `internal/session/v5274_ratelimit_modern_test.go:58` (the old suite asserted it as a true detection); `internal/session/manager.go:146` (old list entry, WARN-tier dialog) | claude-code usage-dialog WARN line, restored after the anchored matcher first dropped it. Test string reused verbatim. |
| 9 | `claude-code.txt` | `Your usage limit will reset at 9pm (America/Los_Angeles).` | pre-change `internal/session/ratelimit_parser_test.go:27`; `manager.go:6785` ("Claude prose" comment) | claude-code prose form of the reset announcement; also the leading-phrase source for the `usage limit will reset` entries. |
| 10 | `claude-code.txt` | `limit will reset at 9pm` | pre-change `internal/session/v5274_ratelimit_modern_test.go:59` (asserted true by the old suite) | Old test string, reused verbatim as the leading-phrase source for `limit will reset at`. |
| 11 | `claude-code.txt` | `Weekly usage limit reached. Resets in 2 days.` | pre-change `internal/session/v5274_ratelimit_modern_test.go:53` (asserted true by the old suite) and `:100` | Old test string for the `weekly usage limit` entry. |
| 12 | `claude-code.txt` | `❯ 1. Stop and wait for limit to reset` | pre-change `internal/session/self_update_dialog_test.go:85`; `manager.go:7667` (auto-accept comment) | Option 1 of the claude-code usage dialog, with the `❯` (U+276F) cursor row; anchored through the family's menu-row prefixes. |
| 13 | `claude-code.txt` | `wait for limit to reset` | pre-change `manager.go:166` (old list entry, commented "Option 1 text — appears when dialog auto-wraps to next line"); `RELEASE-NOTES-v6.2.0.md:121` (BL240) | The wrapped continuation of option 1 (line 12). The text is the old list entry itself; no wider capture exists in-tree. |
| 14 | `claude-code.txt` | `Resets at 2pm` | pre-change `manager.go:164` (old list entry, commented `"Resets at 2pm" — reset time announcement`); `RELEASE-NOTES-v5.27.6.md` (BL215 dialog text) | Reset-time announcement line of the dialog. |

### Secondary in-tree references (corroboration, not fixtures)

- `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.27.6.md` (BL215 root-cause
  text) — the ~600-char modern dialog `5-hour limit reached. Use the limit reset
  time to plan... Resets at 2pm.` (quoted with an ellipsis in the note, so not
  suitable as a verbatim fixture; the "5-hour limit reached" leading phrase is
  covered by line 2).
- `RELEASE-NOTES-v5.27.4.md:46` — states the legacy patterns were
  `"You've hit your limit"` / `"rate limit exceeded"`: both are recorded in the
  release-note text as the *old* pattern strings, not as emitted lines, so they
  are not fixture lines.

## Backends with no real line in-tree (no fixture created)

| Backend | Why no fixture |
|---|---|
| `pi-prompt` | No in-tree source (release notes, CHANGELOG, testdata, session tests with operator-repro comments, `templates/session-CLAUDE.md`) quotes a verbatim throttle line the pi tool prints. `testdata/pi/session-sample.jsonl` contains no rate-limit event. `templates/session-CLAUDE.md` only defines the datawatch protocol marker (`DATAWATCH_RATE_LIMITED:`, `DATAWATCH_NEEDS_INPUT:`) that *any* agent is asked to emit — that is a protocol convention, not a line the backend itself prints. |
| `opencode` / `opencode-acp` / `opencode-prompt` | No in-tree source quotes a verbatim opencode rate-limit line. The only opencode-specific lines recorded in-tree (`CHANGELOG.md:7599` "SSE read timed out"; `CHANGELOG.md:7940` `"SSE Timeout"`, `"SSE error"`, `"failed to connect to ollama"`, etc.) are network/SSE stall diagnostics, not throttle messages, so they do not belong in `rate_limit_lines/`. |
| `ollama` | No in-tree source quotes a verbatim Ollama (or local-model provider) throttle line Ollama prints in a datawatch pane. Ollama's throttle symptoms in the release notes ("slot full", disconnected streams) are described in prose, never quoted as an emitted line, and Ollama serves local models — it rarely rate-limits at all. |

For these backends the only built-in detection is the shared protocol marker
`DATAWATCH_RATE_LIMITED:` (line-start, every backend); operator-configured
`detection.rate_limit_patterns` (flat substring, all backends) is the escape
hatch.

## What the detector's normalisation does to a fixture line

Pipeline for each line in `processOutputLine` (`internal/session/manager.go`):

1. Trailing CR/LF removal and `StripANSI`: cursor-column/cursor-position/
   cursor-back escapes become spaces, every other escape is stripped; the
   leading `⎿` gutter and its two spaces survive (no other escape bytes in
   these fixtures).
2. **Trim + lowercase** — `lineLower := strings.ToLower(strings.TrimSpace(line))`:
   surrounding whitespace is removed before any match, so an indented pane line
   still anchors at its first non-blank character.
3. **Matching (`internal/session/rate_limit_lines.go`)** — line-start anchored,
   case-insensitive, per-backend: the protocol marker (every backend) or a
   phrase from the family's table, optionally after one of the family's
   documented prefixes (the `⎿  ` gutter and the `❯ 1. ` / `1. ` menu rows). A line that merely quotes a phrase mid-sentence never matches.
4. **Length gate** — table-phrase matching only runs on lines with
   `len(line) < 2048`; the `DATAWATCH_RATE_LIMITED:` protocol marker always
   matches regardless.
