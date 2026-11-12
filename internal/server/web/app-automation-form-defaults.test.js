// Automation tab — default models and budget policy in the create and edit form.
//
// Covers reading the execution and verification rows (filled, partly filled and
// cleared), reading the budget policy (a dollar policy, a percent policy, an
// empty ceiling and each invalid case), the model default keys and budget_policy
// in the create and update body, the planning pair travelling only through the
// set_llm request, and the estimated word on the allocation percent option.
// fetch is stubbed and records every request; rendered text is the real English
// bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
const EN = bundle('en');

const PRDS = {
  prds: [
    { id: 'aaaaaaaa1111', title: 'Build the base', status: 'approved' },
    { id: 'bbbbbbbb2222', title: 'Run the checks', status: 'approved' },
  ],
};

const DRAFT = {
  id: 'dr000001', name: 'Draft chain', description: '', status: 'draft', version: 3, entry_member_id: 'm1',
  backend: 'claude-code', model: 'claude-sonnet-5-5', effort: 'high',
  planning_backend: 'ollama', planning_model: 'qwen',
  budget_policy: { ceiling_type: 'dollar', ceiling_value: 5, window: 'week', pacing: 'even', on_breach: 'block' },
  members: [
    { id: 'm1', prd_id: 'aaaaaaaa1111', status: 'pending', on_complete: 'm2' },
    { id: 'm2', prd_id: 'bbbbbbbb2222', status: 'pending' },
  ],
};

const ROUTES = {
  'GET /api/autonomous/prds': { body: PRDS },
  'GET /api/automations': { body: [] },
  'POST /api/automations': { body: { id: 'new00001' } },
  'GET /api/automations/new00001': { body: { id: 'new00001', name: 'Chain', status: 'draft', version: 1, members: [] } },
  'PUT /api/automations/dr000001': { body: Object.assign({}, DRAFT, { version: 4 }) },
  'GET /api/automations/dr000001': { body: DRAFT },
  'POST /api/automations/dr000001/set_llm': { body: Object.assign({}, DRAFT, { version: 5 }) },
};

function loadApp(routes = ROUTES) {
  const els = { view: makeStubElement(), error: { innerHTML: '' } };
  const byId = {
    view: els.view,
    automationFormName: { value: 'Chain' },
    automationFormDescription: { value: '' },
    automationFormReason: { value: '' },
    automationFormError: els.error,
    automationFormMembers: { innerHTML: '' },
    automationFormPicker: { innerHTML: '' },
    automationFormSaveBtn: { disabled: false },
  };
  const sandbox = buildSandbox({ byId });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: '200',
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state._prdBackends = [{ name: 'claude-code' }, { name: 'ollama' }]", sandbox);
  sandbox.__calls = calls;
  sandbox.__els = els;
  return sandbox;
}

const plain = value => JSON.parse(JSON.stringify(value));
const writes = sb => sb.__calls.filter(c => c.method !== 'GET');

// useFields makes getElementById answer from a map of id -> value, the way the
// typed form would; a row's model field is a nested input.
function useFields(sb, fields) {
  const fallback = sb.document.getElementById;
  sb.document.getElementById = id => {
    if (!(id in fields)) return fallback(id);
    const v = fields[id];
    if (v && typeof v === 'object') return v;
    if (id.endsWith('ModelInner')) return { querySelector: () => (v === '' ? null : { value: v }) };
    return { value: v, style: {} };
  };
}

const BUDGET_DOLLAR = {
  automationBudgetValue: '12.5', automationBudgetType: 'dollar', automationBudgetWindow: 'week',
  automationBudgetPacing: 'even', automationBudgetOnBreach: 'block', automationBudgetBackend: '',
};

async function openNew() {
  const sb = loadApp();
  sb.openAutomationForm(null);
  await flushAsync();
  return sb;
}

async function openEdit(source = DRAFT) {
  const sb = loadApp();
  sb.openAutomationForm(JSON.parse(JSON.stringify(source)));
  await flushAsync();
  return sb;
}

// ── automationReadModelDefaults ──────────────────────────────────────────────

test('automationReadModelDefaults reads a filled execution and verification row', async () => {
  const sb = await openNew();
  useFields(sb, {
    automation_execBackend: 'claude-code', automation_execEffort: 'high', automation_execModelInner: 'claude-opus-5-5',
    automation_verifBackend: 'ollama', automation_verifEffort: 'low', automation_verifModelInner: 'qwen3',
  });
  assert.deepEqual(plain(sb.automationReadModelDefaults()), {
    backend: 'claude-code', model: 'claude-opus-5-5', effort: 'high',
    verification_backend: 'ollama', verification_model: 'qwen3', verification_effort: 'low',
  });
});

test('automationReadModelDefaults gives empty strings for a part of a row left alone', async () => {
  const sb = await openNew();
  useFields(sb, {
    automation_execBackend: 'claude-code', automation_execEffort: '', automation_execModelInner: '',
    automation_verifBackend: '', automation_verifEffort: 'low', automation_verifModelInner: '',
  });
  assert.deepEqual(plain(sb.automationReadModelDefaults()), {
    backend: 'claude-code', model: '', effort: '',
    verification_backend: '', verification_model: '', verification_effort: 'low',
  });
});

test('automationReadModelDefaults gives empty strings for cleared rows', async () => {
  const sb = await openNew();
  useFields(sb, {
    automation_execBackend: '', automation_execEffort: '', automation_execModelInner: '',
    automation_verifBackend: '', automation_verifEffort: '', automation_verifModelInner: '',
  });
  assert.deepEqual(plain(sb.automationReadModelDefaults()), {
    backend: '', model: '', effort: '', verification_backend: '', verification_model: '', verification_effort: '',
  });
});

// ── automationReadBudgetPolicy ───────────────────────────────────────────────

test('automationReadBudgetPolicy reads a dollar policy', async () => {
  const sb = await openNew();
  useFields(sb, BUDGET_DOLLAR);
  assert.deepEqual(plain(sb.automationReadBudgetPolicy()), {
    ceiling_type: 'dollar', ceiling_value: 12.5, window: 'week', pacing: 'even', on_breach: 'block',
  });
});

test('automationReadBudgetPolicy reads a percent policy with a backend', async () => {
  const sb = await openNew();
  useFields(sb, Object.assign({}, BUDGET_DOLLAR, {
    automationBudgetValue: '40', automationBudgetType: 'allocation_pct', automationBudgetWindow: 'month',
    automationBudgetPacing: 'none', automationBudgetOnBreach: 'pause', automationBudgetBackend: 'claude-code',
  }));
  assert.deepEqual(plain(sb.automationReadBudgetPolicy()), {
    ceiling_type: 'allocation_pct', ceiling_value: 40, window: 'month', pacing: 'none', on_breach: 'pause', backend: 'claude-code',
  });
});

test('automationReadBudgetPolicy accepts a percent of exactly 100', async () => {
  const sb = await openNew();
  useFields(sb, Object.assign({}, BUDGET_DOLLAR, { automationBudgetValue: '100', automationBudgetType: 'allocation_pct' }));
  assert.equal(sb.automationReadBudgetPolicy().ceiling_value, 100);
});

test('automationReadBudgetPolicy gives null for an empty ceiling', async () => {
  const sb = await openNew();
  useFields(sb, Object.assign({}, BUDGET_DOLLAR, { automationBudgetValue: '  ' }));
  assert.equal(sb.automationReadBudgetPolicy(), null);
});

test('automationReadBudgetPolicy rejects every invalid case with the message key', async () => {
  const sb = await openNew();
  const cases = {
    zero: { automationBudgetValue: '0' },
    negative: { automationBudgetValue: '-3' },
    'not a number': { automationBudgetValue: 'abc' },
    'percent above 100': { automationBudgetValue: '101', automationBudgetType: 'allocation_pct' },
    'bad window': { automationBudgetWindow: 'year' },
    'bad type': { automationBudgetType: 'euro' },
    'bad pacing': { automationBudgetPacing: 'fast' },
    'bad on breach': { automationBudgetOnBreach: 'ignore' },
  };
  for (const [name, patch] of Object.entries(cases)) {
    useFields(sb, Object.assign({}, BUDGET_DOLLAR, patch));
    assert.deepEqual(plain(sb.automationReadBudgetPolicy()), { error: 'automation_budget_invalid' }, name);
  }
  assert.ok(EN.automation_budget_invalid, 'the message key has a text');
});

// ── payload ──────────────────────────────────────────────────────────────────

test('buildAutomationFormPayload carries the model default keys and budget_policy on create', async () => {
  const sb = await openNew();
  const policy = { ceiling_type: 'dollar', ceiling_value: 7, window: 'day', pacing: 'none', on_breach: 'pause' };
  const body = plain(sb.buildAutomationFormPayload({
    name: 'Chain', members: [{ id: 'm1', prd_id: 'aaaaaaaa1111' }],
    modelDefaults: { backend: 'claude-code', model: 'm', effort: 'high', verification_backend: 'ollama', verification_model: 'q', verification_effort: 'low' },
    budgetPolicy: policy,
  }));
  assert.equal(body.backend, 'claude-code');
  assert.equal(body.model, 'm');
  assert.equal(body.effort, 'high');
  assert.equal(body.verification_backend, 'ollama');
  assert.equal(body.verification_model, 'q');
  assert.equal(body.verification_effort, 'low');
  assert.deepEqual(body.budget_policy, policy);
  assert.ok(!('expected_version' in body));
  assert.ok(!('planning_backend' in body) && !('planning_model' in body));
});

test('buildAutomationFormPayload on edit replaces what the loaded Automation had', async () => {
  const sb = await openNew();
  const body = plain(sb.buildAutomationFormPayload({
    name: 'Chain', members: [{ id: 'm1', prd_id: 'aaaaaaaa1111' }], expectedVersion: 3, base: DRAFT,
    modelDefaults: { backend: '', model: '', effort: '', verification_backend: 'ollama', verification_model: '', verification_effort: '' },
    budgetPolicy: null,
  }));
  assert.equal(body.backend, '', 'an emptied row clears the default');
  assert.equal(body.model, '');
  assert.equal(body.effort, '');
  assert.equal(body.verification_backend, 'ollama');
  assert.ok(!('budget_policy' in body), 'an empty ceiling sends no policy');
  assert.equal(body.expected_version, 3);
  assert.ok(!('planning_backend' in body) && !('planning_model' in body));
});

test('buildAutomationFormPayload leaves the loaded defaults alone when the form read none', async () => {
  const sb = await openNew();
  const body = plain(sb.buildAutomationFormPayload({
    name: 'Chain', members: [{ id: 'm1', prd_id: 'aaaaaaaa1111' }], expectedVersion: 3, base: DRAFT,
  }));
  assert.equal(body.backend, 'claude-code');
  assert.equal(body.model, 'claude-sonnet-5-5');
  assert.deepEqual(body.budget_policy, DRAFT.budget_policy);
});

// ── saving ───────────────────────────────────────────────────────────────────

test('saving a new Automation sends the model defaults and budget_policy in the POST body', async () => {
  const sb = await openNew();
  vm.runInContext("_automationForm.members = [{ id: 'm1', prd_id: 'aaaaaaaa1111' }]; _automationForm.entryMemberId = 'm1'", sb);
  useFields(sb, Object.assign({
    automation_execBackend: 'claude-code', automation_execEffort: 'max', automation_execModelInner: 'claude-opus-5-5',
    automation_verifBackend: '', automation_verifEffort: '', automation_verifModelInner: '',
  }, BUDGET_DOLLAR));
  sb.automationFormDefaultsChanged();
  await sb.saveAutomationForm();
  await flushAsync();
  const sent = writes(sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].method, 'POST');
  assert.equal(sent[0].url, '/api/automations');
  assert.equal(sent[0].body.backend, 'claude-code');
  assert.equal(sent[0].body.effort, 'max');
  assert.equal(sent[0].body.model, 'claude-opus-5-5');
  assert.equal(sent[0].body.verification_backend, '');
  assert.deepEqual(sent[0].body.budget_policy, {
    ceiling_type: 'dollar', ceiling_value: 12.5, window: 'week', pacing: 'even', on_breach: 'block',
  });
  assert.ok(!('planning_backend' in sent[0].body) && !('planning_model' in sent[0].body));
});

test('an invalid budget shows its message and sends nothing', async () => {
  const sb = await openNew();
  vm.runInContext("_automationForm.members = [{ id: 'm1', prd_id: 'aaaaaaaa1111' }]; _automationForm.entryMemberId = 'm1'", sb);
  useFields(sb, Object.assign({}, BUDGET_DOLLAR, {
    automation_execBackend: '', automation_execEffort: '', automation_execModelInner: '',
    automation_verifBackend: '', automation_verifEffort: '', automation_verifModelInner: '',
    automationBudgetValue: '150', automationBudgetType: 'allocation_pct',
  }));
  sb.automationFormDefaultsChanged();
  await sb.saveAutomationForm();
  await flushAsync();
  assert.equal(writes(sb).length, 0);
  assert.equal(sb.__els.error.innerHTML, EN.automation_budget_invalid);
});

test('the planning pair goes only through set_llm after the update, never in the PUT body', async () => {
  const sb = await openEdit();
  useFields(sb, Object.assign({
    automation_execBackend: 'claude-code', automation_execEffort: 'high', automation_execModelInner: 'claude-sonnet-5-5',
    automation_verifBackend: '', automation_verifEffort: '', automation_verifModelInner: '',
    automation_planBackend: 'claude-code', automation_planModelInner: 'claude-haiku-5-5',
  }, BUDGET_DOLLAR));
  sb.automationFormDefaultsChanged();
  await sb.saveAutomationForm();
  await flushAsync();
  const sent = writes(sb);
  assert.deepEqual(sent.map(c => c.method + ' ' + c.url), ['PUT /api/automations/dr000001', 'POST /api/automations/dr000001/set_llm']);
  assert.ok(!('planning_backend' in sent[0].body) && !('planning_model' in sent[0].body), 'not in the update body');
  assert.equal(sent[0].body.expected_version, 3);
  assert.equal(sent[0].body.backend, 'claude-code');
  assert.deepEqual(sent[1].body, { planning_backend: 'claude-code', planning_model: 'claude-haiku-5-5' });
});

test('an emptied planning pair is sent as empty strings, which clear it', async () => {
  const sb = await openEdit();
  useFields(sb, { automation_planBackend: '', automation_planModelInner: '' });
  await sb.saveAutomationForm();
  await flushAsync();
  const sent = writes(sb);
  assert.equal(sent[1].url, '/api/automations/dr000001/set_llm');
  assert.deepEqual(sent[1].body, { planning_backend: '', planning_model: '' });
});

test('an unchanged planning pair sends no set_llm request, and a create never does', async () => {
  const edit = await openEdit();
  useFields(edit, { automation_planBackend: 'ollama', automation_planModelInner: 'qwen' });
  await edit.saveAutomationForm();
  await flushAsync();
  assert.deepEqual(writes(edit).map(c => c.method), ['PUT']);

  const created = await openNew();
  vm.runInContext("_automationForm.members = [{ id: 'm1', prd_id: 'aaaaaaaa1111' }]; _automationForm.entryMemberId = 'm1'", created);
  useFields(created, { automation_planBackend: 'claude-code', automation_planModelInner: 'x' });
  await created.saveAutomationForm();
  await flushAsync();
  assert.deepEqual(writes(created).map(c => c.method + ' ' + c.url), ['POST /api/automations']);
});

// ── rendering ────────────────────────────────────────────────────────────────

test('the form draws collapsible Default models and Budget policy sections', async () => {
  const sb = await openNew();
  const html = sb.__els.view.innerHTML;
  assert.match(html, /<details id="automationFormModels"[^>]*>\s*<summary[^>]*>Default models</);
  assert.match(html, /<details id="automationFormBudget"[^>]*>\s*<summary[^>]*>Budget policy</);
  for (const label of ['Execution', 'Verification', 'Ceiling type', 'Ceiling', 'Window', 'Pacing', 'On breach']) {
    assert.ok(html.includes('>' + label), 'missing label ' + label);
  }
  for (const id of ['automation_execBackend', 'automation_execEffort', 'automation_verifBackend', 'automation_verifEffort',
    'automationBudgetType', 'automationBudgetValue', 'automationBudgetWindow', 'automationBudgetPacing', 'automationBudgetOnBreach']) {
    assert.ok(html.includes('id="' + id + '"'), 'missing field ' + id);
  }
  assert.ok(!/\bPRDs?\b/.test(html.slice(html.indexOf('automationFormModels'))), 'no tracker word in the new sections');
});

test('the Planning pair shows only when editing an existing Automation', async () => {
  const created = await openNew();
  assert.ok(!created.__els.view.innerHTML.includes('automation_planBackend'));
  const edited = await openEdit();
  const html = edited.__els.view.innerHTML;
  assert.ok(html.includes('id="automation_planBackend"'));
  assert.ok(html.includes('>Planning'));
  assert.match(html, /<option value="ollama" selected>/, 'the saved planning backend is selected');
});

test('an edit draws the saved defaults and budget policy', async () => {
  const sb = await openEdit();
  const html = sb.__els.view.innerHTML;
  assert.match(html, /id="automationBudgetValue"[^>]*value="5"/);
  assert.match(html, /<option value="week" selected>/);
  assert.match(html, /<option value="even" selected>/);
  assert.match(html, /<option value="block" selected>/);
  assert.match(html, /<details id="automationFormModels" class="automation-form-collapsible" open/);
});

test('the allocation percent option shows the estimated word', async () => {
  const sb = await openNew();
  const html = sb.__els.view.innerHTML;
  assert.match(html, /<option value="allocation_pct">Share of allocation \(estimated\)<\/option>/);
  assert.match(html, /<div id="automationBudgetHint"[^>]*display:none;">estimated<\/div>/, 'hidden while the ceiling is in dollars');
  const percent = await openEdit(Object.assign({}, DRAFT, { budget_policy: { ceiling_type: 'allocation_pct', ceiling_value: 30, window: 'day' } }));
  assert.match(percent.__els.view.innerHTML, /<div id="automationBudgetHint"[^>]*>estimated<\/div>/);
  assert.ok(!/<div id="automationBudgetHint"[^>]*display:none/.test(percent.__els.view.innerHTML), 'shown for a percent ceiling');
});

// ── locales ──────────────────────────────────────────────────────────────────

const NEW_KEYS = [
  'automation_models_section', 'automation_model_execution', 'automation_model_planning', 'automation_model_verification',
  'automation_budget_section', 'automation_budget_type', 'automation_budget_type_dollar', 'automation_budget_type_allocation_pct',
  'automation_budget_pacing_none', 'automation_budget_pacing_even', 'automation_budget_on_breach_pause',
  'automation_budget_on_breach_block', 'automation_budget_invalid',
];

test('every new key exists in all five bundles and is translated outside English', () => {
  const en = bundle('en');
  assert.equal(en.automation_models_section, 'Default models');
  assert.equal(en.automation_budget_type_allocation_pct, 'Share of allocation (estimated)');
  assert.equal(en.automation_budget_invalid, 'Enter a ceiling above zero (at most 100 for a percent)');
  for (const lang of LOCALES) {
    const b = bundle(lang);
    for (const key of NEW_KEYS) {
      assert.equal(typeof b[key], 'string', lang + ' lacks ' + key);
      assert.ok(b[key].length > 0, lang + ' has an empty ' + key);
      if (lang !== 'en' && !['automation_budget_type_dollar'].includes(key)) assert.notEqual(b[key], en[key], lang + ' leaves ' + key + ' in English');
      assert.ok(!/\bPRDs?\b/.test(b[key]), lang + ' ' + key + ' names the tracker word');
    }
  }
  for (const key of ['automation_budget_window_automation', 'automation_budget_window_day', 'automation_budget_window_week', 'automation_budget_window_month']) {
    for (const lang of LOCALES) assert.equal(typeof bundle(lang)[key], 'string', lang + ' lacks ' + key);
  }
});
