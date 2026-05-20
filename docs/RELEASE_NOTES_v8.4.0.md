# Release Notes — v8.4.0

**Date**: 2026-05-19  
**Sprint**: T42 — Discussion Scopes (BL332)  
**Stories**: 32 E2E stories (TS-719–TS-750), 20/20 live pass + 12 code-reviewed

---

## What's new

### BL332 — Discussion Scopes

A new `discussion` memory scope that multiple federated peers can share. Unlike session-local or project-shared scopes, a discussion scope has no single owner — any participant can write to it, and writes sync to all registered participants.

**Architecture**:
- Append-only JSONL WAL at `~/.datawatch/discussions/<id>/wal.jsonl`
- Each WAL entry: `{seq, content, role, timestamp, origin_peer, op}`
- Per-discussion write mutex prevents concurrent WAL corruption
- Participant list at `~/.datawatch/discussions/<id>/participants.json`

**Conflict detection**:  
The daemon watches for writes from different `origin_peer` values with the same first 64 characters of content within 5 seconds. These appear as conflicts at `GET /api/memory/discussion/{id}/conflicts`. Resolve with `POST …/conflicts/resolve`.

**Rate throttle**:  
60 writes/min per Bearer token (sync.Map token bucket). Returns HTTP 429 when exceeded. In a no-auth deployment, all requests share one bucket — use per-peer tokens for correct isolation.

**Participant sync**:  
Every write fans out asynchronously to all registered participants via `POST /api/push/<discussion-id>`. Register participants with `PUT /api/memory/discussion/{id}/participants`.

**REST surface**:
| Method | Path | Cap | Description |
|---|---|---|---|
| GET | `/api/memory/discussion` | CommRead | List all local discussion IDs |
| GET | `/api/memory/discussion/{id}` | CommRead | Recall memories in scope |
| POST | `/api/memory/discussion/{id}` | CommWrite | Append WAL entry + store memory |
| DELETE | `/api/memory/discussion/{id}` | CommWrite | Delete discussion and WAL |
| GET | `/api/memory/discussion/{id}/wal` | CommRead | Full WAL history |
| GET | `/api/memory/discussion/{id}/conflicts` | CommRead | Detect conflicts |
| POST | `/api/memory/discussion/{id}/conflicts/resolve` | CommWrite | Append resolution marker |
| GET | `/api/memory/discussion/{id}/participants` | CommRead | List participant peers |
| PUT | `/api/memory/discussion/{id}/participants` | CommWrite | Set participant list |

**MCP tools**: `memory_discussion_write`, `memory_discussion_recall`, `memory_discussion_wal`, `memory_discussion_participants`

**CLI**: `datawatch memory discussion {list,write,recall,wal,participants}`

**PWA**: Settings → General → Discussion Scopes card

How-to guide: [docs/howto/discussion-scopes.md](howto/discussion-scopes.md)

---

## Fixes

- `datawatch memory discussion` CLI subcommand was missing from the binary due to Cobra duplicate-name resolution. `newDiscussionCmd()` was added to `newMemoryCliCmd()` in `main.go` to ensure it appears in the merged `memory` command tree.

---

## PostgreSQL / shared vector DB note

Discussion scopes use the WAL file system (JSONL at `~/.datawatch/discussions/`) — separate from the PostgreSQL vector index. Two federated systems sharing a Postgres `pgvector` database will not have WAL conflicts because each instance writes to its own WAL file. Memory entries from discussion writes ARE indexed into the shared Postgres store under the `discussion/<id>` scope key, so semantic recall across the shared DB works as expected. The conflict detection mechanism only fires within a single discussion's WAL.
