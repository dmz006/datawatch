// Workload roles — PWA side.
//
// The LLM edit form shows and edits an entry's `roles` (comma separated) and
// sends them on save; the Settings → Automate "Workload Roles" card lists each
// role with the entry GET /api/llm-roles says it resolves to, plus any
// contention warning. Every inserted value is escaped.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// formDom reads the roles input back out of rendered markup; other fields are
// absent (null), which collectLLMForm tolerates.
function formDom(html, rolesOverride) {
  const tag = /<input\b[^>]*\bid="fe_llm_roles"[^>]*>/.exec(html);
  const el = { value: '' };
  if (tag) {
    const v = /\bvalue="([^"]*)"/.exec(tag[0]);
    el.value = v ? v[1].replace(/&amp;/g, '&') : '';
  }
  if (rolesOverride !== undefined) el.value = rolesOverride;
  return id => (id === 'fe_llm_roles' ? el : null);
}

function collect(sandbox, html, original, rolesOverride) {
  sandbox.document.getElementById = formDom(html, rolesOverride);
  sandbox._original = original || {};
  return JSON.parse(vm.runInContext('JSON.stringify(collectLLMForm(_original))', sandbox));
}

test('the editor renders the saved roles and posts them back on save', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'a', kind: 'ollama', roles: ['planning', 'review'] };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  assert.ok(/id="fe_llm_roles"[^>]*value="planning, review"/.test(html), 'roles not rendered');
  assert.equal(collect(sandbox, html, sandbox._llm).roles.join(','), 'planning,review');
  // editing the input changes what is sent; blanks are dropped
  const edited = collect(sandbox, html, sandbox._llm, ' planning , ,coding ');
  assert.deepEqual(edited.roles, ['planning', 'coding']);
});

test('clearing the roles box sends an empty list so the stored roles are cleared', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'a', kind: 'ollama', roles: ['planning'] };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  assert.deepEqual(collect(sandbox, html, sandbox._llm, '').roles, []);
});

test('an entry with no roles renders cleanly', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'a', kind: 'ollama' };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  assert.ok(/id="fe_llm_roles"[^>]*value=""/.test(html));
  assert.ok(!html.includes('undefined'));
  assert.ok(!html.includes('llm-role-warning'));
  assert.deepEqual(collect(sandbox, html, sandbox._llm).roles, []);
});

test('the editor shows contention warnings that name the entry, escaped', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'a', kind: 'ollama', roles: ['x'], role_warnings: ['role "x" <b>claimed</b> twice'] };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  assert.ok(html.includes('llm-role-warning'));
  assert.ok(html.includes('&lt;b&gt;claimed&lt;/b&gt;'));
  assert.ok(!html.includes('<b>claimed</b>'));
});

test('the Workload Roles card lists each role with its resolved entry', () => {
  const sandbox = loadApp();
  sandbox._data = { roles: [{ role: 'planning', entry: 'big-model' }, { role: 'review', entry: 'small' }], warnings: [] };
  const html = vm.runInContext('buildWorkloadRolesHTML(_data)', sandbox);
  for (const s of ['planning', 'big-model', 'review', 'small', 'Resolves to']) {
    assert.ok(html.includes(s), 'card is missing ' + s);
  }
  assert.ok(!html.includes('workload-role-warning'));
});

test('a contention warning is displayed and values are escaped', () => {
  const sandbox = loadApp();
  sandbox._data = {
    roles: [{ role: '<i>r</i>', entry: 'a"b' }],
    warnings: ['role "planning" is claimed by 2 enabled entries (a, b); using "a" <script>'],
  };
  const html = vm.runInContext('buildWorkloadRolesHTML(_data)', sandbox);
  assert.ok(html.includes('workload-role-warning'));
  assert.ok(html.includes('claimed by 2 enabled entries'));
  assert.ok(!html.includes('<script>') && !html.includes('<i>r</i>'));
  assert.ok(html.includes('&lt;script&gt;'));
});

test('no roles and no warnings renders the empty state', () => {
  const sandbox = loadApp();
  sandbox._data = { roles: [], warnings: [] };
  const html = vm.runInContext('buildWorkloadRolesHTML(_data)', sandbox);
  assert.ok(html.includes('No LLM entry claims a workload role.'));
  assert.ok(!html.includes('<table'));
  // a missing body (older daemon) is tolerated too
  assert.ok(vm.runInContext('buildWorkloadRolesHTML(null)', sandbox).includes('No LLM entry'));
});

test('the card loader fetches /api/llm-roles and fills the panel', async () => {
  const sandbox = loadApp();
  const panel = { innerHTML: '' };
  sandbox.document.getElementById = id => (id === 'workloadRolesPanel' ? panel : null);
  const calls = [];
  sandbox.apiFetch = url => { calls.push(url); return Promise.resolve({ roles: [{ role: 'planning', entry: 'a' }], warnings: [] }); };
  vm.runInContext('loadWorkloadRolesPanel()', sandbox);
  await flushAsync();
  assert.deepEqual(calls, ['/api/llm-roles']);
  assert.ok(panel.innerHTML.includes('planning'));
});

test('every new roles key exists in all five locale bundles', () => {
  const keys = ['llm_field_roles', 'llm_roles_hint', 'workload_roles_title', 'workload_roles_desc',
    'workload_roles_empty', 'workload_roles_col_role', 'workload_roles_col_entry', 'workload_roles_warnings'];
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k], lang + ' lacks ' + k);
  }
});
