// GH#192 Phase 3 — #7 three-finger swipe opens a real server-picker
// modal (operator decision 2026-10-07, reversing the prior scroll+
// highlight approach). See docs/plans/2026-10-07-gh192-parity-batch.md.

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

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('openServerPickerModal is exposed globally for the swipe gesture to call', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('typeof window.openServerPickerModal', sandbox), 'function');
});

test('the old scroll+highlight path is gone: highlightServerPicker no longer exists', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('typeof highlightServerPicker', sandbox), 'undefined');
});

test('_loadServerPickerModalList shows All + Local chips even with zero configured servers (the old no-op is gone)', () => {
  const listEl = makeStubElement();
  const sandbox = loadAppJS({ serverPickerModalList: listEl });
  setState(sandbox, { servers: { servers: [] } }); // zero remote servers
  call(sandbox, '_loadServerPickerModalList');
  assert.ok(/selectServer\(&quot;all&quot;\)/.test(listEl.innerHTML), 'expected an All chip');
  assert.ok(/selectServer\(null\)/.test(listEl.innerHTML), 'expected a Local chip');
});

test('_loadServerPickerModalList includes every configured, enabled remote server as its own chip', () => {
  const listEl = makeStubElement();
  const sandbox = loadAppJS({ serverPickerModalList: listEl });
  setState(sandbox, { servers: { servers: [{ name: 'pi-node', enabled: true }, { name: 'disabled-one', enabled: false }] } });
  call(sandbox, '_loadServerPickerModalList');
  assert.ok(listEl.innerHTML.includes('pi-node'));
  assert.ok(!listEl.innerHTML.includes('disabled-one'), 'a disabled server entry should not get a chip');
});

test('_loadServerPickerModalList does not throw if the modal was closed before the server-list fetch resolves', async () => {
  // No 'serverPickerModalList' in byId at all, and state.servers is
  // undefined so the function takes the async fetch branch.
  const sandbox = loadAppJS();
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ servers: [] }) });
  assert.doesNotThrow(() => call(sandbox, '_loadServerPickerModalList'));
  await new Promise(r => setImmediate(r));
});

test('a chip\'s onclick removes the modal after switching servers (no leftover overlay)', () => {
  const listEl = makeStubElement();
  const sandbox = loadAppJS({ serverPickerModalList: listEl });
  setState(sandbox, { servers: { servers: [{ name: 'pi-node', enabled: true }] } });
  call(sandbox, '_loadServerPickerModalList');
  assert.ok(listEl.innerHTML.includes('document.getElementById(&#39;serverPickerModal&#39;).remove()'),
    'every chip must close the modal on click, not just switch servers');
});

test('_openAddServerFromPicker routes to Settings -> Comms (where the reused add-server form lives) instead of rebuilding the form', () => {
  const sandbox = loadAppJS({ headerTitle: makeStubElement() });
  const navigated = [];
  sandbox.history = { pushState: () => {} };
  vm.runInContext('navigate = (v) => { __navigated.push(v); }', Object.assign(sandbox, { __navigated: navigated }));
  call(sandbox, '_openAddServerFromPicker');
  assert.equal(vm.runInContext('_settingsTab', sandbox), 'comms');
  assert.deepEqual(navigated, ['settings']);
});
