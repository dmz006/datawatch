'use strict';
// The header scope changes what each Automata page shows.
//
// The scope is {project, plan}: All, one project, or one plan of a project (an
// arc keyed by its automation id). With fixture projects, plans, Automata that
// carry a project link and plan membership, and event rows that carry
// automation fields, this file runs the real app.js in a vm sandbox (no daemon,
// no tmux) and asserts what each page draws under All, a project scope and a
// plan scope:
//   (a) the Automata list cards and the Templates note;
//   (b) the Events rows: the project query parameter, plan filtering on the
//       client, and paging that keeps filling a short plan page;
//   (c) the Projects sub-tab bodies: Plans, Records, timeline, history,
//       unassigned and project events;
//   (d) an Automation sub-tab list and a global card with the not-filtered note;
//   (e) the banner names the scope and Clear returns to All;
//   (f) the hash carries #projects/<name>/<subtab>?plan=<id>, old forms still
//       parse, and a hash naming a project sets the scope;
//   (g) a late reply for an older scope is dropped.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-scope-pages.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const EN = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
const run = (sb, code) => vm.runInContext(code, sb);

function json(body) {
  return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
}

// ── fixtures ───────────────────────────────────────────────────────────────

const PROJECTS = [
  { name: 'acme', dir: '/work/acme', arcs: [{ automation_id: 'au-1' }, { automation_id: 'au-2' }] },
  { name: 'zeta', dir: '/work/zeta', arcs: [{ automation_id: 'au-9' }] },
];
const AUTOMATIONS = [
  { id: 'au-1', name: 'Launch plan', project: 'acme', status: 'running', members: [] },
  { id: 'au-2', name: 'Cleanup plan', project: 'acme', status: 'running', members: [] },
  { id: 'au-9', name: 'Zeta plan', project: 'zeta', status: 'running', members: [] },
];
// Automata: project link and plan membership. p-1 and p-2 are members of au-1.
const PRDS = [
  { id: 'p-1', title: 'Alpha job', project: 'acme', status: 'running', is_template: false },
  { id: 'p-2', title: 'Beta job', project: 'acme', status: 'running', is_template: false },
  { id: 'p-3', title: 'Gamma job', project: 'acme', automation_id: 'au-2', status: 'running', is_template: false },
  { id: 'p-4', title: 'Delta job', project: 'zeta', automation_id: 'au-9', status: 'running', is_template: false },
  { id: 'p-5', title: 'Epsilon loose', project: '', status: 'running', is_template: false },
  { id: 't-1', title: 'Acme template', project: 'acme', status: 'draft', is_template: true },
  { id: 't-2', title: 'Zeta template', project: 'zeta', status: 'draft', is_template: true },
];
const MEMBERS_AU1 = { id: 'au-1', members: [{ id: 'm1', prd_id: 'p-1' }, { id: 'm2', prd_id: 'p-2' }] };
const ROLL = {
  window_days: 7, points_priced: true,
  total: { tasks: { done: 6, total: 10 }, points: 20, throughput: {} },
  arcs: [
    { automation_id: 'au-1', priority: 1, state: 'active', tasks: { done: 1, total: 4 }, points: 3, automata: [{ id: 'p-1', title: 'First', status: 'completed', points: 1 }, { id: 'p-2', title: 'Second', status: 'running', points: 2 }] },
    { automation_id: 'au-2', priority: 2, state: 'planned', tasks: { done: 5, total: 6 }, points: 17, automata: [{ id: 'p-3', title: 'Third', status: 'completed' }] },
  ],
  unassigned: { tasks: { done: 0, total: 0 }, points: 0, automata: [{ id: 'p-9', title: 'Loose', status: 'planned' }] },
};
// Event rows: the Events tab names an Automation or an Automaton per row.
const ev = (id, extra) => Object.assign({ id, time: '2026-11-10T03:00:00Z', kind: 'watch', project: 'acme', automaton: '', automation: '', summary: 'summary ' + id, detail: '', reason: '', next_action: '' }, extra || {});
const EV_MEMBER = ev('e-member', { automaton: 'p-1' });
const EV_PLAN = ev('e-plan', { automation: 'au-1' });
const EV_OTHER_PLAN = ev('e-other', { automation: 'au-2' });
const EV_OTHER_MEMBER = ev('e-stranger', { automaton: 'p-3' });
const EV_UNNAMED = ev('e-unnamed');

// load builds a sandbox. fetches maps a URL fragment to a reply (a function or a
// body); calls records every URL fetched.
function load({ hash = '', store = null, tab = 'automata', view = 'autonomous', fetches = {} } = {}) {
  const sandbox = buildSandbox({ initialHash: hash });
  const dom = installFakeDom(sandbox, { statics: ['headerScopeBtn', 'headerScopeBadge', 'view', 'automataPanel', 'automataEventsList', 'automationPanel'] });
  ['view', 'automataPanel'].forEach(id => { dom.el(id).insertAdjacentHTML = () => {}; });
  const calls = [];
  sandbox.fetch = (url) => {
    const u = String(url);
    calls.push(u);
    for (const k of Object.keys(fetches)) {
      if (u.includes(k)) { const r = fetches[k]; return typeof r === 'function' ? r(u) : json(r); }
    }
    if (u.endsWith('/api/projects')) return json(PROJECTS);
    if (u.endsWith('/api/automations')) return json(AUTOMATIONS);
    if (u.endsWith('/api/automations/au-1')) return json(MEMBERS_AU1);
    return json([]);
  };
  if (store) {
    sandbox.localStorage = {
      getItem(k) { return store.has(k) ? store.get(k) : null; },
      setItem(k, v) { store.set(k, String(v)); },
      removeItem(k) { store.delete(k); },
    };
  }
  const replaced = [];
  sandbox.history = { pushState() {}, replaceState(s, t, url) { replaced.push(url); sandbox.location.hash = String(url).slice(String(url).indexOf('#')); } };
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  run(sandbox, `state.activeView = ${JSON.stringify(view)}; _automataState.tab = ${JSON.stringify(tab)};
    _projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';
    _automataState.allPrds = ${JSON.stringify(PRDS)};`);
  sandbox.__dom = dom;
  sandbox.__calls = calls;
  sandbox.__replaced = replaced;
  sandbox.__repaints = 0;
  sandbox.renderAutonomousView = () => { sandbox.__repaints++; };
  _shownSandbox = sandbox;
  return sandbox;
}
const scopeOf = sb => JSON.parse(JSON.stringify(sb.getScope()));
const setScope = (sb, project, plan) => run(sb, `setScope(${JSON.stringify(project)}, ${JSON.stringify(plan)}, { silent: true, noHash: true });`);
// planReady seeds the plan's member cache (the Projects tests do not want the
// real Automation read to run its repaint over their fixtures).
function planReady(sb, ids) {
  run(sb, `_scopePlanCache = { key: 'acme\\nau-1', ids: new Set(${JSON.stringify(ids)}), status: 'ready' };`);
}
const has = (html, text) => html.indexOf(text) >= 0;

// ── (a) the Automata list ──────────────────────────────────────────────────

const TITLES = ['Alpha job', 'Beta job', 'Gamma job', 'Delta job', 'Epsilon loose'];
// shown lists the Automata whose list tile (the card renderAutomataCard draws)
// is in the html. The pinned Running strip above the list is a separate block
// and is not what the scope filters, so it is not read here.
let _shownSandbox = null;
const shown = html => TITLES.filter(title => {
  const prd = PRDS.find(p => p.title === title) || { id: 'late', title, project: 'acme', status: 'running' };
  return has(html, _shownSandbox.renderAutomataCard(prd, ''));
});

test('(a) under All the Automata list shows every Automaton and no banner', () => {
  const sb = load();
  const html = sb._automataCardsHtml();
  assert.deepEqual(shown(html), TITLES);
  assert.ok(!has(html, 'scope-banner'));
  assert.ok(!has(html, 'Acme template'), 'templates have their own tab');
});

test('(a) a project scope keeps the Automata linked to that project', () => {
  const sb = load();
  setScope(sb, 'acme', '');
  const html = sb._automataCardsHtml();
  assert.deepEqual(shown(html), ['Alpha job', 'Beta job', 'Gamma job']);
  assert.ok(html.startsWith('<div class="scope-banner"'), 'the banner sits on top');
  assert.ok(has(html, 'Filtered to acme'));
  setScope(sb, 'zeta', '');
  assert.deepEqual(shown(sb._automataCardsHtml()), ['Delta job']);
});

test('(a) a plan scope keeps only the plan members, read once from the Automation', async () => {
  const sb = load();
  setScope(sb, 'acme', 'au-1');
  let html = sb._automataCardsHtml();
  assert.deepEqual(shown(html), [], 'while the member list is read no tile shows');
  await flushAsync();
  assert.ok(sb.__repaints >= 1, 'the page repainted when the members arrived');
  html = sb._automataCardsHtml();
  assert.deepEqual(shown(html), ['Alpha job', 'Beta job']);
  assert.ok(has(html, 'Filtered to acme / Launch plan') || has(html, 'Filtered to acme / au-1'), 'the banner names the plan');
  sb._automataCardsHtml();
  assert.equal(sb.__calls.filter(u => u.endsWith('/api/automations/au-1')).length, 1, 'read once per plan');
  // an Automaton that names the plan's Automation counts without being listed
  const late = { id: 'p-8', title: 'Zeta late', project: 'acme', automation_id: 'au-1', status: 'running', is_template: false };
  run(sb, `_automataState.allPrds.push(${JSON.stringify(late)});`);
  assert.ok(has(sb._automataCardsHtml(), sb.renderAutomataCard(late, '')), 'the Automaton that names the plan Automation shows');
});

test('(a) the Templates tab is global: every template shows with the not-filtered note', () => {
  const sb = load({ tab: 'templates' });
  setScope(sb, 'acme', 'au-1');
  const base = Array.from(sb._automataBaseList()).map(p => p.id).sort();
  assert.deepEqual(base, ['t-1', 't-2'], 'the scope never filters templates');
  const note = sb._automataScopeNoteHtml();
  assert.match(note, /scope-banner-note/);
  assert.match(note, /role="note"/);
  assert.ok(has(note, EN.header_scope_templates_note), 'the Templates note text');
  assert.ok(!has(note, 'Clear'), 'a note, not the filtering banner');
  setScope(sb, '', '');
  assert.equal(sb._automataScopeNoteHtml(), '', 'no note under All');
});

// ── (b) the Events rows ────────────────────────────────────────────────────

// loadEvents wires apiFetch to a queue of pages and records every URL. Pages are
// popped in order; a page may be a function of the URL.
function loadEvents(pages, opts) {
  const sb = load(opts);
  const urls = [];
  sb.apiFetch = async (url) => {
    urls.push(url);
    const p = pages.shift();
    if (p === undefined) return { events: [], next_cursor: '' };
    return typeof p === 'function' ? p(url) : p;
  };
  sb.__urls = urls;
  return sb;
}
const listHtml = sb => sb.__dom.el('automataEventsList').innerHTML;
const rowIds = sb => (listHtml(sb).match(/data-event-id="([^"]*)"/g) || []).map(s => s.slice(15, -1));

test('(b) a project scope sends project= and overrides the operator dropdown', async () => {
  const sb = loadEvents([{ events: [EV_MEMBER, EV_OTHER_PLAN], next_cursor: '' }]);
  run(sb, `_automataEventsState.project = 'zeta';`);
  assert.equal(sb.automataEventsQuery(), '/api/autonomous/event-log?limit=50&project=zeta');
  setScope(sb, 'acme', '');
  assert.equal(sb.automataEventsQuery(), '/api/autonomous/event-log?limit=50&project=acme', 'the scope project wins');
  assert.equal(sb.automataEventsQuery('c1'), '/api/autonomous/event-log?limit=50&project=acme&before=c1');
  sb.automataEventsReload();
  await flushAsync();
  assert.deepEqual(sb.__urls, ['/api/autonomous/event-log?limit=50&project=acme']);
  assert.deepEqual(rowIds(sb), ['e-member', 'e-other'], 'a project scope leaves the rows to the server filter');
  setScope(sb, '', '');
  assert.equal(sb.automataEventsQuery(), '/api/autonomous/event-log?limit=50&project=zeta', 'back to the dropdown under All');
});

test('(b) a plan scope sends the project and keeps only the plan rows on the client', async () => {
  const sb = loadEvents([{ events: [EV_MEMBER, EV_OTHER_PLAN, EV_PLAN, EV_UNNAMED, EV_OTHER_MEMBER], next_cursor: '' }]);
  setScope(sb, 'acme', 'au-1');
  await sb._scopePlanPrdIds();
  await flushAsync();
  sb.automataEventsReload();
  await flushAsync();
  assert.match(sb.__urls[0], /project=acme/);
  assert.ok(!/plan=/.test(sb.__urls[0]), 'the plan is filtered on the client, not sent');
  assert.deepEqual(rowIds(sb), ['e-member', 'e-plan'], 'a member Automaton row and the plan Automation row');
});

test('(b) a plan scope with no matching row says there are no events', async () => {
  const sb = loadEvents([{ events: [EV_OTHER_PLAN, EV_UNNAMED], next_cursor: '' }]);
  setScope(sb, 'acme', 'au-1');
  await flushAsync();
  sb.automataEventsReload();
  await flushAsync();
  assert.deepEqual(rowIds(sb), []);
  assert.ok(has(listHtml(sb), EN.automata_events_empty));
});

test('(b) paging keeps filling: a short plan page pulls the next page without a click, and Load more goes on', async () => {
  const pages = [
    { events: [EV_OTHER_PLAN, EV_OTHER_MEMBER, EV_MEMBER], next_cursor: 'c1' },
    { events: [EV_UNNAMED, ev('e-plan-2', { automation: 'au-1' })], next_cursor: 'c2' },
    { events: [ev('e-plan-3', { automaton: 'p-2' })], next_cursor: '' },
  ];
  const sb = loadEvents(pages);
  setScope(sb, 'acme', 'au-1');
  sb._scopePlanPrdIds();
  await flushAsync();
  sb.automataEventsReload();
  await flushAsync(30);
  assert.ok(sb.__urls.length >= 2, 'the second page was read without any click');
  assert.ok(!/before=/.test(sb.__urls[0]));
  assert.match(sb.__urls[1], /before=c1/, 'the next page follows next_cursor');
  assert.deepEqual(rowIds(sb).slice(0, 2), ['e-member', 'e-plan-2'], 'the plan rows of the pages read so far, in order');
  assert.ok(!rowIds(sb).includes('e-other') && !rowIds(sb).includes('e-unnamed'), 'rows of other plans stay hidden');
  // the rest of the log is one Load more away, and it is filtered the same way
  while (run(sb, '_automataEventsState.nextCursor')) {
    const before = sb.__urls.length;
    sb.automataEventsLoadMore();
    await flushAsync(30);
    assert.ok(sb.__urls.length > before, 'Load more reads the next page');
  }
  assert.deepEqual(rowIds(sb), ['e-member', 'e-plan-2', 'e-plan-3']);
});

test('(b) paging under a project scope or All reads one page: the server already filtered', async () => {
  const sb = loadEvents([{ events: [EV_OTHER_PLAN], next_cursor: 'c1' }, { events: [EV_PLAN], next_cursor: '' }]);
  setScope(sb, 'acme', '');
  sb.automataEventsReload();
  await flushAsync(30);
  assert.equal(sb.__urls.length, 1);
  assert.equal(run(sb, '_automataEventsState.nextCursor'), 'c1', 'Load more stays available');
});

test('(b) filling is capped, so a plan with no rows never reads the whole log', async () => {
  const sb = load();
  let n = 0;
  sb.apiFetch = async (url) => { n++; return { events: [EV_OTHER_PLAN], next_cursor: 'c' + n }; };
  setScope(sb, 'acme', 'au-1');
  sb._scopePlanPrdIds();
  await flushAsync();
  sb.automataEventsReload();
  await flushAsync(80);
  assert.ok(n > 1, 'it did keep reading');
  assert.ok(n <= 11, 'one page plus at most ten more, got ' + n);
});

test('(b) a scope change repaints Events through a reload with the new project', async () => {
  const sb = load();
  const urls = [];
  sb.apiFetch = async (url) => {
    urls.push(url);
    return /project=zeta/.test(url) ? { events: [ev('z1', { project: 'zeta', automaton: 'p-4' })], next_cursor: '' } : { events: [EV_MEMBER], next_cursor: '' };
  };
  sb.renderAutonomousView = () => sb.automataEventsReload();
  run(sb, `_automataState.tab = 'events';`);
  run(sb, `setScope('acme', '')`);
  await flushAsync();
  assert.deepEqual(rowIds(sb), ['e-member']);
  run(sb, `setScope('zeta', '')`);
  await flushAsync();
  assert.ok(urls.some(u => /project=zeta/.test(u)), 'the zeta request went out');
  assert.deepEqual(rowIds(sb), ['z1']);
});

// ── (c) the Projects bodies ────────────────────────────────────────────────

function loadProjects(opts) {
  const sb = load(Object.assign({ tab: 'projects' }, opts));
  return sb;
}
// page draws the selected project page for a sub-tab and returns the body.
function page(sb, subtab, extra) {
  run(sb, `_projectsState.selected = 'acme'; _projectsState.subtab = ${JSON.stringify(subtab)};
    _projectsState.rollup = ${JSON.stringify(ROLL)}; _projectsState.rollupFor = 'acme'; _projectsState.rollupError = '';
    ${extra || ''}`);
  const out = run(sb, `_projectPageHTML(_projectsState.list[0], _projectsState.rollup, '', _projectsState.hist)`);
  const m = /<div id="projectsSubtabBody"[^>]*>([\s\S]*)<\/div><\/div><\/div>$/.exec(out);
  assert.ok(m, 'the sub-tab body is present');
  return m[1];
}
const banner = body => body.startsWith('<div class="scope-banner"');

test('(c) every Projects body starts with the banner under a project or plan scope, and not under All', () => {
  const sb = loadProjects();
  ['overview', 'plans', 'records', 'gantt', 'unassigned', 'history'].forEach(k => {
    setScope(sb, '', '');
    assert.ok(!has(page(sb, k), 'scope-banner'), k + ' has no banner under All');
    setScope(sb, 'acme', '');
    assert.ok(banner(page(sb, k)), k + ' under a project scope');
    setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1', 'p-2']);
    const body = page(sb, k);
    assert.ok(banner(body), k + ' under a plan scope');
    assert.ok(has(body.slice(0, 500), 'Filtered to acme / '), k + ' names the plan');
  });
});

test('(c) Plans: a plan scope keeps the plan roll-up group and the plan record that lists it', () => {
  const sb = loadProjects();
  run(sb, `_plansBrowser = _plansBrowserFresh('acme'); _plansBrowser.status = 'ready'; _plansBrowser.total = 2;
    _plansBrowser.plans = [
      { id: 'pl-1', title: 'Plan one', state: 'active', automation_ids: ['au-1'], progress: {} },
      { id: 'pl-2', title: 'Plan two', state: 'planned', automation_ids: ['au-2'], progress: {} }];`);
  setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1']);
  const body = page(sb, 'plans');
  assert.ok(has(body, 'data-automation="au-1"'));
  assert.ok(!has(body, 'data-automation="au-2"'), 'the other plan is gone');
  const list = run(sb, 'plansBrowserListHTML(_plansBrowser)');
  assert.ok(has(list, 'data-plan="pl-1"'));
  assert.ok(!has(list, 'data-plan="pl-2"'));
  setScope(sb, 'acme', '');
  const all = page(sb, 'plans');
  assert.ok(has(all, 'data-automation="au-1"') && has(all, 'data-automation="au-2"'), 'a project scope keeps every plan');
  assert.ok(has(run(sb, 'plansBrowserListHTML(_plansBrowser)'), 'data-plan="pl-2"'));
});

test('(c) Records: a plan scope keeps rows under the plan record, the Automation or its Automata', () => {
  const sb = loadProjects();
  run(sb, `_plansBrowser = _plansBrowserFresh('acme'); _plansBrowser.status = 'ready';
    _plansBrowser.plans = [{ id: 'pl-1', automation_ids: ['au-1'] }, { id: 'pl-2', automation_ids: ['au-2'] }];
    _recordsTab = _recordsTabFresh('acme'); _recordsTab.status = 'ready';
    _recordsTab.rows = [
      { id: 'r1', title: 'ByPlanRecord', kind: 'registry_row', plan_id: 'pl-1' },
      { id: 'r2', title: 'ByAutomaton', kind: 'finding', plan_id: 'pl-9', automaton_ids: ['p-2'] },
      { id: 'r3', title: 'ByAutomation', kind: 'registry_row', automation_id: 'au-1' },
      { id: 'r4', title: 'OtherPlan', kind: 'registry_row', plan_id: 'pl-2' },
      { id: 'r5', title: 'NoLink', kind: 'finding' }];
    _recordsTab.total = 5;`);
  setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1', 'p-2']);
  const list = run(sb, 'recordsTabListHTML(_recordsTab)');
  ['ByPlanRecord', 'ByAutomaton', 'ByAutomation'].forEach(x => assert.ok(has(list, x), x));
  ['OtherPlan', 'NoLink'].forEach(x => assert.ok(!has(list, x), x + ' is out'));
  assert.ok(banner(page(sb, 'records')));
  setScope(sb, 'acme', '');
  const all = run(sb, 'recordsTabListHTML(_recordsTab)');
  assert.ok(has(all, 'OtherPlan') && has(all, 'NoLink'), 'everything under a project scope');
});

test('(c) Timeline: a plan scope keeps the plan row and reads on until it finds it', async () => {
  const sb = loadProjects({ fetches: { '/timeline': (u) => {
    if (u.includes('level=arcs') && !u.includes('cursor=')) return json({ rows: [{ id: 'au-2', kind: 'arc', title: 'Two', has_children: true }, { kind: 'unassigned', id: 'none' }], next_cursor: 'k1' });
    if (u.includes('level=arcs')) return json({ rows: [{ id: 'au-1', kind: 'arc', title: 'One', has_children: true }], next_cursor: '' });
    return json({ rows: [] });
  } } });
  setScope(sb, 'acme', 'au-1'); planReady(sb, []);
  run(sb, `_projectsState.selected = 'acme';`);
  await run(sb, '_projectsLoadGantt()');
  await flushAsync();
  assert.deepEqual(Array.from(run(sb, '_projectsState.gantt.rows')).map(r => r.id), ['au-1']);
  const body = page(sb, 'gantt');
  assert.ok(banner(body));
  assert.ok(!has(body, '>Two<'), 'the other plan is not drawn');
});

test('(c) History: a plan scope keeps the plan Automata and drops the daily snapshots', () => {
  const sb = loadProjects();
  const hist = `_projectsState.hist = { name: 'acme', next: '', error: '', items: [
    { kind: 'automaton', id: 'p-1', title: 'Mine', status: 'completed', tasks: { done: 1, total: 1 } },
    { kind: 'automaton', id: 'p-3', title: 'Theirs', status: 'completed', tasks: { done: 1, total: 1 } },
    { kind: 'snapshot', taken_at: '2026-11-01T00:00:00Z', progress: { tasks: 4, tasks_done: 1 } }] };`;
  setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1', 'p-2']);
  const body = page(sb, 'history', hist);
  assert.ok(has(body, 'Mine'));
  assert.ok(!has(body, 'Theirs'));
  assert.ok(!has(body, 'data-kind="snapshot"'));
  setScope(sb, 'acme', '');
  const all = page(sb, 'history', hist);
  assert.ok(has(all, 'Theirs') && has(all, 'data-kind="snapshot"'));
});

test('(c) History paging: a plan scope reads more pages while it shows too little, with a cap', async () => {
  let pages = 0;
  const sb = loadProjects({ fetches: { '/history': () => {
    pages++;
    return json({ items: [{ kind: 'automaton', id: pages === 3 ? 'p-1' : 'x-' + pages, title: 'row' + pages, status: 'completed', tasks: {} }], next_cursor: 'c' + pages });
  } } });
  setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1']);
  run(sb, `_projectsState.selected = 'acme';`);
  await run(sb, '_projectsLoadHistory(false)');
  await flushAsync();
  assert.ok(pages >= 3 && pages <= 11, 'read until the plan row appeared, within the cap: ' + pages);
});

test('(c) Unassigned: a plan scope says unassigned Automata are not part of a plan; a project scope lists them', () => {
  const sb = loadProjects();
  setScope(sb, 'acme', 'au-1'); planReady(sb, ['p-1']);
  const body = page(sb, 'unassigned');
  assert.ok(has(body, 'scope-empty-unassigned'));
  assert.ok(has(body, EN.projects_scope_unassigned_not_in_plan));
  assert.ok(!has(body, 'Loose'));
  setScope(sb, 'acme', '');
  assert.ok(has(page(sb, 'unassigned'), 'Loose'));
});

test('(c) Project events: a plan scope keeps the rows of the plan Automation and the request names the project', async () => {
  const sb = loadProjects();
  sb.__ev = { events: [{ id: 'e1', kind: 'task_done', automation_id: 'au-1' }, { id: 'e2', kind: 'task_done', automation_id: 'au-2' }, { id: 'e3', kind: 'note' }] };
  setScope(sb, 'acme', 'au-1');
  const scoped = run(sb, `_projectEventStreamHTML(__ev, '')`);
  assert.ok(has(scoped, 'data-automation="au-1"'));
  assert.ok(!has(scoped, 'data-automation="au-2"'));
  assert.ok(!has(scoped, 'data-kind="note"'));
  setScope(sb, 'acme', '');
  const all = run(sb, `_projectEventStreamHTML(__ev, '')`);
  assert.ok(has(all, 'data-automation="au-2"') && has(all, 'data-kind="note"'));
  run(sb, `_projectsState.selected = 'acme';`);
  await run(sb, '_projectsLoadLinks()');
  assert.ok(sb.__calls.some(u => /\/api\/autonomous\/events\?.*project=acme/.test(u)), 'project= is sent');
});

// ── (d) an Automation sub-tab list and a global card ───────────────────────

const subtab = (sb, id) => run(sb, `_AUTOMATION_SUBTABS.find(x => x.id === ${JSON.stringify(id)})`);
const tiles = (sb, list) => run(sb, `_automationTilesHTML(automationListVisible(${JSON.stringify(list)}, { search: '', historyOn: false }), { prdTitles: {}, mergeById: {}, now: Date.now() })`);
const AUTO_NAMES = ['Launch plan', 'Cleanup plan', 'Zeta plan'];

test('(d) the Automation sub-tab list keeps the Automations of the scoped project, or the scoped plan', () => {
  const sb = load();
  run(sb, `_shippedState.enabled = false;`);
  const names = html => AUTO_NAMES.filter(x => has(html, x));
  assert.deepEqual(names(tiles(sb, AUTOMATIONS)), AUTO_NAMES, 'All shows every Automation');
  setScope(sb, 'acme', '');
  assert.deepEqual(names(tiles(sb, AUTOMATIONS)), ['Launch plan', 'Cleanup plan']);
  setScope(sb, 'acme', 'au-2');
  assert.deepEqual(names(tiles(sb, AUTOMATIONS)), ['Cleanup plan'], 'a plan is one Automation');
  setScope(sb, 'zeta', '');
  assert.deepEqual(names(tiles(sb, AUTOMATIONS)), ['Zeta plan']);
});

test('(d) the Automations sub-tab body carries the banner; the Inbox too; none under All', () => {
  const sb = load();
  setScope(sb, 'acme', '');
  assert.ok(has(subtab(sb, 'automations').bodyHTML(), 'Filtered to acme'));
  assert.ok(has(subtab(sb, 'inbox').bodyHTML(), 'Filtered to acme'));
  assert.ok(has(subtab(sb, 'automations').bodyHTML(), 'onclick="clearScope()"'));
  setScope(sb, '', '');
  assert.ok(!has(subtab(sb, 'automations').bodyHTML(), 'scope-banner'));
});

test('(d) a global card shows the not-filtered note under a scope and nothing under All', () => {
  const sb = load();
  setScope(sb, 'acme', 'au-1');
  ['resources', 'progress', 'housekeeping'].forEach(id => {
    const body = subtab(sb, id).bodyHTML();
    assert.ok(has(body, EN.header_scope_global_note), id + ' says it applies to every project and plan');
    assert.ok(has(body, 'scope-banner-note'), id);
    assert.ok(!has(body, 'onclick="clearScope()"'), id + ' is a note, not a filter banner');
  });
  setScope(sb, '', '');
  ['resources', 'progress', 'housekeeping'].forEach(id => assert.ok(!has(subtab(sb, id).bodyHTML(), 'scope-banner'), id));
  assert.equal(sb._automataGlobalScopeNote(), '');
});

// ── (e) the banner ─────────────────────────────────────────────────────────

test('(e) the banner names the scope: the project, then project / plan name', () => {
  const sb = load();
  assert.equal(sb._scopeBannerHTML(), '');
  setScope(sb, 'acme', '');
  const projectBanner = sb._scopeBannerHTML();
  assert.match(projectBanner, /<div class="scope-banner" role="status">/);
  assert.ok(has(projectBanner, 'Filtered to acme'));
  setScope(sb, 'acme', 'au-2');
  assert.ok(has(sb._scopeBannerHTML(), 'Filtered to acme / au-2'), 'the plan id until its name is known');
  run(sb, `_automationState.list = ${JSON.stringify(AUTOMATIONS)};`);
  assert.ok(has(sb._scopeBannerHTML(), 'Filtered to acme / Cleanup plan'), 'the plan name once known');
});

test('(e) the banner text is data, not markup', () => {
  const sb = load();
  run(sb, `_projectsState.list.push({ name: 'a<b>&"', arcs: [] });`);
  setScope(sb, 'a<b>&"', '');
  const html = sb._scopeBannerHTML();
  assert.ok(!has(html, '<b>'));
  assert.ok(has(html, 'a&lt;b&gt;&amp;&quot;'));
});

test('(e) Clear returns to All, hides the banner, repaints, and shows every Automaton again', () => {
  const store = new Map();
  const sb = load({ store });
  run(sb, `setScope('acme', 'au-2');`);
  assert.deepEqual(JSON.parse(store.get('dw_scope')), { project: 'acme', plan: 'au-2' });
  const before = sb.__repaints;
  const html = sb._scopeBannerHTML();
  const clear = /onclick="([^"]+)"[^>]*>Clear</.exec(html);
  assert.ok(clear, 'a Clear button');
  run(sb, clear[1]);
  assert.deepEqual(scopeOf(sb), { project: '', plan: '' });
  assert.equal(sb._scopeBannerHTML(), '');
  assert.ok(sb.__repaints > before, 'the page repainted');
  assert.deepEqual(JSON.parse(store.get('dw_scope')), { project: '', plan: '' }, 'All is remembered');
  assert.deepEqual(shown(sb._automataCardsHtml()), TITLES);
});

// ── (f) the hash ───────────────────────────────────────────────────────────

test('(f) the hash carries #projects/<name>/<subtab>?plan=<id> as the scope changes', () => {
  const sb = loadProjects({ hash: '#projects/acme/plans' });
  run(sb, `_projectsState.selected = 'acme'; _projectsState.subtab = 'plans';`);
  run(sb, `setScope('acme', 'au-1', { silent: true });`);
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects/acme/plans?plan=au-1');
  run(sb, `setScope('acme', '', { silent: true });`);
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects/acme/plans');
  run(sb, `setScope('', '', { silent: true });`);
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects');
  assert.equal(sb._projectsHashBuild('acme', 'gantt', 'au 1'), '#projects/acme/gantt?plan=au%201');
  assert.equal(sb._projectsHashBuild('acme', 'overview', 'au-1'), '#projects/acme?plan=au-1');
});

test('(f) old hash forms still parse, with and without the plan suffix', () => {
  const sb = load();
  assert.equal(sb._projectsHashParse('#projects'), '');
  assert.equal(sb._projectsHashParse('#projects/acme'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/acme/gantt'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/acme/gantt?plan=au-1'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/a%20b/plans'), 'a b');
  assert.equal(sb._projectsHashParse('#automata'), null, 'not a projects hash');
  assert.equal(sb._projectsHashParse('#projects/acme/gantt/extra'), null);
  assert.equal(sb._projectsHashSubtab('#projects/acme/gantt'), 'gantt');
  assert.equal(sb._projectsHashSubtab('#projects/acme/arcs'), 'plans', 'the old arcs name is the Plans sub-tab');
  assert.equal(sb._projectsHashSubtab('#projects/acme/gantt?plan=au-1'), 'gantt');
  assert.equal(sb._projectsHashSubtab('#projects/acme'), '');
  assert.equal(sb._projectsHashPlan('#projects/acme'), '', 'no suffix, no plan');
  assert.equal(sb._projectsHashPlan('#projects/acme/plans?plan=au-1'), 'au-1');
  assert.equal(sb._projectsHashPlan('#projects?plan=au-1'), '', 'a plan needs a project');
});

test('(f) a hash naming a project sets the scope when the Projects page opens', async () => {
  const store = new Map();
  const sb = loadProjects({ hash: '#projects/zeta/plans', store });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: 'zeta', plan: '' });
  assert.equal(run(sb, '_projectsState.selected'), 'zeta');
  assert.equal(run(sb, '_projectsState.subtab'), 'plans');
  assert.deepEqual(JSON.parse(store.get('dw_scope')), { project: 'zeta', plan: '' });
});

test('(f) a hash naming a project and plan sets both; the Overview hash means All', async () => {
  const sb = loadProjects({ hash: '#projects/acme/gantt?plan=au-2', store: new Map() });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: 'au-2' });
  assert.equal(run(sb, '_projectsState.subtab'), 'gantt');
  const overview = loadProjects({ hash: '#projects', store: new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: '' })]]) });
  overview.renderProjectsView();
  await flushAsync();
  assert.deepEqual(scopeOf(overview), { project: '', plan: '' });
});

test('(f) no hash keeps the remembered scope', async () => {
  const sb = loadProjects({ hash: '', store: new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-1' })]]) });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: 'au-1' });
});

// ── (g) a late reply for an older scope ────────────────────────────────────

test('(g) a late plan-member reply for an older plan is dropped', async () => {
  const held = {};
  const sb = load({ fetches: {
    '/api/automations/au-1': () => new Promise(res => { held.au1 = () => res({ ok: true, status: 200, json: () => Promise.resolve({ members: [{ prd_id: 'p-1' }] }) }); }),
    '/api/automations/au-2': () => json({ members: [{ prd_id: 'p-3' }] }),
  } });
  setScope(sb, 'acme', 'au-1');
  sb._scopePlanPrdIds();
  setScope(sb, 'acme', 'au-2');
  sb._scopePlanPrdIds();
  await flushAsync();
  const repaintsBefore = sb.__repaints;
  held.au1();
  await flushAsync();
  assert.equal(sb.__repaints, repaintsBefore, 'the late reply did not repaint the page');
  assert.equal(run(sb, '_scopePlanCache.status'), 'ready');
  assert.deepEqual(Array.from(run(sb, '[..._scopePlanCache.ids]')), ['p-3'], 'the cache holds only the current plan');
  assert.deepEqual(shown(sb._automataCardsHtml()), ['Gamma job'], 'only the au-2 member: the late au-1 reply changed nothing');
});

test('(g) a late Events reply for an older scope is dropped', async () => {
  const held = {};
  const sb = load();
  let call = 0;
  sb.apiFetch = (url) => {
    call++;
    if (call === 1) return new Promise(res => { held.first = () => res({ events: [ev('stale-row', { project: 'acme' })], next_cursor: '' }); });
    return Promise.resolve({ events: [ev('fresh-row', { project: 'zeta' })], next_cursor: '' });
  };
  setScope(sb, 'acme', '');
  sb.automataEventsReload();
  setScope(sb, 'zeta', '');
  sb.automataEventsReload();
  await flushAsync();
  held.first();
  await flushAsync();
  assert.deepEqual(rowIds(sb), ['fresh-row'], 'the reply for the older scope never painted');
});

test('(g) a late timeline reply and a late project-events reply for an older scope are dropped', async () => {
  const gate = {};
  const hold = name => new Promise(res => { gate[name] = res; });
  const sb = loadProjects({ fetches: {
    '/timeline': () => hold('timeline').then(() => json({ rows: [{ id: 'au-2', kind: 'arc', title: 'Two' }], next_cursor: '' })),
    '/api/autonomous/events': () => hold('events').then(() => json({ events: [{ id: 'e', automation_id: 'au-1' }] })),
  } });
  run(sb, `_projectsState.selected = 'acme';`);
  setScope(sb, 'acme', 'au-2'); planReady(sb, []);
  const tl = run(sb, '_projectsLoadGantt()');
  const ls = run(sb, '_projectsLoadLinks()');
  setScope(sb, 'acme', 'au-1');
  gate.timeline(); gate.events();
  await tl; await ls;
  await flushAsync();
  assert.equal(run(sb, '_projectsState.gantt.rows.length'), 0, 'the timeline reply was not applied');
  assert.equal(run(sb, '_projectsLinks.events'), null, 'the events reply was not applied');
});

test('(g) a scope change bumps the token and clears the plan cache; the same scope again does neither', () => {
  const sb = load();
  const t0 = run(sb, '_scopeToken');
  setScope(sb, 'acme', 'au-1');
  const t1 = run(sb, '_scopeToken');
  assert.ok(t1 > t0);
  setScope(sb, 'acme', 'au-1');
  assert.equal(run(sb, '_scopeToken'), t1, 'no change, no new token');
  setScope(sb, 'acme', '');
  assert.ok(run(sb, '_scopeToken') > t1);
  assert.equal(run(sb, '_scopePlanCache.status'), 'idle');
});
