// Dashboard series cards — lane utilization (busy share per lane per hour),
// throughput forecast (trend points with the completion range) and verifier
// health (waits and runs per node, pass rate for all nodes). Covers the feature
// gates that keep each card off until it has data, the render output for a
// hatched unrecorded hour and a node table, the card definitions staying out of
// the default layout, and the locale keys in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const LOCALE_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const SERIES_KEYS = [
  'dash_lane_util', 'dash_throughput_forecast', 'dash_verifier_health',
  'dash_series_busy_share', 'dash_series_no_history', 'dash_series_none',
  'dash_series_date', 'dash_series_throughput', 'dash_series_range',
  'dash_series_node', 'dash_series_runs', 'dash_series_waits',
  'dash_series_avg_wait', 'dash_series_avg_run', 'dash_series_pass_rate',
  'dash_card_off_lane_util_reason', 'dash_card_off_forecast_reason',
  'dash_card_off_verifier_reason', 'dash_card_off_series_link',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
}

// loadAppSandbox loads the real app.js and gives the series cards' containers
// their own stub elements, so a render can be read back from innerHTML.
function loadAppSandbox() {
  const els = {
    dashLaneUtilCard: makeStubElement(),
    dashForecastCard: makeStubElement(),
    dashVerifierCard: makeStubElement(),
  };
  const sandbox = buildSandbox({ byId: els });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  sandbox.__els = els;
  return sandbox;
}

function setSeries(sb, slot, payload) {
  vm.runInContext('_dash.' + slot + ' = ' + JSON.stringify(payload) + ';', sb);
}

test('locale: every series key exists in all five bundles', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of SERIES_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' is missing ' + k);
    }
  }
});

test('card definitions: the three series cards are addable but not in the default layout', () => {
  const sb = loadAppSandbox();
  const defs = vm.runInContext('DASH_CARD_DEFS', sb).map(d => d.id);
  const layout = vm.runInContext('DASH_DEFAULT_LAYOUT', sb).map(d => d.id);
  for (const id of ['lane-utilization', 'throughput-forecast', 'verifier-health']) {
    assert.ok(defs.includes(id), id + ' should be in the card picker');
    assert.ok(!layout.includes(id), id + ' must not be in the default layout');
  }
});

test('feature gate: lane utilization is off with no busy hour and on once a lane was busy', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['lane-utilization'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesLaneUtil', { lanes: [{ lane: 'claude-code', hours: [{ start: '2026-11-10T11:00:00Z', busy_fraction: 0 }] }] });
  assert.equal(feat.active(), false, 'an idle lane is not shown');
  setSeries(sb, '_seriesLaneUtil', { lanes: [{ lane: 'claude-code', hours: [{ start: '2026-11-10T11:00:00Z', busy_fraction: null }, { start: '2026-11-10T12:00:00Z', busy_fraction: 0.4 }] }] });
  assert.equal(feat.active(), true, 'a busy hour shows the card');
});

test('feature gate: the forecast shows once a point has throughput or a completion range', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['throughput-forecast'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesForecast', { points: [{ date: '2026-11-10', throughput_3d: 0, completion_low_days: null, completion_high_days: null }] });
  assert.equal(feat.active(), false, 'an empty trend is not shown');
  setSeries(sb, '_seriesForecast', { points: [{ date: '2026-11-10', throughput_3d: 0, completion_low_days: 2, completion_high_days: 5 }] });
  assert.equal(feat.active(), true, 'a completion range shows the card');
});

test('feature gate: verifier health shows once a node ran or waited', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['verifier-health'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesVerifier', { nodes: [{ node: 'n1', runs: 0, waits: 0 }], outcomes: { runs: 0, pass_rate: null } });
  assert.equal(feat.active(), false, 'a node with no activity is not shown');
  setSeries(sb, '_seriesVerifier', { nodes: [{ node: 'n1', runs: 2, waits: 0 }], outcomes: { runs: 2, pass_rate: 0.5 } });
  assert.equal(feat.active(), true, 'a run shows the card');
});

test('reasons: each series gate names a locale key that resolves in English', () => {
  const sb = loadAppSandbox();
  const en = bundle('en');
  const feats = vm.runInContext('DASH_CARD_FEATURES', sb);
  for (const id of ['lane-utilization', 'throughput-forecast', 'verifier-health']) {
    assert.ok(en[feats[id].reasonKey], id + ' reason key ' + feats[id].reasonKey + ' missing in en');
    assert.ok(en[feats[id].linkKey], id + ' link key missing in en');
  }
});

test('render: an unrecorded hour is hatched, a recorded hour is tinted by its share', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesLaneUtil', { lanes: [{ lane: 'claude-code', hours: [
    { start: '2026-11-10T11:00:00Z', busy_fraction: null },
    { start: '2026-11-10T12:00:00Z', busy_fraction: 0.5 },
  ] }] });
  vm.runInContext('_dashRenderLaneUtil()', sb);
  const html = sb.__els.dashLaneUtilCard.innerHTML;
  assert.match(html, /repeating-linear-gradient/, 'the unrecorded hour is hatched');
  assert.match(html, /rgba\(124,58,237,0\.53\)/, 'a 50% hour is tinted, not zero');
  assert.ok(html.includes('claude-code'), 'the lane name is shown');
});

test('render: the forecast table keeps a null completion bound as a dash', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesForecast', { points: [
    { date: '2026-11-09', throughput_3d: 3, completion_low_days: 2, completion_high_days: 4.5 },
    { date: '2026-11-10', throughput_3d: 1, completion_low_days: null, completion_high_days: null },
  ] });
  vm.runInContext('_dashRenderForecast()', sb);
  const html = sb.__els.dashForecastCard.innerHTML;
  assert.ok(html.includes('2026-11-09') && html.includes('2.0 – 4.5'), 'the range is shown');
  assert.ok(html.includes('— – —'), 'a missing range shows dashes');
});

test('render: verifier health shows the all-nodes pass rate and escapes node names', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesVerifier', {
    nodes: [{ node: '<n1>', runs: 2, waits: 1, wait_avg_seconds: 30, run_avg_seconds: 120 }],
    outcomes: { runs: 4, pass_rate: 0.75 },
  });
  vm.runInContext('_dashRenderVerifier()', sb);
  const html = sb.__els.dashVerifierCard.innerHTML;
  assert.ok(html.includes('&lt;n1&gt;'), 'the node name is escaped');
  assert.ok(!html.includes('<n1>'), 'no raw node markup');
  assert.ok(html.includes('75%'), 'the pass rate is shown for all nodes');
  assert.ok(html.includes('30s') && html.includes('120s'), 'average wait and run are shown in seconds');
});

test('render: an empty payload shows the empty line, not a blank card', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesVerifier', { nodes: [], outcomes: { runs: 0, pass_rate: null } });
  vm.runInContext('_dashRenderVerifier()', sb);
  assert.ok(sb.__els.dashVerifierCard.innerHTML.includes(bundle('en').dash_series_none));
});
