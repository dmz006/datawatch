# PWA connection stability: research

Date: 2026-10-21. Research only; no product code was changed.
Sources: `internal/server/web/app.js`, `internal/server/web/sw.js`, `internal/server/api.go`, `internal/audit/cef.go`, `internal/mcp/sx_parity.go`, and the daemon access log via the MCP tool `audit_access_query`.

## Access log

**How it was queried.** The MCP tool `audit_access_query` was called twice, with `action=ws_disconnect` and `action=ws_connect`, `limit=300` each. This reads `/api/audit/access` (the access log). The CLI `datawatch audit` reads the operator audit log instead and returned 0 ws events, so it is not usable for this.

**What the log records.** `internal/server/api.go` (~2085-2140) writes `ws_connect` and `ws_disconnect` with only `remote_ip`, `user_agent` and the principal (and the hop chain for federation peers).
- There is **no close code and no close reason** in the log.
- There is no connection ID, so connect/disconnect pairing is by order only.
- Close codes could not be recorded from this data. The Go read loop breaks on any `ReadMessage` error without logging it.

**Data obtained.** 300 events of each kind, covering 2026-10-20 17:22 to 2026-10-21 14:52 (local time, about 21.5 h). Restricted to the overlap of both windows, that is 595 events. 594 of the 600 events came from `100.64.0.1` (Tailscale), a single Android Chrome user agent. One `ws_disconnect` was from `127.0.0.1`, and the 100.x filter dropped it.
- Concurrency reached up to 6 simultaneous sockets from that IP (several tabs or PWA instances, or leaked sockets). Pairing is therefore approximate: connects and disconnects were matched first-in-first-out.
- Both queries hit the `limit=300` cap. Older history exists but was not examined.

**Results (FIFO pairing, 295 sessions, 111 "all sockets closed, then a new connect" gaps):**

| Metric | Value |
|---|---|
| Session length: min / p25 / median / p75 / p90 / max | 8.1 s / 10.1 s / 46.5 s / 170 s / 2044 s / 12446 s (about 3.5 h) |
| Session length: mean | 593 s |
| Sessions under 30 s | 114 of 295 |
| Sessions 30-60 s | 51 |
| Sessions 60-300 s | 67 |
| Sessions 300-1800 s | 31 |
| Sessions over 1800 s | 32 |
| Most common lengths | 10 s (91 sessions), 46 s (16), 70 s (14), 48 s (9), 34 s (9) |
| Sessions 20-70 s | 80 |
| Disconnect-to-reconnect gap: min / median / p75 / p90 / max | 1.4 s / 2.7 s / 4.0 s / 6.0 s / 63.1 s |
| Gap 1-5 s | 94 of 111 |
| Gap over 30 s | 1 |

**Reading.**
- The reconnect gap is short (median 2.7 s). The reconnect loop itself is working.
- The churn is the problem: the median socket lives under a minute, and the very common ~10 s lifetime is not explained by a heartbeat (see Findings).
- The 46-71 s cluster matches the client watchdog window, `WS_STALE_MS` of 50 s plus up to 20 s of tick slop. A watchdog-forced close lands in about 50-70 s. This is circumstantial and cannot be proven without close codes.
- The ~10 s sessions may be a different cause, such as a reload (see Reload paths) or a socket that dies right after open.
- Intervals are server-side timestamps, so the real user-visible outage is longer than the gap: it also includes the client's detection delay (up to ~70 s), which the log cannot show.

**Query to run for more history or exact data.**
`audit_access_query` with `action=ws_disconnect` and `action=ws_connect`, `actor=unauthenticated`, `since=<RFC3339>`, `until=<RFC3339>`, and a larger `limit` in time slices (a 300-event result is about 75 KB). Pair events per `(remote_ip, user_agent)`.
- To get close codes, add a close-code field to `ws_disconnect` first (see Findings).

## Reconnect cost

`connect()` is at `app.js` ~440-675. Every successful WebSocket `open` triggers the following.

1. State changes: `state.connected = true`, `state.reconnectDelay = 1000` (line ~471), `startWsWatchdog()` (ping every 20 s), `updateStatusDot()`.
2. A "Connected" toast (2 s) is shown. The "Not connected" error toast is removed.
3. `GET /api/health` (~492), only if `state._daemonVersion` is already known. If the version differs it calls `location.reload()` (~500). Other effects: splash handling (see below).
4. `GET /api/config` (~548): reads UI preferences into `state` (suppress toasts, auto-restart, recent minutes, max sessions, summarizer).
5. Splash handling: first connect only, shown at most once per 24 h or on a version change; otherwise removed immediately.
6. A per-view re-render, chosen by `state.activeView` (~562-640):
   - `settings` → `renderSettingsView()`
   - `sessions` → `renderSessionsView()` (full re-render, not the in-place diff)
   - `alerts` → `renderAlertsView()`
   - `autonomous` → `renderAutonomousView()`
   - `dashboard` → `renderDashboardView()`
   - `observer` → `renderObserverView()`
   - `session-detail` → `GET /api/sessions`, then `applySessionUpdate` per session, `onSessionsUpdated()`, and (if alerts view) `renderAlertsView()`, then `_lastPaneFrame = null`, `_termHasContent = false` and a **full `renderSessionDetail(sid)`**. That rebuilds the terminal and input bar and sends a fresh `subscribe`.
7. Server side, on every connect (`api.go` ~2095-2110): an access-log write, plus a full `sessions` list pushed to the client unless the delta stream is the configured default.

Cost per reconnect with the detail view open: 2-3 HTTP requests (`/api/health`, `/api/config`, `/api/sessions`), a full detail re-render, a `subscribe` plus a full pane capture, and the sessions list push. The code comment at `app.js` ~575-585 states the optimized "same session alive" path was removed on purpose (v6.11.13) because it left the view in bad states.

Because the connection churns every minute or so (see Access log), these costs are paid continuously.

## Reload paths

Every `location.reload()` / `window.location.reload()` in `app.js`:

| Line | Trigger | Focus/typing protected? |
|---|---|---|
| ~137 | `setLocaleOverride()`, user changes language | Yes: explicit user action |
| ~500 | WS `open`, `/api/health` version differs from `state._daemonVersion` | **No** |
| ~784 | `sessions` WS message carries a different `version` | **No** (this is also reachable from every sessions broadcast) |
| ~17476 | `window.replaySplash()`, explicit user action | Yes: explicit |
| ~19180 | SW `controllerchange` listener (guarded once per page by `_reloadGuard`) | **No** |
| ~19201 | `window.forcePWAUpdate()`, explicit user action | Yes: explicit |

Notes:
- No `reload()` path checks `document.activeElement`, an unsent input draft, or an open modal. A daemon redeploy or SW update reloads the page under the user's hands. The three unprotected paths all fire on a version change or SW update.
- `grep` found no use of `document.activeElement` or a typing flag guarding any of these.

**Other paths that re-render the whole view** (not a page reload):
- WS `open` re-render by view (see Reconnect cost). No focus or typing check; a connection flap while typing in the session-detail input re-creates the input bar via `renderSessionDetail`. Draft text is lost unless it is saved elsewhere (not verified).
- `onSessionsUpdated()` (~1884): on the sessions view uses `tryUpdateSessionsInPlace()` and falls back to a full `renderSessionsView()` when the visible set changes, or when `selectMode` or `sessionFilter` is set (it always returns false in those cases, so each broadcast does a full re-render). On session-detail it only updates buttons and banners (not a full render).
- `saveToken()` (~17455): `disconnect()` then `connect()` after 500 ms.
- Line ~18094 (`state.reconnectDelay = 1000` plus close of the socket): a manual reconnect path that also resets the delay.

**Service worker (`sw.js`):**
- `install` calls `skipWaiting()` unconditionally. A `SKIP_WAITING` message also calls it.
- `activate` purges old caches, calls `clients.claim()`, and then **navigates every open window** (`w.navigate(w.url)`) when `shouldNavigateOnActivate(keys, CACHE_NAME)` is true. That guard returns true only if some cache key differs from the current `CACHE_NAME`, so a no-op re-activation does not churn tabs.
- The "BL442" label is not present in `sw.js` or `app.js`. The guard described above (`shouldNavigateOnActivate`, comment prefix "GATE") is the only navigate guard found; I assumed it is the one meant.
- Both `activate`-navigate and the page's `controllerchange` reload can fire for the same update: the SW navigates the window, and the new page can still receive a `controllerchange`. `_reloadGuard` is per page load only, so a double reload is possible.
- `CACHE_NAME` is the string `datawatch-v7-0-0-alpha-51` and the comment says it needs bumping per release. If it is not bumped, installed PWAs may not see updates. This is unrelated to churn; noted only.
- `fetch` handler: `/api/*` and `/ws` bypass the SW, so the SW does not affect the socket directly.

## Findings

**Heartbeat versus carrier NAT timeouts.**
- Client: `WS_PING_INTERVAL_MS = 20000` and `WS_STALE_MS = 50000` (`app.js` ~390-391). The watchdog runs every 20 s, sends an application-level `ping`, and force-closes the socket if no message arrived for more than 50 s. Detection of a dead socket takes 50-70 s.
- Server: `ws.go` ~387 sends WebSocket protocol pings on a ticker (period not read in this pass). The read deadline is 60 s, refreshed on every message and pong. The server answers app-level `ping` with `pong` (`api.go` ~2354).
- Carrier (5G/CGNAT) idle timeouts are often 30 s or less. The task statement gives this figure and it was **not** measured here. A 20 s ping is inside that window only if the NAT counts it as traffic in both directions, and a single lost ping plus a 50 s stale threshold means detection can lag two to three intervals after the mapping is gone.
- Tailscale adds its own path changes (DERP vs direct). The traffic pattern is not tested here.
- Match to data: sessions clustering around 46-71 s fit a socket that goes silent and is closed by the watchdog (or by the server's 60 s read deadline). Not proven; close codes are not logged.

**Reset of backoff on every open, no jitter.**
- `state.reconnectDelay` is set to 1000 on every `open` (line ~471) and again at ~173 (initial) and ~18094 (manual reconnect). Backoff is `min(delay*2, 30000)` with **no jitter** (`scheduleReconnect`, ~694-695).
- Consequence: a socket that opens and then dies within seconds resets the delay to 1 s, so the backoff never grows. This matches the observed reconnect gaps (median 2.7 s) and the 91 sessions of about 10 s. All clients on one network that drop together will reconnect in lockstep.

**Prioritized fix list** (suggestions only; none applied):
1. **Log close code and reason, plus a connection ID, on `ws_disconnect`** (server). This is needed to explain the churn, and is low risk. Also log which side closed (read deadline, write failure, client close).
2. **Protect unprotected reloads**: for `app.js` ~500, ~784 and the `controllerchange` reload, defer while an input has focus, a draft is non-empty, or a modal is open; show an "Update available, reload" prompt instead. Remove the duplicate reload source (SW navigate vs `controllerchange`).
3. **Do not reset `reconnectDelay` on `open`**: reset only after the socket has stayed open for a stable period (for example 30-60 s). Add random jitter (for example 0.5x-1.5x) to the delay.
4. **Shorten the heartbeat** to fit a 30 s or shorter NAT timeout: ping every 10-15 s and treat the socket as stale after about 2 missed intervals (for example 30 s). Re-measure after change 1 so the effect is visible in the log.
5. **Make reconnect cheaper**: skip the `/api/health` probe (the `sessions` message already carries the version); fetch `/api/config` only after a daemon version change; on session-detail, resume with `subscribe` rather than a full `renderSessionDetail` when the same session was alive, or preserve the input draft across re-render.
6. **Investigate the ~10 s sessions** (91 of 295): use the close code from item 1, and check the client debug log (`_dbg('WS', 'closed code=...')` already exists) on the Android device.
7. **Check for socket leaks**: up to 6 concurrent sockets from one IP suggests tabs or PWA instances that never close; confirm with the connection ID from item 1.
