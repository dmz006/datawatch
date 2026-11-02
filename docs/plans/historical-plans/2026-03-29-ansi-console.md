---
date: 2026-03-29
version: 0.7.3
scope: Web UI terminal — replace raw output with xterm.js ANSI console
status: done
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

### Phase 5 — tmux Output Relay (Planned)

**Approach comparison for real-time ANSI output:**

| Approach | Latency | ANSI Support | CPU Cost | Complexity |
|----------|---------|-------------|----------|------------|
| **pipe-pane (current)** | ~0ms (stream) | Raw bytes including all ANSI | Low (kernel pipe) | Low |
| **capture-pane -p -e** | Polling interval (50-200ms) | Full ANSI with -e flag | Higher (exec per poll) | Medium |
| **PTY relay (socat/script)** | ~0ms (stream) | Full ANSI native | Low | High |
| **tmux control mode (-CC)** | ~0ms (stream) | Structured output, not raw ANSI | Medium | High |

**Recommendation: pipe-pane + xterm.js (hybrid)**

- **Keep `pipe-pane`** for real-time streaming (current approach, ~0ms latency)
- `pipe-pane` outputs the raw PTY byte stream including ALL ANSI sequences
- Feed the raw bytes directly to xterm.js via WebSocket — xterm.js handles ANSI natively
- This gives both real-time performance AND full ANSI rendering
- No polling needed — pipe-pane is a kernel-level stream
- The current `StripANSI` processing is bypassed; raw bytes go straight to xterm.js

**Why NOT capture-pane:**
- Requires periodic polling (exec `tmux capture-pane` every 50-200ms)
- Each poll is a full pane snapshot (~8KB for 220x50), not a diff
- Higher CPU cost for multiple sessions
- Adds latency equal to poll interval
- Only advantage: capture-pane gives a "rendered" snapshot (cursor positioning resolved)
  But xterm.js handles cursor positioning natively, so this is redundant.

**Why NOT PTY relay:**
- Maximum complexity (socat or custom PTY multiplexer)
- Marginal benefit over pipe-pane since both stream raw bytes
- pipe-pane is already built into tmux

**Implementation:**
- Keep `pipe-pane` for output capture (already working)
- New WebSocket message type: `terminal_data` (raw bytes, not line-based)
- Client: xterm.js `terminal.write(rawBytes)` instead of text append
- For encrypted mode: FIFO pipe still works, xterm.js renders decrypted bytes

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
