// Pending item settings — PWA side.
//
// The Pending items group in the Automata settings card has one field for each of
// the nine autonomous.automation.pending_item_* keys. This file checks that every
// field is declared with its key and label key, that the selects offer exactly the
// allowed words, that the group and every field carry a help link that resolves to
// a DOCS_LINKS_BY_KEY entry, and that all five locale bundles carry every key the
// fields use.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const PREFIX = 'autonomous.automation.';

// The nine settings keys and the help anchor each one points at.
const KEYS = [
  { key: 'pending_item_policy', docs: 'settings_pending_item_policy', anchor: '#pending-item-policy' },
  { key: 'pending_item_auto_resume', docs: 'settings_pending_item_auto_resume', anchor: '#pending-item-auto-resume' },
  { key: 'pending_item_source.path', docs: 'settings_pending_item_source_path', anchor: '#pending-item-file-registry-path' },
  { key: 'pending_item_source.id_pattern', docs: 'settings_pending_item_source_id_pattern', anchor: '#pending-item-file-registry-id-pattern' },
  { key: 'pending_item_source.id_column', docs: 'settings_pending_item_source_id_column', anchor: '#pending-item-file-registry-id-column' },
  { key: 'pending_item_source.title_column', docs: 'settings_pending_item_source_title_column', anchor: '#pending-item-file-registry-title-column' },
  { key: 'pending_item_source.state_column', docs: 'settings_pending_item_source_state_column', anchor: '#pending-item-file-registry-state-column' },
  { key: 'pending_item_source.done_values', docs: 'settings_pending_item_source_done_values', anchor: '#pending-item-file-registry-done-values' },
  { key: 'pending_item_source.on_missing_row', docs: 'settings_pending_item_source_on_missing_row', anchor: '#pending-item-file-registry-missing-rows' },
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

const sandbox = loadApp();
const sections = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
const links = vm.runInContext('DOCS_LINKS_BY_KEY', sandbox, { filename: 'app.js' });
const autonomous = sections.find((sec) => sec.id === 'autonomous');

function field(key) {
  return autonomous.fields.find((f) => f.key === PREFIX + key);
}

function group() {
  return autonomous.fields.find((f) => f.type === 'group' && f.labelKey === 'settings_pending_items_group');
}

// Every locale key the group and the nine fields reference.
function usedLocaleKeys() {
  const keys = [group().labelKey];
  for (const k of KEYS) {
    const f = field(k.key);
    keys.push(f.labelKey);
    for (const v of Object.values(f.optionLabelKeys || {})) keys.push(v);
  }
  return keys;
}

test('the Pending items group sits next to the uncommitted-work group', () => {
  const g = group();
  assert.ok(g, 'group row is declared');
  assert.equal(g.docsKey, 'settings_pending_items');
  const idx = autonomous.fields.indexOf(g);
  const unc = autonomous.fields.findIndex((f) => f.key === PREFIX + 'uncommitted_work_scope');
  assert.ok(unc >= 0 && idx === unc + 1, 'group follows the uncommitted-work fields');
});

test('every key has a field with its key, a label and a label key', () => {
  for (const k of KEYS) {
    const f = field(k.key);
    assert.ok(f, k.key + ' field is declared');
    assert.ok(f.label && f.label.length > 0, k.key + ' has a fallback label');
    assert.ok(f.labelKey && f.labelKey.startsWith('settings_automation_pending_item_'), k.key + ' has a label key');
  }
});

test('field types match the kind of value each key holds', () => {
  assert.equal(field('pending_item_policy').type, 'select');
  assert.equal(field('pending_item_auto_resume').type, 'toggle');
  for (const k of ['source.path', 'source.id_pattern', 'source.id_column', 'source.title_column', 'source.state_column']) {
    assert.equal(field('pending_item_' + k).type, 'text', k);
  }
  const done = field('pending_item_source.done_values');
  assert.equal(done.type, 'text');
  assert.equal(done.csv, true, 'done_values is the list-of-text field');
  assert.equal(field('pending_item_source.on_missing_row').type, 'select');
});

test('the selects offer exactly the allowed words and label each one', () => {
  const cases = [
    { key: 'pending_item_policy', options: ['hold', 'skip'], def: 'hold' },
    { key: 'pending_item_source.on_missing_row', options: ['keep', 'drop'], def: 'keep' },
  ];
  const en = bundle('en');
  for (const c of cases) {
    const f = field(c.key);
    assert.deepEqual(Array.from(f.options), c.options, c.key + ' options');
    assert.equal(f.default, c.def, c.key + ' default');
    assert.deepEqual(Object.keys(f.optionLabelKeys).sort(), c.options.slice().sort(), c.key + ' option label keys');
    for (const word of c.options) {
      const text = en[f.optionLabelKeys[word]];
      assert.ok(text && text.startsWith(word), c.key + ' option label for ' + word + ' leads with the word');
    }
  }
});

test('the defaults shown match the documented defaults', () => {
  assert.equal(field('pending_item_source.id_pattern').default, '^\\S+$');
  assert.equal(field('pending_item_source.id_column').default, '1');
  assert.equal(field('pending_item_source.title_column').default, '2');
  assert.equal(field('pending_item_source.state_column').default, undefined, 'state column defaults to none');
  assert.equal(field('pending_item_source.path').default, undefined, 'path defaults to no file registry');
});

test('the group and every field have a help link that resolves to a DOCS_LINKS_BY_KEY entry', () => {
  assert.equal(links[group().docsKey], 'datawatch-definitions.md#pending-items');
  for (const k of KEYS) {
    const f = field(k.key);
    assert.equal(f.docsKey, k.docs, k.key + ' docs key');
    assert.equal(links[f.docsKey], 'datawatch-definitions.md' + k.anchor, k.key + ' resolves to its anchor');
    const html = vm.runInContext('defsLink(' + JSON.stringify(f.docsKey) + ')', sandbox);
    assert.ok(html.includes('datawatch-definitions.md' + k.anchor), k.key + ' help link points at its anchor');
  }
});

test('all five bundles carry every key the group and the fields use', () => {
  const keys = usedLocaleKeys();
  assert.ok(keys.length >= 14, 'group, nine labels and four option labels');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const key of keys) {
      assert.ok(typeof b[key] === 'string' && b[key].length > 0, lang + ' is missing ' + key);
    }
  }
});

test('the English bundle text never says PRD', () => {
  const en = bundle('en');
  for (const key of usedLocaleKeys()) {
    assert.ok(!/\bPRDs?\b/.test(en[key]), key + ' must not say PRD');
  }
});

test('the non-English bundles carry the English text until translations arrive', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter((l) => l !== 'en')) {
    const b = bundle(lang);
    for (const key of usedLocaleKeys()) {
      assert.equal(b[key], en[key], lang + ' ' + key);
    }
  }
});

test('the fallback label matches the English bundle for each field', () => {
  const en = bundle('en');
  for (const k of KEYS) {
    const f = field(k.key);
    assert.equal(f.label, en[f.labelKey], k.key);
  }
  assert.equal(group().label, en[group().labelKey]);
});
