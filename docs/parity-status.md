# Client Parity Status

**Standard: PWA == Android == iOS**  
**Last updated:** v8.9.4 (2026-05-30)

This table tracks the parity state of operator-visible features across all three clients.
iOS parity standard added in v8.8.6 (issue #107 in `dmz006/datawatch`).

Legend:
- ✅ — Implemented and verified
- 🔶 — Partial / in progress
- ❌ — Not yet implemented
- N/A — Not applicable to this platform

## Feature Parity Table

| Feature | PWA | Android | iOS |
|---------|-----|---------|-----|
| Session list + status | ✅ | ✅ | ❌ (planned) |
| Start / stop / kill session | ✅ | ✅ | ❌ (planned) |
| Session output streaming | ✅ | ✅ | ❌ (planned) |
| Settings — General | ✅ | ✅ | ❌ (planned) |
| Settings — LLM backends | ✅ | ✅ | ❌ (planned) |
| Settings — Messaging backends | ✅ | ✅ | ❌ (planned) |
| Push notifications (FCM) | ✅ | ✅ | N/A |
| Push notifications (APNs) | N/A | N/A | ❌ (BL item — next minor) |
| Alert list + mark read | ✅ | ✅ | ❌ (planned) |
| Autonomous PRD list + actions | ✅ | 🔶 | ❌ (planned) |
| Memory recall | ✅ | 🔶 | ❌ (planned) |
| Council run + results | ✅ | 🔶 | ❌ (planned) |
| Monitor tab (stats) | ✅ | 🔶 | ❌ (planned) |
| Orchestrator graphs | ✅ | ❌ | ❌ (planned) |
| Compute nodes | ✅ | ❌ | ❌ (planned) |
| Summarize last response | ✅ | ❌ (tracked: app#146) | ❌ (planned) |
| Chrome session flag | ✅ | ❌ (tracked: app#146) | ❌ (planned) |
| Localization (5 locales) | ✅ | ✅ | ❌ (planned) |
| Dark / light theme | ✅ | ✅ | ❌ (planned) |

## iOS Client Plan

The native SwiftUI iOS client is in development. See:
- `dmz006/datawatch-app` → `docs/plans/2026-05-27-ios-client.md`
- ETA for APNs dependency on server: ~10 weeks from 2026-05-27

### Server-side iOS requirements

| Requirement | Status | Notes |
|------------|--------|-------|
| `platform=apns` on `POST /api/device/register` | ❌ Pending | FCM only today |
| APNs send on alert fire | ❌ Pending | Requires APNs JWT/cert config |
| All REST + WS endpoints platform-neutral | ✅ Done | No iOS-only paths |

## APNs Server Work (next minor release)

APNs support requires:
1. Accept `platform=apns` with APNs device token on `POST /api/device/register`
2. Store APNs tokens alongside FCM tokens in the device registry
3. On alert fire, send to all registered APNs tokens via APNs HTTP/2 API
4. Config: `push.apns.key_id`, `push.apns.team_id`, `push.apns.bundle_id`,
   `push.apns.key_path` (or `${secret:apns-key}`)

APNs payload schema (matching FCM schema):
```json
{
  "aps": {
    "alert": { "title": "Session waiting", "body": "<session name>" },
    "content-available": 1,
    "badge": <unread_alert_count>
  },
  "sessionId": "...",
  "type": "wait"
}
```
