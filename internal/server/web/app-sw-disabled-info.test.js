// Operator-reported: the desktop browser (self-signed TLS cert, click-through
// origin) showed an error-looking console line on page load. Root cause is that
// on an origin that is NOT a valid SW-registration context Chrome rejects the
// /sw.js fetch with a SecurityError, and our .catch was logging it in a way
// that reads like a failure. The fix (app.js, `function registerServiceWorker`
// banner "Service Worker") demotes exactly that case to a single quiet info
// line pointing at docs/operations.md and keeps console.warn for genuine
// failures.
//
// This file tests the two pieces of that fix that don't need a browser:
//   1. _swRegisterFailureMessage — the pure helper that decides "is this the
//      untrusted-cert / insecure-context case?" and returns the info string or
//      null.
//   2. registerServiceWorker's .catch behavior — driven in the same sandbox
//      with a stub navigator.serviceWorker.register that rejects, so we can
//      assert the exact console call shape (one info, zero warn/error, and no
//      re-print on a second call; a genuine failure keeps console.warn).
// We load the REAL app.js (same testutil_browser_stub.js approach as
// app-ws-watchdog.test.js) and capture console by swapping sandbox.console for
// recording functions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  // A self-secure-context by default (the insecure-context branch of the
  // helper is exercised separately, explicitly, when we need it).
  sandbox.isSecureContext = true;
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._swRegisterFailureMessage, 'function',
    '_swRegisterFailureMessage is not defined -- did it get renamed?');
  return sandbox;
}

// Swap in a recording console; returns the recorder so tests can inspect it.
function installConsole(sandbox) {
  const rec = { info: [], warn: [], error: [], log: [] };
  sandbox.console = {
    info: (...a) => rec.info.push(a),
    warn: (...a) => rec.warn.push(a),
    error: (...a) => rec.error.push(a),
    log: (...a) => rec.log.push(a),
  };
  return rec;
}

// Chrome's rejection of /sw.js on a self-signed-cert origin.
const CHROME_MSG = 'An SSL certificate error occurred when fetching the script';

test('helper: Chrome-style SecurityError returns the info string', () => {
  const sandbox = loadAppJS();
  const info = vm.runInContext(
    `((e) => _swRegisterFailureMessage(e))(
      { name: 'SecurityError', message: 'An SSL certificate error occurred when fetching the script' }
    )`,
    sandbox,
  );
  assert.equal(typeof info, 'string');
  assert.match(info, /docs\/operations\.md/);
  // One line, plain, and none of the error-looking words.
  assert.doesNotMatch(info, /\n/);
  assert.doesNotMatch(info.toLowerCase(), /error|failed|warning/);
});

test('helper: Firefox-style insecure-operation SecurityError returns the info string', () => {
  const sandbox = loadAppJS();
  const info = vm.runInContext(
    `((e) => _swRegisterFailureMessage(e))(
      { name: 'SecurityError', message: 'Insecure operation — not allowed in this context' }
    )`,
    sandbox,
  );
  assert.equal(typeof info, 'string');
  assert.match(info, /docs\/operations\.md/);
});

test('helper: unrelated TypeError returns null', () => {
  const sandbox = loadAppJS();
  const out = vm.runInContext(
    `((e) => _swRegisterFailureMessage(e))(
      { name: 'TypeError', message: "cannot read properties of undefined" }
    )`,
    sandbox,
  );
  assert.equal(out, null);
});

test('helper: insecure context (isSecureContext === false) returns the info string even with a generic err', () => {
  const sandbox = loadAppJS();
  sandbox.isSecureContext = false;
  const info = vm.runInContext(
    `(e => _swRegisterFailureMessage(e))({ name: 'TypeError', message: 'x' })`,
    sandbox,
  );
  assert.equal(typeof info, 'string');
  assert.match(info, /docs\/operations\.md/);
});

test('registerServiceWorker: trusted-cert failure -> exactly one console.info, no warn/error, no re-print', async () => {
  const sandbox = loadAppJS();
  const rec = installConsole(sandbox);
  sandbox.__swErr = { name: 'SecurityError', message: CHROME_MSG };
  sandbox.navigator.serviceWorker = {
    controller: null,
    addEventListener: () => {},
    register: () => Promise.reject(sandbox.__swErr),
  };
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.equal(rec.info.length, 1, 'expected exactly one info line, got ' + JSON.stringify(rec.info));
  assert.equal(rec.warn.length, 0, 'expected zero warn calls');
  assert.equal(rec.error.length, 0, 'expected zero error calls');

  // A second call must not print again (module-level once-per-load flag).
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.equal(rec.info.length, 1, 'info line must be printed at most once per page load');
  assert.equal(rec.warn.length, 0);
  assert.equal(rec.error.length, 0);
});

test('registerServiceWorker: unrelated failure keeps console.warn (once) and no info', async () => {
  const sandbox = loadAppJS();
  const rec = installConsole(sandbox);
  sandbox.__swErr2 = { name: 'TypeError', message: 'sw.register is not a function' };
  sandbox.navigator.serviceWorker = {
    controller: null,
    addEventListener: () => {},
    register: () => Promise.reject(sandbox.__swErr2),
  };
  vm.runInContext('registerServiceWorker();', sandbox);
  await flushAsync(6);
  assert.equal(rec.warn.length, 1, 'expected exactly one warn line');
  assert.equal(rec.info.length, 0, 'expected zero info lines for a genuine failure');
  assert.equal(rec.error.length, 0);
});
