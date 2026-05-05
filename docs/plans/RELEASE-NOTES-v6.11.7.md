# Release Notes — v6.11.7 (revert v6.11.6 reconnect/completion changes)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.7

## Summary

Operator: "the pwa session is not connecting after last server restart." Reverting v6.11.6's two speculative changes that caused this regression. Keeping the one safe change.

## Reverted

### JS reconnect path (`internal/server/web/app.js`)

v6.11.6 added an `inputBarMissing` check that, when triggered, fell through to a full `renderSessionDetail()` re-render instead of taking the optimized "same session is alive" path. This new branch was firing in cases it shouldn't and breaking the WS reconnect flow.

Restored the exact pre-v6.11.6 reconnect logic — the v5.26.35 + v5.26.45 + BL249 (v6.5.1) path that had been working for operators.

### Aggressive completion patterns (`internal/session/manager.go`)

v6.11.6 added 12 natural-language end-of-task phrases to `completionPatterns`:

```
Task complete · Task completed · Task is complete · Successfully completed
All tasks complete · All tasks completed · I've completed the task
I have completed the task · The task is now complete · The work is complete
All done · Done!
```

These false-fired on claude-code's natural prose. When the daemon restarted and the pane buffer replayed, the patterns matched, the session was marked `complete` prematurely, and the PWA refused to reconnect to what it now thought was a finished session.

Restored to the pre-v6.11.6 single marker: `DATAWATCH_COMPLETE:`.

## Lesson

The right way to add natural-language session-end detection is via `cfg.Detection.CompletionPatterns` — that config knob already exists and lets the operator opt in with project-specific phrasing they've actually unit-tested in their workflow. The global default set should stay tight and protocol-marker-only to keep false-positive risk low.

## Kept from v6.11.6

- **`StartReconciler` interval 30s → 10s** — safe, narrow, unrelated to the regressions above. Faster pickup of tmux-pane-exit (claude `/exit`, process death).

## Tests

1765 pass.

## See also

- CHANGELOG.md `[6.11.7]`
