// Automata view — Events sub-tab error and quiet entries.
//
// Loads the real app.js into the browser stub sandbox and checks that an
// error entry renders as an alert block (what happened, reason, and a next
// action button that opens the fixing tab), and that a quiet entry renders as
// one muted line with its reason and no alert styling.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const ERROR_AUTOMATON = {
  id: 'err-1',
  time: '2026-11-10T03:00:00Z',
  kind: 'error',
  project: 'datawatch',
  automaton: 'prd-alpha',
  automation: '',
  summary: 'Automaton prd-alpha stopped on a failed task',
  detail: 'long detail text',
  reason: 'the verifier rejected the diff',
  next_action: 'Reset the failed task from the Automaton detail',
  quiet: false,
  severity: 'error',
};

const ERROR_AUTOMATION = Object.assign({}, ERROR_AUTOMATON, {
  id: 'err-2', automaton: '', automation: 'auto-beta', next_action: '',
});

const QUIET_WARN = {
  id: 'q-1',
  time: '2026-11-10T05:00:00Z',
  kind: 'check',
  project: 'datawatch',
  automaton: 'prd-alpha',
  automation: '',
  summary: 'Routine check passed',
  detail: 'quiet detail text',
  reason: 'no change since last check',
  next_action: '',
  quiet: true,
  severity: 'error',
};

test('error entry renders an alert block with what happened, reason and next action', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(ERROR_AUTOMATON, false);
  assert.match(html, /class="automata-event-error"/);
  assert.match(html, /role="alert"/);
  assert.match(html, /What happened/);
  assert.match(html, /Automaton prd-alpha stopped on a failed task/);
  assert.match(html, /Reason:<\/strong> the verifier rejected the diff/);
  assert.match(html, /Next action:<\/strong>/);
  assert.match(html, /class="automata-event-fix[^"]*"[^>]*>Reset the failed task from the Automaton detail</);
});

test('error entry is not a clickable row and does not show detail text', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(ERROR_AUTOMATON, true);
  assert.doesNotMatch(html, /data-event-id=/);
  assert.doesNotMatch(html, /aria-expanded/);
  assert.doesNotMatch(html, /long detail text/);
});

test('error fix button points at the Automata list for an Automaton owner', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(ERROR_AUTOMATON, false);
  assert.match(html, /data-events-goto="automata"/);
});

test('error fix button points at the Automation tab for an Automation owner', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(ERROR_AUTOMATION, false);
  assert.match(html, /data-events-goto="automation"/);
  assert.match(html, /Open and fix/);
});

test('error fix tab falls back to the Automata list with no owner', () => {
  const sandbox = loadAppSandbox();
  const ev = Object.assign({}, ERROR_AUTOMATON, { automaton: '', automation: '' });
  assert.equal(vm.runInContext('automataEventFixTab', sandbox)(ev), 'automata');
});

test('quiet entry renders one muted line with its reason and no alert styling', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(QUIET_WARN, false);
  assert.match(html, /class="automata-event-quiet"/);
  assert.match(html, /Routine check passed/);
  assert.match(html, /no change since last check/);
  assert.doesNotMatch(html, /automata-event-error/);
  assert.doesNotMatch(html, /role="alert"/);
  assert.doesNotMatch(html, /var\(--error/);
  assert.doesNotMatch(html, /data-event-id=/);
  assert.doesNotMatch(html, /quiet detail text/);
  assert.doesNotMatch(html, /<button/);
  // a single line: no block-level children
  assert.doesNotMatch(html, /<div[^>]*>\s*<div/);
});

test('non-error, non-quiet entries keep the normal clickable row', () => {
  const sandbox = loadAppSandbox();
  const ev = Object.assign({}, ERROR_AUTOMATON, { severity: 'warn', quiet: false, id: 'w-1' });
  const html = vm.runInContext('automataEventRowHtml', sandbox)(ev, false);
  assert.match(html, /data-event-id="w-1"/);
  assert.doesNotMatch(html, /automata-event-error/);
});

test('go-to-fix opens only known Automata tabs', () => {
  const sandbox = loadAppSandbox();
  vm.runInContext('var __tabCalls = []; switchAutomataTab = function (t) { __tabCalls.push(t); };', sandbox);
  vm.runInContext('automataEventGoToFix("automata")', sandbox);
  vm.runInContext('automataEventGoToFix("automation")', sandbox);
  vm.runInContext('automataEventGoToFix("bogus")', sandbox);
  const calls = JSON.parse(JSON.stringify(vm.runInContext('__tabCalls', sandbox)));
  assert.deepEqual(calls, ['automata', 'automation']);
});
