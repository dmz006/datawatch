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
