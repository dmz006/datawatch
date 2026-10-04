#!/usr/bin/env node
// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) — regression test for the client-side prototype-pollution fix in
// app.js's handleMessage 'hook_update' case.
//
// There is no existing JS test framework for this PWA (no package.json,
// no bundler, app.js is a single classic <script> file with no module
// wrapper) — this is a plain Node script using only built-ins (vm, fs),
// runnable directly: `node app_security_test.js` (exit 0 = pass).
//
// It loads the REAL, unmodified app.js (not an extracted fragment) into
// a Node vm context with a minimal stubbed browser environment (document/
// window/localStorage/etc. — just enough that the file's top-level code
// runs to completion without throwing, so every real top-level `const`/
// `let` — including `state` and `_dash` — actually initializes, same as
// in a real browser). It then calls the real, hoisted `handleMessage`
// function with a crafted WS message and inspects whether Object.prototype
// got polluted in that same realm.
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
// This test was validated against the actual bug before being committed:
// temporarily reverting the fix and rerunning it reproduces
// POLLUTION_DETECTED: true, confirming the test would have caught this.

const vm = require('vm');
const fs = require('fs');
const path = require('path');

function makeStubElement() {
  const el = {
    style: {},
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    dataset: {}, children: [], childNodes: [],
    getContext() { return makeStubCtx(); },
    appendChild() {}, removeChild() {}, remove() {},
    addEventListener() {}, removeEventListener() {},
    setAttribute() {}, getAttribute() { return null; }, removeAttribute() {},
    querySelector() { return null; }, querySelectorAll() { return []; },
    focus() {}, blur() {}, click() {},
    scrollIntoView() {},
    getBoundingClientRect() { return { width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0 }; },
  };
  Object.defineProperty(el, 'innerHTML', { get() { return ''; }, set() {} });
  Object.defineProperty(el, 'textContent', { get() { return ''; }, set() {} });
  Object.defineProperty(el, 'value', { get() { return ''; }, set() {} });
  Object.defineProperty(el, 'scrollHeight', { get() { return 0; } });
  Object.defineProperty(el, 'scrollTop', { get() { return 0; }, set() {} });
  Object.defineProperty(el, 'clientHeight', { get() { return 0; } });
  return el;
}
function makeStubCtx() {
  return new Proxy({}, { get(_t, _p) { return function () { return makeStubCtx(); }; } });
}

function buildSandbox() {
  const sandbox = {};
  sandbox.window = sandbox;
  sandbox.self = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.console = console;
  sandbox.setTimeout = () => 0; // no-op: never actually schedule anything
  sandbox.clearTimeout = () => {};
  sandbox.setInterval = () => 0;
  sandbox.clearInterval = () => {};
  sandbox.requestAnimationFrame = () => 0;
  sandbox.cancelAnimationFrame = () => {};
  // Deliberately NOT copying Object/Array/Math/JSON/etc. from the host --
  // vm.createContext() already provisions a full, independent set of
  // native intrinsics for the new realm. Injecting host references under
  // the same names would shadow the realm's own Object/Array for any
  // code that refers to them by name, while object/array *literals*
  // ({}/[]) created by code running in the context still bind to the
  // realm's real native intrinsics regardless -- the two would silently
  // diverge, exactly the kind of mismatch this test exists to avoid.
  sandbox.encodeURIComponent = encodeURIComponent;
  sandbox.decodeURIComponent = decodeURIComponent;
  sandbox.fetch = () => Promise.reject(new Error('stub fetch'));
  sandbox.WebSocket = function () { return { close() {}, send() {} }; };
  sandbox.navigator = { userAgent: 'node-test', clipboard: { writeText() { return Promise.resolve(); } } };
  sandbox.localStorage = (() => {
    const m = new Map();
    return {
      getItem(k) { return m.has(k) ? m.get(k) : null; },
      setItem(k, v) { m.set(k, String(v)); },
      removeItem(k) { m.delete(k); },
    };
  })();
  sandbox.location = { hash: '', href: 'http://localhost/', pathname: '/' };
  sandbox.history = { pushState() {}, replaceState() {} };
  sandbox.document = {
    getElementById() { return makeStubElement(); },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    createElement() { return makeStubElement(); },
    addEventListener() {},
    removeEventListener() {},
    body: makeStubElement(),
    documentElement: makeStubElement(),
    head: makeStubElement(),
    visibilityState: 'visible',
    cookie: '',
  };
  sandbox.addEventListener = function () {};
  sandbox.removeEventListener = function () {};
  sandbox.Notification = { permission: 'default', requestPermission() { return Promise.resolve('default'); } };
  sandbox.MutationObserver = function () { return { observe() {}, disconnect() {}, takeRecords() { return []; } }; };
  sandbox.ResizeObserver = function () { return { observe() {}, disconnect() {}, unobserve() {} }; };
  sandbox.IntersectionObserver = function () { return { observe() {}, disconnect() {}, unobserve() {} }; };
  return sandbox;
}

function loadAppJS(appJsPath) {
  const src = fs.readFileSync(appJsPath, 'utf8');
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  // Loading the real file must not throw -- if it does, state/_dash (both
  // top-level `const`, not globalThis properties, but still reachable via
  // further vm.runInContext calls against this same sandbox) may never
  // initialize, and the rest of this test becomes meaningless.
  new vm.Script(src, { filename: appJsPath }).runInContext(sandbox);
  if (typeof sandbox.handleMessage !== 'function') {
    throw new Error('app.js loaded but handleMessage is not defined -- stub environment or app.js itself changed shape');
  }
  return sandbox;
}

function main() {
  const appJsPath = process.argv[2] || path.join(__dirname, 'app.js');
  const sandbox = loadAppJS(appJsPath);

  vm.runInContext(
    "state.activeView = 'dashboard'; _dash.nodes['legit-session-123'] = { hookHealth:'', state:'', threats:0, color:'' };",
    sandbox
  );

  // The attack: a crafted WS message whose session_id is "__proto__".
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: '__proto__', board: { hook_health: 'POLLUTED', state: 'running', telemetry: null } },
  });

  const pollutedViaFreshObject = vm.runInContext('({}).hookHealth', sandbox);
  const pollutedPrototypeDirectly = vm.runInContext('Object.prototype.hookHealth', sandbox);
  const boardsPrototypeUnchanged = vm.runInContext('Object.getPrototypeOf(_dash._boards) === Object.prototype', sandbox);

  // The regression check: a normal session_id must still update its own
  // node correctly -- the fix must not break legitimate hook_update
  // handling.
  sandbox.handleMessage({
    type: 'hook_update',
    data: { session_id: 'legit-session-123', board: { hook_health: 'alive', state: 'running', telemetry: null } },
  });
  const legitHookHealth = vm.runInContext("_dash.nodes['legit-session-123'].hookHealth", sandbox);

  const failures = [];
  if (pollutedViaFreshObject === 'POLLUTED') {
    failures.push('a freshly-created {} object inherited "hookHealth" from Object.prototype -- pollution occurred');
  }
  if (pollutedPrototypeDirectly === 'POLLUTED') {
    failures.push('Object.prototype.hookHealth was set directly -- pollution occurred');
  }
  if (!boardsPrototypeUnchanged) {
    failures.push('_dash._boards was reparented (its own prototype changed) via the __proto__ setter');
  }
  if (legitHookHealth !== 'alive') {
    failures.push(`legitimate session update did not apply: _dash.nodes['legit-session-123'].hookHealth = ${JSON.stringify(legitHookHealth)}, want 'alive'`);
  }

  if (failures.length) {
    console.error('FAIL');
    for (const f of failures) console.error('  - ' + f);
    process.exitCode = 1;
    return;
  }
  console.log('PASS: no prototype pollution via hook_update session_id, legitimate updates still work');
}

main();
