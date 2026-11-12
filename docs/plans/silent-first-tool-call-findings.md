# Silent first tool call leaves a busy claude-code worker in waiting_input

Status: fixed in the daemon state engine (busy probe). Live reproduction not
run in this environment; the findings below come from reading the code paths.

## Symptom

A claude-code one-shot worker is given its task with `send_input`. Its first
action is a long tool call (for example a test run or a long shell command)
that prints nothing new for a while. The session record stays `waiting_input`
even though the worker is busy.

## Why no channel event follows task delivery

The question was whether the channel hook fires only on assistant output, the
event is dropped, or the delivery path fails to mark running. Findings:

1. **Delivery does mark running.** `DeliverOneShotTask`
   (`internal/session/manager.go`, around the call to `SendInput`) only runs
   once the session is `WaitingInput` with its main prompt visible.
   `SendInput` (`internal/session/manager.go`, the block after the send)
   moves `WaitingInput` to `Running` and sets `LastChannelEventAt = now`.
   Delivery is not the gap.

2. **The gap watcher flips it back.** `runChannelStateWatcherTick`
   (`internal/session/state_engine.go`) flips any `Running` session whose
   `LastChannelEventAt` is older than `DefaultRunningToWaitingGap` (15 s) to
   `WaitingInput`. The 15 s clock starts at delivery, so a worker that makes no
   further channel event flips about 15 s after delivery.

3. **Only output-driven events keep the clock alive for claude-code.** The
   sources that call `MarkChannelEvent(EventRunning)` are:
   - the pane-content change branch of `StartScreenCapture`
     (`internal/session/manager.go`, `MarkChannelEvent(sess.FullID, EventRunning)`
     after the "content changed" check). It fires only when the captured pane
     text differs from the previous capture;
   - the structured-channel branch of `processOutputLine`, which runs for log
     lines only;
   - MCP channel text, through `MarkChannelActivityFromText`
     (`internal/session/manager.go`, reached from `internal/server/api.go`);
   - operator input, which `SendInput` already records.
   A silent tool call produces none of the log lines or MCP text. Whether it
   produces a pane change depends on the TUI. A spinner that redraws keeps the
   pane-change path alive and the bug does not appear. A static frame does not
   refresh the clock, so the watcher flips the session.

4. **Nothing flips it back.** The only path from `WaitingInput` to `Running` is
   a channel event, and the prompt detector does not help. `matchPromptLines`
   suppresses prompt transitions while the pane shows "esc to interrupt", a
   `⎿ …` tool line or a spinner line, so no `WaitingInput` transition comes from
   the pane either. The record sits in `waiting_input` until output resumes.

5. **The event is not dropped.** `markChannelEventAt` accepts `EventRunning`
   for `WaitingInput` and `Running`. It refuses only the sticky states
   (`Complete`, `Failed`, `Killed`, `RateLimited`). The events that do arrive
   are applied correctly.

Conclusion: the channel hook fires on assistant output only. A silent tool call
with a static pane has no structural signal, so the gap watcher's fallback
reads it as idle. The delivery path marks running correctly.

## Why no structural fix was made

The structural fix would be a claude-code tool-start signal, for example a
`PreToolUse` hook that posts a channel event. That needs a change to the
Claude Code hook configuration that the daemon writes, which is outside the
state engine this change covers. It is not implemented here. The probe is the
fix inside the state engine, and it does not depend on the TUI redrawing.

## Fix: busy probe

`internal/session/busy_probe.go` adds:

- `paneLooksBusy(capture, childActive)`, a pure function. The pane is busy when
  it shows an explicit working marker, which is "esc to interrupt" in the last
  three lines, a `⎿ …` tool line, or a spinner line (a spinner glyph followed by
  a capitalised verb and an ellipsis). It is also busy when the pane process has
  a child and the pane shows no idle prompt. A past-tense finished-turn line
  such as `✻ Crunched for 52s` has no ellipsis and does not count.
- `Manager.applyBusyProbe(fullID, ts)`, which captures the live pane through
  the injected `TmuxAPI`, asks the child check, and when the probe says busy
  calls `markChannelEventAt(EventRunning)`. That refreshes
  `LastChannelEventAt` and moves `WaitingInput` to `Running`. Terminal and
  rate-limited states are not touched.

Where it runs:

- `DeliverOneShotTask`, right after a successful `SendInput` and
  `MarkTaskDelivered`.
- `runChannelStateWatcherTick`, in two places:
  - a recovery pass: a delivered one-shot claude-code session in `WaitingInput`
    whose pane is busy returns to `Running`;
  - the gap pass: a `Running` session past the gap whose pane is busy keeps
    `Running`, and its clock restarts through the write above, so the next flip
    check is a full gap later.

Scope: the probe applies only to `BackendFamily == "claude-code"` with a tmux
session. Other backends keep the existing gap behaviour. The child check is the
injectable `Manager.paneChildFn`. Its default runs `tmux display-message` for
the pane pid and then `pgrep -P`. Tests replace it with a fake.

## Known limits

- The working markers are Claude Code's current TUI wording ("esc to interrupt",
  `⎿`, spinner glyphs). A TUI change that renames them removes the probe's
  signal, and the session falls back to the old gap behaviour.
- An active child process alone is not enough. Claude Code keeps MCP helper
  processes as children even when idle, so a child counts as work only when the
  pane shows no idle prompt.
- The probe makes one pane capture and up to two cheap process calls per
  probed session per watcher tick. Only delivered one-shot claude-code sessions
  that are waiting, and running sessions past the gap, are probed.
- Not verified live. The daemon was not run against a real claude-code worker
  here. The unit tests use a fake tmux and a fake child check.

## Parity surface

This is a daemon-internal state fix with no operator-facing config, REST, MCP,
CLI, chat or PWA surface change. No config key, endpoint, MCP tool, CLI flag,
chat command or PWA control is added or changed. The 15 s gap, the 30 s
watcher warm-up and the sticky-state rules are unchanged.

## Restart matrix states

Daemon restarts re-run `ResumeMonitors`, which resets the clock for live panes,
and the watcher starts after its 30 s warm-up. The probe's effect after a
restart:

| Session state after restart | Pane | Result |
|---|---|---|
| `Running`, live pane | busy | stays `Running` (gap pass refreshes the clock) |
| `Running`, live pane | idle | flips to `WaitingInput` after the gap, as before |
| `WaitingInput`, one-shot, task delivered (persisted `TaskDeliveredAt`) | busy | returns to `Running` on the first tick after warm-up |
| `WaitingInput`, one-shot, task not delivered | busy | stays `WaitingInput` (no delivery yet) |
| `WaitingInput`, interactive | busy | stays `WaitingInput` (recovery is one-shot only) |
| `Complete`, `Failed`, `Killed`, `RateLimited` | any | unchanged (probe returns before the pane is read, and writes are sticky) |
| `Running`, tmux session gone | capture error | probe returns false, the gap flip applies as before |

## Tests

`internal/session/busy_probe_test.go` covers the pure probe table, busy pane
after delivery (`WaitingInput` to `Running`), bare shell pane staying
`WaitingInput`, terminal states staying sticky, the gap watcher not flipping a
busy session and flipping it once the pane goes quiet, the recovery pass, and
the scope limit to claude-code. All use `NewFakeTmux` and a fake child check.

## Start-tracker restart-matrix findings

These rows cover the start-confirm tracker in the wait loop, the four states in
`docs/plans/silent-first-tool-call-matrix.md`. Each state runs as a fake-clock
scenario test in `cmd/datawatch/session_start_scenarios_test.go`. Observed
results are from `go test ./cmd/datawatch/ -run TestSessionStart`: 21 top-level
test functions, the four scenario tests and 17 tracker tests, all passing. No
live daemon restart was run.

| State | Observed result | Status |
|---|---|---|
| (a) Six-minute silent tool call, `waiting_input` | Survived every tick at the 120 s and 300 s bounds in all four variants (spinner moving or frozen, live command child or not). The tool line is evidence on every tick. In the frozen variants the only pane change is the one at 30 s, so the tool line alone carries the clock. | Pass |
| (b) Bare shell session | Ended at 121 s. The end reason names the 121 s silence against the 120 s limit. | Pass |
| (c) Live working worker adopted after a daemon restart | Not ended at adoption. Survived 0 s and 90 s, and was ended at 91 s, 121 s after the last evidence of life (the pane change at -30 s). A live tool call on the first tick also survives (`TestSessionStartTrackerAdoptedSessionToolCallStillKeepsItAlive`). The brief's "expected survive" holds for a worker showing work, and a frozen pane with no tool line is ended after its bound. | Pass, with a design question for the operator: is a worker whose pane has been frozen with no tool line for the full bound still working? The tracker says no. |
| (d) Bare shell adopted after a restart | Ended at 21 s, 121 s after the adoption seed, not 121 s after the first tick. | Pass |

Open point for the operator: state (c) as written in the brief is "expected
survive". The tests show survival for a worker that shows a tool line or a
changing pane, and an end after the bound for a worker with a frozen pane and
no tool line. Whether a frozen-pane worker should count as working is a design
choice. The tracker follows the current rule, and this change does not alter it.
