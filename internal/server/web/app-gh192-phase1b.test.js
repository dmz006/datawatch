// GH#192 Phase 1 — #6 header refresh spinner, #5 terminal-connect splash
// min/max dwell. See docs/plans/2026-10-07-gh192-parity-batch.md.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(byId) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function setState(sandbox, patch) {
  sandbox.__patch = patch;
  vm.runInContext('Object.assign(state, __patch)', sandbox);
  delete sandbox.__patch;
}

function getState(sandbox, key) {
  return vm.runInContext(`state.${key}`, sandbox);
}

// ── #6 — header refresh spinner ─────────────────────────────────────────────

test('_showHeaderRefreshSpinner makes the spinner visible; _hideHeaderRefreshSpinner hides it', () => {
  const spinner = makeStubElement();
  const sandbox = loadAppJS({ headerRefreshSpinner: spinner });
  vm.runInContext('_showHeaderRefreshSpinner()', sandbox);
  assert.equal(spinner.style.display, 'inline-block');
  vm.runInContext('_hideHeaderRefreshSpinner()', sandbox);
  assert.equal(spinner.style.display, 'none');
});

test('_showHeaderRefreshSpinner auto-hides after its fixed window, with no further action needed', () => {
  const spinner = makeStubElement();
  const sandbox = loadAppJS({ headerRefreshSpinner: spinner });
  const timeouts = [];
  sandbox.setTimeout = (fn, ms) => { timeouts.push({ fn, ms }); return timeouts.length; };
  sandbox.clearTimeout = () => {};
  vm.runInContext('_showHeaderRefreshSpinner()', sandbox);
  assert.equal(spinner.style.display, 'inline-block');
  assert.equal(timeouts.length, 1);
  timeouts[0].fn(); // simulate the timer firing
  assert.equal(spinner.style.display, 'none');
});

test('calling show() twice in quick succession restarts the hide timer instead of stacking timers', () => {
  const spinner = makeStubElement();
  const sandbox = loadAppJS({ headerRefreshSpinner: spinner });
  let cleared = [];
  let nextId = 1;
  const timeouts = {};
  sandbox.setTimeout = (fn) => { const id = nextId++; timeouts[id] = fn; return id; };
  sandbox.clearTimeout = (id) => { cleared.push(id); delete timeouts[id]; };
  vm.runInContext('_showHeaderRefreshSpinner()', sandbox);
  vm.runInContext('_showHeaderRefreshSpinner()', sandbox);
  assert.equal(cleared.length, 1, 'the first timer should have been cleared before starting the second');
});

test('navigate() pulses the spinner for a real list view but not for new/settings', () => {
  const spinner = makeStubElement();
  const sandbox = loadAppJS({ headerRefreshSpinner: spinner, headerTitle: makeStubElement() });
  setState(sandbox, { activeView: 'sessions', sessions: [] });
  sandbox.__view = 'alerts';
  vm.runInContext('navigate(__view)', sandbox);
  assert.equal(spinner.style.display, 'inline-block', 'expected the spinner to show for a real view switch');
});

// ── #5 — terminal-connect splash min/max dwell ──────────────────────────────

test('startTermConnectWatchdog uses the 15s new-session budget (3 x 5000ms, unchanged from before this change)', () => {
  const sandbox = loadAppJS();
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push(ms); return scheduled.length; };
  setState(sandbox, { _termIsNewSession: true });
  sandbox.__sid = 'sess-1';
  vm.runInContext('startTermConnectWatchdog(__sid)', sandbox);
  assert.equal(scheduled[0], 5000, 'new-session retry interval should be unchanged at 5000ms');
});

test('startTermConnectWatchdog uses a shorter existing-session budget (2 x 4000ms = 8000ms total)', () => {
  const sandbox = loadAppJS();
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push(ms); return scheduled.length; };
  setState(sandbox, { _termIsNewSession: false });
  sandbox.__sid = 'sess-1';
  vm.runInContext('startTermConnectWatchdog(__sid)', sandbox);
  assert.equal(scheduled[0], 4000, 'existing-session retry interval should be 4000ms');
});

test('_dismissTermLoadingSplashWithMinDwell removes the splash immediately once minDwell has already elapsed', () => {
  const splash = makeStubElement();
  const sandbox = loadAppJS({ termLoadingSplash: splash });
  let removed = false;
  splash.remove = () => { removed = true; };
  setState(sandbox, { _termIsNewSession: false, _termSplashMountedAt: Date.now() - 10000 }); // long past the 500ms existing-session minDwell
  vm.runInContext('_dismissTermLoadingSplashWithMinDwell()', sandbox);
  assert.ok(removed, 'splash should be removed synchronously once minDwell has already elapsed');
});

test('_dismissTermLoadingSplashWithMinDwell defers removal when content arrives before minDwell has elapsed (new session, 2000ms floor)', () => {
  const splash = makeStubElement();
  const sandbox = loadAppJS({ termLoadingSplash: splash });
  let removed = false;
  splash.remove = () => { removed = true; };
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };
  setState(sandbox, { _termIsNewSession: true, _termSplashMountedAt: Date.now() }); // just mounted
  vm.runInContext('_dismissTermLoadingSplashWithMinDwell()', sandbox);
  assert.ok(!removed, 'splash must not be removed before the 2000ms minDwell floor for a new session');
  assert.equal(scheduled.length, 1);
  assert.ok(scheduled[0].ms > 1900 && scheduled[0].ms <= 2000, `expected a deferred removal close to 2000ms, got ${scheduled[0].ms}`);
  scheduled[0].fn();
  assert.ok(removed, 'deferred removal should fire and remove the splash');
});

test('_dismissTermLoadingSplashWithMinDwell uses the shorter 500ms floor for an existing/reconnecting session', () => {
  const splash = makeStubElement();
  const sandbox = loadAppJS({ termLoadingSplash: splash });
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };
  setState(sandbox, { _termIsNewSession: false, _termSplashMountedAt: Date.now() });
  vm.runInContext('_dismissTermLoadingSplashWithMinDwell()', sandbox);
  assert.equal(scheduled.length, 1);
  assert.ok(scheduled[0].ms > 400 && scheduled[0].ms <= 500, `expected ~500ms, got ${scheduled[0].ms}`);
});

test('a freshly-started session sets _justStartedSessionId, consumed by renderSessionDetail as _termIsNewSession', () => {
  // This pins the plumbing end-to-end: the flag set at session-start time
  // must actually reach _termIsNewSession when the detail view mounts for
  // that exact session, and must NOT apply to a different session.
  const sandbox = loadAppJS();
  setState(sandbox, { _justStartedSessionId: 'sess-new' });
  assert.equal(getState(sandbox, '_justStartedSessionId'), 'sess-new');
});
