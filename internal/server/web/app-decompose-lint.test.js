// Decompose-lint UI (task 4f3f): pins the Settings card in the autonomous
// section (mode select + max-retries number + task-templates toggle +
// guidance textarea + rules JSON editor with validation feedback from the
// dedicated PUT endpoint), the stored-findings + dry-run panel on the
// Automaton detail view, and locale key parity for all new strings across
// the five bundles.
//
// The settings card talks to the dedicated endpoints (GET/PUT
// /api/autonomous/decompose-lint/config), not /api/config; the findings
// panel to /api/autonomous/prds/{id}/decompose-lint (GET stored, POST dry
// run). Tests run app.js's real functions in the shared stub browser.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync, makeStubElement } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');
const APP_JS = path.join(__dirname, 'app.js');

// A plain element stand-in that stores set values (the shared
// makeStubElement drops them and its property definitions are
// non-configurable, so this is a small independent object instead).
function makeValueElement() {
  return { value: '', checked: false, textContent: '', innerHTML: '', style: {} };
}

function freshEnvironment({ byId = {}, fetchImpl } = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  if (fetchImpl) sandbox.fetch = fetchImpl;
  sandbox.__calls = [];
  sandbox.__fetch = function (url, opts) {
    sandbox.__calls.push({ url, opts: opts || {} });
    return fetchImpl ? fetchImpl(url, opts || {}) : Promise.resolve({ ok: false, status: 404, text: async () => '' });
  };
  sandbox.fetch = sandbox.__fetch;
  return sandbox;
}

function run(sandbox, expr) {
  return vm.runInContext(expr, sandbox);
}

// ── field list ───────────────────────────────────────────────────────────────

function decomposeLintField() {
  const sandbox = freshEnvironment();
  const json = run(sandbox,
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.decompose_lint') || null)");
  return JSON.parse(json);
}

test('autonomous settings expose a decompose_lint card field with the three modes', () => {
  const f = decomposeLintField();
  assert.ok(f, 'autonomous.decompose_lint missing from the autonomous settings card');
  assert.equal(f.type, 'decompose_lint');
  assert.deepEqual(f.modeOptions, ['off', 'shadow', 'enforce']);
  assert.ok(typeof f.modeLabelKey === 'string' && f.modeLabelKey.length, 'modeLabelKey missing');
  assert.ok(typeof f.maxRetriesLabelKey === 'string' && f.maxRetriesLabelKey.length, 'maxRetriesLabelKey missing');
  assert.equal(f.maxRetriesDefault, 2);
  assert.equal(f.maxRetriesMax, 5);
  assert.ok(typeof f.taskTemplatesLabelKey === 'string' && f.taskTemplatesLabelKey.length, 'taskTemplatesLabelKey missing');
  assert.equal(f.taskTemplatesHelpKey, 'settings_decompose_lint_task_templates_help');
  assert.ok(typeof f.saveKey === 'string' && f.saveKey.length, 'saveKey missing');
});

// ── locale parity ────────────────────────────────────────────────────────────

const SETTINGS_KEYS = [
  'settings_decompose_lint_group',
  'settings_decompose_lint',
  'settings_decompose_lint_mode',
  'settings_decompose_lint_max_retries',
  'settings_decompose_lint_task_templates',
  'settings_decompose_lint_task_templates_help',
  'settings_decompose_lint_guidance',
  'settings_decompose_lint_rules',
  'settings_decompose_lint_rules_help',
  'settings_decompose_lint_save',
  'settings_decompose_lint_saved',
  'settings_decompose_lint_rules_invalid',
  'settings_decompose_lint_error',
];

const PANEL_KEYS = [
  'prd_dl_title',
  'prd_dl_scope',
  'prd_dl_none',
  'prd_dl_clean',
  'prd_dl_severity_block',
  'prd_dl_severity_warn',
  'prd_dl_dryrun',
  'prd_dl_dryrun_done',
  'prd_dl_running',
  'prd_dl_dryrun_error',
];

test('decompose lint settings and findings panel keys exist in all five locale bundles', () => {
  for (const locale of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    for (const k of [...SETTINGS_KEYS, ...PANEL_KEYS]) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
    }
  }
});

// ── settings card load / save behavior ───────────────────────────────────────

function cardEnvironment() {
  const pid = 'dlc-test';
  const els = {};
  for (const suffix of ['', '-mode', '-maxretries', '-tasktemplates', '-guidance', '-rules', '-feedback']) {
    els[pid + suffix] = makeValueElement();
  }
  const byId = els;

  const okResponse = () => ({ ok: true, status: 200, json: async () => ({
    decompose_lint: {
      mode: 'shadow', max_retries: 2,
      rules: [{ id: 'task-testable', type: 'testable', severity: 'block', value: 2 }],
    },
    task_templates: { enabled: true },
    decompose_guidance: 'Break it into verifiable slices.',
  }) });

  const sandbox = freshEnvironment({ byId, fetchImpl: (url) => {
    if (url === '/api/autonomous/decompose-lint/config' && !(sandbox.__calls[0].opts || {}).method) return Promise.resolve(okResponse());
    return Promise.resolve(okResponse());
  } });

  return { sandbox, els, pid };
}

test('settingsDecomposeLintLoad populates mode, retries, templates toggle, guidance and rules from the GET config', async () => {
  const { sandbox, els, pid } = cardEnvironment();
  run(sandbox, 'settingsDecomposeLintLoad("' + pid + '")');
  await flushAsync();
  assert.equal(els[pid + '-mode'].value, 'shadow');
  assert.equal(els[pid + '-maxretries'].value, 2);
  assert.equal(els[pid + '-tasktemplates'].checked, true);
  assert.equal(els[pid + '-guidance'].value, 'Break it into verifiable slices.');
  const rules = JSON.parse(els[pid + '-rules'].value);
  assert.ok(Array.isArray(rules.rules) && rules.rules.length === 1, 'rules editor should show the {rules:[...]} document');
});

test('settingsDecomposeLintSave PUTs the collected config to the dedicated endpoint', async () => {
  const { sandbox, els, pid } = cardEnvironment();
  run(sandbox, 'settingsDecomposeLintLoad("' + pid + '")');
  await flushAsync();
  els[pid + '-mode'].value = 'enforce';

  let put = null;
  sandbox.fetch = function (url, opts) {
    put = { url, opts: opts || {} };
    return Promise.resolve({ ok: true, status: 200, json: async () => ({ ok: true }) });
  };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();

  assert.ok(put, 'save should issue a fetch');
  assert.equal(put.url, '/api/autonomous/decompose-lint/config');
  assert.equal((put.opts || {}).method, 'PUT');
  const body = JSON.parse(put.opts.body);
  assert.equal(body.decompose_lint.mode, 'enforce');
  assert.equal(body.decompose_lint.max_retries, 2);
  assert.equal(body.task_templates.enabled, true);
  assert.equal(body.decompose_guidance, 'Break it into verifiable slices.');
  assert.ok(Array.isArray(body.decompose_lint.rules), 'rules array should be forwarded');
  assert.equal(els[pid + '-feedback'].textContent.length > 0, true);
  assert.equal(els[pid + '-feedback'].style.color, 'var(--success,#22c55e)');
});

test('saving after changing only the lint mode keeps the template mode map, template override and project override', async () => {
  const pid = 'dlc-templatemap';
  const els = {};
  for (const suffix of ['', '-mode', '-maxretries', '-tasktemplates', '-guidance', '-rules', '-feedback']) {
    els[pid + suffix] = makeValueElement();
  }
  const loadedTemplates = {
    enabled: true,
    templates: [{ id: 'custom_note', title: 'Custom note', spec: 'Write a short operator note.', files: ['notes.md'] }],
    mode: { changelog_entry: 'llm', docs_set: 'off' },
    projects: { 'project-a': { enabled: false } },
  };
  const sandbox = freshEnvironment({
    byId: els,
    fetchImpl: (url) => {
      if (url === '/api/autonomous/decompose-lint/config') {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({
          decompose_lint: {
            mode: 'shadow', max_retries: 2,
            rules: [{ id: 'task-testable', type: 'testable', severity: 'block', value: 2 }],
          },
          decompose_guidance: 'Break it into verifiable slices.',
          task_templates: loadedTemplates,
        }) });
      }
      return Promise.resolve({ ok: false, status: 404, text: async () => '' });
    },
  });

  run(sandbox, 'settingsDecomposeLintLoad("' + pid + '")');
  await flushAsync();

  // Change only the lint mode; the task-templates checkbox stays at the
  // loaded (checked) state.
  els[pid + '-mode'].value = 'enforce';

  let put = null;
  sandbox.fetch = function (url, opts) {
    put = { url, opts: opts || {} };
    return Promise.resolve({ ok: true, status: 200, json: async () => ({ ok: true }) });
  };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();

  assert.ok(put, 'save should issue a fetch');
  assert.equal(put.url, '/api/autonomous/decompose-lint/config');
  assert.equal(put.opts.method, 'PUT');
  const body = JSON.parse(put.opts.body);
  assert.equal(body.decompose_lint.mode, 'enforce');
  assert.deepEqual(body.task_templates, loadedTemplates);
});

test('turning the template enable checkbox off sends the loaded block with only enabled changed', async () => {
  const pid = 'dlc-templateoff';
  const els = {};
  for (const suffix of ['', '-mode', '-maxretries', '-tasktemplates', '-guidance', '-rules', '-feedback']) {
    els[pid + suffix] = makeValueElement();
  }
  const loadedTemplates = {
    enabled: true,
    templates: [{ id: 'custom_note', title: 'Custom note', spec: 'Write a short operator note.', files: ['notes.md'] }],
    mode: { changelog_entry: 'llm', docs_set: 'off' },
    projects: { 'project-a': { enabled: false } },
  };
  const sandbox = freshEnvironment({
    byId: els,
    fetchImpl: (url) => {
      if (url === '/api/autonomous/decompose-lint/config') {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({
          decompose_lint: {
            mode: 'shadow', max_retries: 2,
            rules: [{ id: 'task-testable', type: 'testable', severity: 'block', value: 2 }],
          },
          decompose_guidance: 'Break it into verifiable slices.',
          task_templates: loadedTemplates,
        }) });
      }
      return Promise.resolve({ ok: false, status: 404, text: async () => '' });
    },
  });

  run(sandbox, 'settingsDecomposeLintLoad("' + pid + '")');
  await flushAsync();

  // Uncheck the loaded-enabled checkbox; nothing else changes.
  assert.equal(els[pid + '-tasktemplates'].checked, true);
  els[pid + '-tasktemplates'].checked = false;

  let put = null;
  sandbox.fetch = function (url, opts) {
    put = { url, opts: opts || {} };
    return Promise.resolve({ ok: true, status: 200, json: async () => ({ ok: true }) });
  };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();

  assert.ok(put, 'save should issue a fetch');
  assert.equal(put.url, '/api/autonomous/decompose-lint/config');
  assert.equal(put.opts.method, 'PUT');
  const body = JSON.parse(put.opts.body);
  assert.deepEqual(body.task_templates, {
    enabled: false,
    templates: [{ id: 'custom_note', title: 'Custom note', spec: 'Write a short operator note.', files: ['notes.md'] }],
    mode: { changelog_entry: 'llm', docs_set: 'off' },
    projects: { 'project-a': { enabled: false } },
  });
});

test('a save before the card has loaded omits task_templates from the request', async () => {
  const pid = 'dlc-notloaded';
  const els = {};
  for (const suffix of ['', '-mode', '-maxretries', '-tasktemplates', '-guidance', '-rules', '-feedback']) {
    els[pid + suffix] = makeValueElement();
  }
  const sandbox = freshEnvironment({
    byId: els,
    fetchImpl: () => Promise.resolve({ ok: false, status: 404, text: async () => '' }),
  });

  // No settingsDecomposeLintLoad: the card starts at field defaults; set the
  // max-retries number so the save passes validation.
  els[pid + '-maxretries'].value = '2';

  let put = null;
  sandbox.fetch = function (url, opts) {
    put = { url, opts: opts || {} };
    return Promise.resolve({ ok: true, status: 200, json: async () => ({ ok: true }) });
  };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();

  assert.ok(put, 'save should issue a fetch');
  assert.equal(put.url, '/api/autonomous/decompose-lint/config');
  assert.equal(put.opts.method, 'PUT');
  const body = JSON.parse(put.opts.body);
  assert.equal('task_templates' in body, false, 'task_templates must be absent when no block was loaded');
});

test('settingsDecomposeLintSave rejects an invalid rules editor value before touching the network', async () => {
  const { sandbox, els, pid } = cardEnvironment();
  els[pid + '-rules'].value = '{ this is not valid json ';
  let fetched = false;
  sandbox.fetch = function () { fetched = true; return Promise.resolve({ ok: true, status: 200, json: async () => ({}) }); };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();
  assert.equal(fetched, false, 'invalid rules text must not reach the daemon');
  assert.match(els[pid + '-feedback'].textContent, /JSON/);
  assert.equal(els[pid + '-feedback'].style.color, 'var(--error,#ef4444)');
});

test('settingsDecomposeLintSave surfaces the daemon 400 rule-validation text in the card feedback', async () => {
  const { sandbox, els, pid } = cardEnvironment();
  els[pid + '-maxretries'].value = '1';
  els[pid + '-rules'].value = JSON.stringify({ rules: [{ id: 'task-testable', type: 'no-such-type', severity: 'block' }] });
  sandbox.fetch = function () {
    return Promise.resolve({ ok: false, status: 400, text: async () => 'decompose lint: rules[0]: unknown rule type "no-such-type"' });
  };
  run(sandbox, 'settingsDecomposeLintSave("' + pid + '")');
  await flushAsync();
  assert.match(els[pid + '-feedback'].textContent, /unknown rule type "no-such-type"/);
});

// ── findings panel (Automaton detail view) ──────────────────────────────────

const FINDINGS = [
  { rule_id: 'task-testable', severity: 'block', message: 'task "ship it" has no acceptance criterion', story: 'S1', task: 'ship it', matched: 'ship it' },
  { rule_id: 'task-small', severity: 'warn', message: 'task touches 9 files', story: 'S1', task: 'big refactor', matched: '' },
];

function panelEnvironment(fetchImpl) {
  const slot = { innerHTML: '', style: { display: 'none' } };
  return { sandbox: freshEnvironment({ byId: { prdDecomposeLintSlot: slot }, fetchImpl }), slot };
}

test('panel shows stored findings with severity labels and a dry-run button', async () => {
  const { sandbox, slot } = panelEnvironment(() => Promise.resolve({
    ok: true, status: 200,
    json: async () => ({ decompose_lint: { record_seq: 7, mode: 'shadow', decompose_lint_findings: FINDINGS } }),
  }));
  run(sandbox, '_loadPRDDecomposeLint("prdx")');
  await flushAsync();
  const html = slot.innerHTML;
  assert.match(html, /task-testable/);
  assert.match(html, /prd_dl_severity_block|blocking/i);
  assert.match(html, /prd_dl_severity_warn|warning/i);
  assert.match(html, /_dlRunDryRun/);
  assert.equal(slot.style.display, '');
});

test('panel hides itself when the daemon has no decompose-lint endpoint', async () => {
  const { sandbox, slot } = panelEnvironment(() => Promise.resolve({ ok: false, status: 404, text: async () => 'not found' }));
  run(sandbox, '_loadPRDDecomposeLint("prdx")');
  await flushAsync();
  assert.equal(slot.style.display, 'none', 'slot must stay hidden when the endpoint does not exist');
  assert.equal(slot.innerHTML, '');
});

test('dry run reports its findings without touching the stored record', async () => {
  const { sandbox, slot } = panelEnvironment((url, opts) => {
    if ((opts || {}).method === 'POST') {
      return Promise.resolve({
        ok: true, status: 200,
        json: async () => ({ decompose_lint_report: { findings: [{ rule_id: 'task-testable', severity: 'block', message: 'still no acceptance criterion', story: 'S1', task: 'ship it' }] } }),
      });
    }
    return Promise.resolve({ ok: true, status: 200, json: async () => ({ decompose_lint: null }) });
  });
  let captured = null;
  sandbox.fetch = function (url, opts) { captured = { url, opts: opts || {} }; return sandbox.__fetch(url, opts); };
  run(sandbox, '_dlRunDryRun("prdy")');
  await flushAsync();
  assert.equal(captured.url, '/api/autonomous/prds/prdy/decompose-lint');
  assert.equal(captured.opts.method, 'POST');
  const html = slot.innerHTML;
  assert.match(html, /task-testable/);
  assert.match(html, /prd_dl_dryrun_done|not recorded/i);
});
