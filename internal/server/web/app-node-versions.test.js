// Compute node backend versions (node detail) — PWA side.
//
// window.nodeBackendVersionsBody(d) renders the decoded
// GET /api/compute/nodes/{name}/versions body: each backend with its
// version (or not-installed) and its below-minimum flag from the
// requirements report. loadNodeBackendVersions(name) fetches it into
// #nodeBackendVersions_{name} the first time the node's detail row is
// expanded (the row's <details> carries the ontoggle trigger). The node
// detail row carries the "Backend versions" line, every value is escaped,
// and every node_backend_ locale key is pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const VIEW_PREFIX = 'node_backend_';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function body(d) {
  const sandbox = loadApp();
  sandbox._d = d;
  return vm.runInContext('window.nodeBackendVersionsBody(_d)', sandbox);
}

function okFetch(payload) {
  return (url) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(payload) });
}

// ── rendering ───────────────────────────────────────────────────────────────

test('each backend shows its version; a below-minimum backend is flagged with the minimum', () => {
  const html = body({ backends: [
    { name: 'ollama', version: '0.9.7', flagged: false },
    { name: 'llm', version: '0.9.5', minimum: '0.9.7', flagged: true },
    { name: 'embed', version: null },
  ] });
  assert.ok(html.includes('ollama'), html);
  assert.ok(html.includes('>0.9.7</code>'), 'installed version missing: ' + html);
  assert.ok(html.includes('llm'), html);
  assert.ok(html.includes('below minimum'), 'flag missing');
  assert.ok(html.includes('minimum 0.9.7'), 'minimum missing');
  assert.ok(html.includes('not installed'), 'a backend without a version reads not installed');
});

test('an empty report reads none', () => {
  for (const html of [body({ backends: [] }), body(null)]) {
    assert.ok(html.includes('No backends on this node.'), html);
  }
});

test('every value from the API is escaped', () => {
  const html = body({ backends: [{ name: HOSTILE, version: HOSTILE, minimum: HOSTILE, flagged: true }] });
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── fetch + node detail wiring ──────────────────────────────────────────────

test('expanding a node loads its backend versions into the node detail row', async () => {
  const verEl = makeStubElement();
  const panel = makeStubElement();
  const sandbox = loadApp({ computeNodesPanel: panel, 'nodeBackendVersions_node-a': verEl });
  sandbox.fetch = (url) => {
    if (String(url) === '/api/compute/nodes') return okFetch({ nodes: [
      { name: 'node-a', kind: 'ollama', address: '127.0.0.1:11434', declared_capacity: { max_concurrent_models: 3 } },
    ] })();
    if (String(url) === '/api/compute/nodes/node-a/versions') return okFetch({ backends: [
      { name: 'ollama', version: '0.9.7', flagged: false },
    ] })();
    return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('nf') });
  };
  vm.runInContext('window.loadComputeNodesPanel()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('node-a'), 'node row missing: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('loadNodeBackendVersions'), 'the detail row carries no expand trigger: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('nodeBackendVersions_node-a'), 'the versions slot is missing: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('Backend versions'), 'the Backend versions line is missing');
  await vm.runInContext('window.loadNodeBackendVersions("node-a")', sandbox);
  assert.ok(verEl.innerHTML.includes('ollama'), 'versions not rendered: ' + verEl.innerHTML);
  assert.ok(verEl.innerHTML.includes('>0.9.7</code>'), verEl.innerHTML);
});

test('a failed versions read fills the slot with the escaped error', async () => {
  const verEl = makeStubElement();
  const sandbox = loadApp({ 'nodeBackendVersions_node-b': verEl });
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 500, text: () => Promise.resolve('boom ' + HOSTILE) });
  await vm.runInContext('window.loadNodeBackendVersions("node-b")', sandbox);
  assert.ok(verEl.innerHTML.includes('Could not load the backend versions'), 'error missing: ' + verEl.innerHTML);
  assert.ok(!verEl.innerHTML.includes('<img'), 'an image element was injected');
});

test('docsLinkForKey maps the node versions row to the glossary section', () => {
  const sandbox = loadApp();
  sandbox._key = 'node_backend_versions';
  assert.equal(vm.runInContext('docsLinkForKey(_key)', sandbox), 'datawatch-definitions.md#requirements-report');
});

test('every node_backend_ key is in all five bundles with a real translation', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith(VIEW_PREFIX));
  assert.ok(keys.length >= 5, 'expected the full key set, got ' + keys.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.equal(typeof b[k], 'string', `${lang} lacks ${k}`);
      assert.ok(b[k].trim() !== '', `${lang} has an empty ${k}`);
      if (lang !== 'en') assert.notEqual(b[k], en[k], `${lang} ${k} is an untranslated copy of English`);
      const ph = s => (s.match(/%\d\$s/g) || []).sort().join(',');
      assert.equal(ph(b[k]), ph(en[k]), `${lang} ${k} changes the placeholders`);
    }
  }
});
