// Minimum-Ollama capability overrides (web settings) — PWA side.
//
// loadRequirementsMinOllamaPanel reads the autonomous.requirements block from
// GET /api/config into window._reqMinOllamaState and renders the editor: one
// minimum-version field per known capability (empty = the built-in floor
// applies) plus save / cancel. saveRequirementsMinOllama PUTs the whole block
// back as {"autonomous.requirements":{"min_ollama":{...}}} so the server's
// one shared rule set validates it. Every value is escaped, the card is a
// real settings section of the Automata tab, and every
// requirements_min_ollama_ locale key is pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const VIEW_PREFIX = 'requirements_min_ollama_';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

// inputStub is a form-field stand-in whose value actually round-trips
// (makeStubElement's value is fixed to '').
function inputStub(initial) {
  let v = initial ? String(initial) : '';
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

function setState(sandbox, capabilities, minOllama) {
  vm.runInContext(
    'window._reqMinOllamaState = { capabilities: _caps.slice(), min_ollama: Object.assign({}, _mins) };',
    Object.assign(sandbox, { _caps: capabilities || [], _mins: minOllama || {} }));
}

function editor(sandbox) {
  return vm.runInContext('window.requirementsMinOllamaEditorHtml()', sandbox);
}

// ── rendering ───────────────────────────────────────────────────────────────

test('the editor shows one minimum-version field per known capability', () => {
  const sandbox = loadApp();
  setState(sandbox, ['classification_verdict', 'code_generation'], { code_generation: '0.9.1' });
  const html = editor(sandbox);
  assert.ok(html.includes('Minimum Ollama overrides'), 'card title missing: ' + html);
  assert.ok(html.includes('classification_verdict'), 'a capability row is missing: ' + html);
  assert.ok(html.includes('code_generation'), 'the second capability row is missing');
  assert.ok(html.includes('req-min-ollama-value'), 'the version inputs are missing');
  assert.ok(html.includes('value="0.9.1"'), 'the stored override is not shown in its field');
  assert.ok(html.includes('placeholder="empty = built-in floor"'), 'the clear hint placeholder is missing');
  assert.ok(html.includes('req-min-ollama-save'), 'the save button is missing');
  assert.ok(html.includes('req-min-ollama-refresh'), 'the refresh button is missing');
});

test('no known capabilities renders the empty message', () => {
  const sandbox = loadApp();
  setState(sandbox, [], {});
  const html = editor(sandbox);
  assert.ok(html.includes('No known capabilities were reported.'), html);
});

test('capability names and stored versions are escaped when rendered', () => {
  const sandbox = loadApp();
  setState(sandbox, [HOSTILE], { [HOSTILE]: HOSTILE });
  const html = editor(sandbox);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

test('a save failure message is shown in the status line, escaped', () => {
  const sandbox = loadApp();
  setState(sandbox, ['classification_verdict'], {});
  sandbox._st = { msg: 'boom ' + HOSTILE, error: true };
  vm.runInContext('window._reqMinOllamaStatus = _st;', sandbox);
  const html = editor(sandbox);
  assert.ok(html.includes('req-min-ollama-status'), 'status line missing: ' + html);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped');
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── save ────────────────────────────────────────────────────────────────────

test('save PUTs the whole autonomous.requirements block and reports the result', async () => {
  const calls = [];
  const panel = makeStubElement();
  const clsIn = inputStub('');      // cleared by the user
  const codeIn = inputStub('0.9.1'); // set by the user
  const sandbox = loadApp({
    requirementsMinOllamaPanel: panel,
    reqMinOllamaVal_classification_verdict: clsIn,
    reqMinOllamaVal_code_generation: codeIn,
  });
  setState(sandbox, ['classification_verdict', 'code_generation'], {});
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: opts && opts.method, body: opts && opts.body });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
  };
  await vm.runInContext('window.saveRequirementsMinOllama()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.equal(calls.length, 1, 'one request');
  assert.equal(calls[0].url, '/api/config', calls[0].url);
  assert.equal(calls[0].method, 'PUT', 'save must PUT');
  const body = JSON.parse(calls[0].body);
  assert.ok(body['autonomous.requirements'], 'the whole autonomous.requirements block is the payload');
  const mins = body['autonomous.requirements'].min_ollama;
  assert.equal(mins.classification_verdict, '', 'an empty field clears that capability');
  assert.equal(mins.code_generation, '0.9.1', 'the entered version is sent');
  assert.ok(panel.innerHTML.includes('Minimum Ollama overrides saved.'), 'success status missing: ' + panel.innerHTML);
});

test('a failed save keeps the editor and shows the error, escaped', async () => {
  const panel = makeStubElement();
  const codeIn = inputStub('0.9.1');
  const sandbox = loadApp({ requirementsMinOllamaPanel: panel, reqMinOllamaVal_code_generation: codeIn });
  setState(sandbox, ['code_generation'], {});
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 500, text: () => Promise.resolve('boom ' + HOSTILE) });
  await vm.runInContext('window.saveRequirementsMinOllama()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Saving failed:'), 'error status missing: ' + panel.innerHTML);
  assert.ok(!panel.innerHTML.includes('<img'), 'an image element was injected');
});

// ── load ────────────────────────────────────────────────────────────────────

test('a failed load fills the panel with the escaped error', async () => {
  const panel = makeStubElement();
  const sandbox = loadApp({ requirementsMinOllamaPanel: panel });
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 500, text: () => Promise.resolve('down ' + HOSTILE) });
  await vm.runInContext('window.loadRequirementsMinOllamaPanel()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Loading failed:'), 'error missing: ' + panel.innerHTML);
  assert.ok(!panel.innerHTML.includes('<img'), 'an image element was injected');
});

test('a successful load renders the server block into the panel', async () => {
  const panel = makeStubElement();
  const sandbox = loadApp({ requirementsMinOllamaPanel: panel });
  const body = {
    autonomous: {
      requirements: {
        capabilities: ['classification_verdict', 'code_generation'],
        min_ollama: { code_generation: '0.9.1' },
      },
    },
  };
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  await vm.runInContext('window.loadRequirementsMinOllamaPanel()', sandbox);
  await new Promise(res => setImmediate(res));
  assert.ok(panel.innerHTML.includes('Minimum Ollama overrides'), 'editor missing: ' + panel.innerHTML);
  assert.ok(panel.innerHTML.includes('value="0.9.1"'), 'the stored override is not rendered');
});

// ── wiring + locales ────────────────────────────────────────────────────────

test('the Automata settings tab wires the min-ollama panel', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes('id="requirementsMinOllamaPanel"'), 'the settings panel is not in the settings view');
  assert.ok(src.includes("loadRequirementsMinOllamaPanel()"), 'the panel is never loaded');
});

test('every requirements_min_ollama_ key is in all five bundles with a real translation', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith(VIEW_PREFIX));
  assert.ok(keys.length >= 8, 'expected the full key set, got ' + keys.length);
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
