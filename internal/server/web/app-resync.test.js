// Reconnect resync: one /api/sessions GET, one subscribe, no per-session
// fan-out, and an existing terminal's content flag survives.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function setup(sessionCount) {
  const sandbox = buildSandbox();
  const listeners = {};
  const sent = [];
  const sockets = [];
  sandbox.WebSocket = function () {
    const ws = {
      readyState: 1,
      addEventListener(t, fn) { listeners[t] = fn; },
      send(d) { sent.push(JSON.parse(d)); },
      close() {},
    };
    sockets.push(ws);
    return ws;
  };
  sandbox.WebSocket.OPEN = 1; sandbox.WebSocket.CONNECTING = 0;
  const sessions = Array.from({ length: sessionCount }, (_, i) => ({ id: 's' + i, full_id: 'host-s' + i, state: 'running' }));
  const fetched = [];
  sandbox.fetch = (url) => {
    fetched.push(String(url));
    const body = String(url).startsWith('/api/sessions') ? sessions : {};
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const apiCalls = [];
  sandbox.apiFetch = function (p) { apiCalls.push(p); return Promise.resolve(sessions); };
  return { sandbox, listeners, sent, fetched, apiCalls, sessions };
}

test('reconnect issues one sessions request and one subscribe, no fan-out, keeps terminal flag', async () => {
  const { sandbox, listeners, sent, fetched, apiCalls } = setup(25);
  vm.runInContext('connect()', sandbox);
  listeners.open();                       // first connect
  await flushAsync(30);
  vm.runInContext(`
    state.activeView = 'session-detail';
    state.activeSession = 'host-s3';
    state._termSessionId = 'host-s3';
    state._termHasContent = true;
    state.ws = null; state.connected = false;
  `, sandbox);
  fetched.length = 0; sent.length = 0; apiCalls.length = 0;
  vm.runInContext('connect()', sandbox);
  listeners.open();                       // reconnect
  await flushAsync(50);

  const sessReqs = fetched.filter(u => u.startsWith('/api/sessions'));
  assert.equal(sessReqs.length, 1, `sessions requests: ${fetched}`);
  assert.equal(fetched.length, 1, `no config/health refetch: ${fetched}`);
  assert.equal(sent.filter(m => m.type === 'subscribe').length, 1);
  // Only the open session's coalesced schedule load may appear; never one per session.
  // The bounded session list may not hold the open session (it can be older than
  // the recent window), so one fetch of that session by id is allowed as well.
  assert.ok(apiCalls.length <= 2, `no per-session fan-out: ${apiCalls}`);
  assert.ok(apiCalls.every(p => p.includes('session_id=host-s3') || p === '/api/sessions/host-s3'), `unexpected: ${apiCalls}`);
  assert.equal(vm.runInContext('state._termHasContent', sandbox), true);
});

test('reconnect with no terminal content falls back to a full render', async () => {
  const { sandbox, listeners, fetched } = setup(2);
  vm.runInContext('connect()', sandbox);
  listeners.open();
  await flushAsync(30);
  vm.runInContext(`
    state.activeView = 'session-detail'; state.activeSession = 'host-s1';
    state._termHasContent = false; state.ws = null;
    globalThis.__rendered = [];
    renderSessionDetail = function (sid) { globalThis.__rendered.push(sid); };
  `, sandbox);
  fetched.length = 0;
  vm.runInContext('connect()', sandbox);
  listeners.open();
  await flushAsync(50);
  assert.equal(vm.runInContext('JSON.stringify(globalThis.__rendered)', sandbox), '["host-s1"]');
});
