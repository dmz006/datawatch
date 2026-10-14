# BL433 — PWA performance audit under load

- **Date:** 2026-10-14
- **Version:** 9.0.82 (`var Version` in `cmd/datawatch/main.go` at time of writing)
- **Status:** needs_review — design/audit pass, no implementation approved
- **Issue:** [GH#243](https://github.com/dmz006/datawatch/issues/243) (this audit) — companion to the Android app audit [GH#242](https://github.com/dmz006/datawatch/issues/242) and its server-side PRD BL432 (shipped v9.0.82)
- **Scope:** `internal/server/web/app.js` (31,071 lines), `internal/server/web/index.html`, `internal/server/web/sw.js`. No code changes in this pass.

> **Every row in the Code inventory below is a code-read hypothesis — to be
> confirmed by measurement.** Nothing here is a final verdict. Line numbers
> are `app.js:<line>` as of `main` @ `1dc135d9`; they drift with every PWA
> commit, so the Method pass should re-anchor them by function name.

## Context

BL432 (v9.0.82) added server-side relief for mobile/PWA clients: compact
session list (`view=summary`), gzip for `/api/*` JSON, alerts paging +
`/api/alerts/count` + an `alerts_read` WS event, a WS `hello` with a delta
session list and subscribed-only output, and `/api/info` capabilities. A grep
of `app.js` finds **none of these consumed by the PWA yet** (no
`view=summary`, `/api/alerts/count`, `alerts_read`, or WS `hello` handling —
the only `'hello'` string is the council SSE viewer at app.js:30414). So the
PWA today still pays the pre-BL432 costs inventoried below.

## Code inventory

Legend for the "Cleared" column: **self** = the callback clears itself on its
next tick when its gate fails (so up to one extra tick of work after leaving);
**never** = module-level timer that lives for the page lifetime and only
early-returns on a gate; **n/a** = event-driven / bounded.

"Visibility" = whether the loop pauses when `document.visibilityState` is
`hidden`. See (a.3) — in practice the answer is **no** for every interval.

### (a) Polling loops

#### (a.1) `setInterval` call sites (25 real calls; the other 3 grep hits at app.js:400, 8912, 10649 are comments)

| # | app.js | Interval | Fetches | Owner view / gate | Cleared on tab/view change | Pauses when hidden |
|---|---|---|---|---|---|---|
| 1 | 408 | 20 s | none — WS `ping` send + staleness check (`startWsWatchdog`) | global, while WS open | cleared on WS close (`stopWsWatchdog`, app.js:422) | no (intentional — liveness) |
| 2 | 1848 | 1 s | none — `BroadcastChannel` presence post | global; gated `activeView==='session-detail'` | never | no |
| 3 | 3907 | 3 s | none — input-bar safety DOM check | global; gated `session-detail` | never | no |
| 4 | 4223 | 5 s | `GET /api/stats` (session header stats bar, `loadSessionStats`) + `refreshSessionPluginWidgets()` (10 s throttled) | session detail | cleared on re-entry (app.js:4151) and in nav teardown (app.js:2148); in-flight guarded | no |
| 5 | 4579 | 5 s | `GET /api/observer/envelopes` + `GET /api/compute/nodes/{ref}/detail` (Status → Stats sub-pane) | session detail, Status tab | self (tab or session change) | no |
| 6 | 4803 | 5 s | `GET /api/sessions/{id}/status` | session detail, Status tab | self | no |
| 7 | 5238 | 80 ms | WS tmux key send (arrow auto-repeat) | only while an arrow key is held | on key release | n/a — user-driven, not a poll |
| 8 | 11082 | 1 s | `GET /api/compute/nodes/{name}/detail`, then `showModal()` re-renders the whole modal each tick | Settings → Compute, detail modal | self, when **no** `.modal-overlay` exists (any modal keeps it alive) | no |
| 9 | 12583 | 5 s | `GET /api/autonomous/prds/{id}/decompose/status` | Automata decompose progress — fallback only after SSE fails 3× | on complete/error, or container removed | **no** — the visibilitychange handler (a.3) closes the SSE but not this poll |
| 10 | 14137 | 10 s | `GET /api/logs?lines=50&offset=…` | Settings → Monitor, `#daemonLogPanel` present | never | no |
| 11 | 17670 | 1 s | none — rewrites `.session-elapsed` text on every card | global (any view with cards) | never | no |
| 12 | 17691 | 1 s | none — rewrites `.session-last-activity` text/dot | global (session detail header) | never | no |
| 13 | 18641 | 60 s | `reg.update()` → network fetch of `/sw.js` | global | never | no |
| 14 | 18763 | 30 s | `GET /api/observer/peers` (header peer-stale badge; `#peerStaleBadge` is in index.html:162, so always present) | **global, every view** | never | no |
| 15 | 20690 | 5 s | `GET /api/capacity?prd_id=…` | Automata detail, `#prdCapacityCard` | self (card removed) | no |
| 16 | 20895 | 5 s | `GET /api/sessions` (**full list**) + `GET /api/compute/nodes/{ref}/detail` per node + `GET /api/stats` (`/api/backends` cached) | Automata detail, `#prdActiveSessionCard` | self (card removed) | no |
| 17 | 20961 | 5 s | `GET /api/observer/envelopes` + compute-node detail | Automata detail, `#prdStatusGraphsSlot` (running/decomposing) | self (slot removed) | no |
| 18 | 21246 | 5 s | `GET /api/stats` + `GET /api/compute/nodes/{ref}/detail` per node | Automata detail session-resources slot | self (slot removed) | no |
| 19 | 22798 | 8 s | `GET /api/observer/peers` + **N×** `GET /api/observer/peers/{name}/stats` (`loadPeerResourceOverview`) | Observer view, `#peerResourceList` | never (gated) | no |
| 20 | 22940 | 8 s | `GET /api/stats` + `GET /api/observer/peers` + **N×** `…/peers/{name}/stats` (`loadSystemStatsGrid`) | Observer view, `#perSystemGrid` | never (gated) | no |
| 21 | 23668 | 20 s | `GET /api/websearch/stats?days=14` | created on first stats-panel render (Settings/Observer); `window._webSearchStatsInterval` guard | **never** — runs for the rest of the page lifetime after first render, in every view | no |
| 22 | 24751 | 30 s | full `renderSessionsView()` (time-ago labels) → also `GET /api/schedules?state=pending` via `loadGlobalScheduleBadge` (in-flight guarded) | Sessions view | never (gated) | no |
| 23 | 24809 | 1 s | none — toggles `.stale-comms` class on running-state badges | global | never | no |
| 24 | 27646 | 8 s | `GET /api/observer/stats` + `GET /api/observer/peers` + `GET /api/observer/config` (`renderObserverPeersCard`); first sets `innerHTML` to a loading spinner, so the card flashes each tick | Observer view, `#observerPeersPanel`; skipped while a peer popover is open | never (gated) | no |
| 25 | 27656 | 8 s | `GET /api/pipelines` + `GET /api/orchestrator/graphs` | Settings → Automata sub-tab; skipped while a modal/popover is open | never (gated) | no |

#### (a.2) `setTimeout` / `requestAnimationFrame` loops (self-rescheduling only; the other ~60 `setTimeout` calls are one-shot UI delays)

| app.js | Cadence | Fetches | Owner | Stops when | Pauses when hidden |
|---|---|---|---|---|---|
| 661 | exponential backoff | WS reconnect | global | on connect | no |
| 1044 | 250 ms debounce per `prd_update` WS msg | `GET /api/autonomous/prds/{id}` or full `loadAutomataPanel()` | Automata view | debounce settles | no |
| 9527 | 2 s | `GET /api/marketplace/ollama/tasks/{id}` | Ollama pull in progress (alert dock) | task done/failed | no |
| 12623 | backoff ≤30 s | decompose SSE reconnect | Automata decompose | 3 retries, then falls to the (a.1) #9 poll | yes — SSE only (see a.3) |
| 15584 | 30 s | `GET /api/mcp/resources/read?uri=datawatch://stats/mcp`; resets list `innerHTML` to a spinner each run | Settings MCP sampling log | when `#mcpSamplingList` missing (checked at next run) | no |
| 16839 | backoff 2 s → 30 s | `GET /api/servers` (eager server list) | global, on failure only | on success | no |
| 22007 | 150 ms × ≤20 | none (DOM poll for deep-linked alert) | Alerts deep link | bounded | n/a |
| 25368 / 25480 | `requestAnimationFrame` (every frame) | frame-count gated: `/api/autonomous/prds[/aggregated]` every 150 frames, `/api/cost[/aggregated]` every 900, `/api/analytics?range=30d` and `/api/compute/nodes` every 1800, `/api/smoke/progress` every 75 (active run) / 900 frames (+ `/api/smoke/progress/{id}` when expanded) | Dashboard view | `activeView !== 'dashboard'` (also cancelled in `navigate`, app.js:2023) | **implicitly** — browsers throttle rAF in hidden tabs, so this is the only loop that effectively pauses |

Hypotheses for measurement:

- The dashboard loop schedules fetches by **frame count**, not wall-clock. The
  code comments assume ~30 fps ("every ~5s" = 150 frames). On a 60 Hz display
  that is every 2.5 s; on 120 Hz every 1.25 s. Canvas drawing (`_drawMultiEKG`,
  `_dashForceStep`) runs every frame regardless.
- Interval #8 re-renders a full modal (pretty-printed JSON) at 1 Hz and stays
  alive as long as *any* `.modal-overlay` exists.

#### (a.3) The single `visibilitychange` listener

Only one exists: app.js:12630, inside `_startDecomposeStream` (Automata PRD
decompose progress). It closes the decompose `EventSource` when the page is
hidden and reconnects on return. It does **not** cover the 5 s fallback poll
(`pollIv`, a.1 #9) started by the same function, and nothing else in
`app.js` checks `document.hidden` / `visibilityState`. `sw.js` has no timers.
Hypothesis: a backgrounded PWA tab (or an installed PWA on a phone with the
screen off but the page alive) keeps every global loop in (a.1) running —
at minimum #1, #2, #3, #11, #12, #13, #14, #23 in every view, plus #21 once
created, plus whatever view-gated loops match the view the tab was left on.
Browsers clamp background timers (≥1 s, and more aggressively after minutes),
but fetches still fire.

### (b) Fetch call-sites for endpoints flagged by the app audit

#### `/api/alerts`

| app.js | Call | Trigger | Notes |
|---|---|---|---|
| 21974 | `fetch('/api/alerts')` (raw, unpaged) | `openAlertDeepLink` — notification / URL deep link | Full list fetched just to find one id. |
| 22085–22094 | `fetch('/api/alerts/aggregated')` in "all" mode, `/api/proxy/{srv}/api/alerts` remote, `/api/alerts` local | `renderAlertsView()` | Full unpaged list on every call. Callers below. |
| 22132 | `POST /api/alerts {all:true}` | mark-all-read | write |
| 22450 | `POST /api/alerts {all:true, delete:true}` | clear-all, then re-render (refetch) | write |
| 24698 | `fetch('/api/alerts')` (raw, unpaged) | app init — only to read `unread_count` | Could be `/api/alerts/count` since v9.0.82. |

`renderAlertsView()` (= a full alerts refetch + full list re-render) is called from:
navigate to Alerts (app.js:2171); WS reconnect while on Alerts (app.js:554);
**`updateSession()` whenever a session changes state while on Alerts**
(app.js:1602); the search box **`oninput` on every keystroke** (app.js:22319);
tab switches / clear / send (app.js:22436–22488). Hypothesis: a burst of
`session_state` WS messages or fast typing in the search box fires multiple
concurrent, un-deduplicated `/api/alerts` fetches (no in-flight guard), each
followed by a full `innerHTML` re-render. The WS `alert` case does **not**
refetch (see c).

#### `/api/observer/peers` and `/api/observer/peers/{name}/stats`

| app.js | Function | Fetches | Trigger |
|---|---|---|---|
| 9552 | `ensureFreeObserversCached` | `/api/observer/peers/free` | Compute node form |
| 10438 | Compute add/edit form | `/api/observer/peers` (after `…/free`) | form open |
| 18734 | `updatePeerStaleBadge` | `/api/observer/peers` | 30 s global (a.1 #14) + once at load (app.js:18794) |
| 22737 / 22745 | `loadPeerResourceOverview` | `/api/observer/peers` + N× `/peers/{name}/stats` | Observer view entry (via `loadStatsPanel`, app.js:22534) + 8 s (#19) |
| 22867 / 22876 | `loadSystemStatsGrid` | `/api/stats` + `/api/observer/peers` + N× `/peers/{name}/stats` (self filtered out) | Observer view entry (app.js:27387) + 8 s (#20) |
| 23074 | `loadObserverPeers` | `/api/observer/peers` + `/api/compute/nodes` | Observer view entry (via `loadStatsPanel`, app.js:22526); filter / group toggles; after delete |
| 23301 | peer drill-down popover | `/peers/{name}/stats` | click |
| 23398 | peer delete | `DELETE /peers/{name}` | click |
| 27586 | `renderObserverPeersCard` | `/api/observer/stats` + `/api/observer/peers` + `/api/observer/config` | Observer view entry (app.js:27396) + 8 s (#24) |

Concurrency hypothesis: entering the Observer view fires **four** independent
`GET /api/observer/peers` (27387 grid, 22526 list, 22534 resource overview,
27396 card) plus **2N** `GET /api/observer/peers/{name}/stats` (overview and
grid each fan out per peer). Then every 8 s, three independent intervals
(#19, #20, #24) — not phase-aligned — repeat **three** `/peers` fetches and
**2N** per-peer stats fetches, and the global 30 s badge (#14) adds a fourth
`/peers` fetch. None share a cache or in-flight promise.

#### `/api/sessions` and the all-servers aggregate

| app.js | Call | Trigger | Notes |
|---|---|---|---|
| 597 | `fetch('/api/sessions')` (raw) then `updateSession(s)` **per session** | WS reconnect while in session detail | each `updateSession` calls `onSessionsUpdated()` → N list updates in a row |
| 16482 | `_loadAllServersSessions()` → `fetch('/api/sessions/aggregated')` → `renderSessionsView()` | (1) server picker → "all" (app.js:16460); (2) **every WS `sessions` push while `activeServer==='all'`** (app.js:730) | no in-flight guard / debounce |
| 20778 | `apiFetch('/api/sessions')` (full list, filtered client-side to one PRD) | Automata detail active-session card, 5 s (#16) | full list to show a handful of sessions |

Concurrency hypothesis: in "all" mode each WS `sessions` push starts a new
aggregated fetch with no guard, so a burst of pushes yields overlapping
`/api/sessions/aggregated` requests whose responses can land out of order.
The same `sessions` case also runs `onSessionsUpdated()` immediately on the
stale `state.sessions`, then `_loadAllServersSessions` does a second, full
`renderSessionsView()` when it resolves — two renders per push.

### (c) WS `handleMessage` (app.js:714; dispatched from the `message` listener at app.js:620, which does `JSON.parse` then the switch)

| Case | app.js | Refetch? | Re-render | Work when no terminal/detail pane is open |
|---|---|---|---|---|
| `sessions` | 716 | **yes** in "all" mode (`_loadAllServersSessions`, see b) | `onSessionsUpdated()` (app.js:1917): on Sessions view → `tryUpdateSessionsInPlace()` (app.js:1949), falling back to full `renderSessionsView()` (app.js:2275). On detail view → header buttons/banners only. | Loops all sessions for `channelReady` bookkeeping regardless of view. |
| `session_state` | 760 | no (but on Alerts view triggers `renderAlertsView()` = `/api/alerts` refetch if state changed, app.js:1602) | `updateSession()` (app.js:1588) → `findIndex` + **`onSessionsUpdated()` for the whole list**, i.e. one session change re-runs the full in-place diff (or full render). In detail view may call `renderSessionDetail()` if the input bar must appear. | — |
| `output` | 768 | no | DOM append only if this session's detail is open (`appendOutput`, app.js:1635; no-op when xterm is active) | **Not skipped.** For every session, every line: push into `state.outputBuffer` (re-`slice` to 500 per message when over), then `stripAnsi` + `handleChannelReply()` per line (app.js:1557), which pushes into `state.channelReplies[sid]` and re-`slice`s to 1000 on each push once full. Hypothesis: per-line CPU + allocation for sessions nobody is looking at. (BL432's subscribed-only output on the server side would remove this if the PWA opted in.) |
| `raw_output` | 794 | no | log-mode DOM append only for the open session | Buffers to `state.rawOutputBuffer` (500) for every session regardless. |
| `stats` | 833 | no | Settings → `renderStatsData` full re-render; detail Stats tab → `renderSessionStats()` which **refetches** `/api/observer/envelopes` and resets the pane to a spinner | Payload cached only. |
| `pane_capture` | 849 | no | xterm write: first frame `reset()`, then `\x1b[2J\x1b[3J\x1b[H` + full frame; dedupe vs `_lastPaneFrame`; throttle drops frames <33 ms apart (leading-edge, so the *last* frame of a burst can be dropped until the next capture) | **Skipped** unless `activeView==='session-detail'` and `activeSession===session_id` (and buffered once if xterm not ready). Still JSON-parsed. Also builds `capLines.join('\n')` keys and keeps up to 300 frames in `state._captureHistory`. No client `unsubscribe` exists (only `subscribe`, app.js:3204/3700/3711), so frames for a previously opened session keep arriving after navigating away — hypothesis to confirm server-side. |
| `alert` | 1006 | **no** | `handleAlert()` (app.js:1135): counter increments, `updateAlertBadge()`, toast. Does **not** re-render the Alerts view even when it is open. | badge + toast only |
| `prd_update` | 1033 | yes, 250 ms debounced (a.2) | Automata detail incremental `_liveUpdateDetail`, else full `loadAutomataPanel()` | only when on Automata view |

#### `tryUpdateSessionsInPlace` — "in-place keyed update" in name only?

It is keyed (cards matched by `data-full-id`, falls back to a full render if
the id order/length differs, or in select/filter mode), but per update it
regenerates `sessionCard()` HTML for **every** visible card and compares it to
the live `card.outerHTML`, replacing the card when different. Hypothesis: the
1 s tickers (#11 fills `.session-elapsed` text, which `sessionCard` renders
empty at app.js:2966; #23 toggles `stale-comms`) and `timeAgo()` text mean the
live DOM practically never equals fresh HTML for active sessions, so every WS
`sessions`/`session_state` message replaces every active card's DOM. In tree
view (`renderSessionsAsTree`, app.js:2648) DOM order may differ from the flat
`visible` order, which would force the full-render fallback every time — to
verify.

### (d) Per-card / relative-time tickers that run while idle

| app.js | Interval | What it touches | Notes |
|---|---|---|---|
| 17670 | 1 s | `document.querySelectorAll('.session-elapsed[data-started-at]')` → `textContent` | every view; full-document query each second |
| 17691 | 1 s | `.session-last-activity[data-last-activity]` → `textContent` + dot colour | every view |
| 24809 | 1 s | `.state[data-channel-evt][data-state]` → class toggle | every view |
| 1848 | 1 s | `BroadcastChannel.postMessage` (only in session detail) | cross-tab presence |
| 24751 | 30 s | **full** `renderSessionsView()` to refresh time-ago labels (+ schedules badge fetch) | Sessions view; discards scroll/hover state |
| 3907 | 3 s | input-bar DOM check | session detail |

### (e) `console.log` / `console.debug` calls

11 `console.log` + 8 `console.debug` (the task brief estimated ~11 total).
Separately, a private `_dbg()` ring buffer (app.js:146, 200 entries) wraps
`window.fetch` (app.js:155) but only records non-OK responses and failures,
and is not called per WS message.

| app.js | Call | Path | Hot path? |
|---|---|---|---|
| 479 | `console.log` daemon version changed during reconnect | WS reconnect, immediately followed by reload | no (once) |
| 753 | `console.log` daemon version changed | inside WS `sessions` case, immediately followed by reload | in the WS handler but only on version change — no |
| 4289, 4325, 4332 | `console.debug` session plugin widget failures | 5 s session stats tick, **error path only** | only when a plugin widget fails — then once per tick |
| 5563, 5581, 5591, 5748, 5766, 5785, 5795 | `console.log` `[voice]` | voice recording stop / transcribe | no (user action) |
| 18635 | `console.log` SW registered | startup | no |
| 18684 | `console.log` SW controller changed | SW update | no |
| 25731, 25741 | `console.debug` plugin widget render failure | dashboard plugin widgets, error path | no |
| 25968 | `console.debug` plugin list failure | add-card panel | no |
| 26128, 26133 | `console.debug` plugin widget card render failure | dashboard, error path | no |

Hypothesis: no console call sits on a steady-state WS or render hot path.
(Also present: 12 `console.warn`/`console.error`, e.g. the xterm write
recovery at the end of the `pane_capture` case — error paths only.)

### Summary of hypotheses to measure

1. No loop pauses when the page is hidden (except the dashboard rAF and the decompose SSE).
2. Observer view: 4× `/api/observer/peers` on entry, 3× per 8 s after, plus 2N per-peer stats per 8 s; no shared cache.
3. WS `output` does per-line work for every session even with no pane open.
4. One `session_state` message re-diffs / re-renders the whole session list; the in-place path likely replaces every active card due to the 1 s tickers.
5. "All servers" mode: each WS `sessions` push → unguarded aggregated refetch + double render.
6. Alerts view refetches the full unpaged list on every keystroke and every session state change.
7. Dashboard fetch cadence scales with display refresh rate.
8. Three global 1 s DOM tickers + a 30 s full Sessions re-render while idle.
9. The PWA consumes none of BL432's v9.0.82 payload features yet.

## Method

_To be filled by the next task._

## Measurements

_To be filled by the next task._
