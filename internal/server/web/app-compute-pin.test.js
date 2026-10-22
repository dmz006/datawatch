// Pin keeper settings in the PWA. node_pinned_model and allow_model_swap
// are per compute node: they sit in the Compute node editor and save with
// the node. pin_check_seconds is one global value: it sits in
// Settings > General and saves through PUT /api/config.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  // openComputeAddPanel walks its modal for text nodes; the stub DOM has no tree.
  sandbox.NodeFilter = { SHOW_TEXT: 4 };
  sandbox.document.createTreeWalker = () => ({ nextNode() { return null; } });
  // The wizard's async follow-ups insert into a select; the stub DOM has no insertAdjacentHTML.
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

test('pin_check_seconds is a number field in a Settings card with a label key', () => {
  const sandbox = loadApp();
  const field = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'compute_pin') || {fields: []}).fields.find(f => f.key === 'pin_check_seconds') || null)",
    sandbox));
  assert.ok(field, 'pin_check_seconds missing from the compute pin card');
  assert.equal(field.type, 'number');
  assert.equal(field.labelKey, 'settings_pin_check_seconds');
  assert.equal(field.min, 0);
});

test('pin_check_seconds is flagged as a restart field (keepers start once at boot)', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext("RESTART_FIELDS.has('pin_check_seconds')", sandbox), true);
});

test('saving the pin_check_seconds card PUTs the value to /api/config', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.fetch = (url, opts) => { calls.push({ url, opts }); return Promise.resolve({ ok: true, status: 200 }); };
  vm.runInContext("saveGeneralField('pin_check_seconds', '0')", sandbox);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/config');
  assert.equal(calls[0].opts.method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].opts.body), { pin_check_seconds: '0' });
});

test('compute node editor renders the pinned-model input and allow-swap checkbox from the node', () => {
  const sandbox = loadApp();
  const rendered = [];
  sandbox.document.createElement = () => makeStubElement((html) => rendered.push(html));
  vm.runInContext(
    "openComputeAddPanel({ name: 'gpu-1', kind: 'ollama', address: 'http://gpu-1:11434', node_pinned_model: 'qwen3.8-64k:27b', allow_model_swap: true })",
    sandbox);
  const html = rendered.join('\n');
  assert.match(html, /id="computeNewPinnedModel"[^>]*value="qwen3\.8-64k:27b"/);
  assert.match(html, /id="computeNewAllowSwap" type="checkbox" checked/);
});

test('compute node editor renders an empty pin and an unchecked swap box for an unpinned node', () => {
  const sandbox = loadApp();
  const rendered = [];
  sandbox.document.createElement = () => makeStubElement((html) => rendered.push(html));
  vm.runInContext("openComputeAddPanel({ name: 'gpu-2', kind: 'ollama', address: 'http://gpu-2:11434' })", sandbox);
  const html = rendered.join('\n');
  assert.match(html, /id="computeNewPinnedModel"[^>]*value=""/);
  assert.match(html, /id="computeNewAllowSwap" type="checkbox"\s+\/>/);
});

test('saving a node sends node_pinned_model and allow_model_swap in the PUT body', async () => {
  const byId = {
    computeAddPanel: { dataset: { editName: 'gpu-1' } },
    computeNewName: { value: 'gpu-1' },
    computeNewKind: { value: 'ollama' },
    computeNewAddress: { value: 'http://gpu-1:11434' },
    computeNewPinnedModel: { value: '  qwen3.8-64k:27b  ' },
    computeNewAllowSwap: { checked: true },
  };
  const sandbox = loadApp({ byId });
  const calls = [];
  sandbox.apiFetch = (path, opts) => { calls.push({ path, opts }); return Promise.resolve({ ok: true }); };
  // Toast and list refresh are UI side effects; this test only checks the request.
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  sandbox.loadComputeNodesPanel = () => {};
  vm.runInContext('computeAddNode()', sandbox);
  await flushAsync();
  const save = calls.find(c => c.opts && c.opts.method === 'PUT');
  assert.ok(save, 'no PUT was sent: ' + JSON.stringify(calls.map(c => c.path)));
  assert.equal(save.path, '/api/compute/nodes/gpu-1');
  const body = JSON.parse(save.opts.body);
  assert.equal(body.node_pinned_model, 'qwen3.8-64k:27b');
  assert.equal(body.allow_model_swap, true);
});

test('the five pin label keys exist with the same key set in every locale bundle', () => {
  const keys = ['settings_pin_check_seconds', 'compute_field_pinned_model', 'compute_pinned_model_ph',
    'compute_field_allow_swap', 'compute_pin_hint'];
  const bundles = ['en', 'de', 'es', 'fr', 'ja'].map(l =>
    JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8')));
  for (const b of bundles) {
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' missing or empty');
  }
  const enKeys = Object.keys(bundles[0]).sort();
  for (const b of bundles.slice(1)) assert.deepEqual(Object.keys(b).sort(), enKeys, 'locale key drift');
});
