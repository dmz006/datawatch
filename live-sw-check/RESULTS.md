# Live verification — SW activate no-op guard (`internal/server/web/sw.js`)

Date: 2026-10-20 (evening). Host: johnnyjohnny. Chrome (headless, new): `Chrome/155.0.8059.39` at
`/usr/bin/google-chrome`, driven over raw CDP via Node 24 native WebSocket (puppeteer-core at
`/tmp/puppet` was missing — confirmed `PUPPET_MISSING` before this; the CDP script below uses no
external deps). Daemon: built in this worktree (`make docs-index && go build -o datawatch
./cmd/datawatch/`), sandbox config in `live-sw-check/`, port **18491**, v9.5.31.

## Method

`sw-live.mjs` spawns Chrome with `--headless=new --remote-debugging-port=18499 --user-data-dir=profile`
and drives Phase A/B/C against `http://127.0.0.1:18491/`:

- Injects (via CDP `Page.addScriptToEvaluateOnNewDocument`) a `sessionStorage`-appending hook on
  every page navigation API (`history.replaceState`, `location.assign/href`, `location.reload`).
- Collects CDP events: `Navigation.requestWillBeSent` (document), `Page.frameNavigated`,
  `Page.loadEventFired`, `ServiceWorker.workerVersionUpdated`.
- After load + a settle window, reads `performance.getEntriesByType('navigation')`,
  `caches.keys()`, `navigator.serviceWorker.controller`, and the hook log in-page.
- Kills Chrome on exit.

`document requests: 0` in every phase is expected: once an SW is registered, Chrome does not emit
page-target `Navigation.requestWillBeSent` for the SW-served document; `Page.loadEventFired` +
`frameNavigated` are the correct load counters (both are in the traces below).

## Phase A — fresh profile (no SW, empty cache) — daemon at baseline CACHE_NAME

```
[sw-live] settled: {"readyState":"complete","sw":{"active":true,"state":"activated","script":"http://127.0.0.1:18491/sw.js"}}
document requests: 0
Page.loadEventFired: 2
  +9ms  frameNavigated  http://127.0.0.1:18491/  loader 7831501A...  (initial)
  +106  load #1
  +201  frameNavigated  http://127.0.0.1:18491/  loader 437C810E...  (SW takeover reload)
  +222  load #2
navigation entries: [{"type":"reload"}], reloadCount: 1
caches: ["datawatch-v7-0-0-alpha-51"], controller: http://127.0.0.1:18491/sw.js
page-script navigations: none          (hook never hit — not a `location.reload()` in the app)
```

Interpretation: first-load SW install/activate + `client.claim()` while the initial document fetch
was in flight — Chrome re-navigates the taken-over document through the new SW (known browser
behaviour, present independently of the guard). The guard's activate ran with no stale key
(`caches` at activate time = [current] only) → `shouldNavigateOnActivate` = false; the reload is the
one-time first-load takeover, not a guard-driven `win.navigate`.

## Phase B — same profile (SW installed, current cache present), steady state

```
Page.loadEventFired: 1
  +17  frameNavigated  http://127.0.0.1:18491/  loader 88AEC99D...  (single load)
  +65  load #1
navigation entries: [{"type":"navigate"}], reloadCount: 0
caches: ["datawatch-v7-0-0-alpha-51"], controller: http://127.0.0.1:18491/sw.js
page-script navigations: none
EXTRA document loads: 0 (no reload)   → PASS B
```

Under an already-activated, current-version SW (the restart/no-op scenario), the page loads exactly
once. **No spurious service-worker-driven reload** — the operator-visible "double load on every
daemon restart" symptom is absent.

## Phase C — genuine upgrade: CACHE_NAME bumped to `datawatch-v7-0-0-alpha-51-livecheck`, daemon rebuilt + restarted, same profile (stale cache present)

```
Page.loadEventFired: 2
  +18   frameNavigated  http://127.0.0.1:18491/  loader EF38101C... (initial, old SW still controlling)
  +65   load #1
  +2060..+2133ms  SW update burst (workerVersionUpdated events) — new SW install+activate
  +2133 frameNavigated  http://127.0.0.1:18491/  loader 6B394BAC... (exactly one guard navigate)
  +2154 load #2
navigation entries: [{"type":"reload"}], reloadCount: 1
caches: ["datawatch-v7-0-0-alpha-51-livecheck"]     ← stale `...-alpha-51` purged by activate handler
controller: http://127.0.0.1:18491/sw.js            ← now the new script
page-script navigations: none                        ← reload is SW-initiated, not app JS
EXTRA document loads: 1                              → PASS C (exactly one intentional reload)
```

The guard's positive path verified live: real upgrade → `activate` fires → one `win.navigate` →
exactly one reload, stale cache deleted, new SW becomes controller. `sw.js` was then reverted to its
original `datawatch-v7-0-0-alpha-51` and the daemon binary rebuilt to match; the sandbox daemon
was stopped after the run (see daemon-a/b logs).