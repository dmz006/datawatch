// Automation tab — controls added for operation parity: activate, set the
// verification LLM, set the budget, and the live status row read from the loop
// status. fetch is stubbed and records every request.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    return Promise.resolve({
      ok: true, status: 200, statusText: '200',
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

const draft = { id: 'a1', name: 'Chain', status: 'draft', version: 4, members: [], active_cursors: [] };
const field = value => ({ value, style: {} });
const stateOf = sb => vm.runInContext('_automationState', sb);

test('activate posts to the activate endpoint with the loaded version', async () => {
  const sb = loadApp({ 'POST /api/automations/a1/activate': { body: Object.assign({}, draft, { status: 'active', version: 5 }) } },
    { view: makeStubElement(), automationControls: makeStubElement() });
  stateOf(sb).detailId = 'a1';
  stateOf(sb).detail = draft;
  await sb.automationControl('a1', 'activate');
  const call = sb.__calls.find(c => c.method === 'POST');
  assert.equal(call.url, '/api/automations/a1/activate');
  assert.equal(call.body.expected_version, 4);
  assert.equal(call.body.actor, 'operator');
});

test('the budget dialog sends clear when the ceiling is empty and rejects a bad ceiling', async () => {
  const budgetValue = field('');
  const sb = loadApp({ 'POST /api/automations/a1/set_budget': { body: draft } },
    { view: makeStubElement(), automationControls: makeStubElement(), automationBudgetValue: budgetValue });
  stateOf(sb).detailId = 'a1';
  stateOf(sb).detail = draft;
  let modal;
  sb.showModal = m => { modal = m; };
  sb.openAutomationBudgetDialog('a1');
  assert.equal(modal.title, 'Set budget');
  assert.equal(modal.onConfirm(), true);
  await flushAsync();
  const call = sb.__calls.find(c => c.url === '/api/automations/a1/set_budget');
  assert.equal(call.body.clear, true);

  budgetValue.value = '-3';
  assert.equal(modal.onConfirm(), false);
  assert.equal(sb.__toasts.length, 1);
});

test('the status line words the automations[] row', () => {
  const sb = loadApp({}, {});
  assert.equal(sb.automationStatusLine(null), '');
  assert.equal(
    sb.automationStatusLine({ current_prd_title: 'Ship it', pending_members: 2, idle_seconds: 600 }),
    'Now: Ship it · waiting: 2 · idle: 10m');
});

test('the live status row comes from GET /api/autonomous/status', async () => {
  const el = makeStubElement();
  const sb = loadApp({ 'GET /api/autonomous/status': { body: { automations: [{ id: 'a1', current_member_id: 'm1', pending_members: 1 }] } } },
    { automationLiveStatus: el });
  stateOf(sb).detailId = 'a1';
  await sb.loadAutomationLiveStatus('a1');
  assert.match(el.innerHTML, /Now: m1 · waiting: 1/);
});

test('the verification LLM dialog posts backend, model and effort to set_verification_llm', async () => {
  const ids = {
    view: makeStubElement(), automationControls: makeStubElement(),
    automationSetVerifBackend: field('ollama'),
    automationSetVerifEffort: field('high'),
    automationSetVerifModelInner: makeStubElement(),
  };
  const sb = loadApp({ 'POST /api/automations/a1/set_verification_llm': { body: draft } }, ids);
  stateOf(sb).detailId = 'a1';
  stateOf(sb).detail = draft;
  let modal;
  sb.showModal = m => { modal = m; };
  sb.openAutomationVerificationLLMDialog('a1');
  assert.equal(modal.onConfirm(), true);
  await flushAsync();
  const call = sb.__calls.find(c => c.url === '/api/automations/a1/set_verification_llm');
  assert.equal(call.body.backend, 'ollama');
  assert.equal(call.body.effort, 'high');
  assert.equal(call.body.model, '');
});
