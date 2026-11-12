// Cost report chart cards on the Automata page: cost over time, tool usage,
// retries and failure categories, and the combined report.
//
// Loads the real app.js into the browser stub sandbox and asserts each card,
// its purpose / state / reason / next action lines, its "?" help link to the
// cost report howto, the bounded drawing, the money axes that name their unit
// from the billing mode and never mix units, the single placement constant,
// the rule that an empty or off card is not drawn and its reason becomes an
// Events entry, and that every cost_charts_* key exists in all five locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_SRC = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const readLocale = l => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
const BUNDLES = Object.fromEntries(LANGS.map(l => [l, readLocale(l)]));
const HELP_HREF = '/diagrams.html#docs/howto/cost-report.md';

function jsonRes(body, status) {
  const ok = !status || status < 400;
  return { ok, status: status || 200, statusText: ok ? '' : 'error', json: () => Promise.resolve(body), text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)) };
}

// load builds a sandbox with the English bundle. routes maps a path prefix to
// a response (body or { status, body }); every requested URL is recorded.
function load(routes) {
  const slot = makeStubElement(h => { slot.html = h; });
  slot.style = { display: 'none' };
  const sandbox = buildSandbox({ byId: { automationCostChartsCard: slot } });
  sandbox.URLSearchParams = URLSearchParams;
  const urls = [];
  sandbox.fetch = url => {
    urls.push(String(url));
    const key = Object.keys(routes || {}).find(k => String(url).indexOf(k) >= 0);
    if (!key) return Promise.resolve(jsonRes({}, 404));
    const r = routes[key];
    if (r && typeof r === 'object' && r.status) return Promise.resolve(jsonRes(r.body, r.status));
    return Promise.resolve(jsonRes(r));
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = BUNDLES.en;
  return { sb: sandbox, slot, urls };
}
const run = (sb, expr) => vm.runInContext(expr, sb);

// ── fixtures ────────────────────────────────────────────────────────────────
function days(n) {
  const out = [];
  for (let i = 0; i < n; i++) out.push(new Date(Date.UTC(2026, 10, 1 + i)).toISOString());
  return out;
}
function point(start, tokens, units) {
  return { start, sessions: 1, tokens_in: tokens, tokens_out: 0, tokens, units };
}
function seriesGroup(key, buckets, tokenAt, unitsAt) {
  const points = buckets.map((b, i) => point(b, tokenAt(i), unitsAt ? unitsAt(i) : undefined));
  const tokens = points.reduce((a, p) => a + p.tokens, 0);
  const seen = {};
  points.forEach(p => (p.units || []).forEach(u => { seen[u.unit] = u; }));
  return { key, sessions: points.length, tokens_in: tokens, tokens_out: 0, tokens, unit_totals: Object.values(seen), points };
}
function series(groups, buckets, totalsByUnit, extra) {
  return Object.assign({ group_by: 'project', bucket: 'day', window: '7d', buckets, groups, totals_by_unit: totalsByUnit, retention_days: 31 }, extra || {});
}
const DOLLARS = { unit: 'dollars', billing_mode: 'api_metered', sessions: 3, tokens_in: 1, tokens_out: 1, active_seconds: 1, dollars: 12.5 };
const POINTS = { unit: 'points', billing_mode: 'bundled_plan', sessions: 3, tokens_in: 1, tokens_out: 1, active_seconds: 1, points: 40, percent: 2 };

function toolGroup(key, buckets, calls) {
  const points = buckets.map((b, i) => ({ start: b, sessions: 1, tool_calls: calls(i) }));
  return { key, sessions: points.length, tool_calls: points.reduce((a, p) => a + p.tool_calls, 0), points };
}
function toolUsage(groups, buckets) {
  return { group_by: 'project', bucket: 'day', window: '7d', buckets, groups,
    total_tool_calls: groups.reduce((a, g) => a + g.tool_calls, 0), total_sessions: groups.reduce((a, g) => a + g.sessions, 0),
    tool_names_note: 'Only counts are recorded, never tool names.', retention_days: 31 };
}
function failureGroup(key, weeks, label) {
  const wk = weeks.map((w, i) => ({ start: w, attempts: i === 0 ? 3 : 1, categories: [{ category: 'infra', attempts: i === 0 ? 2 : 1 }, { category: 'content', attempts: i === 0 ? 1 : 0 }].filter(c => c.attempts) }));
  return { key, label: label || '', attempts: 4, tasks: 3, retries: 2, categories: [{ category: 'infra', attempts: 3 }, { category: 'content', attempts: 1 }], weeks: wk,
    top_summaries: [{ category: 'infra', summary: 'timeout', attempts: 3 }] };
}
function failures(groups, weeks) {
  return { group_by: 'project', window: '12w', weeks, groups, categories: [{ category: 'infra', attempts: 3 * groups.length }, { category: 'content', attempts: groups.length }],
    total_attempts: 4 * groups.length, total_tasks: 3 * groups.length, total_retries: 2 * groups.length, undated_attempts: 0,
    retry_note: 'Retries count each task once.' };
}
const WEEKS = ['2026-10-26T00:00:00Z', '2026-11-02T00:00:00Z'];

function fullData(opts) {
  opts = opts || {};
  const b = days(7);
  return {
    cost: { body: series([seriesGroup('alpha', b, i => 100 + i, i => [{ unit: 'dollars', billing_mode: 'api_metered', dollars: i + 1 }]),
      seriesGroup('beta', b, i => 50 + i, i => [{ unit: 'points', billing_mode: 'bundled_plan', points: i + 2 }])].concat(opts.moreGroups || []), b,
    opts.noMoney ? [] : [DOLLARS, POINTS]) },
    tools: { body: toolUsage([toolGroup('alpha', b, i => i + 1), toolGroup('beta', b, i => 2)], b) },
    failures: { body: failures([failureGroup('alpha', WEEKS), failureGroup('beta', WEEKS, 'Beta title')], WEEKS) },
  };
}

const render = (sb, data, opts) => sb.window.costChartsCardsHtml(data, Object.assign({ group: 'project', window: '7d', metric: 'tokens' }, opts || {}));
const has = (html, id) => html.indexOf('id="' + id + '"') >= 0;
const cardHtml = (html, id) => {
  const i = html.indexOf('id="' + id + '"');
  assert.ok(i >= 0, id + ' is drawn');
  const next = html.indexOf('<div id="costCharts', i + 10);
  return html.slice(i, next < 0 ? html.length : next);
};
const count = (s, re) => (s.match(re) || []).length;

// ── the four cards ──────────────────────────────────────────────────────────
test('all four cards are drawn, each with purpose, state, reason, next action and a help link to the cost report howto', () => {
  const { sb } = load();
  const html = render(sb, fullData());
  const titles = { costChartsCostCard: 'Cost over time', costChartsToolsCard: 'Tool usage', costChartsFailuresCard: 'Retries and failure categories', costChartsReportCard: 'Combined usage report' };
  for (const [id, title] of Object.entries(titles)) {
    const c = cardHtml(html, id);
    assert.ok(c.includes(title), id + ' title');
    assert.match(c, /class="cost-charts-purpose"[^>]*>[^<]+</, id + ' purpose line');
    assert.match(c, /<strong>State:<\/strong> Showing/, id + ' state');
    assert.match(c, /<strong>Reason:<\/strong> [^<]+/, id + ' reason');
    assert.match(c, /<strong>Next action:<\/strong> [^<]+/, id + ' next action');
    assert.ok(c.includes('href="' + HELP_HREF + '"'), id + ' help link');
    assert.match(c, /class="docs-link cost-charts-help"[^>]*>\?<\/a>/, id + ' "?" entry');
  }
  assert.equal(count(html, /class="docs-link cost-charts-help"/g), 5, 'four cards plus the heading carry the help entry');
  assert.equal(count(html, /class="cost-charts-card /g), 4);
});

test('cost over time and tool usage draw a line per group; failures draw stacked bars; the report is a joined table', () => {
  const { sb } = load();
  const html = render(sb, fullData());
  const cost = cardHtml(html, 'costChartsCostCard');
  assert.equal(count(cost, /<polyline/g), 2, 'one line per group');
  assert.ok(cost.includes('>tokens<'), 'tokens axis caption');
  const tools = cardHtml(html, 'costChartsToolsCard');
  assert.equal(count(tools, /<polyline/g), 2);
  assert.ok(tools.includes('>tool calls<'));
  assert.ok(tools.includes('Only counts are recorded, never tool names.'), 'the daemon note is shown');
  const fail = cardHtml(html, 'costChartsFailuresCard');
  assert.ok(count(fail, /<rect/g) >= 2, 'bar segments');
  assert.ok(fail.includes('Beta title'), 'the readable title is the row label');
  assert.ok(fail.includes('Retries count each task once.'));
  const rep = cardHtml(html, 'costChartsReportCard');
  for (const h of ['Group', 'Tokens', 'Money', 'Tool calls', 'Failed attempts', 'Retries']) assert.ok(rep.includes('>' + h + '<'), 'column ' + h);
  assert.ok(rep.includes('alpha') && rep.includes('Beta title'), 'rows by group');
  assert.ok(rep.includes('<tfoot>'), 'totals row');
});

test('groupings are Project, Automation and Automaton, and the controls name them', () => {
  const { sb } = load();
  assert.deepEqual(Array.from(run(sb, 'COST_CHARTS_GROUPS')), ['project', 'automation', 'automaton']);
  const html = render(sb, fullData());
  for (const label of ['Project', 'Automation', 'Automaton']) assert.ok(html.includes('>' + label + '</option>'), label);
  assert.ok(html.includes('id="costChartsGroup"') && html.includes('id="costChartsWindow"') && html.includes('id="costChartsMetric"'));
});

// ── bounded size ────────────────────────────────────────────────────────────
test('drawing is bounded: at most six lines, sixty points per line and eight table rows', () => {
  const { sb } = load();
  const b = days(200);
  const many = [];
  for (let i = 0; i < 10; i++) many.push(seriesGroup('g' + i, b, () => 10 + i));
  const data = fullData();
  data.cost = { body: series(many, b, []) };
  const html = render(sb, data);
  const cost = cardHtml(html, 'costChartsCostCard');
  assert.equal(count(cost, /<polyline/g), 6, 'six lines of ten groups');
  assert.ok(cost.includes('Showing the top 6 of 10 groups.'));
  const pts = cost.match(/<polyline[^>]*points="([^"]*)"/)[1].trim().split(/\s+/);
  assert.ok(pts.length <= 60, 'points per line ' + pts.length);
  const rep = cardHtml(html, 'costChartsReportCard');
  assert.equal(count(rep.split('<tfoot>')[0], /<tr>/g), 8, 'eight rows plus none more');
  assert.ok(rep.includes('Showing 8 of '), 'the table says it is bounded');
  assert.equal(run(sb, 'COST_CHARTS_MAX_LINES'), 6);
  assert.equal(run(sb, 'COST_CHARTS_MAX_POINTS'), 60);
  assert.equal(run(sb, 'COST_CHARTS_MAX_ROWS'), 8);
});

// ── money axes ──────────────────────────────────────────────────────────────
test('each money axis names its unit and billing mode, one chart per unit, and units are never mixed', () => {
  const { sb } = load();
  const html = render(sb, fullData(), { metric: 'money' });
  const cost = cardHtml(html, 'costChartsCostCard');
  const svgs = cost.match(/<svg[\s\S]*?<\/svg>/g);
  assert.equal(svgs.length, 2, 'dollars and points are two charts, not one axis');
  assert.ok(svgs[0].includes('$ (dollars) · API metered, billed per use'), 'dollar axis label');
  assert.ok(svgs[1].includes('pts (points) · bundled plan, API-equivalent, not billed'), 'points axis label');
  assert.equal(count(svgs[0], /<polyline/g), 1, 'only the group with dollars is on the dollar axis');
  assert.ok(svgs[0].includes('alpha') && !svgs[0].includes('beta'));
  assert.ok(svgs[1].includes('beta') && !svgs[1].includes('alpha'));
  assert.ok(!cost.includes('>tokens<'), 'no tokens axis beside money');
  // The report table keeps each unit on its own line.
  const rep = cardHtml(html, 'costChartsReportCard');
  assert.ok(rep.includes('<div>$ (dollars) · API metered, billed per use: 12.50</div>'));
  assert.ok(rep.includes('<div>pts (points) · bundled plan, API-equivalent, not billed: 40.00</div>'));
});

test('money with no rate or monthly price draws tokens and says why', () => {
  const { sb } = load();
  const html = render(sb, fullData({ noMoney: true }), { metric: 'money' });
  const cost = cardHtml(html, 'costChartsCostCard');
  assert.ok(cost.includes('>tokens<'));
  assert.ok(cost.includes('Money is not shown: no rate or monthly price is set'));
  assert.ok(cost.includes('Set rates or the monthly price in Settings'), 'next action points at the fix');
  assert.equal(run(sb, 'costChartsUnitLabel')('dollars', 'api_metered').split(' · ').length, 2);
  assert.equal(run(sb, 'costChartsUnitLabel')(null, ''), 'tokens');
});

// ── empty or off: not drawn, an Events entry instead ───────────────────────
test('a card with empty data is not drawn and its reason becomes an Events entry', () => {
  const { sb } = load();
  const data = fullData();
  data.tools = { body: toolUsage([], days(7)) };
  data.failures = { body: failures([], WEEKS) };
  const html = render(sb, data);
  assert.ok(has(html, 'costChartsCostCard'));
  assert.ok(!has(html, 'costChartsToolsCard'), 'empty tool usage is not drawn');
  assert.ok(!has(html, 'costChartsFailuresCard'), 'empty failures are not drawn');
  assert.ok(html.includes('2 charts have no data and are not shown. The reasons are in Events.'));
  const model = run(sb, 'costChartsModel')(data, { group: 'project', window: '7d', metric: 'tokens' });
  assert.equal(model.tools.state, 'empty');
  const entries = sb.window.costChartsEventEntries(model, { group: 'project', window: '7d' }, '2026-11-12T01:00:00Z');
  assert.deepEqual(Array.from(entries, e => e.id), ['cost-charts:tools', 'cost-charts:failures']);
  assert.equal(entries[0].summary, 'Tool usage is not shown');
  assert.equal(entries[0].reason, 'No tool calls were recorded in this window.');
  assert.equal(entries[0].next_action, 'Widen the window.');
  assert.equal(entries[1].reason, 'No failed, stalled or cancelled attempts in this window.');
  assert.ok(entries[0].detail.includes('Grouped by Project, window 7d. State: No data.'));
});

test('a view the daemon cannot supply is off, with the daemon error as the reason', () => {
  const { sb } = load();
  const data = fullData();
  data.failures = { error: new Error('automaton manager unavailable') };
  const model = run(sb, 'costChartsModel')(data, { group: 'project', window: '7d' });
  assert.equal(model.failures.state, 'off');
  const html = render(sb, data);
  assert.ok(!has(html, 'costChartsFailuresCard'));
  const e = sb.window.costChartsEventEntries(model, { group: 'project', window: '7d' }, 'now');
  assert.equal(e.length, 1);
  assert.ok(e[0].reason.includes('automaton manager unavailable'));
  assert.equal(e[0].next_action, 'Check that the daemon is running, then press Refresh.');
});

test('the Automation grouping turns off the views the daemon does not record per Automation', async () => {
  const { sb, slot, urls } = load({ '/api/cost/failures': failures([failureGroup('nightly', WEEKS)], WEEKS) });
  sb.window.costChartsSetGroup('automation');
  await flushAsync();
  assert.deepEqual(urls.map(u => u.split('?')[0]), ['/api/cost/failures'], 'cost and tool usage are not requested');
  assert.ok(urls[0].includes('group_by=automation'));
  assert.ok(!has(slot.html, 'costChartsCostCard') && !has(slot.html, 'costChartsToolsCard'));
  assert.ok(has(slot.html, 'costChartsFailuresCard') && has(slot.html, 'costChartsReportCard'));
  assert.ok(cardHtml(slot.html, 'costChartsReportCard').includes('Cost and tool usage are not recorded per Automation'));
  const ids = Array.from(sb._costChartsState.events, e => e.id);
  assert.deepEqual(ids, ['cost-charts:cost', 'cost-charts:tools']);
  assert.equal(sb._costChartsState.events[0].reason, 'This view is not recorded per Automation, only per Project or Automaton.');
  assert.equal(sb._costChartsState.events[0].next_action, 'Group by Project or Automaton.');
});

test('when no view has data the report card is not drawn either and the controls stay', () => {
  const { sb } = load();
  const b = days(3);
  const data = { cost: { body: series([], b, []) }, tools: { body: toolUsage([], b) }, failures: { body: failures([], WEEKS) } };
  const html = render(sb, data);
  assert.equal(count(html, /class="cost-charts-card /g), 0);
  assert.ok(html.includes('id="costChartsGroup"'), 'the operator can still widen the window');
  assert.ok(html.includes('4 charts have no data'));
  const model = run(sb, 'costChartsModel')(data, { group: 'project', window: '7d' });
  assert.equal(model.report.state, 'empty');
  assert.equal(sb.window.costChartsEventEntries(model, { group: 'project', window: '7d' }, 'now').length, 4);
});

// ── Events panel ────────────────────────────────────────────────────────────
test('Events entries are merged on top of the daemon list and respect the filters', async () => {
  const { sb } = load({
    '/api/cost/series': series([], days(3), []), '/api/cost/tool-usage': toolUsage([], days(3)),
    '/api/cost/failures': failures([], WEEKS),
  });
  sb.window.costChartsRefresh();
  await flushAsync();
  assert.equal(sb._costChartsState.events.length, 4);
  const server = [{ id: 'w1', kind: 'watch', summary: 'watch event', reason: 'r', time: '2026-11-12T00:00:00Z' }];
  const merged = sb.window.costChartsMergeEvents(server);
  assert.equal(merged.length, 5);
  assert.equal(merged[4].id, 'w1', 'the daemon entries follow');
  assert.ok(merged[0].id.indexOf('cost-charts:') === 0);
  const rowHtml = run(sb, 'automataEventRowHtml')(merged[0], true);
  assert.ok(rowHtml.includes('automata-event-kind-cost-chart'));
  assert.ok(rowHtml.includes('is not shown'));
  assert.ok(rowHtml.includes('Next action'), 'the expanded row shows the next action');
  run(sb, "_automataEventsState.kinds.add('watch')");
  assert.equal(sb.window.costChartsMergeEvents(server).length, 1, 'a kind filter leaves the daemon list alone');
  run(sb, "_automataEventsState.kinds.clear(); _automataEventsState.q = 'tool usage'");
  assert.deepEqual(Array.from(sb.window.costChartsMergeEvents([]), e => e.id), ['cost-charts:tools']);
  run(sb, "_automataEventsState.q = ''");
});

test('with nothing recorded the Events list is the daemon list unchanged', () => {
  const { sb } = load();
  const server = [{ id: 'a' }];
  assert.equal(sb.window.costChartsMergeEvents(server), server);
});

// ── loading and placement ───────────────────────────────────────────────────
test('the loader reads the three routes for the chosen grouping and window and paints the slot', async () => {
  const b = days(7);
  const { sb, slot, urls } = load({
    '/api/cost/series': fullData().cost.body, '/api/cost/tool-usage': fullData().tools.body, '/api/cost/failures': fullData().failures.body,
  });
  assert.equal(slot.style.display, 'none', 'hidden until the first paint');
  sb.window.costChartsLoad();
  await flushAsync();
  assert.deepEqual(urls.slice().sort(), [
    '/api/cost/failures?group_by=project&window=7d',
    '/api/cost/series?group_by=project&window=7d',
    '/api/cost/tool-usage?group_by=project&window=7d',
  ]);
  assert.equal(slot.style.display, '', 'shown once loaded');
  assert.equal(count(slot.html, /class="cost-charts-card /g), 4);
  void b;
  sb.window.costChartsSetWindow('30d');
  await flushAsync();
  assert.ok(urls.slice(-3).every(u => u.endsWith('window=30d')), 'a new window reloads');
  sb.window.costChartsSetGroup('automaton');
  await flushAsync();
  assert.ok(urls.slice(-3).every(u => u.indexOf('group_by=automaton') >= 0));
  const before = urls.length;
  sb.window.costChartsSetMetric('money');
  await flushAsync();
  assert.equal(urls.length, before, 'the metric redraws without a new request');
  sb.window.costChartsSetGroup('bogus');
  sb.window.costChartsSetWindow('1y');
  assert.equal(urls.length, before, 'unknown choices are ignored');
});

test('a 503 from a view is shown as off with the daemon text, not as a broken card', async () => {
  const { sb, slot } = load({
    '/api/cost/series': fullData().cost.body, '/api/cost/tool-usage': fullData().tools.body,
    '/api/cost/failures': { status: 503, body: 'automaton manager not available' },
  });
  sb.window.costChartsLoad();
  await flushAsync();
  assert.ok(!has(slot.html, 'costChartsFailuresCard'));
  const ev = sb._costChartsState.events.find(e => e.id === 'cost-charts:failures');
  assert.ok(ev && ev.reason.includes('automaton manager not available'));
});

test('placement is one constant: the cards live in the cost report slot of the tab it names', () => {
  const { sb } = load();
  assert.equal(count(APP_SRC, /^const COST_CHARTS_TAB = /gm), 1, 'one declaration');
  assert.equal(run(sb, 'COST_CHARTS_TAB'), 'progress');
  assert.equal(run(sb, 'COST_CHARTS_SLOT_ID'), 'automationCostChartsCard');
  const slots = run(sb, "_automationSlotsOf(COST_CHARTS_TAB).map(s => s.id)");
  assert.ok(Array.from(slots).includes('automationCostChartsCard'));
  assert.equal(count(APP_SRC, /registerAutomationCardSlot\(COST_CHARTS_TAB,/g), 1, 'registered through the constant, not a literal tab');
  assert.ok(!/\['progress', 'automationCostChartsCard'/.test(APP_SRC), 'no second, literal registration of the slot');
  const slotHtml = run(sb, "_automationSlotsHTML(COST_CHARTS_TAB)");
  assert.ok(slotHtml.includes('id="automationCostChartsCard"') && slotHtml.includes('display:none'), 'a hidden placeholder until it has cards');
  const order = run(sb, "_automationSlotsOf(COST_CHARTS_TAB).map(s => s.id)");
  assert.deepEqual(Array.from(order), ['automationAllProjectsGanttCard', 'automationProgressHeadline', 'automationCostChartsCard']);
});

test('mounting the Progress tab loads the cards and leaving it drops a load in flight', async () => {
  const { sb, slot } = load({ '/api/cost/series': fullData().cost.body, '/api/cost/tool-usage': fullData().tools.body, '/api/cost/failures': fullData().failures.body });
  run(sb, "_automationSlotsMount('progress')");
  await flushAsync();
  assert.ok(has(slot.html, 'costChartsCostCard'));
  const gen = sb._costChartsState.gen;
  run(sb, "_automationSlotsStop('progress')");
  assert.ok(sb._costChartsState.gen > gen);
  assert.equal(sb._costChartsState.loading, false);
});

// ── source rules ────────────────────────────────────────────────────────────
test('a code comment marks the existing helper that was extended', () => {
  assert.match(APP_SRC, /Helper extended: the Projects Gantt helpers\. _costChartsLinesSVG and\n\/\/ _costChartsBarsSVG extend _projectGanttSVG/);
  const { sb } = load();
  assert.equal(typeof run(sb, '_projectGanttSVG'), 'function', 'the helper named in the comment exists');
  assert.equal(typeof run(sb, '_projectGanttPalette'), 'function');
});

test('operator-facing text says Automaton, Automata and Automation, never PRD, and carries no tracker identifier', () => {
  const block = APP_SRC.slice(APP_SRC.indexOf('// ── Cost report chart cards'), APP_SRC.indexOf('registerAutomationCardSlot(COST_CHARTS_TAB'));
  assert.ok(block.length > 5000, 'found the cards block');
  assert.ok(!/\bPRDs?\b/.test(block), 'no PRD in the cards code');
  assert.ok(!/\b(BL|GH|SEC|HLLM)-?\d+\b/.test(block), 'no tracker id in the cards code');
  const { sb } = load();
  const html = render(sb, fullData());
  assert.ok(!/PRD/.test(html));
  for (const l of LANGS) {
    for (const [k, v] of Object.entries(BUNDLES[l])) {
      if (k.indexOf('cost_charts_') !== 0) continue;
      assert.ok(!/PRD/.test(v), l + ' ' + k + ' says PRD');
      assert.ok(!/\b(BL|GH|SEC|HLLM)-?\d+\b/.test(v), l + ' ' + k + ' carries a tracker id');
    }
  }
});

// ── locale keys ─────────────────────────────────────────────────────────────
// Every cost_charts_* key the code names, from literal quoted keys.
function keysInSource() {
  const keys = new Set();
  for (const m of APP_SRC.matchAll(/'(cost_charts_[a-z_]+)'/g)) if (!m[1].endsWith('_')) keys.add(m[1]);
  return Array.from(keys).sort();
}

test('every cost_charts_* key used by the cards exists, non-empty, in all five locales', () => {
  const keys = keysInSource();
  assert.ok(keys.length >= 60, 'found the keys, got ' + keys.length);
  for (const required of [
    'cost_charts_title_cost', 'cost_charts_title_tools', 'cost_charts_title_failures', 'cost_charts_title_report',
    'cost_charts_purpose_cost', 'cost_charts_purpose_tools', 'cost_charts_purpose_failures', 'cost_charts_purpose_report',
    'cost_charts_help_tip', 'cost_charts_help_label', 'cost_charts_unit_tokens', 'cost_charts_unit_dollars', 'cost_charts_unit_points',
    'cost_charts_unit_tool_calls', 'cost_charts_unit_attempts', 'cost_charts_mode_bundled_plan', 'cost_charts_mode_api_metered',
    'cost_charts_mode_free_local', 'cost_charts_reason_empty_cost', 'cost_charts_reason_empty_tools', 'cost_charts_reason_empty_failures',
    'cost_charts_reason_empty_report', 'cost_charts_reason_not_grouped', 'cost_charts_reason_unavailable',
    'cost_charts_group_project', 'cost_charts_group_automation', 'cost_charts_group_automaton',
    'cost_charts_metric_tokens', 'cost_charts_metric_money',
  ]) assert.ok(keys.includes(required), 'the code uses ' + required);
  for (const l of LANGS) {
    for (const k of keys) {
      assert.ok(typeof BUNDLES[l][k] === 'string' && BUNDLES[l][k].length > 0, l + ' has ' + k);
    }
  }
});

test('the locale files carry no cost_charts_* key the code does not use, and keep the same placeholders in every language', () => {
  const used = new Set(keysInSource());
  const placeholders = s => (s.match(/%\d+\$s/g) || []).sort().join(',');
  for (const k of Object.keys(BUNDLES.en).filter(x => x.indexOf('cost_charts_') === 0)) {
    assert.ok(used.has(k), k + ' is used by app.js');
    for (const l of LANGS) assert.equal(placeholders(BUNDLES[l][k]), placeholders(BUNDLES.en[k]), l + ' ' + k + ' placeholders');
  }
  for (const l of LANGS) {
    const own = Object.keys(BUNDLES[l]).filter(x => x.indexOf('cost_charts_') === 0).sort();
    assert.deepEqual(own, Object.keys(BUNDLES.en).filter(x => x.indexOf('cost_charts_') === 0).sort(), l + ' has the same keys as en');
  }
});

test('translations differ from English where the language is not English (no copied placeholders)', () => {
  for (const l of ['de', 'es', 'fr', 'ja']) {
    assert.notEqual(BUNDLES[l].cost_charts_title_cost, BUNDLES.en.cost_charts_title_cost, l + ' title');
    assert.notEqual(BUNDLES[l].cost_charts_purpose_report, BUNDLES.en.cost_charts_purpose_report, l + ' purpose');
    assert.notEqual(BUNDLES[l].cost_charts_reason_empty_cost, BUNDLES.en.cost_charts_reason_empty_cost, l + ' reason');
  }
});
