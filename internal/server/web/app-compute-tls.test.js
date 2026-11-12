// Certificate verification settings for compute nodes in the PWA: the row
// badge for each mode, the three fields in the save body, and the mutual
// disabling of the CA file, fingerprint and skip inputs.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
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

// renderRows runs loadComputeNodesPanel against the given nodes and returns
// the panel HTML.
async function renderRows(nodes) {
  let html = '';
  const panel = makeStubElement((h) => { html = h; });
  const sandbox = loadApp({ byId: { computeNodesPanel: panel } });
  sandbox.apiFetch = () => Promise.resolve({ nodes });
  vm.runInContext('loadComputeNodesPanel()', sandbox);
  await flushAsync();
  return html;
}

const baseNode = { name: 'gpu-1', kind: 'ollama', address: 'https://gpu-1:11434' };

test('a node with verification off shows the error-coloured badge with a tooltip', async () => {
  const html = await renderRows([{ ...baseNode, tls_insecure_skip_verify: true }]);
  assert.match(html, /class="compute-tls-off-badge"[^>]*background:var\(--error\)/);
  // The stub t() returns the key, so the keys stand in for the English text.
  assert.match(html, /compute-tls-off-badge[^>]*title="compute_tls_off_badge_tip"/);
  assert.match(html, /⚠ compute_tls_off_badge</);
  assert.doesNotMatch(html, /compute-tls-pinned-badge|compute-tls-ca-badge/);
});

test('a node with a fingerprint shows the quiet pinned badge', async () => {
  const html = await renderRows([{ ...baseNode, tls_cert_fingerprint: 'ab'.repeat(32) }]);
  assert.match(html, /class="compute-tls-pinned-badge"[^>]*>compute_tls_pinned_badge</);
  assert.doesNotMatch(html, /compute-tls-off-badge/);
});

test('a node with a CA file shows the quiet CA file badge', async () => {
  const html = await renderRows([{ ...baseNode, tls_ca_file: '/etc/ssl/lab-ca.pem' }]);
  assert.match(html, /class="compute-tls-ca-badge"[^>]*>compute_tls_ca_badge</);
  assert.doesNotMatch(html, /compute-tls-off-badge|compute-tls-pinned-badge/);
});

test('a false opt-out renders no badge and no detail row', async () => {
  const html = await renderRows([{ ...baseNode, tls_insecure_skip_verify: false, tls_ca_file: '', tls_cert_fingerprint: '' }]);
  assert.doesNotMatch(html, /compute-tls-/);
  assert.doesNotMatch(html, /tls_insecure_skip_verify|tls_ca_file|tls_cert_fingerprint/);
});

test('a true opt-out is listed in the expanded detail table', async () => {
  const html = await renderRows([{ ...baseNode, tls_insecure_skip_verify: true }]);
  assert.match(html, /<td[^>]*>tls_insecure_skip_verify<\/td>/);
});

test('the opt-out badge wins over a leftover fingerprint', async () => {
  const html = await renderRows([{ ...baseNode, tls_insecure_skip_verify: true, tls_cert_fingerprint: 'ab'.repeat(32) }]);
  assert.match(html, /compute-tls-off-badge/);
  assert.doesNotMatch(html, /compute-tls-pinned-badge/);
});

function renderEditor(node) {
  const sandbox = loadApp();
  const rendered = [];
  sandbox.document.createElement = () => makeStubElement((html) => rendered.push(html));
  vm.runInContext('openComputeAddPanel(' + JSON.stringify(node) + ')', sandbox);
  return rendered.join('\n');
}

test('the editor renders the certificate verification group with three help links', () => {
  const html = renderEditor(baseNode);
  assert.match(html, /id="computeNewTLSCAFile"/);
  assert.match(html, /id="computeNewTLSFingerprint"/);
  assert.match(html, /id="computeNewTLSSkip" type="checkbox"/);
  const links = html.match(/#docs\/datawatch-definitions\.md#compute-node-tls-verification/g) || [];
  assert.equal(links.length, 3, 'one help link per field');
  // Warning line is hidden until the skip box is ticked.
  assert.match(html, /id="computeNewTLSSkipWarn" style="display:none;/);
});

test('the editor prefills the CA file and fingerprint from the node and locks the skip box', () => {
  const html = renderEditor({ ...baseNode, tls_ca_file: '/etc/ssl/lab-ca.pem', tls_cert_fingerprint: 'AB:CD' });
  assert.match(html, /id="computeNewTLSCAFile"[^>]*value="\/etc\/ssl\/lab-ca\.pem"/);
  assert.match(html, /id="computeNewTLSFingerprint"[^>]*value="AB:CD"/);
  assert.match(html, /id="computeNewTLSSkip" type="checkbox"[^>]*disabled/);
});

test('the editor prefills a true opt-out: box ticked, other inputs disabled, warning visible', () => {
  const html = renderEditor({ ...baseNode, tls_insecure_skip_verify: true });
  assert.match(html, /id="computeNewTLSSkip" type="checkbox"[^>]*checked/);
  assert.match(html, /id="computeNewTLSCAFile"[^>]*disabled/);
  assert.match(html, /id="computeNewTLSFingerprint"[^>]*disabled/);
  assert.match(html, /id="computeNewTLSSkipWarn" style="display:;/);
});

function tlsInputs(over) {
  return {
    computeNewTLSSkip: { checked: false, disabled: false, ...(over.skip || {}) },
    computeNewTLSCAFile: { value: '', disabled: false, ...(over.ca || {}) },
    computeNewTLSFingerprint: { value: '', disabled: false, ...(over.fp || {}) },
    computeNewTLSSkipWarn: { style: { display: 'none' } },
  };
}

test('ticking skip disables the CA file and fingerprint and shows the warning', () => {
  const byId = tlsInputs({ skip: { checked: true } });
  const sandbox = loadApp({ byId });
  vm.runInContext('computeTLSModeChanged()', sandbox);
  assert.equal(byId.computeNewTLSCAFile.disabled, true);
  assert.equal(byId.computeNewTLSFingerprint.disabled, true);
  assert.equal(byId.computeNewTLSSkip.disabled, false);
  assert.equal(byId.computeNewTLSSkipWarn.style.display, '');
});

test('typing a CA file or fingerprint disables the skip box', () => {
  for (const over of [{ ca: { value: '/etc/ssl/ca.pem' } }, { fp: { value: 'ABCD' } }]) {
    const byId = tlsInputs(over);
    const sandbox = loadApp({ byId });
    vm.runInContext('computeTLSModeChanged()', sandbox);
    assert.equal(byId.computeNewTLSSkip.disabled, true);
    assert.equal(byId.computeNewTLSCAFile.disabled, false);
    assert.equal(byId.computeNewTLSFingerprint.disabled, false);
    assert.equal(byId.computeNewTLSSkipWarn.style.display, 'none');
  }
});

test('clearing the CA file and fingerprint and unticking skip re-enables everything', () => {
  const byId = tlsInputs({ skip: { checked: false, disabled: true }, ca: { disabled: true }, fp: { disabled: true } });
  const sandbox = loadApp({ byId });
  vm.runInContext('computeTLSModeChanged()', sandbox);
  assert.equal(byId.computeNewTLSSkip.disabled, false);
  assert.equal(byId.computeNewTLSCAFile.disabled, false);
  assert.equal(byId.computeNewTLSFingerprint.disabled, false);
});

async function saveBody(tls, editName) {
  const byId = {
    computeAddPanel: { dataset: { editName } },
    computeNewName: { value: 'gpu-1' },
    computeNewKind: { value: 'ollama' },
    computeNewAddress: { value: 'https://gpu-1:11434' },
    ...tls,
  };
  const sandbox = loadApp({ byId });
  const calls = [];
  sandbox.apiFetch = (p, opts) => { calls.push({ p, opts }); return Promise.resolve({ ok: true }); };
  sandbox.showToast = () => {};
  sandbox.showError = () => {};
  sandbox.loadComputeNodesPanel = () => {};
  vm.runInContext('computeAddNode()', sandbox);
  await flushAsync();
  const save = calls.find(c => c.opts && (c.opts.method === 'PUT' || c.opts.method === 'POST'));
  assert.ok(save, 'no save request was sent');
  return JSON.parse(save.opts.body);
}

test('saving sends the CA file and fingerprint trimmed, and false for the opt-out', async () => {
  const body = await saveBody(tlsInputs({ ca: { value: ' /etc/ssl/ca.pem ' }, fp: { value: ' AB:CD ' } }), 'gpu-1');
  assert.equal(body.tls_ca_file, '/etc/ssl/ca.pem');
  assert.equal(body.tls_cert_fingerprint, 'AB:CD');
  assert.strictEqual(body.tls_insecure_skip_verify, false);
});

test('saving an edit that cleared everything still sends the empty strings and false', async () => {
  const body = await saveBody(tlsInputs({}), 'gpu-1');
  assert.ok('tls_ca_file' in body && 'tls_cert_fingerprint' in body && 'tls_insecure_skip_verify' in body);
  assert.strictEqual(body.tls_ca_file, '');
  assert.strictEqual(body.tls_cert_fingerprint, '');
  assert.strictEqual(body.tls_insecure_skip_verify, false);
});

test('saving with skip ticked sends true and empties the other two', async () => {
  const body = await saveBody(tlsInputs({ skip: { checked: true }, ca: { value: '/stale.pem' }, fp: { value: 'AB' } }), '');
  assert.strictEqual(body.tls_insecure_skip_verify, true);
  assert.strictEqual(body.tls_ca_file, '');
  assert.strictEqual(body.tls_cert_fingerprint, '');
});
