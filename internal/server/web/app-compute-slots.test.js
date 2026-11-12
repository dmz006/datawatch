// The compute node form carries the operator's session_slots override. The
// same builder renders the add and the edit form, and the collector reads
// the field back into the node record: a blank field saves 0 (derive from
// observer data), and a number saves as that whole number.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// The collector only reads .value from the input, so a plain object stands in.
function inputWithValue(value) {
  return { value };
}

test('the node form renders a session_slots input showing the saved override', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama', session_slots: 3})", sandbox);
  assert.match(html, /id="fe_compute_session_slots"/);
  assert.match(html, /id="fe_compute_session_slots"[^>]*value="3"/);
});

test('the node form shows an empty session_slots input when no override is set', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama'})", sandbox);
  assert.match(html, /id="fe_compute_session_slots"[^>]*value=""/);
});

test('collecting the form saves a typed session_slots as a whole number', () => {
  const sandbox = loadApp({ fe_compute_session_slots: inputWithValue('4') });
  const out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox'}))", sandbox));
  assert.equal(out.session_slots, 4);
});

test('collecting the form with a blank session_slots saves 0 (derive)', () => {
  const sandbox = loadApp({ fe_compute_session_slots: inputWithValue('') });
  const out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox', session_slots: 7}))", sandbox));
  assert.equal(out.session_slots, 0);
});
