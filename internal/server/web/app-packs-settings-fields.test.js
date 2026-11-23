// Pins the standard task packs fields in Settings > General: the enabled
// switch, the active order list and the pack input fields. The fields come
// from app-packs-settings-fields.js and sit in GENERAL_CONFIG_FIELDS. The
// enabled switch text names the registry requirement, saves go to
// /api/packs/config and /api/packs/inputs, a refusal shows inline, and every
// string exists in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const HELP_DOCS = 'howto/automata-standard-tasks.md';

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-packs-settings-fields.js'), sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

function packsField(sandbox, key) {
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'packs') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

const LABEL_KEYS = [
  'settings_packs', 'settings_packs_enabled', 'settings_packs_enabled_help',
  'settings_packs_active', 'settings_packs_active_help',
  'settings_packs_inputs_level', 'settings_packs_inputs_pack', 'settings_packs_inputs_key',
  'settings_packs_inputs_values', 'settings_packs_inputs_help',
  'settings_packs_error_refused', 'settings_packs_error_network', 'settings_packs_error_level',
  'settings_packs_error_pack', 'settings_packs_error_project_key', 'settings_packs_error_pairs',
];

test('the packs card sits in the general settings with a docs link', () => {
  const sandbox = loadApp();
  const sec = JSON.parse(vm.runInContext("JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'packs') || null)", sandbox));
  assert.ok(sec, 'packs section missing from GENERAL_CONFIG_FIELDS');
  assert.equal(sec.sectionKey, 'settings_packs');
  assert.equal(sec.docs, HELP_DOCS);
  assert.ok(sec.fields.length >= 6);
});

test('the enabled switch is a toggle that saves through savePacksEnabled and names the registry requirement', () => {
  const sandbox = loadApp();
  const f = packsField(sandbox, 'packs.enabled');
  assert.ok(f, 'packs.enabled missing');
  assert.equal(f.type, 'toggle');
  assert.equal(f.labelKey, 'settings_packs_enabled');
  assert.equal(f.helpDocs, HELP_DOCS);
  assert.equal(f.onchange, 'savePacksEnabled(this)');
  const en = bundle('en');
  assert.match(en.settings_packs_enabled, /registry/i);
  assert.match(en.settings_packs_enabled_help, /skills registry source is connected/);
});

test('the active list is a text field that saves through savePacksActive', () => {
  const sandbox = loadApp();
  const f = packsField(sandbox, 'packs.active');
  assert.ok(f, 'packs.active missing');
  assert.equal(f.type, 'text');
  assert.equal(f.placeholder, 'builtin');
  assert.equal(f.onchange, 'savePacksActive(this)');
});

test('the input fields exist and only the values field saves', () => {
  const sandbox = loadApp();
  for (const k of ['level', 'pack', 'key']) {
    const f = packsField(sandbox, 'packs.inputs.' + k);
    assert.ok(f, k + ' input field missing');
    assert.equal(f.onchange, '_packsInputChanged(this)', k + ' must not save on its own');
  }
  assert.equal(packsField(sandbox, 'packs.inputs.values').onchange, 'savePacksInputs(this)');
});

test('parsers keep order, drop blanks and reject a pair with no name or equals sign', () => {
  const sandbox = loadApp();
  assert.deepEqual(JSON.parse(vm.runInContext("JSON.stringify(parsePacksActive(' builtin, , my-pack ,'))", sandbox)), ['builtin', 'my-pack']);
  assert.deepEqual(JSON.parse(vm.runInContext("JSON.stringify(parsePacksActive(''))", sandbox)), []);
  assert.deepEqual(JSON.parse(vm.runInContext("JSON.stringify(parsePacksInputs('a=1; b=two=2;'))", sandbox)), { a: '1', b: 'two=2' });
  assert.equal(vm.runInContext("parsePacksInputs('novalue')", sandbox), null);
  assert.equal(vm.runInContext("parsePacksInputs('=x')", sandbox), null);
});

test('saving the switch and the list PUTs /api/packs/config and a refusal restores the switch', async () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url, opts });
    return Promise.resolve({ ok: false, text: () => Promise.resolve(JSON.stringify({ error: 'packs need at least one connected skills registry source' })) });
  };
  vm.runInContext('globalThis.fetch = fetch; var _el = {checked: true, value: "builtin, a", parentElement: null};', sandbox);
  await vm.runInContext('savePacksEnabled(_el)', sandbox);
  assert.equal(vm.runInContext('_el.checked', sandbox), false, 'refused switch must go back off');
  assert.match(calls[0].url, /\/api\/packs\/config$/);
  assert.equal(calls[0].opts.method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].opts.body), { enabled: true });
  await vm.runInContext('savePacksActive(_el)', sandbox);
  assert.deepEqual(JSON.parse(calls[1].opts.body), { active: ['builtin', 'a'] });
});

test('saving inputs checks level, pack and project key before sending', () => {
  const sandbox = loadApp();
  let sent = 0;
  sandbox.fetch = () => { sent++; return Promise.resolve({ ok: true }); };
  vm.runInContext('globalThis.fetch = fetch;', sandbox);
  // No input boxes exist in the stub document, so level defaults to global and pack is empty.
  assert.equal(vm.runInContext('savePacksInputs({value: "a=1", parentElement: null})', sandbox), null);
  assert.equal(sent, 0);
});

test('a refusal body reads as the daemon message, plain text or the generic line', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext("_packsRefusalText(JSON.stringify({error: 'nope'}))", sandbox), 'nope');
  assert.equal(vm.runInContext("_packsRefusalText('plain')", sandbox), 'plain');
  assert.equal(vm.runInContext("_packsRefusalText('')", sandbox), 'The daemon did not save this value. Nothing changed.');
});

test('the help link points at the standard tasks howto, which exists', () => {
  const sandbox = loadApp();
  const link = vm.runInContext('settingsHelpLink(' + JSON.stringify(packsField(sandbox, 'packs.enabled')) + ')', sandbox);
  assert.match(link, /href="\/diagrams\.html#docs\/howto\/automata-standard-tasks\.md"/);
  assert.ok(fs.existsSync(path.join(__dirname, '..', '..', '..', 'docs', HELP_DOCS)));
});

test('every packs string exists in all five locale bundles', () => {
  for (const locale of LOCALES) {
    const b = bundle(locale);
    for (const k of LABEL_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + locale);
      assert.ok(!/PRD/.test(b[k]), k + ' must not use the word PRD in ' + locale);
    }
  }
});

test('the script is loaded before app.js and cached by the service worker', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const a = html.indexOf('/app-packs-settings-fields.js');
  const b = html.indexOf('/app.js?');
  assert.ok(a > 0 && a < b);
  assert.match(fs.readFileSync(path.join(__dirname, 'sw.js'), 'utf8'), /app-packs-settings-fields\.js/);
});
