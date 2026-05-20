# E2E Test Cookbook — v8.2.0

**Version**: v8.2.0  
**Sprint**: T40 — Android 1.0.0 blockers + settings UX  
**Stories**: TS-637–TS-682  
**Last Run**: 2026-05-19  
**Pass Rate**: 43/46 (3 skipped: cap enforcement, no-auth env)  
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

## T40a — BL327: Badge/chip multi-select (TS-637–TS-644)

PWA-only UX. No REST surface change — the underlying value is still a comma-separated string.

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-637 | PWA | Secrets tags field: type tag + Enter/comma → badge appears; × removes it | ✅ pass |
| TS-638 | PWA | Federation peer capabilities: dropdown shows known caps; select creates badge | ✅ pass |
| TS-639 | PWA | LLM fallback chain: drag badges to reorder; order reflected in API payload | ✅ pass |
| TS-640 | PWA | Compute node tags: badge input saves as comma-separated to REST | ✅ pass |
| TS-641 | PWA | Profile memory_shared_with: badge input saves and re-renders from saved value | ✅ pass |
| TS-642 | PWA | Profile skills: badge input saves and re-renders from saved value | ✅ pass |
| TS-643 | PWA | Badge input: empty submission shows placeholder, no empty badge added | ✅ pass |
| TS-644 | PWA | Badge input: known-set field filters dropdown on typing; no-match shows no dropdown | ✅ pass |

**Key test notes:**
- All badge fields are PWA-only; REST payloads are unchanged (comma-separated strings).
- TS-639 drag-to-reorder requires a browser with drag events; Playwright best.
- TS-638 fetches `/api/federation/groups/builtins` for the known-set dropdown.

---

## T40b — BL328: Async PRD decompose (TS-645–TS-657)

```sh
# Create a test PRD first.
PRD_ID=$(dw -X POST $DW_HOST/api/autonomous/prds \
  -d '{"title":"E2E BL328 test","description":"Create a simple calculator CLI tool"}' \
  | jq -r .id)
echo "PRD: $PRD_ID"
```

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-645 | REST | POST decompose returns 202 with task_id + stream_url | ✅ pass |
| TS-646 | REST | SSE stream emits story events then complete event | ✅ pass |
| TS-647 | REST | GET /decompose/status returns running → complete progression | ✅ pass |
| TS-648 | REST | Last-Event-ID replay: reconnect mid-stream receives only missed events | ✅ pass |
| TS-649 | REST | Idempotent: second POST for in-flight job returns 202 with same task_id | ✅ pass |
| TS-650 | CLI | datawatch autonomous prd decompose <id> exits 0 | ✅ pass |
| TS-651 | MCP | autonomous_prd_decompose tool returns task_id + stream_url | ✅ pass |
| TS-652 | PWA | Decompose button shows inline progress panel; stories appear incrementally | ✅ pass |
| TS-653 | PWA | Progress panel shows reconnecting state on forced disconnect | ✅ pass |
| TS-654 | Federation | CapAutonomousWrite required: peer lacking cap → 403 | ⏭️ skip |
| TS-655 | Federation | CapAutonomousRead required for status/stream: read-only peer → 503/200 not 403 | ⏭️ skip |
| TS-656 | REST | Status after completion: status=complete, stories array populated | ✅ pass |
| TS-657 | REST | Status for unknown PRD with no job: 404 | ✅ pass |

**REST happy path:**

```sh
# TS-645
RESP=$(dw -X POST $DW_HOST/api/autonomous/prds/$PRD_ID/decompose)
echo $RESP | jq .
# {"task_id":"...","stream_url":"/api/autonomous/prds/.../decompose/stream"}

# TS-646 — subscribe and capture events (Ctrl-C after complete event)
curl -sk -H "Authorization: Bearer $DW_TOKEN" -H "Accept: text/event-stream" \
  "$DW_HOST$(echo $RESP | jq -r .stream_url)"

# TS-647 — poll status
dw "$DW_HOST/api/autonomous/prds/$PRD_ID/decompose/status" | jq .

# TS-648 — replay with Last-Event-ID
curl -sk -H "Authorization: Bearer $DW_TOKEN" -H "Last-Event-ID: 2" -H "Accept: text/event-stream" \
  "$DW_HOST$(echo $RESP | jq -r .stream_url)"

# TS-654 — federation cap (ro peer should get 403)
curl -sk -X POST -H "Authorization: Bearer <ro-peer-token>" \
  "$DW_HOST/api/autonomous/prds/$PRD_ID/decompose"
# → 403 Forbidden
```

---

## T40c — BL329: Identity POST alias (TS-658–TS-664)

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-658 | REST | POST /api/identity returns current identity (same as PATCH) | ✅ pass |
| TS-659 | REST | POST /api/identity with partial body updates only those fields | ✅ pass |
| TS-660 | REST | GET /api/identity returns identity object with role/focus/notes | ✅ pass |
| TS-661 | REST | PUT /api/identity replaces whole identity | ✅ pass |
| TS-662 | Federation | CapConfigWrite required for POST/PUT/PATCH; ro peer → 403 | ✅ pass |
| TS-663 | Federation | CapConfigRead required for GET; monitor peer → 403 | ✅ pass |
| TS-664 | PWA | Settings → General → Identity card: save via POST updates daemon state | ✅ pass |

**REST happy path:**

```sh
# TS-658 — POST (partial update alias for PATCH)
dw -X POST $DW_HOST/api/identity \
  -d '{"role":"Software Engineer","focus":"Datawatch v8.2.0 sprint"}' | jq .

# TS-660 — GET
dw $DW_HOST/api/identity | jq .

# TS-662 — federation cap
curl -sk -X POST -H "Authorization: Bearer <ro-peer-token>" \
  "$DW_HOST/api/identity" -d '{"role":"attacker"}'
# → 403 Forbidden
```

---

## T40d — BL330: UnifiedPush registration (TS-665–TS-673)

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-665 | REST | GET /.well-known/unifiedpush returns {version:1,unifiedpush:{gateway:...}} | ✅ pass |
| TS-666 | REST | POST /api/push/register with endpoint returns {ok:true,id:reg-...} | ✅ pass |
| TS-667 | REST | GET /api/push/register returns registrations list | ✅ pass |
| TS-668 | REST | DELETE /api/push/unregister by id removes registration | ✅ pass |
| TS-669 | REST | DELETE /api/push/unregister by endpoint removes registration | ✅ pass |
| TS-670 | REST | POST /api/push/notify sends to all endpoints; returns 202 | ✅ pass |
| TS-671 | REST | POST /api/push/notify with registration_id targets only that endpoint | ✅ pass |
| TS-672 | CLI | datawatch push list, push test, push unregister all exit 0 | ⏭️ skip |
| TS-673 | Federation | CapCommWrite required for register/unregister/notify; ro → 403 | ✅ pass |

**REST happy path:**

```sh
# TS-665 — discovery
dw $DW_HOST/.well-known/unifiedpush | jq .
# {"version":1,"unifiedpush":{"gateway":"/api/push/notify"}}

# TS-666 — register
REG=$(dw -X POST $DW_HOST/api/push/register \
  -d '{"endpoint":"https://up.example.com/UP?token=test"}')
echo $REG | jq .
REG_ID=$(echo $REG | jq -r .id)

# TS-667 — list
dw $DW_HOST/api/push/register | jq .

# TS-670 — notify all
dw -X POST $DW_HOST/api/push/notify \
  -d '{"title":"E2E test","message":"notify all"}' -w "%{http_code}"
# 202

# TS-671 — notify one
dw -X POST $DW_HOST/api/push/notify \
  -d "{\"registration_id\":\"$REG_ID\",\"title\":\"E2E test\",\"message\":\"targeted\"}" \
  -w "%{http_code}"
# 202

# TS-668 — unregister by id
dw -X DELETE $DW_HOST/api/push/unregister \
  -d "{\"id\":\"$REG_ID\"}" | jq .
# {"ok":true,"removed":1}

# TS-673 — federation cap (ro peer lacks comm:write → 403)
curl -sk -X POST -H "Authorization: Bearer <ro-peer-token>" \
  "$DW_HOST/api/push/register" -d '{"endpoint":"https://up.example.com/UP"}'
# → 403 Forbidden
```

**CLI:**

```sh
# TS-672
datawatch push list
datawatch push test --message "E2E test notification"
# Register first, then:
datawatch push unregister --endpoint "https://up.example.com/UP?token=test"
```

---

## T40e — App#133/134/135: Android parity (TS-674–TS-682)

These stories verify the server-side endpoints are reachable and return the
correct shapes for the Android app to consume. Android-native UI verification
is in `datawatch-app` issues App#133, App#134, App#135.

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-674 | REST | GET /api/alert-rules returns valid schema (App#133 parity) | ✅ pass |
| TS-675 | REST | POST /api/alert-rules creates rule; Android can parse shape | ✅ pass |
| TS-676 | REST | GET /api/skills/registries first entry is community registry (App#134) | ✅ pass |
| TS-677 | REST | GET /api/plugins/browse?registry=community returns plugin list | ✅ pass |
| TS-678 | REST | POST /api/plugins/install returns task_id (App#134 install flow) | ✅ pass |
| TS-679 | REST | GET /api/identity returns role/focus/context_notes fields (App#133 identity card) | ✅ pass |
| TS-680 | REST | POST /api/push/register accepts shape from Android UP library | ✅ pass |
| TS-681 | REST | POST /api/voice/transcribe accepts audio/webm (App#135 mic overlay) | ✅ pass |
| TS-682 | REST | Mic overlay: POST /api/sessions/{id}/input accepts text from transcription | ✅ pass |

---

## Federation Access Matrix — v8.2.0

| Endpoint | Method | Required cap | `comm-bridge` | `read-only` | `federation-peer` |
|---|---|---|---|---|---|
| `/api/push/<topic>` | GET | `comm:read` | ✓ | ✓ | — |
| `/api/push/<topic>` | POST | `comm:write` | ✓ | — | — |
| `/api/push/register` | GET | `comm:read` | ✓ | ✓ | — |
| `/api/push/register` | POST | `comm:write` | ✓ | — | — |
| `/api/push/unregister` | DELETE | `comm:write` | ✓ | — | — |
| `/api/push/notify` | POST | `comm:write` | ✓ | — | — |
| `/api/identity` | GET | `config:read` | — | ✓ | — |
| `/api/identity` | POST/PATCH/PUT | `config:write` | — | — | — |
| `/api/autonomous/prds/{id}/decompose` | POST | `autonomous:write` | — | — | — |
| `/api/autonomous/prds/{id}/decompose/stream` | GET | `autonomous:read` | — | ✓ | — |
| `/api/autonomous/prds/{id}/decompose/status` | GET | `autonomous:read` | — | ✓ | — |

**Notes:**
- `comm-bridge` is the recommended group for Android app federation peers (includes `comm:read + comm:write`).
- Push subscribe (`GET /api/push/<topic>`) and list registrations (`GET /api/push/register`) are read-only; `read-only` group suffices.
- Identity write and autonomous write require `config-admin` or `autonomous-operator` respectively, or `full-control`.
- `federation-peer` (default safe group for new peers) lacks all comm and autonomous caps by design.

---

## Feature Coverage Summary

| Feature | REST | CLI | MCP | PWA | Locale | Federation |
|---|---|---|---|---|---|---|
| BL327 badge input | — (CSS/JS only) | — | — | TS-637–644 | ✓ | — |
| BL328 async decompose | TS-645–657 | TS-650 | TS-651 | TS-652–653 | ✓ | TS-654–655 |
| BL329 identity POST | TS-658–664 | — | — | TS-664 | ✓ | TS-662–663 |
| BL330 push register | TS-665–673 | TS-672 | — | — | ✓ | TS-673 |
| App parity | TS-674–682 | — | — | — | — | — |
