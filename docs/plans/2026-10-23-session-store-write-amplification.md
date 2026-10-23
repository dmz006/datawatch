# Session store write amplification

- **Date:** 2026-10-23
- **Version:** 9.5.110 (baseline measured on this version)
- **Scope:** `Store.Save` in `internal/session/store.go` re-marshals every session with `json.MarshalIndent` and rewrites the whole `sessions.json` through `secfile.WriteFile`, under the store lock and the cross-process flock. A single channel-event update therefore costs the size of the entire store. This plan measures that cost, then replaces the whole-file rewrite with storage whose write cost follows the size of the change.

## Phases

| Phase | Description | Status |
|---|---|---|
| 1 | Measurement harness: `internal/session/store_bench_test.go` (seed helper, 200-event burst, benchmark, write-budget test) and baseline numbers | Done |
| 2 | Write coalescing: `Save`/`Delete`/`Mutate` mark the store dirty and wake one background writer that writes at most once per flush interval (default 1 s, `SetFlushInterval` / `WithFlushInterval`, <= 0 = write-through); compact `json.Marshal`; marshal under the read lock, file write and flock outside the store lock; `Flush`/`Close` write synchronously; daemon shutdown and restart paths call `Manager.Close` | Done |
| 3 | Enable the budget assertion (`storeWriteBudgetEnforced = true`) and re-record the numbers in this document | Done (after-coalescing numbers below) |
| 4 | Incremental storage: one encrypted record file per session under `sessions.d/`, a dirty/deleted set driving the background writer, one-time verified migration of `sessions.json` (`internal/session/store_records.go`); the budget factor is tightened to 3x | Done (numbers below) |
| 5 | PRD store: one record per PRD with side files for large text, verified migration of `prds.jsonl` (`internal/autonomous/store_records.go`) | Done (numbers below) |
| 6 | Session archive and per-field size cap: terminal sessions older than the archive age move to `sessions-archive.jsonl`; oversized session fields are cut with a marker and a side file | Done |
| 7 | Config surfaces for the four new settings, documentation, tracker rows, live validation against a sandbox daemon | Done (2026-10-23) |

Shipped in the next patch above `main`.

## Baseline

Measured against the current whole-file rewrite, 2026-10-23, Linux, 48 CPUs, `go test ./internal/session/ -run TestStoreWriteBudget -bench StoreSaveBurst -benchtime 1x`.

Seed: sessions in a `t.TempDir()` store, every tenth session carrying a 100 KB task field. Burst: 200 `Mutate` + `Save` calls (set `LastChannelEventAt`, as `markChannelEventAt` does), round-robin over 10 sessions. Bytes written are the process `wchar` delta from `/proc/self/io` (falls back to the file size after each Save).

| Sessions | Burst size | Elapsed | CPU | Bytes written | Changed data (200 x one record) | Budget (20x) |
|---|---|---|---|---|---|---|
| 400 (reduced, `-short` or `STORE_BENCH_SMALL=1`) | 200 | 1.45 s | 2.15 s | 858,404,761 (about 819 MiB) | 78,000 | 1,560,000 |
| 4000 (full) | 200 | 13.75 s | 17.68 s | 8,585,293,803 (about 8.0 GiB) | 78,000 | 1,560,000 |

The burst writes about 110,000 times the changed data, against a budget of 20 times. At baseline `TestStoreWriteBudget` measured and logged these numbers and skipped, because `storeWriteBudgetEnforced` was `false`.

## After coalescing

Measured the same way (same seed, same 200-event burst, `TestStoreWriteBudget` with `storeWriteBudgetEnforced = true`, passing), 2026-10-23.

| Sessions | Burst size | Elapsed | CPU | Bytes written inside the burst | Budget (20x) |
|---|---|---|---|---|---|
| 400 (reduced) | 200 | 44 us | 49 us | 0 | 1,560,000 |
| 4000 (full) | 200 | 36 us | 44 us | 0 | 1,560,000 |

Before: 1.45 s / 819 MiB (400 sessions) and 13.75 s / 8.0 GiB (4000 sessions). Each `Save` now costs a map update and a channel send, so the caller no longer waits on marshal or disk.

The burst window records 0 bytes because the writer waits one flush interval after the first change, then writes once. That trailing write is still a full rewrite of the store (compact JSON, no indentation), so the cost of a burst is now one store-size write per interval instead of one per event: 200 saves become 1 write (`TestStoreCoalesce` asserts at most 20 file writes for 200 saves). The write budget therefore holds for a burst shorter than the flush interval; a sustained stream of changes still writes the whole file once per second. Phase 4 removes that remaining size dependence.

Crash behaviour: a change is at risk for at most one flush interval; `Close` (daemon shutdown, restart and re-exec paths) and `Flush` write synchronously. Writes remain temp-file-plus-rename under the cross-process flock, and `TestStoreCrashSafety` SIGKILLs a writer subprocess mid-write five times and reloads a complete previous or new state each time.

## After incremental records

Measured the same way as above, 2026-10-23, with the burst window now ending after a `Flush` so it includes the write the background writer makes one interval later (before, that write fell outside the window and showed as 0 bytes). `TestStoreWriteBudget` passes with `storeWriteBudgetFactor = 3`.

| Sessions | Burst size | Elapsed (incl. flush) | CPU | Bytes written | Changed data (200 x one record) | Budget (3x) |
|---|---|---|---|---|---|---|
| 400 (reduced) | 200 | 5 ms | 10 ms | 106,449 | 78,000 | 234,000 |
| 4000 (full) | 200 | 61 ms | 96 ms | 106,468 | 78,000 | 234,000 |

The bytes written no longer depend on the store size: 400 and 4000 sessions write the same 10 records (about 1.4x the changed data; one of the ten burst sessions carries the 100 KB task, the rest are about 400 bytes). Whole-file rewriting would have been 819 MiB and 8.0 GiB for the same burst. The CPU figure is the flush scan described below.

### Storage layout

```
<data_dir>/sessions.json                           old format, only read when no layout exists
<data_dir>/sessions.json.pre-records.bak           copy of the old file taken at migration
<data_dir>/sessions.d/LAYOUT                       marker; its presence selects the record layout
<data_dir>/sessions.d/p.<FullID>.rec               one session, compact JSON (FullID of letters, digits, - and _)
<data_dir>/sessions.d/x.<sha256 of FullID>.rec     same, for any other or overlong FullID
<data_dir>/sessions/<FullID>/session.json          unchanged: per-session working data, still merged on start
```

- The directory is `sessions.d`, not `sessions`, because `sessions/<id>/` already exists for `mergeFromSessionDirs`.
- Each record is written alone through `secfile.WriteFile` (temp file, fsync, rename) and, when the store has a key, encrypted alone. The ID is read from the record, never from the file name. The `LAYOUT` marker is plain text and holds no session data.
- `Save` marks its `FullID` dirty, `Delete` marks it deleted. The writer wakes once per flush interval, marshals only the dirty sessions, skips any whose JSON hash equals the last write, writes the rest under the cross-process flock, and removes the records of deleted sessions. A failed or lock-timed-out batch is requeued.
- `Mutate` carries no ID, so it flags a scan: the next flush hashes every session and writes the ones that changed. This costs CPU proportional to the store (the 96 ms above) but no extra bytes. `Flush` always scans.
- Writes are atomic per record. A crash mid-batch leaves each record at its old or new generation, never torn (`TestStoreRecordsCrashSafety` SIGKILLs a writer five times). A record that cannot be read is skipped with a warning, and `mergeFromSessionDirs` restores it from `sessions/<id>/session.json` when that exists.
- Restart adoption is unchanged: the store loads the same `Session` values, so `ResumeMonitors` and the live-session adoption see what they saw before.

### Migration

On open, if `sessions.d/LAYOUT` is absent and `sessions.json` exists, the store loads the file with the current reader (including `recoverSessionStore` and `mergeFromSessionDirs`), then under the flock: copies `sessions.json` to `sessions.json.pre-records.bak` (never overwriting; a later copy gets `.1`, `.2`, ...), writes every session as a record, reloads the records and requires the same session set, writes `LAYOUT`, and removes `sessions.json`. Any failure before `LAYOUT` leaves `sessions.json` as it was and the store on the old whole-file format; the next start retries. An encrypted `sessions.json` is read with the store key and the backup stays encrypted. A new data directory with no `sessions.json` starts in the record layout.

Rollback: an older daemon does not know `sessions.d` and would start empty. To go back, stop the daemon, copy `sessions.json.pre-records.bak` to `sessions.json`, and remove `sessions.d/`; sessions created after the migration are not in the backup.

## Autonomous PRD store

`internal/autonomous/store.go` had the same shape: `persist()` rewrote `prds.jsonl` in full (every PRD, one PRD up to 13 MB) on every `SavePRD`, `SaveTask`, `MutateTask`, `MutatePRD` and `CommitPRDIfRev`, and rewrote the learnings, profile, tag and retry-profile files with it.

Change (`internal/autonomous/store_records.go`): one record per PRD at `autonomous/prds/<id>.json`, a dirty set marked by each mutator, and only dirty PRDs written, each through a temp file, fsync and rename. A string field over 64 KB (`SetSideFileCap`), and bulky output fields (`RawJudge`, `Output`, `Checkpoint`, `Transcript`, `Error`, `Spec`) over 4 KB, move to content-addressed side files under `prds/<id>.files/`; the record keeps a `datawatch-side-file:v1:` reference that is resolved on load, so `GetPRD`, `SnapshotPRD`, `deepCopyPRD` and every API shape are unchanged. Text over 32 MB (`SetSideFileMax`) is cut and ends with a truncation marker. A record whose bytes equal the last write is skipped. PRD changes no longer touch the other jsonl files. The write still happens under `Store.mu`, as before; it no longer takes any other lock.

Migration: a store with `prds.jsonl` and no `prds/LAYOUT` loads the old file, copies it to `prds.jsonl.pre-records.bak` (a later copy gets `.1`, `.2`; never overwritten), writes every record, reads them back and requires byte-identical re-encoding, writes `LAYOUT`, then removes `prds.jsonl`. Any failure leaves `prds.jsonl` in place and the store on the old whole-file format. An old file next to an existing layout is imported for PRDs the records lack, then renamed `.imported`. Rollback: stop the daemon, copy the `.bak` over `prds.jsonl` and remove `autonomous/prds/`.

Measured 2026-10-23, `go test ./internal/autonomous -run TestPRDStoreIncremental -v`: seed through `CreatePRD` + `SetStories`, then one `MutateTask` status change on one task.

| Seed | PRDs | Old: bytes per change (whole `prds.jsonl`) | New: bytes per change | Files written | Total record bytes on disk |
|---|---|---|---|---|---|
| reduced (`STORE_BENCH_SMALL=1`, 3x4 tasks, 20 KB judge text) | 30 | 7,862,997 | 5,921 | 1 | 177,353 |
| full (5 stories x 6 tasks, 40 KB judge text per task) | 300 | 389,427,723 | 13,982 | 1 | 4,191,719 |

One change writes one PRD record (13,978 bytes before the change, 13,982 after), about 1/27,800 of the old rewrite at 300 PRDs. Tests: `TestPRDStoreIncremental`, `TestPRDStoreMigrateOldFormat`, `TestPRDStoreSideFile`, `TestPRDStoreConcurrent` (`-race`), `TestPRDStoreCrashSafety` (SIGKILL five times, reload each time).

## Final result

Baseline (whole-file rewrite of `sessions.json` and `prds.jsonl`) against the shipped record layout, same seed and same 200-event burst, 2026-10-23, `STORE_BENCH_SMALL=1` where marked.

| Measure | Baseline | After | Change |
|---|---|---|---|
| Session burst, 400 sessions: bytes written | 858,404,761 (819 MiB) | 106,449 | about 1/8,000 |
| Session burst, 400 sessions: elapsed (after: including the flush) | 1.45 s | 5 ms | about 1/290 |
| Session burst, 4000 sessions: bytes written | 8,585,293,803 (8.0 GiB) | 106,468 | about 1/80,000 |
| Session burst, 4000 sessions: elapsed (after: including the flush) | 13.75 s | 61 ms | about 1/225 |
| Bytes written vs changed data (78,000) | about 110,000x | about 1.4x | budget 3x, enforced |
| Bytes written depend on store size | yes | no (400 and 4000 sessions write the same 10 records) | |
| One PRD change, 300 PRDs | 389,427,723 | 13,982 | about 1/27,800 |
| One PRD change, 30 PRDs (`STORE_BENCH_SMALL=1`) | 7,862,997 | 5,921 | about 1/1,330 |

Latest verification run: see the testing tracker section "Session store and PRD store" for the exact commands and results. `docs/operations.md` section "Session and PRD storage layout" is the operator guide.

## Parity surface

The storage change adds four settings (`session.store_flush_interval`, `session.store_archive_after_days`, `session.store_field_cap_kb`, `session.store_side_file_cap_kb`). They are wired through every config surface (commit "Wire store settings through every config surface"; test `internal/server/store_config_surfaces_test.go`). The session and PRD data shapes are unchanged. The intent or exclusion reason is recorded for each surface.

| Surface | Intent or exclusion |
|---|---|
| REST | Intent: the four settings read and write through `GET`/`PUT /api/config` and apply live. `GET /api/sessions?history=1` or `?q=` also returns archived sessions, and `GET /api/sessions/<id>` falls back to the archive. No other endpoint changes. |
| MCP | Intent: `config_set` and `get_config` carry the four settings. Exclusion: no new tool; archived sessions are reached through the existing session tools. |
| CLI | Intent: `datawatch config set session.store_*` and `datawatch config get`. Startup loads the old file formats and migrates them once. Exclusion: no command to restore an archived session (see operations.md). |
| Comm channel | Intent: `configure session.store_*` sets the four settings. Exclusion: no new command; channel events only become cheaper. |
| YAML/config | Intent: the four fields are in `docs/config-reference.yaml`, `docs/implementation.md` and the generated template. |
| PWA | Intent: the four settings appear in the Settings page with all five locales. The History view reloads the session list so archived sessions are included. |
| Android | Exclusion: no client change; API unchanged. |
| iPhone | Exclusion: no client change; API unchanged. |
