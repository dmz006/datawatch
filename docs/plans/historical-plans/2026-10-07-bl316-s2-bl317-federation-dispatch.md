# BL316 S2 — RemoteDispatcher live-store staleness; BL317 — Dashboard aggregation

- **Date**: 2026-10-07 (backfilled retroactively 2026-10-07, after a
  48-hour compliance audit flagged these two shipped changes as missing
  the dated plan doc AGENT.md's Planning Rule calls for on 3+ file /
  architectural changes — the work itself was already live-verified,
  tested, and shipped before this doc existed; this doc documents what
  was built and why, for the paper trail, not as a pre-implementation
  design pass)
- **Version at planning**: v8.66.3 (start of this pass) → v8.69.2 (end)
- **Status**: Complete. Both shipped: `e48c6ccc` (v8.67.0, BL316 S2),
  `64bee11b` (v8.69.0, BL317 Dashboard aggregation). Two adjacent
  follow-on commits in the same pass (`47a6103b` per-row attribution,
  `cdd135b4` server-picker onclick fix, `88e45acf` TS-387–396 / MCP-cap
  fix) are smaller, non-architectural, and already individually
  documented in their own commit messages + `docs/testing-tracker.md`
  entries — not re-covered here.

## Context

Operator instruction: "proceed with bl316 s2, bl317 and bl318 ... make
sure parity rules and documentation rules are all followed especially
testing rules." A prior audit fork (same day) had already established,
with file:line citations, that:
- `RemoteDispatcher` (`internal/proxy/remote.go`) held `d.servers` as a
  frozen `cfg.Servers` snapshot captured once at daemon construction,
  never refreshed from the live `multiserver.Store` — so any federation
  peer added at runtime (`federation peer add`, the PWA panel, or
  `POST /api/servers`) was invisible to cross-host comm-channel dispatch
  (`FindSession`/`ForwardCommand`) and to the CLI's `--server` flag until
  the next daemon restart. `internal/server/proxy.go`'s `findServer()`
  already merged YAML-seeded + runtime-added entries correctly — this
  dispatcher was the one path that didn't.
- BL317's server-picker UI (`_serverPickerBar`, five call sites) existed
  on Dashboard and Observer, but neither had a backing aggregation
  endpoint behind their "All" mode — the picker was cosmetic there,
  silently continuing to show local-only data while implying otherwise.

## Decisions made before coding (not guessed)

- **BL316 S2 — CLI fix approach.** Two options existed: (a) add a new,
  unredacted server-credential-resolve REST endpoint so the short-lived
  CLI process could fetch a dynamically-added peer's real token, or (b)
  route `--server <name>` through the local daemon's *existing*
  `/api/proxy/<name>/...` passthrough, which already merges YAML + the
  live store and injects the stored token server-side, never handing the
  remote token to the CLI process at all. Chose (b): no new credential-
  exposure surface, reuses an already-audited path, and is strictly less
  code. (This was actually explored as option (a) first in this same
  session, via operator `AskUserQuestion`, then superseded mid-flight —
  see conversation history; (b) is what shipped.)
- **BL317 — Observer scope cut.** Per operator decision (confirmed via
  `AskUserQuestion` during this pass): Observer's "All" chip is *removed*
  rather than given real aggregation. Its ~9 independent cards plus its
  own pre-existing "observer peers" cross-node concept (unrelated to
  multi-server federation) would make full fan-out a much bigger redesign
  than Dashboard's — and conflating the two "peer" concepts was judged a
  worse outcome than a smaller, single-server-only picker.

## What shipped

### BL316 S2 (`internal/proxy/remote.go`, `cmd/datawatch/main.go`)
- `RemoteDispatcher.store atomic.Pointer[multiserver.Store]` +
  `SetStore()` + `effectiveServers()`: every dispatch method
  (`HasServers`, `refreshCache`, `ForwardCommand`, `ForwardHTTP`,
  `ListAllSessions`) now reads the live store on each call instead of the
  frozen `d.servers` snapshot, falling back to the snapshot only before
  the store exists (startup window, or direct-construction tests).
- Dispatcher construction is no longer gated on `len(cfg.Servers) > 0` —
  a daemon that starts with zero YAML-seeded servers still gets a
  (initially-empty) dispatcher, so a peer added purely at runtime isn't
  permanently unreachable.
- `main.go`: `remoteDispatcher.SetStore(msStore)` wired in immediately
  after `msStore` construction (same scope as `httpServer.SetServerStore`
  and `server.StartPeerHealthMonitor`).
- CLI: `daemonAPIURL`/`daemonHTTPClient` now route `--server <name>`
  through `http://localhost:<port>/api/proxy/<name>/api/command` on the
  *local* daemon rather than resolving the remote's own URL/token from
  this process's freshly-loaded `cfg.Servers`.
- Tests: `internal/proxy/remote_test.go` (+3: store-set vs. unset
  fallback, dynamic-peer visibility after `SetStore`, concurrent-access
  safety), `cmd/datawatch/daemon_api_url_test.go` (+4: local-proxy
  routing, unknown-server fallback, `-u`/`--url` mode untouched).

### BL317 Dashboard aggregation (`internal/server/bl312_aggregated.go`,
`internal/server/web/app.js`)
- New `GET /api/cost/aggregated` (`internal/server/bl312_aggregated.go`),
  mirroring the existing Alerts/PRDs aggregated-endpoint shape: fan out
  to every enabled server, tag each result with `.server`, skip
  unreachable peers rather than failing the whole response.
- Dashboard's "All" mode now calls this plus the existing PRDs aggregated
  endpoint (previously fetched but discarded in "All" mode).
  `_dashFetchPRDs()`/`_dashFetchCost()` extracted as shared helpers —
  `renderDashboardView`'s initial load and `_dashLoop`'s periodic
  re-fetch had independently duplicated this branching, which is how a
  pre-existing `total_cost_usd` vs. `total_usd` field-name bug (showing
  $0 in every mode, not just "All") had survived undetected in one of the
  two copies.
- Observer: `_injectServerPickerBar(view, renderObserverView, {hideAll:
  true})` — picker stays, "All" chip suppressed.
- Tests: `internal/server/bl317_aggregated_cost_test.go` (+99 lines),
  `internal/server/web/app-dashboard-aggregation.test.js` (+133 lines,
  new file).

## Parity surface

- **REST**: new `GET /api/cost/aggregated` (BL317). No new REST surface
  for BL316 S2 — `/api/proxy/<name>/...` already existed.
- **MCP**: unaffected by either change — no MCP tool wraps the dispatcher
  or the new cost-aggregation endpoint directly.
- **CLI**: BL316 S2 *is* the CLI fix (`--server` flag). BL317's new
  endpoint has no CLI consumer (Dashboard aggregation is PWA-only data).
- **comm channel**: BL316 S2 *is* the comm-channel fix
  (`FindSession`/`ForwardCommand`, the cross-host `send_input` path).
  BL317's cost aggregation has no comm-channel consumer.
- **YAML/config**: no new config keys in either change.
- **PWA**: BL317 is PWA-only on the Dashboard/Observer side. BL316 S2 has
  no PWA surface (server-add/peer-add UI already existed and already
  worked — that path isn't what was stale).
- **Android / iPhone**: excluded — neither change has a mobile-client
  surface; both are server-side dispatch correctness (BL316 S2) or
  PWA-only data wiring (BL317 Dashboard).

## Verification

- `go test ./...` full suite green (3161→3165 tests across the pass,
  83 packages) after each commit; `-race -count=3` targeted at
  `internal/proxy` for BL316 S2.
- `node --test internal/server/web/*.test.js` full suite green after the
  BL317 app.js changes.
- `make build` (not raw `go build`) before every commit, per the
  docs-sync/docs-index dependency.
- gosec clean on touched files; 2 pre-existing G104 findings in
  `remote.go` confirmed via `git stash` to reproduce identically on
  pre-change code, not introduced by this pass.
- Daemon restarted on the new binary after each commit; confirmed via
  `systemctl --user status` + binary-mtime comparison at the end of the
  pass.
- `docs/plans/README.md` BL316/BL317/BL318 entries and
  `docs/testing-tracker.md` updated in the same commits as the code.
