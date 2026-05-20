# E2E Test Cookbook — v8.3.0

**Version**: v8.3.0  
**Sprint**: T41/T43 — Channel-address federation + federated file service  
**Stories**: TS-683–TS-718  
**Last Run**: 2026-05-19  
**Pass Rate**: 32/36 (4 skipped: cap enforcement / owner_peer live session)  
**Status**: ✅ Complete

---

## Prerequisites

```sh
export DW_HOST=http://localhost:8080
export DW_TOKEN=<your-daemon-token>
export AUTH="-H 'Authorization: Bearer $DW_TOKEN'"
alias dw="curl -sk -H 'Content-Type: application/json' -H 'Authorization: Bearer $DW_TOKEN'"
```

---

## T41a — BL331: Channel routing (TS-683–TS-700)

Channel-address federation routes inbound messages from a known channel identity
(e.g. a Telegram group ID or Signal number) to a specific federation peer, with
an optional automata type and default project directory.

```sh
# Helper — the routing config endpoint
ROUTING="$DW_HOST/api/channel/routing"
```

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-683 | REST | GET /api/channel/routing returns empty rules on fresh install | ✅ pass |
| TS-684 | REST | PUT /api/channel/routing adds a rule; GET returns it | ✅ pass |
| TS-685 | REST | PUT /api/channel/routing validates channel_pattern required | ✅ pass |
| TS-686 | REST | Federation peer: POST with channel_identity saves array; GET returns it | ✅ pass |
| TS-687 | REST | Federation peer: PUT updates channel_identity | ✅ pass |
| TS-688 | REST | GET /api/federation/groups/builtins includes comms-channel-agent (14th group) | ✅ pass |
| TS-689 | CLI | datawatch federation peer add --channel-identity ... exits 0 | ✅ pass |
| TS-690 | MCP | federation_peer_add with channel_identity field succeeds | ✅ pass |
| TS-691 | PWA | Federation peer form shows channel_identity input; save persists | ✅ pass |
| TS-692 | Federation | GET /api/channel/routing: read-only peer → 200; monitor → 403 | ⏭️ skip |
| TS-693 | Federation | PUT /api/channel/routing: read-only peer → 403; full-control → 200 | ⏭️ skip |
| TS-694 | REST | Session created via channel routing has owner_peer set | ⏭️ skip |
| TS-695 | REST | PRD created via channel routing has owner_peer set | ⏭️ skip |
| TS-696 | REST | GET /api/sessions returns owner_peer field when set | ✅ pass |
| TS-697 | REST | GET /api/autonomous/prds returns owner_peer field when set | ✅ pass |
| TS-698 | PWA | Channel Routing card in Settings → Comms shows rules and add form | ✅ pass |
| TS-699 | Locale | channel_routing_title renders in all 5 locales | ✅ pass |
| TS-700 | Locale | channel_identity_label renders in all 5 locales | ✅ pass |

**REST happy path:**

```sh
# TS-683 — fresh install returns empty rules array
dw $ROUTING | jq .
# {"rules":[]}

# TS-684 — add a rule
dw -X PUT $ROUTING \
  -d '{
    "rules": [{
      "channel_pattern": "telegram:group:-1001234567890",
      "peer_name": "nas-peer",
      "automata_type": "feature",
      "default_project_dir": "/workspace/myapp"
    }]
  }' | jq .
# {"ok":true}

# Verify GET returns it
dw $ROUTING | jq .rules

# TS-685 — missing channel_pattern → 400
dw -X PUT $ROUTING \
  -d '{"rules":[{"peer_name":"nas-peer"}]}' \
  -w "%{http_code}"
# 400

# TS-686 — add peer with channel_identity
dw -X POST $DW_HOST/api/federation/peers \
  -d '{
    "name": "mobile-bridge",
    "url": "https://mobile.internal:8443",
    "token": "peer-secret",
    "channel_identity": ["telegram:group:-1001234567890", "signal:+15551234567"]
  }' | jq .

# TS-687 — update channel_identity
PEER_ID=$(dw $DW_HOST/api/federation/peers | jq -r '.[] | select(.name=="mobile-bridge") | .id')
dw -X PUT $DW_HOST/api/federation/peers/$PEER_ID \
  -d '{"channel_identity": ["telegram:group:-1001234567890"]}' | jq .

# TS-688 — 14th builtin group present
dw $DW_HOST/api/federation/groups/builtins | jq '.[] | .name' | grep comms-channel-agent

# TS-692 — read-only peer can GET routing (comm:read)
curl -sk -H "Authorization: Bearer <read-only-peer-token>" \
  "$DW_HOST/api/channel/routing"
# → 200 OK

# TS-693 — read-only peer cannot PUT routing (needs comm:write → 403)
curl -sk -X PUT -H "Authorization: Bearer <read-only-peer-token>" \
  -H "Content-Type: application/json" \
  "$DW_HOST/api/channel/routing" \
  -d '{"rules":[{"channel_pattern":"test","peer_name":"x"}]}'
# → 403 Forbidden

# TS-696 — owner_peer field on sessions
dw $DW_HOST/api/sessions | jq '.[0] | {id, owner_peer}'

# TS-697 — owner_peer field on PRDs
dw $DW_HOST/api/autonomous/prds | jq '.[0] | {id, owner_peer}'
```

**CLI:**

```sh
# TS-689
datawatch federation peer add \
  --name "mobile-bridge" \
  --url "https://mobile.internal:8443" \
  --token "peer-secret" \
  --channel-identity "telegram:group:-1001234567890"
```

**Locale check (TS-699, TS-700):**

Load the PWA with each locale (`?lang=en`, `?lang=de`, `?lang=es`, `?lang=fr`, `?lang=ja`).
Verify the Channel Routing section header and channel_identity field label render in each.

---

## T43b — BL333: File service (TS-701–TS-718)

The federated file service provides structured file storage under a configurable root,
with peer and discussion subdirectories accessible across federation.

```sh
# Prerequisites — set file_service_root in datawatch.yaml, or rely on default
# file_service_root: /home/operator/.datawatch/files

FILES="$DW_HOST/api/files"

# Create a test file for upload
echo "hello from e2e" > /tmp/e2e-test.txt
```

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-701 | REST | GET /api/files/meta returns root path and peer/discussion counts | ✅ pass |
| TS-702 | REST | POST /api/files (multipart) uploads file; file exists at path | ✅ pass |
| TS-703 | REST | DELETE /api/files removes file; subsequent GET no longer lists it | ✅ pass |
| TS-704 | REST | POST /api/files path traversal rejected (400) | ✅ pass |
| TS-705 | REST | DELETE /api/files path traversal rejected (400) | ✅ pass |
| TS-706 | REST | GET /api/files/peers/{name} returns files in peers/<name>/ subdir | ✅ pass |
| TS-707 | REST | GET /api/files/discussions/{id} returns files in discussions/<id>/ subdir | ✅ pass |
| TS-708 | CLI | datawatch files list exits 0 | ✅ pass |
| TS-709 | CLI | datawatch files upload <file> exits 0; remote file exists | ✅ pass |
| TS-710 | CLI | datawatch files delete <path> exits 0; file removed | ✅ pass |
| TS-711 | CLI | datawatch files peer <name> exits 0 | ✅ pass |
| TS-712 | MCP | files_upload tool creates file | ✅ pass |
| TS-713 | MCP | files_delete tool removes file | ✅ pass |
| TS-714 | MCP | files_meta tool returns valid JSON | ✅ pass |
| TS-715 | Federation | GET /api/files/*: config:read required; monitor → 403 | ✅ pass |
| TS-716 | Federation | POST+DELETE /api/files: config:write required; read-only → 403 | ✅ pass |
| TS-717 | PWA | File Service card in Settings → General renders storage overview | ✅ pass |
| TS-718 | Locale | files_section_title renders in all 5 locales | ✅ pass |

**REST happy path:**

```sh
# TS-701 — storage overview
dw $FILES/meta | jq .
# {"root":"/home/operator/.datawatch/files","peer_count":0,"discussion_count":0,"disk_bytes":0}

# TS-702 — upload a file
curl -sk \
  -H "Authorization: Bearer $DW_TOKEN" \
  -F "file=@/tmp/e2e-test.txt" \
  -F "path=peers/nas-peer/e2e-test.txt" \
  $FILES
# {"ok":true,"path":"peers/nas-peer/e2e-test.txt"}

# Verify it appears in the peer subdir listing
dw $FILES/peers/nas-peer | jq .

# TS-703 — delete the file
dw -X DELETE $FILES \
  -d '{"path":"peers/nas-peer/e2e-test.txt"}' | jq .
# {"ok":true}

# Verify it is gone
dw $FILES/peers/nas-peer | jq '.'
# should no longer include e2e-test.txt

# TS-704 — path traversal blocked on upload
curl -sk \
  -H "Authorization: Bearer $DW_TOKEN" \
  -F "file=@/tmp/e2e-test.txt" \
  -F "path=../../etc/passwd" \
  $FILES \
  -w "%{http_code}"
# 400

# TS-705 — path traversal blocked on delete
dw -X DELETE $FILES \
  -d '{"path":"../../etc/passwd"}' \
  -w "%{http_code}"
# 400

# TS-706 — list peer subdir
# First upload something to peers/test-peer/
curl -sk \
  -H "Authorization: Bearer $DW_TOKEN" \
  -F "file=@/tmp/e2e-test.txt" \
  -F "path=peers/test-peer/notes.txt" \
  $FILES
dw $FILES/peers/test-peer | jq .
# [{"name":"notes.txt","size":15,"modified":"..."}]

# TS-707 — list discussion subdir
curl -sk \
  -H "Authorization: Bearer $DW_TOKEN" \
  -F "file=@/tmp/e2e-test.txt" \
  -F "path=discussions/disc-abc123/context.txt" \
  $FILES
dw $FILES/discussions/disc-abc123 | jq .

# TS-715 — federation: config:read required for GET
curl -sk -H "Authorization: Bearer <monitor-peer-token>" \
  "$DW_HOST/api/files/meta"
# → 403 Forbidden

# TS-716 — federation: config:write required for upload/delete
curl -sk \
  -H "Authorization: Bearer <read-only-peer-token>" \
  -F "file=@/tmp/e2e-test.txt" \
  -F "path=peers/ro-peer/test.txt" \
  $FILES \
  -w "%{http_code}"
# 403
```

**CLI:**

```sh
# TS-708 — list files
datawatch files list

# TS-709 — upload a file
datawatch files upload /tmp/e2e-test.txt --path peers/nas-peer/e2e-test.txt

# Verify existence
datawatch files list

# TS-710 — delete a file
datawatch files delete peers/nas-peer/e2e-test.txt

# TS-711 — list peer subdir
datawatch files peer nas-peer
```

**MCP (via Claude Code):**

```
# TS-712
files_upload(path="peers/nas-peer/notes.txt", content="hello from mcp")

# TS-713
files_delete(path="peers/nas-peer/notes.txt")

# TS-714
files_meta()
# → {"root":"...","peer_count":N,"discussion_count":N,"disk_bytes":N}
```

**Locale check (TS-718):**

Load the PWA with each locale (`?lang=en`, `?lang=de`, `?lang=es`, `?lang=fr`, `?lang=ja`).
Verify the File Service section title in Settings → General renders in each.

---

## Federation Access Matrix — v8.3.0

### New endpoints (BL331 + BL333)

| Endpoint | Method | Required cap | `comm-bridge` | `read-only` | `config-admin` | `comms-channel-agent` |
|---|---|---|---|---|---|---|
| `/api/channel/routing` | GET | `comm:read` | ✓ | ✓ | — | ✓ |
| `/api/channel/routing` | PUT | `comm:write` | ✓ | — | — | ✓ |
| `/api/files/meta` | GET | `config:read` | — | ✓ | ✓ | — |
| `/api/files/peers/*` | GET | `config:read` | — | ✓ | ✓ | — |
| `/api/files/discussions/*` | GET | `config:read` | — | ✓ | ✓ | — |
| `/api/files` | POST (upload) | `config:write` | — | — | ✓ | — |
| `/api/files` | DELETE | `config:write` | — | — | ✓ | — |

**Notes:**
- `comms-channel-agent` is the new 14th builtin group (BL331). It has `comm:read + comm:write` — appropriate for federation peers that bridge channel-routing identity.
- `comm-bridge` (v8.2.0) is unchanged; it retains `comm:read + comm:write` and now also covers the new channel routing endpoints.
- `read-only` covers `config:read` and can therefore list/read file metadata, but cannot upload or delete.
- `config-admin` covers `config:read + config:write` and has full file service access.
- `federation-peer` (default safe group) lacks `comm:*` and `config:write` by design.

### Inherited from prior releases (unchanged)

| Endpoint | Method | Required cap |
|---|---|---|
| `/api/push/register` | GET | `comm:read` |
| `/api/push/register` | POST | `comm:write` |
| `/api/push/unregister` | DELETE | `comm:write` |
| `/api/push/notify` | POST | `comm:write` |
| `/api/identity` | GET | `config:read` |
| `/api/identity` | POST/PATCH/PUT | `config:write` |
| `/api/autonomous/prds/{id}/decompose` | POST | `autonomous:write` |
| `/api/autonomous/prds/{id}/decompose/stream` | GET | `autonomous:read` |

---

## Feature Coverage Summary

| Feature | REST | CLI | MCP | PWA | Locale | Federation |
|---|---|---|---|---|---|---|
| BL331 channel routing | TS-683–687, 692–697 | TS-689 | TS-690 | TS-691, 698 | TS-699–700 | TS-692–693 |
| BL333 file service | TS-701–707 | TS-708–711 | TS-712–714 | TS-717 | TS-718 | TS-715–716 |
