// Dashboard lane pulse card — the Multi-EKG card rebuilt as one trace per lane
// and node (busy, waiting, idle) plus a usage burn panel in points against the
// pace line. Covers the pure geometry and burn helpers, the feature gate that
// hides the card until it has data, the absence of any dollar figure on the
// card, and the locale keys it renders in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const NEW_KEYS = [
  'dash_lane_pulse', 'dash_lane_pulse_busy', 'dash_lane_pulse_waiting',
  'dash_lane_pulse_none', 'dash_lane_pulse_usage', 'dash_lane_pulse_points',
  'dash_lane_pulse_pace_line', 'dash_lane_pulse_ahead', 'dash_lane_pulse_on_pace',
  'dash_lane_pulse_stale', 'dash_card_off_ekg_reason', 'dash_card_off_ekg_link',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
}

function loadAppSandbox() {
  const en = bundle('en');
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

const FROM = '2026-11-10T00:00:00Z';
const TO = '2026-11-10T06:00:00Z';
// 06:00 window: one hour at offset 0 is 0%..16.666%.
const HOUR = 3600000;

function series(lane, node, segments, extra) {
  return Object.assign({
    lane, node,
    busy_seconds: 0, waiting_seconds: 0, idle_seconds: 0,
    segments,
  }, extra || {});
}

test('rows: busy and waiting become percent bars, idle is not drawn', () => {
  const sb = loadAppSandbox();
  const rows = vm.runInContext('_lanePulseRows', sb)([
    series('claude-code', 'n1', [
      { state: 'idle', start: FROM, end: '2026-11-10T01:00:00Z' },
      { state: 'busy', start: '2026-11-10T01:00:00Z', end: '2026-11-10T02:00:00Z' },
      { state: 'waiting', start: '2026-11-10T03:00:00Z', end: '2026-11-10T03:30:00Z' },
    ]),
  ], Date.parse(FROM), Date.parse(TO));
  assert.equal(rows.length, 1);
  assert.equal(rows[0].label, 'claude-code · n1');
  assert.equal(rows[0].bars.length, 2, 'idle segment is not a bar');
  const busy = rows[0].bars[0];
  assert.equal(busy.state, 'busy');
  assert.ok(Math.abs(busy.left - 100 / 6) < 0.001, 'busy starts at one hour in');
  assert.ok(Math.abs(busy.width - 100 / 6) < 0.001, 'busy spans one hour');
  const waiting = rows[0].bars[1];
  assert.equal(waiting.state, 'waiting');
  assert.ok(Math.abs(waiting.left - 50) < 0.001, 'waiting starts at three hours in');
  assert.ok(Math.abs(waiting.width - 100 / 12) < 0.001, 'waiting spans thirty minutes');
});

test('rows: one row per lane and node, capped at twelve', () => {
  const sb = loadAppSandbox();
  const many = [];
  for (let i = 0; i < 20; i++) many.push(series('lane' + i, 'n', []));
  const rows = vm.runInContext('_lanePulseRows', sb)(many, Date.parse(FROM), Date.parse(TO));
  assert.equal(rows.length, 12);
  assert.equal(rows[0].label, 'lane0 · n');
});

test('rows: empty or zero-length window draws nothing', () => {
  const sb = loadAppSandbox();
  const f = vm.runInContext('_lanePulseRows', sb);
  assert.deepEqual(Array.from(f(undefined, 0, HOUR)), []);
  assert.deepEqual(Array.from(f([series('a', 'n', [])], 5, 5)), []);
  assert.deepEqual(Array.from(f([series('a', 'n', [])], NaN, NaN)), []);
});

test('rows: a segment that ends before it starts is dropped', () => {
  const sb = loadAppSandbox();
  const rows = vm.runInContext('_lanePulseRows', sb)([
    series('a', 'n', [{ state: 'busy', start: '2026-11-10T02:00:00Z', end: '2026-11-10T01:00:00Z' }]),
  ], Date.parse(FROM), Date.parse(TO));
  assert.equal(rows[0].bars.length, 0);
});

test('burn lines: two or more readings give level and pace polylines on one scale', () => {
  const sb = loadAppSandbox();
  const lines = vm.runInContext('_lanePulseBurnLines', sb)([
    { at: FROM, points: 10, pace_line: 20 },
    { at: TO, points: 40, pace_line: 40 },
  ]);
  assert.ok(lines.points.startsWith('0.00,'));
  assert.ok(lines.points.includes(' 100.00,'));
  assert.ok(lines.pace.includes(' 100.00,'));
  assert.equal(lines.points.split(' ').length, 2);
});

test('burn lines: fewer than two readings draw no line', () => {
  const sb = loadAppSandbox();
  const f = vm.runInContext('_lanePulseBurnLines', sb);
  assert.deepEqual({ ...f([{ points: 1, pace_line: 1 }]) }, { points: '', pace: '' });
  assert.deepEqual({ ...f(null) }, { points: '', pace: '' });
});

test('feature gate: the card is off with no lane time and no burn level', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb).ekg;
  assert.equal(feat.active(), false, 'no payload yet');
  vm.runInContext('_dash._lanePulse = ' + JSON.stringify({
    series: [series('a', 'n', [], { busy_seconds: 0, waiting_seconds: 0, idle_seconds: 600 })],
    burn: { available: false, reason: 'not_wired' },
  }) + ';', sb);
  assert.equal(feat.active(), false, 'only idle time and no usage reading');
});

test('feature gate: the card shows once a lane was busy or waiting, or usage reads', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb).ekg;
  vm.runInContext('_dash._lanePulse = ' + JSON.stringify({
    series: [series('a', 'n', [], { busy_seconds: 90, waiting_seconds: 0, idle_seconds: 0 })],
    burn: { available: false, reason: 'not_wired' },
  }) + ';', sb);
  assert.equal(feat.active(), true, 'busy time shows the card');
  vm.runInContext('_dash._lanePulse = ' + JSON.stringify({
    series: [],
    burn: { available: true, points: 12, pace_line: 10, readings: [] },
  }) + ';', sb);
  assert.equal(feat.active(), true, 'a usage level shows the card');
});

test('feature gate: the off reason and link come from locale keys with a card reference', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb).ekg;
  assert.equal(feat.reasonKey, 'dash_card_off_ekg_reason');
  assert.equal(feat.linkKey, 'dash_card_off_ekg_link');
  const body = vm.runInContext('_dashCardOffBody', sb)(feat);
  assert.ok(body.includes(bundle('en').dash_card_off_ekg_reason.split(' ')[0]));
});

test('burn panel: unavailable shows the locale reason text, never a number', () => {
  const sb = loadAppSandbox();
  const html = vm.runInContext('_lanePulseBurnHtml', sb)({ available: false, reason: 'advice_unavailable' });
  assert.ok(html.includes(bundle('en').lane_pulse_reason_advice_unavailable));
  const text = html.replace(/style="[^"]*"/g, '');
  assert.ok(!/\d/.test(text), 'no numbers in the unavailable state');
});

test('burn panel: available shows points against the pace line, with no dollar sign', () => {
  const sb = loadAppSandbox();
  const html = vm.runInContext('_lanePulseBurnHtml', sb)({
    available: true, points: 42.5, pace_line: 38.0, ahead_of_pace: true, stale: true,
    readings: [{ points: 10, pace_line: 5 }, { points: 42.5, pace_line: 38 }],
  });
  assert.ok(html.includes('42.5'), 'points shown');
  assert.ok(html.includes('38.0'), 'pace line shown');
  assert.ok(html.includes(bundle('en').dash_lane_pulse_ahead));
  assert.ok(html.includes(bundle('en').dash_lane_pulse_stale));
  assert.ok(html.includes('<polyline'), 'pace and level drawn');
  assert.ok(!html.includes('$'), 'no dollar figure on the burn panel');
});

test('card body: lane traces and burn panel, no canvas and no dollar figure', () => {
  const sb = loadAppSandbox();
  const def = vm.runInContext('DASH_CARD_DEFS', sb).find(d => d.id === 'ekg');
  const body = def.body();
  assert.ok(body.includes('id="dashLanePulseTraces"'));
  assert.ok(body.includes('id="dashBurnRate"'));
  assert.ok(!body.includes('ekgCanvas'), 'the canvas is gone');
  assert.ok(!body.includes('$'), 'no dollar figure in the card markup');
  assert.equal(def.labelKey, 'dash_lane_pulse');
});

test('render: the traces and burn panel are written with no dollar figure', () => {
  const sb = loadAppSandbox();
  const writes = {};
  const stub = (id) => ({ set innerHTML(v) { writes[id] = v; }, get innerHTML() { return writes[id] || ''; } });
  const original = sb.document.getElementById;
  sb.document.getElementById = (id) => (id === 'dashLanePulseTraces' || id === 'dashBurnRate' ? stub(id) : original(id));
  vm.runInContext('_dash._costToday = { total_usd: 383 };', sb);
  vm.runInContext('_dash._lanePulse = ' + JSON.stringify({
    window_from: FROM, window_to: TO,
    series: [series('claude-code', 'n1', [{ state: 'busy', start: FROM, end: TO }], { busy_seconds: 21600 })],
    burn: { available: true, points: 7, pace_line: 9, ahead_of_pace: false, readings: [] },
  }) + ';', sb);
  vm.runInContext('_dashRenderLanePulse()', sb);
  assert.ok(writes.dashLanePulseTraces.includes('claude-code · n1'));
  assert.ok(writes.dashLanePulseTraces.includes(bundle('en').dash_lane_pulse_busy));
  assert.ok(writes.dashBurnRate.includes('7.0'));
  assert.ok(!writes.dashLanePulseTraces.includes('$'));
  assert.ok(!writes.dashBurnRate.includes('$'), 'the cost total is not drawn on this card');
});

test('locales: every lane pulse key is in all five bundles, non-empty', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of NEW_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, lang + ' ' + k);
    }
  }
});

test('locales: the lane pulse strings name no tracker id and no PRD', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of NEW_KEYS) {
      assert.ok(!/\b(BL|B|GH|SEC|HLLM)\d+\b/.test(b[k]), lang + ' ' + k + ' has a tracker id');
      if (lang === 'en') assert.ok(!/\bPRD\b/.test(b[k]), k + ' says PRD');
    }
  }
});
