# datawatch v5.26.15 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.14 → v5.26.15
**Patch release** (no binaries — operator directive).
**Closed:** Response capture filters out animation spinners + TUI status decoration.

## What's new

### Cleaner response viewer output

Operator: *"Response capture should filter out animation spinning icon and things not text or useful to read."*

The 📄 Response button on the tmux toolbar surfaces `Session.LastResponse`, which falls back to the last 30 tmux lines when claude's `/copy` shortcut hasn't written `/tmp/claude/response.md`. Those 30 lines often contain TUI animation glyphs, status timers, and footer hints — the operator only wants the actual response prose.

`Manager.CaptureResponse` now runs the tail through `stripResponseNoise` before returning. Filtered:

| Pattern | Examples | Why filtered |
|---------|----------|--------------|
| Single-glyph spinners | `●` `✢` `✶` `✻` `✽` `⠋…⠏` `❯` | Animation noise |
| Box drawing | `╭` `╰` `│` `─` | TUI border decoration |
| Status timer | `(7s · timeout 1m)`, `(5s)` | Live tick noise |
| Footer hints | `esc to interrupt`, `shift+tab to cycle`, `↑↓ to navigate`, `enter to confirm` | claude-code TUI footer |
| Mode markers | `bypass permissions`, `loading…`, `thinking…` | Status decoration |

Preserved:

- Bullet-list lines (`* first finding`, `* second finding`) — only PURE-spinner lines (exactly `*`) get filtered.
- All real prose with `Error:`, technical content, code blocks, etc.
- Paragraph breaks (single blank lines) — three-or-more consecutive blanks collapse to one.

ANSI escape codes are stripped *before* the noise-pattern match so colored output doesn't shield markers.

## Configuration parity

No new config knob.

## Tests

- 1397 → 1403 Go unit tests passing (6 new in `response_filter_test.go`).
- Functional smoke unaffected: 29 PASS / 0 FAIL / 2 SKIP.

## Known follow-ups

- **PRD project_profile + cluster_profile support** (operator-asked
  later in same session): "Prd should be based on directory or
  profile, should be able to check out repo and do work" + "Prd
  should also support using cluster profiles" + "and smoke tests
  tests include those". v5.26.16 design — needs PRD model
  schema additions, executor branching to /api/agents, PWA modal
  dropdown for profile pickers, and smoke coverage for both paths.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-refresh PWA tab once for the new SW cache.
# Click 📄 Response on a running session — the captured text no
# longer ends with claude's spinner glyph + "(7s · timeout 1m)"
# fragment.
```
