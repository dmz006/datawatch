// Projects page — Gantt defaults and drawing.
//
// The Gantt is one expandable timeline fed by GET /api/projects/{name}/timeline.
// It opens at the whole project with the range "All time": one row per arc, an
// arc with no activity shown as planned with no bar, and the presets All, 90,
// 30 and 7 days. Bars use the dashboard Timeline's state colours through the
// shared _ganttStateColour helper.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const DAY = 86400000;
function iso(ms) { return new Date(ms).toISOString(); }

function load({ timeline = null } = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  sandbox.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/timeline')) return ok(timeline || { rows: [] });
    if (u.includes('/history')) return ok({ items: [] });
    if (u.includes('/rollup')) return ok({});
    if (u.includes('/api/projects')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    return ok([]);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'; _projectsState.selected = 'acme'; _projectsState.list = [{ name: 'acme' }]", sandbox);
  return { sb: sandbox };
}

function arc(id, title, firstAgo, lastAgo, o = {}) {
  const now = Date.now();
  return Object.assign({ kind: 'arc', id, title, state: 'active',
    first: firstAgo == null ? null : iso(now - firstAgo * DAY),
    last: lastAgo == null ? null : iso(now - lastAgo * DAY),
    running: false, has_children: firstAgo != null, children_count: 1 }, o);
}

test('the default range is All time and the presets are All, 90, 30 and 7 days', async () => {
  const { sb } = load({ timeline: { rows: [arc('a1', 'Login', 20, 5)] } });
  assert.equal(vm.runInContext('_PROJECTS_GANTT_DEFAULT_WINDOW', sb), 'all');
  assert.deepEqual(Array.from(vm.runInContext('_PROJECTS_GANTT_PRESETS.map(p => p[0])', sb)), ['all', '90d', '30d', '7d']);
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  assert.match(html, /id="projectGantt"/);
  assert.match(html, /data-preset="all" aria-pressed="true"/);
  assert.match(html, />All time<\/button>/);
  assert.match(html, />90 days<\/button>/);
  assert.match(html, />30 days<\/button>/);
  assert.match(html, />7 days<\/button>/);
  assert.match(html, /id="projectGanttRange">[^<]* – [^<]*</, 'the visible range is printed as text');
});

test('each arc is a row with a bar coloured by state; running reads now', async () => {
  const { sb } = load({ timeline: { rows: [
    arc('a1', 'Login', 20, 5, { state: 'done' }),
    arc('a2', 'Checkout', 10, null, { running: true }),
  ] } });
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  assert.equal((html.match(/class="project-gantt-bar/g) || []).length, 2);
  assert.match(html, /background:#10b981/, 'done is green');
  assert.match(html, /project-gantt-now[^>]*>now</, 'running work reads now');
  assert.match(html, /title="Checkout · active · [^"]*now"/);
});

test('the Gantt state colours are shared with the dashboard Timeline', () => {
  const { sb } = load();
  assert.equal(sb._ganttStateColour('completed', '#abc', '#def'), '#10b981');
  assert.equal(sb._ganttStateColour('running', '#abc', '#def'), '#abc');
  assert.equal(sb._ganttStateColour('failed', '#abc', '#def'), '#ef4444');
  assert.equal(sb._ganttStateColour('archived', '#abc', '#def'), '#def');
});

test('titles from the server are escaped, not drawn as markup', async () => {
  const { sb } = load({ timeline: { rows: [arc('x', 'Ship <script>alert(1)</script>', 2, 1)] } });
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  assert.doesNotMatch(html, /<script>alert/);
  assert.match(html, /Ship &lt;script&gt;/);
});

test('loading, failed and empty states say so', async () => {
  const { sb } = load();
  assert.match(sb._projectGanttHTML(), /Loading timeline…/);
  await sb._projectsLoadGantt();
  await flushAsync();
  assert.match(sb._projectGanttHTML(), /No arcs or Automata with activity yet\./);
});

test('the history load no longer carries Gantt rows', async () => {
  const { sb } = load();
  await sb._projectsLoadHistory(false);
  await flushAsync();
  assert.equal('gantt' in vm.runInContext('_projectsState.hist', sb), false);
});

test('the old window selector is gone', () => {
  const src = fs.readFileSync(APP, 'utf8');
  assert.doesNotMatch(src, /_PROJECTS_GANTT_WINDOWS|projectsGanttWindow|projectGanttWindow/);
});

test('span uses start/end, sort helper orders completed, running, then dependencies', () => {
  const { sb } = load(() => ({ rows: [] }));
  const NOW2 = Date.now();
  const iso2 = ms => new Date(ms).toISOString();
  const sp = sb._projectsGanttSpan({ start: iso2(NOW2 - 5000), end: null, status: 'running' }, NOW2);
  assert.equal(sp.b, NOW2);
  assert.equal(sp.running, true);
  const rows = [
    { id: 'r', status: 'running', start: iso2(NOW2 - 1000), end: null },
    { id: 'c2', status: 'completed', start: iso2(NOW2 - 9000), end: iso2(NOW2 - 4000) },
    { id: 'c1', status: 'completed', start: iso2(NOW2 - 9500), end: iso2(NOW2 - 6000) },
    { id: 'w', status: 'waiting', start: iso2(NOW2 - 2000), end: null, depends_on: ['r'] },
  ];
  assert.equal(sb._projectsGanttSortChildren(rows, []).map(x => x.id).join(','), 'c1,c2,r,w');
  assert.equal(sb._projectsGanttSortChildren(rows, [{ from: 'w', to: 'c1' }]).map(x => x.id).join(','), 'r,w,c1,c2', 'an edge puts a row after what it follows');
  assert.equal(rows[0].id, 'r', 'input is not changed');
});

// ---- accuracy: spans, tight axis, status wording ----

test('an arc span is its members min start to max end, never the whole window', () => {
  const { sb } = load({ timeline: { rows: [] } });
  const N = Date.now();
  const a = { kind: 'arc', id: 'a', start: iso(N - 90 * DAY), end: iso(N - 30 * DAY), status: 'completed' };
  const b = { kind: 'arc', id: 'b', start: iso(N - 10 * DAY), end: iso(N - 5 * DAY), status: 'completed' };
  const sa = sb._projectsGanttSpan(a, N), sbb = sb._projectsGanttSpan(b, N);
  assert.equal(sa.a, Date.parse(a.start));
  assert.equal(sa.b, Date.parse(a.end));
  assert.equal(sbb.a, Date.parse(b.start));
  assert.equal(sbb.b, Date.parse(b.end));
  assert.ok(sa.b < sbb.a, 'one arc does not stretch over the other');
});

test('the axis is tight: the rows span plus 2 percent margin at each end', () => {
  const { sb } = load({ timeline: { rows: [] } });
  const N = Date.now();
  const rows = [
    { kind: 'task', id: 't1', start: iso(N - 50 * DAY), end: iso(N - 40 * DAY), status: 'completed' },
    { kind: 'task', id: 't2', start: iso(N - 30 * DAY), end: iso(N - 20 * DAY), status: 'completed' },
  ];
  const s = Date.parse(rows[0].start), e = Date.parse(rows[1].end);
  const m = (e - s) * 0.02;
  const r = sb._projectsGanttRangeFromRows(rows, N);
  assert.ok(Math.abs(r.start - (s - m)) < 1, 'start = first start minus 2 percent');
  assert.ok(Math.abs(r.end - (e + m)) < 1, 'end = last end plus 2 percent, not now');
  const tight = sb._projectsGanttTightRange(rows, N);
  assert.equal(tight.start, s);
  assert.equal(tight.end, e);
});

test('a running row is drawn to now', () => {
  const { sb } = load({ timeline: { rows: [] } });
  const N = Date.now();
  const sp = sb._projectsGanttSpan({ kind: 'automaton', id: 'r', start: iso(N - 3 * DAY), end: null, status: 'running' }, N);
  assert.equal(sp.b, N);
  assert.equal(sp.running, true);
  const range = sb._projectsGanttRangeFromRows([{ kind: 'automaton', id: 'r', start: iso(N - 3 * DAY), end: null, status: 'running' }], N);
  assert.ok(range.end > N, 'the margin sits after now');
});

test('bars say Completed; the merged release shows only in the hover detail', async () => {
  const N = Date.now();
  const row = (id, release) => ({ kind: 'arc', id, title: 'Arc ' + id, state: 'done', status: 'completed', release,
    first: iso(N - 20 * DAY), last: iso(N - 5 * DAY), start: iso(N - 20 * DAY), end: iso(N - 5 * DAY),
    running: false, has_children: false, children_count: 0 });
  const { sb } = load({ timeline: { rows: [row('shipped', 'v9.9.0'), row('plain', '')] } });
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  const titles = Array.from(html.matchAll(/class="project-gantt-bar[^"]*"[^>]*title="([^"]*)"/g)).map(m => m[1]);
  assert.equal(titles.length, 2);
  assert.match(titles[0], /· Completed ·/);
  assert.match(titles[0], /Release v9\.9\.0/);
  assert.match(titles[1], /· Completed ·/);
  assert.doesNotMatch(titles[1], /Release/);
  const stripped = html.replace(/title="[^"]*"/g, '');
  assert.doesNotMatch(stripped, /v9\.9\.0/, 'the release is not in a label or legend, only the hover detail');
  assert.doesNotMatch(stripped, /\bDone\b|\bShipped\b/, 'no old wording on bar or legend');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.equal(en.projects_gantt_status_completed, 'Completed');
});
