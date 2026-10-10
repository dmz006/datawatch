// Coordinator-flagged (2026-10-08) -- extends the prior round's
// instrumentation of Observer's primary stats card (loadStatsPanel,
// pinned in app-fed-cap-errors.test.js) to its other ~11 independent
// sub-card loaders, all of which already route through apiFetch (so
// already get the real 401/403/502 text via _fedFetchError) but
// replaced it with a generic "unavailable" string in their own
// .catch(). See docs/plans/historical-plans/2026-10-08-pwa-federated-error-visibility.md
// for the full list and docs/plans/historical-plans/2026-10-08-gh194-never-say-local.md
// for the related GH#194 round.
//
// Not every one of the 12 loaders gets its own test here -- most share
// one trivial pattern (_fedMsg(e, fallback) in a one-line .catch),
// already proven by loadStatsPanel's own test. This file covers that
// pattern once more on a different endpoint, plus the two genuinely
// different shapes: a "hide the card on any failure" pattern that had
// to become "hide only for a local/non-error case, show the real
// message for a remote denial" (ACME, cluster nodes -- hiding
// unconditionally made a capability denial indistinguishable from the
// card's own normal "not configured" case), and the dual-fetch
// Promise.all peers+nodes pattern that previously swallowed the real
// error before the "no peers" branch ever saw it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._fedMsg, 'function', '_fedMsg is not defined -- did it get renamed?');
  return sandbox;
}

function fakeResponse(status, body) {
  return { ok: status >= 200 && status < 300, status, text: () => Promise.resolve(body || ''), json: () => Promise.resolve(JSON.parse(body || '{}')) };
}

test('loadPluginsStatus: a federated capability error reaches the card, not a generic "plugin status unavailable"', async () => {
  const sandbox = loadAppJS();
  const listEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'pluginsStatusList' ? listEl : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/plugins')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: plugins:read'))
    : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext('loadPluginsStatus()', sandbox);
  await flushAsync();
  assert.match(listEl.innerHTML, /plugins:read/, `expected the real capability error; got: ${listEl.innerHTML}`);
});

test('loadAcmeHealthCard: a federated denial shows the real error AND un-hides the card (a hidden card used to look identical to "ACME not enabled")', async () => {
  const sandbox = loadAppJS();
  const block = makeStubElement();
  const list = makeStubElement();
  sandbox.document.getElementById = (id) => {
    if (id === 'acmeHealthBlock') return block;
    if (id === 'acmeHealthList') return list;
    return makeStubElement();
  };
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/acme/status')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: acme:read'))
    : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext('loadAcmeHealthCard()', sandbox);
  await flushAsync();
  assert.notEqual(block.style.display, 'none', 'a federated denial must not hide the card the way "ACME disabled" does');
  assert.match(list.innerHTML, /acme:read/, `expected the real capability error; got: ${list.innerHTML}`);
});

test('loadAcmeHealthCard: a LOCAL failure still hides the card (preserves the "ACME not enabled" common case)', async () => {
  const sandbox = loadAppJS();
  const block = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'acmeHealthBlock' ? block : makeStubElement());
  vm.runInContext(`state.activeServer = null;`, sandbox);
  sandbox.fetch = () => Promise.resolve(fakeResponse(500, 'internal error'));
  vm.runInContext('loadAcmeHealthCard()', sandbox);
  await flushAsync();
  assert.equal(block.style.display, 'none', 'a non-federated failure should keep the original hide-the-card behavior');
});

test('loadObserverClusterNodes: a federated denial shows the real error AND un-hides the card', async () => {
  const sandbox = loadAppJS();
  const block = makeStubElement();
  const list = makeStubElement();
  sandbox.document.getElementById = (id) => {
    if (id === 'observerClusterBlock') return block;
    if (id === 'observerClusterList') return list;
    return makeStubElement();
  };
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/observer/stats')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: observers:read'))
    : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext('loadObserverClusterNodes()', sandbox);
  await flushAsync();
  assert.notEqual(block.style.display, 'none', 'a federated denial must not hide the card the way "single-node, no cluster" does');
  assert.match(list.innerHTML, /observers:read/, `expected the real capability error; got: ${list.innerHTML}`);
});

test('loadObserverPeers: a federated denial on /api/observer/peers shows the real error, not "no peers registered"', async () => {
  const sandbox = loadAppJS();
  const list = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'observerPeersList' ? list : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/observer/peers')) return Promise.resolve(fakeResponse(403, 'federation peer lacks capability: observers:list'));
    if (url.includes('/api/compute/nodes')) return Promise.resolve(fakeResponse(200, '{"nodes":[]}'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadObserverPeers()', sandbox);
  await flushAsync();
  assert.match(list.innerHTML, /observers:list/, `expected the real capability error, not the generic empty-peers message; got: ${list.innerHTML}`);
});

test('loadObserverPeers: a genuinely empty peer list (no error) still shows the normal "no peers registered" guidance', async () => {
  const sandbox = loadAppJS();
  const list = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'observerPeersList' ? list : makeStubElement());
  vm.runInContext(`state.activeServer = null;`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/observer/peers')) return Promise.resolve(fakeResponse(200, '{"peers":[]}'));
    if (url.includes('/api/compute/nodes')) return Promise.resolve(fakeResponse(200, '{"nodes":[]}'));
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadObserverPeers()', sandbox);
  await flushAsync();
  assert.match(list.innerHTML, /no peers registered/, `expected the normal empty-state guidance; got: ${list.innerHTML}`);
});
