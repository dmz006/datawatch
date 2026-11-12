// Projects page — the expandable Gantt: ranges, zoom and pan, expand and
// collapse, paging, the size cap, running work and dropped late replies.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const DAY = 86400000;
function iso(ms) { return new Date(ms).toISOString(); }
const NOW = Date.now();
const ago = d => iso(NOW - d * DAY);

// load builds the app. `handler(url)` returns the reply body for a timeline
// request (or a Promise); every timeline URL is recorded in `calls`.
function load(handler, { store } = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  const calls = [];
  sandbox.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/timeline')) { calls.push(u); return Promise.resolve(handler(u)).then(ok); }
    if (u.includes('/history')) return ok({ items: [] });
    if (u.includes('/rollup')) return ok({});
    if (u.includes('/api/projects')) return ok([{ name: 'acme' }, { name: 'beta' }]);
    return ok([]);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'; _projectsState.selected = 'acme'; _projectsState.list = [{ name: 'acme' }, { name: 'beta' }]", sandbox);
  if (store) Object.keys(store).forEach(k => sandbox.localStorage.setItem(k, store[k]));
  return { sb: sandbox, calls };
}

const arcRow = (id, f, l, o = {}) => Object.assign({ kind: 'arc', id, title: 'Arc ' + id, state: 'active', first: f == null ? null : ago(f), last: l == null ? null : ago(l), running: false, has_children: f != null, children_count: 2 }, o);
const autoRow = (id, f, l, o = {}) => Object.assign({ kind: 'automaton', id, title: 'Auto ' + id, state: 'completed', first: ago(f), last: l == null ? null : ago(l), running: l == null, has_children: true, children_count: 2 }, o);
const taskRow = (id, f, l) => ({ kind: 'task', id, title: 'Task ' + id, state: 'completed', first: ago(f), last: ago(l), running: false, has_children: false, children_count: 0 });

function standard(u) {
  if (u.includes('level=arcs')) return { rows: [arcRow('a1', 30, 2), arcRow('a2', null, null), { kind: 'unassigned', id: 'none', title: 'Unassigned', state: 'planned', first: ago(5), last: ago(1), running: false, has_children: true, children_count: 1 }] };
  if (u.includes('level=automata&parent=a1')) return { rows: [autoRow('m1', 30, 10), autoRow('m2', 9, null)] };
  if (u.includes('level=automata&parent=none')) return { rows: [autoRow('u1', 5, 1)] };
  if (u.includes('level=tasks&automaton=m1')) return { rows: [taskRow('t1', 30, 20), taskRow('t2', 20, 10)] };
  return { rows: [] };
}

const urlsWith = (calls, s) => calls.filter(u => u.includes(s));

test('pure helpers: range from rows, presets and default', () => {
  const { sb } = load(standard);
  const rows = [arcRow('a', 30, 2), arcRow('b', 10, null, { running: true }), arcRow('c', null, null)];
  const r = sb._projectsGanttRangeFromRows(rows, NOW);
  assert.equal(r.start, Date.parse(ago(30)));
  assert.equal(r.end, NOW, 'running work counts to now');
  assert.equal(sb._projectsGanttRangeFromRows([arcRow('c', null, null)], NOW), null);
  const all = sb._projectsGanttPresetRange('all', rows, NOW);
  assert.equal(all.start, r.start);
  for (const [k, d] of [['90d', 90], ['30d', 30], ['7d', 7]]) {
    const p = sb._projectsGanttPresetRange(k, rows, NOW);
    assert.equal(p.end, NOW);
    assert.equal(p.end - p.start, d * DAY, k);
  }
  assert.equal(sb._projectsGanttPresetRange('bogus', rows, NOW).start, r.start, 'an unknown key falls back to All');
  const empty = sb._projectsGanttPresetRange('all', [], NOW);
  assert.equal(empty.end - empty.start, 30 * DAY);
});

test('pure helpers: zoom and pan clamp to the bounds', () => {
  const { sb } = load(standard);
  const rows = [arcRow('a', 30, 2)];
  const b = sb._projectsGanttBounds(rows, NOW);
  assert.ok(b.end - b.start >= 90 * DAY);
  const cur = sb._projectsGanttPresetRange('30d', rows, NOW);
  const zin = sb._projectsGanttZoom(cur, 0.5, 0.5, b);
  assert.equal(Math.round((zin.end - zin.start) / DAY * 100) / 100, 15);
  assert.equal(Math.round(((zin.start + zin.end) / 2 - (cur.start + cur.end) / 2)), 0, 'the anchor stays put');
  const tiny = sb._projectsGanttZoom(cur, 1e-9, 0.5, b);
  assert.equal(tiny.end - tiny.start, 3600000, 'zoom in stops at one hour');
  const huge = sb._projectsGanttZoom(cur, 1e9, 0.5, b);
  assert.equal(huge.start, b.start);
  assert.equal(huge.end, b.end, 'zoom out stops at the bounds');
  const panned = sb._projectsGanttPan(cur, 1000 * DAY, b);
  assert.equal(panned.end, b.end, 'pan stops at the right edge');
  assert.equal(panned.end - panned.start, cur.end - cur.start, 'pan keeps the span');
  const left = sb._projectsGanttPan(cur, -1000 * DAY, b);
  assert.equal(left.start, b.start);
});

test('wheel zoom and drag pan change the printed range', async () => {
  const { sb } = load(standard);
  await sb._projectsLoadGantt();
  await flushAsync();
  const g = vm.runInContext('_projectsGanttGet()', sb);
  const before = sb._projectsGanttCurrent(g, Date.now());
  sb.projectsGanttWheel({ deltaY: -100, clientX: 0, preventDefault() {} });
  const zoomed = vm.runInContext('_projectsGanttGet().view', sb);
  assert.ok(zoomed.end - zoomed.start < before.end - before.start, 'wheel up zooms in');
  assert.match(sb._projectGanttHTML(), /aria-pressed="false" onclick="projectsGanttPreset\('all'\)"/, 'no preset is active after zoom');
  sb.projectsGanttPreset('7d');
  assert.equal(vm.runInContext('_projectsGanttGet().view', sb), null);
  assert.match(sb._projectGanttHTML(), /data-preset="7d" aria-pressed="true"/);
});

test('an arc with no activity is planned with no bar', async () => {
  const { sb } = load(standard);
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  const row = html.split('data-key="arc:a2"')[1].split('data-key=')[0];
  assert.match(row, /Planned/);
  assert.doesNotMatch(row, /project-gantt-bar/);
  assert.match(html, /Automata with no arc/, 'the unassigned row is the no-arc group');
});

test('expand fetches children once; collapse hides without refetching; state is remembered', async () => {
  const { sb, calls } = load(standard);
  await sb._projectsLoadGantt();
  await flushAsync();
  assert.equal(urlsWith(calls, 'level=automata').length, 0, 'no child fetch before an expand');
  await sb.projectsGanttToggle('arc:a1');
  await flushAsync();
  assert.equal(urlsWith(calls, 'level=automata&parent=a1').length, 1);
  assert.match(sb._projectGanttHTML(), /Auto m1/);
  assert.match(sb._projectGanttHTML(), /aria-label="Collapse Arc a1"/);
  assert.equal(urlsWith(calls, 'level=tasks').length, 0);
  await sb.projectsGanttToggle('auto:m1');
  await flushAsync();
  assert.equal(urlsWith(calls, 'level=tasks&automaton=m1').length, 1);
  assert.match(sb._projectGanttHTML(), /Task t1/);
  await sb.projectsGanttToggle('arc:a1'); // collapse
  assert.doesNotMatch(sb._projectGanttHTML(), /Auto m1/);
  assert.match(sb._projectGanttHTML(), /aria-label="Expand Arc a1"/);
  await sb.projectsGanttToggle('arc:a1'); // expand again
  assert.equal(urlsWith(calls, 'level=automata&parent=a1').length, 1, 'collapse then expand does not refetch');
  assert.match(sb._projectGanttHTML(), /Auto m1/);
  const stored = JSON.parse(sb.localStorage.getItem('cs_projects_gantt_expanded_acme'));
  assert.deepEqual(stored.sort(), ['arc:a1', 'auto:m1']);
  // Reload: a fresh load restores the remembered expansion.
  const again = load(standard, { store: { cs_projects_gantt_expanded_acme: JSON.stringify(stored) } });
  await again.sb._projectsLoadGantt();
  await flushAsync();
  await flushAsync();
  const html = again.sb._projectGanttHTML();
  assert.match(html, /Auto m1/);
  assert.match(html, /Task t1/);
  assert.equal(urlsWith(again.calls, 'level=tasks&automaton=m1').length, 1);
});

test('the no-arc group expands to its Automata', async () => {
  const { sb, calls } = load(standard);
  await sb._projectsLoadGantt();
  await sb.projectsGanttToggle('none');
  await flushAsync();
  assert.equal(urlsWith(calls, 'level=automata&parent=none').length, 1);
  assert.match(sb._projectGanttHTML(), /Auto u1/);
});

test('a broken store reads as nothing expanded and does not throw', async () => {
  const { sb } = load(standard, { store: { cs_projects_gantt_expanded_acme: '{not json' } });
  await sb._projectsLoadGantt();
  await flushAsync();
  assert.doesNotMatch(sb._projectGanttHTML(), /Auto m1/);
});

test('a long project pages with next_cursor', async () => {
  const handler = u => {
    if (u.includes('level=arcs')) return { rows: [arcRow('a1', 30, 2)] };
    if (u.includes('cursor=c2')) return { rows: [autoRow('m3', 5, 4)] };
    if (u.includes('level=automata&parent=a1')) return { rows: [autoRow('m1', 30, 10), autoRow('m2', 9, 8)], next_cursor: 'c2' };
    return { rows: [] };
  };
  const { sb, calls } = load(handler);
  await sb._projectsLoadGantt();
  await sb.projectsGanttToggle('arc:a1');
  await flushAsync();
  let html = sb._projectGanttHTML();
  assert.match(html, /Load more/);
  assert.doesNotMatch(html, /Auto m3/);
  await sb.projectsGanttMore('arc:a1');
  await flushAsync();
  html = sb._projectGanttHTML();
  assert.match(html, /Auto m3/);
  assert.match(html, /Auto m1/, 'earlier rows stay');
  assert.doesNotMatch(html, /Load more/, 'no cursor, no button');
  assert.equal(urlsWith(calls, 'cursor=c2').length, 1);
});

test('the size cap keeps 40 rows and says how many there are', async () => {
  const rows = [];
  for (let i = 0; i < 55; i++) rows.push(arcRow('r' + i, 30, 2, { has_children: false }));
  const { sb } = load(() => ({ rows }));
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  assert.equal(vm.runInContext('_PROJECTS_GANTT_MAX_ROWS', sb), 40);
  assert.equal((html.match(/data-key="arc:/g) || []).length, 40);
  assert.match(html, /Showing 40 of 55 rows\./);
});

test('running work draws to now and reads now', async () => {
  const handler = u => u.includes('level=arcs')
    ? { rows: [arcRow('a1', 30, null, { running: true, last: null })] }
    : { rows: [autoRow('m2', 9, null)] };
  const { sb } = load(handler);
  await sb._projectsLoadGantt();
  await sb.projectsGanttToggle('arc:a1');
  await flushAsync();
  const html = sb._projectGanttHTML();
  assert.equal((html.match(/project-gantt-now/g) || []).length, 2, 'the arc and its running Automaton read now');
  assert.match(html, /class="project-gantt-bar running"/);
});

test('a reply for a project no longer selected is dropped', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const handler = u => gate.then(() => ({ rows: [arcRow('late', 5, 1)] }));
  const { sb } = load(handler);
  const p = sb._projectsLoadGantt();
  vm.runInContext("_projectsState.selected = 'beta'", sb);
  release();
  await p;
  await flushAsync();
  const g = vm.runInContext('_projectsState.gantt', sb);
  assert.equal(g.rows.length, 0, 'the late arcs reply was not stored');
  assert.equal(vm.runInContext('_projectsGanttGet()', sb), null);
});

test('leaving the tab leaves no listeners behind after a drag', async () => {
  const { sb } = load(standard);
  await sb._projectsLoadGantt();
  await flushAsync();
  const added = [], removed = [];
  sb.document.addEventListener = (n, f) => added.push([n, f]);
  sb.document.removeEventListener = (n, f) => removed.push([n, f]);
  sb.document.getElementById = id => (id === 'projectGanttAxis' ? { getBoundingClientRect: () => ({ left: 0, width: 400 }) } : null);
  sb.projectsGanttDown({ button: 0, clientX: 100, target: {} });
  assert.equal(added.length, 3);
  vm.runInContext("_automataState.tab = 'sessions'", sb);
  added[0][1]({ clientX: 50 }); // a move after leaving the tab stops the drag
  assert.equal(removed.length, 3, 'all three listeners are removed');
  assert.equal(vm.runInContext('_projectsGanttDrag', sb), null);
});

test('the Gantt code never kills or signals a session and keeps the layout rules', () => {
  const src = fs.readFileSync(APP, 'utf8');
  const sec = src.slice(src.indexOf('Project Gantt: one expandable'), src.indexOf('function projectsHistoryMore'));
  assert.doesNotMatch(sec, /kill|signal/i);
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.match(css, /\.project-gantt-scroll \{[^}]*overflow-x: auto/);
  assert.match(css, /\.project-gantt-label \{[^}]*position: sticky; left: 0/);
  assert.match(css, /max-width: 720px/);
});
