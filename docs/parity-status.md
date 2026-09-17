# Client Parity Status

**Standard: PWA == Android == iOS**  
**Last updated:** v8.33.32 (2026-09-17)

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
| Session list + status | ✅ | ✅ | ❌ (tracked: app#182) |
| Start / stop / kill session | ✅ | ✅ | ❌ (tracked: app#182) |
| Session output streaming | ✅ | ✅ | ❌ (tracked: app#182) |
| Settings — General | ✅ | ✅ | ❌ (tracked: app#182) |
| Settings — LLM backends | ✅ | ✅ | ❌ (tracked: app#182) |
| Settings — Messaging backends | ✅ | ✅ | ❌ (tracked: app#182) |
| Push notifications (FCM) | ✅ | ✅ | N/A |
| Push notifications (APNs) | N/A | N/A | ❌ (BL item — tracked: app#185) |
| Alert list + mark read | ✅ | ✅ | ❌ (tracked: app#182) |
| Autonomous PRD list + actions | ✅ | ✅ (app#162,#163,#165,#171,#172 closed) | ❌ (tracked: app#182) |
| Automaton spec expand (show full / collapse) | ✅ | ✅ v8.25.0 (app#166 closed) | ❌ (tracked: app#182) |
| Automaton status graphs (progress bars + CPU/RSS) | ✅ | ✅ v8.25.0 (app#166 closed) | ❌ (tracked: app#182) |
| Automaton cancel button visible during running state | ✅ | ✅ v8.25.0 (app#166 closed) | ❌ (tracked: app#182) |
| Memory recall | ✅ | 🔶 (tracked: app#183) | ❌ (tracked: app#183) |
| Council run + results | ✅ | ✅ v8.0 (app#127 closed) | ❌ (tracked: app#182) |
| Monitor tab (stats) | ✅ | ✅ v8.22.0 (app#164 closed) | ❌ (tracked: app#182) |
| Orchestrator graphs | ✅ | ❌ (tracked: app#184) | ❌ (tracked: app#184) |
| Compute nodes | ✅ | ✅ v8.25.3 (app#168,#173 closed) | ❌ (tracked: app#182) |
| Summarize last response | ✅ | ✅ v8.8.13 (app#146,#147 closed) | ❌ (tracked: app#182) |
| Chrome session flag | ✅ | ✅ v8.8.3 (app#146 closed) | ❌ (tracked: app#182) |
| Localization (5 locales) | ✅ | ✅ | ❌ (tracked: app#182) |
| Dark / light theme | ✅ | ✅ | ❌ (tracked: app#182) |
| Image attachment in session input (📷 button) | ✅ | ✅ v8.19.0 (app#158 closed) | ❌ (tracked: app#182) |
| Memory scope model — prd-shared/story-shared (BL385+) | ✅ v8.29.0 | 🔲 (tracked: app#174) | 🔲 (tracked: app#174) |
| Memory lifecycle — seeding/harvest/archive/import (BL386+) | ✅ v8.30.0 | 🔲 (tracked: app#175) | 🔲 (tracked: app#175) |
| Automata memory integration — stats tile, report (BL387+) | ✅ v8.31.0+ | 🔲 (tracked: app#176) | 🔲 (tracked: app#176) |
| files_touched on tasks — uncommitted edits, untracked files, non-git dirs (B102) | ✅ v8.33.27 | 🔲 (tracked: app#177) | 🔲 (tracked: app#177) |
| PRD detail live WS updates — incremental patch, no flicker (B103) | ✅ v8.33.27 | 🔲 (tracked: app#178) | 🔲 (tracked: app#178) |
| PRD list story/task status parity — icon mapping, effective status, card persistence (B104) | ✅ v8.33.28 | 🔲 (tracked: app#179) | 🔲 (tracked: app#179) |
| Automata list — completed in default view, filter badges for all statuses incl. history group (B105) | ✅ v8.33.29 | 🔲 (tracked: app#180) | 🔲 (tracked: app#180) |
| Inline file viewer for task file chips — markdown rendered, plain text, download fallback (B106) | ✅ v8.33.32 | 🔲 (tracked: app#181) | 🔲 (tracked: app#181) |

## Plan/Proposal parity gate

The table above tracks **shipped** client parity. This section tracks **in-flight** work so the parity gate is visible before a feature lands. Per AGENT.md, every plan/proposal under `docs/plans/` must ship across all enumerated surfaces — `REST`, `MCP`, `CLI`, `comm channel`, `PWA` (plus the Android/iPhone clients as parity targets) — or carry a reason-logged exclusion in its `Parity surface` section.

| Planned feature | Target surfaces (per spec) | Status |
|-----------------|----------------------------|--------|
| Eval Sweep — suite × backend matrix | REST, MCP, CLI, comm, PWA | planned — `docs/plans/harness-impl/eval-sweep-api.md` (cross-surface contract §4) |
| Eval nodes in the PRD-DAG orchestrator | REST, MCP, CLI, comm, PWA | planned — spec under `docs/plans/harness-impl/` (proposal: `docs/plans/harness-research/enhancement-proposals.md` §2) |
| Red-team validator pipeline (deepening the injection guard) | REST, MCP, CLI, comm, PWA | planned — spec under `docs/plans/harness-impl/` (proposal: `docs/plans/harness-research/enhancement-proposals.md` §5) |

Features promoted to shipped move a row into the Feature Parity Table above with per-client ✅/🔶/❌ columns and `datawatch-app` tracking; until then they remain visible here.

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
