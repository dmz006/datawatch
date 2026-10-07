# Client Parity Status

**Standard: PWA == Android == iOS**
**Last updated:** v8.62.0 (2026-10-06), cross-referenced against `dmz006/datawatch-app` v1.28.4 (2026-10-06)

This table tracks the parity state of operator-visible features across all three clients.
iOS parity standard added in v8.8.6 (issue #107 in `dmz006/datawatch`).

**Sourcing note on this refresh:** this table was last updated at v8.33.32
(2026-09-17) and had drifted ~29 minor versions / 3 weeks stale — most
visibly, every iOS row still read "❌ (tracked: app#182)" despite
`datawatch-app`'s own CHANGELOG declaring a "Three-way parity release
(PWA ↔ Android ↔ iOS)" on 2026-10-04 and iOS shipping to General
Availability via TestFlight. This refresh is grounded in
`dmz006/datawatch-app/CHANGELOG.md`'s own entries (self-reported by the
app side, cross-checked against specific changelog lines quoted below
where a row changed), not independent testing of the iOS/Android
binaries — flagged per-row where that distinction matters.

Legend:
- ✅ — Implemented and verified
- 🔶 — Partial / in progress
- ❌ — Not yet implemented
- N/A — Not applicable to this platform

## Feature Parity Table

| Feature | PWA | Android | iOS |
|---------|-----|---------|-----|
| Session list + status | ✅ | ✅ | ✅ (three-way parity release, 2026-10-04) |
| Start / stop / kill session | ✅ | ✅ | ✅ |
| Session output streaming | ✅ | ✅ | ✅ |
| Settings — General | ✅ | ✅ | ✅ |
| Settings — LLM backends | ✅ | ✅ | ✅ |
| Settings — Messaging backends | ✅ | ✅ | ✅ |
| Push notifications (FCM) | ✅ | ✅ | N/A |
| Push notifications (APNs) | N/A | N/A | 🔶 app registers + sends token (v1.x "iOS push registration"); **server-side APNs dispatch still pending — this is BL397 Phase 4 / BL335, in progress this session** |
| Alert list + mark read | ✅ | ✅ | ✅ |
| Autonomous PRD list + actions | ✅ | ✅ | ✅ |
| Automaton spec expand (show full / collapse) | ✅ | ✅ | ✅ |
| Automaton status graphs (progress bars + CPU/RSS) | ✅ | ✅ | ✅ |
| Automaton cancel button visible during running state | ✅ | ✅ | ✅ |
| Memory recall | ✅ | ✅ | ✅ |
| Council run + results | ✅ | ✅ | ✅ — live-watch, cancel, replay (app CHANGELOG "Council live runs") |
| Monitor tab (stats) | ✅ | ✅ | ✅ |
| Orchestrator graphs | ✅ | ✅ | ✅ (app CHANGELOG 2026-10-04: "orchestrator graphs" in the three-way parity Automata list) |
| Capacity admission (pools, wait queue, priority, per-node/LLM limits) | ✅ | ✅ (app v1.23.83, 2026-09-29: "Capacity card showing pools, limits and the wait queue") | ✅ |
| Compute nodes | ✅ | ✅ | ✅ |
| Summarize last response | ✅ | ✅ | ✅ |
| Chrome session flag | ✅ | ✅ | ✅ |
| Localization (5 locales) | ✅ | ✅ | ✅ |
| Dark / light theme | ✅ | ✅ | ✅ |
| Image attachment in session input (📷 button) | ✅ | ✅ | ✅ |
| Memory scope model — prd-shared/story-shared (BL385+) | ✅ v8.29.0 | ✅ | ✅ |
| Memory lifecycle — seeding/harvest/archive/import (BL386+) | ✅ v8.30.0 | ✅ | ✅ |
| Automata memory integration — stats tile, report (BL387+) | ✅ v8.31.0+ | ✅ | ✅ |
| files_touched on tasks — uncommitted edits, untracked files, non-git dirs (B102) | ✅ v8.33.27 | ✅ | ✅ |
| PRD detail live WS updates — incremental patch, no flicker (B103) | ✅ v8.33.27 | ✅ | ✅ |
| PRD list story/task status parity — icon mapping, effective status, card persistence (B104) | ✅ v8.33.28 | ✅ | ✅ |
| Automata list — completed in default view, filter badges for all statuses incl. history group (B105) | ✅ v8.33.29 | ✅ | ✅ |
| Inline file viewer for task file chips — markdown rendered, plain text, download fallback (B106) | ✅ v8.33.32 | ✅ | ✅ |
| Watch sessions/automata + watched-only filter | ✅ v8.50.0 | ✅ | ✅ (app CHANGELOG: "👁 N button... Android and iOS") |
| Council markdown rendering | 🔶 v8.46.0 — completed-run modal only; the live in-progress run log is still plain text (GH#181, left open) | ✅ | ✅ |
| Animated "eye" loading state (replacing plain "Loading…" / shimmer bars) | ❌ — tracked, `dmz006/datawatch#186`, not started | ✅ | ✅ |
| Native ACME / Let's Encrypt cert management (BL397) | ✅ v8.62.0 | N/A (server-side feature; apps just consume whatever cert the daemon serves) | N/A |

## Known PWA gaps (tracked, not yet closed)

- **GH#186** — animated "eye" loading state. Apps have it everywhere (cards, full panels, with a pulsing "Loading…" label); PWA still shows plain text / shimmer bars in most spots. Not started.
- **GH#181** — council markdown rendering is partial: the completed-run modal renders markdown, but the live in-progress run log (`councilOpenLiveWatch`) still truncates to plain text at 600/400 chars, exactly as the issue describes. Left open.
- **GH#182** — one item remains: `datawatch://alert/<id>` deep-link route handling, blocked on an operator URL-shape decision.
- **GH#172 D65/D67** — three-finger swipe-up gesture + other under-specified Android session-detail extras. Deferred pending clarification of exact behavior, not a parity gap PWA is positioned to close blind.
- **BL396 approved-but-unshipped**: #17 (alert badge watched-only count), #18 (per-automaton memory stats on Automaton detail), #19 (server info card), #20 (add-memory dialog tags field), 2 cosmetic items (skeleton shimmer timing, splash status granularity).

## Plan/Proposal parity gate

The table above tracks **shipped** client parity. This section tracks **in-flight** work so the parity gate is visible before a feature lands. Per AGENT.md, every plan/proposal under `docs/plans/` must ship across all enumerated surfaces — `REST`, `MCP`, `CLI`, `comm channel`, `PWA` (plus the Android/iPhone clients as parity targets) — or carry a reason-logged exclusion in its `Parity surface` section.

| Planned feature | Target surfaces (per spec) | Status |
|-----------------|----------------------------|--------|
| Eval Sweep — suite × backend matrix | REST, MCP, CLI, comm, PWA | planned — `docs/plans/harness-impl/eval-sweep-api.md` (cross-surface contract §4) |
| Eval nodes in the PRD-DAG orchestrator | REST, MCP, CLI, comm, PWA | planned — spec under `docs/plans/harness-impl/` (proposal: `docs/plans/harness-research/enhancement-proposals.md` §2) |
| Red-team validator pipeline (deepening the injection guard) | REST, MCP, CLI, comm, PWA | planned — spec under `docs/plans/harness-impl/` (proposal: `docs/plans/harness-research/enhancement-proposals.md` §5) |
| ACME DNS-01 (BL397 Phase 2) | REST, MCP, CLI, comm, PWA, YAML | in progress this session — `docs/plans/2026-10-06-bl397-native-acme-letsencrypt.md` |
| ACME zero-downtime hot-swap apply (BL397 Phase 3) | internal only, no new surface | in progress this session |

Features promoted to shipped move a row into the Feature Parity Table above with per-client ✅/🔶/❌ columns and `datawatch-app` tracking; until then they remain visible here.

## iOS Client Plan

The native SwiftUI iOS client reached **General Availability via TestFlight**
(datawatch-app v1.28.x, no public App Store listing yet — see
`dmz006/datawatch-app`'s `docs/ios.md` for install/setup). The "in
development" framing from earlier versions of this doc is stale; iOS is
now a first-class client alongside Android, sharing the same Compose
Multiplatform core.

- `dmz006/datawatch-app` → `docs/ios.md` (TestFlight install, first-run
  setup, permissions, known limitations), `docs/store-listing-ios.md`
  (App Store Connect listing).
- APNs (server-side push dispatch) is the one still-open item blocking
  full iOS push parity — see below.

### Server-side iOS requirements

| Requirement | Status | Notes |
|------------|--------|-------|
| `platform=apns` on `POST /api/device/register` | ✅ Done | `devices.KindAPNS` already in the enum, registration accepted |
| APNs send on alert fire | 🔶 In progress this session (BL397 Phase 4 / BL335) | Server-side HTTP/2 dispatch — see below |
| All REST + WS endpoints platform-neutral | ✅ Done | No iOS-only paths |

## APNs Server Work (BL397 Phase 4 / BL335 — in progress this session)

APNs support requires:
1. Accept `platform=apns` with APNs device token on `POST /api/device/register` — ✅ already works (device kind enum pre-existing).
2. Store APNs tokens alongside FCM tokens in the device registry — ✅ already works.
3. On alert fire, send to all registered APNs tokens via APNs HTTP/2 API — building now.
4. Config: `push.apns.key_id`, `push.apns.team_id`, `push.apns.bundle_id`,
   `push.apns.key_path` (or `${secret:apns-key}`) — building now.

APNs payload schema (matching the existing FCM schema):
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

Once shipped: flip this section's status to ✅, update the "Push
notifications (APNs)" row in the Feature Parity Table above, and close
`dmz006/datawatch#158`/`#183` and `dmz006/datawatch-app#185`.
