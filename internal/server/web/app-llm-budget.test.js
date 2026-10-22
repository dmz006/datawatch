// Dollar budget — PWA side.
//
// The daemon exposes a per-LLM `budget` object on GET/PUT /api/llms
// (ceiling_usd, hours, weekday, time, zone, allocation_pct, peer_window), the
// per-LLM budget state under `budgets` in GET /api/capacity, and the
// autonomous.plan_tier_allocation_usd config table. The PWA must let
// operators edit the budget on the LLM form (an empty ceiling clears it),
// show the budget state in the capacity panel, and edit the allocation
// table in Settings. Locales: every new i18n key must exist in all five
// bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const BUDGET_FIELD_IDS = [
  'fe_llm_budget_ceiling',
  'fe_llm_budget_hours',
  'fe_llm_budget_weekday',
  'fe_llm_budget_time',
  'fe_llm_budget_zone',
  'fe_llm_budget_allocation',
  'fe_llm_budget_peers',
];

// fakeFormDom turns rendered form HTML into a getElementById stand-in: it
// finds each element by id and reads the value/checked/selected state the
// markup carries, so a render -> read-back round trip uses the real markup.
// `overrides` replaces a field's value afterwards, the way typing would.
function fakeFormDom(html, overrides = {}) {
  const els = {};
  for (const id of BUDGET_FIELD_IDS) {
    const tag = new RegExp('<(input|select)\\b[^>]*\\bid="' + id + '"[^>]*>').exec(html);
    if (!tag) continue;
    const el = { value: '', checked: false };
    if (tag[1] === 'input') {
      const v = /\bvalue="([^"]*)"/.exec(tag[0]);
      el.value = v ? v[1].replace(/&amp;/g, '&') : '';
      el.checked = /\bchecked\b/.test(tag[0]);
    } else {
      const body = new RegExp('<select\\b[^>]*\\bid="' + id + '"[^>]*>([\\s\\S]*?)</select>').exec(html)[1];
      const sel = /<option value="([^"]*)" selected>/.exec(body);
      el.value = sel ? sel[1] : '';
    }
    els[id] = el;
  }
  for (const [id, patch] of Object.entries(overrides)) Object.assign(els[id], patch);
  return id => els[id] || null;
}

function collectWith(sandbox, html, overrides, original) {
  sandbox.document.getElementById = fakeFormDom(html, overrides);
  sandbox._original = original || {};
  return JSON.parse(vm.runInContext('JSON.stringify(collectLLMForm(_original))', sandbox));
}

test('the LLM edit form renders every budget field with its saved value', () => {
  const sandbox = loadApp();
  sandbox._llm = {
    name: 'claude-pro', kind: 'claude-code',
    budget: { ceiling_usd: 25.5, weekday: 'friday', time: '09:30', zone: 'America/New_York', allocation_pct: true, peer_window: ['alpha', 'beta'] },
  };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  for (const id of BUDGET_FIELD_IDS) {
    assert.ok(html.includes('id="' + id + '"'), 'form is missing field ' + id);
  }
  assert.ok(/id="fe_llm_budget_ceiling"[^>]*value="25.5"/.test(html), 'ceiling value not rendered');
  assert.ok(/value="friday" selected/.test(html), 'weekday not marked selected');
  assert.ok(/id="fe_llm_budget_time"[^>]*value="09:30"/.test(html), 'time not rendered');
  assert.ok(/id="fe_llm_budget_zone"[^>]*value="America\/New_York"/.test(html), 'zone not rendered');
  assert.ok(/id="fe_llm_budget_allocation"[^>]*\bchecked\b/.test(html), 'allocation checkbox not checked');
  assert.ok(/id="fe_llm_budget_peers"[^>]*value="alpha, beta"/.test(html), 'peer window not rendered');
});

test('the LLM edit form renders an empty budget group when the LLM has no budget', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'plain', kind: 'ollama' };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  for (const id of BUDGET_FIELD_IDS) assert.ok(html.includes('id="' + id + '"'), 'missing ' + id);
  assert.ok(/id="fe_llm_budget_ceiling"[^>]*value=""/.test(html), 'ceiling should be empty');
  assert.ok(!/id="fe_llm_budget_allocation"[^>]*\bchecked\b/.test(html), 'allocation must start unchecked');
  assert.ok(/value="" selected/.test(html.slice(html.indexOf('fe_llm_budget_weekday'))), 'weekday should default to none');
});

test('the allocation checkbox is labelled as an estimate and the peer field says peers are off unless listed', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('buildLLMForm({name: "x", kind: "ollama"})', sandbox);
  assert.match(html, /\(an estimate\)/, 'allocation label must say it is an estimate');
  assert.match(html, /off for every peer unless the peer is listed here/, 'peer window hint missing');
});

test('collectLLMForm reads a weekly budget back using the REST field names', () => {
  const sandbox = loadApp();
  const record = {
    name: 'claude-pro', kind: 'claude-code',
    budget: { ceiling_usd: 25.5, weekday: 'friday', time: '09:30', zone: 'America/New_York', allocation_pct: true, peer_window: ['alpha', 'beta'] },
  };
  sandbox._llm = record;
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  const out = collectWith(sandbox, html, {}, record);
  assert.deepEqual(out.budget, {
    ceiling_usd: 25.5, weekday: 'friday', time: '09:30', zone: 'America/New_York',
    allocation_pct: true, peer_window: ['alpha', 'beta'],
  });
});

test('collectLLMForm builds a rolling budget from typed values and omits blank optional fields', () => {
  const sandbox = loadApp();
  sandbox._llm = { name: 'plain', kind: 'ollama' };
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  const out = collectWith(sandbox, html, {
    fe_llm_budget_ceiling: { value: '12' },
    fe_llm_budget_hours: { value: '24' },
    fe_llm_budget_peers: { value: ' a , ,b ' },
  });
  assert.deepEqual(out.budget, { ceiling_usd: 12, hours: 24, peer_window: ['a', 'b'] });
  assert.ok(!('weekday' in out.budget) && !('zone' in out.budget) && !('allocation_pct' in out.budget),
    'blank optional fields must not be sent');
});

test('an empty ceiling clears the stored budget by sending budget: null', () => {
  const sandbox = loadApp();
  const record = { name: 'claude-pro', kind: 'claude-code', budget: { ceiling_usd: 10, hours: 24 } };
  sandbox._llm = record;
  const html = vm.runInContext('buildLLMForm(_llm)', sandbox);
  const out = collectWith(sandbox, html, { fe_llm_budget_ceiling: { value: '' } }, record);
  assert.ok('budget' in out, 'budget key must be present so the daemon sees the clear');
  assert.equal(out.budget, null, 'empty ceiling must send budget: null');
});

test('capacity lines show spend of ceiling, window end, rollover and no badge or allocation when absent', () => {
  const sandbox = loadApp();
  sandbox._budgets = [{
    llm: 'claude-pro', ceiling_usd: 10, spend_usd: 3.2, peer_spend_usd: 0.3, remaining_usd: 6.5,
    window: 'rolling', window_start: '2026-10-21T10:00:00Z', window_end: '2026-10-22T10:00:00Z',
    rolls_over_at: '2026-10-22T11:00:00Z', full: false,
  }];
  const html = vm.runInContext('budgetCapacityLines(_budgets)', sandbox);
  assert.ok(html.includes('claude-pro'), 'LLM name missing: ' + html);
  assert.ok(html.includes('$3.50 of $10.00'), 'spend of ceiling missing: ' + html);
  assert.ok(html.includes('Window ends'), 'window end missing: ' + html);
  assert.ok(html.includes('Rolls over'), 'rollover time missing: ' + html);
  assert.ok(!html.includes('budget window full'), 'badge must not show while the window has room');
  assert.ok(!html.includes('Allocation'), 'allocation must not show when the daemon sent none');
});

test('capacity lines show the full badge and mark the allocation figure as estimated', () => {
  const sandbox = loadApp();
  sandbox._budgets = [{
    llm: 'claude-pro', ceiling_usd: 10, spend_usd: 10, peer_spend_usd: 0, remaining_usd: 0,
    window: 'weekly', window_start: '2026-10-19T00:00:00Z', window_end: '2026-10-26T00:00:00Z',
    rolls_over_at: '2026-10-26T00:00:00Z', full: true,
    allocation: { allocation_pct: 42.5, estimated: true, allocation_usd: 100, plan_tier: 'max_5x', basis: 'x' },
  }];
  const html = vm.runInContext('budgetCapacityLines(_budgets)', sandbox);
  assert.ok(html.includes('budget window full'), 'full badge missing: ' + html);
  assert.ok(html.includes('42.5%'), 'allocation figure missing: ' + html);
  assert.ok(/42\.5%[^<]*\(estimated\)/.test(html), 'allocation must be marked estimated: ' + html);
});

test('capacity lines are empty when no LLM has a budget', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext('budgetCapacityLines([])', sandbox), '');
  assert.equal(vm.runInContext('budgetCapacityLines(undefined)', sandbox), '');
});

test('capacity lines escape LLM names', () => {
  const sandbox = loadApp();
  sandbox._budgets = [{ llm: '<img src=x>', ceiling_usd: 1, spend_usd: 0, peer_spend_usd: 0, full: false }];
  const html = vm.runInContext('budgetCapacityLines(_budgets)', sandbox);
  assert.ok(!html.includes('<img'), 'LLM name must be escaped: ' + html);
});

test('autonomous settings expose autonomous.plan_tier_allocation_usd as a plan_tier_allocation_table field', () => {
  const sandbox = loadApp();
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.plan_tier_allocation_usd') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.plan_tier_allocation_usd missing from autonomous card fields');
  assert.equal(parsed.type, 'plan_tier_allocation_table');
  assert.equal(parsed.labelKey, 'settings_plan_tier_allocation_usd');
});

test('savePlanTierAllocationTable PUTs the per-tier dollar table (blank tier -> 0) to /api/config', async () => {
  const sandbox = loadApp();
  const captured = [];
  sandbox.fetch = (url, opts) => {
    captured.push({ url, opts });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
  };
  sandbox.document.querySelectorAll = (sel) => sel === 'input[data-plan-tier-allocation]' ? [
    { value: '20', dataset: { planTierAllocation: 'pro' } },
    { value: '99.5', dataset: { planTierAllocation: 'max_5x' } },
    { value: '', dataset: { planTierAllocation: 'max_20x' } },
    { value: '', dataset: { planTierAllocation: 'api' } },
  ] : [];
  vm.runInContext('savePlanTierAllocationTable()', sandbox);
  await flushAsync();
  const put = captured.find(c => c.url === '/api/config' && c.opts && c.opts.method === 'PUT');
  assert.ok(put, 'no PUT /api/config captured — got: ' + JSON.stringify(captured.map(c => c.url)));
  const body = JSON.parse(put.opts.body);
  assert.deepEqual(body['autonomous.plan_tier_allocation_usd'], { pro: 20, max_5x: 99.5, max_20x: 0, api: 0 },
    'PUT body must carry the allocation table; blank tier encodes 0');
});

test('app.js wires the budget group into the form, the collector and the capacity card', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.ok(src.includes('${buildLLMBudgetGroup(l)}'), 'LLM edit form does not include the budget group');
  assert.ok(src.includes('out.budget = collectLLMBudget()'), 'collectLLMForm omits the budget');
  assert.ok(src.includes('budgetCapacityLines(cap && cap.budgets)'), 'capacity card ignores cap.budgets');
});

test('all new budget i18n keys exist in all five locale bundles', () => {
  const keys = [
    'llm_budget_group',
    'llm_budget_hint',
    'llm_field_budget_ceiling',
    'llm_field_budget_hours',
    'llm_field_budget_weekday',
    'llm_budget_weekday_unset',
    'llm_field_budget_time',
    'llm_field_budget_zone',
    'llm_field_budget_allocation',
    'llm_field_budget_peers',
    'llm_budget_peers_hint',
    'capacity_budget_label',
    'capacity_budget_spend',
    'capacity_budget_window_end',
    'capacity_budget_rolls_over',
    'capacity_budget_full',
    'capacity_budget_allocation',
    'capacity_budget_estimated',
    'settings_plan_tier_allocation_usd',
    'settings_plan_tier_allocation_hint',
  ];
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
    }
    assert.ok(bundle.capacity_budget_spend.includes('{spend}') && bundle.capacity_budget_spend.includes('{ceiling}'),
      'capacity_budget_spend lost its placeholders in ' + locale);
  }
});
