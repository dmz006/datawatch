# Staged restart validation, stage 10 (2026-10-23 15:04)

Restart onto the build that adds per-record session and PRD stores, with two PRDs mid-run and a task verifying in each. Snapshots: `/tmp/sv/restart-test/stage10-before.json` and `stage10-after.json`.

| PRD | Task | Session before | Session after | Worker process before | Worker process after |
|---|---|---|---|---|---|
| dbd5000a (pi-prompt) | verifying | 7ca3 | 7ca3 | pi 578572 | pi 578572 |
| 5a330001 (pi-prompt) | verifying | cd33 | cd33 | pi 991802 | pi 991802 |

- Result: both in-flight sessions kept, no duplicate session, no task reset, no worker process restarted.
- The first start on the new build migrated the 34 MB `sessions.json` into `sessions.d/` (3,921 records) and `prds.jsonl` (354 MB) into `autonomous/prds/`. The originals stay as `sessions.json.pre-records.bak` and `prds.jsonl.pre-records.bak`.
- The capacity API answered in about 4 ms after the restart, against 130 ms to 6 s before the store change.
- The tmux server was untouched (NRestarts 3).
- Side finding: while preparing the run, an explicit `--model qwen3.8-64k:27b` on a PRD made pi fail with "ambiguous across providers". Leave the PRD model empty on pi-prompt PRDs.
