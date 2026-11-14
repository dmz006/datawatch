// Projects page phone overflow — at a 360px viewport no part of the Projects
// page may be wider than the screen on any sub-tab.
//
// The test harness (testutil_browser_stub) has no layout engine, so a rendered
// width cannot be measured. Instead this file asserts on the generated markup
// (no element carries a fixed pixel width or min-width above 360, the picker and
// toolbar carry the shrink hooks, long lines carry their wrap class) and on the
// style.css rules those classes resolve to (min-width 0 / max-width 100%,
// overflow-wrap, overflow-x auto inside the sub-tab row, pre and table scroll in
// their own box).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const CSS = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
const VIEWPORT = 360;
const DAY = 86400000;

function load() {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  vm.runInContext('_projectsShowEmpty = function () { return true; }', sandbox);
  return sandbox;
}

const LONG = 'a-very-long-project-directory-name-with-no-break-points-at-all-'.repeat(3);
const FULL = { name: 'acme', dir: '/work/' + LONG, owner: 'dana', weekly_share: 40, description: LONG };
// The Automaton carries no session, so the Events sub-tab shows the
// "has no sessions yet" line.
const ROLLUP = {
  arcs: [{ automation_id: 'auto-a', priority: 1, state: 'active', tasks: { done: 1, total: 2 }, automata: [
    { id: 'prd-1', title: 'Login', status: 'running', session_ids: [] }] }],
  unassigned: { automata: [{ id: 'loose-1', title: 'Loose', session_ids: ['s-9'] }] },
};
function iso(ms) { return new Date(ms).toISOString(); }
function hist() {
  const now = Date.now();
  return {
    name: 'acme', next: '', error: '',
    items: [{ at: iso(now - DAY), kind: 'automaton_completed', automaton_id: 'prd-1' }],
    gantt: [{ kind: 'automaton', automaton_id: 'prd-1', automation_id: 'auto-a', title: 'Login', state: 'completed', start: iso(now - 5 * DAY), end: iso(now - 3 * DAY) }],
  };
}
function page(sb, sub) {
  vm.runInContext(`_projectsState.subtab = ${JSON.stringify(sub)}`, sb);
  vm.runInContext(`_projectsSummary = { name: 'acme', error: '', data: { header: {}, progress: { automata: { done: 0, total: 0, percent: 0 }, points: 0 }, active_plans: [], next: null, counts: {} } }`, sb);
  return sb._projectPageHTML(FULL, ROLLUP, '', hist());
}

// rulesFor returns the declaration text of every rule whose selector list
// names the class (the pattern also reaches rules nested in an @media block).
function rulesFor(cls) {
  const re = /([^{}]+)\{([^{}]*)\}/g;
  const hit = new RegExp('(^|[^\\w-])\\.' + cls + '([^\\w-]|$)');
  const bodies = [];
  let m;
  while ((m = re.exec(CSS))) {
    if (m[1].split(',').some(sel => hit.test(sel.trim()) || sel.trim() === cls)) bodies.push(m[2]);
  }
  return bodies.join(' ');
}
const has = (body, decl) => new RegExp('(^|[;\\s])' + decl.replace(/[-\s]/g, c => (c === ' ' ? '\\s*' : c)) + '\\s*(;|$)').test(body);
const shrinks = body => has(body, 'min-width: 0') || has(body, 'max-width: 100%');

const SUBTABS = ['overview', 'plans', 'records', 'gantt', 'unassigned', 'history', 'events', 'policy'];

test('no sub-tab carries a fixed pixel width above the 360px viewport', () => {
  const sb = load();
  const SUBS = JSON.parse(vm.runInContext('JSON.stringify(_PROJECTS_SUBTABS)', sb));
  for (const s of SUBS) assert.ok(SUBTABS.includes(s), s + ' is covered by this test');
  for (const sub of SUBTABS) {
    const html = page(sb, sub);
    for (const m of html.matchAll(/(?:^|[;"'\s])(min-width|width)\s*:\s*(\d+(?:\.\d+)?)px/g)) {
      // The Gantt chart is the one fixed-width surface; it sits inside its own
      // overflow-x:auto wrapper (project-gantt-scroll), so it never widens the page.
      if (sub === 'gantt') continue;
      assert.ok(Number(m[2]) <= VIEWPORT, `${sub}: ${m[1]}:${m[2]}px is wider than ${VIEWPORT}px`);
    }
    assert.ok(!/\b\d*\.?\d+\s*(vw)\b/.test(html), sub + ': no vw width');
  }
  const now = Date.now();
  const rows = [{ kind: 'arc', id: 'auto-a', title: 'Login', state: 'done', first: iso(now - 5 * DAY), last: iso(now - 3 * DAY), running: false, has_children: true, children_count: 1 }];
  vm.runInContext(`_projectsState.selected = 'acme'; _projectsState.gantt = { name: 'acme', rows: ${JSON.stringify(rows)}, next: '', error: '', loading: false, kids: {}, preset: 'all', view: null, expanded: {} }`, sb);
  const gantt = page(sb, 'gantt');
  assert.match(gantt, /class="project-gantt-scroll"/, 'the Gantt chart sits in its own scroll box');
  assert.ok(has(rulesFor('project-gantt-scroll'), 'overflow-x: auto'), 'the Gantt wrapper scrolls sideways');
});

test('the sub-tab row scrolls inside its own box and does not widen its parent', () => {
  const sb = load();
  for (const sub of SUBTABS) {
    const html = page(sb, sub);
    assert.match(html, /<div class="projects-subtabs" role="tablist"/, sub + ': strip drawn');
    assert.match(html, /<div class="project-detail-card"/, sub + ': card drawn');
  }
  const strip = rulesFor('projects-subtabs');
  assert.ok(has(strip, 'overflow-x: auto'), 'strip has overflow-x:auto');
  assert.ok(has(strip, 'min-width: 0') && has(strip, 'max-width: 100%'), 'strip has min-width 0 and max-width 100%');
  const card = rulesFor('project-detail-card');
  assert.ok(has(card, 'min-width: 0') && has(card, 'max-width: 100%') && has(card, 'box-sizing: border-box'), 'the parent card shrinks');
});

test('the selector and toolbar row wrap and shrink; the checkbox label may wrap on a phone', () => {
  const sb = load();
  const html = sb._projectsPickerHTML([FULL], 'acme', '');
  assert.match(html, /id="projectsView" class="projects-view"/);
  assert.match(html, /class="projects-toolbar"/);
  assert.match(html, /id="projectsPicker" class="projects-picker"/);
  assert.match(html, /<label for="projectsShowEmpty" class="projects-show-empty"/);
  const label = /<label for="projectsShowEmpty"[^>]*>/.exec(html)[0];
  assert.ok(!/white-space/.test(label), 'the label carries no inline white-space (the class sets it)');
  assert.ok(!/width:\s*\d+px/.test(html.replace(/max-width:\s*\d+px/g, '')), 'no fixed pixel width in the toolbar');

  const toolbar = rulesFor('projects-toolbar');
  assert.ok(has(toolbar, 'flex-wrap: wrap'), 'toolbar wraps');
  assert.ok(shrinks(toolbar), 'toolbar shrinks');
  assert.ok(has(rulesFor('projects-picker'), 'min-width: 0'), 'selector shrinks');
  assert.ok(shrinks(rulesFor('projects-view')), 'the page container shrinks');
  const media = /@media \(max-width: 600px\) \{([^@]*?)\n\}/.exec(CSS.slice(CSS.indexOf('.projects-subtabs {')));
  assert.ok(media, 'a phone media block follows the Projects rules');
  assert.match(media[1], /\.projects-show-empty\s*\{[^}]*white-space:\s*normal/, 'the label wraps at phone width');
});

test('every card in the page can shrink and stays inside its parent', () => {
  for (const cls of ['project-detail-card', 'project-section-card', 'project-section-body', 'project-cards', 'project-card', 'project-card-card', 'project-card-tile', 'project-automaton', 'project-arc']) {
    const body = rulesFor(cls);
    assert.ok(has(body, 'max-width: 100%'), cls + ' max-width:100%');
    assert.ok(has(body, 'min-width: 0'), cls + ' min-width:0');
    assert.ok(has(body, 'box-sizing: border-box'), cls + ' box-sizing:border-box');
  }
  assert.match(rulesFor('project-cards-tiles'), /minmax\(min\(220px, 100%\), 1fr\)/, 'tiles shrink below 220px on a phone');
});

test('long text lines wrap, including the "has no sessions yet" line', () => {
  const sb = load();
  const html = page(sb, 'events');
  const line = /<div class="project-event-line"[^>]*>([^<]*has no sessions yet[^<]*)<\/div>/.exec(html);
  assert.ok(line, 'the no-sessions line carries project-event-line');
  assert.ok(has(rulesFor('project-event-line'), 'overflow-wrap: anywhere'), 'event lines wrap anywhere');
  assert.ok(has(rulesFor('project-detail-card'), 'min-width: 0'));
  assert.ok(has(rulesFor('projects-view'), 'overflow-wrap: anywhere'), 'the page wraps long text anywhere');
  assert.ok(/#projectsSubtabBody\s*\{[^}]*overflow-wrap:\s*anywhere/.test(CSS), 'the sub-tab body wraps long text anywhere');
  assert.ok(has(rulesFor('project-card-detail'), 'overflow-wrap: anywhere'), 'card detail lines wrap');
  assert.ok(has(rulesFor('project-overview-dir'), 'overflow-wrap: anywhere'), 'the directory line wraps');
  // The Overview sub-tab holds the long description and directory.
  const overview = page(sb, 'overview');
  assert.ok(overview.includes(LONG.slice(0, 40)), 'the long text is drawn on Overview');
});

test('pre and table content scrolls inside its own box', () => {
  assert.match(CSS, /\.project-detail-card pre, \.projects-view pre \{[^}]*max-width: 100%;[^}]*overflow-x: auto/);
  assert.match(CSS, /\.project-detail-card table, \.projects-view table \{[^}]*display: block;[^}]*max-width: 100%;[^}]*overflow-x: auto/);
  assert.match(CSS, /#projectsManageForm, #projectsManageForm > \* \{[^}]*min-width: 0/);
  // The Manage list is a table: it is drawn inside the Projects view.
  const sb = load();
  assert.match(sb._projectsPickerHTML([FULL], 'acme', ''), /<table id="projectsManageList"/);
});

test('style.css braces stay balanced', () => {
  const open = (CSS.match(/\{/g) || []).length;
  const close = (CSS.match(/\}/g) || []).length;
  assert.equal(open, close);
});
