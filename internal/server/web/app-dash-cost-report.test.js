// Dashboard "Cost report" card (id 'cost-report'): cache and local-model token
// columns, the totals line, the reconciliation against the usage readings
// (points against the pace line), the API-equivalent label on every dollar
// figure, and the DASH_CARD_FEATURES active() gate that keeps the card off
// while the report is unread or empty.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, vm } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
const HOSTILE = '<img src=x onerror=alert(1)>';

function load() {
  const sandbox = vm.createContext(buildSandbox());
  loadScript(APP_JS, sandbox);
  sandbox.__en = bundle('en');
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  return sandbox;
}
const evalIn = (sb, code) => vm.runInContext(code, sb);
const textOf = html => String(html).replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ');

const REPORT = {
  group_by: 'backend', retention_days: 31,
  totals: { sessions: 3, tokens_in: 1340, tokens_out: 710, cache_read: 100050, cache_creation: 20000, local_tokens: 550 },
  rows: [
    { key: 'claude-code', sessions: 1, tokens_in: 1000, tokens_out: 500, cache_read: 100000, cache_creation: 20000,
      local_tokens: 0, rate_configured: true, billing_mode: 'bundled_plan', billing_label: 'bundled plan', api_equivalent_usd: 4.5 },
    { key: 'pi-prompt', sessions: 2, tokens_in: 340, tokens_out: 210, cache_read: 50, cache_creation: 0,
      local_tokens: 550, rate_configured: false },
  ],
  reconciliation: {
    points: 5, pace_line: 3, ahead_of_pace: true, api_equivalent_usd: 4.5, implied_usd_per_point: 0.9,
    days: [{ date: '2026-10-06', points: 1, api_equivalent_usd: 0.5 }, { date: '2026-10-07', points: 4, api_equivalent_usd: 4 }],
  },
};

test('cost-report gate: off while unread or empty, on with rows or once touched', () => {
  const sb = load();
  const active = () => evalIn(sb, "DASH_CARD_FEATURES['cost-report'].active()");
  assert.equal(active(), false, 'report not read yet');
  evalIn(sb, '_dash._costReport.report = { rows: [] };');
  assert.equal(active(), false, 'empty report');
  sb.__r = REPORT;
  evalIn(sb, '_dash._costReport.report = __r;');
  assert.equal(active(), true, 'rows present');
  evalIn(sb, '_dash._costReport.report = { rows: [] }; _dash._costReport.userTouched = true;');
  assert.equal(active(), true, 'operator touched a control: keep the controls reachable');
  evalIn(sb, '_dash._costReport = null;');
  assert.equal(active(), false, 'no card state');
});

test('cost-report gate: off body says why in one line, with the reference link', () => {
  const sb = load();
  const html = evalIn(sb, "_dashCardOffBody(DASH_CARD_FEATURES['cost-report'])");
  assert.ok(textOf(html).includes(bundle('en').dash_card_off_cost_report_reason));
  assert.ok(html.includes('href='), 'links to the card reference');
  assert.equal((html.match(/<div/g) || []).length, 1, 'one line');
  evalIn(sb, '_dash._costReport.report = { rows: [] };');
  assert.equal(evalIn(sb, "_dashCardBodySafe({ id: 'cost-report', body: () => 'BODY' })"), html);
});

test('cache and local-model tokens render as columns and in the totals line', () => {
  const sb = load();
  const text = textOf(sb.costReportCardHtml(REPORT, {}));
  assert.ok(text.includes('Cache read/created'));
  assert.ok(text.includes('100000 / 20000'));
  assert.ok(text.includes('Local-model tokens'));
  assert.ok(text.includes('550'));
  assert.ok(text.includes('Totals: 1340 in · 710 out · 100050 cache read · 20000 cache created · 550 local-model'));
});

test('the local column and the totals line stay out when there is nothing to show', () => {
  const sb = load();
  const text = textOf(sb.costReportCardHtml({
    rows: [{ key: 'a', sessions: 1, tokens_in: 5, tokens_out: 5, cache_read: 0, cache_creation: 0, local_tokens: 0 }],
  }, {}));
  assert.ok(!text.includes('Local-model tokens'));
  assert.ok(!text.includes('Totals:'), 'no totals line without totals');
});

test('every dollar figure is labelled API-equivalent', () => {
  const sb = load();
  const html = sb.costReportCardHtml({
    rows: [{ key: 'a', sessions: 1, tokens_in: 1, tokens_out: 1, usd_estimate: 2.35, api_equivalent_usd: 4.5 }],
  }, { showUsd: true });
  const text = textOf(html);
  assert.ok(text.includes('$2.35 API-equivalent'));
  assert.ok(text.includes('$4.50 API-equivalent'));
  assert.ok(text.includes('API-equivalent USD'), 'column headers');
  assert.ok(!/\$\d+\.\d\d(?! API-equivalent)/.test(text), 'no bare dollar figure: ' + text);
  const rc = textOf(sb.costReportReconcileHtml(REPORT.reconciliation));
  assert.ok(!/\$\d+\.\d\d(?! API-equivalent)/.test(rc), 'reconciliation dollars labelled: ' + rc);
});

test('reconciliation shows points against the pace line and the burn per day', () => {
  const sb = load();
  const text = textOf(sb.costReportCardHtml(REPORT, {}));
  assert.ok(text.includes('Against the usage readings'));
  assert.ok(text.includes('5 points used against a pace line of 3 — ahead of the pace line'));
  assert.ok(text.includes('$4.50 API-equivalent over 5 points is $0.90 API-equivalent per point'));
  assert.ok(text.includes('2026-10-06: 1 points · $0.50 API-equivalent'));
  assert.ok(text.includes('2026-10-07: 4 points · $4.00 API-equivalent'));
  const within = textOf(sb.costReportReconcileHtml(Object.assign({}, REPORT.reconciliation, { ahead_of_pace: false, points: 2 })));
  assert.ok(within.includes('within the pace line'));
});

test('no readings: no reconciliation block, and malformed input never throws', () => {
  const sb = load();
  const rep = Object.assign({}, REPORT); delete rep.reconciliation;
  assert.ok(!textOf(sb.costReportCardHtml(rep, {})).includes('Against the usage readings'));
  for (const bad of [null, undefined, 'x', [], {}, { points: 'a', pace_line: 1 }, { points: 1 }]) {
    assert.equal(sb.costReportReconcileHtml(bad), '');
  }
  for (const bad of [null, 5, [], 'x']) assert.equal(sb.costReportTotalsHtml(bad), '');
  assert.equal(
    sb.costReportReconcileHtml({ points: 1, pace_line: 1, days: [null, { points: 'x' }, 7] }).includes('undefined'), false);
});

test('hostile text in a reconciliation date is escaped', () => {
  const sb = load();
  const html = sb.costReportReconcileHtml({ points: 1, pace_line: 2, ahead_of_pace: false,
    days: [{ date: HOSTILE, points: 1, api_equivalent_usd: 1 }] });
  assert.ok(!html.includes('<img'));
  assert.ok(html.includes('&lt;img'));
});

test('every new operator-visible string has a key in all five locales', () => {
  const keys = ['dash_card_off_cost_report_reason', 'dash_card_off_cost_report_link', 'cost_report_usd_api_value',
    'cost_report_column_cache', 'cost_report_cache_io', 'cost_report_column_local', 'cost_report_totals',
    'cost_report_reconcile_title', 'cost_report_reconcile_points', 'cost_report_reconcile_ahead',
    'cost_report_reconcile_within', 'cost_report_reconcile_implied', 'cost_report_reconcile_day'];
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].trim(), `${lang} lacks ${k}`);
    assert.ok(/API/.test(b.cost_report_usd_api_value), `${lang}: dollar label names the API`);
    for (const k of keys) assert.ok(!/\bPRD\b/.test(b[k]), `${lang}.${k} must not say PRD`);
  }
});
