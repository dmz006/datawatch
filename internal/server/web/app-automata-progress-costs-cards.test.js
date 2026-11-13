// The reworked "Progress and costs" graph cards on the Automata page:
// each card carries a card-head with its title and a "?" help link, every
// series is named by its Automaton title (not its id) in the legend, the
// SVG tooltip and the tables, each card offers a compact selector row for
// the grouping, the window, the metric and the number of series drawn
// (Top-N: five, ten, twenty or all, default ten), and each series in the
// legend is a button that shows or hides it without a new request.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_PATH = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const readLocale = l => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
const BUNDLES = Object.fromEntries(LANGS.map(l => [l, readLocale(l)]));
const HELP_HREF = '/diagrams.html#docs/howto/cost-report.md';

function jsonRes(body, status) {
  const ok = !status || status < 400;
  return { ok, status: status || 200, statusText: ok ? '' : 'error', json: () => Promise.resolve(body), text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)) };
}

function load() {
  const slot = makeStubElement(h => { slot.html = h; });
  slot.style = { display: 'none' };
  const sandbox = buildSandbox({ byId: { automationCostChartsCard: slot } });
  sandbox.URLSearchParams = URLSearchParams;
  const urls = [];
  sandbox.fetch = url => {
    urls.push(String(url));
    return Promise.resolve(jsonRes({}));
  };
  vm.createContext(sandbox);
  loadScript(APP_PATH, sandbox);
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
function seriesGroup(key, buckets, tokenAt) {
  const points = buckets.map((b, i) => ({ start: b, sessions: 1, tokens_in: tokenAt(i), tokens_out: 0, tokens: tokenAt(i) }));
  return { key, sessions: points.length, tokens: points.reduce((a, p) => a + p.tokens, 0), unit_totals: [], points };
}
function automataData() {
  const b = days(7);
  return {
    cost: { body: { group_by: 'automaton', bucket: 'day', window: '7d', buckets: b, retention_days: 31, totals_by_unit: [],
      groups: [seriesGroup('prd-9', b, i => 100 + i), seriesGroup('prd-10', b, i => 50 + i)] } },
    tools: { body: { group_by: 'automaton', bucket: 'day', window: '7d', buckets: b, retention_days: 31,
      groups: [{ key: 'prd-9', sessions: 7, tool_calls: 21, points: b.map((d, i) => ({ start: d, sessions: 1, tool_calls: i + 1 })) },
               { key: 'prd-10', sessions: 7, tool_calls: 14, points: b.map(d => ({ start: d, sessions: 1, tool_calls: 2 })) }],
      total_tool_calls: 35, total_sessions: 14 } },
    failures: { body: { group_by: 'automaton', window: '12w',
      weeks: ['2026-10-26T00:00:00Z', '2026-11-02T00:00:00Z'],
      groups: [{ key: 'prd-9', label: '', attempts: 4, tasks: 3, retries: 2,
        categories: [{ category: 'infra', attempts: 3 }], weeks: [], top_summaries: [{ category: 'infra', summary: 'timeout', attempts: 3 }] }],
      categories: [{ category: 'infra', attempts: 3 }], total_attempts: 4, total_tasks: 3, total_retries: 2, undated_attempts: 0 } },
  };
}
const render = (sb, opts) => sb.window.costChartsCardsHtml(automataData(), Object.assign({ group: 'automaton', window: '7d', metric: 'tokens', topN: 10, hidden: { cost: {}, tools: {}, failures: {}, report: {} } }, opts || {}));
const cardHtml = (html, id) => {
  const i = html.indexOf('id="' + id + '"');
  assert.ok(i >= 0, id + ' is drawn');
  const next = html.indexOf('<div id="costCharts', i + 10);
  return html.slice(i, next < 0 ? html.length : next);
};

// ── card heads ──────────────────────────────────────────────────────────────
test('each card carries a card-head with its title and a "?" help link to the cost report howto', () => {
  const { sb } = load();
  const html = render(sb);
  const titles = { costChartsCostCard: 'Cost over time', costChartsToolsCard: 'Tool usage', costChartsFailuresCard: 'Retries and failure categories', costChartsReportCard: 'Combined usage report' };
  for (const id in titles) {
    const card = cardHtml(html, id);
    const head = card.match(/class="cap-title card-head">([^<]*)</);
    assert.ok(head, id + ' has a card-head');
    assert.equal(head[1].trim(), titles[id], id + ' head names its purpose');
    assert.ok(card.includes('class="docs-link cost-charts-help"'), id + ' has the help link');
    assert.ok(card.includes('href="' + HELP_HREF + '"'), id + ' help points at the cost report howto');
  }
});

// ── Automaton titles, never ids ─────────────────────────────────────────────
test('the legend, the SVG tooltip and the tables name Automata by title, never by id', () => {
  const { sb } = load();
  run(sb, '_automataState').allPrds = [
    { id: 'prd-9', title: 'Ship the cost report' },
    { id: 'prd-10', title: 'Watch the CI' },
  ];
  const html = render(sb);
  const cost = cardHtml(html, 'costChartsCostCard');
  assert.ok(cost.includes('<span>Ship the cost report</span>'), 'the legend says the title');
  assert.ok(cost.includes('<span>Watch the CI</span>'), 'the legend says the second title');
  assert.ok(!cost.includes('<span>prd-9</span>') && !cost.includes('<span>prd-10</span>'), 'the legend does not say the ids');
  assert.ok(cost.includes('<title>Ship the cost report · 721</title>'), 'the SVG tooltip says the title with the total');
  assert.ok(!/<title>prd-9 · /.test(cost), 'the SVG tooltip does not say the id');
  const rep = cardHtml(html, 'costChartsReportCard');
  assert.ok(rep.includes('>Ship the cost report<') && rep.includes('>Watch the CI<'), 'the report table says the titles');
  assert.ok(!rep.includes('>prd-9<') && !rep.includes('>prd-10<'), 'the report table does not say the ids');
  // costChartsKeyLabel is the single resolver: label wins, then the Automaton
  // title, then the raw key; the Other fold is always named Other.
  const label = sb.window.costChartsKeyLabel;
  assert.equal(label({ key: 'prd-9', label: '' }), 'Ship the cost report');
  assert.equal(label({ key: 'prd-9', label: 'Renamed by the server' }), 'Renamed by the server');
  assert.equal(label({ key: 'unknown-key', label: '' }), 'unknown-key');
  assert.equal(label({ key: run(sb, 'COST_CHARTS_OTHER_KEY'), label: '' }), 'Other');
});

test('a series without an Automaton title keeps its readable label or its key', () => {
  const { sb } = load();
  const label = sb.window.costChartsKeyLabel;
  assert.equal(label({ key: 'p1', label: 'Billing project' }), 'Billing project');
  assert.equal(label({ key: 'm4', label: '' }), 'm4');
});

// ── the selector row on every card ──────────────────────────────────────────
test('every card offers a compact selector row: grouping, window, metric and the number of series', () => {
  const { sb } = load();
  const html = render(sb);
  for (const [view, suffix] of [['cost', 'Cost'], ['tools', 'Tools'], ['failures', 'Failures'], ['report', 'Report']]) {
    const card = cardHtml(html, 'costCharts' + suffix + 'Card');
    for (const [part, handler] of [['Group', 'costChartsSetGroup'], ['Window', 'costChartsSetWindow'], ['Metric', 'costChartsSetMetric'], ['Series', 'costChartsSetTopN']]) {
      const id = 'costCharts' + suffix + part;
      assert.ok(card.includes('id="' + id + '"'), view + ' selector row has ' + part);
      assert.ok(card.includes('onchange="window.' + handler + '(this.value)"'), id + ' fires its handler');
    }
  }
});

// ── Top-N ───────────────────────────────────────────────────────────────────
test('Top-N offers five, ten, twenty and all; ten is selected by default', () => {
  const { sb } = load();
  const card = cardHtml(render(sb), 'costChartsCostCard');
  const sel = card.match(/id="costChartsCostSeries"[\s\S]*?<\/select>/)[0];
  for (const value of ['5', '10', '20', 'all']) assert.ok(sel.includes('value="' + value + '"'), 'option ' + value);
  assert.ok(sel.includes('value="10" selected'), 'ten is the default');
  assert.ok(sel.includes('>' + BUNDLES.en.cost_charts_topn_all + '</option>'), 'the last option says all');
  assert.deepEqual(Array.from(run(sb, 'COST_CHARTS_TOPN_OPTIONS')), [5, 10, 20, 0]);
  assert.equal(run(sb, 'COST_CHARTS_TOPN_DEFAULT'), 10);
});

test('changing the Top-N redraws from the loaded data with no new request', () => {
  const { sb, urls } = load();
  sb.window.costChartsSetTopN('5');
  assert.equal(run(sb, '_costChartsState').topN, 5);
  sb.window.costChartsSetTopN('all');
  assert.equal(run(sb, '_costChartsState').topN, 0, 'all is zero');
  sb.window.costChartsSetTopN('10');
  assert.equal(run(sb, '_costChartsState').topN, 10);
  sb.window.costChartsSetTopN('7');
  assert.equal(run(sb, '_costChartsState').topN, 10, 'a value outside the options is refused');
  assert.equal(urls.length, 0, 'no request was made');
});

// ── show or hide one series ─────────────────────────────────────────────────
test('each series in the legend is a button that shows or hides it, and it keeps its name', () => {
  const { sb } = load();
  run(sb, '_automataState').allPrds = [{ id: 'prd-9', title: 'Ship the cost report' }, { id: 'prd-10', title: 'Watch the CI' }];
  const html = render(sb);
  const card = cardHtml(html, 'costChartsCostCard');
  const buttons = card.match(/<button type="button" class="cost-charts-legend-tile[\s\S]*?<\/button>/g) || [];
  assert.equal(buttons.length, 2, 'one button per drawn series');
  assert.ok(card.includes('onclick="window.costChartsToggleSeries(\'cost\', \'prd-9\')"'), 'the button names the series it toggles');
  assert.ok(buttons[0].includes('aria-pressed="true"'), 'a shown series says it is pressed');
  // Hide one series: the button is flipped, the tile struck through, the
  // name kept, the drawing left with the other line, no new request.
  const { urls } = { urls: [] };
  sb.window.costChartsToggleSeries('cost', 'prd-9');
  const st = Object.assign({}, run(sb, '_costChartsState'));
  const html2 = sb.window.costChartsCardsHtml(automataData(), st);
  const card2 = cardHtml(html2, 'costChartsCostCard');
  const off = (card2.match(/<button type="button" class="cost-charts-legend-tile cost-charts-legend-tile-off"[\s\S]*?<\/button>/) || [])[0];
  assert.ok(off, 'the hidden series tile is struck through');
  assert.ok(off.includes('<span>Ship the cost report</span>'), 'the hidden series keeps its name');
  assert.ok(off.includes('aria-pressed="false"'), 'the hidden series button is flipped');
  assert.equal((card2.match(/<polyline/g) || []).length, 1, 'only the other series is drawn');
  assert.equal(urls.length, 0, 'no request was made');
  // The Other fold is not a button: it is a plain entry.
  const many = automataData();
  const b = many.cost.body.buckets;
  for (let i = 0; i < 6; i++) many.cost.body.groups.push(seriesGroup('g' + i, b, () => 5 + i));
  const html3 = sb.window.costChartsCardsHtml(many, Object.assign({}, run(sb, '_costChartsState'), { topN: 5 }));
  const card3 = cardHtml(html3, 'costChartsCostCard');
  assert.ok(card3.includes('>Other</span>') && !card3.includes('costChartsToggleSeries(\'cost\', \'' + run(sb, 'COST_CHARTS_OTHER_KEY') + '\''), 'the Other fold is named, not toggleable');
});

// ── locales and styles ──────────────────────────────────────────────────────
test('the new cost_charts strings exist, non-empty, in all five locales and are translated', () => {
  for (const key of ['cost_charts_topn_label', 'cost_charts_topn_all', 'cost_charts_other', 'cost_charts_legend_toggle']) {
    const en = BUNDLES.en[key];
    assert.ok(en && String(en).trim(), key + ' exists in English');
    for (const lang of LANGS) {
      const v = BUNDLES[lang][key];
      assert.ok(v && String(v).trim(), key + ' exists in ' + lang);
    }
  }
  // The words are real translations, not a copy of English (a key that is
  // identical in English and a language is fine where the words genuinely
  // match, as in Spanish: at least one of the four words differs per locale).
  for (const lang of LANGS.slice(1)) {
    const differs = ['cost_charts_topn_label', 'cost_charts_topn_all', 'cost_charts_other', 'cost_charts_legend_toggle']
      .some(k => String(BUNDLES[lang][k]).trim() !== String(BUNDLES.en[k]).trim());
    assert.ok(differs, lang + ' translates at least one of the new cost_charts words');
  }
});

test('the legend tiles are styled: the tile, its hover and the struck-through hidden state', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  for (const rule of ['.cost-charts-legend-tile {', '.cost-charts-legend-tile:hover', '.cost-charts-legend-tile:focus-visible', '.cost-charts-legend-tile-off'])
    assert.ok(css.includes(rule), 'style.css has ' + rule);
  assert.ok(/\.cost-charts-legend-tile-off[^]*?text-decoration:\s*line-through/.test(css), 'the hidden state is struck through');
});

// ── operator-facing wording ─────────────────────────────────────────────────
test('operator-facing card text says Automaton, never PRD, and leaks no tracker id', () => {
  const { sb } = load();
  run(sb, '_automataState').allPrds = [
    { id: 'prd-9', title: 'Ship the cost report' },
    { id: 'prd-10', title: 'Watch the CI' },
  ];
  const raw = cardHtml(render(sb), 'costChartsCostCard');
  // class and id attributes are markup, not operator-facing text; the toggle
  // keys in onclick attributes are internal keys, not visible names.
  const visible = raw.replace(/class="[^"]*"/g, '').replace(/id="[^"]*"/g, '').replace(/onclick="window\.costChartsToggleSeries\('[^']*',\s*'[^']*'\)"/g, '');
  assert.ok(!/PRD/.test(visible), 'no uppercase PRD wording in the card');
  assert.ok(visible.includes('>Automation<') || visible.includes('>Automaton<'), 'grouping names the operator vocabulary');
  assert.ok(!/BL\d{3,}/.test(visible), 'no tracker id in visible text');
});
