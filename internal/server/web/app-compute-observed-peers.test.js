// Observed peers with no node, and the unanswered auto-created report, on
// the Compute page. The list endpoint carries observed_peers and
// unanswered_auto_created; the PWA renders each as its own section and the
// peer action opens the normal add form with only the name filled in.

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

async function renderPanel(listBody) {
  const rendered = [];
  const byId = { computeNodesPanel: makeStubElement(h => rendered.push(h)) };
  const sandbox = loadApp({ byId });
  sandbox.apiFetch = (p) => {
    if (p === '/api/compute/nodes') return Promise.resolve({ ok: true, ...listBody });
    return Promise.resolve({ ok: true });
  };
  vm.runInContext('loadComputeNodesPanel()', sandbox);
  await flushAsync();
  return { sandbox, html: rendered.join('\n') };
}

const liveNode = { name: 'example-node-1', kind: 'ollama', address: 'http://example-node-1:11434' };

test('an observed peer renders its name, shape and the add action', async () => {
  const { html } = await renderPanel({ nodes: [], observed_peers: [{ name: 'example-peer-1', shape: 'B' }], unanswered_auto_created: [] });
  assert.match(html, /id="computeObservedPeers"/);
  assert.match(html, /compute_observed_peers_title/);
  assert.match(html, /<strong>example-peer-1<\/strong>/);
  assert.match(html, />B<\/span>/);
  assert.match(html, /compute_observed_peer_add</);
  assert.doesNotMatch(html, /id="computeUnanswered"/);
});

test('the peer action opens the add form with the name filled and the address empty', async () => {
  const { sandbox, html } = await renderPanel({ nodes: [], observed_peers: [{ name: 'example-peer-1', shape: 'B' }] });
  const m = html.match(/onclick="(computeAddFromObservedPeer\([^"]*\))"/);
  assert.ok(m, 'button calls the peer action');
  const forms = [];
  sandbox.document.createElement = () => makeStubElement(h => forms.push(h));
  vm.runInContext(m[1].replace(/&quot;/g, '"'), sandbox);
  const form = forms.join('\n');
  assert.match(form, /id="computeNewName"[^>]*value="example-peer-1"/);
  assert.match(form, /id="computeNewAddress"[^>]*value=""/);
  // Add mode: name is editable, title is the add title, no edit marker.
  assert.doesNotMatch(form, /readonly/);
  assert.match(form, /compute_add_title/);
  assert.doesNotMatch(form, /compute_edit_title/);
});

test('the unanswered section lists a dead auto-created node', async () => {
  const { html } = await renderPanel({
    nodes: [liveNode], observed_peers: [],
    unanswered_auto_created: [{ name: 'example-dead-1', address: 'http://example-dead-1:11434', kind: 'ollama' }],
  });
  assert.match(html, /id="computeUnanswered"/);
  assert.match(html, /compute_unanswered_title/);
  assert.match(html, /<strong>example-dead-1<\/strong>/);
  assert.match(html, /http:\/\/example-dead-1:11434/);
  // Review aid only: no action inside the section.
  const section = html.slice(html.indexOf('id="computeUnanswered"'));
  assert.doesNotMatch(section.split('</div>\n  </div>')[0] || '', /<button/);
});

test('with only a live node and empty arrays neither section renders', async () => {
  const { html } = await renderPanel({ nodes: [liveNode], observed_peers: [], unanswered_auto_created: [] });
  assert.doesNotMatch(html, /computeObservedPeers|computeUnanswered/);
  assert.doesNotMatch(html, /example-dead/);
});

test('missing arrays render neither section', async () => {
  const { html } = await renderPanel({ nodes: [liveNode] });
  assert.doesNotMatch(html, /computeObservedPeers|computeUnanswered/);
});

test('dynamic values are escaped', async () => {
  const { html } = await renderPanel({
    nodes: [], observed_peers: [{ name: '<b>example-peer-2</b>', shape: '<i>' }],
    unanswered_auto_created: [{ name: '<u>x</u>', address: '<s>y</s>' }],
  });
  assert.doesNotMatch(html, /<b>example-peer-2|<u>x<|<s>y</);
});
