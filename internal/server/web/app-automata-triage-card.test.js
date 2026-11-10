// Automata triage card and list renderers (app.js):
//
//   - window.triageCardHtml(record) draws one inbox card: the headline for
//     the record's status, the reason, the Automaton kind chip, the owner
//     chip when automation_name is set, and the action buttons. Retry needs
//     a task_id, Resolve review needs merge_pending, Cancel is always there.
//   - window.triageListHtml(body) draws a card per record, or the empty
//     placeholder when the body has no records.
//   - every value is escaped and the output never shows 'undefined' or 'NaN'.
//
// The real app.js and automata-triage-model.js run in a vm sandbox with the
// English bundle active, the same way app-repo-ci-watch.test.js loads them.
//
//   node --test app-automata-triage-card.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const MODEL_JS = path.join(__dirname, 'automata-triage-model.js');

// One sandbox with the triage model and the real app.js loaded, and the
// English bundle active so t() resolves to the English strings.
function loadApp() {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(MODEL_JS, sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  return sandbox;
}

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

// Count how many times a data-action button of this name appears.
function countAction(html, action) {
  return html.split(`data-action="${action}"`).length - 1;
}

function failedRecord(overrides = {}) {
  return Object.assign({
    prd_id: 'prd-1',
    task_id: 'task-1',
    status: 'failed',
    task_title: 'Wire the parser',
    error: 'compile error in parser.go',
    first_seen_at: '2025-01-01T00:00:00Z',
  }, overrides);
}

function blockedRecord(overrides = {}) {
  return Object.assign({
    prd_id: 'prd-2',
    status: 'blocked',
    blocked_reason: 'Needs the signing key',
    first_seen_at: '2025-01-01T00:00:00Z',
  }, overrides);
}

test('card: failed record shows the failed headline, reason, Automaton label and owner', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(failedRecord({
    automation_id: 'auto-9',
    automation_name: 'Nightly release',
  }));
  // Headline: "Stopped: the task "Wire the parser" failed"
  assert.ok(html.includes('Stopped: the task &quot;Wire the parser&quot; failed') ||
    html.includes('Stopped: the task "Wire the parser" failed'), html);
  assert.ok(html.includes('compile error in parser.go'), 'reason shown');
  assert.ok(html.includes(EN.triage_kind_automaton), 'Automaton kind label');
  assert.ok(html.includes('Nightly release'), 'owner name shown');
  assert.ok(html.includes('class="triage-owner-chip"'), 'owner chip rendered');
});

test('card: failed record without automation_name has no owner chip', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(failedRecord());
  assert.ok(!html.includes('triage-owner-chip'), 'no owner chip');
  assert.ok(html.includes('Stopped: the task'), 'failed headline still shown');
});

test('card: blocked record shows the blocked headline and blocked_reason', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(blockedRecord());
  assert.ok(html.includes('Blocked: Needs the signing key'), 'blocked headline');
  assert.ok(html.includes('Needs the signing key'), 'blocked_reason shown as reason');
  assert.ok(!html.includes('Stopped: the task'), 'not the failed headline');
});

test('card: empty record renders the triage_reason_none text', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml({});
  assert.ok(html.includes(EN.triage_reason_none), html);
  assert.ok(html.includes(EN.triage_headline_none), 'no-headline text');
});

test('card: null record renders the empty-record placeholder', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(null);
  assert.ok(html.includes(EN.triage_record_empty));
  assert.ok(!html.includes('data-action='), 'no buttons without a record');
});

test('card: Retry appears only when task_id is set', () => {
  const sandbox = loadApp();
  const withTask = sandbox.triageCardHtml(failedRecord({ task_id: 'task-1' }));
  assert.equal(countAction(withTask, 'retry'), 1);
  assert.ok(withTask.includes('data-task-id="task-1"'));

  const noTask = sandbox.triageCardHtml(failedRecord({ task_id: '' }));
  assert.equal(countAction(noTask, 'retry'), 0);
  assert.ok(!noTask.includes('data-task-id='));
});

test('card: Resolve review appears only for merge_pending', () => {
  const sandbox = loadApp();
  const merge = sandbox.triageCardHtml({
    prd_id: 'prd-3', status: 'merge_pending', branch: 'feat/x',
    first_seen_at: '2025-01-01T00:00:00Z',
  });
  assert.equal(countAction(merge, 'resolve-review'), 1);
  assert.ok(merge.includes('Ready to merge: branch feat/x is waiting'));

  for (const status of ['failed', 'blocked']) {
    const html = sandbox.triageCardHtml({ prd_id: 'p', task_id: 't', status, blocked_reason: 'r', error: 'e' });
    assert.equal(countAction(html, 'resolve-review'), 0, `no Resolve review for ${status}`);
  }
});

test('card: Cancel is always present', () => {
  const sandbox = loadApp();
  const records = [
    failedRecord(),
    failedRecord({ task_id: '' }),
    blockedRecord(),
    { prd_id: 'p', status: 'merge_pending', branch: 'b' },
    {},
    null,
  ].filter(r => r !== null);
  for (const r of records) {
    const html = sandbox.triageCardHtml(r);
    assert.equal(countAction(html, 'cancel'), 1, `Cancel present for ${JSON.stringify(r)}`);
    assert.ok(html.includes(EN.triage_action_cancel));
  }
});

test('card: an HTML-looking reason is escaped', () => {
  const sandbox = loadApp();
  const payload = '<img src=x onerror="alert(1)"><script>alert(2)</script>';
  const html = sandbox.triageCardHtml(blockedRecord({ blocked_reason: payload }));
  assert.ok(!html.includes('<img'), 'no raw img tag');
  assert.ok(!html.includes('<script'), 'no raw script tag');
  assert.ok(html.includes('&lt;img'), 'angle brackets escaped');
  assert.ok(html.includes('&lt;script'), 'script brackets escaped');

  const failed = sandbox.triageCardHtml(failedRecord({ error: '<b>boom</b>', task_title: '<i>t</i>' }));
  assert.ok(!/<b>|<\/b>|<i>|<\/i>/.test(failed), 'failed fields escaped too');
});

test('card: output never contains undefined or NaN', () => {
  const sandbox = loadApp();
  const fixtures = [
    failedRecord(),
    failedRecord({ task_id: '', automation_id: 'a', automation_name: '' }),
    blockedRecord({ blocked_reason: '' }),
    { prd_id: 'p', status: 'merge_pending', branch: '' },
    { status: 'unknown-status' },
    {},
    null,
  ];
  for (const r of fixtures) {
    const html = sandbox.triageCardHtml(r);
    assert.ok(!html.includes('undefined'), `no 'undefined' for ${JSON.stringify(r)}`);
    assert.ok(!html.includes('NaN'), `no 'NaN' for ${JSON.stringify(r)}`);
  }
});

test('list: two records render two cards', () => {
  const sandbox = loadApp();
  const html = sandbox.triageListHtml({
    records: [failedRecord(), blockedRecord()],
    alert_minutes: 30,
  });
  assert.equal(html.split('class="prd-capacity-card triage-card"').length - 1, 2);
  assert.ok(html.includes('Stopped: the task'));
  assert.ok(html.includes('Blocked: Needs the signing key'));
  assert.ok(!html.includes('triage-list-empty'), 'no empty placeholder');
});

test('list: empty body renders the empty placeholder', () => {
  const sandbox = loadApp();
  for (const body of [{ records: [] }, {}, null]) {
    const html = sandbox.triageListHtml(body);
    assert.ok(html.includes('triage-list-empty'), 'empty placeholder class');
    assert.ok(html.includes(EN.triage_empty), 'empty placeholder text');
    assert.ok(!html.includes('triage-card'), 'no cards');
  }
});
