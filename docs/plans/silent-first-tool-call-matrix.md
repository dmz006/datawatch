# Silent first tool call: restart-matrix states

This matrix covers the start-confirm tracker in the autonomous wait loop
(`internal/autonomous/start_tracker.go`). The tracker decides whether a
claude-code worker that was handed its task and sits in `waiting_input` has
started, and ends it when the silence exceeds the bound. The busy probe that
keeps the session state right is a separate mechanism; its findings are in
`docs/plans/silent-first-tool-call-findings.md`.

## Harness

No `scripts/restart-matrix*.sh` harness exists in this tree, so the states are
recorded here and verified by scenario tests in
`cmd/datawatch/session_start_scenarios_test.go`. The tests run the tracker on a
fake clock with fixed panes and a fixed process tree. Nothing touches tmux and
nothing sleeps. No daemon was restarted and no live worker was run to produce
these results. The adoption seed is modelled by passing the seed time to the
tracker, which is what the wait loop does with `sessionStartAdoptionSeed`.

Tick cadence: 30 s steps unless a state says otherwise. Bound (limit): 120 s,
and the 300 s variant for state (a).

## State table

| State | Setup | Expected result | How verified | Covered by |
|---|---|---|---|---|
| (a) Worker begins with a six-minute silent tool call, `waiting_input` | Task delivered. The silent prompt is shown until 30 s, then a tool call runs until 360 s. The pane shows a tool line (`⏺ Bash(...)`) and a running line (`⎿ Running…`). Four variants: the spinner's elapsed count moves each tick or stays frozen, and the tool's command is a live child of the agent or is not. | Survives every tick at both the 120 s and 300 s bound. Never ended during the call. | Fake clock, fixed pane and process tree, one tick per step, each tick's end decision checked | `TestSessionStartScenarioSilentToolCallSurvives` (4 variants × 2 limits) |
| (b) Bare shell session, `waiting_input` | Task delivered. Pane is a bare shell prompt, no agent child, no tool line, no assistant turn. Ticks every 30 s to 120 s, then 121 s. | Not ended at 120 s. Ended at 121 s, with the end reason naming the 121 s silence against the 120 s limit. | Same fake-clock harness | `TestSessionStartScenarioBareShellEnded` |
| (c) Live working worker adopted after a daemon restart | Adopted with a seed 600 s before the first tick (the seed is the latest of update time, last channel event and log mtime). Pane changes at -30 s while the session is running. From 0 s the session is `waiting_input` and the pane is frozen. | Survives the first tick and the rest of the bound measured from the last evidence of life (the pane change at -30 s). The bound is not measured from adoption. | Same fake-clock harness | `TestSessionStartScenarioAdoptedWorkingSurvives` (pane-change evidence), with `TestSessionStartTrackerAdoptedSessionToolCallStillKeepsItAlive` for a live tool call on the first tick |
| (d) Bare shell adopted after a restart, `waiting_input` | Adopted with a seed 100 s before the first tick. Pane is a bare shell prompt, task delivered. Ticks at 0, 10, 20 and 21 s. | Ended at 21 s, which is 121 s after the adoption seed. The bound is not reset by adoption, and the first tick does not start a new 120 s window. | Same fake-clock harness | `TestSessionStartScenarioAdoptedBareShellEnded` |

## Parity surface

This is a test-only addition with a plan document. It changes no daemon
behaviour, config key, REST endpoint, MCP tool, CLI flag, chat command or PWA
control. The 120 s bound, the adoption seed and the evidence rules are
unchanged.

## Known limits

- The tests use a fake clock and fixed panes. They do not exercise a real
  restart, the orphan sweep, or a real claude-code TUI. A live run on a sandboxed
  daemon is still needed before this matrix counts as a restart-proof result.
- State (c) with a frozen pane and no tool line is ended 121 s after its last
  evidence. The scenario states that expectation. A live worker shows a tool
  line or spinner while it works, and that keeps it alive. A frozen pane with
  no tool line is not a working worker by the tracker's rules.
- A child process that started before the wait began is not evidence. Only a
  new pid counts. This is pinned by
  `TestSessionStartTrackerNewChildProcessKeepsClockRunning`.
