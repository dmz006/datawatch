# datawatch v5.26.10 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.9 → v5.26.10
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** Scroll-mode regression from v5.26.9 + smoke covers claude / opencode / ollama PRD CRUD.

## What's new

### Scroll mode is scrolling again

Operator: *"Scroll mode isn't scrolling now."*

v5.26.9's `if (state._scrollMode) break;` guard skipped pane_capture redraws while in scroll mode, but tmux copy-mode needs those redraws to surface scroll-back lines as the operator pages through. The original problem (constant flash from idle redraws) was the *content* not changing, not the scroll-mode flag.

v5.26.10 replaces the unconditional skip with a **content-aware dedupe**:

```js
const frameKey = capLines.join('\n');
if (frameKey === state._lastPaneFrame) break; // identical frame; skip flash
state._lastPaneFrame = frameKey;
state.terminal.write('\x1b[2J\x1b[3J\x1b[H' + capLines.join('\r\n'));
```

- Idle tmux + redrawing status bar = same content = skip = no flash.
- Operator scrolls in tmux copy-mode = content changes = redraw fires = new scroll-back lines render.
- `state._lastPaneFrame` cleared in `destroyXterm` so cross-session state doesn't leak.

Trade-off vs the v5.26.9 unconditional skip: a tmux that updates its status bar every second still triggers a redraw (because `[1s]` → `[2s]` is a content change). Acceptable — tmux statuses are usually configured to update every 15s, and the dedupe still covers most idle frames.

### `release-smoke.sh` covers every supported worker backend

Operator: *"And make sure smoke tests for prd work with claude, opencode and ollama."*

Section 6 of `scripts/release-smoke.sh` now iterates every worker backend whose `enabled && available` is true on the running daemon:

```
== 6.claude-code — CRUD ==
  PASS  [claude-code] create PRD
  PASS  [claude-code] PRD record has backend=claude-code
  PASS  [claude-code] GET /children empty list
  PASS  [claude-code] set_llm round-trip: backend=claude-code, model=claude-sonnet-4-5
  PASS  [claude-code] hard-delete PRD

== 6.opencode — CRUD ==
  ... (same five checks)

== 6.ollama — CRUD ==
  ... (same five checks; model=qwen3:8b)
```

The model name is per-backend (claude/opencode use claude-sonnet-4-5; ollama uses qwen3:8b). Backends that aren't `available` on the running daemon are silently skipped — the per-host smoke output will reflect what's actually exercised.

Initial run on the dev workstation: **25 PASS / 0 FAIL / 2 SKIP** (memory + orchestrator skipped; not enabled here).

## Configuration parity

No new config knob.

## Tests

- 1397 Go unit tests passing.
- **Functional smoke: 25 PASS / 0 FAIL / 2 SKIP** via `scripts/release-smoke.sh` against the local daemon.

## Known follow-ups

Same as v5.26.9 — v6.0 cumulative release notes + CI for `parent-full` + `agent-goose` + CI for `release-smoke.sh` against a kind cluster + GHCR past-minor cleanup run.

## Upgrade path

```bash
git pull
# Hard-refresh PWA tab once to pick up new SW cache + app.js.
# Restart the daemon: datawatch restart
# Try Scroll mode in tmux: ↑/↓/PgUp/PgDown should now actually update
# the scroll-back content as you page.
# Operators running their own smoke before tag:
#   ./scripts/release-smoke.sh
```
