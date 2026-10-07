// BL315 (2026-10-07, operator-reported) — the PWA window-expand toggle
// (_pwaExpanded, toggleFullscreen) must persist across a page reload or
// daemon restart (browser auto-reconnect reloads the page) — state only
// changes when the operator clicks the button, never as a reload side
// effect.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(initialStored, btn) {
  const sandbox = buildSandbox({ byId: btn ? { headerFullscreenBtn: btn } : {} });
  if (initialStored !== undefined) {
    sandbox.localStorage.setItem('cs_pwa_expanded', initialStored);
  }
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.toggleFullscreen, 'function', 'toggleFullscreen is not defined -- did it get renamed?');
  return sandbox;
}

function call(sandbox, fnName) {
  return vm.runInContext(`${fnName}()`, sandbox);
}

test('a fresh load with no stored preference starts collapsed', () => {
  const btn = makeStubElement();
  const sandbox = loadAppJS(undefined, btn);
  assert.equal(vm.runInContext('_pwaExpanded', sandbox), false);
  assert.equal(btn.title, 'Expand window');
});

test('loading with cs_pwa_expanded="1" already stored restores the expanded state immediately, before any click', () => {
  const btn = makeStubElement();
  const sandbox = loadAppJS('1', btn);
  assert.equal(vm.runInContext('_pwaExpanded', sandbox), true, 'state must be restored on load, not just on next click');
  assert.equal(btn.title, 'Restore window size');
});

test('loading with cs_pwa_expanded="0" stays collapsed', () => {
  const btn = makeStubElement();
  const sandbox = loadAppJS('0', btn);
  assert.equal(vm.runInContext('_pwaExpanded', sandbox), false);
});

test('clicking the button persists the new state to localStorage', () => {
  const btn = makeStubElement();
  const sandbox = loadAppJS(undefined, btn);
  call(sandbox, 'toggleFullscreen');
  assert.equal(sandbox.localStorage.getItem('cs_pwa_expanded'), '1');
  call(sandbox, 'toggleFullscreen');
  assert.equal(sandbox.localStorage.getItem('cs_pwa_expanded'), '0');
});

test('a simulated reload (fresh sandbox) after toggling on reads the persisted "1" back', () => {
  const btn1 = makeStubElement();
  const first = loadAppJS(undefined, btn1);
  call(first, 'toggleFullscreen');
  const persisted = first.localStorage.getItem('cs_pwa_expanded');

  // Fresh sandbox = fresh page load, but with localStorage carrying over
  // (real localStorage survives a reload; only the in-memory JS state does
  // not, which is exactly the bug being fixed here).
  const btn2 = makeStubElement();
  const second = loadAppJS(persisted, btn2);
  assert.equal(vm.runInContext('_pwaExpanded', second), true);
  assert.equal(btn2.title, 'Restore window size');
});

test('localStorage throwing AFTER load (e.g. quota exceeded, or blocked mid-session) does not crash toggleFullscreen', () => {
  // A fully-throwing localStorage from the very first call is a bigger,
  // pre-existing gap elsewhere in app.js (state.token = localStorage.
  // getItem('cs_token') at top-of-file has no try/catch at all) -- not
  // something this fix introduces or is responsible for. What this fix's
  // OWN try/catch needs to survive is localStorage becoming unavailable
  // partway through a session (e.g. a quota error), after the app has
  // already loaded successfully.
  const btn = makeStubElement();
  const sandbox = loadAppJS(undefined, btn);
  sandbox.localStorage.setItem = () => { throw new Error('quota exceeded'); };
  assert.doesNotThrow(() => call(sandbox, 'toggleFullscreen'));
  // The in-memory toggle must still have applied even though persistence failed.
  assert.equal(vm.runInContext('_pwaExpanded', sandbox), true);
});
