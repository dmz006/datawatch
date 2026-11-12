// Reconnect backoff + connection-stability window (docs/plans/2026-10-21-pwa-connection-stability.md).
// Tests the pure helpers _wsBackoffDelay and _wsStableEnough, plus the
// flapping-socket behaviour they imply for the attempt counter.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CLOSING = 2;
  sandbox.WebSocket.CLOSED = 3;
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._wsBackoffDelay, 'function', '_wsBackoffDelay is not defined');
  assert.equal(typeof sandbox._wsStableEnough, 'function', '_wsStableEnough is not defined');
  return sandbox;
}

const delay = (sb, attempt, rand) => vm.runInContext(`_wsBackoffDelay(${attempt}, ${rand})`, sb);
const stable = (sb, openedAt, now) => vm.runInContext(`_wsStableEnough(${openedAt}, ${now})`, sb);

test('_wsBackoffDelay never exceeds the 30s cap', () => {
  const sb = loadAppJS();
  for (let a = 0; a < 40; a++) {
    assert.ok(delay(sb, a, 1) <= 30000, `attempt ${a}`);
    assert.ok(delay(sb, a, 0.999999) <= 30000, `attempt ${a}`);
  }
  assert.equal(delay(sb, 40, 1), 30000);
});

test('_wsBackoffDelay ceiling grows with attempt', () => {
  const sb = loadAppJS();
  assert.equal(delay(sb, 0, 1), 1000);
  assert.equal(delay(sb, 1, 1), 2000);
  assert.equal(delay(sb, 2, 1), 4000);
  assert.equal(delay(sb, 4, 1), 16000);
  assert.equal(delay(sb, 5, 1), 30000);
  assert.ok(delay(sb, 3, 1) > delay(sb, 2, 1));
});

test('_wsBackoffDelay never falls below the 250ms floor', () => {
  const sb = loadAppJS();
  for (let a = 0; a < 10; a++) {
    assert.equal(delay(sb, a, 0), 250);
    assert.ok(delay(sb, a, 0.01) >= 250);
  }
});

test('_wsBackoffDelay uses the supplied rand', () => {
  const sb = loadAppJS();
  assert.equal(delay(sb, 3, 0.5), 4000);
  assert.equal(delay(sb, 3, 0.25), 2000);
  assert.notEqual(delay(sb, 3, 0.3), delay(sb, 3, 0.9));
});

test('_wsStableEnough is false before 15s and true after', () => {
  const sb = loadAppJS();
  assert.equal(stable(sb, 1000000, 1000000), false);
  assert.equal(stable(sb, 1000000, 1000000 + 14999), false);
  assert.equal(stable(sb, 1000000, 1000000 + 15000), true);
  assert.equal(stable(sb, 1000000, 1000000 + 60000), true);
  assert.equal(stable(sb, 0, 1000000), false, 'no open recorded');
});

test('flapping opens shorter than the window keep the attempt counter growing', () => {
  const sb = loadAppJS();
  // Mirrors the open/close handler logic: reset only if stable at close.
  const result = vm.runInContext(`
    (function () {
      let attempt = 0, openedAt = 0, now = 5000000;
      const seen = [];
      for (let i = 0; i < 6; i++) {
        openedAt = now;            // socket opens
        now += 3000;               // dies after 3s (< WS_STABLE_MS)
        if (_wsStableEnough(openedAt, now)) attempt = 0;
        attempt++;                 // scheduleReconnect
        seen.push(attempt);
        now += 1000;
      }
      // Now a socket that stays up long enough resets the counter.
      openedAt = now; now += 16000;
      if (_wsStableEnough(openedAt, now)) attempt = 0;
      seen.push(attempt);
      return seen;
    })()`, sb);
  assert.deepEqual(Array.from(result), [1, 2, 3, 4, 5, 6, 0]);
});
