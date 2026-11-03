// app-usage-billing.test.js
//
// Covers the usage-billing surface of app.js:
//   1. `release` is an accepted cost-report grouping end-to-end (query
//      builder);
//   2. the cost report renders the billing columns (mode/label, usage
//      points, share of monthly price, API-equivalent unbilled, units)
//      ONLY when the report data actually carries them — never blank
//      columns — missing values show a dash, and the report's own
//      retention_note lands in the card, escaped;
//   3. the usage-billing settings card: profile parts ordering/format
//      (money + currency, reset weekday + time), the per-provider row
//      (localized mode badge, full actions when profiled), the
//      unprofiled row (localized "assumed default" badge, no remove,
//      no field list), the add/edit form (pinned vs free provider,
//      current values preselected), and the rows-union ordering;
//   4. loadUsageBillingConfig: one action-triggered fetch, disabled
//      backends are never assumed, rows + add affordance render into
//      #usageBillingList;
//   5. hostile strings (provider keys, server notes) never break out of
//      their elements (escHtml discipline, including the onclick
//      attribute that carries the provider key);
//   6. every locale bundle carries the new settings/cost-report keys.
//
// Same harness discipline as the sibling view tests: the REAL app.js is
// loaded into a stub browser context (testutil_browser_stub.js), with the
// EN bundle injected so label assertions read the shipped strings. No
// browser, no jest — just node:test + node:assert + vm.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = path.join(__dirname, 'locales');
const HOSTILE = '<img src=x onerror=alert(1)>';

const enBundle = () => JSON.parse(fs.readFileSync(path.join(LOCALES, 'en.json'), 'utf8'));

function textOf(s) {
  return s.replace(/<style[\s\S]*?<\/style>/g, ' ')
          .replace(/<[^>]+>/g, ' ')
          .replace(/&lt;/g, '<').replace(/&gt;/g, '>')
          .replace(/&quot;/g, '"').replace(/&#39;/g, "'")
          .replace(/&amp;/g, '&')
          .replace(/\s+/g, ' ').trim();
}

function load() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = enBundle();
  return sandbox;
}

// ── 1. The release grouping is part of the query contract ─────────────────

test('buildCostReportQuery accepts the release grouping', (t) => {
  const sb = load();
  const q = sb.buildCostReportQuery({ groupBy: 'release', windowDays: 30 });
  assert.ok(q.startsWith('group_by=release'), 'release must be an accepted grouping: ' + q);
  assert.ok(/&since=\d{4}-\d{2}-\d{2}$/.test(q), 'window days still apply');
  // Unknown groupings are dropped, not passed through.
  const bad = sb.buildCostReportQuery({ groupBy: 'bogus', windowDays: 30 });
  assert.ok(!bad.includes('group_by'), 'bogus grouping must not leak into the query');
});

// ── 2. Billing columns: data-driven, never blank ──────────────────────────

test('report columns appear only when the report carries the fields', (t) => {
  const sb = load();
  const html = sb.costReportCardHtml({
    group_by: 'automaton', retention_days: 31,
    rows: [{ key: 'automaton', sessions: 2, tokens_in: 10, tokens_out: 20 }],
  }, {});
  const text = textOf(html);
  // Base columns:
  assert.ok(text.includes('Group'));
  assert.ok(text.includes('Sessions'));
  assert.ok(text.includes('Tokens'));
  // Billing columns must not appear without billing data:
  assert.ok(!text.includes('Billing'));
  assert.ok(!text.includes('Share of price'));
  assert.ok(!text.includes('API-equiv'));
});

test('billing columns render values and dashes where a row lacks them', (t) => {
  const sb = load();
  const html = sb.costReportCardHtml({
    group_by: 'automation', retention_days: 31,
    rows: [
      { key: 'automaton', sessions: 2, tokens_in: 10, tokens_out: 20,
        billing_mode: 'bundled_plan', billing_label: 'claude max · 90 days',
        estimated_usage_points: 950.5, share_of_monthly_price: 0.125,
        api_equivalent_usd: 3.4, units: 12 },
      { key: 'datawatch', sessions: 1, tokens_in: 1, tokens_out: 1 },
    ],
  }, {});
  const text = textOf(html);
  for (const h of ['Billing', 'Points', 'Share of price', 'API-equiv (unbilled)', 'Units']) {
    assert.ok(text.includes(h), 'header "' + h + '" must render when any row has billing data');
  }
  assert.ok(text.includes('950.5 pts'), 'usage points value');
  assert.ok(text.includes('12.5%'), 'share-of-price value (12.5%)');
  assert.ok(text.includes('$3.40'), 'api-equivalent value');
  assert.ok(text.includes('claude max · 90 days'), 'billing label');
  assert.ok(text.includes('12'), 'units value');
});

test('billing columns stay hidden when every value is blank or null', (t) => {
  const sb = load();
  const html = sb.costReportCardHtml({
    group_by: 'automaton', retention_days: 31,
    rows: [{
      key: 'a', sessions: 1, tokens_in: 1, tokens_out: 1,
      billing_mode: '', billing_label: '', estimated_usage_points: null,
      share_of_monthly_price: '', units: null,
    }],
  }, {});
  const text = textOf(html);
  assert.ok(!text.includes('Points'), 'points hidden for null data');
  assert.ok(!text.includes('Share of price'), 'share hidden for blank data');
  assert.ok(!text.includes('Units'), 'units hidden for null data');
  assert.ok(!text.includes('API-equiv'), 'api-equiv hidden when not carried');
  // costReportFieldHas is the presence predicate:
  assert.equal(sb.costReportFieldHas([{ units: null }], 'units'), false);
  assert.equal(sb.costReportFieldHas([{ units: '' }], 'units'), false);
  assert.equal(sb.costReportFieldHas([{ units: 0 }], 'units'), true, 'zero is real data');
  assert.equal(sb.costReportFieldHas([], 'units'), false);
});

// ── 3. The report's own retention note ────────────────────────────────────

test('retention_note from the report body renders, escaped', (t) => {
  const sb = load();
  const html = sb.costReportCardHtml({
    group_by: 'automaton', retention_note: HOSTILE + ' 31 days', rows: [],
  }, {});
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'), 'note must be escaped');
  assert.ok(!/<img/.test(html), 'no raw img tag in the card');
});

// ── 4. The usage billing settings card ────────────────────────────────────

test('mode and unit labels resolve from the bundle; unknown modes render raw', (t) => {
  const sb = load();
  assert.equal(sb._usageBillingModeLabel('bundled_plan'), 'bundled plan');
  assert.equal(sb._usageBillingModeLabel('api_metered'), 'API metered');
  assert.equal(sb._usageBillingModeLabel('free_local'), 'free / local');
  assert.equal(sb._usageBillingModeLabel(''), '');
  assert.equal(sb._usageBillingModeLabel('custom_future_mode'), 'custom_future_mode');
  assert.equal(sb._usageBillingUnitLabel('percent'), 'percent');
  assert.equal(sb._usageBillingUnitLabel('tokens'), 'tokens');
  assert.equal(sb._usageBillingUnitLabel('requests'), 'requests');
});

test('profile parts are ordered and formatted; unsets are blank for the dash', (t) => {
  const sb = load();
  const parts = sb._usageBillingProfileParts({
    mode: 'bundled_plan', plan: 'max5', monthly_price: 120, currency: 'eur',
    reset_weekday: 'friday', reset_time: '00:00', allowance_unit: 'percent',
  });
  // (compare elementwise: the parts array is a vm-realm Array, which
  // deepStrictEqual refuses as "same structure, different realm")
  assert.deepEqual(Array.from(parts.map(p => p.label)), ['Mode', 'Plan', 'Monthly price', 'Reset', 'Allowance unit']);
  assert.equal(parts[0].value, 'bundled plan');
  assert.equal(parts[1].value, 'max5');
  assert.equal(parts[2].value, 'EUR 120', 'currency uppercased + amount');
  assert.equal(parts[3].value, 'friday 00:00', 'reset weekday + time');
  assert.equal(parts[4].value, 'percent');

  const empty = sb._usageBillingProfileParts({});
  assert.ok(empty.every(p => p.value === ''), 'unset fields carry empty values');
  const priceOnly = sb._usageBillingProfileParts({ monthly_price: 50 }).map(p => p.value);
  assert.equal(priceOnly[2], '50', 'amount shows even without a currency');
});

test('a profiled provider row shows its fields, mode badge and full actions', (t) => {
  const sb = load();
  const html = sb._usageBillingRowHtml('claude-code', {
    mode: 'bundled_plan', plan: 'max5', monthly_price: 120, currency: 'USD',
    reset_weekday: 'friday', reset_time: '00:00', allowance_unit: 'percent',
  });
  const text = textOf(html);
  assert.ok(text.includes('claude-code'));
  assert.ok(text.includes('bundled plan'), 'mode badge');
  assert.ok(text.includes('max5'), 'plan value');
  assert.ok(text.includes('USD 120'), 'money value');
  assert.ok(text.includes('friday 00:00'), 'reset value');
  assert.ok(html.includes('window._usageBillingOpen'), 'edit action');
  assert.ok(html.includes('window._usageBillingRemove'), 'remove action');
});

test('an unprofiled provider row gets the assumed-default badge and no remove', (t) => {
  const sb = load();
  const html = sb._usageBillingRowHtml('some-provider', undefined);
  assert.ok(textOf(html).includes('assumed default'), 'assumed badge');
  assert.ok(html.includes('window._usageBillingOpen'), 'edit action still present');
  assert.ok(!html.includes('window._usageBillingRemove'), 'no remove on an assumed profile');
  assert.ok(!textOf(html).includes('Monthly price'), 'no field list on an assumed profile');
});

test('hostile provider names cannot inject markup, even via the onclick key', (t) => {
  const sb = load();
  const html = sb._usageBillingRowHtml(HOSTILE, { mode: 'api_metered' });
  assert.ok(!html.includes('<img'), 'no raw img tag in the row');
  const m = html.match(/_usageBillingOpen\('([^']*)'\)/);
  assert.ok(m, 'edit onclick with the provider key');
  assert.ok(m[1].includes('&lt;img'), 'key escaped inside the onclick attribute');
});

test('rows cover the union of configured and known providers, sorted', (t) => {
  const sb = load();
  const html = sb._usageBillingRowsHtml(
    { 'claude-code': { mode: 'bundled_plan' } },
    [{ name: 'zeta', disabled: false }, { name: 'alpha', disabled: false }],
  );
  const text = textOf(html);
  assert.ok(text.includes('claude-code') && text.includes('alpha') && text.includes('zeta'), 'union of names');
  const pos = n => text.indexOf(n);
  assert.ok(pos('alpha') < pos('claude-code') && pos('claude-code') < pos('zeta'), 'sorted');
  // A profiled row and an assumed row coexist in the same card:
  assert.ok(html.includes('bundled plan') && html.includes('assumed default'));
});

test('the form pins the provider in edit mode and leaves it free in add mode', (t) => {
  const sb = load();
  const edit = sb._usageBillingFormHtml('claude-code', { mode: 'bundled_plan', reset_weekday: 'monday' }, false);
  assert.ok(/id="ubProvider" value="claude-code" disabled/.test(edit), 'pinned provider');
  assert.ok(/<option value="bundled_plan" selected/.test(edit), 'current mode preselected');
  assert.ok(/<option value="monday" selected/.test(edit), 'current weekday preselected');

  const add = sb._usageBillingFormHtml('', {}, true);
  const providerTag = (add.match(/id="ubProvider"[^>]*>/) || [''])[0];
  assert.ok(providerTag.includes('placeholder'), 'provider is a free field in add mode');
  assert.ok(!providerTag.includes('disabled'), 'provider editable in add mode');
  for (const id of ['ubMode', 'ubPlan', 'ubPrice', 'ubCurrency', 'ubWeekday', 'ubTime', 'ubUnit']) {
    assert.ok(add.includes('id="' + id + '"'), 'form field ' + id);
  }
});

test('the load action fetches config + backends and renders the card without assuming disabled ones', async (t) => {
  let listHtml = '';
  const listEl = makeStubElement(v => { listHtml = v; });
  const sandbox = buildSandbox({ byId: { usageBillingList: listEl } });
  vm.createContext(sandbox);
  sandbox.fetch = (url) => {
    if (url === '/api/config') {
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ usage: { billing: { 'claude-code': { mode: 'bundled_plan', plan: 'max5' } } } }) });
    }
    if (url === '/api/llms') {
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ llms: [{ name: 'alpha' }, { name: 'ghost', disabled: true }] }) });
    }
    return Promise.resolve({ ok: false, status: 404 });
  };
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = enBundle();
  sandbox.loadUsageBillingConfig();
  await flushAsync(20);
  const text = textOf(listHtml);
  assert.ok(text.includes('claude-code'), 'configured provider rendered');
  assert.ok(text.includes('alpha'), 'known backend rendered');
  assert.ok(!text.includes('ghost'), 'disabled backends are never assumed');
  assert.ok(listHtml.includes('bundled plan'), 'profile badge rendered');
  assert.ok(text.includes('assumed default'), 'unprofiled backend badged');
  assert.ok(listHtml.includes('window._usageBillingOpenNew'), 'add affordance');
  // The card keeps its state for the form/save paths — no second fetch needed.
  assert.ok(sandbox._usageBilling && sandbox._usageBilling.billing['claude-code'], 'state cached on window');
});

// ── 6. Locale bundles carry the new keys ─────────────────────────────────

test('every locale bundle carries the new usage-billing and cost-report keys', (t) => {
  const newKeys = [
    'settings_usage_billing_hint',
    'settings_usage_billing_empty',
    'settings_usage_billing_add',
    'settings_usage_billing_provider',
    'settings_usage_billing_mode',
    'settings_usage_billing_plan',
    'settings_usage_billing_monthly_price',
    'settings_usage_billing_currency',
    'settings_usage_billing_reset',
    'settings_usage_billing_reset_weekday',
    'settings_usage_billing_reset_time',
    'settings_usage_billing_allowance_unit',
    'settings_usage_billing_unset',
    'settings_usage_billing_assumed',
    'settings_usage_billing_assumed_hint',
    'settings_usage_billing_remove',
    'settings_usage_billing_remove_confirm',
    'settings_usage_billing_provider_missing',
    'settings_usage_billing_saved',
    'settings_usage_billing_save_failed',
    'settings_usage_billing_removed',
    'settings_usage_billing_mode_bundled_plan',
    'settings_usage_billing_mode_api_metered',
    'settings_usage_billing_mode_free_local',
    'settings_usage_billing_unit_percent',
    'settings_usage_billing_unit_requests',
    'settings_usage_billing_unit_tokens',
    'cost_report_column_billing',
    'cost_report_column_points',
    'cost_report_column_share',
    'cost_report_column_api_equiv',
    'cost_report_column_units',
    'cost_report_points_value',
    'cost_report_share_value',
    'cost_report_group_release',
  ];
  for (const loc of ['en', 'de', 'es', 'fr', 'ja']) {
    const data = JSON.parse(fs.readFileSync(path.join(LOCALES, loc + '.json'), 'utf8'));
    for (const k of newKeys) {
      assert.equal(typeof data[k], 'string', loc + '.' + k);
      assert.ok(data[k].length > 0, loc + '.' + k + ' non-empty');
    }
  }
});
