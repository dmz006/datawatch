// GH#194 (operator decision via datawatch-app): the PWA must never label
// the connected server "local" to the operator -- everywhere it used to
// say the literal word "Local" now shows the server's real hostname
// (from /api/health's own hostname field) instead: the picker chip, the
// swipe-gesture modal's picker, the "Back to %1$s" button, the
// connection toast, the Settings server list entry, and Observer's
// per-system stats grid (which also had a real duplicate-card bug: the
// local /api/stats card and the server's own synthesized "self" peer
// entry in /api/observer/peers represent the same physical machine).
//
// "local" itself stays the stable routing/comparison key used
// throughout apiFetch/selectServer/etc. -- this is purely a display
// layer fix. See docs/plans/historical-plans/2026-10-08-gh194-never-say-local.md.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._ensureLocalHostname, 'function', '_ensureLocalHostname is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._serverPickerBar, 'function', '_serverPickerBar is not defined -- did it get renamed?');
  assert.equal(typeof sandbox.loadSystemStatsGrid, 'function', 'loadSystemStatsGrid is not defined -- did it get renamed?');
  return sandbox;
}

function fakeResponse(status, body) {
  return { ok: status >= 200 && status < 300, status, text: () => Promise.resolve(body || ''), json: () => Promise.resolve(JSON.parse(body || '{}')) };
}

// ── _ensureLocalHostname ────────────────────────────────────────────────

test('_ensureLocalHostname: caches the real hostname from /api/health', async () => {
  const sandbox = loadAppJS();
  sandbox.fetch = (url) => url.includes('/api/health')
    ? Promise.resolve(fakeResponse(200, JSON.stringify({ hostname: 'johnnyjohnny', version: '1.2.3' })))
    : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext('_ensureLocalHostname()', sandbox);
  await flushAsync();
  assert.equal(vm.runInContext('_localHostname', sandbox), 'johnnyjohnny');
});

test('_ensureLocalHostname: does not re-fetch once already resolved', async () => {
  const sandbox = loadAppJS();
  let calls = 0;
  sandbox.fetch = (url) => { if (url.includes('/api/health')) calls++; return Promise.resolve(fakeResponse(200, JSON.stringify({ hostname: 'h1' }))); };
  vm.runInContext('_ensureLocalHostname()', sandbox);
  await flushAsync();
  vm.runInContext('_ensureLocalHostname()', sandbox);
  await flushAsync();
  assert.equal(calls, 1, 'a second call after the hostname is already known should be a no-op');
});

// ── _serverPickerBar: local chip never says "Local" ─────────────────────

test('_serverPickerBar: shows the real hostname for the local chip once known', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`
    state.servers = { servers: [{ name: 'host-a', enabled: true }] };
    _localHostname = 'johnnyjohnny';
  `, sandbox);
  const html = vm.runInContext(`_serverPickerBar({})`, sandbox);
  assert.match(html, /johnnyjohnny/, `expected the real hostname in the chip; got: ${html}`);
  assert.doesNotMatch(html, />Local</, 'the literal word "Local" must never appear as a chip label');
});

test('_serverPickerBar: falls back to a generic (never "Local") placeholder before the hostname is known', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state.servers = { servers: [{ name: 'host-a', enabled: true }] };`, sandbox);
  // _localHostname deliberately left null/unresolved.
  const html = vm.runInContext(`_serverPickerBar({})`, sandbox);
  assert.doesNotMatch(html, />Local</, `the literal word "Local" must never appear, even before the real hostname resolves; got: ${html}`);
});

// ── Observer per-system grid: no duplicate card, no "local" badge ──────

test('loadSystemStatsGrid: the server\'s own synthesized self-peer is not shown as a second, duplicate card', async () => {
  const sandbox = loadAppJS();
  const gridEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'perSystemGrid' ? gridEl : makeStubElement());
  vm.runInContext(`state.activeServer = null;`, sandbox);
  sandbox.fetch = (url) => {
    if (url.includes('/api/stats')) {
      return Promise.resolve(fakeResponse(200, JSON.stringify({
        timestamp: Date.now(), hostname: 'johnnyjohnny', cpu_cores: 8, cpu_load_avg_1: 1, cpu_load_avg_5: 1, cpu_load_avg_15: 1,
        mem_used: 100, mem_total: 200,
      })));
    }
    if (url.includes('/api/observer/peers/')) {
      // per-peer snapshot fetch for the one real (non-self) peer
      return Promise.resolve(fakeResponse(200, JSON.stringify({ cpu: { pct: 5 }, mem: null, gpu: [] })));
    }
    if (url.endsWith('/api/observer/peers')) {
      return Promise.resolve(fakeResponse(200, JSON.stringify({
        peers: [
          { name: 'johnnyjohnny', is_self: true },   // same machine as /api/stats above -- must be filtered
          { name: 'other-peer', is_self: false, last_push_at: new Date().toISOString() },
        ],
      })));
    }
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadSystemStatsGrid()', sandbox);
  await flushAsync();
  await flushAsync();
  const html = gridEl.innerHTML;
  const occurrences = (html.match(/johnnyjohnny/g) || []).length;
  assert.equal(occurrences, 1, `"johnnyjohnny" (the local hostname AND the self-peer's name) should render exactly once, not duplicated; got ${occurrences} in: ${html}`);
  assert.match(html, /other-peer/, 'the genuinely distinct peer should still render');
  assert.doesNotMatch(html, />local</, 'no card should carry a literal "local" badge/tag');
});

// ── Settings server list: local entry shows the real hostname ──────────

test('loadServers (Settings list): the local entry displays its real hostname, not the literal word "local"', async () => {
  const sandbox = loadAppJS();
  const listEl = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'serverStatus' ? listEl : makeStubElement());
  sandbox.fetch = (url) => {
    if (url.includes('/api/servers/health')) return Promise.resolve(fakeResponse(200, '[]'));
    if (url.includes('/api/config')) return Promise.resolve(fakeResponse(200, '{}'));
    if (url.includes('/api/servers')) {
      return Promise.resolve(fakeResponse(200, JSON.stringify([
        { name: 'local', url: 'http://localhost:8443', has_auth: false, enabled: true, hostname: 'johnnyjohnny' },
      ])));
    }
    return Promise.resolve(fakeResponse(200, '{}'));
  };
  vm.runInContext('loadServers()', sandbox);
  await flushAsync();
  await flushAsync();
  assert.match(listEl.innerHTML, /johnnyjohnny/, `expected the real hostname; got: ${listEl.innerHTML}`);
  assert.doesNotMatch(listEl.innerHTML, /<strong>local<\/strong>/, 'the row must not display the literal word "local" as the name');
});
