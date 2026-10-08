// Coordinator/operator-driven round (2026-10-08): extends the federation
// error handling from Sessions/Alerts (_fedConnStatus, _fedFetchError)
// across more views and adds a genuine third failure class (unreachable,
// distinct from auth/capability denial), plus background picker-chip
// reachability indication.
//
// _fedFetchError (was _fedCapError) now distinguishes THREE situations
// instead of two:
//   401 -- bad/missing token (can't authenticate at all)
//   403 -- valid token, missing capability (server names the exact one)
//   502 -- genuinely unreachable (handleProxy's own dial failed)
// apiFetch() now applies this automatically for any proxied call, so
// every apiFetch()-based view (Automata, Dashboard, Observer, future
// Settings work) gets real error messages without each call site
// reimplementing the 401/403/502 split.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._fedFetchError, 'function', '_fedFetchError is not defined -- did it get renamed?');
  assert.equal(typeof sandbox.apiFetch, 'function', 'apiFetch is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._probePickerReachability, 'function', '_probePickerReachability is not defined -- did it get renamed?');
  return sandbox;
}

function fakeResponse(status, body) {
  return { ok: status >= 200 && status < 300, status, text: () => Promise.resolve(body || ''), json: () => Promise.resolve(JSON.parse(body || '{}')) };
}

// ── _fedFetchError itself ───────────────────────────────────────────────

test('_fedFetchError: 401 gives the "authentication failed" message', async () => {
  const sandbox = loadAppJS();
  await assert.rejects(
    vm.runInContext('_fedFetchError(__r)', Object.assign(sandbox, { __r: fakeResponse(401) })),
    /auth/i
  );
});

test('_fedFetchError: 403 surfaces the server\'s real capability-denial text verbatim', async () => {
  const sandbox = loadAppJS();
  sandbox.__r = fakeResponse(403, 'federation peer lacks capability: sessions:list');
  const err = await vm.runInContext('_fedFetchError(__r)', sandbox).catch(e => e);
  // Not `instanceof Error` -- the Error was constructed inside the VM's
  // own realm, which is a different Error class than the host Node
  // process's (a real, if surprising, Node `vm` module gotcha -- same
  // object conceptually, fails a cross-realm instanceof check). Check
  // the actual properties instead.
  assert.equal(typeof err.message, 'string');
  assert.equal(err.message, 'federation peer lacks capability: sessions:list');
});

test('_fedFetchError: 502 surfaces the real dial-failure text verbatim, not a bare "HTTP 502"', async () => {
  const sandbox = loadAppJS();
  sandbox.__r = fakeResponse(502, 'proxy error: Get "https://ralfthewise:8443/api/health": dial tcp: connect: connection refused');
  const err = await vm.runInContext('_fedFetchError(__r)', sandbox).catch(e => e);
  assert.match(err.message, /connection refused/);
});

test('_fedFetchError: an empty 403/502 body falls back to a locale-driven generic message, not blank', async () => {
  const sandbox = loadAppJS();
  const err403 = await vm.runInContext('_fedFetchError(__r)', Object.assign(sandbox, { __r: fakeResponse(403, '') })).catch(e => e);
  assert.ok(err403.message && err403.message.length > 0);
  const err502 = await vm.runInContext('_fedFetchError(__r)', Object.assign(sandbox, { __r: fakeResponse(502, '   ') })).catch(e => e);
  assert.ok(err502.message && err502.message.length > 0);
});

// ── apiFetch auto-applying the classifier ───────────────────────────────

test('apiFetch: a 403 from a PROXIED call rejects with the real capability message', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = () => Promise.resolve(fakeResponse(403, 'federation peer lacks capability: config:read'));
  const err = await vm.runInContext(`apiFetch('/api/config')`, sandbox).catch(e => e);
  assert.equal(err.message, 'federation peer lacks capability: config:read');
});

test('apiFetch: a LOCAL (non-proxied) 403 keeps the original generic behavior, not the federation framing', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state.activeServer = null;`, sandbox);
  sandbox.fetch = () => Promise.resolve(fakeResponse(403, 'some local error text'));
  const err = await vm.runInContext(`apiFetch('/api/config')`, sandbox).catch(e => e);
  assert.equal(err.message, 'some local error text');
});

test('apiFetch: still proxies the URL exactly as before (regression guard -- this fix only touched error handling)', () => {
  const sandbox = loadAppJS();
  let calledUrl = null;
  sandbox.fetch = (url) => { calledUrl = url; return Promise.resolve(fakeResponse(200, '{}')); };
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  vm.runInContext(`apiFetch('/api/config')`, sandbox);
  assert.equal(calledUrl, '/api/proxy/host-a/api/config');
});

// ── loadAutomataPanel: no longer swallows the error before it's seen ────

test('loadAutomataPanel: a federated capability error reaches the panel, not an empty "0 automata" list', async () => {
  const sandbox = loadAppJS();
  const panelEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'automataPanel' ? panelEl : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/autonomous/prds')) return Promise.resolve(fakeResponse(403, 'federation peer lacks capability: autonomous:list'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadAutomataPanel()', sandbox);
  await flushAsync();
  assert.match(panelEl.innerHTML, /autonomous:list/, `panel should show the real capability error; got: ${panelEl.innerHTML}`);
});

// ── Observer: primary stats card surfaces the real federated error ─────

test('loadStatsPanel: a federated capability error reaches the stats card, not a generic "Stats unavailable"', async () => {
  const sandbox = loadAppJS();
  const statsEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'statsPanel' ? statsEl : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/stats')) return Promise.resolve(fakeResponse(403, 'federation peer lacks capability: observers:read'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadStatsPanel()', sandbox);
  await flushAsync();
  assert.match(statsEl.innerHTML, /observers:read/, `stats panel should show the real capability error; got: ${statsEl.innerHTML}`);
});

test('loadStatsPanel: a LOCAL stats failure keeps the generic "Stats unavailable" message (no federation framing)', async () => {
  const sandbox = loadAppJS();
  const statsEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'statsPanel' ? statsEl : makeStubElement());
  vm.runInContext(`state.activeServer = null;`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/stats')) return Promise.resolve(fakeResponse(500, 'internal error'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadStatsPanel()', sandbox);
  await flushAsync();
  assert.doesNotMatch(statsEl.innerHTML, /internal error/, 'a local failure should not leak raw server text through the federation path');
  assert.match(statsEl.innerHTML, /[Ss]tats/, `expected the generic unavailable message; got: ${statsEl.innerHTML}`);
});

// ── Dashboard: federated load failure gets a one-time visible banner ────

test('renderDashboardView: a federated PRDs-fetch failure on initial load shows a banner (not silent)', async () => {
  const sandbox = loadAppJS();
  // t() falls back to returning the bare key when no locale bundle is
  // loaded (buildSandbox() doesn't fetch /locales/*.json) -- in a real
  // browser initI18n() always loads a bundle before any view renders.
  // Populate just the one key this test exercises so t()'s real
  // %1$s/%2$s substitution logic actually runs, instead of silently
  // passing only because the untranslated key happens to contain a
  // matching substring.
  vm.runInContext(`window._i18n.bundle = { dash_fed_error: '%1$s: %2$s' }; window._i18n.fallback = window._i18n.bundle;`, sandbox);
  const gridEl = makeStubElement();
  gridEl.insertAdjacentHTML = function (pos, html) { this.innerHTML = html + this.innerHTML; };
  sandbox.document.getElementById = (id) => (id === 'dashCardGrid' ? gridEl : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a'; state.activeView = 'dashboard'; _automataState.allPrds = [];`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/autonomous/prds')) return Promise.resolve(fakeResponse(403, 'federation peer lacks capability: autonomous:list'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  // renderDashboardView does a lot (canvas, requestAnimationFrame loop,
  // layout fetch) -- isolate just the piece under test.
  sandbox.requestAnimationFrame = () => 0;
  sandbox._dashInitNodes = () => {};
  sandbox._dashLoadLayout = () => {};
  vm.runInContext('renderDashboardView()', sandbox);
  await flushAsync();
  assert.match(gridEl.innerHTML, /autonomous:list/, `dashboard grid should show the real capability error; got: ${gridEl.innerHTML}`);
});

// ── Picker reachability probing ──────────────────────────────────────────

test('_probePickerReachability: marks a server unreachable on a failed probe, without blocking (fire-and-forget)', async () => {
  const sandbox = loadAppJS();
  sandbox.fetch = (url) => url.includes('host-down') ? Promise.reject(new Error('network error')) : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext(`_probePickerReachability([{name:'host-down'},{name:'host-up'}])`, sandbox);
  // Call returns immediately -- must not have awaited anything synchronously.
  await flushAsync();
  assert.equal(vm.runInContext(`_serverReachability['host-down']`, sandbox), false);
  assert.equal(vm.runInContext(`_serverReachability['host-up']`, sandbox), true);
});

test('_probePickerReachability: does not re-probe a server already resolved this session', async () => {
  const sandbox = loadAppJS();
  let callCount = 0;
  sandbox.fetch = () => { callCount++; return Promise.resolve(fakeResponse(200, '{}')); };
  vm.runInContext(`_probePickerReachability([{name:'host-a'}])`, sandbox);
  await flushAsync();
  vm.runInContext(`_probePickerReachability([{name:'host-a'}])`, sandbox);
  await flushAsync();
  assert.equal(callCount, 1, 'a second probe call for the same already-resolved host should be a no-op');
});

test('_serverPickerBar: an unreachable server renders dimmed with a warning marker, but the onclick is unchanged (still fully clickable)', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`
    state.servers = { servers: [{ name: 'host-down', enabled: true }] };
    _serverReachability['host-down'] = false;
  `, sandbox);
  const html = vm.runInContext(`_serverPickerBar({})`, sandbox);
  assert.match(html, /opacity:0\.45/, 'unreachable chip should be visually dimmed');
  assert.match(html, /⚠/, 'unreachable chip should carry a warning marker');
  assert.match(html, /selectServer\(/, 'the chip must still be clickable -- the probe result can be stale, and clicking is how the real error surfaces');
});

test('_serverPickerBar: a server with no known reachability state (not yet probed) renders normally, not dimmed', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state.servers = { servers: [{ name: 'host-unknown', enabled: true }] };`, sandbox);
  const html = vm.runInContext(`_serverPickerBar({})`, sandbox);
  assert.ok(!html.includes('opacity:0.45'), 'a never-probed server should not be dimmed by default');
});
