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

## Parity surface

This is an internal storage change with no new user-facing setting. Each surface keeps its current behavior; the intent or exclusion reason is recorded for each.

| Surface | Intent or exclusion |
|---|---|
| REST | Exclusion: no endpoint changes. Existing session endpoints read the same in-memory store. |
| MCP | Exclusion: no tool changes; same reason as REST. |
| CLI | Exclusion: no command changes. Intent: `datawatch` startup must load the old file format unchanged. |
| Comm channel | Exclusion: no command changes; channel events only become cheaper. |
| YAML/config | Exclusion: no new config field planned. If a field is added, it goes in `docs/implementation.md`. |
| PWA | Exclusion: no UI change; session lists come from the same API. |
| Android | Exclusion: no client change; API unchanged. |
| iPhone | Exclusion: no client change; API unchanged. |
