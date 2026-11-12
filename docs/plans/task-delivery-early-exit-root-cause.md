# Task delivery early exit — root cause

Read-only investigation. No source file was changed.

## Symptom

Autonomous claude-code worker sessions (one-shot, PRD-owned) die about 1.5 to 2 s after the task is claimed, and the task is never delivered. Timeline of every failing session (0129, be23, 6b9c, 8763, 950d, 195b, ...):

```
created
running -> waiting_input  (WARNING: Loading development channels)
input-sent | source:auto-accept-disclaimer | 1
killed | session ended          (no input-sent source:channel-task)
```

`session.json` of each failing session has `channel_ready=true`, `input_count=1` and a `task_delivered_at` that is 1 s after the disclaimer accept and 1.4 to 1.9 s before the kill. The healthy session 1c5f delivers the task about 5 s after creation.

Facts from the evidence:

- The kill is not an executor kill log line, an audit `kill` event, a stall detector or the one-shot reaper (`oneShotFinished` skips PRD-owned sessions, `internal/session/oneshot_reap.go:36`). The journal has no `[autonomous]` line for any of them.
- The attempts chain: be23 dies at 08:51:52, the next attempt 0129 is created at 08:51:53, which is the executor's 3 s verify tick noticing a terminal state. The retry hint is `verifier: no changes detected (diff empty and no new files)` (0129 `conversation.md`).
- Failing sessions often show two `killed | session ended` lines, so the kill path ran twice.
- Summaries are generated in the second before the kill (`summary_generated_at` 08:51:58.29 < `updated_at` 08:51:58.47 for 0129).

## Root cause

The task delivery is not protected against the session's own completion detector while the task text is being typed. Three defects combine.

1. **The claim does not arm the task-echo suppression.** `Manager.ClaimTaskDelivery` (`internal/session/manager.go:3056`) only sets the persisted `sess.TaskDeliveredAt`. The completion detector's task-echo guard reads a different thing: the in-memory `m.taskDeliveredAt` map (`manager.go:2644`, same guard in `processOutputLine` at `manager.go:6279`). That map is written only by `MarkTaskDelivered` (`manager.go:3031`). In the channel path, `handleChannelReady` calls `MarkTaskDelivered` after `SendInput` returns (`internal/server/api.go:9072`), whereas the TUI path deliberately calls it before (`cmd/datawatch/main.go:7612`, with a comment explaining why). So for the whole time the task is being typed into the claude input box there is no suppression, although the claim already won.
2. **The task text itself contains the completion marker.** Every autonomous spec ends with a `DATAWATCH_COMPLETE:` instruction (and this PRD harness appends the line `Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary ...>`). While it is typed, the claude input box wraps it and a wrapped line can begin with `DATAWATCH_COMPLETE:`. The visible scan and the scrollback scan in `StartScreenCapture` (`manager.go:2540-2640`) accept any line with that prefix via `matchesCompletionPattern` (`manager.go:74`). The only placeholder guard is `strings.Contains(l, "<one-sentence summary")` (`manager.go:2565`, `2609`), which does not match the `<one-line plain-text summary` wording used by these specs, and the claude box is not bordered with `│`/`┃` so the border guard does not apply either. For a one-shot session the scan covers all visible lines (`visibleScanLimit = len(capLines)`, `manager.go:2539`), so this fires as completion: `commitOnCompletion`, then `onSessionEnd`, then `triggerSummarize` (`manager.go:2677`).
3. **The executor treats any terminal session state as finished work.** The verify wait loop in `cmd/datawatch/main.go:4672` breaks on `StateComplete`, `StateFailed` or `StateKilled` without checking that a task was ever delivered or a completion marker was ever produced by the model. It then calls `mgr.Kill` if the state is not already killed (`main.go:4894`, which explains the second `session ended` line), runs verification, gets "no changes detected", and burns a retry through `executeOne` (`internal/autonomous/executor.go:1143`, spawn). The next attempt hits the same race.

The ordering that makes it lethal: the auto-accept goroutines (`cmd/datawatch/main.go:2235` and `7558`, source `auto-accept-disclaimer`) dismiss the dialog, the bridge posts ready, `handleChannelReady` wins `ClaimTaskDelivery` (`api.go:9044`) and starts its 1 s poll goroutine (`api.go:9057-9078`), the first poll finds the prompt idle and calls `SendInput(..., "channel-task")`, and within about 0.5 s the screen watcher sees the echoed task, declares completion and the session ends. `SendInput` never reaches the point where `input-sent source:channel-task` is recorded, which is why that line is missing. The claim is won but the delivery is never completed, and `ReleaseTaskDelivery` (`manager.go:3074`) is not called because the send itself did not error.

Not the cause: the disclaimer accept being sent to a dying session (it is sent once, `input_count=1`, to a live session that reaches `❯ Try "how..."` per `output.log`), and a second launch superseding the first (each session is killed before the next is created; no overlap).

### Confidence

Defects 1 to 3 are confirmed in code. The link "the echoed task triggers the completion scan" is the best fit for the timing (kill 0.5 to 0.9 s after the first send, summary just before the kill, double kill line) but the daemon has no debug log for it: no `StartScreenCapture: visible-scan match` line exists because debug logging was off. It must be confirmed with a repro (below) before the fix is declared done. If the repro shows the kill does not come from `StartScreenCapture`, the guard in defect 3 still stands and the next suspect is the `SendInput` path itself.

## Planned fix

1. `ClaimTaskDelivery` also records `m.taskDeliveredAt[fullID] = now` (under the same lock) so suppression is active from the moment the claim is won; `ReleaseTaskDelivery` removes it. Keep `MarkTaskDelivered` after the send as a refresh. Add the same call before `SendInput` in the `handleChannelReady` goroutine for clarity.
2. Make the placeholder guard in both scans and in `processOutputLine` match any `DATAWATCH_COMPLETE:` line that still contains an angle-bracket placeholder (`<...summary`), via one shared helper, so spec wording cannot defeat it.
3. In the executor wait loop (`cmd/datawatch/main.go:4672`), a terminal state reached when the task was never delivered (`!mgr.TaskDelivered(id)`) or with no completion marker in the output is a delivery failure: log it, record it as a retryable infra stall (`ErrWorkerStalled`), and do not run the verifier on it. Log every kill in this path with the caller so a silent kill cannot recur.
4. Tests: a session-manager test that claims delivery and feeds a pane containing the typed task with a wrapped `DATAWATCH_COMPLETE: <one-line plain-text summary ...>` line and asserts no completion; a test that `ClaimTaskDelivery` arms the guard and `ReleaseTaskDelivery` clears it; an executor test that a killed session with no delivery is retried as a stall without verification. Run with `env -u TMUX -u TMUX_TMPDIR`; never use any command that stops a tmux server.
5. Repro to confirm first: enable detection debug logging, spawn one autonomous claude-code task and check for `StartScreenCapture: visible-scan match` right after `[channel] task delivered` is attempted.
