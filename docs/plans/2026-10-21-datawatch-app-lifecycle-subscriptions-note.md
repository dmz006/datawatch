# datawatch-app — Automaton lifecycle event subscriptions (Phase 3) — app-side note

**Date:** 2026-10-21
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart:** this repo — Phase 3 of [`plans/2026-10-13-prd-lifecycle-events.md`](2026-10-13-prd-lifecycle-events.md) (Unreleased)
**Status:** **file a GitHub issue on `dmz006/datawatch-app` when this daemon build merges.** The daemon surface below lands with this build; the app UI is not built here (per this repo's parity policy — the app repo owns its UI).

## Context

Phase 3 (this build) adds *persisted* Automaton lifecycle event subscriptions to the daemon: "when this event fires, deliver it there" — with sinks `comm` / `session` / `topic` / `alert`, an optional `send_input` action, `one_shot`, `expires_at`, `coalesce`, and full CRUD across REST / MCP / CLI / comm. Phase 2 (v9.3.0) already gives Android/iOS *event delivery* — the daemon's `topic` sink publishes to the existing SSE → UnifiedPush (Android) / APNs (iOS) topic path; the app already renders those pushes. What is new in this build is **subscription management** (create / list / edit / delete rules), which the app does not yet surface.

## Subscribable events (kinds)

- All PRD status: `prd.draft`, `prd.planning`, `prd.needs_review`, `prd.approved`, `prd.revisions_asked`, `prd.active`, `prd.running`, `prd.completed`, `prd.rejected`, `prd.cancelled`, `prd.archived`, `prd.blocked`, `prd.failed`, `prd.paused`
- All task statuses: `task.<status>` (pending, queued, in_progress, running_tests, verifying, completed, failed, blocked, cancelled, waiting_capacity, awaiting_approval)
- Session: `task.session_started`, `task.session_cleared`
- PRD updates: `prd.updated` (persist + status-write sites)

**App-side recommendation:** default filters should follow the operator's existing notification preferences (the v9.3.0 `autonomous.notify.default_events` set) — do not invent new defaults unilaterally; the daemon's default-set key is the single source of truth for "what fires by default".

## Delivery path (already shipped — v9.3.0 daemon)

```
daemon topic sink  →  server.PublishToTopic  →  SSE
   → Android: UnifiedPush → FCM  ·  iOS: APNs registration  →  app push UI
```

No new transport is needed for *delivery* in this phase. The app work in this note is the **management UI** against the REST surface below (and parity of any `send_input` action text shown to the operator, e.g. "this rule types `<text>` into session `<id>`").

## REST surface (this build — Unreleased)

| Method & path | Auth | Body / response |
|---|---|---|
| `GET /api/autonomous/subscriptions[?owner=<owner>]` | `autonomous:read` | → `200` `{ "subscriptions": [Subscription] }` |
| `POST /api/autonomous/subscriptions` | `autonomous:write` | → `201` + the stored record (with generated `id`) |
| `DELETE /api/autonomous/subscriptions/{id}` | `autonomous:write` | → `200` |

Errors: `400` malformed payload or unknown event kind, `404` unknown id, `503` when the daemon's lifecycle engine is not wired (should not occur in a normal daemon).

### Subscription shape (JSON)

```json
{
  "id": "sub_01J…",
  "owner": "operator",
  "filter": {
    "kinds": ["task.session_started"],          // closed known-kind set — omit to match level
    "level": "task",                             // "prd" | "task"
    "prd_id": "",                                // optional: scope to one Automaton
    "task_id": "task_7"                          // optional: scope to one task
  },
  "sink": { "type": "session", "target": "5a9e0c41" },   // comm | session | topic | alert
  "action": { "type": "send_input", "text": "Start with the Setup section of TASK.md." },  // optional; session sinks only; max 4096 chars
  "coalesce": false,
  "one_shot": true,
  "created_at": "2026-10-21T12:00:00Z",
  "expires_at": "2026-10-21T23:59:59Z"           // optional TTL
}
```

Field notes: `filter.kinds` is a closed set (the daemon rejects unknown kinds at creation with a message containing `not a known event kind`); at least one of `kinds`/`level` must be set; `sink.type` ∈ `comm` | `session` | `topic` | `alert` (the daemon **rejects** `webhook` / `mcp_queue` in this release — surface them as disabled in the UI with a "later release" hint); `action` is only valid with a `session` sink and is capped at 4096 chars.

Semantics the UI should communicate: `one_shot` = removed after first *successful* delivery (one retry on failure, then log + drop); `expires_at` = the rule stops matching at the timestamp (janitor prunes); `coalesce` = human-facing sink deliveries are batched in quiet windows.

## MCP (for reference — the app talks REST, not MCP)

`autonomous_subscribe(kinds, level, prd_id, task_id, sink_type, sink_target, action_text?, one_shot?, coalesce?, expires_in_seconds?)` → the stored record; `autonomous_unsubscribe(id)`; `autonomous_subscriptions()`. The MCP tools proxy the same daemon store; `expires_in_seconds` maps to `expires_at` server-side.

## CLI (reference)

```
datawatch autonomous subscribe --task <id> --on session_started \
    --send "Start with the Setup section of TASK.md" \
    --sink session --target <session> --one-shot
datawatch autonomous subscriptions [--owner <owner>]
datawatch autonomous unsubscribe <id>
```

## Comm (reference)

`autonomous subscribe <kinds…> [prd=<id>] [task=<id>]` / `autonomous subscriptions` / `autonomous unsubscribe <id>` — owner-scoped (caller = owner = sink target).

## Android build guidance

- Surface: **Automata screen** → per-Automaton sheet (next to the existing review/approve actions) + a global "Event rules" entry; reuse the existing settings-card component pattern. The app already has an Automata list + status views — the subscription editor is a CRUD form over the REST shape above.
- Push receipt is unchanged (v9.3.0 topic path). Add at most a "matched rule" tag to the push payload if the daemon starts including it in a later release — do not block on it.
- No new daemon ports or bridges; all management is HTTPS to the existing daemon endpoint.
- Offline: manage offline-queue (enqueue create/delete, flush on reconnect) per the app's existing offline-queue pattern; a subscription's one-shot/expire semantics are enforced daemon-side, so a flushed create is still valid at delivery time — safe under the existing queue.
- i18n: new strings go through the app's locale pipeline; no English-only UI.

## iOS build guidance

- Mirror the Android surface in the existing Automata UI; APNs delivery path unchanged.
- Same offline-queue note (one-shot + expiry are daemon-authoritative, so queued management writes are safe).
- The `send_input` action text should render as a quoted preview with the 4096-char limit enforced client-side.

## Parity surface for this note (app repo)

| Surface | Status |
|---|---|
| Push delivery for `topic`-sink events | ✅ already shipped with the v9.3.0 daemon (UnifiedPush / APNs) |
| Subscription list / create / delete UI (both platforms) | ❌ — the work item for this note |
| `send_input` action preview + 4096-char limit | ❌ — part of the UI work item |
| Disabled-but-visible `webhook` / `mcp_queue` sink types ("later release") | ❌ — part of the UI work item |

## Open questions (app-repo)

1. Where in the nav: per-Automaton sheet vs. a top-level "Event rules" screen vs. both (list + drill-in)?
2. Should "turn on delivery for event kind X" default to the daemon's `autonomous.notify.default_events` set, or be purely operator-picked?
3. Do we want a quick-action on the push itself ("stop notifying me for this Automaton") that maps to `DELETE` on the matching `owner` filter?
