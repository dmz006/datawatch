// Reconnect resync: a drop and restore issues exactly one first-page summary
// request (local and through the proxy for a named server), never an
// unqualified /api/sessions, and quick successive reconnect events share it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function setup(opts = {}) {
  const sandbox = buildSandbox();
  const listeners = {};
  const sent = [];
  sandbox.WebSocket = function () {
    return {
      readyState: 1,
      addEventListener(t, fn) { listeners[t] = fn; },
      send(d) { sent.push(JSON.parse(d)); },
      close() {},
    };
  };
  sandbox.WebSocket.OPEN = 1; sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.CLOSING = 2; sandbox.WebSocket.CLOSED = 3;
  const rows = [
    { id: 's0', full_id: 'host-s0', state: 'running' },
    { id: 's1', full_id: 'host-s1', state: 'running', task: 'abc…', truncated_fields: ['task'] },
  ];
  const fetched = [];
  const gate = { hold: false, release: null };
  sandbox.fetch = (url) => {
    fetched.push(String(url));
    const respond = () => {
      const u = String(url);
      let body = {};
      if (u.includes('/api/sessions?')) body = { sessions: rows, next_cursor: '', total: rows.length };
      else if (/\/api\/sessions\/[^?]+$/.test(u)) body = { id: 's1', full_id: 'host-s1', state: 'running', task: 'abc and the rest' };
      return { ok: true, status: 200, json: () => Promise.resolve(body) };
    };
    if (gate.hold && String(url).includes('/api/sessions?')) {
      return new Promise(res => { gate.release = () => res(respond()); });
    }
    return Promise.resolve(respond());
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  if (opts.server) vm.runInContext(`state.activeServer = ${JSON.stringify(opts.server)};`, sandbox);
  return { sandbox, listeners, sent, fetched, gate };
}

// Open once (first connect), then simulate a drop and a restore.
async function dropAndRestore(ctx) {
  vm.runInContext('connect()', ctx.sandbox);
  ctx.listeners.open();
  await flushAsync(30);
  ctx.fetched.length = 0;
  ctx.listeners.close({ code: 1006 });
  vm.runInContext('state.ws = null; state.connected = false; state.reconnectTimer = null;', ctx.sandbox);
  vm.runInContext('connect()', ctx.sandbox);
  ctx.listeners.open();
}

const sessionReqs = (fetched) => fetched.filter(u => /\/api\/sessions(\?|$)/.test(u));

for (const [label, server, expectPrefix] of [
  ['local', '', ''],
  ['named server', 'peer one', '/api/proxy/peer%20one'],
]) {
  test(`${label}: drop and restore issues exactly one summary page request`, async () => {
    const ctx = setup({ server });
    await dropAndRestore(ctx);
    await flushAsync(50);
    const reqs = sessionReqs(ctx.fetched);
    assert.equal(reqs.length, 1, `sessions requests: ${ctx.fetched}`);
    assert.ok(reqs[0].startsWith(expectPrefix + '/api/sessions?view=summary&limit='), reqs[0]);
    assert.ok(!ctx.fetched.some(u => /\/api\/sessions$/.test(u)), `unqualified list fetch: ${ctx.fetched}`);
    assert.deepEqual(ctx.fetched.filter(u => !u.includes("/api/schedules")), reqs, `no other list refetched: ${ctx.fetched}`);
    // Rows merged into state.sessions.
    assert.equal(vm.runInContext('state.sessions.length', ctx.sandbox), 2);
  });

  test(`${label}: two quick reconnect events share one request`, async () => {
    const ctx = setup({ server });
    vm.runInContext('connect()', ctx.sandbox);
    ctx.listeners.open();
    await flushAsync(30);
    ctx.fetched.length = 0;
    ctx.gate.hold = true;                 // first answer still pending
    ctx.listeners.open();                 // reconnect event 1
    ctx.listeners.open();                 // reconnect event 2, before the answer
    ctx.gate.release();
    await flushAsync(50);
    assert.equal(sessionReqs(ctx.fetched).length, 1, `sessions requests: ${ctx.fetched}`);
    // A third event right after the answer (flapping socket) is coalesced too.
    ctx.gate.hold = false;
    ctx.listeners.open();
    await flushAsync(50);
    assert.equal(sessionReqs(ctx.fetched).length, 1, `after settle: ${ctx.fetched}`);
  });
}

test('a failed resync is not reused by the next reconnect', async () => {
  const ctx = setup({ server: 'peer' });
  vm.runInContext('connect()', ctx.sandbox);
  ctx.listeners.open();
  await flushAsync(30);
  ctx.fetched.length = 0;
  const realFetch = ctx.sandbox.fetch;
  ctx.sandbox.fetch = (url) => { ctx.fetched.push(String(url)); return Promise.reject(new Error('down')); };
  ctx.listeners.open();
  await flushAsync(50);
  ctx.sandbox.fetch = realFetch;
  ctx.listeners.open();
  await flushAsync(50);
  assert.equal(sessionReqs(ctx.fetched).length, 2, `retry after failure: ${ctx.fetched}`);
});

test('open session with a truncated summary row refetches its full record once', async () => {
  const ctx = setup();
  vm.runInContext('connect()', ctx.sandbox);
  ctx.listeners.open();
  await flushAsync(30);
  vm.runInContext(`
    state.activeView = 'session-detail'; state.activeSession = 'host-s1';
    state._termSessionId = 'host-s1'; state._termHasContent = true;
  `, ctx.sandbox);
  ctx.fetched.length = 0; ctx.sent.length = 0;
  ctx.listeners.open();
  await flushAsync(60);
  assert.equal(ctx.fetched.filter(u => u.includes('/api/sessions?view=summary')).length, 1, `${ctx.fetched}`);
  assert.equal(ctx.fetched.filter(u => /\/api\/sessions\/host-s1$/.test(u)).length, 1, `${ctx.fetched}`);
  // The detail view redraws once the full record arrives, which subscribes again.
  assert.ok(ctx.sent.filter(m => m.type === 'subscribe').length >= 1);
});
