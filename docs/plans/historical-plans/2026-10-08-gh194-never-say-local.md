# GH#194 — never label the connected server "local"

**Date:** 2026-10-08
**Version at planning time:** v8.73.8

## Scope

Operator decision via the datawatch-app session, part of final v9.0.0
debugging across PWA and the mobile apps (Android/iOS parity work
#234–#236 makes the identical change on their side). First of three
queued items (this one; Dashboard/Observer denial-coverage extension;
full Settings-tab federation), run in the operator's suggested order
(smallest/most contained first).

Files:
- `internal/server/api.go` (`handleListServers`)
- `internal/server/health_test.go` (new test)
- `internal/server/web/app.js`
- `internal/server/web/locales/{en,de,es,fr,ja}.json`
- `internal/server/web/app-server-identity-label.test.js` (new)

## Problem

Two related issues reported against the current PWA:

1. **Observer duplicate card.** `/api/observer/peers` has synthesized a
   "self" entry (`is_self: true`) for the local instance since v7.0.0
   (`synthesizeSelfPeer`, #184) so the federated-peers table shows every
   host in one place. Observer's System Statistics grid independently
   builds a card for the local machine from `/api/stats`, *and* iterates
   `/api/observer/peers` for peer cards without excluding that synthetic
   self entry — so the exact same physical machine rendered twice: once
   tagged "local" (from `/api/stats`), once untagged (from the peers
   list, using whatever name it registered itself under, which can equal
   the local hostname).
2. **The word "Local" everywhere.** The picker chip, "Back to Local"
   button, a Sessions-view tooltip, the connection toast, and the
   Settings server list all displayed the literal word "Local"/"local"
   for the current server instead of its real name.

## Investigation corrected the directive's premise

The original ask assumed `/api/stats` had no hostname field. It
already does — `internal/stats/collector.go`'s `Hostname` field, wired
via `SetServerIdentity` in `cmd/datawatch/main.go` (GH#192/D78a). So did
`/api/health` (`s.hostname`, pre-existing). The real gaps were: (a) the
client-side Observer dedup never excluded the self-peer entry, and (b)
every *other* "local" surface (picker, buttons, toasts, Settings list)
had no hostname source to read at all — `/api/servers`' synthesized
local entry carried only the literal name `"local"`, never a real
hostname.

## Design: keep "local" as a routing key, add a display name

`sv.name === 'local'` (and `state.activeServer === null`/`'local'`) is
load-bearing throughout the client — `apiFetch`'s `isRemote` check,
`selectServer`, dozens of `!== 'local'` comparisons. Renaming that
identity key was out of scope and high-blast-radius for what is
fundamentally a display-text fix. Instead:

- **Server-side**: `handleListServers`' local `serverInfo` entry gained
  a new `Hostname` field (`s.hostname`), alongside the unchanged `Name:
  "local"` sentinel. `TestListServers_LocalEntryCarriesRealHostname`
  pins it.
- **Client-side**: a new `_localHostname` cache + `_ensureLocalHostname()`
  lazily fetches `/api/health`'s `hostname` field once per page session
  (same lazy, non-blocking, patch-in-place pattern as the existing
  picker-reachability probe from the prior federation-error-visibility
  round) and is read everywhere the word "Local" used to appear.

## Changes

1. **Picker chip** (`_serverPickerBar`) — local chip label is
   `_localHostname` once known, falling back to a generic, never-"Local"
   placeholder (`server_local_label`, repurposed from "Local" to "This
   server") while the fetch is in flight. `_injectServerPickerBar` kicks
   off `_ensureLocalHostname()` right after the bar renders (never
   blocks it). The chip also gets a tooltip naming the real hostname.
2. **Swipe-gesture modal picker** (`_loadServerPickerModalList`) — a
   second, independent chip-list implementation with the identical bug;
   fixed the same way. Its markup carries an active-state "●/○" prefix
   `_updatePickerChipLocalLabel`'s blind text-patch would have clobbered,
   so this surface re-renders via a callback (`_ensureLocalHostname(render)`)
   instead of a text patch.
3. **"Back to Local" button** → `fed_conn_back_to_local` parameterized
   to "Back to %1$s".
4. **Sessions server-indicator tooltip** ("Click to return to local",
   previously hardcoded English with no `t()` call at all) → new key
   `server_indicator_return_tip`, parameterized.
5. **Connection toast** → `toast_connected_local` parameterized to
   "Connected to %1$s".
6. **Settings server list** (`loadServers()`) — the local row's
   displayed name is `sv.hostname || sv.name` instead of the literal
   `sv.name` (which is always `"local"` for that row); the `selectServer`
   call still passes `sv.name` unchanged (routing untouched).
7. **Observer System Statistics grid** (`loadSystemStatsGrid`) —
   `peersP` filters out `is_self` entries before building cards (fixes
   the duplicate); `sysCard`'s `isLocal`-driven "local" badge/tag removed
   entirely (every card is titled by its real name now, local or not).
8. A Dashboard mini resource-table label (`localStatsFetch`'s `ref`
   field) had the same `t('server_local_label')||'Local'` fallback
   pattern — updated to prefer the real hostname too, for consistency,
   though its locale-driven value alone would already have fixed the
   visible text.

## Explicitly out of scope

- The Compute Node Add/Edit form's free-observer-peer dropdown hint
  (`" (local datawatch host)"`, `internal/server/web/app.js` ~line
  10284-10290) uses "local" in a different, lower-case descriptive-hint
  sense, not as the server's displayed name/identity. Not one of the
  operator's five enumerated surfaces; left alone, flagged here in case
  a future pass wants full sweep consistency.
- `selectServer('local')` (triggered by the Settings list's own
  "Select" button on the local row, since it passes the literal
  `sv.name`) sets `state.activeServer = 'local'` rather than `null` —
  a pre-existing routing quirk, functionally harmless (every `!==
  'local'` check already treats it as non-remote) but not what
  `selectServer(null)` (the picker chip's own path) does. Not part of
  this display-text fix; noted for awareness only.

## Mobile parity

Could not cross-check exact wording against the Android/iOS
implementations (#234–#236) from this repo — no access to those
codebases in this environment. Chose generic, neutral phrasing
("This server", "Back to %1$s", "Connected to %1$s"). Flagged for the
operator/a follow-up pass to verify once those PRs land, per the
Mobile-Parity Rule.

## Parity surface

- **REST**: `GET /api/servers`' local entry gained a `hostname` field
  (additive, non-breaking — old clients ignore unknown fields).
- **MCP**: unaffected — no MCP tool wraps `handleListServers` or any of
  the touched client-side rendering.
- **CLI**: unaffected — PWA-only display fix.
- **comm channel**: unaffected.
- **YAML/config**: no new config keys — `s.hostname` already existed
  (`cfg.Hostname`, auto-populated via `os.Hostname()` if unset).
- **PWA**: this round's entire surface.
- **Android / iPhone**: the identical change is being made in parallel
  by the mobile apps (#234–#236) — see Mobile parity note above.

## Verification

- `go test ./internal/server/...` — new
  `TestListServers_LocalEntryCarriesRealHostname`, full package green.
- `go test ./...` — full suite green, no regressions.
- `node --test internal/server/web/app-server-identity-label.test.js` —
  6 new tests (hostname caching/dedup-fetch, picker chip real-name vs.
  never-"Local" fallback, Observer duplicate-card fix, Settings list
  display name). All passing.
- `node --test internal/server/web/*.test.js` — full suite, 167/167
  green.
- `go build` clean.

## Status

**Done**, shipped in this commit. Items 2 (Dashboard/Observer denial
coverage extension) and 3 (full Settings-tab federation) are next per
the operator's queue — see
`docs/plans/historical-plans/2026-10-08-pwa-federated-error-visibility.md` for item 2's
starting point (the two judgment calls flagged there).
