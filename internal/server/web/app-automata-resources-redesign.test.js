// The Resources lanes card is one row per lane with a small bounded bar, a clear
// gap under its header and no decisions log; the Progress headline cards show a
// readable number with a labelled unit and a trend; every card has a '?' help
// entry; a card that is off, empty or never measured is not drawn.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}
const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
const status = {
  mode: 'on',
  lanes: [
    { id: 'gpu-1', type: 'gpu', state: 'occupied', occupant: { title: 'Build it', backend: 'pi-prompt', node: 'node-a' }, eligible_waiting: 0 },
    { id: 'claude-1', type: 'claude', state: 'waiting_verifier', wait_reason: 'slot busy', occupant: { title: 'Fix it', backend: 'claude-code' } },
    { id: 'claude-2', type: 'claude', state: 'idle', eligible_waiting: 2 },
  ],
  queued_eligible: { gpu: 0, claude: 2 }, evidence: {}, alerts: [],
  decisions: [{ at: '2026-01-01T00:00:00Z', lane_id: 'gpu-1', action: 'start' }],
};

test('each lane is one row with name, node, state, what it runs and a bounded bar', () => {
  const html = load().laneFillCardHtml(status, null);
  assert.equal((html.match(/class="lane-fill-lane lane-row"/g) || []).length, 3);
  assert.equal((html.match(/class="lane-bar"/g) || []).length, 3);
  assert.match(html, /lane-row-node">lane_fill_node</);
  assert.match(html, /lane-row-node">lane_fill_node_none</);
  assert.match(html, /Build it · pi-prompt/);
  assert.match(html, /width:100%/);
  assert.match(html, /width:50%/);
  assert.match(html, /width:0%/);
});

test('the recent decisions log is not part of the lanes card', () => {
  const html = load().laneFillCardHtml(status, null);
  assert.doesNotMatch(html, /lane_fill_decisions_heading/);
  assert.doesNotMatch(html, /lane-fill-decision/);
});

test('the header has a gap class and the lanes card has a help entry with a docs link', () => {
  const html = load().laneFillCardHtml(status, null);
  assert.match(html, /cap-title card-head/);
  assert.match(html, /class="usage-help"/);
  assert.match(html, /usage-help-docs" href="\/diagrams\.html#docs\/howto\/automata-resources\.md"/);
  assert.match(css, /\.card-head\s*\{[^}]*margin-bottom:\s*\d+px/);
});

test('size classes are bounded in the stylesheet', () => {
  assert.match(css, /\.lane-card\s*\{[^}]*max-width:\s*\d+px/);
  assert.match(css, /\.lane-bar\s*\{[^}]*max-width:\s*\d+px/);
  assert.match(css, /\.metric-card\s*\{[^}]*width:\s*\d+px[^}]*max-width:\s*100%/);
  assert.match(css, /\.metric-card\s*\{[^}]*height:\s*\d+px/);
});

test('metric cards show the number, a labelled unit, the trend and a help entry', () => {
  const sb = load();
  const specs = sb.metricCardSpecs(
    { totals: { tasks: 40, tasks_done: 10 }, completion: { low_days: 2, high_days: 5.55 } },
    { direction: 'converging' }, { total_usd: 12.5 }, 10);
  const [progress, cost, forecast] = specs.map(s => sb.metricCardHtml(s));
  assert.match(progress, /10 \/ 40/);
  assert.match(progress, /metric_progress_unit/);
  assert.match(progress, /metric_trend_down/);
  assert.match(cost, /\$12\.50/);
  assert.match(cost, /metric_cost_unit/);
  assert.match(cost, /metric_trend_up/);
  assert.match(forecast, /metric_forecast_unit/);
  assert.match(forecast, /2\u20135\.5/);
  [progress, cost, forecast].forEach(h => {
    assert.match(h, /class="usage-help"/);
    assert.match(h, /usage-help-docs/);
  });
});

test('a card that is off, empty or never measured is not drawn', () => {
  const sb = load();
  assert.equal(sb.laneFillCardHtml(null, null), '');
  assert.equal(sb.laneFillCardHtml({ mode: 'on', lanes: [] }, null), '');
  assert.equal(sb.laneFillCardHtml({ mode: 'off', lanes: status.lanes }, null), '');
  sb.metricCardSpecs(null, null, null).forEach(s => assert.equal(sb.metricCardHtml(s), ''));
  sb.metricCardSpecs({ totals: { tasks: 0, tasks_done: 0 } }, null, { total_usd: 0 }).forEach(s => assert.equal(sb.metricCardHtml(s), ''));
});

test('every new key exists in all five locale bundles', () => {
  const keys = ['lane_fill_node', 'lane_fill_node_none', 'lane_fill_help_text', 'metric_trend_up', 'metric_trend_down', 'metric_trend_flat',
    'metric_progress_title', 'metric_progress_unit', 'metric_progress_help', 'metric_cost_title', 'metric_cost_unit', 'metric_cost_help',
    'metric_forecast_title', 'metric_forecast_unit', 'metric_forecast_help'];
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    keys.forEach(k => assert.ok(j[k], `${l} missing ${k}`));
  }
});

test('units are labelled in plain words', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.match(en.metric_cost_unit, /API-equivalent dollars/);
  assert.match(en.metric_progress_unit, /tasks/);
  assert.match(en.metric_forecast_unit, /days/);
});
