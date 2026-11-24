// hard_limit is a per-node setting: off (default) lets an operator start go
// over the node's engine slots; on makes them a physical limit. It sits in both
// compute node forms and saves with the node (PUT replaces the whole node, so
// the value is always sent).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  sandbox.NodeFilter = { SHOW_TEXT: 4 };
  sandbox.document.createTreeWalker = () => ({ nextNode() { return null; } });
  const getById = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => {
    const el = getById(id);
    if (!el.insertAdjacentHTML) el.insertAdjacentHTML = () => {};
    return el;
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

test('the inline node form shows hard_limit checked for a hard node and unchecked otherwise', () => {
  const sandbox = loadApp();
  const hard = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama', hard_limit: true})", sandbox);
  assert.match(hard, /id="fe_compute_hard_limit" type="checkbox" checked/);
  const soft = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama'})", sandbox);
  assert.match(soft, /id="fe_compute_hard_limit" type="checkbox"\s+\/>/);
});

test('collecting the inline form saves hard_limit from the checkbox, true and false', () => {
  let sandbox = loadApp({ byId: { fe_compute_hard_limit: { checked: true } } });
  let out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox'}))", sandbox));
  assert.equal(out.hard_limit, true);
  sandbox = loadApp({ byId: { fe_compute_hard_limit: { checked: false } } });
  out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox', hard_limit: true}))", sandbox));
  assert.equal(out.hard_limit, false);
});

test('the node editor panel renders the hard_limit checkbox from the node', () => {
  const sandbox = loadApp();
  const rendered = [];
  sandbox.document.createElement = () => makeStubElement((html) => rendered.push(html));
  vm.runInContext("openComputeAddPanel({ name: 'gpu-1', kind: 'ollama', address: 'http://gpu-1:11434', hard_limit: true })", sandbox);
  assert.match(rendered.join('\n'), /id="computeNewHardLimit" type="checkbox" checked/);
});

test('saving a node from the editor panel sends hard_limit in the PUT body', async () => {
  const byId = {
    computeAddPanel: { dataset: { editName: 'gpu-1' } },
    computeNewName: { value: 'gpu-1' },
    computeNewKind: { value: 'ollama' },
    computeNewAddress: { value: 'http://gpu-1:11434' },
    computeNewHardLimit: { checked: true },
  };
  const sandbox = loadApp({ byId });
  const calls = [];
  sandbox.apiFetch = (p, opts) => { calls.push({ p, opts }); return Promise.resolve({ ok: true }); };
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  sandbox.loadComputeNodesPanel = () => {};
  vm.runInContext('computeAddNode()', sandbox);
  await flushAsync();
  const save = calls.find(c => c.opts && c.opts.method === 'PUT');
  assert.ok(save, 'no PUT was sent');
  assert.equal(JSON.parse(save.opts.body).hard_limit, true);
});

test('the hard_limit label and hint keys exist in every locale bundle', () => {
  const keys = ['compute_field_hard_limit', 'compute_hard_limit_hint'];
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ': ' + k + ' missing or empty');
  }
});
