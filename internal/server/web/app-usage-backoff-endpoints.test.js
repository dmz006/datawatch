// Usage back-off card — API addresses.
//
// The card's three calls (refresh / set / clear) must hit the route the
// server actually registers: /api/usage-backoff
// (server.go -> apiMux.HandleFunc("/api/usage-backoff", api.handleUsageBackoff),
// which accepts GET/POST/DELETE). A path of /api/usage/backoff matches no
// registered route at all, so every card action fails in the browser.
// apiFetch is replaced so every URL and method the card sends is observed.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const SERVER_ROUTE = '/api/usage-backoff';

function loadCardSandbox(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const calls = [];
  sandbox.apiFetch = async (url, opts = {}) => {
    calls.push({ url, method: (opts && opts.method) || 'GET', body: opts && opts.body });
    return { holds: [], active: [] };
  };
  sandbox._apiCalls = calls;
  return sandbox;
}

function callsOf(sandbox) {
  return vm.runInContext('_apiCalls', sandbox);
}

test('refresh does a GET on the registered route, never /api/usage/backoff', async () => {
  const sb = loadCardSandbox();
  vm.runInContext('usageBackoffRefresh()', sb);
  await flushAsync();
  const calls = callsOf(sb);
  assert.equal(calls.length, 1, 'one request');
  assert.equal(calls[0].url, SERVER_ROUTE, 'the server serves ' + SERVER_ROUTE);
  assert.equal(calls[0].method, 'GET');
  assert.ok(!calls.some(c => c.url.startsWith('/api/usage/backoff')),
    '/api/usage/backoff is not a registered route; no call may use it');
});

test('set does a POST on the registered route', async () => {
  const sb = loadCardSandbox({
    usageBackoffBackend: { value: 'claude-code' },
    usageBackoffMinutes: { value: '30' },
    usageBackoffModels: { value: 'opus, sonnet' },
    usageBackoffReason: { value: 'manual test' },
  });
  vm.runInContext('usageBackoffSet()', sb);
  await flushAsync();
  const calls = callsOf(sb);
  assert.equal(calls.length, 1, 'one request');
  assert.equal(calls[0].url, SERVER_ROUTE, 'the server serves ' + SERVER_ROUTE);
  assert.equal(calls[0].method, 'POST');
  const body = JSON.parse(calls[0].body);
  assert.equal(body.backend, 'claude-code');
  assert.equal(body.minutes, 30);
  assert.deepEqual(body.models, ['opus', 'sonnet']);
  assert.ok(!calls.some(c => c.url.startsWith('/api/usage/backoff')),
    '/api/usage/backoff is not a registered route; no call may use it');
});

test('clear does a DELETE (with the backend) then refreshes via the registered route', async () => {
  const sb = loadCardSandbox({
    usageBackoffBackend: { value: 'claude-code' },
    usageBackoffMinutes: { value: '' },
    usageBackoffModels: { value: '' },
    usageBackoffReason: { value: '' },
  });
  vm.runInContext('usageBackoffClear()', sb);
  await flushAsync();
  const calls = callsOf(sb);
  assert.equal(calls.length, 2, 'DELETE then the refresh GET');
  assert.equal(calls[0].url, SERVER_ROUTE, 'the server serves ' + SERVER_ROUTE);
  assert.equal(calls[0].method, 'DELETE');
  assert.equal(JSON.parse(calls[0].body).backend, 'claude-code');
  assert.equal(calls[1].url, SERVER_ROUTE);
  assert.equal(calls[1].method, 'GET');
  assert.ok(!calls.some(c => c.url.startsWith('/api/usage/backoff')),
    '/api/usage/backoff is not a registered route; no call may use it');
});
