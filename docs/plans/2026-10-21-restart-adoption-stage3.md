# Staged restart validation, stage 3 (2026-10-22 04:16)

Daemon restarted onto 9.5.76 (all of BL557, BL558 and BL561 included) with Automaton `de774e82` mid-run: task 0005 labelled verifying, claude-code worker session 7307 running, four tasks still queued. Snapshots: `/tmp/sv/restart-test/stage3-pre.json`, `stage3-post2.json`.

| Case | Before | After | Result |
|---|---|---|---|
| Claude worker `7307` (task 0005 of `de774e82`) | running, task verifying | same session id, still running, no duplicate session, no summary-on-end log | **kept** (BL561 fix confirmed live) |
| Automaton `de774e82` | running | parked by the boot-resume age gate ("last activity 7989 s ago"), although the worker was alive | **failed** |
| Explicit resume (`prd-resume`) | parked | running, adopted session 7307 | works |

Cause: the gate counts a live worker only for `in_progress` tasks; this task was labelled `verifying`. Filed P0 BL564. The pi-prompt case was not re-run; it passed in stages 1 and 2.
