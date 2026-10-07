// BL315 (2026-10-07, reinstated) — regression test for the PWA install
// prompt. Factored as _onBeforeInstallPrompt/_onAppInstalled/installPWA
// so this is testable directly: app.js's test sandbox stubs
// addEventListener as a no-op (same as attempting to synthesize a real
// 'beforeinstallprompt' event in Node/jsdom, which neither can do
// realistically), so the real addEventListener-registered callbacks
// themselves are unreachable from a test -- the factored-out logic is
// what's actually exercised here.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(installBtn) {
  const byId = installBtn ? { headerInstallBtn: installBtn } : {};
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  for (const fn of ['_onBeforeInstallPrompt', '_onAppInstalled', 'installPWA']) {
    assert.equal(typeof sandbox[fn], 'function', `${fn} is not defined -- did it get renamed?`);
  }
  return sandbox;
}

function call(sandbox, fnName, arg) {
  sandbox.__arg = arg;
  const result = vm.runInContext(arg !== undefined ? `${fnName}(__arg)` : `${fnName}()`, sandbox);
  delete sandbox.__arg;
  return result;
}

test('_onBeforeInstallPrompt shows the install button and calls preventDefault', () => {
  const btn = makeStubElement();
  btn.style.display = 'none';
  const sandbox = loadAppJS(btn);
  let prevented = false;
  call(sandbox, '_onBeforeInstallPrompt', { preventDefault: () => { prevented = true; } });
  assert.equal(prevented, true);
  assert.equal(btn.style.display, '');
});

test('_onAppInstalled hides the install button', () => {
  const btn = makeStubElement();
  btn.style.display = '';
  const sandbox = loadAppJS(btn);
  call(sandbox, '_onAppInstalled');
  assert.equal(btn.style.display, 'none');
});

test('installPWA is a safe no-op when no prompt event has fired', () => {
  const btn = makeStubElement();
  btn.style.display = 'none';
  const sandbox = loadAppJS(btn);
  // Must not throw even though _pwaInstallPrompt is still null.
  call(sandbox, 'installPWA');
  assert.equal(btn.style.display, 'none', 'button state must not change');
});

test('installPWA calls prompt(), awaits userChoice, then hides the button', async () => {
  const btn = makeStubElement();
  btn.style.display = 'none';
  const sandbox = loadAppJS(btn);

  let promptCalled = false;
  const fakeEvent = {
    preventDefault: () => {},
    prompt: () => { promptCalled = true; },
    userChoice: Promise.resolve({ outcome: 'accepted' }),
  };
  call(sandbox, '_onBeforeInstallPrompt', fakeEvent);
  assert.equal(btn.style.display, '', 'button should be visible after the prompt event');

  call(sandbox, 'installPWA');
  assert.equal(promptCalled, true, 'installPWA must call the captured event\'s prompt()');

  await flushAsync();
  assert.equal(btn.style.display, 'none', 'button should hide once userChoice resolves');
});

test('a fresh _onBeforeInstallPrompt after install can re-show the button (reinstall/uninstall-reinstall case)', () => {
  const btn = makeStubElement();
  const sandbox = loadAppJS(btn);
  call(sandbox, '_onBeforeInstallPrompt', { preventDefault: () => {} });
  call(sandbox, '_onAppInstalled');
  assert.equal(btn.style.display, 'none');
  call(sandbox, '_onBeforeInstallPrompt', { preventDefault: () => {} });
  assert.equal(btn.style.display, '');
});
