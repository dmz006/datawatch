// Dashboard Timeline card (id 'gantt') — bars drawn from task timestamps.
//
// Loads the real app.js into the browser stub sandbox and checks that each
// bar's start and end come from the task record's started_at / completed_at
// (a live task runs to now), that bars are clipped to the 6h window, that the
// card is grouped by Automaton, that the SVG stays bounded in width, and that
// the card is hidden through its DASH_CARD_FEATURES active() check when no
// Automaton task has a bar in the window.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const HOUR = 3600000;
const WIN_MS = 6 * HOUR;

function loadAppSandbox(svgEl) {
  const sandbox = buildSandbox({ byId: svgEl ? { dashGanttSvg: svgEl } : {} });
  sandbox.getComputedStyle = () => ({ getPropertyValue: () => '' });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// svgStub is the #dashGanttSvg element: its parent is the card body, whose
// clientWidth is what the Timeline sizes itself from.
function svgStub(clientWidth) {
  const el = { parentElement: { clientWidth }, innerHTML: '', attrs: {} };
  el.setAttribute = (k, v) => { el.attrs[k] = String(v); };
  return el;
}

function iso(ms) { return new Date(ms).toISOString(); }

function setPRDs(sandbox, prds) {
  sandbox.__prds = prds;
  vm.runInContext('_dash._prds = __prds', sandbox);
}

// A fixed clock for the pure bar helper, so placement is exact.
const NOW = Date.UTC(2026, 10, 10, 12, 0, 0);

const PRD_ALPHA = {
  id: 'prd-alpha',
  title: 'Alpha Automaton',
  status: 'running',
  stories: [
    {
      id: 's1',
      tasks: [
        // Finished inside the window: bar runs from started_at to completed_at.
        { id: 'done-2h', title: 'Done task', status: 'completed', started_at: iso(NOW - 2 * HOUR), completed_at: iso(NOW - 1 * HOUR) },
        // Running now: bar runs from started_at to now.
        { id: 'live-30m', title: 'Live task', status: 'in_progress', started_at: iso(NOW - 30 * 60000) },
        // Finished before the window: dropped.
        { id: 'old', title: 'Old task', status: 'completed', started_at: iso(NOW - 9 * HOUR), completed_at: iso(NOW - 8 * HOUR) },
        // Started before the window, finished inside it: clipped to the window start.
        { id: 'spans', title: 'Spanning task', status: 'failed', started_at: iso(NOW - 7 * HOUR), completed_at: iso(NOW - 1 * HOUR) },
        // No start timestamp yet: no bar.
        { id: 'queued', title: 'Queued task', status: 'pending' },
        // Starts in the future: no bar.
        { id: 'future', title: 'Future task', status: 'pending', started_at: iso(NOW + HOUR) },
        // Completed without a completed_at: no end, so no bar.
        { id: 'no-end', title: 'No end task', status: 'completed', started_at: iso(NOW - 3 * HOUR) },
      ],
    },
  ],
};

const PRD_BETA_OLD_ONLY = {
  id: 'prd-beta',
  title: 'Beta Automaton',
  status: 'blocked',
  stories: [{ id: 's2', tasks: [
    { id: 'gone', title: 'Gone task', status: 'completed', started_at: iso(NOW - 10 * HOUR), completed_at: iso(NOW - 9 * HOUR) },
  ] }],
};

test('bars take start and end from the task timestamps', () => {
  const sandbox = loadAppSandbox();
  const bars = JSON.parse(JSON.stringify(
    vm.runInContext('_ganttTaskBars', sandbox)(PRD_ALPHA, NOW, WIN_MS)));
  const byId = Object.fromEntries(bars.map(b => [b.id, b]));
  assert.deepEqual(Object.keys(byId).sort(), ['done-2h', 'live-30m', 'spans'].sort());

  assert.equal(byId['done-2h'].start, NOW - 2 * HOUR);
  assert.equal(byId['done-2h'].end, NOW - 1 * HOUR);
  assert.equal(byId['done-2h'].live, false);
});

test('a live task runs to now, and a bar that began before the window is clipped to its start', () => {
  const sandbox = loadAppSandbox();
  const bars = JSON.parse(JSON.stringify(
    vm.runInContext('_ganttTaskBars', sandbox)(PRD_ALPHA, NOW, WIN_MS)));
  const byId = Object.fromEntries(bars.map(b => [b.id, b]));

  assert.equal(byId['live-30m'].start, NOW - 30 * 60000);
  assert.equal(byId['live-30m'].end, NOW);
  assert.equal(byId['live-30m'].live, true);

  assert.equal(byId['spans'].start, NOW - WIN_MS, 'clipped to the window start');
  assert.equal(byId['spans'].end, NOW - 1 * HOUR);
});

test('an Automaton with only tasks outside the window yields no bars', () => {
  const sandbox = loadAppSandbox();
  const bars = vm.runInContext('_ganttTaskBars', sandbox)(PRD_BETA_OLD_ONLY, NOW, WIN_MS);
  assert.equal(bars.length, 0);
});

test('the card is active only while some Automaton has a bar, and says why when off', () => {
  const sandbox = loadAppSandbox();
  setPRDs(sandbox, []);
  assert.equal(vm.runInContext("_dashCardFeature('gantt').active()", sandbox), false);
  assert.equal(vm.runInContext("_dashCardFeature('gantt').reasonKey", sandbox), 'dash_card_off_gantt_reason');

  setPRDs(sandbox, [PRD_BETA_OLD_ONLY]);
  assert.equal(vm.runInContext("_dashCardFeature('gantt').active()", sandbox), false);

  setPRDs(sandbox, [PRD_BETA_OLD_ONLY, PRD_ALPHA]);
  assert.equal(vm.runInContext("_dashCardFeature('gantt').active()", sandbox), true);
});

test('the SVG groups bars under their Automaton and draws each bar at its timestamp', () => {
  const svg = svgStub(1200);
  const sandbox = loadAppSandbox(svg);
  // Timestamps relative to the real clock, since _drawGantt reads Date.now().
  const now = Date.now();
  const prd = { id: 'prd-alpha', title: 'Alpha Automaton', status: 'running', stories: [{ id: 's1', tasks: [
    { id: 'done', title: 'Done task', status: 'completed', started_at: iso(now - 2 * HOUR), completed_at: iso(now - 1 * HOUR) },
  ] }] };
  setPRDs(sandbox, [prd]);
  vm.runInContext('_drawGantt()', sandbox);

  assert.match(svg.innerHTML, /Alpha Automaton/, 'Automaton heading drawn');
  assert.match(svg.innerHTML, /<title>Done task<\/title>/, 'task bar drawn');
  assert.doesNotMatch(svg.innerHTML, /NaN|undefined/);

  // Left edge of the bar: 164px label gutter + (4h / 6h) of the 772px bar area.
  const expectedX = 164 + (4 / 6) * 772;
  const m = svg.innerHTML.match(/<rect x="([\d.]+)"[^>]*><title>Done task<\/title>/);
  assert.ok(m, 'bar rect found');
  assert.ok(Math.abs(Number(m[1]) - expectedX) <= 1, `bar x ${m[1]} near ${expectedX.toFixed(1)}`);
});

test('the SVG width is capped at 960px on a wide card', () => {
  const svg = svgStub(2400);
  const sandbox = loadAppSandbox(svg);
  const now = Date.now();
  setPRDs(sandbox, [{ id: 'p', title: 'Wide', stories: [{ tasks: [
    { id: 'w', title: 'Wide task', status: 'in_progress', started_at: iso(now - HOUR) },
  ] }] }]);
  vm.runInContext('_drawGantt()', sandbox);
  assert.equal(svg.attrs.width, '960');
  assert.match(svg.attrs.viewBox, /^0 0 960 /);
});

test('with no bars the SVG shows the one-line reason and no bar', () => {
  const svg = svgStub(1000);
  const sandbox = loadAppSandbox(svg);
  setPRDs(sandbox, []);
  vm.runInContext('_drawGantt()', sandbox);
  assert.match(svg.innerHTML, /dash_card_off_gantt_reason|Automaton task has run/);
  assert.doesNotMatch(svg.innerHTML, /<title>/);
});
