# Release Notes — v6.11.11 (BL263 follow-up: screen-size + input-disabled + keyboard scroll)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.11

## Summary

Three operator-reported bugs after v6.11.10:

1. "Screen size is compacted to Window size and not the full size going wider than the screen so lines were wrapping."
2. "Tmux command panel still not displaying after restart."
3. "If I tap on the screen to type directly on the terminal, the keyboard comes up but the screen doesn't [scroll] like when typing in tmux command window it does."

## Fixed

### Bug 1 — terminal sized for stale dims after restart

The reconnect-resume path sent `resize_term` using `state.terminal.cols` / `.rows` BEFORE refitting the addon. Browser may have resized during the disconnect window (device rotation, dock open/close, devtools, etc.). Stale dims → tmux pinned the pane to the wrong width → output wrapped at the wrong column → operator saw a compacted-then-overflowing terminal.

Fix: call `state.termFitAddon.fit()` first so xterm recomputes its cell-grid for the live container, then read `cols`/`rows` for the resize_term send.

### Bug 2 — input bar greyed out after restart

The input bar gets the `input-disabled` class while disconnected (opacity:0.5 + pointer-events:none). On reconnect the bar is in the DOM but appears washed out — operator perceives it as "missing".

Fix: drop `input-disabled` from `#inputBar` immediately on WS reconnect.

### Bug 3 — iOS keyboard hides terminal content

Mobile browsers auto-scroll-into-view when a native `<input>` or `<textarea>` gets focus. xterm.js uses a hidden helper textarea positioned absolutely + tiny → browser's heuristic doesn't fire. Tapping the terminal opens the keyboard but doesn't scroll the visible content above it.

Fix: hook the `.xterm-helper-textarea` focus event to call `scrollIntoView({ block: 'end', behavior: 'smooth' })` on the input bar (or container) after 250 ms. The delay covers the iOS keyboard-show animation so the scroll lands at the right position.

### Bonus: deadline-timestamp pending-refresh on reconnect

`state._pendingPaneCaptureRefresh` set to `Date.now() + 700` instead of boolean `true` on reconnect, mirroring the v6.11.8 scroll-mode fix so concurrent frames during the reconnect window don't race the flag.

## Remaining symptom (filed for follow-up)

> "exiting and going back in didn't [work] but new command did"

When the operator backs out of session-detail and re-enters after a daemon restart, the pane stream sometimes doesn't refresh until tmux has actual new output. Likely cause: PWA-side `state._lastPaneFrame` dedupe skips the post-restart capture because content is identical to the pre-disconnect view. Investigation continues — will ship in a separate patch once root-caused.

## Tests

1767 pass.

## Mobile parity

[`datawatch-app#65`](https://github.com/dmz006/datawatch-app/issues/65) filed — same fixes for the Compose Multiplatform app.

## See also

- CHANGELOG.md `[6.11.11]`
