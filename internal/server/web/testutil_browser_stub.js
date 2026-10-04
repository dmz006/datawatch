// Shared stub-browser-environment helpers for this directory's security
// regression tests (app_security_test.js, diagrams_security_test.js,
// app_escaping_test.js). There is no JS test framework for this PWA (no
// package.json, no bundler -- every file here is a classic <script>),
// so these are plain Node scripts using only built-ins (vm, fs). Factored
// out here once two near-identical copies of this existed (BL394
// security review, docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) -- a third test shouldn't triple it again.
//
// Each test loads the REAL, unmodified target file into a Node vm context
// with just enough of a browser shim that the file's real top-level code
// (including its own top-level `const`/`let` declarations -- `state`,
// `_dash`, etc. in app.js) runs to completion without throwing, same as
// in a real browser. Top-level `function` declarations are hoisted and
// end up as real properties on the sandbox object; top-level `const`/
// `let` are NOT globalThis properties, but remain reachable from outside
// via further vm.runInContext(code, sandbox) calls against the SAME
// sandbox object (V8 shares the top-level lexical environment across
// separate Script evaluations run against one context -- the same
// mechanism that lets a REPL's later statement see an earlier one's
// `const`).

const vm = require('vm');
const fs = require('fs');

// makeStubElement returns a minimal DOM Element stand-in. Pass
// onSetInnerHTML to capture what gets written to .innerHTML (used by
// tests that need to inspect rendered output, e.g. diagrams_security_test.js).
function makeStubElement(onSetInnerHTML) {
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
  let html = '';
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; if (onSetInnerHTML) onSetInnerHTML(v); },
  });
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

// buildSandbox returns a fresh global-object stub covering the union of
// what app.js and diagrams.js need at their own top level. A few unused
// stub globals cost nothing; omitting one that IS needed means the
// loaded file throws before its own state initializes, which is the
// failure mode this whole approach exists to avoid.
//
// byId lets a caller pre-register specific getElementById(id) -> element
// stubs (e.g. so it can capture what lands in a specific #main/#list
// element); any id not in byId gets a fresh generic stub element.
function buildSandbox({ initialHash = '', byId = {} } = {}) {
  const sandbox = {};
  sandbox.window = sandbox;
  sandbox.self = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.console = console;
  // No-op timers: never actually schedule anything. These tests only
  // need the loaded file's SYNCHRONOUS top-level code (plus whatever a
  // test explicitly awaits/flushes afterward) to run; a real timer
  // firing later would just keep the Node process alive for no reason.
  sandbox.setTimeout = () => 0;
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
  // diverge, which is exactly the kind of mismatch a security test like
  // this must not have (found and fixed once already, see the prototype-
  // pollution test's own history in BL394 §3h).
  sandbox.encodeURIComponent = encodeURIComponent;
  sandbox.decodeURIComponent = decodeURIComponent;
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 404 });
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
  sandbox.location = { hash: initialHash, href: 'http://localhost/', pathname: '/' };
  sandbox.history = { pushState() {}, replaceState() {} };
  sandbox.document = {
    getElementById(id) { return byId[id] || makeStubElement(); },
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
  // diagrams.js-specific top-level calls -- harmless no-ops for app.js,
  // which never references them.
  sandbox.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
  sandbox.mermaid = { initialize() {}, render() { return Promise.reject(new Error('stub: no mermaid render in tests')); } };
  // marked is left undefined on purpose where it matters (diagrams.js's
  // own `typeof marked !== 'undefined'` guard exercises its plain <pre>
  // fallback, which is what these tests actually care about).
  return sandbox;
}

// loadScript loads jsPath's real source into sandbox (already passed to
// vm.createContext by the caller) and runs its top-level code. Throws if
// the file errors before completing -- callers want this loud: a
// swallowed top-level exception can leave `state`/`_dash`/etc. half-
// initialized, which would make everything downstream silently wrong
// rather than cleanly failing.
function loadScript(jsPath, sandbox) {
  const src = fs.readFileSync(jsPath, 'utf8');
  new vm.Script(src, { filename: jsPath }).runInContext(sandbox);
}

// flushAsync waits long enough for a handful of chained `await`s (e.g. an
// async function awaiting a stubbed fetch()) to settle, without relying
// on real timers (which are stubbed to no-ops above).
function flushAsync(ticks = 10) {
  return new Promise((resolve) => {
    let n = 0;
    const tick = () => { if (++n >= ticks) return resolve(); setImmediate(tick); };
    tick();
  });
}

module.exports = { makeStubElement, makeStubCtx, buildSandbox, loadScript, flushAsync, vm };
