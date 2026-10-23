# Session store write amplification

- **Date:** 2026-10-23
- **Version:** 9.5.110 (baseline measured on this version)
- **Scope:** `Store.Save` in `internal/session/store.go` re-marshals every session with `json.MarshalIndent` and rewrites the whole `sessions.json` through `secfile.WriteFile`, under the store lock and the cross-process flock. A single channel-event update therefore costs the size of the entire store. This plan measures that cost, then replaces the whole-file rewrite with storage whose write cost follows the size of the change.

## Phases

| Phase | Description | Status |
|---|---|---|
| 1 | Measurement harness: `internal/session/store_bench_test.go` (seed helper, 200-event burst, benchmark, write-budget test) and baseline numbers | Done |
| 2 | Incremental storage: persist only changed session records; keep the on-disk format readable by older daemons or provide a migration | Not started |
| 3 | Enable the budget assertion (`storeWriteBudgetEnforced = true`) and re-record the numbers in this document | Not started |

## Baseline

Measured against the current whole-file rewrite, 2026-10-23, Linux, 48 CPUs, `go test ./internal/session/ -run TestStoreWriteBudget -bench StoreSaveBurst -benchtime 1x`.

Seed: sessions in a `t.TempDir()` store, every tenth session carrying a 100 KB task field. Burst: 200 `Mutate` + `Save` calls (set `LastChannelEventAt`, as `markChannelEventAt` does), round-robin over 10 sessions. Bytes written are the process `wchar` delta from `/proc/self/io` (falls back to the file size after each Save).

| Sessions | Burst size | Elapsed | CPU | Bytes written | Changed data (200 x one record) | Budget (20x) |
|---|---|---|---|---|---|---|
| 400 (reduced, `-short` or `STORE_BENCH_SMALL=1`) | 200 | 1.45 s | 2.15 s | 858,404,761 (about 819 MiB) | 78,000 | 1,560,000 |
| 4000 (full) | 200 | 13.75 s | 17.68 s | 8,585,293,803 (about 8.0 GiB) | 78,000 | 1,560,000 |

The burst writes about 110,000 times the changed data, against a budget of 20 times. `TestStoreWriteBudget` currently measures and logs these numbers and skips, because `storeWriteBudgetEnforced` is `false`. It must pass, with the constant set to `true`, once Phase 2 lands.

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
