# E2E Test Cookbook — v8.4.0

**Version**: v8.4.0  
**Sprint**: T42a/b/c — Discussion Scopes (BL332)  
**Stories**: TS-719–TS-750  
**Last Run**: —  
**Pass Rate**: — (0/32)  
**Status**: 📋 Ready to run

---

## Prerequisites

```sh
export DW_HOST=http://localhost:8080
export DW_TOKEN=<your-daemon-token>
alias dw="curl -sk -H 'Content-Type: application/json' -H 'Authorization: Bearer $DW_TOKEN'"
```

---

## T42a — BL332 Core: Discussion scope + WAL + REST API (TS-719–TS-732)

Discussion scopes provide per-discussion memory namespaces with a durable write-ahead log.
Each discussion lives at `~/.datawatch/discussions/<id>/wal.jsonl` and is accessible via
REST, MCP, and CLI. The scope constant `ScopeDiscussion` resolves to
`(projectDir="", role="discussion/<id>", sessionID="")`.

```sh
# Helper — discussion base endpoint
DISC="$DW_HOST/api/memory/discussion"
```

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-719 | REST | GET /api/memory/discussion returns empty list on fresh node | 📋 planned |
| TS-720 | REST | POST /api/memory/discussion/test-1 with {content:"hello"} returns 200 | 📋 planned |
| TS-721 | REST | GET /api/memory/discussion/test-1 returns the written entry | 📋 planned |
| TS-722 | REST | GET /api/memory/discussion/test-1/wal shows 1 entry with seq=1 | 📋 planned |
| TS-723 | REST | DELETE /api/memory/discussion/test-1 removes all entries | 📋 planned |
| TS-724 | REST | POST with path traversal in id (../etc) is rejected 400 | 📋 planned |
| TS-725 | REST | GET /api/memory/discussion lists test-1 after write | 📋 planned |
| TS-726 | Federation | GET /api/memory/discussion: comm:read required; monitor peer → 403 | 📋 planned |
| TS-727 | Federation | POST /api/memory/discussion/{id}: comm:write required; read-only peer → 403 | 📋 planned |
| TS-728 | MCP | memory_discussion_write tool creates entry | 📋 planned |
| TS-729 | MCP | memory_discussion_recall tool returns entry | 📋 planned |
| TS-730 | MCP | memory_discussion_wal tool returns WAL entries | 📋 planned |
| TS-731 | CLI | datawatch memory discussion write test-1 "hello" exits 0 | 📋 planned |
| TS-732 | CLI | datawatch memory discussion recall test-1 exits 0 | 📋 planned |

**REST happy path:**

```sh
# TS-719 — fresh node returns empty list
dw $DISC | jq .
# {"discussions":[]}

# TS-720 — write an entry to discussion test-1
dw -X POST $DISC/test-1 \
  -d '{"content": "hello from e2e"}' | jq .
# {"ok":true,"seq":1}

# TS-721 — GET the discussion entries
dw $DISC/test-1 | jq .
# {"id":"test-1","entries":[{"seq":1,"content":"hello from e2e","ts":"..."}]}

# TS-722 — GET the WAL directly
dw $DISC/test-1/wal | jq .
# {"id":"test-1","entries":[{"seq":1,"content":"hello from e2e","ts":"...","origin_peer":"","origin_wal_seq":0}]}

# TS-725 — list shows test-1 after write
dw $DISC | jq .discussions
# ["test-1"]

# TS-723 — DELETE removes all entries
dw -X DELETE $DISC/test-1 | jq .
# {"ok":true}

# Verify gone
dw $DISC | jq .
# {"discussions":[]}

# TS-724 — path traversal rejected
dw -X POST "$DISC/../etc" \
  -d '{"content":"bad"}' \
  -w "%{http_code}"
# 400

# TS-726 — comm:read required for GET (monitor peer lacks comm:read → 403)
curl -sk -H "Authorization: Bearer <monitor-peer-token>" \
  "$DW_HOST/api/memory/discussion"
# → 403 Forbidden

# TS-727 — comm:write required for POST (read-only peer lacks comm:write → 403)
curl -sk -X POST \
  -H "Authorization: Bearer <read-only-peer-token>" \
  -H "Content-Type: application/json" \
  "$DW_HOST/api/memory/discussion/test-2" \
  -d '{"content":"unauthorized write"}' \
  -w "%{http_code}"
# 403
```

**CLI:**

```sh
# TS-731 — write via CLI
datawatch memory discussion write test-1 "hello from cli"
# exit 0

# TS-732 — recall via CLI
datawatch memory discussion recall test-1
# exit 0; prints entries
```

**MCP (via Claude Code):**

```
# TS-728
memory_discussion_write(id="test-1", content="hello from mcp")
# → {"ok":true,"seq":N}

# TS-729
memory_discussion_recall(id="test-1")
# → {"id":"test-1","entries":[...]}

# TS-730
memory_discussion_wal(id="test-1")
# → {"id":"test-1","entries":[...]}
```

---

## T42b — BL332 Sync: Federated sync + throttle + conflict API (TS-733–TS-742)

Federated discussion sync fans out writes to participant peers on each successful POST.
Loop prevention is enforced via `origin_peer` + `origin_wal_seq` in the WAL; self-originated
entries are not re-fanned. A token-bucket throttle limits each peer to 60 ops/min.

```sh
# Helpers
DISC="$DW_HOST/api/memory/discussion"
DISCUSSION_ID="collab-1"
```

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-733 | REST | GET /api/memory/discussion/collab-1/participants returns empty list | 📋 planned |
| TS-734 | REST | PUT /api/memory/discussion/collab-1/participants sets peer list; GET returns it | 📋 planned |
| TS-735 | REST | POST triggers sync to participants (mock peer receives push) | 📋 planned |
| TS-736 | REST | origin_peer loop prevention: write with own hostname not re-synced | 📋 planned |
| TS-737 | REST | 61st write within 60s from same peer returns 429 | 📋 planned |
| TS-738 | REST | GET /api/memory/discussion/collab-1/conflicts returns conflicts | 📋 planned |
| TS-739 | REST | POST /api/memory/discussion/collab-1/conflicts/resolve marks winner | 📋 planned |
| TS-740 | CLI | datawatch memory discussion participants collab-1 --set peer1,peer2 exits 0 | 📋 planned |
| TS-741 | MCP | memory_discussion_participants sets and gets peer list | 📋 planned |
| TS-742 | Federation | PUT /api/memory/discussion/{id}/participants: comm:write required; read-only → 403 | 📋 planned |

**REST happy path:**

```sh
# TS-733 — fresh discussion has empty participant list
dw $DISC/$DISCUSSION_ID/participants | jq .
# {"id":"collab-1","participants":[]}

# TS-734 — set participant list
dw -X PUT $DISC/$DISCUSSION_ID/participants \
  -d '{"participants":["peer-alpha","peer-beta"]}' | jq .
# {"ok":true}

# Verify GET returns updated list
dw $DISC/$DISCUSSION_ID/participants | jq .participants
# ["peer-alpha","peer-beta"]

# TS-735 — write an entry; participant peers receive push (requires mock peer at peer-alpha)
# Set up a listener on the mock peer, then:
dw -X POST $DISC/$DISCUSSION_ID \
  -d '{"content":"synced entry"}' | jq .
# {"ok":true,"seq":1}
# Mock peer should have received POST /api/push/collab-1 with the entry

# TS-736 — self-origin loop prevention
# Write with origin_peer set to this daemon's own hostname
dw -X POST $DISC/$DISCUSSION_ID \
  -d '{"content":"self write","origin_peer":"this-host","origin_wal_seq":1}' | jq .
# {"ok":true,"seq":2}
# Verify no outbound sync was triggered to participants for this write

# TS-737 — throttle: 60 writes allowed, 61st returns 429
for i in $(seq 1 60); do
  dw -X POST $DISC/$DISCUSSION_ID \
    -d "{\"content\":\"write $i\"}" > /dev/null
done
# 61st write:
dw -X POST $DISC/$DISCUSSION_ID \
  -d '{"content":"over throttle"}' \
  -w "%{http_code}"
# 429

# TS-738 — conflicts (concurrent writes from two peers produce a conflict)
# After simulating two concurrent writes, check the conflicts list:
dw $DISC/$DISCUSSION_ID/conflicts | jq .
# {"id":"collab-1","conflicts":[{"seq_a":N,"seq_b":M,"ts":"..."}]}

# TS-739 — resolve: mark seq_a as the winner
dw -X POST $DISC/$DISCUSSION_ID/conflicts/resolve \
  -d '{"winner_seq":1,"loser_seq":2}' | jq .
# {"ok":true}

# Verify conflicts list is now empty
dw $DISC/$DISCUSSION_ID/conflicts | jq .conflicts
# []

# TS-742 — comm:write required for PUT participants (read-only peer → 403)
curl -sk -X PUT \
  -H "Authorization: Bearer <read-only-peer-token>" \
  -H "Content-Type: application/json" \
  "$DW_HOST/api/memory/discussion/$DISCUSSION_ID/participants" \
  -d '{"participants":["attacker-peer"]}' \
  -w "%{http_code}"
# 403
```

**CLI:**

```sh
# TS-740 — set participants via CLI
datawatch memory discussion participants collab-1 --set peer-alpha,peer-beta
# exit 0

# Verify
datawatch memory discussion participants collab-1
# peers: peer-alpha, peer-beta
```

**MCP (via Claude Code):**

```
# TS-741
memory_discussion_participants(id="collab-1", participants=["peer-alpha","peer-beta"])
# → {"ok":true}

memory_discussion_participants(id="collab-1")
# → {"id":"collab-1","participants":["peer-alpha","peer-beta"]}
```

---

## T42c — BL332 Surfaces: MCP + CLI + PWA + Locale (TS-743–TS-750)

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-743 | PWA | Discussion Scopes card renders in Settings → General | 📋 planned |
| TS-744 | PWA | Discussion list shows IDs after writes | 📋 planned |
| TS-745 | PWA | New Discussion form creates a discussion scope | 📋 planned |
| TS-746 | PWA | Recall button in discussion card fetches entries | 📋 planned |
| TS-747 | Locale | discussion_scope_title renders in all 5 locales | 📋 planned |
| TS-748 | Locale | discussion_participants renders in all 5 locales | 📋 planned |
| TS-749 | MCP | All 4 discussion MCP tools appear in server tool list | 📋 planned |
| TS-750 | REST | Full roundtrip: write → wal → participants → conflict → resolve | 📋 planned |

**PWA walkthrough (TS-743–TS-746):**

1. Open Settings → General in the PWA.
2. Scroll to the **Discussion Scopes** card (TS-743 — must render).
3. Write an entry to any discussion via REST or CLI; reload the PWA.
4. The card's discussion list must show the discussion ID (TS-744).
5. Use the **New Discussion** form to create a new discussion ID (TS-745).
   - Enter an ID in the form field, press Create.
   - Verify `GET /api/memory/discussion` includes the new ID.
6. Click the **Recall** button on any discussion row (TS-746).
   - The card expands to show the entries returned by `GET /api/memory/discussion/{id}`.

**Locale check (TS-747, TS-748):**

Load the PWA with each locale (`?lang=en`, `?lang=de`, `?lang=es`, `?lang=fr`, `?lang=ja`).
Verify:
- `discussion_scope_title` — the Discussion Scopes card header renders in each locale.
- `discussion_participants` — the participants sub-section label renders in each locale.

**MCP tool list (TS-749):**

```sh
# Verify all 4 discussion tools appear in the MCP tool catalogue
curl -sk -H "Authorization: Bearer $DW_TOKEN" \
  "$DW_HOST/api/mcp/docs" | jq '[.tools[] | select(.name | startswith("memory_discussion"))] | map(.name)'
# ["memory_discussion_participants","memory_discussion_recall","memory_discussion_wal","memory_discussion_write"]
```

**Full roundtrip (TS-750):**

```sh
DISC="$DW_HOST/api/memory/discussion"
ID="roundtrip-e2e"

# 1. Write two entries
dw -X POST $DISC/$ID -d '{"content":"entry A"}' | jq .seq
# 1
dw -X POST $DISC/$ID -d '{"content":"entry B"}' | jq .seq
# 2

# 2. Check WAL
dw $DISC/$ID/wal | jq '.entries | length'
# 2

# 3. Set participants
dw -X PUT $DISC/$ID/participants \
  -d '{"participants":["mock-peer"]}' | jq .ok
# true

# 4. Simulate a conflict (mock concurrent write from peer)
# Inject a conflicting WAL entry by writing from two origins simultaneously.
# Then verify conflicts detected:
dw $DISC/$ID/conflicts | jq '.conflicts | length'
# >= 0 (0 if no concurrent writes; run conflict simulation to see > 0)

# 5. Resolve (if conflicts present)
# dw -X POST $DISC/$ID/conflicts/resolve \
#   -d '{"winner_seq":1,"loser_seq":2}' | jq .ok

# 6. Verify discussion still readable
dw $DISC/$ID | jq '.entries | length'
# >= 2
```

---

## Federation Access Matrix — v8.4.0

### New endpoints (BL332)

| Endpoint | Method | Required cap | `comm-bridge` | `read-only` | `config-admin` | `full-control` |
|---|---|---|---|---|---|---|
| `/api/memory/discussion` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/memory/discussion/{id}` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/memory/discussion/{id}/wal` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/memory/discussion/{id}/participants` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/memory/discussion/{id}/conflicts` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/memory/discussion/{id}` | POST | `comm:write` | ✓ | — | — | ✓ |
| `/api/memory/discussion/{id}` | DELETE | `comm:write` | ✓ | — | — | ✓ |
| `/api/memory/discussion/{id}/participants` | PUT | `comm:write` | ✓ | — | — | ✓ |
| `/api/memory/discussion/{id}/conflicts/resolve` | POST | `comm:write` | ✓ | — | — | ✓ |

**Notes:**
- `comm-bridge` (comm:read + comm:write) has full access to all discussion endpoints.
- `read-only` can list and read discussion entries and WAL but cannot write, delete, or modify participants.
- `config-admin` does not have `comm:*` caps; it cannot access discussion endpoints by default.
- `full-control` has all 50 capabilities including comm:read + comm:write.
- The 60 ops/min throttle applies per peer on POST `/api/memory/discussion/{id}` regardless of capability.

### Inherited from prior releases (unchanged)

| Endpoint | Method | Required cap |
|---|---|---|
| `/api/channel/routing` | GET | `comm:read` |
| `/api/channel/routing` | PUT | `comm:write` |
| `/api/files/meta` | GET | `config:read` |
| `/api/files` | POST (upload) | `config:write` |
| `/api/files` | DELETE | `config:write` |
| `/api/push/register` | POST | `comm:write` |
| `/api/push/notify` | POST | `comm:write` |

---

## Feature Coverage Summary

| Feature | REST | CLI | MCP | PWA | Locale | Federation |
|---|---|---|---|---|---|---|
| BL332 T42a: core scope + WAL | TS-719–725 | TS-731–732 | TS-728–730 | TS-743–746 | TS-747–748 | TS-726–727 |
| BL332 T42b: sync + throttle + conflicts | TS-733–739 | TS-740 | TS-741 | — | — | TS-742 |
| BL332 T42c: surfaces + roundtrip | TS-750 | — | TS-749 | TS-743–746 | TS-747–748 | — |
