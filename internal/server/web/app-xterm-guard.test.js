// Regression tests for the xterm terminal guards in app.js.
//
// Operator-reported failure this guards: xterm.js's fitAddon.fit() (and
// scrollToBottom/resize/write/...) throws `TypeError: Cannot read properties
// of undefined (reading 'dimensions')` once the terminal has been disposed
// — which happens on session switch / destroyXterm while a scheduled
// fit/scroll job (rAF / debounced setTimeout) is still pending. Without the
// _termUsable/_termSafeFit/_termSchedule-gen/destroyXterm-cancel guards
// those stale timers crash the timer chain or fire the outgoing terminal.
//
// Like app-pane-capture.test.js / app-ws-watchdog.test.js, this loads the
// REAL, unmodified app.js into a Node vm context (testutil_browser_stub.js)
// and drives its real top-level `state` and hoisted functions via
// vm.runInContext against the same sandbox. The stub's no-op timers are
// overridden with recording versions so tests can see which timer ids were
// pending, that destroyXterm cleared them, and fire stale callbacks by hand.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// Exactly the TypeError a real disposed xterm throws from fit()/
// scrollToBottom()/resize()/... (render service torn down -> undefined
// `dimensions` dereference).
const DIM_ERR = "Cannot read properties of undefined (reading 'dimensions')";

// ── Fake xterm pieces (host-realm, so tests inspect counters directly) ──

// Stub container element: settable size + connectivity, like the test asks.
function makeContainer() {
  return {
    style: {},
    isConnected: true,
    clientWidth: 480,
    clientHeight: 320,
    offsetWidth: 480,
    offsetHeight: 320,
    innerHTML: '',
    querySelector() { return null; },
    appendChild() {},
    removeChild() {},
    getBoundingClientRect() { return { top: 0, left: 0, width: 480, height: 320 }; },
  };
}

const terminalInstances = [];
const addonInstances = [];

// FakeTerminal: every method app.js calls throws the real xterm dimensions
// TypeError once disposed() has been called; otherwise records calls.
class FakeTerminal {
  constructor(opts = {}) {
    this.disposed = false;
    this.disposeCalls = 0;
    this.element = {};
    this.cols = 80;
    this.rows = 24;
    this.options = Object.assign({}, opts);
    this.container = null;
    this.onDataCb = null;
    this.onScrollCb = null;
    this.resizeCalls = 0;
    this.scrollToBottomCalls = 0;
    terminalInstances.push(this);
  }
  _throwIfDisposed() {
    if (this.disposed) throw new TypeError(DIM_ERR);
  }
  dispose() { this.disposeCalls++; this.disposed = true; }
  open(container) { this._throwIfDisposed(); this.container = container; }
  loadAddon(addon) { this._throwIfDisposed(); addon.term = this; }
  resize() { this._throwIfDisposed(); this.resizeCalls++; }
  scrollToBottom() { this._throwIfDisposed(); this.scrollToBottomCalls++; }
  write() { this._throwIfDisposed(); this.writeCalls = (this.writeCalls || 0) + 1; }
  reset() { this._throwIfDisposed(); this.resetCalls = (this.resetCalls || 0) + 1; }
  onData(cb) { this._throwIfDisposed(); this.onDataCb = cb; }
  onScroll(cb) { this._throwIfDisposed(); this.onScrollCb = cb; }
}

// FakeFitAddon: fit() throws the dimensions TypeError when its terminal is
// disposed OR when the container it was opened into is zero-sized (the same
// conditions under which a real xterm fit() hits the undefined dimensions
// dereference).
class FakeFitAddon {
  constructor() {
    this.term = null; // set by FakeTerminal.loadAddon (mimics xterm)
    this.fitCalls = 0;
    addonInstances.push(this);
  }
  fit() {
    const t = this.term;
    const c = t && t.container;
    if (!t || t.disposed || !c || c.clientWidth === 0 || c.clientHeight === 0) {
      throw new TypeError(DIM_ERR);
    }
    this.fitCalls++;
  }
}

// ── Recording timers: same interface as the stub's, but the test sees ids ──

function installRecordingTimers(sandbox) {
  const rec = { nextId: 1, ids: [], cleared: new Set(), fns: new Map() };
  const schedule = (fn) => {
    const id = rec.nextId++;
    rec.ids.push(id);
    rec.fns.set(id, fn); // kept even after "clear" so stale callbacks can fire by hand
    return id;
  };
  const clear = (id) => { rec.cleared.add(id); };
  sandbox.setTimeout = schedule;
  sandbox.clearTimeout = clear;
  sandbox.requestAnimationFrame = schedule;
  sandbox.cancelAnimationFrame = clear;
  return rec;
}

function setup(opts = {}) {
  const container = opts.container || makeContainer();
  const sandbox = buildSandbox({ byId: { outputAreaTmux: container } });
  const rec = installRecordingTimers(sandbox);
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.__container = container;
  const run = (code) => vm.runInContext(code, sandbox);
  return { sandbox, rec, container, run };
}

// Put a terminal in state, then run the app's REAL destroyXterm on it (which
// disposes it, adds it to the _disposedTerms WeakSet, and nulls state), then
// point state back at the now-disposed fake — the exact state a stale caller
// can observe.
function disposeInState({ sandbox, run }, term, addon) {
  sandbox.__term = term;
  sandbox.__addon = addon;
  run(`
    state.terminal = __term;
    state.termFitAddon = __addon;
    destroyXterm();
    state.terminal = __term;      // disposed fake left in state
    state.termFitAddon = __addon;
  `);
  assert.equal(term.disposed, true, 'destroyXterm should have disposed the terminal');
}

test('_termUsable: false for null, disposed, element-less, detached and zero-sized; true for a healthy open term', () => {
  const { sandbox, run } = setup();
  const healthy = makeContainer();
  const detached = makeContainer(); detached.isConnected = false;
  const zero = makeContainer(); zero.clientWidth = 0; zero.clientHeight = 0;
  sandbox.__hc = healthy; sandbox.__detached = detached; sandbox.__zero = zero;

  // Null terminal
  assert.equal(run('_termUsable(null, __hc)'), false);

  // Disposed terminal (destroyed through the app's own path)
  const term = new FakeTerminal();
  sandbox.__term = term;
  sandbox.__addon = new FakeFitAddon();
  disposeInState({ sandbox, run }, term, sandbox.__addon);
  assert.equal(run('_termUsable(__term, __hc)'), false, 'disposed term must not be usable');

  // Terminal with no .element yet (constructed but never open()ed)
  sandbox.__noel = { disposed: false, element: null, cols: 80, rows: 24, options: {} };
  run('state.terminal = __noel;');
  assert.equal(run('_termUsable(__noel, __hc)'), false, 'element-less term must not be usable');

  // Healthy term in a detached container
  const ok = new FakeTerminal();
  sandbox.__ok = ok;
  run('state.terminal = __ok;');
  assert.equal(run('_termUsable(__ok, __detached)'), false, 'detached container must defeat usability');

  // Healthy term in a zero-sized container
  assert.equal(run('_termUsable(__ok, __zero)'), false, 'zero-sized container must defeat usability');

  // Healthy open term in a visible container
  ok.open(healthy);
  assert.equal(run('_termUsable(__ok, __hc)'), true, 'healthy open term in visible container must be usable');
});

test('_termSafeFit: zero-sized container -> no fit call and no throw; healthy -> fit called exactly once', () => {
  const { sandbox, run } = setup();
  const term = new FakeTerminal();
  const addon = new FakeFitAddon();
  term.loadAddon(addon);
  term.open(makeContainer());
  sandbox.__term = term; sandbox.__addon = addon;
  run('state.terminal = __term;');

  // Zero-sized container: _termUsable must defeat the fit before it happens.
  // (Pre-fix direct fitAddon.fit() here would throw the dimensions TypeError.)
  const zero = makeContainer(); zero.clientWidth = 0; zero.clientHeight = 0;
  sandbox.__zero = zero;
  assert.doesNotThrow(() => run('_termSafeFit(__term, __addon, __zero)'),
    'a zero-sized container must not let a fit slip through');
  assert.equal(run('_termSafeFit(__term, __addon, __zero)'), false);
  assert.equal(addon.fitCalls, 0, 'fit() must NOT be called for a zero-sized container');

  // Healthy container: fit() called exactly once, returns true.
  const healthy = makeContainer();
  sandbox.__hc = healthy;
  assert.equal(run('_termSafeFit(__term, __addon, __hc)'), true);
  assert.equal(addon.fitCalls, 1, 'fit() must be called exactly once when healthy');
});

test('changeTermFontSize and termFitToWidth: disposed state.terminal -> no throw, no fit, no infinite loop', () => {
  const { sandbox, run } = setup();
  const term = new FakeTerminal();
  const addon = new FakeFitAddon();
  term.loadAddon(addon);
  term.open(makeContainer());
  disposeInState({ sandbox, run }, term, addon);

  // Both public entry points must survive a disposed terminal sitting in
  // state. Pre-fix they called fit() (or looped calling it), throwing the
  // dimensions TypeError from the disposed fake.
  assert.doesNotThrow(() => run('changeTermFontSize(1)'),
    'changeTermFontSize must not throw after dispose');
  assert.doesNotThrow(() => run('termFitToWidth()'),
    'termFitToWidth must not throw (and must terminate) after dispose');
  assert.equal(addon.fitCalls, 0, 'no fit() must be attempted on a disposed addon');
});

test('_paneJumpToLive: disposed state.terminal -> no throw, no scrollToBottom on the dead terminal', () => {
  const { sandbox, run } = setup();
  const term = new FakeTerminal();
  const addon = new FakeFitAddon();
  term.loadAddon(addon);
  term.open(makeContainer());
  disposeInState({ sandbox, run }, term, addon);

  // Pre-fix: scrollToBottom() on the disposed terminal threw the dimensions
  // TypeError into the pane-capture path. Post-fix: recognized as torn down.
  assert.doesNotThrow(() => run('_paneJumpToLive()'),
    '_paneJumpToLive must not throw for a disposed terminal');
  assert.equal(term.scrollToBottomCalls, 0,
    'scrollToBottom must not be called on a disposed terminal');
});

test('destroyXterm: cancels every pending _termSchedule job; firing stale callbacks does nothing', () => {
  const { sandbox, rec, run } = setup();
  const term = new FakeTerminal();
  const addon = new FakeFitAddon();
  term.loadAddon(addon);
  term.open(makeContainer());
  sandbox.__term = term; sandbox.__addon = addon;
  run('state.terminal = __term; state.termFitAddon = __addon;');

  // Schedule jobs through the app's real _termSchedule (rAF and ms forms).
  const ids = run(`
    __jobFired = 0;
    const a = _termSchedule(() => { try { __addon.fit(); } catch (e) {} __jobFired++; }, 10);
    const b = _termSchedule(() => { try { __addon.fit(); } catch (e) {} __jobFired++; });
    const c = _termSchedule(() => { try { __addon.fit(); } catch (e) {} __jobFired++; }, 50);
    [a, b, c];
  `);
  assert.equal(ids.length, 3, 'three jobs should be recorded');

  run('destroyXterm();');

  // Every recorded timer id must have been cleared by both clear mechanisms.
  for (const id of ids) {
    assert.ok(rec.cleared.has(id), `timer id ${id} must have been cleared by destroyXterm`);
  }

  // Firing the stale callbacks by hand (as if the timer had beaten the
  // cancel) must throw nothing and must never run the job (no fit attempt).
  for (const id of ids) {
    assert.doesNotThrow(() => rec.fns.get(id)(), `firing stale timer ${id} must not throw`);
  }
  assert.equal(run('__jobFired'), 0, 'a stale scheduled job must never execute after destroyXterm');
  assert.equal(addon.fitCalls, 0, 'no fit() may be attempted on the disposed terminal');
});

test('rapid session switch: double initXterm leaves first terminal untouched and its timers inert', () => {
  const { sandbox, rec, run, container } = setup();
  // Stub the global constructors app.js's initXterm needs.
  sandbox.Terminal = FakeTerminal;
  sandbox.FitAddon = { FitAddon: FakeFitAddon };

  const instBefore = terminalInstances.length;
  const addonBefore = addonInstances.length;
  run('initXterm("sess-1", null, 0, 0);');
  const firstInitIds = rec.ids.slice(-1); // fit job scheduled by the first init
  assert.ok(firstInitIds.length >= 1, 'first init should schedule at least one fit job');
  const term1 = terminalInstances[instBefore];
  const addon1 = addonInstances[addonBefore];
  assert.ok(term1 && addon1, 'first init should have created a terminal + fit addon');

  // Second session in a row (operator rapid-switch / reload).
  run('initXterm("sess-2", null, 0, 0);');
  const term2 = terminalInstances[instBefore + 1];
  assert.equal(term1.disposed, true, 'second init must dispose the first terminal');
  sandbox.__term2 = term2;
  assert.equal(run('state.terminal === __term2'), true, 'state must point at the second terminal');

  // Every timer the first init scheduled must have been cancelled.
  for (const id of firstInitIds) {
    assert.ok(rec.cleared.has(id), `first-init timer id ${id} must have been cleared by the second init`);
  }

  // Firing every first-init timer by hand (a lost race against the cancel)
  // must throw nothing and never touch the first terminal or its addon.
  for (const id of firstInitIds) {
    assert.doesNotThrow(() => rec.fns.get(id)(), `firing first-init timer ${id} must not throw`);
  }
  assert.equal(addon1.fitCalls, 0, 'first addon must never be fit()ed after the switch');
  assert.equal(term1.resizeCalls, 0, 'first terminal must never be resized after the switch');
  assert.equal(term1.scrollToBottomCalls, 0, 'first terminal must never scroll after the switch');
  // The first terminal's registered onData path must not deliver input either.
  assert.doesNotThrow(() => term1.onDataCb && term1.onDataCb('x'));
  assert.equal(addon1.fitCalls, 0, 'first-addon fit() calls still zero after firing stale handlers');
});

test('toggleFullscreen: disposed state.termFitAddon -> no throw, no fit on the dead addon', () => {
  const { sandbox, run } = setup();
  const term = new FakeTerminal();
  const addon = new FakeFitAddon();
  term.loadAddon(addon);
  term.open(makeContainer());
  disposeInState({ sandbox, run }, term, addon);

  // Pre-fix: toggleFullscreen called fitAddon.fit() directly — a disposed
  // fake throws the dimensions TypeError right out of the fullscreen button.
  assert.doesNotThrow(() => run('toggleFullscreen()'),
    'toggleFullscreen must not throw for a disposed terminal/addon');
  assert.equal(addon.fitCalls, 0, 'no fit() may be attempted on the disposed addon');
});
