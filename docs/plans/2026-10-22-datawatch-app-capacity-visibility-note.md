# datawatch-app — Capacity, back-off and budget visibility, plus the audit query — app-side note

**Date:** 2026-10-22
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart:** this repo — the visibility, audit and parity close-out phase of [`plans/2026-10-14-concurrency-and-budget-design.md`](2026-10-14-concurrency-and-budget-design.md) (unreleased development build)
**Status:** **a GitHub issue on `dmz006/datawatch-app` is needed and has not been filed.** It cannot be filed from the session that wrote this note (no access to that repository), so it must be filed by the supervisor or the operator, using this note as the issue text. The daemon surface below lands with this build; the app UI is not built here (per this repo's parity policy — the app repo owns its UI).

## Context

Autonomous tasks can wait for several different reasons: a pool is full, a compute node is busy, a backend is in a usage back-off, an LLM's dollar budget window is full, the global cool-down is active, a task is awaiting an external step, or another task holds the project tree lock. Until now the apps could only show a one-line `wait_reason` string. This build gives every waiting task a machine-readable **cause**, a plain-language **reason**, a **reset time** when one is known, and the **holder** it is waiting on; and gives every running task a list of what it **holds**. It also adds a `reset_known` flag to each back-off entry, and a query for the audit events that the capacity gate now emits.

The PWA already renders all of this (a Capacity card on the Automata list, cause chips and a Holds line in the task detail and on the session Status tab). Android and iOS have a capacity card (pools, limits, wait queue) and need to catch up with the fields below.

## Fields to parse

### `GET /api/capacity` — new `tasks` object

```json
{
  "pools": [ … ], "leases": [ … ], "waiting": [ … ],
  "backoff": [
    { "backend": "claude-code", "until": "2026-10-22T18:30:00Z",
      "reason": "usage limit reached, resets 6:30pm", "reset_known": true }
  ],
  "budgets": [ … ],
  "tasks": {
    "waiting": [
      { "task_id": "t_ab12", "prd_id": "5dfb0001", "prd_title": "…", "task_title": "…",
        "cause": "pool_full",
        "reason": "pool llm:claude-code full (2/2, held by t_cd34)",
        "since": "2026-10-22T18:00:00Z",
        "resets_at": "2026-10-22T18:30:00Z",
        "holder": "t_cd34" }
    ],
    "running": [
      { "task_id": "t_cd34", "prd_id": "5dfb0001", "task_title": "…",
        "pools": ["host", "llm:claude-code", "node:gpu-a"],
        "node": "gpu-a", "session_id": "5a9e0c41",
        "since": "2026-10-22T17:55:00Z",
        "note": "" }
    ]
  }
}
```

Field notes:

- `tasks.waiting[]` — one entry per queued task. `cause` is one of `pool_full`, `node_busy`, `backoff`, `budget`, `global_cooldown`, `await_external`, `tree_lock`; the field can be absent on an older daemon, so fall back to showing `reason` alone. `reason` is the plain sentence to display. `resets_at` is present only when the daemon knows when the wait ends (a back-off with a parsed reset time, or a budget window rollover). `holder` names the task or session the wait depends on, when there is one.
- `tasks.running[]` — one entry per running task. `pools` lists the pools and locks the task holds; `node` and `session_id` are present when known; `note` is a sentence for a task that is parked while holding resources (show it after the pool list).
- Both arrays are always present and may be empty; never treat a missing array as an error.
- `?prd_id=<id>` limits `tasks` to one Automaton.
- `backoff[].reset_known` — `false` means `until` is only the maximum back-off the daemon applied because the rate-limit message named no reset time. Show "reset time unknown" instead of the clock time when it is `false`.
- `budgets[]` — already in the payload for the existing budget work; the apps now render it (see layout below). Fields: `llm`, `ceiling_usd`, `spend_usd`, `peer_spend_usd`, `remaining_usd`, `window`, `window_start`, `window_end`, `rolls_over_at` (optional), `full`, optional `allocation` (`allocation_pct`, `estimated`, `allocation_usd`, `plan_tier`, `basis`) and optional `peers`.

### Session status — `automaton` block

`GET /api/sessions/{id}/status` (the session status board) carries, inside its `automaton` block for a session linked to an autonomous task:

| Field | Meaning |
|---|---|
| `wait_reason` | existing plain-language reason; unchanged |
| `wait_cause` | the cause code above; omitted when the task is not waiting |
| `wait_resets_at` | RFC 3339 time the wait ends; omitted when unknown |
| `holds` | array of pool and lock names the task holds; omitted when it holds nothing |
| `holds_note` | one sentence for a parked task; omitted when empty |

All five are optional; an older daemon omits the last four.

## What to display

**Cause chip and reason text** (task detail and session status):

| `cause` | Chip text |
|---|---|
| `pool_full` | Pool full |
| `node_busy` | Node busy |
| `backoff` | Back-off |
| `budget` | Budget |
| `global_cooldown` | Cool-down |
| `await_external` | Awaiting external |
| `tree_lock` | Tree lock |

Show the chip, then the `reason` text next to or under it. When `resets_at` / `wait_resets_at` is present, append "Resets at <local time>"; when it is absent, show nothing (do not invent a time). When the cause is missing, show the reason text with no chip. For a running task show a **Holds** line: the `pools` list, then the `note` when there is one. All reason and pool text is data from the daemon: escape or render it as plain text, never as markup. The chip labels, "Holds" and "Resets at" are new strings and go through the app's locale pipeline.

**Capacity and budget card** (the Automata list, above the list; the PWA is the reference):

1. *Pools* — one row each: label (host sessions, compute node, LLM), `used/limit` count, a usage bar, and the holders by short id.
2. *Usage back-off* — one row per entry: backend, "Resets <time>" when `reset_known` is true or "reset time unknown" when it is false, and the reason.
3. *LLM budgets* — one block per entry in `budgets`: "`$spend of $ceiling`" (spend is `spend_usd` plus `peer_spend_usd`), a **spend bar** (green below 75 percent, amber from 75, red at 100 or when `full`), "Window ends" and "Rolls over" times, and a red "budget window full" badge while `full` is true. When `allocation` is present also show the share ("Allocation: N% (estimated)") with a tooltip or long-press text saying the figure is an estimate from the plan tier, not a measured number. The **estimate label is required** whenever the allocation share is shown; the share never appears without it.
4. The card is hidden when the response has no pools, back-offs or budgets. The PWA polls it every 5 seconds while the Automata list is visible; the apps should poll at a comparable rate only while that screen is in front.

## Audit query — for any app screen that lists audit events

`GET /api/audit` returns entries newest first. Filters, all optional query parameters:

| Parameter | Meaning |
|---|---|
| `actor` | exact match on who did it (`operator`, `session:<id>`, `peer:<name>`) |
| `action` | exact match, **or a prefix match when the value ends in `*`**: `autonomous_budget_*` lists the budget family, `autonomous_*` lists every autonomous event |
| `subject` | exact match on what the event is about (a task id, a backend name or an LLM name) |
| `session_id` | exact match on the session |
| `since`, `until` | RFC 3339 bounds; a value that is not RFC 3339 returns `400` with a message, not an ignored bound |
| `limit` | positive integer; default 100 |

The `*` must be sent as-is (URL-encode it as `%2A` if the HTTP client requires). The capacity gate emits these actions: `autonomous_capacity_wait`, `autonomous_capacity_admitted`, `autonomous_usage_backoff_set`, `autonomous_usage_backoff_cleared`, `autonomous_usage_backoff_auto_cleared`, `autonomous_usage_backoff_patterns_rechecked`, `autonomous_budget_ceiling_wait`, `autonomous_budget_window_resumed`, `autonomous_budget_window_rollover`. The Settings → Audit & Access Log screen the apps are adding (see the parity table) should offer the action prefix and subject filters; a "why did this task wait" view can query `subject=<task id>&action=autonomous_capacity_*`.

## Other surfaces (reference — the app talks REST)

| Surface | Capacity | Audit |
|---|---|---|
| MCP | `capacity_status` (carries `tasks`, `reset_known`) | `audit_query` (`actor`, `action`, `subject`, `session_id`, `since`, `until`, `limit`) |
| CLI | `datawatch capacity` prints queued reasons and holdings | `datawatch audit --actor --action --subject --session-id --since --until --limit` |
| Comm | `capacity` reply shows the same text as the CLI | `audit actor=… action=… subject=… session_id=… since=… until=… limit=…` |

## Android build guidance

- Extend the existing capacity card: add the back-off line with the reset or unknown label, the budgets block with spend bars and the estimate label, and the holder ids on pool rows.
- Task detail and the session status view: cause chip, reason, optional reset time, Holds line. Reuse the app's existing chip component.
- Parse every new field as optional; the apps talk to daemons of different ages.
- i18n: the new strings go through the app's locale pipeline; no English-only UI.

## iOS build guidance

- Mirror the Android surface in the existing Automata and session status views. No iOS-specific behavior; no push or transport change.

## Parity surface for this note (app repo)

| Surface | Status |
|---|---|
| PWA capacity and budget card, cause chips, Holds line | ✅ done in the daemon repo |
| Android: capacity card back-off reset label, budgets block with spend bars and estimate label | ❌ — work item for this note |
| Android: cause chip, reason, reset time and Holds line in task detail and session status | ❌ — work item for this note |
| Android: audit screen action-prefix and subject filters | ❌ — part of the audit screen work already tracked |
| iOS: same three items as Android | ❌ — same issue, same scope |

## Open questions (app-repo)

1. Should the budgets block live on the same card as the pools, or as its own card on the Automata list?
2. Should the apps poll `GET /api/capacity` on a timer, or refresh only on pull-to-refresh and when a task's status changes?

## Follow-up: node beside each holder (2026-10-24)

Mobile-Parity Rule follow-up for the holder-node change. The daemon now carries the
node each holder's lease sits on, and the PWA capacity card shows it.

**New field (optional, parse as absent when missing):** `GET /api/capacity` → each
`pools[]` entry → `hold_nodes` (array of strings, parallel to `holders`). An entry is
`""` when that lease names no node. The field is omitted when no holder has a node.

**Display rule (same on every client):** show the node name as a small muted label
beside each holder's short id in the pool's holder line, and only when the pool's
holders span two or more distinct nodes. A pool whose holders all sit on one node
(or have no node) renders exactly as before. The node name is data from the
daemon: render it as plain text, never as markup. No new locale string is needed,
because only the node name is shown, with no added word.

**Android and iPhone/iOS work item:** add the holder-node label to the capacity card
pool rows, under the rule above. Capability parity is required; each platform uses its
own layout.

**Issue status:** not filed from the daemon session that wrote this section, which has
no access to `dmz006/datawatch-app`. It must be filed by the supervisor or the operator
as a `[sync]` issue (Mobile App Sync Rule), using this section as the issue text.

| Surface | Status |
|---|---|
| PWA capacity card: node beside each holder | ✅ done in the daemon repo |
| Android: node beside each holder on the capacity card | ❌ — work item for this follow-up |
| iOS: same as Android | ❌ — same issue, same scope |
