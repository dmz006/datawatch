// Automata triage detail panel (app.js): window.openTriageDetail(recordOrId).
//
//   - opens a drawer (the automation drawer markup) with the full headline,
//     the complete reason text (not clamped), the Automaton and owning
//     Automation labels, the wait time, the retry budget table and the
//     dated notes history.
//   - a section with no data is left out; an empty history shows
//     triage_detail_empty_history.
//   - the same four action buttons as the card, dispatched by the same
//     helpers the card uses.
//   - a card records its record, so openTriageDetail(prdID, taskID) finds it.
//
//   node --test app-triage-detail.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const MODEL_JS = path.join(__dirname, 'automata-triage-model.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

// A captured element: keeps the innerHTML it was given and its listeners,
// so a test can read the panel and click its buttons.
function captureElement() {
  const listeners = [];
  return {
    innerHTML: '',
    listeners,
    addEventListener(type, fn) { listeners.push({ type, fn }); },
    setAttribute() {}, remove() { this.removed = true; },
  };
}

function loadApp() {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(MODEL_JS, sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.window._i18n.fallback = EN;
  sandbox.window._i18n.bundle = EN;
  const appended = [];
  sandbox.document.body = { appendChild(el) { appended.push(el); } };
  sandbox.document.createElement = () => captureElement();
  sandbox.document.getElementById = id => appended.find(e => e.id === id) || null;
  sandbox._appended = appended;
  return sandbox;
}

function failedRecord(overrides = {}) {
  return Object.assign({
    prd_id: 'prd-1',
    prd_title: 'Parser rewrite',
    task_id: 'task-1',
    task_title: 'Wire the parser',
    status: 'failed',
    error: 'line one of the error\nline two of the error\nline three of the error\nline four of the error\nline five of the error',
    first_seen_at: new Date(Date.now() - 3 * 3600 * 1000).toISOString(),
    retry_budget: {
      infra: { limit: 3, used: 1, left: 2 },
      content: { limit: 0, used: 0, left: 0 },
      scope: { limit: 2, used: 2, left: 0 },
      policy: { limit: 1, used: 0, left: 1 },
      ceiling_total: 6, ceiling_left: 3,
    },
    notes: [
      { at: '2026-11-01T10:00:00Z', source: 'review', text: 'Held for a second look' },
      { at: '2026-11-02T11:30:00Z', text: 'Operator asked for a retry' },
    ],
    automation_id: 'auto-9',
    automation_name: 'Nightly release',
  }, overrides);
}

function detailHtml(sandbox, record) {
  sandbox.openTriageDetail(record);
  return sandbox._appended[sandbox._appended.length - 1].innerHTML;
}

test('panel: full headline, the complete unclamped error, and both labels', () => {
  const sandbox = loadApp();
  const html = detailHtml(sandbox, failedRecord());
  assert.ok(html.includes('Stopped: the task &quot;Wire the parser&quot; failed'), html);
  for (const line of ['line one', 'line two', 'line three', 'line four', 'line five']) {
    assert.ok(html.includes(line), `full error line "${line}" shown`);
  }
  assert.ok(!html.includes('-webkit-line-clamp'), 'reason is not clamped');
  assert.ok(html.includes('Parser rewrite'), 'Automaton label shown');
  assert.ok(html.includes(EN.triage_kind_automaton), 'Automaton row label');
  assert.ok(html.includes('Nightly release'), 'owning Automation name shown');
  assert.ok(html.includes(EN.triage_detail_automation), 'Automation row label');
});

test('panel: wait time, budget table with four rows, and dated notes', () => {
  const sandbox = loadApp();
  const html = detailHtml(sandbox, failedRecord());
  assert.ok(html.includes('waiting 3h'), 'first seen as a wait time');
  for (const key of ['triage_budget_infra', 'triage_budget_content', 'triage_budget_scope', 'triage_budget_policy']) {
    assert.ok(html.includes(EN[key]), `budget row ${key}`);
  }
  assert.ok(html.includes('Ceiling: 3 left of 6'), 'ceiling line');
  assert.ok(html.includes('Held for a second look'), 'first note');
  assert.ok(html.includes('Operator asked for a retry'), 'second note');
  assert.ok(html.includes('review'), 'note source shown');
  assert.equal(html.split('<li>').length - 1, 2, 'one list entry per note');
  assert.ok(!html.includes(EN.triage_detail_empty_history), 'no empty-history text when notes exist');
});

test('panel: an empty history shows the empty-history text', () => {
  const sandbox = loadApp();
  const html = detailHtml(sandbox, failedRecord({ notes: [] }));
  assert.ok(html.includes(EN.triage_detail_empty_history), html);
  assert.ok(html.includes(EN.triage_detail_history), 'history heading still shown');
});

test('panel: sections with no data are left out', () => {
  const sandbox = loadApp();
  const html = detailHtml(sandbox, failedRecord({
    automation_id: undefined, automation_name: undefined,
    first_seen_at: '0001-01-01T00:00:00Z', retry_budget: undefined,
    prd_title: undefined, notes: undefined,
  }));
  assert.ok(!html.includes(EN.triage_detail_automation), 'no Automation row without an owner');
  assert.ok(!html.includes('waiting '), 'no wait line without a first sighting');
  assert.ok(!html.includes(EN.triage_retry_budget), 'no budget section without a budget');
  assert.ok(!html.includes('undefined') && !html.includes('NaN'), html);
});

test('panel: the four action buttons, same as the card', () => {
  const sandbox = loadApp();
  const html = detailHtml(sandbox, failedRecord({ status: 'merge_pending', branch: 'feat/x' }));
  for (const action of ['open-detail', 'retry', 'resolve-review', 'cancel']) {
    assert.ok(html.includes(`data-action="${action}"`), `button ${action}`);
  }
  assert.ok(html.includes('data-prd-id="prd-1"'));
  assert.ok(html.includes('data-task-id="task-1"'));
});

test('panel: buttons dispatch to the card helpers', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.prdResetTask = (p, tk) => calls.push(['reset', p, tk]);
  sandbox.prdResolveTaskReview = (p, tk, r) => calls.push(['review', p, tk, r.action]);
  sandbox.automataCancel = (p, s) => calls.push(['cancel', p, s]);
  sandbox.openTriageDetail(failedRecord({ status: 'merge_pending' }));
  const el = sandbox._appended[sandbox._appended.length - 1];
  const click = action => el.listeners.find(l => l.type === 'click').fn({
    target: { closest: sel => (sel === '[data-action]'
      ? { getAttribute: n => ({ 'data-action': action, 'data-prd-id': 'prd-1', 'data-task-id': 'task-1', 'data-status': 'merge_pending' })[n] }
      : null) },
  });
  click('retry');
  click('resolve-review');
  click('cancel');
  assert.deepEqual(calls, [
    ['reset', 'prd-1', 'task-1'],
    ['review', 'prd-1', 'task-1', 'retry'],
    ['cancel', 'prd-1', 'merge_pending'],
  ]);
});

test('a card records its record, so openTriageDetail(prdID, taskID) opens the panel', () => {
  const sandbox = loadApp();
  sandbox.triageCardHtml(failedRecord());
  sandbox.openTriageDetail('prd-1', 'task-1');
  const html = sandbox._appended[sandbox._appended.length - 1].innerHTML;
  assert.ok(html.includes('Nightly release'), html);
});

test('openTriageDetail with an unknown id opens nothing', () => {
  const sandbox = loadApp();
  sandbox.openTriageDetail('nope', 'nope');
  assert.equal(sandbox._appended.length, 0);
});

test('reopening replaces the open panel', () => {
  const sandbox = loadApp();
  const first = sandbox.openTriageDetail(failedRecord());
  assert.equal(first, undefined);
  sandbox.openTriageDetail(failedRecord({ prd_id: 'prd-2' }));
  assert.equal(sandbox._appended.length, 2);
});

test('a click on the panel backdrop closes it', () => {
  const sandbox = loadApp();
  sandbox.openTriageDetail(failedRecord());
  const el = sandbox._appended[sandbox._appended.length - 1];
  el.listeners.find(l => l.type === 'click').fn({ target: el, closest: () => null });
  assert.equal(el.removed, true);
});
