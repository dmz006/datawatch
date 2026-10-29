# Client Parity Status

**Standard: PWA == Android == iOS**
**Last updated:** v8.79.0 (2026-10-09), cross-referenced against `dmz006/datawatch-app` as of app#241 (filed 2026-10-09, not yet actioned)

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
- design only — Design written and on file under `docs/plans/`; no implementation yet
- N/A — Not applicable to this platform

## Feature Parity Table

| Feature | PWA | Android | iOS |
|---------|-----|---------|-----|
| Session list + status | ✅ | ✅ | ✅ (three-way parity release, 2026-10-04) |
| WebSocket session window (default-view rows on connect, one `session_state` per change, paged history over REST) | ✅ | ❌ pending the datawatch-app issue | ❌ pending the datawatch-app issue |
| Start / stop / kill session | ✅ | ✅ | ✅ |
| Session output streaming | ✅ | ✅ | ✅ |
| Settings — General | ✅ | ✅ | ✅ |
| Settings — LLM backends | ✅ | ✅ | ✅ |
| Settings — Messaging backends | ✅ | ✅ | ✅ |
| Settings — Audit & Access Log (v8.74.0) | ✅ | ❌ (tracked: app#241) | ❌ (tracked: app#241) |
| Settings — Cross-Session Communication Rule (v8.79.0) | ✅ | ❌ (tracked: app#241) | ❌ (tracked: app#241) |
| Settings — Comms: imap-mcp channel config (BL404) | ✅ | ❌ (tracked: [app#250](https://github.com/dmz006/datawatch-app/issues/250)) | ❌ (tracked: [app#250](https://github.com/dmz006/datawatch-app/issues/250)) |
| Push notifications (FCM) | ✅ | ✅ | N/A |
| Push notifications (APNs) | N/A | N/A | ✅ v8.62.x — server-side dispatch shipped (BL397 Phase 4 / BL335); **unit-tested only, not live-verified against real Apple servers** (no Apple Developer account/Auth Key/TestFlight device token available in this environment) |
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
| Account usage calculator (readings, advice, three policies, three PWA variants, federation keeper) | ✅ MCP, REST, CLI, comm; PWA 🔶 not yet shipped | ❌ pending: [app#286](https://github.com/dmz006/datawatch-app/issues/286) (contract: `plans/2026-10-25-usage-calculator-mobile-contract.md`); locale keys: [app#287](https://github.com/dmz006/datawatch-app/issues/287) | ❌ pending: [app#286](https://github.com/dmz006/datawatch-app/issues/286), same scope and locale keys [app#287](https://github.com/dmz006/datawatch-app/issues/287) |
| Usage back-off (capacity panel wait-reason chip + `autonomous.usage_backoff` / `autonomous.backoff_max_wait_minutes` settings fields + status display) | ✅ | ❌ — datawatch-app item: mirror the capacity-panel back-off display and the two settings fields | ❌ — datawatch-app item, same scope |
| Capacity visibility (task wait cause chip, reason, reset time and Holds line; capacity card back-off reset label, budget spend bars with the estimate label; audit query `subject` filter and `action` prefix) | ✅ | ❌ — pending the datawatch-app issue written up in `plans/2026-10-22-datawatch-app-capacity-visibility-note.md` (not filed yet) | ❌ — same issue, same scope |
| Capacity card — compute node usage rows (per node: loaded model with memory size and expiry or stays-loaded, slots held of limit, GPU utilization and memory from the observer; `usage` object on REST, `capacity_status` MCP, CLI, comm) | ✅ | ❌ pending the datawatch-app issue written up in `plans/2026-10-24-datawatch-app-capacity-node-usage-note.md` (not filed) | ❌ — same issue, same scope (five-locale key set specified in that note) |
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
| Watch sessions/automata + watched-only filter | ✅ v8.50.0; on the session list and detail, watching and the Watched only toggle now sit in the header Tags dropdown (Unreleased) | ✅ | ✅ (app CHANGELOG: "👁 N button... Android and iOS") |
| Session header Tags dropdown — Watched only, tag filter, Watch this session, Edit tags of this session; detail header drops the last-output counter and the eye button (Unreleased) | ✅ Unreleased | ❌ — tracked, `dmz006/datawatch-app#285` (9 new locale keys, DE/ES/FR/JA via the Compose-Multiplatform pipeline) | ❌ — tracked, `dmz006/datawatch-app#285` (same capability) |
| Session header Tags button is a tag icon (title and aria-label `Tags`); the session tag row shows only read-only chips and is hidden when empty; `Edit tags` stays in the dropdown; locale keys `tag_session_none` and `tag_session_edit` removed | ✅ next patch release | ❌ — tracked, https://github.com/dmz006/datawatch-app/issues/289 | ❌ — tracked, https://github.com/dmz006/datawatch-app/issues/289 |
| Council markdown rendering | 🔶 v8.46.0 — completed-run modal only; the live in-progress run log is still plain text (GH#181, left open) | ✅ | ✅ |
| Animated "eye" loading state (replacing plain "Loading…" / shimmer bars) | ❌ — tracked, `dmz006/datawatch#186`, not started | ✅ | ✅ |
| Native ACME / Let's Encrypt cert management — HTTP-01 + DNS-01 (Cloudflare) + zero-downtime hot-swap (BL397) | ✅ v8.62.x | N/A (server-side feature; apps just consume whatever cert the daemon serves) | N/A |
| Plan review before approval — verdict panel (pass / findings / reviewer-outage state), re-run button, long approve-rejection toast, `plan_review_*` + `smoke_verification_*` settings fields, smoke/final-gate task chips | ✅ | ❌ (tracked: [app#266](https://github.com/dmz006/datawatch-app/issues/266)) | ❌ (tracked: [app#266](https://github.com/dmz006/datawatch-app/issues/266)) |
| Automaton lifecycle event subscriptions — daemon-side CRUD + update + test-fire (REST/MCP/CLI/comm), session-push, pending-events queue (`autonomous_events_pending`, `events`) and the PWA Subscriptions tab (list/add/edit/delete/test) | ✅ (Unreleased — Phases 3–4 of `plans/2026-10-13-prd-lifecycle-events.md`) | ❌ (subscription list/editor UI not built in the app; event *delivery* already flows via the v9.3.0 `PublishToTopic` → UnifiedPush path) | ⏳ file a `dmz006/datawatch-app` issue on merge — spec + Android/iOS build guidance in [`plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md`](plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md) |
| Automaton + guardrail-profile notify set — which lifecycle event kinds notify (`notify_events`): per-Automaton list, per-profile list, global `autonomous.notify.default_events`, resolver + subscription gate, audit `autonomous_notify_events_changed`; CLI `autonomous prd-set-notify-events` / `guardrail profile create|update --notify-events`, comm `set-notify-events` / `guardrail profile notify-events`, MCP `autonomous_prd_set_notify_events`, REST `POST …/set_notify_events`, PWA "Notification events" panel + Guardrail Profiles editor (node tests; live-browser run owed) | ✅ | ⏳ file a `dmz006/datawatch-app` issue on merge — spec in [`plans/2026-10-23-datawatch-app-notify-events-note.md`](plans/2026-10-23-datawatch-app-notify-events-note.md) | ⏳ same as Android |
| Automation control operations — pause, resume, cancel (required `cancel_in_flight`), skip, insert (with `agent_insert_policy` and the one-urgent cap), remove, reorder edges, approve insert (Phase 3b). Server-side on REST, MCP (`automation_*` control tools), CLI (`datawatch automation …`) and comm (`automation …`); all four are parity-complete | ✅ the Automation tab now drives them: pause, resume, cancel (always asks for `cancel_in_flight`), skip, insert, remove, reorder and approve, with a reload prompt on a stale version | N/A — no client change is needed for this phase (server-side only; the app does not drive Automations yet). Draft `dmz006/datawatch-app` issue text for a later app UI is in the Phase 3b "Mobile-Parity" note in [`testing-tracker.md`](testing-tracker.md) | N/A — same as Android |
| Automation model-tier defaults — `set_llm` and `set_verification_llm` on an Automation (execution, planning and verification backend, model and effort; resolution task, story, Automaton, Automation, global) and `executor_source` on task run records beside `verifier_source` (Phase 4). Server-side on REST, MCP (`automation_set_llm`, `automation_set_verification_llm`), CLI (`datawatch automation set-llm`, `set-verification-llm`) and comm (`automation set-llm`, `set-verification-llm`); all four are parity-complete | ✅ shows `executor_source` as an Executor line on the task and the session status card; setting the defaults is API-only until the Automation view exists | ⏳ file a `dmz006/datawatch-app` issue on merge — draft issue text in the Phase 4 "Mobile-Parity" note in [`testing-tracker.md`](testing-tracker.md) | ⏳ same as Android |
| Lane fill: status view, per-Automaton lane preference, `autonomous.lane_fill` settings. REST, MCP, CLI and comm channel parity complete. | ✅ Lane fill card on the Automation sub-tab, lane preference select in the Automaton settings form, settings fields and locale keys in all five bundles | ❌ issue text drafted in the plan, not filed | ❌ issue text drafted in the plan, not filed |
| Triage records: the record of every failed or blocked Automaton — identity, failed task, first-seen/alarm times, retry budgets shown as given, append-only notes — plus the triage pass and the single triage alarm (`autonomous.automation.triage_alert_minutes`, default 30, `0` = off). REST `GET /api/autonomous/triage`, MCP `autonomous_triage`, CLI `datawatch autonomous triage`, comm `autonomous triage`; PWA triage card on the Automaton detail page. Read-only: nothing here starts, retries or edits anything. | ✅ Triage card on the Automaton detail page + five-locale keys (node tests; the live browser run is owed) | ⏳ file a `dmz006/datawatch-app` issue on merge — the "Mobile-Parity" note in [`testing-tracker.md`](testing-tracker.md) carries the ready-to-file issue text (`feat(automaton-detail): triage card for failed/blocked records`) | ⏳ same as Android |
| Automation tab (list, pipeline strip, controls, create and edit) — a top-level view left of Automata: list of Automations, pipeline strip with fork lanes, joins and expandable nested levels, the control operations, and a create and edit form with fork, join and nested members, default models and budget policy | ✅ built in the development build (node tests; the live browser run is owed) | ⏳ file a `dmz006/datawatch-app` issue on merge — draft issue text in the Automation tab "Mobile-Parity" note in [`testing-tracker.md`](testing-tracker.md) | ⏳ same as Android |
| Automata tags — nested tag registry (create, rename, recolor, move), tags on an Automaton, browse by tag with an include-sub-tags switch, bulk tag. Server-side on REST, MCP, CLI and comm; `tag_ids` on the PRD JSON | ✅ Tag Registry in Settings → Automata, picker, card chips, browse selector, batch **Tag** action (node tests; the live browser run is owed) | ⏳ file a `dmz006/datawatch-app` issue on merge — spec in [`plans/2026-10-23-datawatch-app-tags-note.md`](plans/2026-10-23-datawatch-app-tags-note.md); view, assign and browse only, registry management stays web-only for now | ⏳ same as Android |
| Automata progress dashboard — counts by kind, throughput and utilization per active day, work left split into known and estimated, a completion range with its methods and drivers, inactive windows, daily snapshots and a trend; an Automaton kind setter. Server-side on REST, MCP, CLI and comm (`progress`, `progress trend`, `autonomous set-kind`) | ✅ *Progress* card on the Automata tab and a *Kind* select in Automaton settings (node tests; the live browser run is owed) | ❌ not yet: ready-to-file issue text is in `plans/2026-10-14-automation-orchestration-design.md` under *Phase 8 companion apps*; not filed | ❌ not yet: same issue text, iOS section; not filed |
| Task failure escalation policy: ladder of worker steps, hold for review at a failure count, review actions (retry, changed spec, worker or file scope, cancel), override on the daemon, an Automaton or a task. Server-side on REST, MCP, CLI and comm; PWA settings, override editors and review actions | 🔶 implemented, live run pending | pending (companion-app issue filed at merge) | pending (companion-app issue filed at merge) |
| Tag browsing and PRD tiles | shared tile live in the Automata and Automation lists (progress, tag chips, overflow menu, narrow rows); tag browser and attention view still to come — `docs/plans/2026-10-20-tag-browsing-tile-design.md` | not started — issue draft `docs/plans/assets/tag-browsing-datawatch-app-issue.md` | not started (same issue draft, iPhone and iPad) |
| Operator question channel — record operator decisions (raise, list, get, answer, withdraw, answer log) via REST; off by default behind the `autonomous.questions.enabled` switch. Step 1 notification channel (this iteration): raise announces over WebSocket + messaging group + high-priority alert (toggles `notify_messaging` / `notify_alert`, both default on); answer and withdrawal emit `question.answered` / `question.withdrawn` lifecycle events (exactly-once, restart-durable); the polled `GET /api/questions/{id}` answers the answer summary; opt-in `DATAWATCH_ANSWER:` return-path line | ✅ REST + Config (all six endpoints, 503 naming the switch while off; the three notification toggles reachable from REST/MCP/CLI/comm/PWA; both event kinds in the closed set, subscribable; polling read) — MCP tools, CLI/comm question verbs and the PWA queue view deferred (reasons per cell in [operations](operations.md#operator-questions) and [howto](howto/operator-questions-notifications.md)) — see [api/autonomous](api/autonomous.md#operator-question-channel) and [api-mcp-mapping](api-mcp-mapping.md#operator-questions) | ❌ the queue view (list, raise, answer) is not built in the companion app yet — tracked: [datawatch-app#283](https://github.com/dmz006/datawatch-app/issues/283) (filed 2026-10-25; verified open — title: “feat(questions): operator question channel — queue view, answer sheet + settings toggle”) | ❌ the queue view (list, raise, answer) is not built in the companion app yet — tracked [datawatch-app#283](https://github.com/dmz006/datawatch-app/issues/283) (filed 2026-10-25; verified open — title: “feat(questions): operator question channel — queue view, answer sheet + settings toggle”) |

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
| ACME B1 (delegated-zone DNS-01 via an extended `dns_channel`) | internal + YAML | deferred — a much larger subsystem than Phase 2's scoped-token path (B2, shipped); see the BL397 plan doc's scope note |
| Automata lifecycle subscriptions — remaining work: WebSocket payload enrichment + live toasts (follow-up), Phase 5 (webhook sink, federation forwarding) | REST, MCP, CLI, comm, PWA, app (note) | planned — `docs/plans/2026-10-13-prd-lifecycle-events.md` (Phases 3–4 shipped Unreleased; app-side UI tracked in `docs/plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md`) |
| Account usage calculator — comm-channel command (`usage`, `usage advice`, `usage reading`, `usage extra`, `usage work`) | comm channel (the surfaces-and-parity debt item in `docs/plans/2026-10-23-usage-calculator-design.md`) | ✅ daemon side shipped — router parses the grammar and proxies the REST loopback (`internal/router/usage_calculator.go`); unit-tested (`TestUsageCalc*` in `internal/router/usage_calculator_test.go`), live validation pending |

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
| APNs send on alert fire | ✅ Shipped v8.62.x (BL397 Phase 4 / BL335) | `internal/apns` — see below |
| All REST + WS endpoints platform-neutral | ✅ Done | No iOS-only paths |

## APNs Server Work (BL397 Phase 4 / BL335 — shipped v8.62.x)

1. Accept `platform=apns` with APNs device token on `POST /api/device/register` — ✅ already worked (device kind enum pre-existing).
2. Store APNs tokens alongside FCM tokens in the device registry — ✅ already worked.
3. On alert fire, send to all registered APNs tokens via APNs HTTP/2 API — ✅ `internal/apns.Dispatcher`, wired into the real `alertStore.AddListener` alert-fire path (`cmd/datawatch/main.go`). A device that returns `410 Unregistered` is automatically pruned.
4. Config: `push.apns.{key_id,team_id,bundle_id}` + `key_path` or `key_secret` (`${secret:name}`) — ✅ shipped, see [operations.md](operations.md) "APNs Push Notifications".

APNs payload schema (matches the pre-existing FCM schema exactly):
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

**Verification status**: 9 unit tests in `internal/apns`, including a
real cryptographic check (the signed JWT is independently verified via
`ecdsa.Verify` against the key's own public half, not just checked for
well-formed JSON). **Not live-verified** against Apple's real APNs
servers — that needs a real Apple Developer account, a provisioned Auth
Key, and a TestFlight-registered device token, none of which exist in
this environment. Unlike BL397 Phase 1 (ACME), which was fully live-
verified against a real Let's Encrypt directory and a real public host,
this is the one BL397 sub-feature shipped on unit tests alone — flagged
here rather than overclaiming. First real end-to-end push would be the
natural next validation step whenever Apple credentials are available.

Test send: `POST /api/push/apns/test` or `datawatch push apns-test`.

Tracking: closes `dmz006/datawatch#158`/`#183` and
`dmz006/datawatch-app#185`'s server-side half (the app's own token
registration/handling is unaffected, already done on their side).
