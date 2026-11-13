// SW upgrade path: versioned registration + once-only auto-reload.
//
// Two problems this file pins down (see sw.js CACHE_NAME comment + app.js
// registerServiceWorker):
//   1. The worker script's bytes are identical every release, so a bare
//      /sw.js registration can leave the browser on a stale worker. The page
//      therefore registers /sw.js?v=<daemon version> (a distinct script URL
//      per release) and adopts the versioned URL later when the version is
//      first learned.
//   2. When a new worker takes control (controllerchange on a real upgrade)
//      and the page is idle, the page reloads EXACTLY ONCE: the sessionStorage
//      marker (SW_AUTO_RELOAD_MARK_KEY) survives the reload, so any later
//      controllerchange (claim re-firing, re-register, next boot of the same
//      tab) must not reload again. If the marker cannot be written, the slot
//      is refused rather than risking an unrecorded (i.e. repeatable) reload.
//
// Same harness as the other app-*.test.js files here: load the REAL app.js
// into a Node vm sandbox (testutil_browser_stub.js) and drive the public
// top-level functions against stubs.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const MARK_KEY = 'cs_sw_autoreload_done';

// Map-backed sessionStorage stand-in (sessionStorage survives a page reload
// within a tab; the Map does the same for the duration of the test).
function makeSessionStorage() {
  const m = new Map();
  return {
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  };
}

function loadAppJS(opts = {}) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.registerServiceWorker, 'function',
    'registerServiceWorker is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._swScriptUrl, 'function',
    '_swScriptUrl is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._swTakeAutoReloadSlot, 'function',
    '_swTakeAutoReloadSlot is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._swAdoptDaemonVersion, 'function',
    '_swAdoptDaemonVersion is not defined -- did it get renamed?');
  if (opts.sessionStorage) sandbox.sessionStorage = opts.sessionStorage;
  sandbox.__reloads = 0;
  sandbox.location.reload = () => { sandbox.__reloads += 1; };
  return sandbox;
}

// navigator.serviceWorker stand-in that records register() calls and the
// controllerchange listener; register() resolves with a minimal
// registration (update/addEventListener only; no waiting/installing).
function installServiceWorker(sandbox, { controller = null } = {}) {
  const out = { registerCalls: [], controllerchange: [] };
  const reg = {
    scope: 'http://localhost/',
    update: () => Promise.resolve(),
    addEventListener: () => {},
  };
  sandbox.navigator.serviceWorker = {
    controller,
    register(url) { out.registerCalls.push(url); return Promise.resolve(reg); },
    addEventListener(type, fn) {
      if (type === 'controllerchange') out.controllerchange.push(fn);
    },
  };
  return out;
}

// ── _swScriptUrl (pure) ────────────────────────────────────────────────

test('_swScriptUrl: version becomes a distinct, encoded script URL', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext(`_swScriptUrl('9.119.0')`, sandbox), '/sw.js?v=9.119.0');
  assert.equal(vm.runInContext(`_swScriptUrl('v7.0.0-alpha.51')`, sandbox), '/sw.js?v=v7.0.0-alpha.51');
  // Two releases -> two different script URLs (the browser sees a new file).
  const a = vm.runInContext(`_swScriptUrl('9.119.0')`, sandbox);
  const b = vm.runInContext(`_swScriptUrl('9.120.0')`, sandbox);
  assert.notEqual(a, b);
  // Unsafe characters are percent-encoded, never left raw.
  assert.equal(vm.runInContext(`_swScriptUrl('a/b c')`, sandbox), '/sw.js?v=a%2Fb%20c');
});

test('_swScriptUrl: no usable version falls back to the bare URL', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext(`_swScriptUrl(undefined)`, sandbox), '/sw.js');
  assert.equal(vm.runInContext(`_swScriptUrl('')`, sandbox), '/sw.js');
  assert.equal(vm.runInContext(`_swScriptUrl('   ')`, sandbox), '/sw.js');
});

// ── _swTakeAutoReloadSlot (pure, once-only) ────────────────────────────

test('_swTakeAutoReloadSlot: the slot is granted exactly once', () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  sandbox.__slotStore = sandbox.sessionStorage;
  assert.equal(vm.runInContext('_swTakeAutoReloadSlot(globalThis.__slotStore)', sandbox), true);
  assert.equal(sandbox.sessionStorage.getItem(MARK_KEY), '1');
  // Second ask in the same tab: refused.
  assert.equal(vm.runInContext('_swTakeAutoReloadSlot(globalThis.__slotStore)', sandbox), false);
});

test('_swTakeAutoReloadSlot: a missing or broken store refuses the slot', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('_swTakeAutoReloadSlot(null)', sandbox), false);
  sandbox.__broken = { getItem() { throw new Error('denied'); }, setItem() {} };
  assert.equal(vm.runInContext('_swTakeAutoReloadSlot(globalThis.__broken)', sandbox), false);
});

// ── versioned registration ─────────────────────────────────────────────

test('registerServiceWorker: registers the versioned script URL when the version is known', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  vm.runInContext(`state._daemonVersion = '9.119.0';`, sandbox);
  const sw = installServiceWorker(sandbox, { controller: null });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.deepEqual(sw.registerCalls, ['/sw.js?v=9.119.0']);
  assert.equal(sandbox._swRegUrl, '/sw.js?v=9.119.0');
});

test('registerServiceWorker: bare URL before the version is known, versioned adopt after', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  // Version not recorded yet -> bare /sw.js (the fallback).
  assert.equal(vm.runInContext('state._daemonVersion', sandbox), undefined);
  const sw = installServiceWorker(sandbox, { controller: null });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.deepEqual(sw.registerCalls, ['/sw.js']);
  assert.equal(sandbox._swRegUrl, '/sw.js');

  // The daemon version is first learned after registration (boot health
  // fetch / first sessions frame): adopt the versioned URL.
  vm.runInContext(`state._daemonVersion = '9.119.0'; _swAdoptDaemonVersion('9.119.0');`, sandbox);
  await flushAsync(6);
  assert.deepEqual(sw.registerCalls, ['/sw.js', '/sw.js?v=9.119.0']);
  assert.equal(sandbox._swRegUrl, '/sw.js?v=9.119.0');

  // Re-adopting the SAME version is a no-op (idempotent per scope).
  vm.runInContext(`_swAdoptDaemonVersion('9.119.0');`, sandbox);
  await flushAsync(6);
  assert.deepEqual(sw.registerCalls, ['/sw.js', '/sw.js?v=9.119.0']);
});

test('_swAdoptDaemonVersion: a no-op when serviceWorker is unavailable or no version', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  const sw = installServiceWorker(sandbox, { controller: null });
  vm.runInContext(`_swAdoptDaemonVersion(undefined); _swAdoptDaemonVersion('');`, sandbox);
  await flushAsync(4);
  assert.deepEqual(sw.registerCalls, []);
});

// ── controllerchange: reload exactly once ──────────────────────────────

test('controllerchange (real upgrade, idle): reloads exactly once, marker persists', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  const sw = installServiceWorker(sandbox, { controller: { id: 'old-worker' } }); // an existing controller
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.equal(sw.controllerchange.length, 1, 'exactly one controllerchange listener attached');
  const handler = sw.controllerchange[0];

  // Idle enough for the auto-reload path.
  vm.runInContext(`state._lastUserActivityAt = Date.now() - 60000;`, sandbox);

  handler(); // the new worker took control
  assert.equal(sandbox.__reloads, 1, 'first upgrade signal reloads the idle page');
  assert.equal(sandbox.sessionStorage.getItem(MARK_KEY), '1', 'the once-only marker was written');

  // The marker survives the reload. Any further controllerchange in this
  // tab (claim re-firing, re-register, etc.) must NOT reload again.
  handler();
  assert.equal(sandbox.__reloads, 1, 'second controllerchange must not reload');
  handler();
  assert.equal(sandbox.__reloads, 1, 'third controllerchange must not reload');
});

test('controllerchange on a freshly reloaded page: the surviving marker blocks the reload', async () => {
  // Model a page that just came back from its upgrade reload: new
  // registration, new handler, but the SAME tab's sessionStorage marker.
  const sessionStorage = makeSessionStorage();
  sessionStorage.setItem(MARK_KEY, '1'); // the marker that survived the reload
  const sandbox = loadAppJS({ sessionStorage });
  const sw = installServiceWorker(sandbox, { controller: { id: 'new-worker' } });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  vm.runInContext(`state._lastUserActivityAt = Date.now() - 60000;`, sandbox);
  sw.controllerchange[0]();
  assert.equal(sandbox.__reloads, 0, 'marker from before the reload must block a second reload');
});

test('controllerchange (first install): first event is ignored, a real upgrade still reloads once', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  // No controller before registration -> first event is the install claim.
  const sw = installServiceWorker(sandbox, { controller: null });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  const handler = sw.controllerchange[0];
  vm.runInContext(`state._lastUserActivityAt = Date.now() - 60000;`, sandbox);

  handler(); // install claim, not an upgrade
  assert.equal(sandbox.__reloads, 0, 'first-install controllerchange must not reload');
  assert.equal(sandbox.sessionStorage.getItem(MARK_KEY), null, 'no slot consumed by first install');

  handler(); // a genuine upgrade later
  assert.equal(sandbox.__reloads, 1, 'a real upgrade still auto-reloads exactly once');
  handler();
  assert.equal(sandbox.__reloads, 1);
});

test('controllerchange (idle) but storage unavailable: refuses to reload rather than loop', async () => {
  // No sessionStorage global at all (sandbox default) -> the slot cannot be
  // recorded -> refuse: a reload that cannot be remembered would repeat.
  const sandbox = loadAppJS();
  assert.equal(typeof sandbox.sessionStorage, 'undefined');
  const sw = installServiceWorker(sandbox, { controller: { id: 'old-worker' } });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  vm.runInContext(`state._lastUserActivityAt = Date.now() - 60000;`, sandbox);
  sw.controllerchange[0]();
  assert.equal(sandbox.__reloads, 0, 'unrecordable reload must be refused');
});

test('controllerchange (user not idle): no reload, banner path instead', async () => {
  const sandbox = loadAppJS({ sessionStorage: makeSessionStorage() });
  const sw = installServiceWorker(sandbox, { controller: { id: 'old-worker' } });
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  // User is actively typing: _markUserActivity just ran at load time, so
  // the idle threshold (30s) has not been met.
  sw.controllerchange[0]();
  assert.equal(sandbox.__reloads, 0, 'an active user is never reloaded');
  assert.equal(sandbox.sessionStorage.getItem(MARK_KEY), null, 'the once-only slot is not consumed');
  assert.equal(vm.runInContext('state._updateBannerShown', sandbox), true,
    'the update banner path (banner + recheck) is used instead');
});
