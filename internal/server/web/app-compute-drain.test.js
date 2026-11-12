// Drain controls on the Compute node cards in the PWA.
//
// The daemon exposes per-node drain (internal/server/compute.go):
//   POST   /api/compute/nodes/<name>/drain   { until?: RFC3339, reason? }
//   DELETE /api/compute/nodes/<name>/drain   (idempotent undrain)
// The node list carries drained/drain_until/drain_reason/permissions;
// the live held-task count comes from the node's /health endpoint and is
// rendered as "N tasks still finishing" while the node is drained.
//
// Every operator-visible string goes through t(key); each key must exist
// in all five locale bundles (en/de/es/fr/ja), matching the pin test.

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

// renderPanel loads app.js, stubs apiFetch to serve `nodes` from the list
// endpoint (and `health` from the per-node health endpoint), runs
// loadComputeNodesPanel and captures what lands in #computeNodesPanel.
function renderPanel(nodes, health) {
  const rendered = [];
  const byId = { computeNodesPanel: makeStubElement(h => rendered.push(h)) };
  const sandbox = loadApp({ byId });
  const calls = [];
  sandbox.apiFetch = (p) => {
    calls.push(p);
    if (p === '/api/compute/nodes') return Promise.resolve({ ok: true, nodes });
    if (p.endsWith('/health')) return Promise.resolve({ ok: true, ...(health || {}) });
    return Promise.resolve({ ok: true });
  };
  vm.runInContext('loadComputeNodesPanel()', sandbox);
  return { sandbox, calls, html: () => rendered.join('\n') };
}

test('an undrained node card shows the Drain button and the inline form, no undrain', async () => {
  const r = renderPanel([
    { name: 'gpu-1', kind: 'ollama', address: 'http://gpu-1:11434' },
  ]);
  await flushAsync();
  const html = r.html();
  // Drain button wired to toggle the inline form for this node.
  assert.match(html, /computeToggleDrainForm\(&quot;gpu-1&quot;\)/);
  // The inline form is present (hidden) with per-node until + reason fields.
  assert.match(html, /id="drainForm-gpu-1"/);
  assert.match(html, /type="datetime-local" id="drainUntil-gpu-1"/);
  assert.match(html, /type="text" id="drainReason-gpu-1"/);
  assert.match(html, /computeSubmitDrain\(&quot;gpu-1&quot;\)/);
  // No undrain and no drained state line for an undrained node.
  assert.doesNotMatch(html, /computeUndrain\(/);
  assert.doesNotMatch(html, />compute_drain_open</);
  assert.doesNotMatch(html, />compute_drain_until</);
});

test('a drained node with an until shows the state line and an Undrain button', async () => {
  const r = renderPanel([
    { name: 'gpu-1', kind: 'ollama', drained: true, drain_until: '2026-11-02T09:30:00Z' },
  ]);
  await flushAsync();
  const html = r.html();
  // State line: drained with an end time (t(key) resolves to the key in the
  // sandbox, since the i18n bundle is fetched and not loaded here).
  assert.match(html, /compute_drain_until/);
  assert.doesNotMatch(html, /compute_drain_open/);
  // Undrain action wired, no Drain button/form.
  assert.match(html, /computeUndrain\(&quot;gpu-1&quot;\)/);
  assert.doesNotMatch(html, /computeToggleDrainForm\(/);
  assert.doesNotMatch(html, /drainForm-gpu-1/);
  // The held-task placeholder line is present while drained.
  assert.match(html, /id="drainHeld-gpu-1"/);
});

test('a drained node with no until shows the open-ended state line', async () => {
  const r = renderPanel([
    { name: 'gpu-1', kind: 'ollama', drained: true },
  ]);
  await flushAsync();
  const html = r.html();
  assert.match(html, /compute_drain_open/);
  assert.doesNotMatch(html, /compute_drain_until/);
});

test('the held-task line is filled from the node health endpoint while drained', async () => {
  const held = { textContent: '' };
  const byId = {
    computeNodesPanel: makeStubElement(() => {}),
    'drainHeld-gpu-1': held,
  };
  const sandbox = loadApp({ byId });
  const calls = [];
  sandbox.apiFetch = (p) => {
    calls.push(p);
    if (p === '/api/compute/nodes') {
      return Promise.resolve({ ok: true, nodes: [{ name: 'gpu-1', kind: 'ollama', drained: true }] });
    }
    if (p === '/api/compute/nodes/gpu-1/health') return Promise.resolve({ ok: true, held_tasks: 3 });
    return Promise.resolve({ ok: true });
  };
  vm.runInContext('loadComputeNodesPanel()', sandbox);
  await flushAsync();
  assert.ok(calls.includes('/api/compute/nodes/gpu-1/health'), 'health endpoint not fetched: ' + calls.join(', '));
  assert.ok(held.textContent.length > 0, 'held-task line was not filled');
});

test('denied consumers are listed on the node card when present', async () => {
  const r = renderPanel([
    { name: 'gpu-1', kind: 'ollama', permissions: { denied_consumers: ['agent-a', 'agent-b'] } },
  ]);
  await flushAsync();
  const html = r.html();
  assert.match(html, /compute_denied/);
  assert.match(html, /agent-a/);
  assert.match(html, /agent-b/);
});

test('denied consumers are absent when the node denies none', async () => {
  const r = renderPanel([
    { name: 'gpu-1', kind: 'ollama' },
  ]);
  await flushAsync();
  const html = r.html();
  assert.doesNotMatch(html, /compute_denied/);
});

test('drain POSTs until and reason to /api/compute/nodes/<name>/drain', async () => {
  const sandbox = loadApp({
    byId: {
      'drainUntil-gpu-1': { value: '2026-11-02T09:30' },
      'drainReason-gpu-1': { value: '  maintenance window  ' },
    },
  });
  const calls = [];
  sandbox.apiFetch = (p, o) => { calls.push({ p, o }); return Promise.resolve({ ok: true }); };
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  let reloaded = 0;
  sandbox.loadComputeNodesPanel = () => { reloaded++; };
  vm.runInContext('computeSubmitDrain("gpu-1")', sandbox);
  await flushAsync();
  const call = calls.find(c => c.p === '/api/compute/nodes/gpu-1/drain');
  assert.ok(call, 'no drain call: ' + JSON.stringify(calls.map(c => c.p)));
  assert.equal(call.o.method, 'POST');
  const body = JSON.parse(call.o.body);
  assert.equal(body.reason, 'maintenance window');
  // datetime-local value converted to an RFC3339 (ISO) timestamp.
  assert.match(body.until, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  assert.equal(reloaded, 1, 'panel did not reload after drain');
});

test('drain with empty until and reason sends an open-ended drain body', async () => {
  const sandbox = loadApp({
    byId: { 'drainUntil-gpu-1': { value: '' }, 'drainReason-gpu-1': { value: '' } },
  });
  const calls = [];
  sandbox.apiFetch = (p, o) => { calls.push({ p, o }); return Promise.resolve({ ok: true }); };
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  sandbox.loadComputeNodesPanel = () => {};
  vm.runInContext('computeSubmitDrain("gpu-1")', sandbox);
  await flushAsync();
  const call = calls.find(c => c.p === '/api/compute/nodes/gpu-1/drain');
  assert.ok(call);
  assert.equal(call.o.method, 'POST');
  const body = JSON.parse(call.o.body);
  assert.ok(!('until' in body), 'until should be omitted when empty');
  assert.equal(body.reason, '');
});

test('undrain DELETEs /api/compute/nodes/<name>/drain and reloads the panel', async () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.apiFetch = (p, o) => { calls.push({ p, o }); return Promise.resolve({ ok: true }); };
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  let reloaded = 0;
  sandbox.loadComputeNodesPanel = () => { reloaded++; };
  vm.runInContext('computeUndrain("gpu-1")', sandbox);
  await flushAsync();
  const call = calls.find(c => c.p === '/api/compute/nodes/gpu-1/drain');
  assert.ok(call, 'no undrain call: ' + JSON.stringify(calls.map(c => c.p)));
  assert.equal(call.o.method, 'DELETE');
  assert.equal(reloaded, 1, 'panel did not reload after undrain');
});

test('every drain key exists in en.json', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const keys = [
    'compute_drain_until', 'compute_drain_open', 'compute_drain_held',
    'compute_drain_btn', 'compute_drain_btn_title', 'compute_drain_apply',
    'compute_drain_cancel', 'compute_drain_until_label', 'compute_drain_reason_label',
    'compute_drain_ok', 'compute_drain_failed',
    'compute_undrain_btn', 'compute_undrain_btn_title',
    'compute_undrain_ok', 'compute_undrain_failed',
    'compute_denied',
  ];
  for (const k of keys) {
    assert.ok(typeof en[k] === 'string' && en[k].length > 0, k + ' missing or empty in en.json');
  }
});

test('the drain keys exist in every locale bundle with the same key set as en', () => {
  const bundles = ['en', 'de', 'es', 'fr', 'ja'].map(l =>
    JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8')));
  const drainKeys = Object.keys(bundles[0]).filter(k =>
    k.startsWith('compute_drain') || k.startsWith('compute_undrain') || k === 'compute_denied');
  assert.ok(drainKeys.length >= 16, 'expected at least 16 drain keys in en.json');
  for (const b of bundles) {
    for (const k of drainKeys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' missing or empty in ' + b);
    }
  }
  const enKeys = Object.keys(bundles[0]).sort();
  for (const b of bundles.slice(1)) assert.deepEqual(Object.keys(b).sort(), enKeys, 'locale key drift');
});
