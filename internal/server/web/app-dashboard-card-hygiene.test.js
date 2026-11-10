// 2026-11-02 dashboard card audit (docs/plans/2026-11-02-dashboard-card-audit.md)
// hygiene test. Same style as app-dashboard-aggregation.test.js: loads the
// real, unmodified app.js into a Node vm sandbox (via testutil_browser_stub)
// and exercises its own definitions and grid builder, plus checks the
// English locale bundle directly against the five locale files on disk.
//
// Proves the audit's three UI guarantees:
//   (a) duplicate card ids in a layout render once, not twice
//       (_dashBuildGrid's defensive dedupe);
//   (b) the subsystem tiles the audit dropped from the default layout
//       (guardrails, smoke, memory-scope, websearch-usage) are NOT in
//       DASH_DEFAULT_LAYOUT — while staying in DASH_CARD_DEFS, i.e.
//       "remains addable" via Edit → Add Card when their feature has data;
//   (c) a feature card whose subsystem has no data is hidden while
//       `dashboard.hide_inactive_cards` is on (the default), and with it
//       off renders a one-line reason plus a link to the related setting's
//       docs page (resolved through the EN bundle — a raw key rendered
//       would mean the locale file lost the key, exactly the failure the
//       locale-coverage guard exists to catch).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const { buildSandbox, makeStubElement, loadScript, vm, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const EN_LOCALE = 'en';

// Subsystem tiles the 2026-11-02 audit dropped from the default dashboard
// layout (they stay available in Edit → Add Card).
const TILE_IDS = ['guardrails', 'smoke', 'memory-scope', 'websearch-usage'];

// (key, reason text from the EN bundle) pairs used by the off-body test.
const OFF_CARDS = [
  { id: 'smoke', docsKey: 'dash_card_smoke', reasonKey: 'dash_card_off_smoke_reason', linkKey: 'dash_card_off_smoke_link' },
  { id: 'memory-scope', docsKey: 'dash_card_memory_scope', reasonKey: 'dash_card_off_memory_reason', linkKey: 'dash_card_off_memory_link' },
  { id: 'guardrails', docsKey: 'dash_card_guardrails', reasonKey: 'dash_card_off_guardrails_reason', linkKey: 'dash_card_off_guardrails_link' },
  { id: 'websearch-usage', docsKey: 'dash_card_websearch_usage', reasonKey: 'dash_card_off_websearch_reason', linkKey: 'dash_card_off_websearch_link' },
  { id: 'integration-status', docsKey: 'dash_card_integration_status', reasonKey: 'dash_card_off_integration_reason', linkKey: 'dash_card_off_integration_link' },
];

// Where each off-card's docs link must point (the related setting's help
// page, per docs/plans/2026-11-02-dashboard-card-audit.md), URL-encoded
// exactly the way _dashCardOffBody builds it:
// /diagrams.html#docs/<DOCS_LINKS_BY_KEY value>.
const OFF_EXPECTED_HREF = {
  smoke: '/diagrams.html#docs/datawatch-definitions.md%23dashboard',
  guardrails: '/diagrams.html#docs/datawatch-definitions.md%23dashboard',
  'memory-scope': '/diagrams.html#docs/memory.md',
  'websearch-usage': '/diagrams.html#docs/datawatch-definitions.md%23web-search-multi-provider-registry',
  'integration-status': '/diagrams.html#docs/datawatch-definitions.md%23recurring-named-schedules',
};

function readLocale(name) {
  const raw = fs.readFileSync(path.join(LOCALES_DIR, name + '.json'), 'utf8');
  return JSON.parse(raw);
}

function loadDashboardApp() {
  // Capture whatever _dashBuildGrid writes into #dashCardGrid so tests can
  // inspect the rendered card HTML without a real DOM.
  const grid = makeStubElement();
  const sandbox = buildSandbox({ byId: { dashCardGrid: grid } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // The PWA loads i18n bundles asynchronously over fetch(); the sandbox's
  // fetch stub 404s, so install the real EN bundle directly — exactly what
  // the browser ends up with after the locale fetch resolves.
  sandbox.__en = readLocale(EN_LOCALE);
  vm.runInContext(
    'window._i18n.bundle = __en; window._i18n.fallback = __en; window._i18n.ready = true;',
    sandbox
  );
  delete sandbox.__en;
  return { sandbox, grid };
}

// buildLayout runs _dashBuildGrid(layout) inside the sandbox.
function buildLayout({ sandbox, grid }, layout) {
  sandbox.__layout = layout;
  vm.runInContext(
    'Object.assign(_dash, { _layout: __layout }); _dashBuildGrid(__layout);',
    sandbox
  );
  delete sandbox.__layout;
  return grid.innerHTML;
}

// countId counts rendered card wrappers for a card id — exactly one
// `data-card-id="..."` attribute per rendered card, in both the normal-body
// and the one-line-off-body modes (the off body has no element id of its
// own, so this is the only marker that works for both).
function countId(html, cardId) {
  const re = new RegExp('data-card-id="' + cardId + '"', 'g');
  return (html.match(re) || []).length;
}

test('2026-11-02 audit: locale bundles carry the five dash_card_off_* key pairs', async (t) => {
  const en = readLocale(EN_LOCALE);
  for (const off of OFF_CARDS) {
    for (const k of [off.reasonKey, off.linkKey]) {
      assert.equal(typeof en[k], 'string', `en bundle missing key "${k}"`);
      assert.ok(en[k].length > 0, `en bundle key "${k}" is empty`);
    }
  }
  // EN parity: every EN key must exist in every other supported bundle, and
  // no bundle may carry keys EN lacks (the Go LocaleTests enforce this too;
  // asserting it here fails the PWA test if a bundle drifts).
  const all = Object.keys(en).sort();
  for (const name of ['de', 'es', 'fr', 'ja']) {
    const other = readLocale(name);
    for (const k of all) assert.ok(Object.prototype.hasOwnProperty.call(other, k), `${name} missing key "${k}"`);
    for (const k of Object.keys(other)) assert.ok(Object.prototype.hasOwnProperty.call(en, k), `${name} has key "${k}" missing from en`);
  }
});

test('2026-11-02 audit: audit-dropped tiles absent from DASH_DEFAULT_LAYOUT, still in DASH_CARD_DEFS (remains addable)', async (t) => {
  const { sandbox } = loadDashboardApp();
  const layoutIds = vm.runInContext('DASH_DEFAULT_LAYOUT.map(c => c.id)', sandbox);
  const defIds = vm.runInContext('DASH_CARD_DEFS.map(c => c.id)', sandbox);
  for (const id of TILE_IDS) {
    assert.ok(!layoutIds.includes(id), `default layout still contains "${id}" — the 2026-11-02 audit dropped it`);
    assert.ok(defIds.includes(id), `card definition "${id}" missing from DASH_CARD_DEFS — Edit → Add Card would not offer it (remains addable)`);
  }
  // Duplicate ids in DASH_CARD_DEFS would make the Add-Card picker show a
  // card twice and let one card shadow another's renderer registration.
  const dup = defIds.filter((x, i) => defIds.indexOf(x) !== i);
  assert.equal(dup.length, 0, `duplicate ids in DASH_CARD_DEFS: ${dup.join(', ')}`);
  // The audit-kept cards are all still there — the audit changed the default
  // layout only, it did not remove any card type.
  for (const id of ['tree', 'orbital', 'events', 'sparklines', 'gantt', 'heatmap', 'ekg']) {
    assert.ok(layoutIds.includes(id), `default layout lost "${id}" — the audit kept it`);
  }
});

test('2026-11-02 audit: duplicate layout ids render once, not twice', async (t) => {
  const app = loadDashboardApp();
  const html = buildLayout(app, [
    { id: 'ekg', cs: 2, rs: 2 },
    { id: 'ekg', cs: 3, rs: 1 },
    { id: 'ekg', cs: 12, rs: 2 },
    { id: 'guardrails', cs: 3, rs: 2 },
    { id: 'guardrails', cs: 6, rs: 2 },
  ]);
  assert.equal(countId(html, 'ekg'), 1, 'ekg card rendered once despite three layout entries');
  assert.equal(countId(html, 'guardrails'), 0, 'guardrails must be hidden by the default (off, feature has no data) — and any duplicate entry would still render at most once');
  // With the toggle off, the dedupe still applies: one guardrails wrapper,
  // not two.
  vm.runInContext('Object.assign(state, { _dashHideInactiveCards: false });', app.sandbox);
  const html2 = buildLayout(app, [
    { id: 'guardrails', cs: 3, rs: 2 },
    { id: 'guardrails', cs: 6, rs: 2 },
  ]);
  assert.equal(countId(html2, 'guardrails'), 1, 'duplicate guardrails ids rendered twice (dedupe broken)');
});

test('2026-11-02 audit: feature-off card hidden by default (dashboard.hide_inactive_cards default true)', async (t) => {
  const app = loadDashboardApp();
  // state starts without `_dashHideInactiveCards` set — the default-true
  // path. Every off card is requested in the layout; none may render.
  const layout = OFF_CARDS.map(c => ({ id: c.id, cs: 6, rs: 2 }));
  const html = buildLayout(app, layout);
  for (const off of OFF_CARDS) {
    assert.equal(countId(html, off.id), 0, `"${off.id}" card rendered although its feature has no data and hide-inactive is on (default)`);
    assert.ok(html.indexOf(off.reasonKey) === -1, `raw key ${off.reasonKey} leaked into the grid`);
  }
});

test('2026-11-02 audit: with hide_inactive_cards=false the card shows its reason + docs link', async (t) => {
  const app = loadDashboardApp();
  vm.runInContext('Object.assign(state, { _dashHideInactiveCards: false });', app.sandbox);
  const en = app.sandbox.window._i18n.bundle;
  for (const off of OFF_CARDS) {
    const html = buildLayout(app, [{ id: off.id, cs: 6, rs: 2 }]);
    // Reason text is present — AND it is the translated text from the EN
    // bundle, not the raw key (t() returns the key on miss, which would
    // mean the locale file lost this key).
    assert.ok(html.indexOf(en[off.reasonKey]) !== -1,
      `reason text for "${off.id}" ("${en[off.reasonKey]}") not rendered with hide_inactive_cards=false`);
    assert.ok(html.indexOf(off.reasonKey) === -1, `raw key ${off.reasonKey} rendered instead of its EN value`);
    // Link text + a link to the related setting's docs page, built by
    // _dashCardOffBody from DOCS_LINKS_BY_KEY — must point at the real,
    // related fragment (not a missing-anchor or unrelated page).
    assert.ok(en[off.linkKey] && html.indexOf(en[off.linkKey]) !== -1,
      `docs link text for "${off.id}" (${en[off.linkKey]}) not rendered`);
    assert.ok(html.indexOf(OFF_EXPECTED_HREF[off.id]) !== -1,
      `docs link for "${off.id}" must point at ${OFF_EXPECTED_HREF[off.id]} — the related setting's help page`);
  }
  // Sanity: once the feature HAS data, the card renders its real body
  // (not the off-line) — the off-line is a "no data yet" state, not a
  // permanent replacement.
  vm.runInContext(
    '_dash._smokeEnvelopes = [{ started_at: "2026-11-02T12:00:00Z", finished_at: "2026-11-02T12:01:00Z", results: [] }];',
    app.sandbox
  );
  const html = buildLayout(app, [{ id: 'smoke', cs: 6, rs: 2 }]);
  assert.ok(html.indexOf('dashSmokeCard') !== -1, 'smoke card with data must render its real body');
  await flushAsync(3);
});
