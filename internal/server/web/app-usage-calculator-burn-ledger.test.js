// Usage calculator burn-ledger variant — PWA side.
//
// window.usageCalcRenderers['burn-ledger'](body, opts) draws the bar chart of the points used
// each day since the reset (one bar for each daily entry), the cost-per-Automaton table with a
// measured and a planning column, the queue line, the empty state for other consumers and the
// text forms. The shared policy and lane card come from the frame. The renderer only reads the
// response; these tests check what it draws from the worked example and from edited copies.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody } = require('./usage-calculator-fixture');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(sandbox, body) {
  sandbox._body = body;
  return vm.runInContext("window.usageCalcRenderers['burn-ledger'](_body, {})", sandbox);
}

function frame(sandbox, body, opts) {
  sandbox._body = body;
  sandbox._opts = opts || {};
  return vm.runInContext('window.usageCalcFrameHtml(_body, _opts)', sandbox);
}

// barsOf reads each day bar: its index, its points as the body gives them, and its estimated mark.
function barsOf(html) {
  return [...html.matchAll(/<g data-day="(\d+)"([^>]*)>[\s\S]*?<rect data-points="([^"]*)"/g)].map(m => ({
    day: Number(m[1]),
    points: Number(m[3]),
    estimated: m[2].includes('data-estimated="true"'),
  }));
}

// textOf strips the markup from a fragment and collapses its white space.
function textOf(fragment) {
  return fragment.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
}

// dataText reads the text of the element that carries data-<attr>="<value>".
function dataText(html, attr, value) {
  const m = new RegExp(`<[a-z]+ data-${attr}="${value}"[^>]*>([\\s\\S]*?)</[a-z]+>`).exec(html);
  return m ? textOf(m[1]) : null;
}

test('the burn-ledger renderer is registered under its variant id', () => {
  const s = loadApp();
  assert.equal(vm.runInContext("typeof window.usageCalcRenderers['burn-ledger']", s), 'function');
});

test('one bar is drawn for each daily entry, with its points and its estimated mark', () => {
  const body = workedExampleBody();
  const bars = barsOf(render(loadApp(), body));
  assert.equal(bars.length, body.advice.daily.length);
  bars.forEach((bar, i) => {
    const want = body.advice.daily[i];
    assert.equal(bar.day, i, `bar ${i} index`);
    assert.equal(bar.points, want.points, `bar ${i} points`);
    assert.equal(bar.estimated, want.estimated, `bar ${i} estimated`);
  });
});

test('the day bars sum to the newest percent for the worked example', () => {
  const body = workedExampleBody();
  const total = barsOf(render(loadApp(), body)).reduce((sum, bar) => sum + bar.points, 0);
  assert.ok(Math.abs(total - body.advice.percent) < 0.001,
    `bars sum to ${total}, the newest percent is ${body.advice.percent}`);
});

test('no split by kind of work is drawn: one rectangle per day and no kind segments', () => {
  const html = render(loadApp(), workedExampleBody());
  const rects = html.match(/<rect /g) || [];
  assert.equal(rects.length, workedExampleBody().advice.daily.length);
  assert.ok(!html.includes('data-kind'), 'no kind segments are marked');
  for (const word of ['Build workers', 'Decompose', 'Verify', 'Supervision', 'Merges']) {
    assert.ok(!html.includes(word), `no ${word} share is drawn`);
  }
  assert.ok(html.includes(EN.usage_calc_ledger_kind_note), 'the kind-of-work note is shown instead');
});

test('the chart note says the totals are estimates that sum to the newest percent', () => {
  const html = render(loadApp(), workedExampleBody());
  assert.ok(html.includes('Points used each day since the reset'));
  assert.ok(textOf(html).includes('The totals are estimates from straight-line interpolation'));
  assert.ok(textOf(html).includes('sum to the newest percent'));
});

test('the other consumers section is an honest empty state with no rows and no figures', () => {
  const html = render(loadApp(), workedExampleBody());
  const start = html.lastIndexOf('<div', html.indexOf('class="usage-calc-ledger-consumers"'));
  const end = html.lastIndexOf('<div', html.indexOf('class="usage-calc-ledger-text"'));
  assert.ok(start >= 0 && end > start, 'the consumers section sits before the text forms');
  const section = html.slice(start, end);
  assert.ok(section.includes(EN.usage_calc_ledger_consumers_empty));
  assert.ok(section.includes('No consumer registry exists yet'));
  assert.ok(!/<(tr|table)[\s>]/.test(section), 'no example rows are drawn');
  assert.ok(!/\d/.test(textOf(section)), 'no invented figures are drawn');
});

test('the queue line shows the estimate and the typed override, and which one is in use', () => {
  const s = loadApp();
  const estimate = render(s, workedExampleBody());
  assert.equal(dataText(estimate, 'queue', 'queued'), 'Queued Automata: 6, at 3.3 points each.');
  assert.match(dataText(estimate, 'queue', 'estimate'), /^Estimate: 20\.0 points/);
  assert.equal(dataText(estimate, 'queue', 'typed'), 'Typed override: none.');
  assert.equal(dataText(estimate, 'queue', 'in-use'), 'In use: the estimate.');

  const typed = render(s, workedExampleBody({ advice: { remaining_work: { typed_points: 15.5, in_use: 'typed', points: 15.5 } } }));
  assert.match(dataText(typed, 'queue', 'estimate'), /^Estimate: 20\.0 points/, 'the estimate stays on the screen');
  assert.equal(dataText(typed, 'queue', 'typed'), 'Typed override: 15.5 points.');
  assert.equal(dataText(typed, 'queue', 'in-use'), 'In use: the typed override.');
});

test('a body with no daily entries shows the empty state and draws no bars', () => {
  const html = render(loadApp(), workedExampleBody({ advice: { daily: [] } }));
  assert.ok(html.includes(EN.usage_calc_ledger_daily_empty));
  assert.equal(barsOf(html).length, 0);
  assert.ok(!html.includes('<svg'), 'no chart is drawn');
});

test('the cost table shows the measured and the planning burn of each lane count', () => {
  const body = workedExampleBody();
  const html = render(loadApp(), body);
  assert.equal(body.advice.by_lanes.length, 4);
  for (const row of body.advice.by_lanes) {
    assert.equal(dataText(html, 'measured', row.lanes), row.burn_per_24h.toFixed(1), `measured at ${row.lanes} lanes`);
    assert.equal(dataText(html, 'planning', row.lanes), row.planning_burn_per_24h.toFixed(1), `planning at ${row.lanes} lanes`);
  }
  assert.ok(textOf(html).includes('The planning column is not a forecast'));
});

test('the text-form preview draws the body text forms as they are, escaped', () => {
  const body = workedExampleBody({ text: { cli: 'usage: 72% read <Fri> 22:08', comm: 'Weekly 72%\nReply: usage lanes 1' } });
  const html = render(loadApp(), body);
  assert.ok(html.includes(EN.usage_calc_ledger_text_cli));
  assert.ok(html.includes('usage: 72% read &lt;Fri&gt; 22:08'), 'the CLI form is escaped and shown');
  assert.ok(html.includes('Weekly 72%\nReply: usage lanes 1'), 'the comm form keeps its lines');
});

test('a body without text forms shows the empty text form', () => {
  const body = workedExampleBody();
  delete body.text;
  assert.ok(render(loadApp(), body).includes(EN.usage_calc_ledger_text_empty));
});

test('the frame supplies the shared policy and lane card around the burn-ledger body', () => {
  const html = frame(loadApp(), workedExampleBody(), { variant: 'burn-ledger' });
  assert.equal((html.match(/name="usageCalcPolicy"/g) || []).length, 3, 'the three policy radios');
  assert.ok(html.includes('id="usageCalcLanesPlus"'), 'the lane stepper');
  assert.ok(html.includes('Points used each day since the reset'), 'the burn-ledger chart is inside the frame');
});

test('the verdict card carries the nine shared reading fields, open from the start', () => {
  const html = frame(loadApp(), workedExampleBody(), { variant: 'burn-ledger' });
  const start = html.indexOf('class="usage-calc-ledger-fields"');
  assert.ok(start > html.indexOf('class="usage-calc-verdict"'), 'the fields sit in the verdict card');
  assert.equal((html.match(/data-field="\d+"/g) || []).length, 9, 'one row for each shared field');
  assert.ok(!html.includes('<details'), 'the ledger does not fold the fields away');
});

test('the layout is fluid at phone width: the chart scales and nothing is fixed wider than the column', () => {
  const html = render(loadApp(), workedExampleBody());
  assert.ok(html.includes('<svg viewBox="0 0 320 132" width="100%"'), 'the chart scales to the column');
  assert.ok(!/width:\s*\d{3,}px/.test(html), 'no fixed pixel width of three digits or more');
  assert.ok(!html.includes('nowrap'), 'no line is forced to stay on one row');
});

test('the visible text names Automata, not PRD, and carries no tracker id', () => {
  const text = textOf(render(loadApp(), workedExampleBody()));
  assert.ok(!/\bPRD/.test(text), 'the work unit is Automata or Automaton');
  assert.ok(!/\b(BL|GH|SEC|B)\d+\b/.test(text), 'no tracker id');
});

test('every usage_calc_ledger key is in all five bundles', () => {
  const keys = Object.keys(EN).filter(k => k.startsWith('usage_calc_ledger_'));
  assert.ok(keys.length >= 30, `found ${keys.length} ledger keys`);
  for (const l of ['de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${l}.json`), 'utf8'));
    for (const k of keys) assert.ok(k in bundle, `${l}.json is missing ${k}`);
  }
});

test('every usage_calc_ledger key the renderer names exists in the English bundle', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const used = new Set(src.match(/usage_calc_ledger_[a-z0-9_]+/g));
  assert.ok(used.size > 0, 'the renderer names ledger keys');
  for (const k of used) assert.ok(k in EN, `en.json has no ${k}`);
});
