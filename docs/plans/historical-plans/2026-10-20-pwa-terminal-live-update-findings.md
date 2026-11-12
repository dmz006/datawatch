# PWA terminal does not live-update after sending input: findings

Date: 2026-10-20. Investigation only; no fix is written here. The fix ships in the
next patch above main.

Method: code reading (ws.go, api.go WebSocket handler, session manager, tmux
wrapper, app.js, sw.js) plus throwaway tmux experiments on a private socket
(`env -u TMUX -u TMUX_TMPDIR tmux -S <private socket>`, killed by name, no
kill-session -a, production tmux and daemon untouched). The daemon and the
operator's browser were not inspected, so the root cause below is established
from code and measurements, not from a live reproduction.

## How the live view actually works

The terminal display does not read output.log. `subscribe`
(api.go:2245) starts `StartScreenCapture` (manager.go:2239), a 200 ms ticker per
(client, session) that runs `tmux capture-pane` and, only when the text changed
(manager.go:2280), broadcasts a `pane_capture` frame to every connected client
through `Hub.BroadcastPaneCapture` (ws_output_scope.go:126). The PWA writes that
frame into xterm.js (app.js:860-950). output.log feeds state detection,
summaries and the first `output` frame only.

## Root cause

The PWA's `pane_capture` handler discards frames and never asks for them again,
while the server only sends a frame when the pane text changes. A discarded frame
therefore stays discarded until the next unrelated change.

1. Leading-edge throttle with no trailing flush, app.js:874
   (`if (state._lastPaneWrite && (now - state._lastPaneWrite) < 33) break;`).
   Any frame arriving less than 33 ms after the previous one is dropped. The
   server's `lastCapture` compare (manager.go:2280) means the dropped frame is not
   resent. Typing echo plus the TUI's follow-up redraw is exactly the burst that
   produces two changes close together, and every client viewing the same session
   adds its own capture goroutine at its own phase, all broadcast to all clients,
   so near-simultaneous frames are routine. After the last frame of a burst is
   dropped the screen shows the previous state until something else changes,
   which for an idle prompt is never.
2. Live updates are suppressed while the xterm viewport is above the bottom,
   app.js:935 (`if (buf && buf.viewportY < buf.baseY) break;`). There is no
   indicator and no auto-return. One stray wheel or touch scroll freezes the live
   view for good, and a wide, tall pane (333x89) with a 5000-line scrollback makes
   an accidental scroll more likely.
3. No recovery path once the first frame has arrived. The only retry loop is the
   connect watchdog (app.js:3716-3735, 4-5 s timeout, 2-3 retries) and it stops
   when `_termHasContent` is set. The "5 second fallback poll" does not exist for
   terminal content: the only 5000 ms intervals in the session view are the stats
   polls (app.js:4258, 4661, 4887). The stale-socket watchdog
   (app.js:390-419, tested in app-ws-watchdog.test.js) detects a dead TCP
   connection after 50-70 s of silence, not a live socket that has stopped
   delivering useful frames.

The chosen fix, to be implemented in the next patch above main:

- Client: replace the leading-edge drop with a trailing-edge throttle. Keep the
  newest dropped frame in `state._pendingPaneFrame` and flush it from a timer so
  the final frame of every burst is drawn. Replace the `viewportY < baseY` break
  with an explicit "scrolled up, jump to live" control that stays visible, and
  scroll to the bottom on `send_input`.
- Server: add a keyframe. The capture goroutine re-sends the current pane
  every 2 s even when unchanged, so any dropped frame heals without client
  logic. Cost is small (see numbers below) and only applies to subscribed
  sessions.
- Client: if no `pane_capture` has arrived for N seconds on an active session
  view, re-send `subscribe` (extend the connect watchdog past first content).
- Add a unit test next to app-ws-watchdog.test.js that feeds two frames 10 ms
  apart and asserts the second is drawn.

Contributing causes that should ship in the same change are listed under
"Confirmed contributing leads" below.

## Lead-by-lead results

### (1) WebSocket output path

- Frame subscription: confirmed working, with a gap. `subscribe` is handled
  inline in the read loop (api.go:2245-2312) and starts a per-client capture.
  The PWA never sends `unsubscribe` and never sends `hello`
  (no match in app.js), so it stays in the default output scope `all`
  (ws_output_scope.go, "The PWA relies on this"). Result: every session the
  operator has opened since the socket connected keeps a 200 ms capture goroutine
  (cancelled only on disconnect), and every resulting frame is broadcast to every
  client whether or not that session is open. Frames for sessions that are not on
  screen are discarded by the handler (app.js:870 guard) after being sent.
- Slow-client behaviour: confirmed amplifier. Each client has a 2048-message send
  buffer (api.go:2073). Frames queue in order, so a phone on a weak link sees the
  frame for the session it is looking at behind a backlog of frames for other
  sessions (added latency). When the buffer fills, the hub closes the client
  (ws.go:243) and the PWA reconnects. That heals, with flicker, but is not a
  steady-state design.
- Subscribe re-sent after reconnect or daemon restart: confirmed OK. The `open`
  handler refreshes `/api/sessions` and calls `renderSessionDetail`
  (app.js:578-650), which sends `subscribe` (app.js:3239) on both the success and
  failure path. The reconnect backoff caps at 30 s (app.js:670-676).
- Stale-socket watchdog: works as designed (see above) and is not the cause.
- Fallback poll: does not exist for terminal content (see Root cause point 3).
- Read-loop blocking: confirmed contributing. `subscribe` and `resize_term` run
  inline in the single read loop, so a slow `tmux capture-pane -S -` delays
  later messages from that client, including `send_input`.

### (2) Very large output.log

Ruled out as the cause of the missing live frames. Evidence:

- The live display does not touch output.log (above).
- `TailOutput` (manager.go:3873) seeks to the last 64 KB for plaintext logs
  (`tailBytes`, manager.go:~3908-3920), so a 560 MB log costs one 64 KB read per
  `subscribe` and per `/api/output` call (api.go:1774). The `n` argument is only
  used after the 64 KB read.
- `monitorOutput` seeks to the end of the file (manager.go:5799) and follows it.
- Remaining whole-file reads are in the encrypted-log branches of `TailOutput`
  (manager.go:3890) and `OutputSince` (manager.go:4087), which call `ReadAll` on
  the decrypted file, and in the opencode-acp startup scan
  (manager.go:~5736, `os.ReadFile`). These affect encrypted or ACP sessions only
  and not the reported claude-code session. Noted for a later patch, not part of
  this fix.

### (3) Recreated pane or output pipe not re-attached

Mostly ruled out.

- Daemon restart: `ResumeMonitors` re-pipes unconditionally for non-encrypted
  sessions (manager.go:4786-4787 calls `RepipeOutput`). Encrypted sessions are
  skipped (comment at manager.go:4776-4782), so their output.log stops growing
  after a restart, but the live view is unaffected because it polls the pane.
- tmux server restart: a session whose tmux session no longer exists is marked
  failed (manager.go:4742-4753), so it ends visibly instead of silently stalling.
- Silent-stall risk found, not confirmed on the operator's session: the capture
  goroutine `continue`s forever on a capture error (manager.go:2270-2272) and
  exits only when the session state leaves running, waiting_input or
  rate_limited. A session still marked running whose pane is gone produces no
  frames and no signal to the client. It also reads `sess.TmuxSession` captured
  at subscribe time rather than `current.TmuxSession`. Worth hardening: emit an
  error frame or stop after N consecutive failures.

### (4) Service worker and cache serving a stale app.js

Ruled out as a cause, one hygiene issue.

- sw.js fetch handler is network-first for `/app.js`, `/index.html`, `/style.css`
  (sw.js:~95-116); the cache is used only when offline. The daemon sends
  `Cache-Control: no-store` for `/`, `/index.html` and `/sw.js` and
  `no-cache, must-revalidate` plus an ETag for `/app.js` (server.go:1523-1533).
  index.html loads `app.js?v=%%DW_VERSION%%` (index.html:170).
- Hygiene: `CACHE_NAME` in sw.js:27 still carries an old pre-release tag and has
  not changed across many releases. Because the worker file is byte-identical,
  browsers never see an update, so the install and activate path that purges
  caches and force-navigates open windows never runs. This is harmless while
  network-first works but defeats the stated "bump each release" design. Fix:
  derive the cache name from the daemon version (serve sw.js through the same
  `%%DW_VERSION%%` substitution as index.html) or bump it in the release script.

### (5) Pane size mismatch (333x89 vs 120x40)

Confirmed contributing, not the root cause.

- 333 columns is not a stray value: the PWA sizes xterm with FitAddon and sends
  its own `term.cols x term.rows` as `resize_term` on open and on container
  resize (app.js:4093-4100, 4130-4149). At the default 9 px font a wide desktop
  window fits about 330 columns. The server applies it as long as it is not below
  the session's `console_cols`/`console_rows` (api.go:2314-2332), and
  `ResizeTmux` (manager.go:2746) resizes the whole tmux window. The pane is
  therefore sized by the last client to open or resize it (last writer wins).
- Mismatch when two clients view one session: the desktop sets the pane to
  333x89, then a phone opens it and its xterm is 120 wide. Every captured line is
  up to 333 columns, xterm wraps each line to about three rows, 89 lines become
  about 250 rows in a 40-row terminal, and the top of the TUI scrolls out of
  view. The reverse (phone last) leaves the desktop xterm wider than the pane,
  which renders correctly but narrow. A resize by one client is not pushed to
  other clients' xterm, since `syncTmuxSize` runs only on that client's own size
  change (app.js:4130-4145).
- Frame size, measured on private tmux windows after 3000 lines of coloured
  output (history limit left at the tmux default):

  | pane | visible `capture-pane -e -p` | `capture-pane -e -p -S -` |
  |---|---|---|
  | 333x89 | 15,049 bytes, about 3.5 ms | 340,464 bytes, about 12.4 ms |
  | 120x40 | 3,320 bytes, about 3.0 ms | 170,281 bytes, about 8.6 ms |

  So a full redraw of the wide pane is about 4.5x larger. A busy TUI changes
  almost every 200 ms tick, giving roughly 75 KB/s per subscribed session per
  client for the wide pane, multiplied by the broadcast-to-all behaviour above.
  The initial `subscribe` capture and the post-`resize_term` capture both use
  `-S -` (tmux.go:312, api.go:2277, api.go:2338): about 340 KB for the wide pane,
  sent as a single frame, then `reset()` and a full xterm write. This causes a
  visible stall on open and on every resize, but not a permanent freeze.
- Proposed fix: send only the visible pane (plus a bounded scrollback, e.g. 200
  lines) on subscribe and resize; broadcast resize results to all subscribers so
  other clients re-fit; stop broadcasting pane frames to clients that have not
  subscribed to the session.

## Confirmed contributing leads (ship with the fix)

1. PWA never unsubscribes or opts in to scoped output, so capture goroutines and
   broadcast traffic scale with sessions ever opened (api.go:2245,
   ws_output_scope.go). Fix: send `unsubscribe` when leaving a session and scope
   `pane_capture` to subscribers even for `all` clients, since the handler
   already ignores frames for non-active sessions.
2. Full-scrollback capture on subscribe and resize (see (5)).
3. Subscribe and resize run in the WebSocket read loop (api.go:2245, 2314): move
   the captures to a goroutine so `send_input` is never queued behind them.
4. sw.js cache name never changes (see (4)).
5. Capture goroutine silent on repeated tmux errors (see (3)).

## Leads ruled out

- Stale-socket watchdog or reconnect without re-subscribe.
- Whole-file reads of output.log in the live path (plaintext).
- Output pipe not re-attached after a daemon restart (non-encrypted sessions).
- Stale app.js served by the service worker.

## Test plan for the fix

- node:test for the pane_capture handler: two frames 10 ms apart both reach the
  terminal; a frame arriving while scrolled up is queued and shown after
  "jump to live"; keyframe resend of an identical frame does not flash.
- Go test for the capture goroutine keyframe interval and for stopping after N
  capture errors.
- Throwaway tmux experiment (private socket only, `env -u TMUX -u TMUX_TMPDIR`)
  comparing frame size and latency at 333x89 and 120x40 after the capped
  scrollback change.

## Server-side check (2026-10-21): no code change on this branch

The confirmed root cause is client-side: the `pane_capture` handler in app.js drops
frames (33 ms leading-edge throttle, no trailing flush) and stops writing while the
xterm viewport is scrolled up. The server sends each changed pane correctly, the
output.log, output-pipe re-attach and service worker/cache-header paths were ruled out
above, and no server-side defect was confirmed as the cause of the stall. The
server-side items listed under "Confirmed contributing leads" (periodic keyframe,
capture-error stop, off-loop subscribe/resize captures, scoped pane frames, sw.js cache
name) are hardening that heals or narrows the symptom, not the cause. Per the scope of
this task (server-side causes only) none of them is implemented here, so there is no Go
change or test. The client fix (trailing-edge throttle, jump-to-live control, subscribe
re-send on silence) and the server hardening remain for the next patch above main.

## Client fix implemented (2026-10-21)

internal/server/web/app.js: `pane_capture` now goes through `handlePaneCapture`
(trailing-edge 33 ms throttle; the newest held frame is flushed from a 40 ms timer),
frames received while the xterm viewport is scrolled up are held in
`state._pendingPaneFrame` with a visible "Jump to live" button (also triggered by typing,
sending input, or scrolling back to the bottom), and the 20 s WS watchdog tick re-sends
`subscribe` when no pane frame has arrived for 30 s on an open session view. Tests:
internal/server/web/app-pane-capture.test.js. Server-side hardening remains for a later patch.
