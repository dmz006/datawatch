// Projects page and Automation detail — the focus view of one Automation:
// the Focus button, the first-to-last-task range, every Automaton and task,
// expand/collapse, presets and zoom, the empty Automation, Back, the failed
// read and the dropped late reply. The page only reads.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const DAY = 86400000;
const NOW = Date.now();
const iso = ms => new Date(ms).toISOString();
const ago = d => iso(NOW - d * DAY);

const autoRow = (id, f, l) => ({ kind: 'automaton', id, title: 'Auto ' + id, state: 'completed', first: ago(f), last: ago(l), running: false, has_children: true, children_count: 2 });
const taskRow = (id, f, l) => ({ kind: 'task', id, title: 'Task ' + id, state: 'completed', first: ago(f), last: ago(l), running: false, has_children: false, children_count: 0 });

// The focus reply: each Automaton followed by its tasks. The first task starts 30
// days ago and the last ends 2 days ago.
const REPLY = { rows: [
  autoRow('m1', 31, 12), taskRow('t1', 30, 20), taskRow('t2', 20, 12),
  autoRow('m2', 10, 1), taskRow('t3', 9, 5), taskRow('t4', 5, 2),
] };

// load builds the app. handler(url) returns a body, a Promise of a body, or
// { __status, __text } for an error reply. Every request is recorded.
function load(handler) {
  const cap = { panel: '', view: '', body: '' };
  const panel = makeStubElement(h => { cap.panel = h; });
  const view = makeStubElement(h => { cap.view = h; });
  const body = makeStubElement(h => { cap.body = h; });
  panel.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { automataPanel: panel, view, projectFocusBody: body } });
  const calls = [];
  const timers = { n: 0 };
  sandbox.setTimeout = () => { timers.n++; return 0; };
  sandbox.setInterval = () => { timers.n++; return 0; };
  sandbox.fetch = (url, opts) => {
    const u = String(url);
    const method = String((opts && opts.method) || 'GET').toUpperCase();
    const reply = b => {
      if (b && b.__status) return { ok: false, status: b.__status, statusText: 'err', json: () => Promise.resolve({}), text: () => Promise.resolve(b.__text || 'error') };
      return { ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) };
    };
    if (u.includes('/timeline')) { calls.push({ url: u, method }); return Promise.resolve(handler(u)).then(reply); }
    calls.push({ url: u, method });
    if (u.includes('/api/projects/') ) return Promise.resolve(reply({}));
    if (u.includes('/api/projects')) return Promise.resolve(reply([{ name: 'acme' }, { name: 'beta' }]));
    return Promise.resolve(reply([]));
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'; _projectsState.selected = 'acme'; _projectsState.list = [{ name: 'acme' }, { name: 'beta' }]", sandbox);
  timers.n = 0; // timers the app started while loading are not the focus page's
  return { sb: sandbox, calls, cap, timers };
}

const ok = u => (u.includes('focus=a1') ? REPLY : { rows: [] });
const focusState = sb => vm.runInContext('_projectsFocus', sb);
const bodyHtml = sb => vm.runInContext('_projectsFocusInner(_projectsFocus)', sb);

test('the Focus button renders on an arc row, an arc card and the Automation detail header', async () => {
  const { sb } = load(u => ({ rows: [
    { kind: 'arc', id: 'a1', title: 'Arc one', state: 'active', first: ago(30), last: ago(2), running: false, has_children: true, children_count: 2 },
    { kind: 'unassigned', id: 'none', title: 'x', state: 'planned', first: ago(5), last: ago(1), running: false, has_children: true, children_count: 1 },
  ] }));
  await sb._projectsLoadGantt();
  await flushAsync();
  const html = sb._projectGanttHTML();
  const arcRowHtml = html.split('data-key="arc:a1"')[1].split('data-key=')[0];
  assert.match(arcRowHtml, /project-gantt-focus[^>]*data-focus="a1"[^>]*>Focus</, 'arc row has Focus');
  const unRow = html.split('data-key="none"')[1] || '';
  assert.doesNotMatch(unRow, /project-gantt-focus/, 'the no-arc group has no Focus button');

  vm.runInContext("_projectsState.subtab = 'plans'", sb); // the plan cards sit on the Plans page sub-tab
  const card = vm.runInContext(`_projectPageHTML({ name: 'acme' }, { arcs: [{ automation_id: 'a1', state: 'active', priority: 1, tasks: { done: 1, total: 2 }, automata: [] }], unassigned: { automata: [] } }, '', null)`, sb);
  assert.match(card, /<summary[\s\S]*data-focus="a1"[\s\S]*<\/summary>/, 'arc card has Focus beside the Gantt');

  const detail = sb.buildAutomationDetailHTML({ id: 'au1', name: 'Release work', status: 'running', version: 3, members: [] }, {});
  assert.match(detail, /automation-focus-button[^>]*data-focus="au1"[^>]*>Focus</, 'detail header has Focus');
  assert.ok(detail.indexOf('automation-detail-back') < detail.indexOf('automation-focus-button'));
});

test('focus reads once with focus=<id>, the range is first task to last task', async () => {
  const { sb, calls, cap } = load(ok);
  await sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const tl = calls.filter(c => c.url.includes('/timeline'));
  assert.equal(tl.length, 1, 'one reply');
  assert.match(tl[0].url, /\/api\/projects\/acme\/timeline\?focus=a1$/);
  const f = focusState(sb);
  const r = sb._projectsGanttCurrent(f, NOW);
  const mg = (Date.parse(ago(2)) - Date.parse(ago(30))) * 0.02;
  assert.equal(r.start, Date.parse(ago(30)) - mg, 'starts at the first task minus the 2 percent margin');
  assert.equal(r.end, Date.parse(ago(2)) + mg, 'ends at the last task plus the margin');
  assert.match(cap.panel, /Focus: Arc one/);
  // The label prints the range the page uses (first task minus the margin to last
  // task plus the margin), not a hand-picked instant — ago(31) can be the previous
  // calendar day from r.start depending on time zone and time of day.
  const dateRe = ts => new Date(ts).toLocaleDateString().replace(/[/.]/g, '\\$&');
  assert.match(bodyHtml(sb), new RegExp('First task ' + dateRe(r.start) + ' – last task ' + dateRe(r.end)));
  assert.ok(tl.every(c => c.method === 'GET'), 'a read');
  assert.equal(calls.filter(c => c.method !== 'GET').length, 0, 'no write route');
});

test('every Automaton and every task is listed, uncapped', async () => {
  const rows = [];
  for (let i = 0; i < 30; i++) { rows.push(autoRow('m' + i, 31, 12)); rows.push(taskRow('ta' + i, 30, 20)); rows.push(taskRow('tb' + i, 20, 12)); }
  const { sb } = load(() => ({ rows }));
  await sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const html = bodyHtml(sb);
  assert.equal((html.match(/data-kind="automaton"/g) || []).length, 30);
  assert.equal((html.match(/data-kind="task"/g) || []).length, 60, 'beyond the 40-row Gantt cap');
  assert.doesNotMatch(html, /Showing \d+ of/);
  const small = load(ok);
  await small.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const h = bodyHtml(small.sb);
  for (const n of ['Auto m1', 'Auto m2', 'Task t1', 'Task t2', 'Task t3', 'Task t4']) assert.match(h, new RegExp(n));
});

test('expand and collapse work, are remembered, and fetch nothing more', async () => {
  const { sb, calls } = load(ok);
  await sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const n = calls.length;
  await sb.projectsFocusToggle('auto:m1');
  let h = bodyHtml(sb);
  assert.doesNotMatch(h, /Task t1/);
  assert.match(h, /Task t3/, 'the other Automaton stays open');
  assert.match(h, /aria-label="Expand Auto m1"/);
  assert.deepEqual(JSON.parse(sb.localStorage.getItem('cs_projects_gantt_expanded_focus:a1')), ['auto:m2']);
  await sb.projectsFocusToggle('auto:m1');
  assert.match(bodyHtml(sb), /Task t1/);
  assert.equal(calls.length, n, 'no fetch on expand or collapse');
  // A later open restores what was stored, not the open-all default.
  await sb.projectsFocusToggle('auto:m2');
  await sb.projectsFocusToggle('auto:m2');
  await sb.projectsFocusToggle('auto:m1');
  const again = load(ok);
  again.sb.localStorage.setItem('cs_projects_gantt_expanded_focus:a1', JSON.stringify(['auto:m1']));
  await again.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  h = bodyHtml(again.sb);
  assert.match(h, /Task t1/);
  assert.doesNotMatch(h, /Task t3/);
});

test('presets, wheel zoom and drag pan move the focus range', async () => {
  const { sb } = load(ok);
  await sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const f = focusState(sb);
  sb.projectsFocusPreset('7d');
  let r = sb._projectsGanttCurrent(f, NOW);
  assert.equal(r.end - r.start, 7 * DAY);
  assert.match(bodyHtml(sb), /data-preset="7d" aria-pressed="true" onclick="projectsFocusPreset\('7d'\)"/);
  sb.projectsFocusPreset('all');
  r = sb._projectsGanttCurrent(f, NOW);
  assert.ok(r.start < Date.parse(ago(30)) && Date.parse(ago(30)) - r.start < 0.03 * (r.end - r.start), '2 percent margin before the first row');
  const before = r.end - r.start;
  sb.projectsFocusWheel({ deltaY: -100, clientX: 0, preventDefault() {} });
  assert.ok(f.view.end - f.view.start < before, 'wheel up zooms in');
  assert.match(bodyHtml(sb), /id="projectFocusChart"[^>]*onwheel="projectsFocusWheel\(event\)" onpointerdown="projectsFocusDown\(event\)"/);
  sb.projectsFocusPreset('30d');
  assert.equal(f.view, null, 'a preset clears the zoom');
});

test('an Automation with no tasks shows the planned empty state, not an error', async () => {
  const { sb } = load(() => ({ rows: [] }));
  await sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  const h = bodyHtml(sb);
  assert.match(h, /has no Automata or tasks yet/);
  assert.match(h, /Planned|It is planned/);
  assert.doesNotMatch(h, /failed/i);
  // Automata with no task yet read as planned rows.
  const some = load(() => ({ rows: [{ kind: 'automaton', id: 'm9', title: 'Auto m9', state: 'planned', first: null, last: null, running: false, has_children: false, children_count: 0 }] }));
  await some.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  assert.match(bodyHtml(some.sb), /project-gantt-planned/);
});

test('Back returns to the project page, or to the Automation detail', async () => {
  const p = load(ok);
  await p.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  assert.match(p.cap.panel, /project-focus-back/);
  await p.sb.projectsFocusBack();
  assert.equal(focusState(p.sb), null);
  assert.doesNotMatch(p.cap.panel, /project-focus-back/, 'the project page is painted again');
  assert.match(p.cap.panel, /project-picker|projectsView|projectPicker|project-detail-card/);

  const a = load(u => (u.includes('/acme/') ? { __status: 404, __text: 'arc not found' } : ok(u)));
  const opened = [];
  a.sb.openAutomationDetail = id => { opened.push(id); return Promise.resolve(); };
  await a.sb.projectsFocusOpen('a1', 'Release work', 'automation');
  await flushAsync();
  assert.match(a.cap.view, /project-focus-back/, 'drawn in the Automation page area');
  assert.deepEqual(a.calls.filter(c => c.url.includes('/timeline')).map(c => c.url.split('/')[3]), ['acme', 'beta'], 'a project without the Automation is skipped');
  assert.match(bodyHtml(a.sb), /Task t3/);
  await a.sb.projectsFocusBack();
  assert.deepEqual(opened, ['a1']);
  assert.equal(focusState(a.sb), null);
});

test('a failed read shows the standard error line; a late reply after leaving is dropped; no timer remains', async () => {
  const bad = load(() => ({ __status: 500, __text: 'boom' }));
  await bad.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await flushAsync();
  assert.match(bodyHtml(bad.sb), /boom/);
  assert.equal(bad.timers.n, 0);

  let release;
  const gate = new Promise(r => { release = r; });
  const late = load(() => gate.then(() => REPLY));
  const pending = late.sb.projectsFocusOpen('a1', 'Arc one', 'project');
  await late.sb.projectsFocusBack(); // the viewer leaves
  const bodyBefore = late.cap.body;
  release();
  await pending;
  await flushAsync();
  assert.equal(late.cap.body, bodyBefore, 'the late reply paints nothing');
  assert.equal(focusState(late.sb), null);
  assert.equal(late.timers.n, 0, 'no timer');
});

test('focus code never kills or signals a session and calls no write route', () => {
  const src = fs.readFileSync(APP, 'utf8');
  const a = src.indexOf('// ── Focus view: one Automation');
  const b = src.indexOf('// _projectsLoadHistory reads one page');
  const block = src.slice(a, b);
  assert.ok(block.length > 500);
  assert.doesNotMatch(block, /kill|signal|\/sessions|method:\s*'(POST|PUT|DELETE|PATCH)'/i);
});

test('the locale keys exist in all five locales', () => {
  const keys = ['automation_focus_button', 'projects_gantt_focus_title', 'projects_gantt_focus_back', 'automation_gantt_focus_empty', 'projects_gantt_focus_range_text'];
  for (const code of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', code + '.json'), 'utf8'));
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k], code + ' ' + k);
    assert.doesNotMatch(b.projects_gantt_focus_title + b.automation_gantt_focus_empty, /\bPRD\b/);
  }
});
