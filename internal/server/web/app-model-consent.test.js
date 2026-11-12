// Model pull consent (web setup view) — PWA side.
//
// loadModelConsentPanel reads GET /api/automation/model-consent into
// window._modelConsentState and renders the editor: one pull-allowed toggle
// per listed model, an add-model row, the publisher allowlist with its
// chips, and a save button that PUTs the whole record. The mutation helpers
// (toggle / add / remove model, add / remove publisher) update the state and
// re-render #modelConsentPanel. Every value is escaped, the card is a real
// settings section of the Automata tab, and every model_consent_ locale key
// is pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const VIEW_PREFIX = 'model_consent_';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

// inputStub is a form-field stand-in whose value actually round-trips
// (makeStubElement's value is fixed to '').
function inputStub() {
  let v = '';
  return {
    style: {}, dataset: {}, children: [], childNodes: [],
    addEventListener() {}, removeEventListener() {},
    querySelector() { return null; }, querySelectorAll() { return []; },
    focus() {}, blur() {},
    get value() { return v; }, set value(x) { v = String(x); },
  };
}

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

function setConsent(sandbox, consent) {
  sandbox._consent = consent;
  vm.runInContext(
    'window._modelConsentState = { models: _consent.models.map(m => ({ name: m.name, publisher: m.publisher || "", allowed: !!m.allowed })), publishers: _consent.publishers.slice() };',
    sandbox);
}

function editor(sandbox) {
  return vm.runInContext('window.modelConsentEditorHtml(null)', sandbox);
}

// ── rendering ───────────────────────────────────────────────────────────────

test('the editor shows the title, both parts, the toggles and the save button', () => {
  const sandbox = loadApp();
  setConsent(sandbox, {
    models: [{ name: 'qwen3:8b', publisher: 'qwen', allowed: true }, { name: 'llama3', allowed: false }],
    publishers: ['qwen'],
  });
  const html = editor(sandbox);
  assert.ok(html.includes('Model pull consent'), 'card title missing: ' + html);
  assert.ok(html.includes('Pull-allowed models'), 'models part missing');
  assert.ok(html.includes('Publisher allowlist'), 'publishers part missing');
  assert.ok(html.includes('qwen3:8b'), 'model name missing');
  assert.ok(html.includes('publisher qwen'), 'model publisher missing');
  assert.ok(html.includes('class="model-consent-allowed" checked'), 'an allowed model must render checked');
  assert.ok(html.includes('llama3'), 'second model missing');
  assert.ok(html.includes('model-consent-publisher') && html.includes('>qwen <button'), 'publisher chip missing');
  assert.ok(html.includes('model-consent-add-row'), 'add-model row missing');
  assert.ok(html.includes('model-consent-add-publisher-row'), 'add-publisher row missing');
  assert.ok(html.includes('model-consent-save'), 'save button missing');
  assert.ok(html.includes('>Save<'), html);
  assert.ok(html.includes('model-consent-refresh'), 'refresh button missing');
});

test('an empty consent reads none for models and publishers', () => {
  const sandbox = loadApp();
  setConsent(sandbox, { models: [], publishers: [] });
  const html = editor(sandbox);
  assert.ok(html.includes('No models listed — none may be pulled.'), html);
  assert.ok(html.includes('No publishers allowed.'), html);
});

test('a save failure message is shown in the status line, escaped', () => {
  const sandbox = loadApp();
  setConsent(sandbox, { models: [], publishers: [] });
  sandbox._st = { msg: 'boom ' + HOSTILE, error: true };
  vm.runInContext('window._modelConsentStatus = _st;', sandbox);
  const html = editor(sandbox);
  assert.ok(html.includes('model-consent-status'), 'status line missing: ' + html);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped');
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

test('every value from the API is escaped', () => {
  const sandbox = loadApp();
  setConsent(sandbox, { models: [{ name: HOSTILE, publisher: HOSTILE }], publishers: [HOSTILE] });
  const html = editor(sandbox);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── mutations ───────────────────────────────────────────────────────────────

function state(sandbox) {
  return JSON.parse(vm.runInContext('JSON.stringify({ models: _modelConsentNorm().models, publishers: _modelConsentNorm().publishers })', sandbox));
}

test('toggle, remove and add model update the state and re-render', () => {
  const panel = makeStubElement();
  const nameIn = inputStub(); const pubIn = inputStub();
  const sandbox = loadApp({ modelConsentPanel: panel, mcNewModelName: nameIn, mcNewModelPublisher: pubIn });
  setConsent(sandbox, { models: [{ name: 'a', publisher: 'p', allowed: true }], publishers: ['p'] });
  vm.runInContext('window.modelConsentToggleModel(0, false);', sandbox);
  assert.equal(state(sandbox).models[0].allowed, false, 'toggle off');
  assert.ok(panel.innerHTML.includes('model-consent-model'), 'toggle did not re-render');
  vm.runInContext('window.modelConsentRemoveModel(0);', sandbox);
  assert.equal(state(sandbox).models.length, 0, 'remove model');
  nameIn.value = 'new-model';
  pubIn.value = 'new-pub';
  vm.runInContext('window.modelConsentAddModel();', sandbox);
  const st = state(sandbox);
  assert.equal(st.models.length, 1, 'add model');
  assert.equal(st.models[0].name, 'new-model');
  assert.equal(st.models[0].publisher, 'new-pub');
  assert.equal(st.models[0].allowed, true, 'an added model is pull-allowed by default');
});

test('adding a model with an empty name is a no-op', () => {
  const sandbox = loadApp({ mcNewModelName: makeStubElement() });
  setConsent(sandbox, { models: [], publishers: [] });
  vm.runInContext('window.modelConsentAddModel();', sandbox);
  assert.equal(state(sandbox).models.length, 0, 'an empty name must not add a model');
});

test('adding a duplicate publisher is a no-op', () => {
  const sandbox = loadApp({ mcNewPublisher: inputStub() });
  setConsent(sandbox, { models: [], publishers: ['qwen'] });
  const el = sandbox.document.getElementById('mcNewPublisher');
  el.value = 'qwen';
  vm.runInContext('window.modelConsentAddPublisher();', sandbox);
  assert.deepEqual(state(sandbox).publishers, ['qwen'], 'duplicate publisher added twice');
});

// ── save ────────────────────────────────────────────────────────────────────

test('save PUTs the whole record to /api/automation/model-consent and reports the result', async () => {
  const calls = [];
  const panel = makeStubElement();
  const sandbox = loadApp({ modelConsentPanel: panel });
  setConsent(sandbox, { models: [{ name: 'qwen3:8b', publisher: 'qwen', allowed: true }], publishers: ['qwen'] });
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: opts && opts.method, body: opts && opts.body });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ models: sandbox._consent.models, publishers: sandbox._consent.publishers }) });
  };
  await vm.runInContext('window.saveModelConsent()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.equal(calls.length, 1, 'one request');
  assert.equal(calls[0].url, '/api/automation/model-consent', calls[0].url);
  assert.equal(calls[0].method, 'PUT', 'save must PUT');
  const body = JSON.parse(calls[0].body);
  assert.equal(body.models[0].name, 'qwen3:8b');
  assert.deepEqual(body.publishers, ['qwen']);
  assert.ok(panel.innerHTML.includes('Model pull consent saved.'), 'success status missing: ' + panel.innerHTML);
});

test('a failed save keeps the editor and shows the error, escaped', async () => {
  const panel = makeStubElement();
  const sandbox = loadApp({ modelConsentPanel: panel });
  setConsent(sandbox, { models: [{ name: 'm', allowed: true }], publishers: [] });
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 500, text: () => Promise.resolve('boom ' + HOSTILE) });
  await vm.runInContext('window.saveModelConsent()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Could not save the model pull consent'), 'error status missing: ' + panel.innerHTML);
  assert.ok(!panel.innerHTML.includes('<img'), 'an image element was injected');
});

test('a failed load fills the panel with the escaped error', async () => {
  const panel = makeStubElement();
  const sandbox = loadApp({ modelConsentPanel: panel });
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 500, text: () => Promise.resolve('down') });
  await vm.runInContext('window.loadModelConsentPanel()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Could not load the model pull consent'), 'error missing: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('down'), panel.innerHTML);
});

test('a successful load renders the server record into the panel', async () => {
  const panel = makeStubElement();
  const sandbox = loadApp({ modelConsentPanel: panel });
  const body = { models: [{ name: 'qwen3:8b', publisher: 'qwen', allowed: true }], publishers: ['qwen'] };
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  await vm.runInContext('window.loadModelConsentPanel()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Model pull consent'), 'editor missing: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('qwen3:8b'), panel.innerHTML);
  assert.ok(panel.innerHTML.includes('>qwen <button'), panel.innerHTML);
});

// ── wiring + locales ────────────────────────────────────────────────────────

test('the setup view wires the model consent panel and the editor source never stores an interval on an element', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("id=\"modelConsentPanel\""), 'the settings panel is not in the setup view');
  const start = src.indexOf('_modelConsentNorm');
  const end = src.indexOf('Compute node backend versions', start);
  const block = src.slice(start, end);
  assert.ok(!/setInterval/.test(block), 'the consent editor must not start its own interval');
});

test('docsLinkForKey maps the consent card and its parts to the glossary section', () => {
  const base = 'datawatch-definitions.md';
  for (const k of ['model_consent', 'model_consent_card_title', 'model_consent_models', 'model_consent_publishers']) {
    assert.equal(docsLink(k), base + '#model-pull-consent', k);
  }
});

test('every model_consent_ key is in all five bundles with a real translation', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith(VIEW_PREFIX));
  assert.ok(keys.length >= 20, 'expected the full key set, got ' + keys.length);
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
