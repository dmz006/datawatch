'use strict';
// Header scope selector — model, persistence, URL, icon button, modal, banner.
//
// The scope is what the Automata pages are filtered to: All, one project, or
// one plan (a project arc keyed by its automation id). This file pins:
//   (a) the model: getScope / setScope / clearScope / _scopeActive / _scopeLabel,
//       a plan implies its project, one dw_scope key, blocked storage is safe;
//   (b) boot: a remembered scope is restored at once and validated when the
//       project list arrives (a removed project or plan falls back and the
//       fallback is saved; a failed list never clears the scope);
//   (c) the URL: ?plan= on the #projects hash, every older form still parses,
//       a hash sets the scope, setScope rewrites the hash, the in-page picker
//       goes through setScope;
//   (d) the filter helpers: _scopeMatchesPrd, _scopePlanPrdIds and the stale
//       reply guard;
//   (e) the header button and its badge, and the banner;
//   (f) the modal: dialog semantics, rows, aria-current, applying a choice,
//       Escape / backdrop / focus return, loading and error states;
//   (g) locales and CSS.
//
// No daemon, no tmux: the real app.js runs in a vm sandbox.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-scope.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const INDEX_HTML = path.join(WEB, 'index.html');
const STYLE_CSS = path.join(WEB, 'style.css');
const KEYS = [
  'header_scope_button_label', 'header_scope_all', 'header_scope_modal_title',
  'header_scope_project_heading', 'header_scope_plan_heading', 'header_scope_all_plans',
  'header_scope_badge_label', 'header_scope_banner', 'header_scope_clear',
  'header_scope_help_label', 'header_scope_templates_note',
];

const PROJECTS = [
  { name: 'acme', dir: '/work/acme', arcs: [{ automation_id: 'au-1' }, { automation_id: 'au-2' }] },
  { name: 'zeta', dir: '/work/zeta', arcs: [] },
  { name: 'odd <b>&"', dir: '/work/odd', arcs: [{ automation_id: 'au-9' }] },
];
const AUTOMATIONS = [
  { id: 'au-1', name: 'Launch plan' },
  { id: 'au-2', name: 'Cleanup plan' },
];

function json(body) {
  return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
}

// load builds the sandbox. fetches maps a URL fragment to a reply (a function
// returning a promise, or a body). store seeds localStorage (a shared Map lets
// a second load model "a fresh page on the same device").
function load({ hash = '', store = null, projects = PROJECTS, projectsFail = false, view = 'autonomous', tab = 'projects', fetches = {} } = {}) {
  const sandbox = buildSandbox({ initialHash: hash });
  const dom = installFakeDom(sandbox, { statics: ['headerScopeBtn', 'headerScopeBadge', 'view', 'automataPanel'] });
  ['view', 'automataPanel'].forEach(id => { dom.el(id).insertAdjacentHTML = () => {}; });
  const calls = [];
  sandbox.fetch = (url) => {
    const u = String(url);
    calls.push(u);
    for (const k of Object.keys(fetches)) {
      if (u.includes(k)) { const r = fetches[k]; return typeof r === 'function' ? r(u) : json(r); }
    }
    if (u.endsWith('/api/projects')) {
      if (projectsFail) return Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('boom <b>'), json: () => Promise.reject(new Error('x')) });
      return json(projects);
    }
    if (u.endsWith('/api/automations')) return json(AUTOMATIONS);
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
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`state.activeView = ${JSON.stringify(view)}; _automataState.tab = ${JSON.stringify(tab)};`, sandbox);
  sandbox.__dom = dom;
  sandbox.__calls = calls;
  sandbox.__replaced = replaced;
  sandbox.renderAutonomousView = () => { sandbox.__renderedAutomata = (sandbox.__renderedAutomata || 0) + 1; };
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);
function setList(sb, projects) { run(sb, `_projectsState.list = ${JSON.stringify(projects || PROJECTS)}; _projectsState.error = '';`); }
function stored(sb) { const v = sb.localStorage.getItem('dw_scope'); return v === null ? null : JSON.parse(v); }
function btn(sb) { return sb.__dom.el('headerScopeBtn'); }
function modal(sb) { return sb.__dom.byId('scopeModal'); }
function rows(sb) { const b = sb.__dom.byId('scopeModalBody'); return b ? b.querySelectorAll('button') : []; }
function rowBy(sb, attr, val) {
  const hit = rows(sb).find(r => r.getAttribute(attr) === val);
  assert.ok(hit, `no row with ${attr}=${JSON.stringify(val)}`);
  return hit;
}
function click(sb, row) { run(sb, row.getAttribute('onclick')); }

// ── (a) the model ──────────────────────────────────────────────────────────

test('(a) the scope starts as All and the model functions exist', () => {
  const sb = load();
  for (const f of ['getScope', 'setScope', 'clearScope', '_scopeActive', '_scopeLabel', '_scopeMatchesPrd', '_scopePlanPrdIds', '_scopeBannerHTML', 'renderScopeButton', 'openScopeModal', 'closeScopeModal']) {
    assert.equal(typeof sb[f], 'function', f + ' is defined');
  }
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: '', plan: '' });
  assert.equal(sb._scopeActive(), false);
  assert.equal(sb._scopeLabel(), 'All');
});

test('(a) setScope sets a project, then a plan of it; a plan implies its project; clearScope returns to All', () => {
  const sb = load();
  setList(sb);
  assert.equal(sb.setScope('acme', ''), true);
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: '' });
  assert.equal(sb._scopeActive(), true);
  assert.equal(sb._scopeLabel(), 'acme');
  sb.setScope('acme', 'au-1');
  assert.equal(sb._scopeLabel(), 'acme / au-1', 'with no name known the label falls back to the id');
  assert.equal(sb.setScope('acme', 'au-1'), false, 'the same scope again changes nothing');
  assert.equal(sb.setScope('', 'au-1'), true, 'a plan with no project clears to All');
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: '', plan: '' });
  sb.setScope('acme', 'au-2');
  assert.equal(sb.clearScope(), true);
  assert.equal(sb._scopeLabel(), 'All');
});

test('(a) the plan label is the Automation name when one is known', () => {
  const sb = load();
  run(sb, `_automationState.list = [{ id: 'au-1', name: 'Launch plan' }];`);
  sb.setScope('acme', 'au-1');
  assert.equal(sb._scopeLabel(), 'acme / Launch plan');
});

test('(a) one localStorage key, dw_scope, holds JSON and survives a fresh page', () => {
  const store = new Map();
  const sb = load({ store });
  sb.setScope('acme', 'au-2');
  assert.deepEqual([...store.keys()].filter(k => k.startsWith('dw_scope')), ['dw_scope']);
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-2' });
  const next = load({ store });
  assert.deepEqual(JSON.parse(JSON.stringify(next.getScope())), { project: 'acme', plan: 'au-2' }, 'restored at once, before any list');
  next.clearScope();
  assert.deepEqual(stored(next), { project: '', plan: '' });
});

test('(a) blocked storage leaves the scope in memory and breaks nothing', () => {
  const sb = load();
  sb.localStorage = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); }, removeItem() { throw new Error('blocked'); } };
  assert.doesNotThrow(() => sb.setScope('acme', 'au-1'));
  assert.equal(sb.getScope().project, 'acme');
  assert.doesNotThrow(() => sb.clearScope());
  const bad = load({ store: new Map([['dw_scope', '{not json']]) });
  assert.equal(bad._scopeActive(), false, 'unreadable saved text means All');
  const odd = load({ store: new Map([['dw_scope', JSON.stringify({ project: '', plan: 'x' })]]) });
  assert.deepEqual(JSON.parse(JSON.stringify(odd.getScope())), { project: '', plan: '' }, 'a saved plan without a project is dropped');
});

// ── (b) boot validation ────────────────────────────────────────────────────

test('(b) a remembered project that is no longer listed falls back to All and the fallback is saved', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'gone', plan: '' })]]);
  const sb = load({ store, view: 'sessions', tab: 'automata' });
  assert.equal(sb.getScope().project, 'gone', 'restored optimistically');
  sb._scopeBootKick();
  await flushAsync();
  assert.equal(sb._scopeActive(), false);
  assert.deepEqual(stored(sb), { project: '', plan: '' });
});

test('(b) a remembered plan that is no longer an arc of the project falls back to the project', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-gone' })]]);
  const sb = load({ store, view: 'sessions', tab: 'automata' });
  sb._scopeBootKick();
  await flushAsync();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: '' });
  assert.deepEqual(stored(sb), { project: 'acme', plan: '' });
});

test('(b) a still-valid remembered scope is kept', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-1' })]]);
  const sb = load({ store, view: 'sessions', tab: 'automata' });
  sb._scopeBootKick();
  await flushAsync();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: 'au-1' });
});

test('(b) a project list that fails to load never clears the remembered scope', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-1' })]]);
  const sb = load({ store, view: 'sessions', tab: 'automata', projectsFail: true });
  sb._scopeBootKick();
  await flushAsync();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: 'au-1' });
});

test('(b) with no scope active boot reads no project list', () => {
  const sb = load({ view: 'sessions', tab: 'automata' });
  sb._scopeBootKick();
  assert.equal(sb.__calls.filter(u => u.endsWith('/api/projects')).length, 0);
});

test('(b) boot calls _scopeBootKick without waiting on it', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /_scopeBootKick\(\);/);
  assert.ok(!/await _scopeBootKick/.test(src), 'boot never waits for the project list');
});

// ── (c) the URL ────────────────────────────────────────────────────────────

test('(c) every older #projects form still parses and builds', () => {
  const sb = load();
  assert.equal(sb._projectsHashParse('#projects'), '');
  assert.equal(sb._projectsHashParse('#projects/acme'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/acme/gantt'), 'acme');
  assert.equal(sb._projectsHashParse('#automata'), null);
  assert.equal(sb._projectsHashSubtab('#projects/acme/arcs'), 'plans');
  assert.equal(sb._projectsHashSubtab('#projects/acme'), '');
  assert.equal(sb._projectsHashBuild(''), '#projects');
  assert.equal(sb._projectsHashBuild('acme'), '#projects/acme');
  assert.equal(sb._projectsHashBuild('acme', 'gantt'), '#projects/acme/gantt');
  assert.equal(sb._projectsHashBuild('acme', 'overview'), '#projects/acme');
  assert.equal(sb._projectsHashPlan('#projects/acme'), '');
});

test('(c) the plan rides on a ?plan= suffix and every parser ignores it for its own part', () => {
  const sb = load();
  assert.equal(sb._projectsHashBuild('acme', '', 'au-1'), '#projects/acme?plan=au-1');
  assert.equal(sb._projectsHashBuild('acme', 'gantt', 'a b'), '#projects/acme/gantt?plan=a%20b');
  assert.equal(sb._projectsHashBuild('', 'gantt', 'au-1'), '#projects', 'a plan needs a project');
  assert.equal(sb._projectsHashParse('#projects/acme?plan=au-1'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/acme/gantt?plan=au-1'), 'acme');
  assert.equal(sb._projectsHashSubtab('#projects/acme/gantt?plan=au-1'), 'gantt');
  assert.equal(sb._projectsHashPlan('#projects/acme?plan=au-1'), 'au-1');
  assert.equal(sb._projectsHashPlan('#projects/acme/gantt?x=1&plan=a%20b'), 'a b');
  assert.equal(sb._projectsHashPlan('#projects?plan=au-1'), '', 'no project, no plan');
  assert.equal(sb._projectsHashPlan('#projects/acme?plan=%E0%A4%A'), '', 'a bad escape is no plan');
  const round = sb._projectsHashBuild('a b/c', 'plans', 'x&y');
  assert.equal(sb._projectsHashParse(round), 'a b/c');
  assert.equal(sb._projectsHashSubtab(round), 'plans');
  assert.equal(sb._projectsHashPlan(round), 'x&y');
});

test('(c) a hash naming a project and plan sets the scope when the Projects page opens', async () => {
  const store = new Map();
  const sb = load({ hash: '#projects/acme?plan=au-2', store });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: 'au-2' });
  assert.equal(run(sb, '_projectsState.selected'), 'acme');
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-2' });
});

test('(c) the Overview hash means All', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: '' })]]);
  const sb = load({ hash: '#projects', store });
  sb.renderProjectsView();
  await flushAsync();
  assert.equal(sb._scopeActive(), false);
  assert.equal(run(sb, '_projectsState.selected'), '');
});

test('(c) no hash keeps the remembered scope and the Projects page opens on it', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-1' })]]);
  const sb = load({ hash: '', store });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: 'au-1' });
  assert.equal(run(sb, '_projectsState.selected'), 'acme');
});

test('(c) with no scope and no hash the old remembered project still opens (and becomes the scope)', async () => {
  const sb = load({ hash: '' });
  sb.localStorage.setItem('cs_projects_last', 'zeta');
  sb.renderProjectsView();
  await flushAsync();
  assert.equal(run(sb, '_projectsState.selected'), 'zeta');
  assert.equal(sb.getScope().project, 'zeta', 'one source of truth');
});

test('(c) changing the scope on the Projects tab rewrites the hash with replaceState, keeping the sub-tab', () => {
  const sb = load({ hash: '#projects/acme/gantt' });
  setList(sb);
  run(sb, `_projectsState.selected = 'acme'; _projectsState.subtab = 'gantt';`);
  sb.setScope('acme', 'au-1', { silent: true });
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects/acme/gantt?plan=au-1');
  sb.setScope('acme', '', { silent: true });
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects/acme/gantt');
  sb.setScope('', '', { silent: true });
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects');
});

test('(c) off the Projects tab setScope leaves the address alone', () => {
  const sb = load({ tab: 'automata' });
  setList(sb);
  sb.setScope('acme', 'au-1', { silent: true });
  assert.equal(sb.__replaced.length, 0);
});

test('(c) choosing a sub-tab keeps the plan in the hash', () => {
  const sb = load({ hash: '#projects/acme' });
  setList(sb);
  sb.setScope('acme', 'au-1', { silent: true });
  run(sb, `selectProjectsSubtab('gantt')`);
  assert.equal(sb.__replaced[sb.__replaced.length - 1], '/#projects/acme/gantt?plan=au-1');
});

test('(c) the in-page picker goes through setScope and stays in sync', async () => {
  const store = new Map();
  const sb = load({ store });
  setList(sb);
  const seen = [];
  const real = sb.setScope;
  sb.setScope = function (p, pl, o) { seen.push([p, pl, !!(o && o.silent)]); return real.apply(this, arguments); };
  await sb.selectProjectsView('acme');
  assert.deepEqual(seen[0].slice(0, 2), ['acme', '']);
  assert.equal(run(sb, '_projectsState.selected'), 'acme');
  assert.deepEqual(stored(sb), { project: 'acme', plan: '' });
  assert.equal(sb.localStorage.getItem('cs_projects_last'), 'acme');
  await sb.selectProjectsView('');
  assert.equal(sb._scopeActive(), false);
  assert.equal(run(sb, '_projectsState.selected'), '');
});

test('(c) a scope change repaints the current Automata sub-page without a reload', async () => {
  const sbA = load({ tab: 'automata' });
  setList(sbA);
  sbA.setScope('acme', '');
  assert.equal(sbA.__renderedAutomata, 1, 'a non-Projects Automata sub-page goes through renderAutonomousView');
  sbA.setScope('acme', '');
  assert.equal(sbA.__renderedAutomata, 1, 'no change, no repaint');
  const sbP = load({ tab: 'projects' });
  setList(sbP);
  let painted = 0;
  sbP._projectsPaint = () => { painted++; };
  sbP._projectsLoadSelected = () => Promise.resolve();
  sbP.setScope('acme', '');
  await flushAsync();
  assert.ok(painted >= 1, 'the Projects tab repaints through _projectsPaint');
  assert.equal(sbP.__renderedAutomata, undefined);
  const sbS = load({ view: 'sessions', tab: 'automata' });
  sbS.setScope('acme', '');
  assert.equal(sbS.__renderedAutomata, undefined, 'off the Automata view nothing repaints');
});

// ── (d) filter helpers ─────────────────────────────────────────────────────

const PRD = (id, project, extra) => Object.assign({ id, project }, extra || {});

test('(d) _scopeMatchesPrd is true for everything under All', () => {
  const sb = load();
  assert.equal(sb._scopeMatchesPrd(PRD('a', 'acme')), true);
  assert.equal(sb._scopeMatchesPrd(PRD('b', '')), true);
  assert.equal(sb._scopeMatchesPrd(null), true);
});

test('(d) a project scope matches on the Automaton\'s project field only', () => {
  const sb = load();
  setList(sb);
  sb.setScope('acme', '', { silent: true });
  assert.equal(sb._scopeMatchesPrd(PRD('a', 'acme')), true);
  assert.equal(sb._scopeMatchesPrd(PRD('b', 'zeta')), false);
  assert.equal(sb._scopeMatchesPrd(PRD('c', '')), false);
  assert.equal(sb._scopeMatchesPrd({ id: 'd' }), false);
  assert.equal(sb._scopeMatchesPrd(null), false);
});

test('(d) a plan scope reads the Automation once, caches the member ids and matches only members', async () => {
  let reads = 0;
  const sb = load({
    fetches: {
      '/api/automations/au-1': () => { reads++; return json({ id: 'au-1', members: [{ id: 'm1', prd_id: 'p1' }, { id: 'm2', prd_id: 'p2' }, { id: 'm3', prd_id: '' }] }); },
    },
  });
  setList(sb);
  sb.setScope('acme', 'au-1', { silent: true });
  assert.equal(sb._scopePlanPrdIds().size, 0, 'empty until the reply arrives');
  await flushAsync();
  const ids = sb._scopePlanPrdIds();
  assert.deepEqual([...ids].sort(), ['p1', 'p2']);
  sb._scopePlanPrdIds();
  sb._scopeMatchesPrd(PRD('p1', 'acme'));
  assert.equal(reads, 1, 'loaded once per plan');
  assert.equal(sb._scopeMatchesPrd(PRD('p1', 'acme')), true);
  assert.equal(sb._scopeMatchesPrd(PRD('p1', 'elsewhere')), true, 'a plan implies its project: a member matches');
  assert.equal(sb._scopeMatchesPrd(PRD('p9', 'acme')), false, 'same project, not in the plan');
  assert.equal(sb._scopeMatchesPrd(PRD('x', 'acme', { automation_id: 'au-1' })), true, 'the Automaton\'s own Automation link also counts');
});

test('(d) the plan cache is cleared on a scope change and a late reply for the older scope is dropped', async () => {
  const pending = {};
  const sb = load({
    fetches: {
      '/api/automations/au-1': () => new Promise(res => { pending.au1 = res; }),
      '/api/automations/au-2': () => json({ members: [{ prd_id: 'q1' }] }),
    },
  });
  setList(sb);
  sb.setScope('acme', 'au-1', { silent: true });
  const tok1 = run(sb, '_scopeToken');
  sb._scopePlanPrdIds();
  sb.setScope('acme', 'au-2', { silent: true });
  assert.ok(run(sb, '_scopeToken') > tok1, 'a scope change bumps the token');
  sb._scopePlanPrdIds();
  await flushAsync();
  pending.au1({ ok: true, status: 200, json: () => Promise.resolve({ members: [{ prd_id: 'late' }] }) });
  await flushAsync();
  assert.deepEqual([...sb._scopePlanPrdIds()], ['q1'], 'the late au-1 reply was dropped');
  sb.setScope('acme', '', { silent: true });
  assert.equal(sb._scopePlanPrdIds().size, 0, 'no plan, no ids');
});

test('(d) a failed Automation read leaves the plan with no members and never loops', async () => {
  let reads = 0;
  const sb = load({ fetches: { '/api/automations/au-1': () => { reads++; return Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('no'), json: () => Promise.reject(new Error('x')) }); } } });
  setList(sb);
  sb.setScope('acme', 'au-1', { silent: true });
  sb._scopePlanPrdIds();
  await flushAsync();
  sb._scopePlanPrdIds();
  sb._scopePlanPrdIds();
  assert.equal(reads, 1);
  assert.equal(sb._scopePlanPrdIds().size, 0);
});

// ── (e) button, badge, banner ──────────────────────────────────────────────

test('(e) index.html carries the scope button beside the server button, before the identity button', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  const header = src.slice(src.indexOf('<header class="header">'), src.indexOf('</header>'));
  assert.equal((src.match(/id="headerScopeBtn"/g) || []).length, 1);
  const tag = header.match(/<button\b[^>]*id="headerScopeBtn"[^>]*>/)[0];
  assert.match(tag, /type="button"/);
  assert.match(tag, /class="header-scope-btn"/);
  assert.match(tag, /aria-haspopup="dialog"/);
  assert.match(tag, /aria-expanded="false"/);
  assert.match(tag, /onclick="openScopeModal\(\)"/);
  assert.match(tag, /style="display:none"/);
  assert.match(tag, /aria-label="[^"]+"/);
  const at = header.indexOf('id="headerScopeBtn"');
  assert.ok(header.indexOf('id="serverPickerBtn"') < at, 'after the server button');
  assert.ok(at < header.indexOf('id="headerIdentityBtn"'), 'before the identity button');
  const block = header.slice(at, header.indexOf('</button>', at));
  assert.match(block, /<svg[^>]*aria-hidden="true"/, 'the icon is decorative');
  assert.match(block, /id="headerScopeBadge"/);
  assert.ok(!/<span[^>]*>\s*[A-Za-z]/.test(block.replace(/<span id="headerScopeBadge"[^>]*><\/span>/, '')), 'icon only: no visible text');
});

test('(e) renderScopeButton sets aria-label and title from header_scope_button_label and shows the badge only while scoped', () => {
  const sb = load();
  setList(sb);
  sb.renderScopeButton();
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: All');
  assert.equal(btn(sb).getAttribute('title'), 'Scope: All');
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'false');
  const badge = sb.__dom.el('headerScopeBadge');
  assert.equal(badge.style.display, 'none');
  assert.equal(badge.innerHTML, '');
  sb.setScope('acme', 'au-2', { silent: true });
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: acme / au-2');
  assert.equal(badge.style.display, '');
  assert.match(badge.innerHTML, /header-scope-badge-text/);
  assert.match(badge.innerHTML, /Filter active/, 'visually hidden text comes from header_scope_badge_label');
  sb.clearScope();
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: All');
  assert.equal(badge.style.display, 'none');
});

test('(e) the label is data, never markup', () => {
  const sb = load();
  setList(sb);
  sb.setScope('odd <b>&"', '', { silent: true });
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: odd <b>&"');
  const banner = sb._scopeBannerHTML();
  assert.ok(!banner.includes('<b>'), 'the banner escapes the project name');
  assert.match(banner, /odd &lt;b&gt;&amp;&quot;/);
});

test('(e) navigate shows the button only on the Automata view and keeps the remembered scope while hidden', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /headerScopeBtn\.style\.display = view === 'autonomous' \? 'inline-flex' : 'none';/);
  const nav = src.slice(src.indexOf('function navigate('), src.indexOf('// ── Projects view'));
  assert.ok(nav.indexOf('headerIdentityBtn') < nav.indexOf('headerScopeBtn'), 'next to the identity button logic');
  assert.match(nav, /renderScopeButton\(\);/);
  assert.ok(!/clearScope|setScope\(/.test(nav), 'navigate never clears the scope');
});

test('(e) the banner is empty under All and otherwise says what the page is filtered to, with a Clear button', () => {
  const sb = load({ tab: 'automata' });
  setList(sb);
  assert.equal(sb._scopeBannerHTML(), '');
  sb.setScope('acme', '', { silent: true });
  let html = sb._scopeBannerHTML();
  assert.match(html, /<div class="scope-banner" role="status">/);
  assert.match(html, /Filtered to acme/);
  assert.match(html, /<button type="button"[^>]*onclick="clearScope\(\)"[^>]*>Clear<\/button>/);
  sb.setScope('acme', 'au-1', { silent: true });
  html = sb._scopeBannerHTML();
  assert.match(html, /Filtered to acme \/ au-1/);
  run(sb, 'clearScope()');
  assert.equal(sb._scopeBannerHTML(), '');
});

// ── (f) the modal ──────────────────────────────────────────────────────────

async function openModal(sb) {
  run(sb, 'openScopeModal()');
  await flushAsync();
  return modal(sb);
}

test('(f) the modal is a labelled dialog with Close, a help link to the projects how-to, and focus inside', async () => {
  const sb = load({ tab: 'automata' });
  const m = await openModal(sb);
  assert.ok(m, 'scopeModal overlay is in the DOM');
  const dlg = m.querySelector('[role="dialog"]');
  assert.equal(dlg.getAttribute('aria-modal'), 'true');
  assert.equal(dlg.getAttribute('aria-labelledby'), 'scopeModalTitle');
  assert.equal(sb.__dom.byId('scopeModalTitle').textContent, 'Scope');
  const help = m.querySelector('a.header-modal-help');
  assert.match(help.getAttribute('href'), /#docs\/howto\/projects-tab\.md$/);
  assert.equal(help.getAttribute('aria-label'), 'Help: scope');
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'true');
  assert.ok(m.contains(sb.__dom.active()), 'focus moved into the dialog');
  assert.match(sb.__dom.byId('scopeModalBody').innerHTML, /Templates are shown for every project and plan\./);
});

test('(f) the rows are real buttons: All, then the Project heading and one row per project', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  const all = rows(sb).filter(r => r.getAttribute('data-scope-all'));
  assert.equal(all.length, 1);
  assert.equal(all[0].getAttribute('aria-current'), 'true', 'All is current under All');
  const names = rows(sb).filter(r => r.getAttribute('data-scope-project') !== null).map(r => r.getAttribute('data-scope-project'));
  assert.deepEqual(names, ['acme', 'zeta', 'odd <b>&"']);
  assert.ok(rows(sb).every(r => r.tagName === 'BUTTON' && r.getAttribute('type') === 'button' || r.classList.contains('header-modal-close')));
  const body = sb.__dom.byId('scopeModalBody').innerHTML;
  assert.match(body, />Project</);
  assert.ok(!/>Plan</.test(body), 'no Plan heading before a project is chosen');
  assert.ok(!body.includes('<b>&"'), 'project names are escaped');
});

test('(f) choosing All clears the scope at once and closes the modal with focus on the header button', async () => {
  const sb = load({ tab: 'automata' });
  setList(sb);
  sb.setScope('acme', '', { silent: true });
  await openModal(sb);
  click(sb, rows(sb).find(r => r.getAttribute('data-scope-all')));
  assert.equal(sb._scopeActive(), false);
  assert.equal(modal(sb), null, 'closed');
  assert.equal(sb.__dom.active(), btn(sb), 'focus returned to #headerScopeBtn');
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'false');
  assert.equal(sb.__dom.listenerCount('keydown'), 0, 'no keydown listener left behind');
});

test('(f) choosing a project with plans shows its plans, All plans and one row per plan; nothing applies yet', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  click(sb, rowBy(sb, 'data-scope-project', 'acme'));
  assert.equal(sb._scopeActive(), false, 'the project is not applied until a plan choice');
  assert.ok(modal(sb), 'still open');
  const body = sb.__dom.byId('scopeModalBody').innerHTML;
  assert.match(body, />Plan</);
  assert.ok(rowBy(sb, 'data-scope-all-plans', '1'));
  const plans = rows(sb).filter(r => r.getAttribute('data-scope-plan') !== null).map(r => r.getAttribute('data-scope-plan'));
  assert.deepEqual(plans, ['au-1', 'au-2']);
  assert.equal(sb.__dom.active().getAttribute('data-scope-project'), 'acme', 'focus stays on the project row');
  assert.equal(rowBy(sb, 'data-scope-project', 'acme').getAttribute('aria-expanded'), 'true');
});

test('(f) plan rows carry the readable Automation name once it is read', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  await flushAsync();
  click(sb, rowBy(sb, 'data-scope-project', 'acme'));
  const labels = rows(sb).filter(r => r.getAttribute('data-scope-plan') !== null).map(r => r.textContent);
  assert.deepEqual(labels, ['Launch plan', 'Cleanup plan']);
});

test('(f) All plans applies the project at once and closes', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  click(sb, rowBy(sb, 'data-scope-project', 'acme'));
  click(sb, rowBy(sb, 'data-scope-all-plans', '1'));
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: '' });
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
});

test('(f) choosing a plan applies project and plan and closes', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  click(sb, rowBy(sb, 'data-scope-project', 'acme'));
  click(sb, rowBy(sb, 'data-scope-plan', 'au-2'));
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'acme', plan: 'au-2' });
  assert.equal(modal(sb), null);
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-2' });
});

test('(f) a project with no plans applies at once on its own row', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  click(sb, rowBy(sb, 'data-scope-project', 'zeta'));
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'zeta', plan: '' });
  assert.equal(modal(sb), null);
});

test('(f) a hostile project name round-trips through the row handler as data', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  click(sb, rowBy(sb, 'data-scope-project', 'odd <b>&"'));
  click(sb, rowBy(sb, 'data-scope-plan', 'au-9'));
  assert.deepEqual(JSON.parse(JSON.stringify(sb.getScope())), { project: 'odd <b>&"', plan: 'au-9' });
});

test('(f) active rows carry aria-current: the project and the plan of the current scope', async () => {
  const sb = load({ tab: 'automata' });
  setList(sb);
  sb.setScope('acme', 'au-2', { silent: true });
  await openModal(sb);
  assert.equal(rowBy(sb, 'data-scope-project', 'acme').getAttribute('aria-current'), 'true');
  assert.equal(rowBy(sb, 'data-scope-plan', 'au-2').getAttribute('aria-current'), 'true');
  assert.equal(rowBy(sb, 'data-scope-plan', 'au-1').getAttribute('aria-current'), null);
  assert.equal(rowBy(sb, 'data-scope-all-plans', '1').getAttribute('aria-current'), null);
  assert.equal(rows(sb).find(r => r.getAttribute('data-scope-all')).getAttribute('aria-current'), null);
  assert.equal(sb.__dom.active().getAttribute('aria-current'), 'true', 'focus lands on an active row');
});

test('(f) Escape and a backdrop click close it and return focus to the header button', async () => {
  const sb = load({ tab: 'automata' });
  await openModal(sb);
  sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
  assert.equal(sb.__dom.listenerCount('keydown'), 0);
  const m = await openModal(sb);
  m.dispatch('click', { target: m });
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
  await openModal(sb);
  run(sb, "document.getElementById('scopeModal').querySelector('.header-modal-close').getAttribute('onclick')");
  const close = modal(sb).querySelector('.header-modal-close');
  run(sb, close.getAttribute('onclick'));
  assert.equal(modal(sb), null);
  for (let i = 0; i < 3; i++) { await openModal(sb); run(sb, 'closeScopeModal()'); }
  assert.equal(sb.__dom.listenerCount('keydown'), 0, 'repeated open/close leaks no listener');
});

test('(f) while the project list loads the modal shows the loading block, then re-renders when it arrives', async () => {
  let release;
  const sb = load({
    tab: 'automata',
    fetches: { '/api/projects': () => new Promise(res => { release = () => res({ ok: true, status: 200, json: () => Promise.resolve(PROJECTS) }); }) },
  });
  run(sb, 'openScopeModal()');
  assert.equal(rows(sb).length, 0, 'no rows yet');
  assert.match(sb.__dom.byId('scopeModalBody').textContent, /Loading/, 'the loading block is drawn');
  assert.ok(sb.__calls.some(u => u.endsWith('/api/projects')), 'the modal asked for the list');
  release();
  await flushAsync();
  assert.ok(rowBy(sb, 'data-scope-project', 'acme'), 'rows arrived');
  assert.equal(run(sb, '_scopeListWanted'), 0);
});

test('(f) a load error shows the localized error text and still offers All', async () => {
  const sb = load({ tab: 'automata', projectsFail: true });
  await openModal(sb);
  const body = sb.__dom.byId('scopeModalBody').innerHTML;
  assert.match(body, /role="alert"/);
  assert.match(body, /Could not load projects:/);
  assert.match(body, /boom &lt;b&gt;/, 'the failure text is escaped');
  assert.ok(rows(sb).find(r => r.getAttribute('data-scope-all')), 'All is still there');
});

test('(f) the project list is read for the scope from any Automata sub-page, and a late reply off the page is still dropped by _projectsLoad when nothing wants it', async () => {
  const sb = load({ tab: 'automata' });
  await sb._projectsLoad();
  assert.equal(run(sb, '_projectsState.list'), null, 'nothing wants the list: dropped as before');
  const sb2 = load({ tab: 'automata' });
  run(sb2, '_scopeListWanted = 1');
  await sb2._projectsLoad();
  assert.equal(run(sb2, '_projectsState.list.length'), 3);
});

// ── (g) locales and CSS ────────────────────────────────────────────────────

test('(g) the eleven keys exist in en, de, es, fr and ja, with identical placeholders and no PRD wording or tracker IDs', () => {
  const bundles = {};
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) bundles[l] = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', l + '.json'), 'utf8'));
  for (const k of KEYS) {
    for (const l of Object.keys(bundles)) {
      assert.ok(typeof bundles[l][k] === 'string' && bundles[l][k].trim(), `${l} has ${k}`);
      assert.ok(!/\bPRDs?\b/i.test(bundles[l][k]), `${l}.${k} never says PRD`);
      assert.ok(!/\b(BL|GH|B)\d{2,}\b/.test(bundles[l][k]), `${l}.${k} has no tracker id`);
      const ph = s => (s.match(/%\d\$s/g) || []).join(',');
      assert.equal(ph(bundles[l][k]), ph(bundles.en[k]), `${l}.${k} keeps the placeholders`);
    }
  }
  assert.ok(bundles.en.header_scope_button_label.includes('%1$s'));
  assert.ok(bundles.en.header_scope_banner.includes('%1$s'));
  for (const l of ['de', 'es', 'fr', 'ja']) {
    const same = KEYS.filter(k => bundles[l][k] === bundles.en[k]);
    assert.ok(same.length <= 2, `${l} is translated (identical to English: ${same.join(', ')})`);
  }
});

test('(g) style.css has the button, badge and banner rules and the button keeps the server button no-wrap rules', () => {
  const css = fs.readFileSync(STYLE_CSS, 'utf8');
  assert.match(css, /\.header > \.header-scope-btn\s*\{[^}]*min-width:\s*44px;[^}]*white-space:\s*nowrap;/);
  assert.match(css, /\.header-scope-badge\s*\{/);
  assert.match(css, /\.header-scope-badge-text\s*\{[^}]*clip:/);
  assert.match(css, /\.scope-banner\s*\{/);
  assert.match(css, /\.scope-modal-row\s*\{/);
  assert.ok(!/\.header-scope-btn[^{]*\{[^}]*display:\s*none/.test(css), 'visibility is the inline style navigate() sets');
});
