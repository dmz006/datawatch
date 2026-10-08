# PWA federated error visibility: 401/403/502 distinction across views

**Date:** 2026-10-08
**Version at planning time:** v8.73.7

## Scope

Operator-approved full-scope round. Started as a Settings-tab federation
task, expanded twice mid-session to cover the same underlying pattern
across every federation-aware PWA view:

- `internal/server/web/app.js`
- `internal/server/web/locales/{en,de,es,fr,ja}.json`
- `internal/server/web/app-fed-cap-errors.test.js` (new)
- `internal/server/web/app-fed-conn-status.test.js` (one assertion fixed
  to match the new, more specific 502 behavior — see Phase 1)

No Go files touched. No change to `scripts/run-tests.sh`,
`internal/server/multiserver/store_test.go`, or the v9.0.0 release
pipeline (reserved for other in-flight work).

## Problem

`apiFetch()` already auto-proxies any call through
`/api/proxy/<name>/...` when a specific remote server is selected, and
the server already returns meaningful bodies on 403 (`federation peer
lacks capability: <cap>`, from `federation_cap.go`'s `fedCap()`) and 502
(`proxy error: <real dial error>`, from `proxy.go`/`api.go`). But almost
nothing on the client read those bodies — call sites either showed a
bare "HTTP 403" / silently failed / spun forever, and several views
(Automata, Dashboard) actively swallowed the error via an empty
`.catch(() => ...)` before it ever reached anything visible. Sessions'
own `_checkFederatedConnection` had already been fixed for 401 but
conflated 401 (bad token) and 403 (bad capability) under one message.

## Phase 1 — Central classifier (`_fedFetchError`)

Renamed `_fedCapError` → `_fedFetchError`; it now distinguishes three
situations instead of two:

- **401** — bad/missing token → `fed_conn_error_auth` (unchanged wording)
- **403** — valid token, missing capability → server's real body text
  verbatim (`federation peer lacks capability: sessions:list` etc.),
  falling back to `fed_conn_error_forbidden` only when the body is empty
- **502** — genuinely unreachable (proxy dial failed) → server's real
  body text verbatim, falling back to `fed_conn_error_unreachable` only
  when the body is empty

`apiFetch()` applies this automatically for any proxied call (401/403/502
→ `_fedFetchError`), so every current and future `apiFetch`-based caller
gets real error text without reimplementing the split per call site.
`_checkFederatedConnection` (Sessions' own probe, not `apiFetch`-based)
was updated to route through the same classifier instead of its own
narrower 401/403-only check — this is what surfaced a real regression in
the pre-existing `app-fed-conn-status.test.js` 502 test, which asserted
on a bare `/502/` match; fixed to assert on real dial-failure text
instead, matching the new (correct) behavior.

Status: **Done**, shipped this round.

## Phase 2 — Alerts, Automata, Dashboard brought to the same standard

- **Alerts**: primary fetch now classifies 401/403/502 via
  `_fedFetchError` (previously had no error handling of any kind on this
  path — `Promise.all([...]).then(([data, cmds, freshSessions]) => ...)`
  with no `.catch` at all).
- **Automata**: `loadAutomataPanel()`'s `.catch(() => ({ prds: [] }))`
  removed — it was swallowing every failure into a fake empty success
  before the existing (correct) outer `.catch` ever saw it. Also cleaned
  up the outer catch and `loadAutomataTemplatesPanel`'s matching catch to
  render `err.message` instead of `String(err)` (avoids a stray "Error:"
  prefix).
- **Dashboard**: architecturally different from the others — a
  continuous `requestAnimationFrame` loop with 6+ independently-polled
  cards, all deliberately silent (`.catch(() => {})`) by design for a
  stable local daemon's background refresh. Judged full per-poll
  instrumentation out of scope for this round; added a single one-time
  banner on the *initial* PRDs load only, shown when `state.activeServer`
  is a specific remote. **Flagged for operator sign-off**: periodic
  re-poll failures after the initial successful load remain silent, as
  they were before.
- **Observer**: same problem shape as Dashboard — ~12 independent
  sub-card loaders (`loadStatsPanel`, `loadPluginsStatus`,
  `loadEBPFStatus`, `loadEBPFNetworkTraffic`, `loadObserverPeers`,
  `loadObserverClusterNodes`, `loadChannelBridge`,
  `loadChannelDiagnostics`, `loadPeerResourceOverview`,
  `loadBackendHealthCard`, `loadObserverEnvelopesCard`,
  `loadAcmeHealthCard`), all already routed through `apiFetch` (so they
  already benefit from Phase 1's classification at the fetch layer) but
  most swallow the resulting message into a generic string. Instrumented
  only the primary, most-visible card — `loadStatsPanel`'s System
  Statistics card — to surface the real federated error instead of a
  generic "Stats unavailable." **Flagged for operator sign-off**: the
  other 11 sub-cards still show generic/silent failure on federated
  denial; not instrumented individually this round given the scope
  already covered elsewhere.

Status: **Done** (Alerts/Automata), **Partial by design** (Dashboard,
Observer) — both flagged above.

## Phase 3 — Unreachable-vs-denied distinction everywhere + picker chip state

- The "could not reach this server" message (previously Sessions-only,
  via `_checkFederatedConnection`) is now available to every
  `apiFetch`-based view automatically via Phase 1's 502 handling — a
  genuinely-down host (connection refused/timeout) gets `proxy error:
  ...` text, distinct from a 403 capability denial.
- Server picker chips: added background reachability probing
  (`_probePickerReachability`), bounded to a short per-host timeout, that
  never blocks the picker's own synchronous render. The bar renders
  immediately as before; probes run afterward and patch only the
  affected chip (`data-server-name` selector) once resolved — no full
  bar re-render. Unreachable chips render dimmed (`opacity:0.45`) with a
  `⚠` marker and a title tooltip, but remain fully clickable (status may
  be stale; clicking still surfaces the real, authoritative error per
  Phase 1/2). Per the operator's explicit requirement, down hosts are
  dimmed rather than removed from the list — removing them would make a
  configured-but-temporarily-down host indistinguishable from a deleted
  one.

Status: **Done**, shipped this round.

## Explicitly NOT done this round

- **Settings tab** (the original directive's literal subject): the
  server picker was not added to `renderSettingsView()`, no sub-card was
  made federation-aware, and the raw-`fetch()`-based loaders
  (`loadCommsConfig`, `loadGeneralConfig`, etc. — as opposed to
  `loadLLMTabConfig`, which already uses `apiFetch`) were not migrated.
  This is a large, separate pass across ~30+ sub-cards and needs its own
  session. **Needs explicit operator sign-off on priority**: whether this
  still happens next, or whether the federation-error-visibility pattern
  (this round) was the higher-value half of the original ask.
- Observer's remaining 11 sub-cards beyond the primary stats card (listed
  in Phase 2).
- Dashboard's periodic re-poll failures (only the initial load is
  instrumented).

## Parity surface

- **REST**: no new endpoints. Consumes existing `fedCap()` 403 bodies and
  `proxy.go`/`api.go` 502 bodies that already existed server-side —
  this round is purely about the PWA reading and displaying text the
  server was already sending.
- **MCP**: unaffected — no MCP tool wraps any of the touched client-side
  fetch paths.
- **CLI**: unaffected — this is PWA-only (`app.js`), no CLI consumer of
  these views.
- **comm channel**: unaffected — no comm-channel surface touches
  Sessions/Alerts/Automata/Dashboard/Observer's federated fetch paths.
- **YAML/config**: no new config keys.
- **PWA**: this round's entire surface — Sessions, Alerts, Automata,
  Dashboard (partial), Observer (partial), server picker chips.
- **Android / iPhone**: excluded — this is PWA-specific client code; no
  mobile-client federation UI exists for these views to parity against
  yet (tracked separately, not part of this round's directive).

## Verification

- `node --test internal/server/web/app-fed-cap-errors.test.js` — new
  file, 15 tests covering `_fedFetchError` (401/403/502/empty-body
  fallback), `apiFetch` auto-classification (proxied vs. local, URL
  regression guard), `loadAutomataPanel` no-longer-swallowing,
  `loadStatsPanel` (federated error surfaced / local error stays
  generic), `renderDashboardView` banner, `_probePickerReachability`
  (marks-unreachable, no-re-probe), `_serverPickerBar`
  (dimmed-when-unreachable, normal-when-unprobed). All passing.
- `node --test internal/server/web/*.test.js` — full suite, 159 tests,
  0 failures after fixing one pre-existing test's fixture
  (`app-fed-conn-status.test.js`'s 502 case needed a `.text()` method on
  its mock response, and its assertion updated from a bare `/502/` match
  to the real dial-failure text — a direct, expected consequence of
  Phase 1 making 502 handling more specific everywhere, including
  Sessions' own probe).
- No Go changes — `go build`/`go test ./...` not expected to be affected;
  not re-run as a formality beyond the standing discipline check.

## Status

Phases 1–3: **Done**, shipped in this commit. Settings tab and the
remaining Observer/Dashboard sub-cards: **deferred**, flagged above for
operator sign-off on priority.
