'use strict';
// GH#204 phase 4 — "Integration status" dashboard card (opt-in).
//
// Loads the REAL app.js into the shared stub-browser sandbox (no bundler,
// no browser — Node vm) and covers:
//   * the card is opt-in: present in DASH_CARD_DEFS, NOT in
//     DASH_DEFAULT_LAYOUT, and listed prominently (top half) so the
//     Add-Card picker shows it up front, not buried at the end;
//   * the card label + picker entry resolve through t('dash_integ_status')
//     so they pick up the operator's locale instead of a static string;
//   * row rendering: schedule name, status badge, escaped summary/title,
//     relative posted time, retained count, click-through into the posting
//     session, and an inline SVG sparkline of recent_statuses (oldest →
//     newest) bounded at 60 bars;
//   * hostile schedule_name / payload.summary / session_id values are
//     escaped for BOTH html and the inline onclick JS attribute — this is
//     the same discipline as the SEC-021 / BL394 fixes;
//   * empty list → explanatory empty state with the result-panel howto
//     link; fetch failure → error state, no crash, no raw t() keys.
//
// Run:  node --test internal/server/web/app-integration-status-card.test.js
// (or as part of:  node --test internal/server/web/*.test.js)

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } =
  require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN_BUNDLE = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

// loadSandbox runs the real app.js top-level code to completion (it would
// throw here if the new card code had a syntax/scope error), then makes t()
// resolve against the EN bundle exactly as the server would serve it.
function loadSandbox({ byId = {} } = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = Object.assign({}, EN_BUNDLE);
  sandbox._i18n.fallback = Object.assign({}, EN_BUNDLE);
  return sandbox;
}

// sampleResult returns one /api/schedule-results list element in the exact
// shape the API documents (ScheduleResultSummary), with a healthy ok result.
function sampleResult() {
  return {
    schedule_name: 'nightly-sweep',
    count: 12,
    recent_statuses: ['ok', 'ok', 'warn', 'ok', 'error', 'warn', 'ok'],
    latest: {
      payload: {
        title: 'Nightly sweep',
        status: 'ok',
        summary: 'All 24 checks passed in 3m12s.',
      },
      session_id: 'sess_1234',
      schedule_name: 'nightly-sweep',
      posted_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
    },
  };
}

// stubFetch makes apiFetch('/api/schedule-results') resolve with data.
function stubFetch(sandbox, data) {
  sandbox.fetch = async () => ({ ok: true, status: 200, json: async () => data, text: async () => '' });
}

test('card is opt-in: defined, not in default layout, listed prominently', () => {
  const s = loadSandbox();
  const defs = vm.runInContext('DASH_CARD_DEFS', s);
  const layout = vm.runInContext('DASH_DEFAULT_LAYOUT', s);
  const def = defs.find(d => d.id === 'integration-status');
  assert.ok(def, 'card definition exists in DASH_CARD_DEFS');
  assert.ok(!layout.some(e => e.id === 'integration-status'),
    'card must NOT be in DASH_DEFAULT_LAYOUT (opt-in)');
  const idx = defs.indexOf(def);
  assert.ok(idx >= 0 && idx * 2 < defs.length,
    `card listed prominently near the top of the picker (idx ${idx} of ${defs.length})`);
  assert.equal(typeof def.body, 'function', 'card has a body() renderer');
  assert.match(def.body(), /id="dashIntegrationStatusCard"/);
});

test('card label resolves through t() and follows the active bundle', () => {
  const s = loadSandbox();
  const def = vm.runInContext("DASH_CARD_DEFS.find(d => d.id === 'integration-status')", s);
  assert.equal(s.t('dash_integ_status'), 'Integration status'); // EN bundle
  assert.equal(s._dashDefLabel(def), 'Integration status');
  // Switch the live bundle → label must follow (locale-aware, not static).
  s._i18n.bundle = Object.assign({}, EN_BUNDLE, { dash_integ_status: 'Statut des intégrations' });
  assert.equal(s._dashDefLabel(def), 'Statut des intégrations');
});

test('row renders name, badge, summary, time, retained count, sparkline, click-through', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  stubFetch(s, [sampleResult()]);
  s._dashFetchIntegStatus();
  await flushAsync();
  const html = el.innerHTML;

  assert.match(html, /nightly-sweep/, 'schedule name');
  assert.match(html, /All 24 checks passed in 3m12s\./, 'payload summary');
  assert.match(html, /Nightly sweep/, 'payload title');
  assert.match(html, /integ-dot/); // badge bullet
  assert.ok(html.includes('>' + EN_BUNDLE.status_result_ok + '</span>'), 'badge label = t(status_result_ok) value');
  assert.match(html, /<svg/, 'inline svg sparkline present');
  assert.match(html, /\d+ retained/, 'retained count via t(dash_integ_retained, [count])');
  assert.match(html, /_integOpenSession\('sess_1234'\)/, 'click-through with session id');
  assert.ok(html.indexOf('warn') !== -1, 'warn status present in mixed history');
});

test('sparkline is bounded at 60 bars and shows all three status colors', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  const r = sampleResult();
  r.count = 200;
  const statuses = [];
  for (let i = 0; i < 200; i++) statuses.push(['ok', 'warn', 'error'][i % 3]);
  r.recent_statuses = statuses;
  stubFetch(s, [r]);
  s._dashFetchIntegStatus();
  await flushAsync();
  const bars = el.innerHTML.match(/<rect /g) || [];
  assert.equal(bars.length, 60, 'sparkline caps at 60 bars even with 200 retained');
  assert.match(el.innerHTML, /var\(--success,#10b981\)/, 'ok color');
  assert.match(el.innerHTML, /var\(--warning,#f59e0b\)/, 'warn color');
  assert.match(el.innerHTML, /var\(--error,#ef4444\)/, 'error color');
});

test('hostile schedule name, summary and session id are escaped in html and onclick', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  const r = sampleResult();
  r.schedule_name = `<script>alert(1)</script>`;
  r.latest.payload.summary = `" onclick="alert(2)"`;
  r.latest.payload.status = 'error';
  r.latest.session_id = `s'drop;alert(3)`;
  stubFetch(s, [r]);
  s._dashFetchIntegStatus();
  await flushAsync();
  const html = el.innerHTML;

  assert.ok(!html.includes('<script>'), 'no raw <script> injection from schedule_name');
  assert.ok(html.includes('&lt;script&gt;'), 'hostile name escaped as entities');
  assert.ok(!html.includes('" onclick="alert(2)"'), 'no attribute breakout from summary');
  assert.ok(html.includes("s\\\u0026#39;drop;alert(3)"),
    'session id backslash-escaped for the inline onclick (BL394 discipline)');
  assert.match(html, /integ-dot/, 'error badge still renders');
});

test('empty list renders the howto-guiding empty state with no raw t() keys', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  stubFetch(s, []);
  s._dashFetchIntegStatus();
  await flushAsync();
  const html = el.innerHTML;

  assert.match(html, /integ-empty/);
  assert.ok(html.includes(EN_BUNDLE.dash_integ_empty_title), 'localized empty title');
  assert.ok(html.includes('/api/openapi.yaml'), 'links the API reference (result endpoint)');
  // Every new locale key that renders here must resolve — raw keys = misses.
  const renderedText = html.replace(/&\w+;|&#\d+;/g, '');
  for (const key of ['dash_integ_empty_title', 'dash_integ_empty', 'dash_integ_empty_link']) {
    assert.ok(!renderedText.includes(key), `raw locale key ${key} leaked into the UI`);
  }
});

test('fetch failure renders an error state (and does not crash the loop)', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  s.fetch = async () => { throw new Error('network down'); };
  s._dashFetchIntegStatus();
  await flushAsync();
  assert.match(el.innerHTML, /integ-error/, 'error state rendered');
  assert.ok(el.innerHTML.includes('daemon') || el.innerHTML.includes('démon') || el.innerHTML.includes('Daemon'),
    'localized error text present');
  assert.ok(!el.innerHTML.includes('dash_integ_error'), 'no raw locale key leaked');
});

test('missing latest/payload still renders a name-only row without throwing', async () => {
  const el = makeStubElement();
  const s = loadSandbox({ byId: { dashIntegrationStatusCard: el } });
  stubFetch(s, [{ schedule_name: 'orphan-schedule', count: 1 }]);
  s._dashFetchIntegStatus();
  await flushAsync();
  assert.match(el.innerHTML, /orphan-schedule/);
  assert.ok(!el.innerHTML.includes('<svg>'), 'no sparkline without recent_statuses');
  assert.equal(typeof s._integOpenSession, 'function', 'click-through helper exposed');
});
