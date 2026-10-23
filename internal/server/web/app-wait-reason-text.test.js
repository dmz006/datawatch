'use strict';
// Wait reason text and holdings — PWA side.
//
// window.taskWaitHtml(task) and window.taskHoldsHtml(task) render the daemon's
// reason text (as sent), a cause chip, the reset time and a running task's
// held pools and node. The Automata task detail (renderTask) and the session
// Status tab (renderSessionStatusBoardInner) both use them, so the same
// input gives the same wait markup in both places.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-wait-reason-text.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const enBundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const CAUSES = ['pool_full', 'node_busy', 'backoff', 'budget', 'global_cooldown', 'await_external', 'tree_lock'];
const KEYS = CAUSES.map(c => 'task_wait_cause_' + c).concat(['task_holds_label', 'task_wait_resets_at']);

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = enBundle;
  return sandbox;
}

function wait(s, task) {
  s._task = task;
  return vm.runInContext('window.taskWaitHtml(_task)', s);
}
function holds(s, task) {
  s._task = task;
  return vm.runInContext('window.taskHoldsHtml(_task)', s);
}

// The wait markup of a rendered Status tab: the contents of the "Waiting on"
// row's value span, between the warning-coloured wrapper tags.
function statusWait(s, auto) {
  const area = makeStubElement();
  s.renderSessionStatusBoardInner(area, { state: 'idle', automaton: { prd_id: 'p1', ...auto } }, 'sess-1');
  const m = /<span style="color:var\(--warning,#f59e0b\);">(.*?)<\/span><\/span><\/div>/s.exec(area.innerHTML);
  return { html: area.innerHTML, wait: m && m[1] };
}

const DAEMON_TEXT = 'pool node:gpu1 is full (2/2), held by 3f2a9c10';

for (const cause of CAUSES) {
  test(`cause ${cause} renders its chip and the daemon text`, () => {
    const s = loadApp();
    const html = wait(s, { status: 'waiting_capacity', cause, wait_reason: DAEMON_TEXT });
    assert.ok(html.includes('task-wait-chip'), html);
    assert.ok(html.includes(`data-cause="${cause}"`), html);
    assert.ok(html.includes(enBundle['task_wait_cause_' + cause]), 'chip label missing: ' + html);
    assert.ok(html.includes(DAEMON_TEXT), 'daemon text must appear verbatim: ' + html);
  });
}

test('resets_at is shown only when present', () => {
  const s = loadApp();
  const without = wait(s, { cause: 'backoff', wait_reason: 'usage back-off on claude' });
  assert.ok(!without.includes('task-wait-resets'), without);
  assert.ok(!without.includes(enBundle.task_wait_resets_at), without);
  const withTs = wait(s, { cause: 'backoff', wait_reason: 'usage back-off on claude', resets_at: '2026-10-22T18:30:00Z' });
  assert.ok(withTs.includes('task-wait-resets'), withTs);
  assert.ok(withTs.includes(enBundle.task_wait_resets_at), withTs);
  assert.ok(withTs.includes('usage back-off on claude'), withTs);
  // The status block names the field wait_resets_at.
  assert.ok(wait(s, { wait_cause: 'backoff', wait_reason: 'x', wait_resets_at: '2026-10-22T18:30:00Z' }).includes('task-wait-resets'));
  // An unparseable time is dropped rather than shown as "Invalid Date".
  const bad = wait(s, { cause: 'backoff', wait_reason: 'x', resets_at: 'not a time' });
  assert.ok(!bad.includes('Invalid'), bad);
  assert.ok(!bad.includes('task-wait-resets'), bad);
});

test('a running task shows a Holds line with its pools and node', () => {
  const s = loadApp();
  const html = holds(s, { status: 'running', pools: ['host', 'llm:claude-pro', 'node:gpu1'], node: 'gpu1' });
  assert.ok(html.includes(enBundle.task_holds_label), html);
  for (const p of ['host', 'llm:claude-pro', 'node:gpu1']) assert.ok(html.includes(`<code>${p}</code>`), html);
  assert.equal((html.match(/gpu1/g) || []).length, 1, 'node already in pools must not repeat: ' + html);
  const nodeOnly = holds(s, { status: 'running', pools: ['host'], node: 'gpu2' });
  assert.ok(nodeOnly.includes('<code>gpu2</code>'), nodeOnly);
  // The status block names the field holds, with a note for a parked task.
  const parked = holds(s, { task_status: 'running', holds: ['tree:/repo'], holds_note: 'parked; inference slot released' });
  assert.ok(parked.includes('tree:/repo') && parked.includes('parked; inference slot released'), parked);
  // Not running, or nothing held: no line.
  assert.equal(holds(s, { status: 'waiting_capacity', pools: ['host'] }), '');
  assert.equal(holds(s, { status: 'running' }), '');
  assert.equal(holds(s, null), '');
});

test('an older payload without cause renders its wait_reason unchanged', () => {
  const s = loadApp();
  assert.equal(wait(s, { status: 'waiting_capacity', wait_reason: 'pool host is full' }), 'pool host is full');
  assert.equal(wait(s, { status: 'waiting_capacity', wait_reason: 'pool host is full', cause: 'something_new' }), 'pool host is full');
  assert.equal(wait(s, {}), '');
  // In the real task detail the line reads as it did before the daemon sent a cause.
  const prd = { id: 'prd1', status: 'running' };
  const task = { id: 't1', title: 'T', status: 'waiting_capacity', wait_reason: 'pool host is full' };
  const html = s.renderTask(prd, { id: 's1' }, task, false);
  assert.ok(html.includes('Waiting for capacity: pool host is full</div>'), html);
  assert.ok(!html.includes('task-wait-chip'));
});

test('the reason text is HTML-escaped', () => {
  const s = loadApp();
  const evil = '<img src=x onerror=alert(1)> & "q"';
  const withCause = wait(s, { cause: 'pool_full', wait_reason: evil });
  assert.ok(!withCause.includes('<img'), withCause);
  assert.ok(withCause.includes('&lt;img src=x onerror=alert(1)&gt; &amp; &quot;q&quot;'), withCause);
  assert.ok(!wait(s, { wait_reason: evil }).includes('<img'));
  assert.ok(!wait(s, { cause: '"><script>', wait_reason: 'x' }).includes('<script'));
  const h = holds(s, { status: 'running', pools: ['<b>p</b>'], node: '<i>n</i>', note: '<u>x</u>' });
  assert.ok(!/<(b|i|u)>/.test(h), h);
});

test('the Status tab and the task detail produce the same wait markup', () => {
  const s = loadApp();
  const reason = DAEMON_TEXT + ' <b>';
  const ts = '2026-10-22T18:30:00Z';
  const expected = wait(s, { cause: 'node_busy', wait_reason: reason, resets_at: ts });
  assert.ok(expected.includes('task-wait-chip'));

  const task = { id: 't1', title: 'T', status: 'waiting_capacity', cause: 'node_busy', wait_reason: reason, resets_at: ts };
  const detail = s.renderTask({ id: 'prd1', status: 'running' }, { id: 's1' }, task, false);
  assert.ok(detail.includes(expected), 'task detail does not carry the shared wait markup');

  const { html, wait: statusMarkup } = statusWait(s, { task_status: 'waiting_capacity', wait_cause: 'node_busy', wait_reason: reason, wait_resets_at: ts });
  assert.equal(statusMarkup, expected);
  assert.ok(html.includes(expected));
});

test('a running task shows its Holds line in the task detail and on the Status tab', () => {
  const s = loadApp();
  const task = { id: 't1', title: 'T', status: 'running', pools: ['host', 'node:gpu1'], node: 'gpu1' };
  const detail = s.renderTask({ id: 'prd1', status: 'running' }, { id: 's1' }, task, false);
  assert.ok(detail.includes(holds(s, task)), 'holds line missing from the task detail');
  const { html } = statusWait(s, { task_status: 'running', holds: ['host', 'node:gpu1'] });
  assert.ok(html.includes(enBundle.task_holds_label + ':'), html);
  assert.ok(html.includes('<code>node:gpu1</code>'), html);
});

test('all new keys exist in all five bundles, with no tracker identifiers', () => {
  for (const loc of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.equal(typeof b[k], 'string', `${loc}.json is missing ${k}`);
      assert.ok(b[k].trim().length > 0, `${loc}.json has an empty ${k}`);
      assert.doesNotMatch(b[k], /\b(BL|GH|B)#?\d+\b/, `${loc}.json ${k} names a tracker id`);
      // "Budget" is the native word in German and French.
      const sameWord = k === 'task_wait_cause_budget' && (loc === 'de' || loc === 'fr');
      if (loc !== 'en' && !sameWord) assert.notEqual(b[k], enBundle[k], `${loc}.json ${k} is untranslated`);
    }
  }
});
