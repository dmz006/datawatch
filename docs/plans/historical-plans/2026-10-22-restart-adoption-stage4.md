# Staged restart validation, stage 4 (2026-10-22 09:19)

Daemon restarted onto 9.5.86 (includes the BL557, BL558, BL561, BL564 and BL565 fixes) with Automaton `412a0001` mid-run: task 0006 labelled verifying, claude-code worker session `bf53` running, three tasks still queued. Snapshots: `/tmp/sv/restart-test/stage4-pre.json`, `stage4-post.json`.

| Case | Before | After | Result |
|---|---|---|---|
| Claude worker `bf53` (task 0006 of `412a0001`, labelled verifying) | running | same session id, still running, no duplicate session, no summary-on-end log | **kept** |
| Automaton `412a0001` | running | boot-resume: "worker session bf53 is alive, so the PRD is not stale"; executor re-launched, status running | **resumed, not parked** |

Together with stages 1 and 2 (pi workers kept) and stage 3 (Claude worker kept, PRD parked because of the verifying label, fixed by BL564), BL546 live-session adoption now passes for both worker types on a real restart. BL552 is complete.
