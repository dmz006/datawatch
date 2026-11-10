// Dashboard 30-Day Activity card (id 'heatmap') — rebuilt from the daily
// progress trend (Automata completed, throughput), the shipped counts (shipped
// per merge day) and the release tags. Checks the day rows (30 UTC days, the
// empty post-restart trend point skipped, release tags on their day), that the
// card is hidden through its DASH_CARD_FEATURES active() check while no day has
// anything to show, the fetch wiring, and the new strings in all five locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const NOW = Date.UTC(2026, 10, 10, 12, 0, 0); // 2026-11-10 12:00 UTC

function load() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);

// rowsFor runs the pure day builder in the sandbox and returns plain rows.
function rowsFor(sandbox, points, byDay, tags) {
  sandbox.__points = points;
  sandbox.__byDay = byDay;
  sandbox.__tags = tags;
  sandbox.__now = NOW;
  return JSON.parse(JSON.stringify(run(sandbox,
    '_heatmapActivityDays(__points, __byDay, __tags, __now, 30)')));
}

test('one row per UTC day, 30 days, oldest first, ending on today', () => {
  const sb = load();
  const rows = rowsFor(sb, [], {}, []);
  assert.equal(rows.length, 30);
  assert.equal(rows[0].date, '2026-10-12');
  assert.equal(rows[29].date, '2026-11-10');
  for (let i = 1; i < rows.length; i++) {
    assert.ok(rows[i].date > rows[i - 1].date, 'dates ascend');
  }
});

test('a trend point with 0 completed and 0 throughput is skipped, not plotted', () => {
  const sb = load();
  const rows = rowsFor(sb, [
    { date: '2026-11-06', completed_prds: 0, throughput_3d: 0 }, // post-restart empty state
    { date: '2026-11-05', completed_prds: 315, throughput_3d: 1.5 },
  ], {}, []);
  const d6 = rows.find(r => r.date === '2026-11-06');
  const d5 = rows.find(r => r.date === '2026-11-05');
  assert.equal(d6.hasProgress, false, 'empty point not plotted');
  assert.equal(d6.completed, 0);
  assert.equal(d5.hasProgress, true);
  assert.equal(d5.completed, 315);
  assert.equal(d5.throughput, 1.5);
});

test('a point with only throughput still plots (not an empty state)', () => {
  const sb = load();
  const rows = rowsFor(sb, [{ date: '2026-11-04', completed_prds: 0, throughput_3d: 0.4 }], {}, []);
  assert.equal(rows.find(r => r.date === '2026-11-04').hasProgress, true);
});

test('shipped per day comes from the by_day counts, PRDs only', () => {
  const sb = load();
  const rows = rowsFor(sb, [], { '2026-11-08': { prds: 3, automations: 1 } }, []);
  assert.equal(rows.find(r => r.date === '2026-11-08').shipped, 3);
  assert.equal(rows.find(r => r.date === '2026-11-09').shipped, 0);
});

test('release tags are attached to their day, several per day allowed', () => {
  const sb = load();
  const rows = rowsFor(sb, [], {}, [
    { name: 'v9.54.0', date: '2026-11-02' },
    { name: 'v9.55.0', date: '2026-11-02' },
    { name: 'v9.60.0', date: '2026-11-06' },
  ]);
  assert.deepEqual(rows.find(r => r.date === '2026-11-02').tags, ['v9.54.0', 'v9.55.0']);
  assert.deepEqual(rows.find(r => r.date === '2026-11-06').tags, ['v9.60.0']);
  assert.deepEqual(rows.find(r => r.date === '2026-11-03').tags, []);
});

test('card is inactive until a day has progress, a shipped record or a release', () => {
  const sb = load();
  run(sb, '_dash._heatmapData = null;');
  assert.equal(run(sb, "_dashCardFeature('heatmap').active()"), false);
  run(sb, '_dash._heatmapData = _heatmapActivityDays([{date:"2026-11-06",completed_prds:0,throughput_3d:0}], {}, [], Date.UTC(2026,10,10), 30);');
  assert.equal(run(sb, "_dashCardFeature('heatmap').active()"), false, 'an empty trend point alone is not data');
  run(sb, '_dash._heatmapData = _heatmapActivityDays([{date:"2026-11-06",completed_prds:4,throughput_3d:1}], {}, [], Date.UTC(2026,10,10), 30);');
  assert.equal(run(sb, "_dashCardFeature('heatmap').active()"), true);
  run(sb, '_dash._heatmapData = _heatmapActivityDays([], {"2026-11-01":{prds:1}}, [], Date.UTC(2026,10,10), 30);');
  assert.equal(run(sb, "_dashCardFeature('heatmap').active()"), true, 'shipped alone counts');
  run(sb, '_dash._heatmapData = _heatmapActivityDays([], {}, [{name:"v9.60.0",date:"2026-11-06"}], Date.UTC(2026,10,10), 30);');
  assert.equal(run(sb, "_dashCardFeature('heatmap').active()"), true, 'a release alone counts');
});

test('fetch wiring reads trend, shipped counts and release tags; the old analytics read is gone', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /\/api\/autonomous\/progress\/trend\?days='\s*\+\s*HEATMAP_DAYS/);
  assert.match(src, /apiFetch\('\/api\/autonomous\/shipped\/counts'\)/);
  assert.match(src, /apiFetch\('\/api\/autonomous\/release-tags'\)/);
  assert.doesNotMatch(src, /\/api\/analytics\?range=30d/);
  assert.match(src, /_dashPumpFeatureData[\s\S]*kick\(_dashCardFeature\('heatmap'\), _dashFetchHeatmap\)/);
});

test('off-card docs link key is mapped', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /dash_card_heatmap: DOCS_DEFINITIONS \+ '#dashboard'/);
});

test('new strings exist in all five locales and never say PRD', () => {
  const keys = [
    'dash_card_off_heatmap_reason', 'dash_card_off_heatmap_link',
    'dash_heatmap_tip_completed', 'dash_heatmap_tip_shipped', 'dash_heatmap_tip_release',
    'dash_heatmap_tip_none', 'dash_heatmap_total',
  ];
  for (const l of LOCALES) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof j[k] === 'string' && j[k].length > 0, `${l} missing ${k}`);
      assert.doesNotMatch(j[k], /\bPRDs?\b/, `${l} ${k} must not say PRD`);
    }
  }
});

test('draw reads the day rows and the tooltip is wired without a tracker id', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const body = src.slice(src.indexOf('function _drawHeatmap()'), src.indexOf('// Guardrail verdicts bar chart'));
  assert.match(body, /_dash\._heatmapData/);
  assert.match(body, /canvas\.onmousemove/);
  assert.match(body, /t\('dash_heatmap_total'/);
  assert.doesNotMatch(src.slice(src.indexOf('// 30-day activity heatmap'), src.indexOf('// Guardrail verdicts bar chart')), /\bBL\d+\b/);
});
