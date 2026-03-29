---
date: 2026-03-29
version: 0.7.3
scope: Web UI terminal — replace raw output with xterm.js ANSI console
status: planned
---

# Plan: ANSI Console for Web UI

## Problem

TUI applications (claude-code, opencode) use alternate screen mode, cursor positioning, and 256-color ANSI sequences. The current web UI strips ANSI codes and shows raw text, making TUI output unreadable. Users must attach to tmux directly to see the real terminal.

## Scope

- `internal/server/web/` — app.js, style.css
- `internal/server/ws.go` — output broadcast
- `internal/server/web/` — new xterm.js assets
- `internal/session/tmux.go` — capture-pane alternative

## Phases

### Phase 1 — Integrate xterm.js (Planned)

- Bundle xterm.js + xterm-addon-fit into `internal/server/web/`
- Replace the `<div class="output-area">` with an xterm.js Terminal instance
- Feed output lines through xterm.js `write()` instead of `textContent`
- xterm.js handles all ANSI rendering natively

### Phase 2 — Terminal Sizing and Mobile (Planned)

- `xterm-addon-fit` auto-sizes terminal to container width
- Default font size fits a phone screen width (~80 cols at 10px)
- Font size selector in session detail header (S/M/L or slider)
- CSS: terminal fills available height between header and input bar
- Touch scrollback: swipe up/down to scroll history

### Phase 3 — Scroll and History (Planned)

- xterm.js scrollback buffer (default 5000 lines)
- Page-up/page-down keyboard support
- Mobile: scroll indicators on right edge
- "Jump to bottom" button when scrolled up

### Phase 4 — Bidirectional Terminal (Planned)

- xterm.js `onData` callback sends keystrokes to tmux via WebSocket
- New WS message type: `terminal_input` (raw bytes, no Enter appending)
- Server: `tmux send-keys -l` for literal key sequences
- Replaces the text input bar for TUI sessions (keep for non-TUI)

### Phase 5 — tmux capture-pane Integration (Planned)

- Replace `pipe-pane` with periodic `tmux capture-pane -p -e` (preserves ANSI)
- Or: use `pipe-pane` with `-I` flag for input relay
- Compare: direct PTY relay vs capture-pane snapshots

## Key Files

- `internal/server/web/xterm.min.js` — bundled xterm.js
- `internal/server/web/xterm-addon-fit.min.js` — fit addon
- `internal/server/web/app.js` — Terminal instance, output routing
- `internal/server/web/style.css` — terminal container sizing
- `internal/server/ws.go` — terminal_input message type
- `internal/session/tmux.go` — capture-pane or PTY relay

## Dependencies

- `xterm.js` v5.x (MIT license, ~300KB minified)
- `xterm-addon-fit` (auto-sizing)

## Estimated Effort

2-3 weeks. Phase 1-2 can be delivered independently.
