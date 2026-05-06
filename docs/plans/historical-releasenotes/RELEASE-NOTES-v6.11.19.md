# Release Notes — v6.11.19 (BL265 — content-aware channel state detection)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.19

## Operator directive

> "It didn't work for capturing session state, are you getting it from the actual message? Debug and get it working, this is one of the most important features, knowing when jobs are done or blocked and or input."

## What changed

v6.11.18 (BL264) used existence-only detection — any channel event was treated as "Running activity". That covered the false-positive `WaitingInput` case but missed the operator's real ask: classify what the LLM is actually saying.

v6.11.19 now parses the message content and classifies into one of:

- **complete** — task done, results delivered. Transitions to `StateComplete` and fires the `onSessionEnd` callback.
- **input** — LLM is asking the operator for input/confirmation. Transitions to `StateWaitingInput`.
- **blocked** — LLM reports being stuck. Logged + UpdatedAt touched, but state unchanged (text alone is too unreliable for terminal transitions).
- **""** (generic activity) — backward-compatible v6.11.18 behavior; bumps `WaitingInput` → `Running`.

## Pattern sets (case-insensitive substring match)

### Completion (15 phrases)
`task complete`, `task completed`, `task is complete`, `task is now complete`, `successfully completed`, `all tasks complete`, `all tasks completed`, `i've completed the task`, `i have completed the task`, `the task is now complete`, `the work is complete`, `all done`, `task done`, `job complete`, `job done`

### Input-needed (20 phrases + trailing-`?` heuristic)
`should i proceed`, `should i continue`, `shall i proceed`, `shall i continue`, `do you want me to`, `would you like me to`, `please confirm`, `please advise`, `awaiting your input`, `awaiting your response`, `awaiting confirmation`, `waiting for your`, `waiting for input`, `need your input`, `need your decision`, `need your guidance`, `need clarification`, `can you clarify`, `could you clarify`, `how would you like`, `what would you like` — plus any message ending in `?`.

### Blocked (10 phrases)
`i'm blocked`, `i am blocked`, `i'm stuck`, `i am stuck`, `unable to proceed`, `cannot proceed`, `can't proceed`, `hit an error`, `encountered an error`, `blocked on`, `stuck on`

## State-transition matrix

| Current | Channel signal | Result |
|---|---|---|
| Running / WaitingInput | `complete` | → **StateComplete** + `onSessionEnd` fires |
| Running / WaitingInput | `input` | → **StateWaitingInput** |
| any | `blocked` | log only, no transition |
| WaitingInput | generic activity | → **StateRunning** (back-compat with v6.11.18) |
| Running | generic activity | UpdatedAt touched, no transition |
| Complete / Failed / Killed / RateLimited | any | UpdatedAt touched, state preserved (locks honored) |

## Why per-channel patterns instead of widening global

Per the saved memory rule from v6.11.6 (don't widen global default detection patterns to natural-language phrases — they false-fire on tmux pane-buffer replay), the channel patterns are kept **separate**. They only run on per-message channel events; tmux pane-buffer replay doesn't touch them. Safe to be loose.

## Tests

1788 pass (1776 + 12 new BL265 cases):
- `detectChannelStateSignal` classifier on 7 completion phrases
- ditto on 8 input-needed phrases
- ditto on 5 blocked phrases
- ditto on 4 generic phrases (no signal)
- per-state transition cases: complete-from-Running, input-from-Running, generic-from-Waiting (still wakes), blocked-no-change
- state-lock cases: never overrides RateLimited, no resurrect from Complete
- `EmitChatMessage` integration with assistant `Task complete`
- back-compat: zero-text `MarkChannelActivity` falls through to v6.11.18 behavior

## Mobile parity

Not needed — daemon-internal; WS messages unchanged.

## See also

- CHANGELOG.md `[6.11.19]`
