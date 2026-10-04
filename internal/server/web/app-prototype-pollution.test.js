// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) — regression test for the client-side prototype-pollution fix in
// app.js's handleMessage 'hook_update' case. Uses Node's built-in test
// runner (node:test, zero dependencies) — there is no bundler/framework
// for this PWA otherwise. Shared stub-browser-environment helpers live in
// testutil_browser_stub.js. Run: `node --test internal/server/web/*.test.js`.
//
// What was wrong (fixed in this same commit): `_dash.nodes[hSid]` reads a
// plain {} object by an attacker-influenced key. On a plain object,
// `obj["__proto__"]` doesn't return undefined for a missing key the way
// every other key does — it returns the shared Object.prototype via the
// inherited accessor. The code then wrote ordinary properties (n.hookHealth
// = ..., etc.) onto whatever `n` was, which — for hSid === "__proto__" —
// meant writing directly onto Object.prototype, polluting it for every
// plain object on the page. Fixed by rejecting hSid === '__proto__' /
// 'constructor' / 'prototype' before it's ever used as a key.
//
// Validated against the actual bug before being committed: temporarily
// reverting the fix and rerunning this file reproduces a failure on the
// "does not pollute Object.prototype" case, confirming the test would
// have caught it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.handleMessage, 'function',
    'app.js loaded but handleMessage is not defined -- stub environment or app.js itself changed shape');
  vm.runInContext(
    "state.activeView = 'dashboard'; _dash.nodes['legit-session-123'] = { hookHealth:'', state:'', threats:0, color:'' };",
    sandbox
  );
  return sandbox;
}

test('hook_update with session_id="__proto__" does not pollute Object.prototype', () => {
  const sandbox = loadAppJS();
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: '__proto__', board: { hook_health: 'POLLUTED', state: 'running', telemetry: null } },
  });
  assert.notEqual(vm.runInContext('({}).hookHealth', sandbox), 'POLLUTED',
    'a freshly-created {} object inherited "hookHealth" from Object.prototype');
  assert.notEqual(vm.runInContext('Object.prototype.hookHealth', sandbox), 'POLLUTED',
    'Object.prototype.hookHealth was set directly');
});

test('hook_update with session_id="__proto__" does not reparent _dash._boards', () => {
  const sandbox = loadAppJS();
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: '__proto__', board: { hook_health: 'POLLUTED', state: 'running', telemetry: null } },
  });
  assert.equal(
    vm.runInContext('Object.getPrototypeOf(_dash._boards) === Object.prototype', sandbox),
    true,
    '_dash._boards was reparented via the __proto__ setter'
  );
});

test('hook_update with session_id="constructor" is also rejected', () => {
  const sandbox = loadAppJS();
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: 'constructor', board: { hook_health: 'POLLUTED2', state: 'running', telemetry: null } },
  });
  assert.notEqual(vm.runInContext('({}).hookHealth', sandbox), 'POLLUTED2');
});

test('a legitimate session_id still updates its own node (regression check)', () => {
  const sandbox = loadAppJS();
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: 'legit-session-123', board: { hook_health: 'alive', state: 'running', telemetry: null } },
  });
  assert.equal(vm.runInContext("_dash.nodes['legit-session-123'].hookHealth", sandbox), 'alive');
});
