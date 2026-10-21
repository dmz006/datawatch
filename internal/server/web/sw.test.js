// GATE: navigate-on-activate guard tests. Motivation mirrors the long
// string of SW-activate comments in sw.js (v6.13.13 and the GATE alpha.36
// bump): on a real upgrade the new SW must force every controlled window
// to re-navigate so installed PWA tabs drop the stale cached app.js.
// But activate also re-runs on a complete no-op re-activation (the browser
// re-activates the same SW on restart with no cache change) — the guard
// (shouldNavigateOnActivate) keeps that path from needlessly churning the
// user's open tabs: purge + clients.claim() still happen, the
// matchAll/navigate loop is skipped.
//
// This file (a) unit-tests the pure guard via the module.exports sw.js
// exposes for node, and (b) drives the real 'activate' listener
// end-to-end with a stubbed `self` (captured at registration time via
// addEventListener) plus stubbed `caches` and `self.clients`.

const test = require('node:test');
const assert = require('node:assert/strict');

// sw.js is a bare service-worker script that references `self` at load
// time (self.addEventListener / self.skipWaiting). Stub it BEFORE
// requiring so the top-level registrations run cleanly; addEventListener
// captures the listeners so the activate handler can be invoked directly.
const listeners = {};
global.self = {
  addEventListener(type, fn) { listeners[type] = fn; },
  skipWaiting() {},
  clients: {},
  registration: {},
};

const { shouldNavigateOnActivate } = require('./sw.js');

const CURRENT = 'datawatch-v7-0-0-alpha-51';

// ---------------- pure guard ----------------

test('shouldNavigateOnActivate: no keys at all is a no-op re-activation', () => {
  assert.equal(shouldNavigateOnActivate([], CURRENT), false);
});

test('shouldNavigateOnActivate: only the current cache present is a no-op re-activation', () => {
  assert.equal(shouldNavigateOnActivate([CURRENT], CURRENT), false);
});

test('shouldNavigateOnActivate: current plus a stale key is a genuine upgrade', () => {
  assert.equal(shouldNavigateOnActivate(['datawatch-v5-6-1', CURRENT], CURRENT), true);
});

test('shouldNavigateOnActivate: multiple stale keys are a genuine upgrade', () => {
  assert.equal(shouldNavigateOnActivate(['old-1', 'old-2', CURRENT], CURRENT), true);
});

test('shouldNavigateOnActivate: non-array input is treated as no keys', () => {
  assert.equal(shouldNavigateOnActivate(null, CURRENT), false);
  assert.equal(shouldNavigateOnActivate(undefined, CURRENT), false);
});

// ---------------- activate handler end-to-end ----------------

function makeWindow(url, opts = {}) {
  return {
    url,
    attempts: 0,
    async navigate(u) {
      this.attempts += 1;
      assert.equal(u, url, 'navigate called with unexpected URL');
      if (opts.reject) throw new Error(opts.reject);
    },
  };
}

// Invoke the captured activate handler with a given set of live cache
// keys and `n` window clients; resolve the handler's waitUntil promise and
// return observables.
function runActivate({ cacheKeys, windows }) {
  const deleted = [];
  let matchAllCalls = 0;
  global.caches = {
    keys: async () => cacheKeys.slice(),
    delete: k => { deleted.push(k); return Promise.resolve(true); },
    open: () => Promise.reject(new Error('activate must not open caches')),
  };
  const claims = { count: 0 };
  global.self.clients = {
    claim: async () => { claims.count += 1; },
    matchAll: async () => { matchAllCalls += 1; return windows; },
  };

  let settled;
  const event = {
    waitUntil(p) {
      settled = p.then(
        () => ({ ok: true }),
        err => ({ ok: false, err }),
      );
    },
  };
  listeners['activate'](event);
  return settled.then(result => ({ result, deleted, claims, matchAllCalls, windows }));
}

test('activate: stale cache present -> stale deleted, claim called, every window navigated once', async () => {
  const w1 = makeWindow('https://datawatch.local/');
  const w2 = makeWindow('https://datawatch.local/diagrams.html');
  const { result, deleted, claims, windows } = await runActivate({
    cacheKeys: ['datawatch-v5-6-1', CURRENT, 'datawatch-v6-0-0'],
    windows: [w1, w2],
  });
  assert.equal(result.ok, true, `activate handler rejected: ${result.err}`);
  assert.deepEqual(deleted.sort(), ['datawatch-v5-6-1', 'datawatch-v6-0-0'], 'only stale caches deleted');
  assert.equal(claims.count, 1, 'clients.claim called once');
  assert.equal(windows[0].attempts, 1, 'window 1 navigated exactly once');
  assert.equal(windows[1].attempts, 1, 'window 2 navigated exactly once');
});

test('activate: only the current cache present (no-op) -> claim called, no navigate', async () => {
  const w1 = makeWindow('https://datawatch.local/');
  const { result, deleted, claims, matchAllCalls } = await runActivate({
    cacheKeys: [CURRENT],
    windows: [w1],
  });
  assert.equal(result.ok, true, `activate handler rejected: ${result.err}`);
  assert.deepEqual(deleted, [], 'nothing deleted');
  assert.equal(claims.count, 1, 'clients.claim still called');
  assert.equal(matchAllCalls, 0, 'matchAll skipped on no-op re-activation');
  assert.equal(w1.attempts, 0, 'window NOT navigated');
});

test('activate: no caches at all -> claim called, no navigate', async () => {
  const w1 = makeWindow('https://datawatch.local/');
  const { result, deleted, claims, matchAllCalls } = await runActivate({
    cacheKeys: [],
    windows: [w1],
  });
  assert.equal(result.ok, true, `activate handler rejected: ${result.err}`);
  assert.deepEqual(deleted, [], 'nothing deleted');
  assert.equal(claims.count, 1, 'clients.claim still called');
  assert.equal(matchAllCalls, 0, 'matchAll skipped with no caches');
  assert.equal(w1.attempts, 0, 'window NOT navigated');
});

test('activate: one window navigate rejecting does not abort the others or the handler', async () => {
  const w1 = makeWindow('https://datawatch.local/', { reject: 'boom: not controlled' });
  const w2 = makeWindow('https://datawatch.local/diagrams.html');
  const { result } = await runActivate({
    cacheKeys: ['datawatch-v6-0-0', CURRENT],
    windows: [w1, w2],
  });
  assert.equal(result.ok, true, `activate handler must swallow navigate errors: ${result.err}`);
  assert.equal(w1.attempts, 1, 'failing window was attempted');
  assert.equal(w2.attempts, 1, 'second window still navigated despite first rejecting');
});
