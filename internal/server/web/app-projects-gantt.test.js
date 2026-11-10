// Projects page — Gantt of the project from the history endpoint rows.
//
// The project page draws a Gantt from the gantt rows of
// GET /api/projects/{name}/history: one bar per Automaton and one per task.
// A window selector (7, 30 or 90 days) keeps the bars that overlap the window;
// a running bar runs to now. At most 40 rows are drawn, newest Automata first,
// and a note says how many were left out. Bars use the dashboard Timeline's
// state colours, through the shared _ganttStateColour helper.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const DAY = 86400000;

function iso(ms) { return new Date(ms).toISOString(); }

// load builds the app with the Projects tab active and English strings. The
// history reply (when given) answers every history request.
function load({ historyBody = null } = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { view } });
  sandbox.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/history')) return ok(historyBody || { items: [], gantt: [] });
    if (u.includes('/rollup')) return ok({});
    if (u.includes('/api/projects')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    return ok([]);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'projects'", sandbox);
  return { sb: sandbox, view };
}

// row builders for history gantt rows, placed relative to now.
function auto(id, title, startAgo, endAgo, state = 'completed') {
  const now = Date.now();
  return { kind: 'automaton', automaton_id: id, automation_id: 'arc-a', title, state,
    start: startAgo == null ? null : iso(now - startAgo * DAY),
    end: endAgo == null ? null : iso(now - endAgo * DAY) };
}
function task(autoId, taskId, title, startAgo, endAgo, state = 'completed') {
  const now = Date.now();
  return { kind: 'task', automaton_id: autoId, task_id: taskId, title, state,
    start: startAgo == null ? null : iso(now - startAgo * DAY),
    end: endAgo == null ? null : iso(now - endAgo * DAY) };
}

test('the window keeps bars that overlap it and drops older ones', () => {
  const { sb } = load();
  const now = Date.now();
  const gantt = [
    auto('old', 'Old work', 60, 50),        // outside 30 days
    auto('recent', 'Recent work', 10, 8),   // inside 30 days, outside 7
    auto('spanning', 'Spanning', 40, 2),    // starts before the window, ends inside
  ];
  // Newest first: the bar that starts latest leads.
  // Array.from copies out of the sandbox realm so deepEqual compares plain arrays.
  const ids = rows => Array.from(rows.items, x => x.row.automaton_id);
  const rows30 = sb._projectsGanttRows(gantt, '30d', now);
  assert.deepEqual(ids(rows30), ['recent', 'spanning']);
  const rows7 = sb._projectsGanttRows(gantt, '7d', now);
  assert.deepEqual(ids(rows7), ['spanning'], 'a bar that ends inside the window stays; one that ended before it is dropped');
  const rows90 = sb._projectsGanttRows(gantt, '90d', now);
  assert.equal(rows90.total, 3);
});

test('a running bar with no end runs to now and stays in the window', () => {
  const { sb } = load();
  const now = Date.now();
  const sp = sb._projectsGanttSpan(auto('run', 'Running', 2, null, 'running'), now);
  assert.equal(sp.running, true);
  assert.equal(sp.b, now);
  assert.ok(sp.a < sp.b);
  assert.equal(sb._projectsGanttSpan({ kind: 'task', state: 'planned' }, now), null, 'no timestamps means no bar');
  const rows = sb._projectsGanttRows([auto('run', 'Running', 2, null, 'running')], '7d', now);
  assert.equal(rows.items.length, 1);
});

test('rows are bounded and the newest Automata come first with their tasks under them', () => {
  const { sb } = load();
  const now = Date.now();
  const gantt = [];
  // 30 Automata, each with 2 tasks: 90 rows in the window.
  // a0 is the newest (started 1 day ago); a29 the oldest (15.5 days ago).
  for (let i = 0; i < 30; i++) {
    const s = 1 + i * 0.5;
    gantt.push(auto('a' + i, 'Automaton ' + i, s, s - 0.5));
    gantt.push(task('a' + i, 't' + i + 'x', 'Task x', s, s - 0.2));
    gantt.push(task('a' + i, 't' + i + 'y', 'Task y', s - 0.3, s - 0.5));
  }
  const rows = sb._projectsGanttRows(gantt, '30d', now);
  assert.equal(rows.total, 90, 'the total counts every row in the window');
  assert.equal(rows.items.length, 40, 'at most 40 rows are drawn');
  assert.equal(rows.items[0].row.automaton_id, 'a0', 'newest Automaton first');
  assert.equal(rows.items[1].row.kind, 'task', 'its tasks follow it');
  assert.equal(rows.items[1].row.automaton_id, 'a0');
  // 40 rows = 13 whole Automata of 3 rows each, plus the first row of the 14th.
  const drawnAutomata = rows.items.filter(x => x.row.kind !== 'task').length;
  assert.equal(drawnAutomata, 14, 'Automata are drawn whole until the bound');
});

test('the page shows a bar per row, coloured by state, with a window selector', () => {
  const { sb } = load();
  const hist = { gantt: [
    auto('a1', 'Login', 5, 4, 'completed'),
    task('a1', 't1', 'Build form', 5, 4.5, 'completed'),
    auto('a2', 'Checkout', 3, null, 'running'),
    task('a2', 't2', 'Payments', 3, 1, 'blocked'),
  ], items: [], next: '' };
  const html = sb._projectGanttHTML(hist);
  assert.match(html, /id="projectGantt"/);
  assert.match(html, /<select id="projectGanttWindow" onchange="projectsGanttWindow\(this\.value\)"/);
  assert.match(html, /<option value="30d" selected>Last 30 days<\/option>/);
  assert.match(html, /<option value="7d">Last 7 days<\/option>/);
  assert.match(html, /<option value="90d">Last 90 days<\/option>/);
  assert.equal((html.match(/<rect x="[^"]*" y="[^"]*" width="[^"]*" height="12"/g) || []).length, 4, 'one bar per row');
  assert.match(html, /fill="#10b981"/, 'completed is green');
  assert.match(html, /fill="#ef4444"/, 'blocked is red');
  assert.match(html, /opacity="0.55"/, 'a running bar is drawn lighter');
  assert.match(html, /<title>Checkout · running · [^<]*now<\/title>/, 'a running bar says so in its tooltip');
});

test('the Gantt names how many rows were left out when the bound applies', () => {
  const { sb } = load();
  const gantt = [];
  for (let i = 0; i < 30; i++) {
    gantt.push(auto('a' + i, 'A' + i, 20 - i * 0.5, 19 - i * 0.5));
    gantt.push(task('a' + i, 't' + i, 'T', 20 - i * 0.5, 19 - i * 0.5));
  }
  const html = sb._projectGanttHTML({ gantt, items: [], next: '' });
  assert.match(html, /Showing 40 of 60 rows; the newest are drawn first\./);
  assert.equal((html.match(/height="12"/g) || []).length, 40);
});

test('an empty window and a loading page say so', () => {
  const { sb } = load();
  assert.match(sb._projectGanttHTML(null), /Loading timeline…/);
  assert.match(sb._projectGanttHTML({ gantt: [auto('old', 'Old', 200, 190)], items: [] }), /No Automata or tasks in this window\./);
  assert.match(sb._projectGanttHTML({ gantt: [], items: [] }), /No Automata or tasks in this window\./);
});

test('titles from the server are escaped, not drawn as markup', () => {
  const { sb } = load();
  const html = sb._projectGanttHTML({ gantt: [auto('x', 'Ship <script>alert(1)</script>', 2, 1)], items: [] });
  assert.doesNotMatch(html, /<script>alert/);
  assert.match(html, /Ship &lt;script&gt;/);
});

test('the window selector switches the window and rejects an unknown key', () => {
  const { sb } = load();
  sb.projectsGanttWindow('90d');
  assert.equal(vm.runInContext('_projectsState.ganttWindow', sb), '90d');
  sb.projectsGanttWindow('bogus');
  assert.equal(vm.runInContext('_projectsState.ganttWindow', sb), '30d');
});

test('the Gantt state colours are shared with the dashboard Timeline', () => {
  const { sb } = load();
  assert.equal(sb._ganttStateColour('completed', '#abc', '#def'), '#10b981');
  assert.equal(sb._ganttStateColour('running', '#abc', '#def'), '#abc');
  assert.equal(sb._ganttStateColour('in_progress', '#abc', '#def'), '#abc');
  assert.equal(sb._ganttStateColour('failed', '#abc', '#def'), '#ef4444');
  assert.equal(sb._ganttStateColour('archived', '#abc', '#def'), '#def', 'unknown states take the muted colour');
});

test('the history load keeps the Gantt rows from the first page', async () => {
  const gantt = [auto('a1', 'Login', 5, 4)];
  const { sb } = load({ historyBody: { items: [], gantt, next_cursor: '' } });
  vm.runInContext("_projectsState.selected = 'acme'; _projectsState.list = [{ name: 'acme' }]", sb);
  await sb._projectsLoadHistory(false);
  await flushAsync();
  const hist = vm.runInContext('_projectsState.hist', sb);
  assert.equal(hist.gantt.length, 1);
  assert.equal(hist.gantt[0].automaton_id, 'a1');
});
