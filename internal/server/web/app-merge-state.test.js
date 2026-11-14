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
