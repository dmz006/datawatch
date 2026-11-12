// Usage calculator pace-gauge variant — PWA side.
//
// window.usageCalcRenderers['pace-gauge'](body, opts) draws the variant body of
// the worked example: one bar from 0 to the ceiling, the pace tick and the two
// projection ticks, the over chip and the over-the-edge label, the reading fields
// in the shared order, the chosen-lanes line and the series chart. The renderer
// only reads fields of the response; these tests check the drawing of them.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody } = require('./usage-calculator-fixture');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(sandbox, body) {
  sandbox._body = body;
  return vm.runInContext("window.usageCalcRenderers['pace-gauge'](_body, {})", sandbox);
}

test('the pace-gauge renderer is registered under its variant id', () => {
  const s = loadApp();
  assert.equal(vm.runInContext("typeof window.usageCalcRenderers['pace-gauge']", s), 'function');
});

test('the worked example places the pace, recent and average ticks on a 0 to 100 bar', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  // width 320, ceiling 100: pace 79.84 -> 255.49; recent 105.98 is past the
  // ceiling so it is pinned to the right edge at 320.00; average 90.18 -> 288.58.
  assert.ok(html.includes('data-tick="pace" x1="255.49" x2="255.49"'), 'pace tick position');
  assert.ok(html.includes('data-tick="recent" x1="320.00" x2="320.00"'), 'recent tick pinned to the edge');
  assert.ok(html.includes('data-tick="average" x1="288.58" x2="288.58"'), 'average tick position');
  assert.ok(html.includes('data-fill="percent" x="0" y="18" width="230.40"'), 'weekly percent fill at 72 of 100');
});

test('the solid pace tick is solid, the recent tick dashed and the average tick dotted', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  const paceTag = html.match(/<line data-tick="pace"[^>]*\/>/);
  assert.ok(paceTag, 'pace tick missing');
  assert.ok(!paceTag[0].includes('stroke-dasharray'), 'the pace tick is solid');
  assert.match(html, /data-tick="recent"[^>]*stroke-dasharray:4 3/);
  assert.match(html, /data-tick="average"[^>]*stroke-dasharray:1 3/);
});

test('the over chip shows for the recent burn and the week-average chip is within', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(html.includes('usage-calc-chip over'), 'over chip missing');
  assert.ok(html.includes('Over budget'));
  assert.ok(html.includes('usage-calc-chip within'), 'week-average within chip missing');
  assert.ok(html.includes('Within budget'));
});

test('a projection past the ceiling carries the red zone and the over-the-edge label', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(html.includes('data-zone="over"'), 'red zone at the right edge missing');
  assert.ok(html.includes('data-label="over-edge"'), 'over-the-edge label missing');
  assert.ok(html.includes('Recent burn: over, past the edge (106.0%)'), 'over-the-edge text wrong');
  assert.ok(!html.includes('Week average: over, past the edge'), 'the within projection gets no label');
});

test('no over-the-edge label shows when both projections are within the ceiling', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody({
    advice: { verdict: 'within', projected_end_recent: 90.5, projected_end_avg: 80.1 },
  }));
  assert.ok(!html.includes('data-label="over-edge"'), 'label must not show within the ceiling');
  assert.ok(html.includes('usage-calc-chip within'));
  assert.ok(!html.includes('usage-calc-chip over'));
});

test('the reading fields come in the shared order', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  const labels = [...html.matchAll(/data-field="(\d+)"[^>]*><span[^>]*>([^<]*)<\/span>/g)].map(m => m[2]);
  assert.deepEqual(labels, [
    'Read',
    'Days since reset',
    'Pace line',
    'Burn per 24 hours',
    'Projected end at reset',
    'Runs out',
    'Verdict',
    'Lanes and policy',
    'Advice',
  ]);
});

test('the field values read the response: percent, days, pace and headroom, burns and projections', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(html.includes('72% · '), 'percent and read time');
  assert.ok(html.includes('22:08'), 'read time in the reset zone clock');
  assert.ok(html.includes('5.59 days'), 'days since reset');
  assert.ok(html.includes('79.8% · 7.8 points of headroom'), 'pace line with headroom');
  assert.ok(html.includes('Recent burn 24.1 · Week average 12.9'), 'both burns');
  assert.ok(html.includes('Recent burn 106.0% · Week average 90.2%'), 'both projected ends');
  assert.ok(html.includes('6.0 hours early'), 'hours early at the recent burn');
  assert.ok(html.includes('none before the reset'), 'no run out at the week average');
  assert.ok(html.includes('3 lanes, Pace to the week'), 'lanes and policy');
});

test('the headroom is replaced by "ahead of the pace line" when the reading is ahead', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody({ advice: { ahead_of_pace: true, headroom: -4.1 } }));
  assert.ok(html.includes('ahead of the pace line'), 'ahead-of-pace wording missing');
  assert.ok(!html.includes('points of headroom'), 'headroom must not show when ahead');
});

test('the chosen-lanes line shows the row for the lanes input', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(html.includes('Chosen: 3 lanes, 24.1 per 24 hours, ends at 106.0%'), 'chosen-lanes line missing');
});

test('zero chosen lanes is labelled as never advised', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody({
    advice: { lanes: 0, chosen: { lanes: 0, burn_per_24h: 3, projected_end: 76.23, run_out_at: null, hours_early: 0 } },
  }));
  assert.ok(html.includes('never advised.'), 'zero-lane wording missing');
});

test('the series chart plots every reading and draws the dashed projections to the reset', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  const m = html.match(/data-series="readings" points="([^"]*)"/);
  assert.ok(m, 'readings polyline missing');
  assert.equal(m[1].split(' ').length, 17, 'one point per reading since the reset');
  assert.ok(html.includes('data-series="recent"'), 'recent projection missing');
  assert.ok(html.includes('data-series="average"'), 'average projection missing');
});

test('the series axis runs between the two reset times of the response, never from a day total', () => {
  const s = loadApp();
  const pointsOf = html => html.match(/data-series="readings" points="([^"]*)"/)[1].split(' ').map(p => Number(p.split(',')[0]));
  const plain = pointsOf(render(s, workedExampleBody()));
  // 134.13 of the 168 hours between the resets, on a 320 wide chart.
  assert.ok(Math.abs(plain[plain.length - 1] - 255.5) < 0.2, 'newest reading x was ' + plain[plain.length - 1]);
  // The day figures are not read for the axis: nonsense values change nothing.
  const skewed = pointsOf(render(s, workedExampleBody({ advice: { days_since_reset: 999, days_left: 0 } })));
  assert.deepEqual(skewed, plain);
});

test('each projection past the ceiling gets its own label read from its own field', () => {
  const s = loadApp();
  const both = render(s, workedExampleBody({ advice: { projected_end_recent: 131.4, projected_end_avg: 118.2 } }));
  assert.ok(both.includes('Recent burn: over, past the edge (131.4%)'));
  assert.ok(both.includes('Week average: over, past the edge (118.2%)'));
  const avgOnly = render(s, workedExampleBody({ advice: { projected_end_recent: 90.5, projected_end_avg: 118.2 } }));
  assert.ok(!avgOnly.includes('Recent burn: over, past the edge'));
  assert.ok(avgOnly.includes('Week average: over, past the edge (118.2%)'));
});

test('the renderer shows no NaN or undefined in the drawn text', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(!html.includes('NaN'), 'NaN in the output');
  assert.ok(!html.includes('undefined'), 'undefined in the output');
});
