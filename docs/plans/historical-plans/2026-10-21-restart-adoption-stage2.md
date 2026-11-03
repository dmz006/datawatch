# Staged restart validation, stage 2 (2026-10-21 22:15)

Daemon restarted onto 9.5.68 (which holds the BL557 and BL558 fixes) while tasks were verifying.
Snapshots: `/tmp/sv/restart-test/stage2-pre.json` (22:15:09), `stage2-post1.json` (+33 s), `stage2-post2.json` (+97 s).

| Case | Worker before | After the restart | Result |
|---|---|---|---|
| GPU Automaton `6eb30001` task 000a, pi-prompt | session a687 | same session a687, same pi process, task still verifying | kept |
| Claude Automaton `98e10001` task 0004 | session d43b | session ended 22:15:24 (summary saved, state killed); task put into waiting_capacity, no session | **failed** |
| Claude Automaton `faac0001` task 0002 | session 1f86 | session ended 22:15:24; a new session 44e7 launched for the same task | **failed** |

Boot-resume itself behaved: every running Automaton was resumed and its executor re-launched, no Automaton was parked. The Claude worker sessions were ended about 7 seconds after start, before the Automaton scan, so the end comes from the session manager's own startup recovery. The BL557 unit tests pass, so the real pane content differs from the test fixture.

Filed P0 BL561 (PRD `b0280001`) with the evidence. Stage 3 (restart idle) waits for that fix.

## Supervisor lesson recorded the same night

`/tmp/sv/trial-merge.sh` used `git checkout --ours` on `internal/server/api.go` and `cmd/datawatch/main.go` when they conflicted, which silently dropped the branch's real changes in those files (found on the BL428 Phase 3 merge, caught by the build). It now resolves only the version-variable hunks. An audit of the last 60 merge commits found no earlier loss.
