// Merge-state badge on the Automata cards: a finished record whose working
// branch still has commits that never landed on the base branch carries the
// read-only merge_state value finished_not_merged (derived on the daemon,
// never persisted). Only that value renders a badge; absent or other values
// render nothing. The badge is wired into the card header next to the
// planning-failure badge.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('merge_state badge renders for finished_not_merged only', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderMergeStateBadge', { id: 'p1', status: 'completed', merge_state: 'finished_not_merged' });
  assert.match(html, /merge-state-badge/);
  assert.match(html, /prd_merge_state_badge/);
  assert.match(html, /finished_not_merged/);
});

test('merge_state badge renders nothing when the field is absent', () => {
  const sandbox = loadAppJS();
  assert.equal(call(sandbox, 'renderMergeStateBadge', { id: 'p1', status: 'active' }), '');
  assert.equal(call(sandbox, 'renderMergeStateBadge', null), '');
});

test('merge_state badge renders nothing for other values', () => {
  const sandbox = loadAppJS();
  assert.equal(call(sandbox, 'renderMergeStateBadge', { id: 'p1', merge_state: '' }), '');
  assert.equal(call(sandbox, 'renderMergeStateBadge', { id: 'p1', merge_state: 'merged' }), '');
});

test('the card header includes the merge_state badge slot', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/\$\{renderPlanningFailureBadge\(prd\)\}(.{0,120})/);
  assert.ok(m, 'card header line with the planning badge not found');
  assert.match(m[1], /\$\{renderMergeStateBadge\(prd\)\}/);
});

// The badge carries a "?" docs link (honoring the cs_show_docs_links default-on
// setting) that opens the Automaton merge state entry of the definitions
// glossary, and the wiring keeps it inline so the phone tile widths do not
// overflow.

test('the merge_state badge has the merge-state docs link', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderMergeStateBadge', { id: 'p1', status: 'completed', merge_state: 'finished_not_merged' });
  assert.match(html, /class="docs-link merge-state-help"/);
  assert.match(html, /#docs\/datawatch-definitions\.md#automaton-merge-state/);
});

test('the Projects card line includes the merge_state badge slot', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/\$\{pts\(a\.points\)\}<\/span\>(.{0,120})/);
  assert.ok(m, 'Projects card line not found');
  assert.match(m[1], /\$\{renderMergeStateBadge\(a\)\}/);
});

test('the Gantt row label includes the merge_state badge slot for Automata', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/project-gantt-label-text(.{0,200})/);
  assert.ok(m, 'Gantt row label not found');
  assert.match(m[1], /\$\{r\.kind === 'automaton' \? renderMergeStateBadge\(r\) : ''\}/);
});

test('the badge copy and help link exist in every locale bundle', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    assert.ok(bundle.prd_merge_state_badge && String(bundle.prd_merge_state_badge).trim(), lang + ' missing badge label');
    assert.ok(bundle.merge_state_help_link && String(bundle.merge_state_help_link).trim(), lang + ' missing help link label');
  }
});

// Projects surface rendering: the badge actually appears on the project page
// row and the Gantt automaton row when the data carries merge_state, and not
// otherwise.

test('the project page renders exactly one badge for a finished_not_merged Automaton', () => {
  const sandbox = loadAppJS();
  const project = { name: 'acme', dir: '/work/acme' };
  const rollup = {
    arcs: [{ automation_id: 'auto-a', priority: 1, state: 'active', tasks: { done: 1, total: 2 }, automata: [
      { id: 'prd-1', title: 'Login', status: 'completed', merge_state: 'finished_not_merged' },
      { id: 'prd-2', title: 'Cart', status: 'running' } ]}],
    unassigned: { automata: [] },
  };
  const hist = { name: 'acme', next: '', error: '', items: [], gantt: [] };
  const render = roll => {
    sandbox.__p = project;
    sandbox.__rollup = roll;
    sandbox.__hist = hist;
    vm.runInContext(`_projectsState.subtab = 'arcs'`, sandbox);
    return vm.runInContext(`_projectPageHTML(__p, __rollup, '', __hist)`, sandbox);
  };
  const with_ = render(rollup);
  assert.equal((with_.match(/merge-state-badge/g) || []).length, 1, 'one finished_not_merged automaton renders one badge');
  const without = render({ arcs: rollup.arcs.map(a => ({ ...a, automata: a.automata.filter(x => x.id !== 'prd-1') })), unassigned: rollup.unassigned });
  assert.ok(!without.includes('merge-state-badge'), 'no badge when no automaton carries merge_state');
});

test('the Gantt automaton row renders exactly one badge when merge_state is set', () => {
  const sandbox = loadAppJS();
  const iso = ms => new Date(ms).toISOString();
  const now = Date.now();
  vm.runInContext(`_projectsState.selected = 'acme'`, sandbox);
  const rowPending = { id: 'prd-1', kind: 'automaton', automation_id: 'auto-a', title: 'Login',
    status: 'completed', merge_state: 'finished_not_merged',
    start: iso(now - 3 * 24 * 3600 * 1000), end: iso(now - 1 * 24 * 3600 * 1000) };
  const rowPlain = { id: 'p9', kind: 'automaton', title: 'Plain',
    start: iso(now - 3 * 24 * 3600 * 1000), end: iso(now - 1 * 24 * 3600 * 1000) };
  const render = rows => {
    sandbox.__rows = rows;
    return vm.runInContext(`_projectGanttInnerHTML({ name: 'acme', preset: '14d', rows: __rows, kids: {}, expanded: {}, next: '' })`, sandbox);
  };
  const g = render([rowPending]);
  assert.equal((g.match(/merge-state-badge/g) || []).length, 1, 'finished_not_merged Gantt row renders one badge');
  assert.match(g, /finished_not_merged/);
  const clean = render([rowPlain]);
  assert.ok(!clean.includes('merge-state-badge'), 'row without merge_state renders no badge');
});
