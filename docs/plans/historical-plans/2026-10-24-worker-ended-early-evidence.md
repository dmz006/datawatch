# Workers ended after about five minutes: evidence for the investigation

Collected 2026-10-24 01:45 by the supervisor, as input to the worker-ended-mid-command PRD. Facts only; the cause is not yet known.

## Timing across the Claude task workers since the restart that deployed 9.8.0

| Session | Task | Seconds from creation to end | Seconds from task delivery to end |
|---|---|---|---|
| d5d4 | restart matrix harness core | 359 | about 330 |
| 10f1 | websocket delta broadcast | 349 | about 320 |
| 268f | remaining restart matrix states | 317 | 281 |
| ae32 | Observer matrix (Sonnet) | 346 | 318 |
| 455c | Observer matrix (default model) | 343 | about 330 |
| bd77 | this investigation's own first task | 344 | **304** |

Every one ended with `claude_alive` true and a channel event within 0.02 to 0.16 seconds before the end. The window is close to 300 seconds, which is also the default of the orphan sweep grace period, but the orphan sweep logged nothing in any of these cases (it logs when it first sees an orphan and when it ends one).

## What the daemon saw for session bd77 (the only one watched live)

- 01:40:04 created, the development-channels dialog confirmed, 01:40:06 last channel event.
- 01:40:40 task delivered with send_input. After this **no channel event arrived and the session record stayed `waiting_input` with `updated_at` frozen at 01:40:06** for the next five minutes.
- 01:45:44 the session was killed ("session ended") and **in the same second the executor spawned the verifier for the task** (`[autonomous:verify:cd5d0002] spawned 14dc`). So the executor concluded the worker had finished and ended it. Nothing else is logged at that second.
- The worktree's Claude settings contain only PreCompact and Stop hooks (the memory hooks), no UserPromptSubmit or PostToolUse hook, so the daemon has no activity signal from the worker other than channel events.

## Working hypothesis (unverified)

The daemon learns that a Claude worker is busy only through channel events. After the task is delivered no further event arrives, the session record keeps saying `waiting_input`, and a silence of about five minutes is read as "finished", so the worker is ended while still working and the verifier then reports "no new changes". This fits the Observer session `ae32`, which was in the middle of a ten-minute wait loop when it was ended.

## Questions the investigation must answer

1. Which code path ends the session and which condition fires after about 300 seconds?
2. Why does no channel event follow the task delivery for these sessions, when earlier sessions (and most before the afternoon of 2026-10-23) flipped to running?
3. What changed: the restart that deployed 9.8.0 at 00:25 on 2026-10-24, the per-session commit guard (deployed 18:58 on 2026-10-23), the capacity lease changes, or something else? The Observer sessions 455c and ae32 died the same way at 17:04 and 22:57 on 2026-10-23, before the 9.8.0 restart.
