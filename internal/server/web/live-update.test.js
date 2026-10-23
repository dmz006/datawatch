// Live-update connection review (docs/plans/2026-10-23-web-ui-load-performance.md,
// "Live-update connection review"). Drives the real app.js WebSocket code with a
// fake clock, a fake WebSocket and fake page-lifecycle events, and records
// reconnect timing. The assertions describe the fixed behaviour.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function harness() {
  const sandbox = buildSandbox();
  let now = 1_000_000;
  const timers = new Map();
  let nextId = 1;
  sandbox.setTimeout = (fn, ms) => { const id = nextId++; timers.set(id, { fn, at: now + (ms || 0), every: 0 }); return id; };
  sandbox.setInterval = (fn, ms) => { const id = nextId++; timers.set(id, { fn, at: now + ms, every: ms }); return id; };
  sandbox.clearTimeout = sandbox.clearInterval = id => { timers.delete(id); };
  const listeners = { window: {}, document: {} };
  const reg = (bag) => (ev, fn) => { (bag[ev] = bag[ev] || []).push(fn); };
  sandbox.addEventListener = reg(listeners.window);
  sandbox.document.addEventListener = reg(listeners.document);
  const sockets = [];
  class FakeWS {
    constructor(url) {
      this.url = url; this.readyState = 0; this.sent = []; this.l = {};
      sockets.push(this);
    }
    addEventListener(ev, fn) { (this.l[ev] = this.l[ev] || []).push(fn); }
    send(d) { this.sent.push(d); }
    close() { if (this.readyState >= 2) return; this.readyState = 2; this.pendingClose = true; }
    // test controls
    open() { this.readyState = 1; (this.l.open || []).forEach(f => f({})); }
    msg(o) { (this.l.message || []).forEach(f => f({ data: JSON.stringify(o) })); }
    fireClose() { this.readyState = 3; (this.l.close || []).forEach(f => f({ code: 1006 })); }
  }
  FakeWS.CONNECTING = 0; FakeWS.OPEN = 1; FakeWS.CLOSING = 2; FakeWS.CLOSED = 3;
  sandbox.WebSocket = FakeWS;
  const fetches = [];
  sandbox.fetch = (u) => { fetches.push(String(u)); return Promise.resolve({ ok: true, json: () => Promise.resolve([]) }); };
  vm.createContext(sandbox);
  vm.runInContext('Date.now = () => globalThis.__now;', sandbox);
  sandbox.__now = now;
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const h = {
    sandbox, sockets, listeners, fetches,
    get now() { return now; },
    // advance fake time, firing due timers in order
    advance(ms) {
      const end = now + ms;
      for (;;) {
        let best = null, bid = 0;
        for (const [id, t] of timers) if (t.at <= end && (!best || t.at < best.at)) { best = t; bid = id; }
        if (!best) break;
        now = best.at; sandbox.__now = now;
        if (best.every) best.at += best.every; else timers.delete(bid);
        best.fn();
      }
      now = end; sandbox.__now = now;
    },
    setVisible(v) { sandbox.document.visibilityState = v ? 'visible' : 'hidden'; (listeners.document.visibilitychange || []).forEach(f => f()); },
    fire(ev) { (listeners.window[ev] || []).forEach(f => f()); },
    run(code) { return vm.runInContext(code, sandbox); },
    pings(ws) { return ws.sent.filter(s => JSON.parse(s).type === 'ping').length; },
    live() { return sockets.filter(s => s.readyState === 0 || s.readyState === 1).length; },
  };
  return h;
}

// Mirror of the server: every ping is answered with a pong frame.
function connectedHarness() {
  const h = harness();
  h.run('connect()');
  h.sockets[0].open();
  return h;
}

test('healthy but quiet socket (pongs only) is never closed by the watchdog', () => {
  const h = connectedHarness();
  const ws = h.sockets[0];
  for (let i = 0; i < 40; i++) { h.advance(15000); ws.msg({ type: 'pong' }); }
  assert.equal(ws.readyState, 1);
  assert.equal(h.sockets.length, 1);
  assert.ok(h.pings(ws) >= 39);
});

test('stalled socket (no frames at all) is force-closed within one watchdog tick of the stale limit', () => {
  const h = connectedHarness();
  const ws = h.sockets[0];
  h.advance(35000);
  assert.equal(ws.readyState, 1, 'not stale at the limit');
  h.advance(15000);
  assert.equal(ws.readyState, 2, 'closed after the limit plus one tick (<=50s)');
});

test('a close schedules exactly one reconnect within the full-jitter ceiling and opens one socket', () => {
  const h = connectedHarness();
  h.sockets[0].fireClose();
  h.advance(1000);
  assert.equal(h.sockets.length, 2);
  assert.equal(h.live(), 1);
});

test('repeated failures back off with growing ceiling, never exceeding the 30s cap, with no overlapping sockets', () => {
  const h = harness();
  h.run('connect()');
  const gaps = [];
  let last = h.now;
  for (let i = 0; i < 12; i++) {
    const s = h.sockets[h.sockets.length - 1];
    s.fireClose();
    const before = h.sockets.length;
    let waited = 0;
    while (h.sockets.length === before && waited < 31000) { h.advance(250); waited += 250; }
    assert.equal(h.sockets.length, before + 1, 'one reconnect per close');
    gaps.push(waited);
    assert.ok(waited <= 30250, 'gap ' + waited + ' within cap');
    assert.equal(h.live(), 1);
  }
  assert.ok(Math.max(...gaps) >= 1000, 'backoff grows beyond the first step: ' + gaps.join(','));
});

test('a socket that opens and dies at once does not reset the backoff (flap)', () => {
  const h = harness();
  h.run('connect()');
  for (let i = 0; i < 5; i++) {
    const s = h.sockets[h.sockets.length - 1];
    s.open(); s.fireClose();
    h.advance(31000);
  }
  assert.ok(h.run('state.reconnectAttempt') >= 4);
});

test('backoff resets once a socket has stayed open past the stable window', () => {
  const h = harness();
  h.run('connect()');
  h.sockets[0].fireClose(); h.advance(31000);
  h.sockets[1].fireClose(); h.advance(31000);
  const ws = h.sockets[2];
  ws.open();
  for (let i = 0; i < 3; i++) { h.advance(15000); ws.msg({ type: 'pong' }); }
  assert.equal(h.run('state.reconnectAttempt'), 0);
});

test('hidden page: no reconnect while hidden, one socket on return (no storm)', () => {
  const h = connectedHarness();
  h.setVisible(false);
  h.sockets[0].fireClose();
  h.advance(120000);
  assert.equal(h.sockets.length, 1, 'no reconnect while hidden');
  h.setVisible(true);
  h.fire('online'); // phones often fire both
  assert.equal(h.sockets.length, 2, 'exactly one new socket for visible+online');
  assert.equal(h.live(), 1);
});

test('returning from background with a silently dead socket reconnects within a short grace, not 35-50s', () => {
  const h = connectedHarness();
  const ws = h.sockets[0];
  h.setVisible(false);
  h.advance(10 * 60 * 1000); // phone asleep, mapping dropped, no frames
  h.setVisible(true);
  assert.ok(h.pings(ws) >= 1, 'probes with a ping on return');
  h.advance(6000); // WS_WAKE_GRACE_MS
  assert.equal(ws.readyState, 2, 'dead socket closed after the grace window');
});

test('returning from background with a live socket keeps it when the pong arrives', () => {
  const h = connectedHarness();
  const ws = h.sockets[0];
  h.setVisible(false);
  h.advance(10 * 60 * 1000);
  h.setVisible(true);
  ws.msg({ type: 'pong' });
  h.advance(20000);
  assert.equal(ws.readyState, 1);
  assert.equal(h.sockets.length, 1);
});

test('offline then online reconnects immediately and restarts backoff from the first step', () => {
  const h = harness();
  h.run('connect()');
  for (let i = 0; i < 6; i++) { h.sockets[h.sockets.length - 1].fireClose(); h.advance(31000); }
  assert.ok(h.run('state.reconnectAttempt') >= 5);
  const n = h.sockets.length;
  h.sockets[n - 1].fireClose();
  h.fire('online');
  assert.equal(h.sockets.length, n + 1, 'connects at once on online');
  assert.equal(h.run('state.reconnectAttempt'), 0, 'attempt counter restarted');
});

test('late close of a superseded socket does not drop the new socket or spawn a duplicate', () => {
  const h = connectedHarness();
  const a = h.sockets[0];
  h.advance(50000); // watchdog closes a (CLOSING, close event still pending)
  assert.equal(a.readyState, 2);
  h.fire('online'); // wake opens b while a is still closing
  const b = h.sockets[1];
  assert.ok(b, 'new socket opened');
  b.open();
  a.fireClose(); // late close from the old socket
  assert.equal(h.run('state.ws') === b, true, 'state.ws still points at the new socket');
  assert.equal(h.run('state.connected'), true);
  h.fire('online');
  b.msg({ type: 'pong' });
  for (let i = 0; i < 4; i++) { h.advance(15000); b.msg({ type: 'pong' }); }
  assert.equal(h.live(), 1, 'still a single live socket');
  assert.equal(h.sockets.length, 2, 'no duplicate opened');
});

test('reconnect resync fetches the summary form, never the full unpaged sessions list', async () => {
  const h = connectedHarness();
  h.sockets[0].fireClose();
  h.advance(31000);
  h.sockets[1].open();
  await new Promise(r => setImmediate(r));
  const sess = h.fetches.filter(u => u.startsWith('/api/sessions'));
  assert.ok(sess.length >= 1, 'resync fetched sessions');
  assert.ok(sess.every(u => u.includes('view=summary') && u.includes('limit=')), sess.join(' '));
});

test('federated-server resync also uses the summary form', async () => {
  const h = connectedHarness();
  h.run("state.activeServer = 'remote1'");
  h.sockets[0].fireClose();
  h.advance(31000);
  h.sockets[1].open();
  await new Promise(r => setImmediate(r));
  const sess = h.fetches.filter(u => u.startsWith('/api/sessions'));
  assert.ok(sess.every(u => u.includes('view=summary')), sess.join(' '));
});

test('list loader keeps polling at its interval while the socket is down, and skips polls while the page is hidden', async () => {
  const h = harness();
  let calls = 0;
  h.run('window.__calls = 0');
  const ctl = h.run(`listLoader({ key: 'lu-test', el: null, pollMs: 30000, fetch: () => { window.__calls++; return Promise.resolve([]); }, render: () => '' })`);
  await ctl.load();
  assert.equal(h.run('window.__calls'), 1);
  h.advance(30000); await new Promise(r => setImmediate(r));
  assert.equal(h.run('window.__calls'), 2, 'polled with no socket open');
  h.setVisible(false);
  h.advance(120000); await new Promise(r => setImmediate(r));
  assert.equal(h.run('window.__calls'), 2, 'no polling while hidden');
  h.setVisible(true);
  await new Promise(r => setImmediate(r));
  assert.equal(h.run('window.__calls'), 3, 'refreshes on return when the data is past its interval');
});
