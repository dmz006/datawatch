// Projects Gantt — dependency edges: pure geometry, toggle and accessibility.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
function load() {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sb = buildSandbox({ byId: { automataPanel: view, view } });
  vm.createContext(sb);
  loadScript(APP, sb);
  return sb;
}
const T = Date.parse('2026-11-01T00:00:00Z'), H = 3600000;
const row = (id, a, b, extra) => Object.assign({ type: 'row', row: Object.assign({ id, kind: 'task', start: new Date(T + a * H).toISOString(), end: new Date(T + b * H).toISOString(), status: 'completed' }, extra || {}) });
const range = { start: T, end: T + 100 * H };

test('edges: depends_on and edges list become elbow paths from end to start', () => {
  const sb = load();
  const items = [row('a', 0, 10), row('b', 20, 30, { depends_on: ['a'] }), row('c', 40, 50)];
  const out = Array.from(vm.runInContext('_projectsGanttEdges', sb)(items, [{ from: 'b', to: 'c' }], { range, now: T + 200 * H, mode: 'on' }));
  assert.equal(out.length, 2);
  assert.equal(out[0].from, 'a'); assert.equal(out[0].to, 'b');
  assert.equal(out[0].x1, 10); assert.equal(out[0].x2, 20);
  assert.equal(out[0].y1, 11); assert.equal(out[0].y2, 33);
  assert.match(out[0].d, /^M10 11H15V33H20$/);
  assert.equal(out[1].from, 'b'); assert.equal(out[1].to, 'c');
});

test('edges: off draws none, unknown or collapsed ends are skipped, all lifts the extra cap', () => {
  const sb = load();
  const f = vm.runInContext('_projectsGanttEdges', sb);
  const items = [row('a', 0, 10), row('b', 20, 30, { depends_on: ['a', 'missing'] })];
  assert.equal(f(items, [], { range, mode: 'off' }).length, 0);
  assert.equal(f(items, [], { range, now: T + 200 * H }).length, 1);
  const many = [row('a', 0, 10), row('b', 20, 30), row('c', 40, 50)];
  const e = [{ from: 'a', to: 'b' }, { from: 'b', to: 'c' }];
  assert.deepEqual(Array.from(f(many, e, { range, max: 1, mode: 'on' })).map(x => x.extra), [false, true]);
  assert.deepEqual(Array.from(f(many, e, { range, max: 1, mode: 'all' })).map(x => x.extra), [false, false]);
});

test('edges: markup is aria-hidden, toggle has aria-pressed, choice survives a blocked store', () => {
  const sb = load();
  const src = fs.readFileSync(APP, 'utf8');
  assert.match(src, /aria-hidden="true" focusable="false" viewBox/);
  assert.match(src, /projects_gantt_edges'\)\)\}<\/button>/);
  assert.equal((src.match(/edgeMode: 'projectsGanttEdgeMode'/g) || []).length, 2, 'both Gantt surfaces carry the toggle');
  sb.localStorage = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); } };
  assert.equal(vm.runInContext('_projectsGanttEdgeMode()', sb), 'on');
  vm.runInContext("projectsGanttEdgeMode('toggle')", sb);
  assert.equal(vm.runInContext('_projectsGanttEdgeMode()', sb), 'off');
});

test('edges: locale keys exist in every language', () => {
  const keys = ['projects_gantt_edges', 'projects_gantt_edges_all', 'projects_gantt_edges_off', 'projects_gantt_status_completed', 'projects_gantt_status_running', 'projects_gantt_status_waiting', 'projects_gantt_status_blocked', 'projects_gantt_status_failed', 'projects_gantt_status_cancelled', 'projects_gantt_release_detail'];
  ['en', 'de', 'es', 'fr', 'ja'].forEach(l => {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    keys.forEach(k => assert.ok(j[k] && !/\b[0-9a-f]{8}\b/.test(j[k]), l + ' ' + k));
  });
});

test('edges: a task dependency and a chain step between Automata are each drawn', () => {
  const sb = load();
  const f = vm.runInContext('_projectsGanttEdges', sb);
  const auto = (id, a, b) => ({ type: 'row', row: { id, kind: 'automaton', status: 'completed', start: new Date(T + a * H).toISOString(), end: new Date(T + b * H).toISOString() } });
  const tasks = [row('t1', 0, 10), row('t2', 20, 30, { depends_on: ['t1'] })];
  const dep = Array.from(f(tasks, [], { range, now: T + 200 * H }));
  assert.equal(dep.length, 1);
  assert.deepEqual([dep[0].from, dep[0].to], ['t1', 't2']);
  const chain = Array.from(f([auto('p1', 0, 10), auto('p2', 20, 30)], [{ from: 'p1', to: 'p2' }], { range, now: T + 200 * H }));
  assert.equal(chain.length, 1);
  assert.deepEqual([chain[0].from, chain[0].to], ['p1', 'p2']);
  assert.ok(chain[0].x1 < chain[0].x2, 'from the end of p1 to the start of p2');
});

test('edges: the toggle hides the edge layer in the drawn page', () => {
  const sb = load();
  const items = [row('a', 0, 10), row('b', 20, 30, { depends_on: ['a'] })];
  const g = { rows: [], kids: {}, expanded: {} };
  sb.__items = items; sb.__g = g; sb.__range = range;
  const draw = () => vm.runInContext('_projectsGanttEdgesSvg(__items, __g, __range, ' + (T + 200 * H) + ')', sb);
  assert.match(draw(), /project-gantt-edges/);
  vm.runInContext("_projectsGanttEdgeModeValue = 'off'", sb);
  assert.equal(draw(), '', 'edges hidden by the toggle');
});

test('edges: at 300 rows only expanded and focused rows get edges', () => {
  const sb = load();
  const collect = vm.runInContext('_projectsGanttCollectEdges', sb);
  const kids = {};
  for (let i = 0; i < 300; i++) kids['auto:p' + i] = { rows: [], edges: [{ from: 'x' + i, to: 'y' + i }] };
  const expanded = { 'auto:p7': true, 'auto:p250': true };
  const got = Array.from(collect({ edges: [], kids, expanded })).map(e => e.from).sort();
  assert.deepEqual(got, ['x250', 'x7'], 'collapsed rows contribute no edges');
  // Past the edge cap the rest are marked extra: drawn only for a focused end.
  const f = vm.runInContext('_projectsGanttEdges', sb);
  const items = [];
  for (let i = 0; i < 300; i++) items.push(row('r' + i, i, i + 0.5));
  const list = [];
  for (let i = 0; i < 299; i++) list.push({ from: 'r' + i, to: 'r' + (i + 1) });
  const out = Array.from(f(items, list, { range: { start: T, end: T + 400 * H }, now: T + 500 * H, mode: 'on' }));
  const cap = vm.runInContext('_PROJECTS_GANTT_EDGE_MAX', sb);
  assert.equal(out.filter(e => !e.extra).length, cap);
  assert.equal(out.filter(e => e.extra).length, 299 - cap);
});

test('ordering: by completion time, then topological with a tie broken by completion', () => {
  const sb = load();
  const sort = vm.runInContext('_projectsGanttSortChildren', sb);
  const ids = l => Array.from(l).map(r => r.id).join(',');
  const rows = [row('c', 12, 40), row('b', 2, 20, { depends_on: ['d'] }), row('d', 60, 80), row('a', 4, 30)];
  assert.equal(ids(sort(rows.filter(r => !r.row.depends_on).map(r => r.row), [])), 'a,c,d');
  const flat = rows.map(r => r.row);
  const out = ids(sort(flat, []));
  assert.ok(out.indexOf('d') < out.indexOf('b'), 'a dependency sits above its dependent');
  assert.ok(out.indexOf('a') < out.indexOf('c'), 'free rows keep completion order');
});
