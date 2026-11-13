// Spec check block: the create and revise forms show the spec_check findings
// the daemon returns, every label through t(), nothing for a clean spec.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadAppJS(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext('window._i18n.bundle = ' + JSON.stringify(en), sandbox);
  return sandbox;
}
function call(sandbox, fn, ...args) {
  sandbox.__args = args;
  const r = vm.runInContext(`${fn}(...__args)`, sandbox);
  delete sandbox.__args;
  return r;
}

const WARN = { rule: 'names_tracker_id', phrase: 'BL123', severity: 'warning', correctable: true, suggestion: "remove 'BL123'" };
const ERR = { rule: 'private-address', phrase: '10.0.0.5', severity: 'error', correctable: false, suggestion: 'use a host name' };

test('SpecCheck renders phrase, rule, suggestion and the daemon-can-fix note', () => {
  const html = call(loadAppJS(), 'renderSpecCheck', [WARN]);
  assert.match(html, /BL123/);
  assert.match(html, /names_tracker_id/);
  assert.match(html, /remove &#39;BL123&#39;|remove 'BL123'/);
  assert.match(html, /The daemon can fix this wording itself/);
  assert.match(html, /Spec check/);
});

test('SpecCheck an error finding carries no daemon-fix note', () => {
  const html = call(loadAppJS(), 'renderSpecCheck', [ERR]);
  assert.match(html, /10\.0\.0\.5/);
  assert.doesNotMatch(html, /daemon can fix/);
});

test('SpecCheck renders nothing for a clean or missing list', () => {
  const sb = loadAppJS();
  assert.equal(call(sb, 'renderSpecCheck', []), '');
  assert.equal(call(sb, 'renderSpecCheck', undefined), '');
  assert.equal(call(sb, 'renderSpecCheck', 'x'), '');
});

test('SpecCheck escapes server-supplied text', () => {
  const html = call(loadAppJS(), 'renderSpecCheck', [{ ...WARN, phrase: '<img src=x>' }]);
  assert.doesNotMatch(html, /<img/);
});

test('SpecCheck locale keys exist in all five bundles', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of ['spec_check_title', 'spec_check_severity_warning', 'spec_check_severity_error', 'spec_check_fixable', 'spec_check_suggestion']) {
      assert.ok(b[k], `${l} lacks ${k}`);
    }
  }
});

test('SpecCheck wizard create hands the response findings to the render helper', async () => {
  const sb = loadAppJS({ wizardIntent: { value: 'Fix BL123' }, selectedDirDisplay: { textContent: '/work/proj' } });
  const seen = [];
  sb.renderSpecCheck = f => { seen.push(f); return ''; };
  sb.apiFetch = url => Promise.resolve(url === '/api/autonomous/prds' ? { id: 'a1', spec_check: [WARN] } : {});
  sb._prdCloseModal = () => {}; sb.renderPRDDetailView = () => {}; sb.showToast = () => {};
  vm.runInContext('_wizardSubmit()', sb);
  await flushAsync();
  assert.deepEqual(seen[0], [WARN]);
});

test('SpecCheck revise action hands the response findings to the render helper', async () => {
  const sb = loadAppJS();
  const seen = [];
  sb.renderSpecCheck = f => { seen.push(f); return ''; };
  sb.apiFetch = () => Promise.resolve({ id: 'a1', spec_check: [WARN] });
  sb.showToast = () => {}; sb._refreshAutomataOrPRD = () => {};
  call(sb, 'prdAction', 'a1', 'request_revision', 'POST', { note: 'n' });
  await flushAsync();
  assert.deepEqual(seen[0], [WARN]);
});
