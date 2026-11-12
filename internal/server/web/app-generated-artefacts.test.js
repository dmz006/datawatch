// Generated artefacts (PWA): one toggle per setting in the Automata
// settings, and a read-only change-record summary with a Regenerate
// button on the Automaton detail. Loads the real app.js into the stub
// browser and exercises the real functions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const NAMES = ['change_record', 'gate_report', 'changelog_draft', 'parity_draft', 'tracker_rows', 'registry_row', 'combined_docs_task'];

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}
function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('the Automata settings have one default-on toggle per generated artefact', () => {
  const sandbox = loadAppJS();
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sandbox));
  const group = fields.find(f => f.type === 'group' && f.labelKey === 'settings_generated_artefacts_group');
  assert.ok(group, 'Generated artefacts group missing');
  for (const n of NAMES) {
    const f = fields.find(x => x.key === 'autonomous.generated_artefacts.' + n);
    assert.ok(f, n + ' toggle missing');
    assert.equal(f.type, 'toggle');
    assert.equal(f.default, true);
    assert.equal(f.labelKey, 'settings_generated_' + n);
    assert.equal(f.helpKey, 'settings_generated_' + n + '_help');
  }
});

test('the detail block has a Regenerate button and a kind picker, all through t()', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_prdGeneratedArtefactsBlock', { id: 'p1' });
  assert.match(html, /prd_change_record_title/);
  assert.match(html, /prd_generated_regenerate/);
  assert.match(html, /_regenerateArtefact\(/);
  for (const k of ['gate_report', 'changelog_draft', 'parity_draft', 'tracker_rows', 'registry_row']) {
    assert.match(html, new RegExp('value="' + k + '"'));
    assert.match(html, new RegExp('prd_generated_kind_' + k));
  }
  assert.equal(call(sandbox, '_prdGeneratedArtefactsBlock', {}), '');
});

test('the change-record summary is read-only and counts each list', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_changeRecordSummaryHtml', {
    files: [{ path: 'a' }, { path: 'b' }], packages: ['p'], settings_added: [], routes_added: ['r1', 'r2', 'r3'],
    locale_keys_added: [{ key: 'k' }], tests_added: ['T1'],
  });
  assert.match(html, /prd_change_record_files: 2/);
  assert.match(html, /prd_change_record_packages: 1/);
  assert.match(html, /prd_change_record_settings: 0/);
  assert.match(html, /prd_change_record_routes: 3/);
  assert.match(html, /prd_change_record_locale_keys: 1/);
  assert.match(html, /prd_change_record_tests: 1/);
  assert.doesNotMatch(html, /<button|<input/);
  assert.match(call(sandbox, '_changeRecordSummaryHtml', null), /prd_change_record_empty/);
});

test('Regenerate posts to the regenerate route for the chosen kind', async () => {
  const sandbox = loadAppJS();
  const calls = [];
  sandbox.__calls = calls;
  vm.runInContext(`
    window.apiFetch = apiFetch = function(p, o) { __calls.push({ p, o }); return Promise.resolve({ status: 'written' }); };
    showToast = function(m, kind) { __calls.push({ toast: m, kind }); };
  `, sandbox);
  await call(sandbox, 'window._regenerateArtefact', 'p1', 'parity_draft');
  assert.equal(calls[0].p, '/api/autonomous/prds/p1/generated/parity_draft/regenerate');
  assert.equal(calls[0].o.method, 'POST');
  assert.ok(calls.some(c => c.toast && c.toast.includes('prd_generated_regenerate_done') && c.kind === 'success'));
});

test('every new key is in all five locale bundles with the same keys', () => {
  const keys = [];
  for (const n of NAMES) keys.push('settings_generated_' + n, 'settings_generated_' + n + '_help');
  keys.push('settings_generated_artefacts_group', 'prd_change_record_title', 'prd_generated_regenerate', 'prd_generated_regenerate_failed');
  const en = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const d = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, l + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(d[k], l + ' missing ' + k);
      if (l !== 'en') assert.notEqual(d[k], en[k], l + ' not translated: ' + k);
      assert.doesNotMatch(d[k], /\bPRD\b/);
    }
  }
});
