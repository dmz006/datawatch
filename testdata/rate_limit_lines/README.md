# `testdata/rate_limit_lines/` — observed per-backend rate-limit lines (BL646 data)

Real, verbatim error lines each allowed backend prints when throttled, captured
**from in-tree sources only** (release notes, `CHANGELOG.md`, session tests with
operator-repro comments, testdata). Every fixture below was extracted by copying
the exact source text; none was invented or paraphrased. If no in-tree source
shows a real line for an allowed backend, **no fixture is created** and the row
below records the absence and why.

Byte notes: files end with a single trailing `\n` (POSIX text fixture). `⎿` is
U+237F (`e2 8e bf`), `·` is U+00B7 (`c2 b7`), `'` is U+2019 (`e2 80 99`) — the
apostrophe is the typographic one, not ASCII `'`.

## Fixtures

| Fixture | Verbatim line | Source (file:line) | Provenance |
|---|---|---|---|
| `claude-code.txt` | `⎿  You've hit your limit · resets 10pm (America/New_York)` | `internal/session/ratelimit_parser_test.go:87` (BL185 comment: "BL185 — operator repro 2026-04-26", comment block 85–91); identical fixture text at `internal/session/ratelimit_parser_test.go:94` | Operator-reported claude-code rate-limit pane line (BL185). Includes the TUI output gutter glyph `⎿` + two spaces that claude-code prints before secondary lines, plus the U+00B7 middle dot. Extracted verbatim from line 87. |

### Secondary in-tree references for the claude-code line (not fixtures, for corroboration)

- `CHANGELOG.md:14569` — BL262 operator-reported line
  `You're out of extra usage · resets 11:50am (America/New_York)`.
- `docs/plans/historical-releasenotes/RELEASE-NOTES-v6.11.2.md:56` and
  `RELEASE-NOTES-v6.11.3.md:14` — same BL262 line; v6.11.3:26 notes a `⎿`-prefix variant.
- `internal/session/ratelimit_parser_test.go:113-128` — BL262 fixtures incl. the `⎿`-prefixed form.
- `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.27.6.md` (BL215 root-cause text) — the
  ~600-char modern dialog `5-hour limit reached. Use the limit reset time to plan... Resets at 2pm.`
  (quoted with an ellipsis in the note, so not suitable as a verbatim fixture).

## Backends with no real line in-tree (no fixture created)

| Backend | Why no fixture |
|---|---|
| `pi-prompt.txt` | No in-tree source (release notes, CHANGELOG, testdata, session tests with operator-repro comments, `templates/session-CLAUDE.md`) quotes a verbatim throttle line the pi tool prints. `testdata/pi/session-sample.jsonl` contains no rate-limit event. `templates/session-CLAUDE.md` only defines the datawatch protocol marker (`DATAWATCH_RATE_LIMITED:`, `DATAWATCH_NEEDS_INPUT:`) that *any* agent is asked to emit — that is a protocol convention, not a line the backend itself prints. |
| `opencode.txt` / `opencode-acp.txt` / `opencode-prompt.txt` | No in-tree source quotes a verbatim opencode rate-limit line. The only opencode-specific lines recorded in-tree (`CHANGELOG.md:7599` "SSE read timed out"; `CHANGELOG.md:7940` `"SSE Timeout"`, `"SSE error"`, `"failed to connect to ollama"`, etc.) are network/SSE stall diagnostics, not throttle messages, so they do not belong in `rate_limit_lines/`. |
| `ollama.txt` | No in-tree source quotes a verbatim Ollama (or local-model provider) throttle line Ollama prints in a datawatch pane. Ollama's throttle symptoms in the release notes ("slot full", disconnected streams) are described in prose, never quoted as an emitted line, and Ollama serves local models — it rarely rate-limits at all. |

## Which backends the current pattern list targets

- `internal/session/manager.go:137` — the `rateLimitPatterns` hard-coded list.
- `internal/config/config.go:4060-4064` — `DefaultDetection().RateLimitPatterns`
  (`"DATAWATCH_RATE_LIMITED:"`, `"You've hit your limit"`, `"rate limit exceeded"`,
  `"quota exceeded"`), merged by `GetDetection` per backend.

The phrase set is written around the **claude-code** dialogs (per the comment at
`manager.go:141` and the BL185/BL215/BL248/BL262 history in `CHANGELOG.md:14569`, `CHANGELOG.md:15664`, release notes v5.27.4 (`RELEASE-NOTES-v5.27.4.md:48`) and v5.27.6): all of the above verbatim lines are claude-code. The generic
entries (`rate limit exceeded`, `quota exceeded`) are backend-agnostic and could
match *any* backend if it printed them, but the tree contains no recorded line
from any other backend that actually demonstrates that.

## What the detector's normalisation does to a fixture line

Pipeline for each line in `processOutputLine` (`internal/session/manager.go`):

1. **`manager.go:7462`** — `rawTrimmed := strings.TrimRight(rawLine, "\r\n")`:
   trailing CR/LF removed. (Does not touch leading bytes, so the `⎿` prefix survives.)
2. **`manager.go:7463`** — `line := StripANSI(rawTrimmed)` (`manager.go:108-133`):
   - cursor-forward `\x1b[<n>C` is **expanded** to `<n>` literal spaces (capped at 80);
   - cursor-column (`\x1b[<n>G`), cursor-position (`\x1b[r;cH`) and cursor-back
     (`\x1b[<n>D`) are each **replaced by one** space;
   - every other escape (OSC/DCS/PM/APC, any CSI, any two-byte ESC byte, DEC
     private-mode) is **stripped entirely (replaced with "")**.
   Our fixture contains no escape bytes, so this is a no-op for it.
3. **Matching (`manager.go:7629-7641`)** — there is **no `TrimSpace`** and **no
   glyph/indicator-stripping** in the rate-limit path (unlike the completion
   matcher, which strips leading non-ASCII indicator runes). `lineLower` is
   `strings.ToLower(line)` and each pattern is matched with a case-insensitive
   **substring** `strings.Contains`. So the detector sees the whole line including the
   `⎿` gutter and two leading spaces, and matches on the substring
   `you've hit your limit` from the pattern at `manager.go:139`
   ("You've hit your limit").
4. **Length gate (`manager.go:7632`)** — phrase matching only runs on lines with
   `len(line) < 2048` runes; the `DATAWATCH_RATE_LIMITED:` protocol marker always
   matches regardless (BL215 history: the old 200-char gate dropped ~600-char
   claude dialogs). The fixture line is 60 bytes — well under the gate.
