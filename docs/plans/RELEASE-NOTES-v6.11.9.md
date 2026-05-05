# Release Notes — v6.11.9 (BL263 — re-pipe tmux after daemon restart)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.9

## Summary

BL263 — fix the actual cause of the "session is frozen / I have to restart it" symptom operators have been reporting since the v6.11.x daemon-restart investigation began. The fix is one missing call: `ResumeMonitors` (called on daemon startup) re-attached the `monitorOutput` goroutine to surviving tmux sessions but never called `tmux pipe-pane` again. Without the re-pipe, the log file never received new lines, so the daemon's fsnotify watcher saw nothing, and every downstream channel (pane_capture, comm-bridge, completion detection) appeared frozen.

Everything I tried in v6.11.6 and v6.11.7 (PWA reconnect optimization, completion-pattern widening) was treating symptoms instead of the cause.

## Root cause

```
v6.11.5 daemon process A      v6.11.5 daemon dies          v6.11.9+ daemon process B
     │                               │                          │
     │  tmux pipe-pane child         │   pipe-pane child         │
     │  writes to output.log         │   either dies or writes   │
     │  (FD held by A)               │   to a now-closed FD      │
     │  monitor goroutine             │                          │   ResumeMonitors:
     │  reads new lines via           │                          │   - re-attach monitor ✓
     │  fsnotify ──────────────► fsnotify still works            │   - re-pipe tmux ✗ (BUG)
     │                                                            │
                                                                  └─► monitor sits silent
                                                                       no new lines arrive
                                                                       PWA pane_capture stale
                                                                       comm channel frozen
                                                                       operator: "tmux or
                                                                       channel isn't working"
```

## Fixed

- **`internal/session/tmux.go`** — new `RepipeOutput(session, logFile string)` method on `TmuxAPI` and `TmuxManager`. Two-step: closes any pipe-pane in effect (no-op if none), then opens a fresh one. Handles both "old pipe-pane child died" and "old pipe-pane child survived but is broken" cases.
- **`internal/session/manager.go`** — `ResumeMonitors` calls `m.tmux.RepipeOutput()` per surviving active session before starting the monitor goroutine.
- **`internal/session/fake_tmux.go`** — `FakeTmux.RepipeOutput` records as `"repipe"`.
- **`internal/session/bl263_repipe_test.go`** — 2 new tests:
  - `TestBL263_ResumeMonitorsRepipesActiveSessions` — verifies 2 active surviving sessions both receive a `repipe` call and the completed session doesn't.
  - `TestBL263_ResumeMonitorsSkipsRepipeForDeadTmux` — verifies dead-tmux sessions don't get re-piped (state moves to failed via existing reconcile path).

## What's still a follow-up

- **Encrypted-FIFO sessions** — the FIFO file is on disk but nothing reads from it after restart. ResumeMonitors currently skips re-pipe for encrypted sessions; operator must restart those manually. Tracked as v6.11.x follow-up.

## Tests

1767 pass (was 1765 + 2 new BL263 tests).

## Mobile parity

Not needed — daemon-internal fix; WS messages to mobile clients unchanged.

## Why earlier releases didn't fix this

| Release | Attempted | Why it didn't fix it |
|---|---|---|
| v6.11.6 | PWA reconnect path: fit + force-rerender + 12 new completion patterns | Symptom-side; daemon was still not receiving any tmux output, so PWA fixes couldn't display anything |
| v6.11.7 | Reverted v6.11.6 | Restored stability but didn't address the actual missing pipe-pane call |
| v6.11.8 | Eager version-reload + scroll-mode fixes | Unrelated bugs; no effect on the silent-tmux problem |
| v6.11.9 | RepipeOutput in ResumeMonitors | Root cause fixed |

## See also

- CHANGELOG.md `[6.11.9]`
- `docs/plans/README.md` BL263 entry
