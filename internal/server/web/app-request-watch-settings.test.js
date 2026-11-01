// Pins the model request watch settings: the eleven autonomous.request_watch.*
// fields in the Automata settings list (right type, label, hint and docs keys),
// one help link per key in the exact anchor form, and every label, hint and
// group string in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const TYPES = {
  enabled: 'toggle',
  poll_seconds: 'number',
  no_progress_minutes: 'number',
  observe_workers: 'toggle',
  observe_verifiers: 'toggle',
  use_gpu_signal: 'toggle',
  gpu_busy_percent: 'number',
  unreachable_counts: 'toggle',
  raise_alert: 'toggle',
  raise_question: 'toggle',
  remediation_options: 'text',
};
const NAMES = Object.keys(TYPES);

function load() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function autonomousFields(sandbox) {
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sandbox));
}

function docsLinks(sandbox) {
  return JSON.parse(vm.runInContext('JSON.stringify(DOCS_LINKS_BY_KEY)', sandbox));
}

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

test('all eleven request watch fields exist with the right type and keys', () => {
  const fields = autonomousFields(load());
  for (const n of NAMES) {
    const f = fields.find(x => x.key === 'autonomous.request_watch.' + n);
    assert.ok(f, 'missing field autonomous.request_watch.' + n);
    assert.equal(f.type, TYPES[n], n + ' type');
    assert.equal(f.labelKey, 'settings_request_watch_' + n, n + ' labelKey');
    assert.equal(f.hintKey, 'settings_request_watch_' + n + '_hint', n + ' hintKey');
    assert.equal(f.docsKey, 'autonomous.request_watch.' + n, n + ' docsKey');
  }
});

test('number fields carry min and max and the list field is comma separated', () => {
  const fields = autonomousFields(load());
  const get = n => fields.find(x => x.key === 'autonomous.request_watch.' + n);
  assert.equal(get('poll_seconds').min, 1);
  assert.equal(get('no_progress_minutes').min, 1);
  assert.equal(get('gpu_busy_percent').min, 1);
  assert.equal(get('gpu_busy_percent').max, 100);
  for (const n of ['poll_seconds', 'no_progress_minutes', 'gpu_busy_percent']) {
    assert.equal(typeof get(n).max, 'number', n + ' max');
  }
  assert.equal(get('remediation_options').csv, true);
});

test('the request watch group header follows the infrastructure retry fields', () => {
  const fields = autonomousFields(load());
  const group = fields.findIndex(f => f.type === 'group' && f.labelKey === 'settings_request_watch_group');
  assert.ok(group >= 0, 'group header missing');
  assert.equal(fields[group].docsKey, 'settings_request_watch');
  const lastRetry = Math.max(...fields.map((f, i) => (f.key || '').startsWith('autonomous.infra_retry.') ? i : -1));
  assert.ok(group > lastRetry, 'group header must come after the infrastructure retry fields');
  assert.equal(fields[group + 1].key, 'autonomous.request_watch.enabled');
});

test('every request watch key has a help link in the exact anchor form', () => {
  const links = docsLinks(load());
  for (const n of NAMES) {
    const k = 'autonomous.request_watch.' + n;
    assert.equal(links[k], 'datawatch-definitions.md#autonomous-request-watch-' + n.replace(/_/g, '-'), k);
  }
  assert.equal(links.settings_request_watch, 'datawatch-definitions.md#model-request-progress');
});

test('every label, hint and group string exists in all five bundles', () => {
  const keys = ['settings_request_watch_group'];
  for (const n of NAMES) keys.push('settings_request_watch_' + n, 'settings_request_watch_' + n + '_hint');
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ' missing ' + k);
      assert.ok(!/\bPRDs?\b/.test(b[k]), l + ' ' + k + ' says PRD');
      assert.ok(!/\b(BL|GH|B)\d+\b/.test(b[k]), l + ' ' + k + ' has a tracker ID');
    }
  }
});
