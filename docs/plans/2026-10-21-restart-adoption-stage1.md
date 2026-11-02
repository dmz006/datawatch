# Staged restart validation, stage 1 (BL552), 2026-10-21 19:53

Goal: validate BL546 (a daemon restart keeps a running worker) with PRDs mid-run on both a Claude lane
and a GPU (pi-prompt) lane. Restart: `systemctl --user restart datawatch`, onto v9.5.65. Five PRDs
in flight. Pre and post snapshots: `/tmp/sv/restart-test/stage1-pre.json`, `stage1-post1.json`.

| PRD | Lane | In-flight task | Session before | Session after | Result |
|---|---|---|---|---|---|
| `6eb30001` | GPU, pi-prompt | 0003 | e7e6 | e7e6 | **Kept**; pi process 3664546 alive |
| `7da90001` | GPU, pi-prompt | 0007 | aa3e | aa3e | Session and pi process 3357102 kept, but the **PRD was parked** (blocked) by the age gate |
| `5f980001` | Claude | 0002 (27 s old) | fb19 | 26b6 | Kept at +50 s, **replaced by +2.5 min** |
| `98e10001` | Claude | 0002 (about 17 min old) | e0e2 | 0920 | **Killed 18 s after restart, task re-launched** |
| `a5530001` | Claude | 0004 (about 17 min old) | 71b7 | 0164 | **Killed 18 s after restart, task re-launched, then the PRD failed** (worker stalled; tasks 0004 to 0006 reset once) |

Verdict: **adoption works for pi-prompt workers and for a Claude worker only a few seconds old; it
fails for Claude workers that have been running for minutes, and the age gate parks a PRD whose
worker is alive.** Two P0 fixes filed:

1. **BL557** (`9eaf0001`): claude-code workers killed by the restart. Evidence: session timelines show
   `complete -> waiting_input`, then `killed session ended` twice, 17 s after restart; daemon log shows
   summaries saved right after startup. Likely cause: the recovery scan finds the `DATAWATCH_COMPLETE`
   instruction inside the typed task text and declares completion, because the task-echo suppression
   is in memory only and empty after a restart (same defect class as BL548).
2. **BL558** (`01100001`): `decideBootResume` measured activity from the PRD record (3946 s) while the pi
   worker was alive; the park message says "approve it manually" but approve returns "blocked is not
   approvable" and run returns "call /approve first".

Stage 2 (restart while a task is verifying) and stage 3 (restart idle) wait for BL557 and BL558.
`7da90001` stays parked until its pi worker ends, then is cancelled, its task reset (work preserved)
and run.

## Recovery of the parked PRD (7da90001), 20:25

The pi worker (session aa3e) kept running for 87 minutes after the restart and finished its work (files
modified up to 20:11). Resuming the parked PRD took four calls because the advertised action does not
work: `prd-approve` and `prd-run` both fail on a blocked PRD. What worked: `prd-cancel` (PRD), then
`autonomous_prd_cancel_task` on the in-flight task (a task in `verifying` cannot be reset, even with
force), then `autonomous_prd_reset_task` with `force=true`, which put the PRD back to running with the
preserved work restored. BL558 must make this one supported resume action.
