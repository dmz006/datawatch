// Operator-reported 2026-10-06 — "sent a command after being idle for a
// few minutes and had to exit the session and go back in for it to start
// moving again." Root cause: ws.readyState can stay OPEN after a NAT/proxy
// silently drops an idle TCP mapping — no close/error event ever fires, so
// scheduleReconnect() (which only runs from those two event handlers) never
// triggers. Live-verified with Playwright against a real daemon: a real
// ping/pong round trip happens every WS_PING_INTERVAL_MS, and freezing
// wsLastActivityAt against further writes (simulating every frame — not
// just pings — silently failing to arrive) causes the watchdog to force-
// close the stale socket and open a fresh one within one tick.
//
// This file unit-tests the two pieces of that fix that don't require a
// real timer or network: _wsIsStale (the pure staleness decision — the
// interval callback that calls it is a no-op in this stub environment,
// see testutil_browser_stub.js) and _wsSendPing (the readyState guard).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  // The stub's WebSocket is a bare constructor with no static readyState
  // constants (testutil_browser_stub.js) -- real values per the WHATWG spec,
  // needed here since _wsSendPing's guard compares against WebSocket.OPEN.
  sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CLOSING = 2;
  sandbox.WebSocket.CLOSED = 3;
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._wsIsStale, 'function', '_wsIsStale is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._wsSendPing, 'function', '_wsSendPing is not defined -- did it get renamed?');
  return sandbox;
}

test('_wsIsStale: fresh activity is not stale', () => {
  const sandbox = loadAppJS();
  const now = 1000000;
  const isStale = vm.runInContext('_wsIsStale(999998, 1000000)', sandbox); // 2ms ago
  assert.equal(isStale, false);
});

test('_wsIsStale: activity exactly at the threshold is not yet stale', () => {
  const sandbox = loadAppJS();
  // WS_STALE_MS is 50000 in app.js; exactly at the boundary must not trip
  // (the check is a strict >, matching "more than two ping cycles of
  // silence", not "at least two").
  const isStale = vm.runInContext('_wsIsStale(1000000 - 50000, 1000000)', sandbox);
  assert.equal(isStale, false);
});

test('_wsIsStale: activity older than the threshold is stale', () => {
  const sandbox = loadAppJS();
  const isStale = vm.runInContext('_wsIsStale(1000000 - 50001, 1000000)', sandbox);
  assert.equal(isStale, true);
});

test('_wsIsStale: a long-dead connection (minutes of silence) is stale', () => {
  const sandbox = loadAppJS();
  // The exact scenario reported: idle for "a few minutes".
  const threeMinutesAgo = 1000000 - 3 * 60 * 1000;
  const isStale = vm.runInContext(`_wsIsStale(${threeMinutesAgo}, 1000000)`, sandbox);
  assert.equal(isStale, true);
});

test('_wsSendPing: sends a ping frame when the socket is OPEN', () => {
  const sandbox = loadAppJS();
  let sent = null;
  vm.runInContext(
    `state.ws = { readyState: WebSocket.OPEN, send(msg) { __sent = msg; } };`,
    sandbox,
  );
  sandbox.__sent = null;
  vm.runInContext('_wsSendPing()', sandbox);
  sent = sandbox.__sent;
  assert.ok(sent, 'expected ws.send to be called');
  assert.deepEqual(JSON.parse(sent), { type: 'ping' });
});

test('_wsSendPing: does nothing when the socket is not OPEN (e.g. CONNECTING or null)', () => {
  const sandbox = loadAppJS();
  vm.runInContext(
    `state.ws = { readyState: WebSocket.CONNECTING, send() { __called = true; } }; __called = false;`,
    sandbox,
  );
  vm.runInContext('_wsSendPing()', sandbox);
  assert.equal(vm.runInContext('__called', sandbox), false);

  vm.runInContext('state.ws = null; __called2 = false;', sandbox);
  // Must not throw on a null socket.
  vm.runInContext('_wsSendPing()', sandbox);
  assert.equal(vm.runInContext('__called2', sandbox), false);
});
