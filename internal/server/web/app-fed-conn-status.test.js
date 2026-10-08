// Operator-reported (2026-10-07): connecting to a federated server in the
// PWA showed no host-picker, no spinner, and never surfaced sessions or an
// error -- stuck indefinitely with zero feedback. Two separate bugs:
//
// 1. state.servers defaulted to [] instead of undefined, so
//    _injectServerPickerBar's "haven't fetched yet" check (`state.servers
//    === undefined`) was always false -- the picker's own fetch never ran
//    at all unless some other view (Settings) happened to populate
//    state.servers as a side effect first.
// 2. Even when the fetch DID run, a single failure permanently set
//    state.servers = null with no retry, and a federated WS connection
//    failure had no visible status/error -- just silent, endless loading.
//
// This pins: the corrected default, that a loading state is now rendered
// (not nothing) while servers load, that a failed load retries instead of
// giving up forever, and that selecting a federated server drives a real,
// event-based connecting/connected/error status rather than a canned one.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.loadServerListEager, 'function', 'loadServerListEager is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._injectServerPickerBar, 'function', '_injectServerPickerBar is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._checkFederatedConnection, 'function', '_checkFederatedConnection is not defined -- did it get renamed?');
  return sandbox;
}

test('state.servers defaults to undefined, not [] -- the actual root cause of the picker never loading', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('state.servers', sandbox), undefined);
});

test('_injectServerPickerBar kicks off loadServerListEager (not nothing) while state.servers is undefined', () => {
  // The stub DOM's innerHTML setter doesn't parse into real child nodes
  // (no real layout engine), so the tmp.firstChild / insertBefore path
  // this function uses for DOM insertion isn't directly observable here
  // -- that's a harness limitation shared with the pre-existing
  // _serverPickerBar()/_injectServerPickerBar() code, not something new.
  // What IS observable and is the actual regression this pins: the loader
  // actually starts (state._serversLoading flips true) instead of the old
  // behavior of silently doing nothing because the undefined-check never
  // matched.
  const sandbox = loadAppJS();
  const container = makeStubElement();
  vm.runInContext('fetch = () => new Promise(() => {})', sandbox); // never resolves -- isolate the "still loading" state
  sandbox.__container = container;
  vm.runInContext('_injectServerPickerBar(__container, null)', sandbox);
  assert.equal(vm.runInContext('state._serversLoading', sandbox), true, 'the eager loader should have started');
});

test('loadServerListEager populates state.servers from a successful fetch', async () => {
  const sandbox = loadAppJS();
  const fakeServers = { servers: [{ name: 'host-a', url: 'https://host-a:8443', enabled: true }] };
  vm.runInContext(
    `fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve(${JSON.stringify(fakeServers)}) })`,
    sandbox
  );
  vm.runInContext('loadServerListEager()', sandbox);
  await flushAsync();
  const result = vm.runInContext('JSON.stringify(state.servers)', sandbox);
  assert.deepEqual(JSON.parse(result), fakeServers);
});

test('loadServerListEager does NOT poison state.servers to null on failure (retries instead of giving up forever)', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: false, status: 500 })`, sandbox);
  vm.runInContext('loadServerListEager()', sandbox);
  await flushAsync();
  // Must still be undefined ("haven't succeeded yet"), never null
  // ("permanently gave up") -- null was the old behavior that hid the
  // picker for the rest of the session after one transient failure.
  assert.equal(vm.runInContext('state.servers', sandbox), undefined);
});

test('_checkFederatedConnection sets phase=connecting immediately, then fetches real sessions and clears the status entirely on success', async () => {
  // Live-tested against a real federated peer (2026-10-08): the first
  // version of this probed /api/health, which is deliberately
  // unauthenticated -- it reported "connected" even when the peer had no
  // token configured and every real data call was silently 401ing
  // forever. Now probes the real sessions endpoint itself, which both
  // validates actual auth and supplies the data directly.
  const sandbox = loadAppJS();
  const fakeSessions = [{ id: 's1', name: 'remote session' }];
  vm.runInContext(`fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(${JSON.stringify(fakeSessions)}) })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'; onSessionsUpdated = function(){};`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  const immediate = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(immediate.phase, 'connecting', 'status must flip to connecting synchronously, before the probe resolves');
  assert.equal(immediate.server, 'host-a');
  await flushAsync();
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null, 'status clears entirely on success -- no intermediate "connected" phase left waiting on the WS');
  const sessions = JSON.parse(vm.runInContext('JSON.stringify(state.sessions)', sandbox));
  assert.deepEqual(sessions, fakeSessions, 'the probe response itself should populate state.sessions directly, not wait on a WS push');
});

test('_checkFederatedConnection passes through a phase=loading_sessions step between the response arriving and the list being ready (GH#235 parity with the apps)', async () => {
  // The apps show "Connecting…" while the authenticated check is in
  // flight, then "Loading sessions from X…" while that response is turned
  // into the list. This was a single opaque "connecting" the whole time on
  // the PWA; split it at the same point -- once the response itself
  // resolves (ok, status known) but before r.json() has finished.
  const sandbox = loadAppJS();
  const fakeSessions = [{ id: 's1', name: 'remote session' }];
  let resolveJson;
  const jsonPromise = new Promise((resolve) => { resolveJson = resolve; });
  sandbox.__jsonPromise = jsonPromise; // sandbox IS the vm context's global object -- this makes __jsonPromise visible to code run via runInContext below
  vm.runInContext(`fetch = () => Promise.resolve({ ok: true, status: 200, json: () => __jsonPromise })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'; onSessionsUpdated = function(){};`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync(1);
  const mid = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(mid.phase, 'loading_sessions', 'should have moved past connecting once the response resolved, before the list is ready');
  assert.equal(mid.server, 'host-a');
  resolveJson(fakeSessions);
  await flushAsync();
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null, 'status clears once the list actually lands');
  const sessions = JSON.parse(vm.runInContext('JSON.stringify(state.sessions)', sandbox));
  assert.deepEqual(sessions, fakeSessions);
});

test('_checkFederatedConnection sets phase=error with a specific auth message on 401/403 (not a generic "HTTP 401")', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: false, status: 401 })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync();
  const status = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(status.phase, 'error');
  assert.match(status.message, /auth/i, 'a 401/403 should surface an actionable "authentication failed" message, not a bare status code -- this is the actual bug found live: a missing token in servers.json looked identical to a dead host');
});

test('_checkFederatedConnection sets phase=error with the real dial-failure text for a non-auth failure, not a bare status code', async () => {
  // Was a bare "HTTP 502" -- now routes through _fedFetchError like every
  // other apiFetch-based caller, surfacing the real proxy dial error.
  const sandbox = loadAppJS();
  vm.runInContext(
    `fetch = () => Promise.resolve({ ok: false, status: 502, text: () => Promise.resolve('proxy error: dial tcp: connect: connection refused') })`,
    sandbox
  );
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync();
  const status = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(status.phase, 'error');
  assert.match(status.message, /connection refused/);
});

test('_checkFederatedConnection ignores a stale probe result after the operator already switched to a different server', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve([]) })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'; onSessionsUpdated = function(){};`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  // Operator switches away before the probe resolves.
  vm.runInContext(`state.activeServer = 'host-b'; state._fedConnStatus = null;`, sandbox);
  await flushAsync();
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null, 'a stale host-a probe result must not overwrite the newer host-b state');
});

test('real session data arriving clears any in-progress federated connection status', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state._fedConnStatus = { server: 'host-a', phase: 'connecting' }`, sandbox);
  vm.runInContext(`handleMessage({ type: 'sessions', data: { sessions: [] } })`, sandbox);
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null);
});

test('in "All servers" mode, a plain local sessions WS push does NOT clobber the aggregated list with local-only data', () => {
  // Operator-reported (2026-10-08): switching to "All" showed every
  // server's sessions briefly, then quickly filtered down to local only.
  // "All" mode stays on the local WS (the aggregated list comes from a
  // separate HTTP fetch, _loadAllServersSessions) -- that same local WS
  // still broadcasts its own ordinary local-only 'sessions' snapshot,
  // which used to unconditionally overwrite state.sessions and silently
  // drop every remote entry the aggregated fetch had just added.
  const sandbox = loadAppJS();
  const aggregated = [
    { id: 'local-1', server: 'local' },
    { id: 'remote-1', server: 'host-a' },
  ];
  vm.runInContext(`state.activeServer = 'all'; state.sessions = ${JSON.stringify(aggregated)};`, sandbox);
  let reloaded = false;
  sandbox._loadAllServersSessions = () => { reloaded = true; };
  // Simulate the local WS's own ordinary (non-aggregated) push.
  vm.runInContext(`handleMessage({ type: 'sessions', data: { sessions: [{ id: 'local-1', server: 'local' }] } })`, sandbox);
  const sessions = JSON.parse(vm.runInContext('JSON.stringify(state.sessions)', sandbox));
  assert.deepEqual(sessions, aggregated, 'state.sessions must be untouched by the raw local push while in All mode -- the remote entry must survive');
  assert.equal(reloaded, true, 'should re-trigger the real aggregated fetch instead, to pick up the local change without losing remote entries');
});

test('outside "All" mode, a local sessions WS push still updates state.sessions normally (regression guard)', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state.activeServer = null;`, sandbox);
  vm.runInContext(`handleMessage({ type: 'sessions', data: { sessions: [{ id: 'local-1' }] } })`, sandbox);
  const sessions = JSON.parse(vm.runInContext('JSON.stringify(state.sessions)', sandbox));
  assert.deepEqual(sessions, [{ id: 'local-1' }]);
});

test('selectServer() kicks off a federated connection check when switching to a named remote server', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => new Promise(() => {})`, sandbox);
  // Isolate selectServer's own branching from connect()'s real WS
  // handshake logic (covered elsewhere) -- the stub WebSocket doesn't
  // implement addEventListener.
  vm.runInContext(`connect = function(){}; loadServers = function(){};`, sandbox);
  vm.runInContext(`let __checkedServer = null; _checkFederatedConnection = (name) => { __checkedServer = name; };`, sandbox);
  vm.runInContext('selectServer("host-a")', sandbox);
  assert.equal(vm.runInContext('__checkedServer', sandbox), 'host-a');
});

test('renderAlertsView() proxies through /api/proxy/<name>/... for a specific named remote server (not just "all")', () => {
  // Operator-reported (2026-10-08): the Alerts tab ignored the selected
  // federated host entirely -- only 'all' mode was special-cased
  // (aggregated endpoint); any specific named remote fell straight
  // through to the LOCAL /api/alerts and /api/sessions, so the picker
  // had no effect on this view at all.
  const sandbox = loadAppJS();
  const fetchedUrls = [];
  sandbox.fetch = (url) => {
    fetchedUrls.push(url);
    return Promise.resolve({ ok: false, status: 404 });
  };
  sandbox.document.getElementById = () => makeStubElement();
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  vm.runInContext('renderAlertsView()', sandbox);
  const alertsUrl = fetchedUrls.find(u => u.includes('/alerts') && !u.includes('/commands'));
  const sessionsUrl = fetchedUrls.find(u => u.includes('/sessions'));
  assert.equal(alertsUrl, '/api/proxy/host-a/api/alerts', `alerts fetch should proxy through host-a, got: ${alertsUrl}`);
  assert.equal(sessionsUrl, '/api/proxy/host-a/api/sessions', `sessions fetch should proxy through host-a, got: ${sessionsUrl}`);
});

test('renderAlertsView() stays on the aggregated endpoint for "all" mode and does not clobber state.sessions', () => {
  const sandbox = loadAppJS();
  const fetchedUrls = [];
  sandbox.fetch = (url) => {
    fetchedUrls.push(url);
    return Promise.resolve({ ok: false, status: 404 });
  };
  sandbox.document.getElementById = () => makeStubElement();
  vm.runInContext(`state.activeServer = 'all'; state.sessions = [{id:'keep-me'}];`, sandbox);
  vm.runInContext('renderAlertsView()', sandbox);
  const alertsUrl = fetchedUrls.find(u => u.includes('/alerts') && !u.includes('/commands'));
  assert.equal(alertsUrl, '/api/alerts/aggregated');
  assert.ok(!fetchedUrls.some(u => u === '/api/sessions'), 'must not fetch the LOCAL /api/sessions while in All mode (that is the exact clobbering bug already fixed in handleMessage)');
});

test('renderAlertsView() uses plain local endpoints when no federated server is selected (regression guard)', () => {
  const sandbox = loadAppJS();
  const fetchedUrls = [];
  sandbox.fetch = (url) => {
    fetchedUrls.push(url);
    return Promise.resolve({ ok: false, status: 404 });
  };
  sandbox.document.getElementById = () => makeStubElement();
  vm.runInContext(`state.activeServer = null;`, sandbox);
  vm.runInContext('renderAlertsView()', sandbox);
  const alertsUrl = fetchedUrls.find(u => u.includes('/alerts') && !u.includes('/commands'));
  const sessionsUrl = fetchedUrls.find(u => u.includes('/sessions'));
  assert.equal(alertsUrl, '/api/alerts');
  assert.equal(sessionsUrl, '/api/sessions');
});

test('reconnect-driven view refresh now includes "alerts" (switching servers mid-tab used to do nothing until navigating away and back)', () => {
  const src = require('fs').readFileSync(require('path').join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /state\.activeView === 'alerts'\) \{[\s\S]{0,400}renderAlertsView\(\);/, 'connect()\'s reconnect-refresh switch must include an alerts branch calling renderAlertsView()');
});

// Operator-reported (2026-10-08): "can't select different server on
// automata page" -- NOT a proxying bug (apiFetch already proxies
// correctly for a specific remote); the reconnect-driven view-refresh
// list (same mechanism just fixed for alerts) never included
// 'autonomous' either, so clicking a different server chip updated
// state.activeServer and reconnected the WS, but nothing ever
// re-fetched the PRD list until navigating away and back. Dashboard and
// Observer had the identical gap, found proactively and fixed in the
// same pass -- these three pin all of them so the pattern can't regress
// silently view-by-view again.
for (const [view, renderFn] of [['autonomous', 'renderAutonomousView'], ['dashboard', 'renderDashboardView'], ['observer', 'renderObserverView']]) {
  test(`reconnect-driven view refresh now includes "${view}" (same gap as alerts, same fix)`, () => {
    const src = require('fs').readFileSync(require('path').join(__dirname, 'app.js'), 'utf8');
    const re = new RegExp(`state\\.activeView === '${view}'\\) \\{[\\s\\S]{0,400}${renderFn}\\(\\);`);
    assert.match(src, re, `connect()'s reconnect-refresh switch must include a '${view}' branch calling ${renderFn}()`);
  });
}

test('selectServer() clears federated status when switching back to Local', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => new Promise(() => {})`, sandbox);
  vm.runInContext(`connect = function(){}; loadServers = function(){};`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'; state._fedConnStatus = { server: 'host-a', phase: 'connecting' };`, sandbox);
  vm.runInContext('selectServer(null)', sandbox);
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null);
});
