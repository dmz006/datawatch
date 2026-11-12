// Cost report card — PWA side.
//
// window.buildCostReportQuery builds the GET /api/cost/report query string
// from the card's group_by / window (days) controls, and
// window.costReportCardHtml / window.costReportRowHtml render the decoded
// report body: tokens first, a USD figure only where the report actually
// carried a usd_estimate (and the show_usd_estimates preference allows it),
// plus the retention line (retention_days) and the lifetime-fallback note
// the daemon sends. Every value is escaped; a malformed body renders the
// empty state instead of throwing.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function query(params) {
  const sandbox = loadApp();
  sandbox._params = params;
  return vm.runInContext('window.buildCostReportQuery(_params)', sandbox);
}

function card(report, opts) {
  const sandbox = loadApp();
  sandbox._rep = report;
  sandbox._opts = opts;
  return vm.runInContext('window.costReportCardHtml(_rep, _opts)', sandbox);
}

function stripAll(s, re) {
  let prev;
  do { prev = s; s = s.replace(re, ''); } while (s !== prev);
  return s;
}

function textOf(html) {
  return stripAll(stripAll(html, /<[^>]*>/g), /&#\d+;|&#x[0-9a-f]+;/gi).trim();
}

// ── query construction ───────────────────────────────────────────────────────

test('buildCostReportQuery passes a valid group_by and a since window derived from now', () => {
  // 2026-11-01T00:00:00Z minus 7 days → since 2026-10-25 in the local day.
  const now = Date.UTC(2026, 10, 1, 0, 0, 0);
  const q = query({ groupBy: 'week', windowDays: 7, now });
  assert.ok(q.includes('group_by=week'), 'group_by missing: ' + q);
  assert.ok(/^since=\d{4}-\d{2}-\d{2}$/.test(q.split('&').find(p => p.startsWith('since='))), 'since shape wrong: ' + q);
});

test('buildCostReportQuery drops an unknown group_by and a sub-day window', () => {
  const now = Date.UTC(2026, 10, 1, 0, 0, 0);
  assert.equal(query({ groupBy: 'PRD', windowDays: 0.5, now }), '', 'must emit nothing for invalid input');
  assert.equal(query(null), '', 'null config');
  assert.equal(query(undefined, {}), '');
  assert.equal(query({ groupBy: 'backend' }), 'group_by=backend');
});

// ── card rendering ───────────────────────────────────────────────────────────

function report(over) {
  return Object.assign({
    group_by: 'week',
    rows: [
      { key: 'w2026-39', sessions: 3, tokens_in: 1200, tokens_out: 800, rate_configured: true, usd_estimate: 2.35 },
      { key: 'w2026-40', sessions: 1, tokens_in: 500, tokens_out: 200, rate_configured: false },
    ],
    retention_days: 31,
    lifetime_fallback: false,
  }, over);
}

test('the card is titled, linked, and shows the retention line and rows from the body', () => {
  const html = card(report());
  assert.ok(html.includes('Cost report'), 'title missing: ' + html);
  assert.ok(html.includes('datawatch-definitions.md#cost-report'), 'help href missing: ' + html);
  assert.ok(html.includes('About cost report'), 'help label missing');
  assert.ok(html.includes('Figures rest on the last 31 days of recorded sessions.'), 'retention line missing');
  assert.ok(html.includes('w2026-39') && html.includes('w2026-40'), 'group keys missing');
  assert.ok(html.includes('1200 in · 800 out'), 'tokens-first cell missing');
  assert.ok(html.includes('$2.35'), 'usd cell missing');
});

test('the card shows the lifetime-fallback note only when the body says so', () => {
  assert.ok(card(report({ lifetime_fallback: true })).includes('Some USD figures fall back to lifetime rates.'));
  assert.ok(!card(report({ lifetime_fallback: false })).includes('fall back to lifetime rates'));
  assert.ok(!card(report({ retention_days: undefined })).includes('days of recorded sessions.'));
});

test('usd appears only where the report carried it, and only while the preference allows', () => {
  const noUsd = report({ rows: [ { key: 'a', sessions: 1, tokens_in: 10, tokens_out: 2, rate_configured: false } ] });
  const html = card(noUsd);
  assert.ok(!html.includes('USD (est.)'), 'usd column must be absent when no row has usd_estimate');
  assert.ok(!html.includes('$'), 'no usd figures at all');
  assert.ok(html.includes('10 in · 2 out'), 'tokens still shown first');

  // show_usd_estimates=false hides the column even where data exists.
  const off = card(report(), { showUsd: false });
  assert.ok(!off.includes('USD (est.)'), 'preference off must drop the column');
  const on = card(report(), { showUsd: true });
  assert.ok(on.includes('USD (est.)') && on.includes('$2.35'));
  // Row with no figure renders the placeholder, not zero dollars.
  assert.ok(on.includes('>—<') || on.includes('—'), 'absent figure placeholder missing');
  assert.ok(!on.includes('$0.00'), 'no fake $0.00 for a rateless group');
});

test('the card shows the empty state for no rows, a missing list or a malformed body', () => {
  for (const body of [report({ rows: [] }), { group_by: 'week' }, null, undefined, 'x', []]) {
    const html = card(body);
    assert.ok(html.includes('No recorded sessions with token usage in this window.'), 'empty text missing for ' + JSON.stringify(body));
    assert.ok(!html.includes('cost-report-row'), 'no rows for an empty list');
  }
});

test('the card escapes every value and caps a long list', () => {
  const hostile = report({
    rows: [
      { key: HOSTILE, sessions: 1, tokens_in: 5, tokens_out: 1, usd_estimate: 0.5 },
      { key: '"><script>alert(2)</script>', sessions: 1, tokens_in: 1, tokens_out: 1 },
    ],
  });
  const html = card(hostile);
  assert.ok(!html.includes(HOSTILE), 'hostile markup reached the page: ' + html);
  assert.ok(!/<script>alert(2)/.test(html), 'script tag reached the page');

  const many = Array.from({ length: 60 }, (_, i) => ({ key: 'g' + i, sessions: 1, tokens_in: 1, tokens_out: 1, usd_estimate: 0.1 }));
  const big = card(report({ rows: many }));
  assert.equal((big.match(/class="cost-report-row"/g) || []).length, 50, 'one row per group, capped');
  assert.ok(big.includes('10 more groups not shown'), 'overflow note missing');
  assert.ok(card(report({ rows: many }), { maxRows: 5 }).includes('55 more groups not shown'), 'custom cap');
});

test('the help link follows the docs-link preference', () => {
  const sandbox = loadApp();
  sandbox.localStorage.getItem = k => (k === 'cs_show_docs_links' ? '0' : null);
  const html = vm.runInContext('window.costReportCardHtml({rows: []})', sandbox);
  assert.ok(!html.includes('cost-report-help'), 'the link must hide with the preference');
  assert.ok(html.includes('About cost report') === false, 'label must hide with the link');
});

test('no hard-coded setting defaults are visible in the card', () => {
  // Default group_by / window come from the daemon config, not the view.
  const html = card(report());
  assert.ok(!html.includes('default_window_days'), 'config key leaked into the view');
  assert.ok(!/\b7d\b|default 7 days/i.test(textOf(html)), 'no hard-coded default window wording');
});

test('the card never says PRD and names no tracker ID', () => {
  const html = textOf(card(report()));
  assert.ok(!/\bPRDs?\b/.test(html), 'PRD wording leaked: ' + textOf(html));
});

// ── locales: every surface the card and settings use is present everywhere ──

const mustHave = [
  'dash_cost_report',
  'cost_report_card_title',
  'cost_report_help_link',
  'cost_report_refresh',
  'cost_report_group_by',
  'cost_report_window_days',
  'cost_report_column_group',
  'cost_report_column_sessions',
  'cost_report_column_tokens',
  'cost_report_column_usd',
  'cost_report_retention_window',
  'cost_report_lifetime_fallback',
  'cost_report_empty',
  'cost_report_rows_not_shown',
  'settings_cost_report',
  'settings_cost_report_default_group_by',
  'settings_cost_report_default_window_days',
  'settings_cost_report_show_usd_estimates',
  'settings_cost_report_week_start',
  'settings_cost_report_include_peer_spend',
];

test('every locale has the cost-report and cost_report settings keys', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of mustHave) {
      assert.equal(typeof b[k], 'string', lang + ' missing ' + k);
      assert.ok(String(b[k]).length > 0, lang + ' empty ' + k);
    }
  }
});

test('cost report shows detail columns, window notes and the unknown marker', () => {
  const sb = loadApp();
  sb._i18n.bundle = bundle('en');
  const html = sb.costReportCardHtml({
    rows: [
      { key: 'a', sessions: 2, tokens_in: 1, tokens_out: 2, cache_read: 7, cache_creation: 8, tool_calls: 9, span_seconds: 125, active_seconds: 30 },
      { key: 'b', sessions: 1, tokens_in: 1, tokens_out: 2, spend_unknown: true },
    ],
    window_note: 'Partial window note.', unknown_spend_note: 'Spend before the ledger is unknown.',
  });
  for (const s of ['Cache read/created', 'Tool calls', 'Span', 'Active time', '7 / 8', '2m 5s', 'Partial window note.', 'before the ledger is unknown']) {
    assert.ok(html.includes(s), 'missing ' + s);
  }
  assert.ok(html.includes('unknown'));
});

test('every locale has the new cost detail keys', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ['automata_detail_cost_unknown', 'cost_report_unknown', 'cost_report_column_cache',
      'cost_report_column_tool_calls', 'cost_report_column_span', 'cost_report_column_active']) {
      assert.ok(b[k] && b[k].length, lang + ' missing ' + k);
    }
  }
});
