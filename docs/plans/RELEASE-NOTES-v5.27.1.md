# datawatch v5.27.1 — release notes

**Date:** 2026-04-28
**Patch.** Single-bug fix on top of v5.27.0.

## What's fixed

### Session-detail xterm refit + input rebind on prompt cycle

Operator-reported: *"After a prompt finishes, and i submit a new one, it refreshes the page, screen size refreshes wrong size, tmux input goes away and i have to exit and reenter."*

The yellow "Input Required" banner is added/removed in two paths:

1. **Explicit Dismiss button** (operator clicks ✕). v5.26.44 already fits xterm + sends `resize_term` immediately on the next animation frame.
2. **State-driven** (`updateSession` → `refreshNeedsInputBanner` when the session transitions in/out of `waiting_input`). This path **was patching the slot innerHTML but skipping the immediate refit** — the operator was at the mercy of `ResizeObserver`'s 200ms debounce, which is long enough to see a busted layout.

When the operator submitted a new prompt, the session went `waiting_input` → `running`, the banner was removed by the state-driven path, and the xterm container size mismatched until the debounced fit caught up. On the next prompt arrival the banner reappeared, the size mismatched again, and on a fast second prompt the input element could end up reattached without its Enter handler — at which point the operator had to back out of the session and re-enter to get the input bar working again.

### Fix

- `refreshNeedsInputBanner` (`app.js`) now compares before/after banner HTML; on any change it runs the same `requestAnimationFrame` → `fitAddon.fit()` → `send('resize_term', …)` sequence the explicit dismiss path uses.
- The Enter-key handler is now tagged on the `#sessionInput` element with `_dwEnterBound`. `refreshNeedsInputBanner` rebinds it if missing (covers the case where the input element was reattached during a state transition); `renderSessionDetail` checks the same flag to avoid double-binding.

## Tests

- 1469 unit tests still pass
- Smoke unaffected (the bug is PWA-only)

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (or wait for the service worker to pick up
# the new cache name `datawatch-v5-27-1`).
```

No data migration. No new schema. No new endpoints.
