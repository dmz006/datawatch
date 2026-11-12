// Lane utilization dashboard card — PWA side.
//
// window.laneUtilizationCardHtml(view) draws GET /api/autonomous/lane-utilization
// as a card: window picker, stacked busy-share graph, a per-lane table with an
// idle-while-eligible flag, the lane fill mode with its newest decisions, and
// plan usage in points and tokens. window.laneUtilizationLoad(win) fetches it
// and the same runs feed the Gantt lane rows.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const HOSTILE = '<img src=x onerror=alert(1)>';

function loadApp(fetchImpl) {
  const sandbox = buildSandbox();
  if (fetchImpl) sandbox.fetch = fetchImpl;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(view) {
  const sandbox = loadApp();
  sandbox._view = view;
  return vm.runInContext('window.laneUtilizationCardHtml(_view)', sandbox);
}

function bucket(share) {
  return { start: '2026-11-12T10:00:00Z', end: '2026-11-12T11:00:00Z', busy_share: share };
}

function fixture() {
  return {
    window_start: '2026-11-11T11:00:00Z', window_end: '2026-11-12T11:00:00Z', window_seconds: 86400, bucket_count: 3,
    lanes: [
      {
        id: 'gpu-1', type: 'gpu', busy_seconds: 43200, idle_seconds: 43200, busy_share: 0.5, idle_share: 0.5,
        runs: [
          { prd_id: 'p1', title: 'Cache column', stage: 'run', kind: 'task', backend: 'pi-prompt', start: '2026-11-12T08:00:00Z', end: '2026-11-12T09:00:00Z' },
          { prd_id: 'p2', title: 'Second', stage: 'run', kind: 'task', backend: 'pi-prompt', start: '2026-11-12T09:30:00Z', end: '2026-11-12T10:00:00Z' },
        ],
        queue_wait: { count: 2, mean_seconds: 125, max_seconds: 300 },
        idle_with_eligible_work: false, idle_with_eligible_seconds: 0, buckets: [bucket(0.2), bucket(0.5), bucket(0.8)],
      },
      {
        id: 'claude-1', type: 'claude', busy_seconds: 8640, idle_seconds: 77760, busy_share: 0.1, idle_share: 0.9,
        runs: [{ prd_id: 'p3', title: 'Doc', stage: 'run', kind: 'task', backend: 'claude-code', start: '2026-11-12T07:00:00Z', end: '2026-11-12T07:10:00Z' }],
        queue_wait: { count: 0, mean_seconds: 0, max_seconds: 0 },
        idle_with_eligible_work: true, idle_with_eligible_seconds: 3600, first_eligible_item: 'Write the docs', buckets: [bucket(0), bucket(0.1), bucket(0.2)],
      },
    ],
    backend_mix: [{ backend: 'pi-prompt', busy_seconds: 5400, share: 0.75 }, { backend: 'claude-code', busy_seconds: 600, share: 0.25 }],
    lane_fill: {
      mode: 'shadow', modes: {},
      decisions: [{ at: '2026-11-12T10:30:00Z', mode: 'shadow', lane_id: 'claude-1', lane_type: 'claude', action: 'would_start', prd_id: 'p4', title: 'Next job', reason: 'idle lane' }],
    },
    plan_usage: { plan_points: 100, points: 42.5, tokens: 123456, percent_of_plan: 42.5 },
  };
}

test('renders one table row per lane with busy, idle, runs, wait and backend mix', () => {
  const html = render({ win: '24h', data: fixture() });
  assert.equal(html.split('class="lane-util-row').length - 1, 2, html);
  assert.ok(html.includes('data-lane-id="gpu-1"') && html.includes('data-lane-id="claude-1"'));
  assert.ok(html.includes('50%') && html.includes('10%') && html.includes('90%'), 'busy and idle percentages missing');
  assert.ok(html.includes('2m 5s'), 'mean queue wait missing: ' + html);
  assert.ok(html.includes('pi-prompt ×2'), 'per-lane backend mix missing');
  assert.ok(html.includes('pi-prompt 75%'), 'overall backend mix missing');
});

test('draws the stacked graph, the window picker and the help link', () => {
  const html = render({ win: '6h', data: fixture() });
  assert.equal(html.split('<polygon').length - 1, 2, 'one stacked area per lane');
  assert.ok(html.includes('<option value="6h" selected>'), 'selected window missing');
  for (const w of ['1h', '6h', '24h', '7d']) assert.ok(html.includes(`value="${w}"`), 'window ' + w);
  assert.ok(html.includes('usage-help'), "'?' help button missing");
  assert.ok(html.includes('usage-help-docs'), 'docs link missing');
});

test('flags a lane that was idle while eligible work existed, and only that lane', () => {
  const html = render({ win: '24h', data: fixture() });
  assert.equal(html.split('class="lane-util-flag"').length - 1, 1, html);
  assert.ok(html.includes('Idle while eligible work existed'));
  assert.ok(html.includes('Write the docs'), 'first eligible item missing');
  assert.ok(/lane-util-row-flagged"[^>]*data-lane-id="claude-1"/.test(html), 'flagged row must be claude-1');
});

test('shows the lane fill mode with its newest decisions and plan usage in points and tokens', () => {
  const html = render({ win: '24h', data: fixture() });
  assert.ok(html.includes('Lane fill mode: '), 'fill heading missing');
  assert.ok(html.includes('claude-1 · would_start · Next job'), 'decision missing: ' + html);
  assert.ok(html.includes('42.5 of 100 points'), 'points missing');
  assert.ok(html.includes('123456 tokens'), 'tokens missing');
});

test('empty response renders the empty state, not a table', () => {
  const html = render({ win: '24h', data: { lanes: [], backend_mix: [] } });
  assert.ok(html.includes('lane-util-empty') && html.includes('No lanes were active'));
  assert.ok(!html.includes('<table'), 'no table for an empty response');
});

test('no response yet renders the loading state', () => {
  const html = render({ win: '24h', data: null });
  assert.ok(html.includes('lane-util-loading'));
});

test('error state shows the escaped message and no table', () => {
  const html = render({ win: '24h', data: null, error: HOSTILE });
  assert.ok(html.includes('lane-util-error') && html.includes('Could not load'));
  assert.ok(!html.includes(HOSTILE), 'error text must be escaped');
  assert.ok(html.includes('&lt;img'));
});

test('server strings are escaped', () => {
  const d = fixture();
  d.lanes[0].id = HOSTILE;
  d.lanes[1].first_eligible_item = HOSTILE;
  d.backend_mix[0].backend = HOSTILE;
  d.lane_fill.decisions[0].title = HOSTILE;
  const html = render({ win: '24h', data: d });
  assert.ok(!html.includes(HOSTILE), 'raw hostile markup leaked');
  assert.ok(html.includes('&lt;img'));
});

test('no dollar sign appears anywhere in the output', () => {
  for (const view of [{ win: '24h', data: fixture() }, { win: '24h', data: { lanes: [] } }, { win: '7d', data: null }, { win: '1h', error: 'boom' }]) {
    assert.ok(!render(view).includes('$'), 'a dollar sign leaked into the card');
  }
});

test('every visible string resolves: no lane_utilization key shows raw', () => {
  const html = render({ win: '24h', data: fixture() });
  assert.ok(!/lane_utilization_[a-z0-9_]+/.test(html), 'unresolved locale key in output');
});

test('loading fetches the window, and a window change refetches with the new window', async () => {
  const urls = [];
  const sandbox = loadApp(url => {
    urls.push(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(fixture()) });
  });
  await vm.runInContext('window.laneUtilizationLoad("24h")', sandbox);
  await vm.runInContext('window.laneUtilizationSetWindow("7d")', sandbox);
  assert.equal(urls.length, 2);
  assert.ok(urls[0].includes('/api/autonomous/lane-utilization') && urls[0].includes('window=24h'), urls[0]);
  assert.ok(urls[1].includes('window=7d'), urls[1]);
  assert.equal(vm.runInContext('window._laneUtilState.win', sandbox), '7d');
  assert.equal(vm.runInContext('window._laneUtilState.data.lanes.length', sandbox), 2);
});

test('a failed fetch leaves the error state and no data', async () => {
  const sandbox = loadApp(() => Promise.resolve({ ok: false, status: 500, statusText: 'boom', text: () => Promise.resolve('server exploded') }));
  await vm.runInContext('window.laneUtilizationLoad("24h")', sandbox);
  assert.equal(vm.runInContext('window._laneUtilState.data', sandbox), null);
  assert.ok(vm.runInContext('window._laneUtilState.error', sandbox).includes('server exploded'));
  const html = vm.runInContext('window.laneUtilizationCardHtml(window._laneUtilState)', sandbox);
  assert.ok(html.includes('lane-util-error'));
});

test('the card is registered on the dashboard and the Gantt gets lane rows from the same runs', () => {
  const sandbox = loadApp();
  const out = vm.runInContext(`(() => {
    const def = DASH_CARD_DEFS.find(d => d.id === 'lane-utilization-detail');
    window._laneUtilState.data = ${JSON.stringify(fixture())};
    const lanes = _laneUtilGanttLanes(Date.parse('2026-11-12T06:00:00Z'), Date.parse('2026-11-12T11:00:00Z'));
    return JSON.stringify({ def: !!def, body: def && def.body(), lanes: lanes.map(l => [l.id, l.runs.length]) });
  })()`, sandbox);
  const r = JSON.parse(out);
  assert.ok(r.def, 'card definition missing');
  assert.ok(r.body.includes('dashLaneUtilDetailCard'));
  assert.deepEqual(r.lanes, [['gpu-1', 2], ['claude-1', 1]]);
});
