// Projects page sub-tabs — order, remembered choice, hidden empty Events,
// phone layout, picker placement, routes, the Gantt cap and the old Policy
// choice falling back to Overview with the policy Settings link in the header.
//
// A selected project's page shows one body at a time under a strip of
// sub-tab buttons: Overview, Arcs, Gantt, Unassigned, History, Events.
// Events is only offered while the page has something to say.
// The former Policy sub-tab is gone; its one Settings link sits by the
// project name, and a stored or hashed 'policy' value falls back to Overview.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const ORDER = ['overview', 'arcs', 'gantt', 'unassigned', 'history', 'events'];
const KEY = 'cs_projects_subtab';
const DAY = 86400000;

const ROLLUP_WITH_CONTENT = {
  arcs: [{ automation_id: 'arc-a', priority: 1, state: 'active', tasks: { done: 1, total: 2 }, automata: [] }],
  unassigned: { automata: [{ id: 'u1', title: 'Loose', status: 'running' }] },
};
// No arcs and nothing unassigned: the page reports both as Events entries.
const ROLLUP_NO_EVENTS = ROLLUP_WITH_CONTENT;
const ROLLUP_EVENTS = { arcs: [], unassigned: { automata: [] } };

function makeStorage(initial = {}) {
  const m = new Map(Object.entries(initial));
  return {
    m,
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  };
}

// load builds the app with English strings and the stubbed Projects replies.
function load({ hash = '#projects/acme', storage = makeStorage(), rollup = ROLLUP_NO_EVENTS, gantt = [], phone = false, throwStorage = false } = {}) {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view }, initialHash: hash });
  const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  sandbox.fetch = (url) => {
    const u = String(url);
    if (u.includes('/history')) return ok({ items: [], gantt, next_cursor: '' });
    if (u.includes('/rollup')) return ok(rollup);
    if (u.includes('/policy/effective')) return ok({ policy: { merge_mode: { value: 'auto', source: 'default' } } });
    if (u.includes('/merge-pipeline')) return ok({ source: 'file', conflict_policy: 'ask', definition: { steps: [] } });
    if (u.includes('/api/autonomous/events')) return ok({ events: [] });
    if (u.includes('/api/projects')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    return ok([]);
  };
  sandbox.localStorage = storage;
  if (phone) {
    sandbox.innerWidth = 375;
    sandbox.innerHeight = 700;
    sandbox.matchMedia = (q) => ({ matches: /max-width/.test(String(q)), media: String(q), addEventListener() {}, addListener() {} });
  }
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  // app.js reads storage unguarded outside the Projects code (load, auth
  // token), so only the Projects keys fail, and only once the app is up.
  if (throwStorage) {
    const boom = k => { if (String(k).startsWith('cs_projects')) throw new Error('blocked'); };
    sandbox.localStorage = {
      getItem(k) { boom(k); return storage.getItem(k); },
      setItem(k, v) { boom(k); storage.setItem(k, v); },
      removeItem(k) { boom(k); storage.removeItem(k); },
    };
  }
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, out, storage };
}

const run = (sb, code) => vm.runInContext(code, sb);
const buttons = html => [...html.matchAll(/data-projects-subtab="([a-z]+)"/g)].map(m => m[1]);
const activeBody = html => (html.match(/data-projects-subtab-body="([a-z]+)"/) || [])[1];

async function open(opts) {
  const l = load(opts);
  await l.sb.renderProjectsView();
  await flushAsync(30);
  return l;
}

function row(kind, autoId, id, title, startAgo, endAgo) {
  const now = Date.now();
  const r = { kind, automaton_id: autoId, title, state: 'completed', start: new Date(now - startAgo * DAY).toISOString(), end: new Date(now - endAgo * DAY).toISOString() };
  if (kind === 'task') r.task_id = id; else r.automation_id = 'arc-a';
  return r;
}

test('the sub-tab buttons are overview, arcs, gantt, unassigned, history, policy, events in that order', async () => {
  const { sb, out } = await open({ rollup: ROLLUP_EVENTS });
  assert.deepEqual(JSON.parse(run(sb, 'JSON.stringify(_PROJECTS_SUBTABS)')), ORDER);
  assert.deepEqual(buttons(out.view), ORDER);
  assert.match(out.view, /id="projectsSubtab_overview"[^>]*aria-selected="true"/, 'Overview is selected with no stored choice');
});

test('choosing Gantt is remembered, a fresh load restores it, an unknown value means Overview', async () => {
  const storage = makeStorage();
  const first = await open({ hash: '', storage });
  run(first.sb, "_projectsState.selected = 'acme'");
  first.sb.selectProjectsSubtab('gantt');
  assert.equal(storage.getItem(KEY), 'gantt', 'the choice is stored');
  assert.equal(activeBody(first.out.view), 'gantt');

  // A fresh app with the same storage and the project's address (no sub-tab)
  // restores the Gantt choice when the address names no project page choice.
  const second = load({ hash: '', storage });
  run(second.sb, "_projectsState.list = [{ name: 'acme' }]; _projectsState.selected = 'acme'");
  assert.equal(second.sb._projectsActiveSubtab(false), 'gantt', 'a fresh load restores Gantt');
  second.sb._projectsPaint();
  assert.equal(activeBody(second.out.view), 'gantt');

  const bogus = load({ hash: '', storage: makeStorage({ [KEY]: 'sideways' }) });
  assert.equal(bogus.sb._projectsActiveSubtab(false), 'overview', 'an unknown stored value falls back to Overview');
});

test('a localStorage that throws does not break rendering', async () => {
  const { sb, out } = await open({ throwStorage: true });
  assert.equal(sb._projectsActiveSubtab(false), 'overview');
  assert.deepEqual(buttons(out.view), ORDER, 'the strip still renders');
  assert.equal(activeBody(out.view), 'overview');
  assert.doesNotThrow(() => sb.selectProjectsSubtab('arcs'));
  assert.equal(activeBody(out.view), 'arcs', 'the choice still applies for this page');
});

test('Events is hidden while empty and a stored events choice falls back to Overview', async () => {
  const empty = await open({ hash: '', storage: makeStorage({ [KEY]: 'events' }), rollup: ROLLUP_NO_EVENTS });
  run(empty.sb, "_projectsState.selected = 'acme'; _projectsState.subtab = ''");
  empty.sb._projectsPaint();
  assert.ok(!buttons(empty.out.view).includes('events'), 'no Events button without events');
  assert.equal(activeBody(empty.out.view), 'overview', 'a stored events choice falls back to Overview');
  assert.equal(empty.sb._projectsActiveSubtab(false), 'overview');

  const full = await open({ rollup: ROLLUP_EVENTS });
  assert.ok(buttons(full.out.view).includes('events'), 'the Events button shows when there are events');
  assert.equal(full.sb._projectsActiveSubtab(true), 'overview');
  run(full.sb, "_projectsState.subtab = 'events'");
  assert.equal(full.sb._projectsActiveSubtab(true), 'events', 'events is honoured while it has content');
});

test('phone layout: the strip is a tablist that scrolls sideways', async () => {
  const { sb, out } = await open({ phone: true, rollup: ROLLUP_EVENTS });
  assert.equal(run(sb, 'window.innerWidth'), 375);
  assert.ok(sb.matchMedia('(max-width: 600px)').matches, 'the stub viewport is phone width');
  assert.match(out.view, /<div class="projects-subtabs" role="tablist"/);
  assert.equal((out.view.match(/role="tablist"/g) || []).length, 1);
  assert.equal((out.view.match(/role="tab"/g) || []).length, ORDER.length);
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const rule = css.match(/\.projects-subtabs\s*\{([^}]*)\}/);
  assert.ok(rule, 'style.css has the strip rule');
  assert.match(rule[1], /overflow-x:\s*auto/);
});

test('the picker and the show-empty checkbox are above the strip', async () => {
  const { out } = await open();
  const picker = out.view.indexOf('id="projectsPicker"');
  const show = out.view.indexOf('id="projectsShowEmpty"');
  const strip = out.view.indexOf('class="projects-subtabs"');
  assert.ok(picker >= 0 && show >= 0 && strip >= 0, 'all three are rendered');
  assert.ok(picker < strip, 'picker comes before the strip');
  assert.ok(show < strip, 'show-empty checkbox comes before the strip');
});

test('#projects and #projects/acme both open Overview', async () => {
  const storage = makeStorage({ [KEY]: 'gantt' });
  for (const hash of ['#projects', '#projects/acme']) {
    const { sb, out } = await open({ hash, storage });
    assert.equal(run(sb, '_projectsState.subtab'), 'overview', hash + ' opens Overview despite the stored Gantt');
    if (hash === '#projects/acme') {
      assert.equal(activeBody(out.view), 'overview');
      assert.match(out.view, /id="projectsSubtab_overview"[^>]*aria-selected="true"/);
    }
  }
});

test('the Gantt sub-tab draws the Gantt alone and keeps the size cap', async () => {
  const { sb, out } = await open({ hash: '#projects/acme/gantt' });
  const rows = [];
  for (let i = 0; i < 60; i++) {
    rows.push({ kind: 'arc', id: 'a' + i, title: 'A' + i, state: 'active', first: new Date(Date.now() - (20 - i * 0.2) * 86400000).toISOString(), last: new Date(Date.now() - 1000).toISOString(), running: false, has_children: false, children_count: 0 });
  }
  run(sb, `_projectsState.selected = 'acme'; _projectsState.gantt = { name: 'acme', rows: ${JSON.stringify(rows)}, next: '', error: '', loading: false, kids: {}, preset: 'all', view: null, expanded: {} }`);
  run(sb, "_projectsState.subtab = 'gantt'");
  sb._projectsPaint();
  assert.equal(activeBody(out.view), 'gantt');
  const body = out.view.slice(out.view.indexOf('id="projectsSubtabBody"'));
  assert.match(body, /id="projectGantt"/);
  assert.doesNotMatch(body, /project-merge-pipeline|project-policy-effective|id="projectEvents"|project-progress/, 'no other section is in the body');
  assert.match(body, /Showing 40 of 60 rows\./);
  assert.equal((body.match(/data-kind="arc"/g) || []).length, 40, 'at most 40 rows are drawn');
});

test('there is no Policy sub-tab: a policy choice falls back to Overview and the policy Settings link sits by the project name', async () => {
  const l = await open({ rollup: ROLLUP_EVENTS });
  assert.ok(!buttons(l.out.view).includes('policy'), 'no Policy button in the strip');
  assert.ok(!l.out.view.includes('data-projects-subtab="policy"'), 'no policy button element');
  l.sb.selectProjectsSubtab('policy');
  await flushAsync(10);
  assert.equal(l.sb._projectsActiveSubtab(true), 'overview', 'a stored policy choice falls back to Overview');
  assert.equal(activeBody(l.out.view), 'overview');
  assert.match(l.out.view, /onclick="_projectsOpenSettings\(&quot;project_policy&quot;\);return false;/, 'the policy Settings link is in the header row');
  assert.match(l.out.view, /class="project-settings-link"/);
  assert.doesNotMatch(l.out.view, /data-projects-subtab-body="policy"/);

  // A hashed policy address does the same.
  const hash = load({ hash: '#projects/acme/policy', storage: makeStorage() });
  await hash.sb.renderProjectsView();
  await flushAsync(30);
  assert.equal(activeBody(hash.out.view), 'overview', 'a #projects/acme/policy address opens Overview');
  assert.match(hash.out.view, /onclick="_projectsOpenSettings\(&quot;project_policy&quot;\);return false;/);
});
