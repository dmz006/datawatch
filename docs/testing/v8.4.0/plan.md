# Test Plan — v8.4.0

**Version**: v8.4.0  
**Sprint**: T42a/b/c — Discussion Scopes (BL332)  
**Stories**: TS-719–TS-750 (32 stories)  
**Go unit tests**: 516 (server + memory + autonomous packages)

## Scope

| BL | Feature | Stories | Go tests |
|---|---|---|---|
| BL332 T42a | Discussion scope constant + WAL + REST API | TS-719–TS-732 (14 stories) | bl332_discussion_scope_test.go (4 tests) |
| BL332 T42b | Federated sync + throttle + conflict API | TS-733–TS-742 (10 stories) | bl332_discussion_sync_test.go (3 tests) |
| BL332 T42c | MCP tools + CLI + PWA + locale | TS-743–TS-750 (8 stories) | 0 new tests |

## New Go tests in v8.4.0

| File | Tests | What they cover |
|---|---|---|
| `internal/server/bl332_discussion_scope_test.go` | 4 | GET /api/memory/discussion (empty list), POST /api/memory/discussion/{id} creates entry, GET /api/memory/discussion/{id}/wal returns WAL entries, path traversal in discussion ID rejected 400 |
| `internal/server/bl332_discussion_sync_test.go` | 3 | Participant list round-trip (PUT then GET), throttle enforcement (61st write in window returns 429), loop prevention (self-originated write not re-synced) |

**Running total:**  
- Server + memory + autonomous packages: 509 (v8.3.0 baseline) + 4 (T42a) + 3 (T42b) + 0 (T42c) = **516**

## Federation cap enforcement verified

BL332 discussion scope endpoints use the `comm:*` capability surface:

| Endpoint | Method | Required cap |
|---|---|---|
| `GET /api/memory/discussion` | GET | `comm:read` |
| `GET /api/memory/discussion/{id}` | GET | `comm:read` |
| `GET /api/memory/discussion/{id}/wal` | GET | `comm:read` |
| `GET /api/memory/discussion/{id}/participants` | GET | `comm:read` |
| `GET /api/memory/discussion/{id}/conflicts` | GET | `comm:read` |
| `POST /api/memory/discussion/{id}` | POST | `comm:write` (+ throttled at 60 ops/min per peer) |
| `DELETE /api/memory/discussion/{id}` | DELETE | `comm:write` |
| `PUT /api/memory/discussion/{id}/participants` | PUT | `comm:write` |
| `POST /api/memory/discussion/{id}/conflicts/resolve` | POST | `comm:write` |

See [cookbook.md](cookbook.md) § Federation Access Matrix for the full endpoint→cap table.

## New structs and config fields

### BL332 T42a

- `ScopeDiscussion` constant added to `internal/memory/scopes.go`
  - Resolves to `(projectDir="", role="discussion/<id>", sessionID="")`
- New REST endpoints for discussion scope CRUD and WAL access
- Per-discussion WAL at `~/.datawatch/discussions/<id>/wal.jsonl`
- Per-discussion write mutex using `sync.Map` (concurrent writes from sync fan-out are safe)

### BL332 T42b

- Participant peer list stored per discussion (PUT to set, GET to read)
- Push-on-write: on POST success, async fan-out to participant peers via their `/api/push/<discussion-id>`
- Loop prevention fields in WAL entries: `origin_peer` + `origin_wal_seq`; self-originated writes are not re-fanned
- Throttle: token bucket, 60 ops/min per peer; returns 429 when exceeded
- Conflict detection: concurrent writes to the same discussion within a time window are flagged
- Conflict resolution: POST to mark a winning entry; losers are tombstoned

### BL332 T42c

- 4 new MCP tools: `memory_discussion_write`, `memory_discussion_recall`, `memory_discussion_wal`, `memory_discussion_participants`
- New CLI subcommands: `datawatch memory discussion list|write|recall|wal|participants`
- PWA: Discussion Scopes card in Settings → General
- Locale: 6 new keys in all 5 locale files (en, de, es, fr, ja)

## PWA additions

- **BL332**: Discussion Scopes card in Settings → General. Displays a list of known discussion IDs, a New Discussion form, and per-discussion Recall and Participants buttons.

## Run

```sh
# Go unit tests
go test ./internal/server/... ./internal/memory/... ./internal/autonomous/... -timeout 90s

# E2E against a live daemon
# See cookbook.md for full REST/CLI/MCP/PWA story scripts
```
