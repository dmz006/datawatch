// Blocked triage card — the "how to unblock" guidance.
//
// The card a blocked Automaton shows is the whole message: its reason in
// full (no two-line clamp), a small label saying who or what set it when the
// record carries a recognized reason_source, and one concrete next step
// chosen by the reason kind (window.AutomataTriageModel.triageUnblockHint).
// A record with no reason gets the honest no-reason line, which already
// names the next step; a record with a reason source nobody recognizes gets
// neither label nor hint, so the card never invents an explanation.
// The card's Retry, Resolve review and Cancel buttons each carry a short
// line saying what they do; the detail panel keeps the bare buttons.
//
//   node --test app-blocked-triage-guidance.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const MODEL_JS = path.join(__dirname, 'automata-triage-model.js');
const LOCALES_DIR = path.join(__dirname, 'locales');

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

// One sandbox with the real model and app.js, the given bundle active.
function loadAppWith(lang) {
  const b = bundle(lang);
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(MODEL_JS, sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.window._i18n.fallback = b;
  sandbox.window._i18n.bundle = b;
  return sandbox;
}

function loadApp() {
  return loadAppWith('en');
}

const EN = bundle('en');

// A blocked record with a multi-line reason, from where the operator's
// decision is needed.
function blocked(overrides = {}) {
  return Object.assign({
    prd_id: 'prd-b',
    task_id: 'task-b',
    status: 'blocked',
    task_title: 'Run the rollout',
    blocked_reason: 'Step 4 needs the operator to approve credentials.\nThe signing key is expired.\nRotate it, then retry the task.',
    first_seen_at: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
  }, overrides);
}

// ── the full reason ─────────────────────────────────────────────────

test('the blocked reason is drawn in full, without the two-line clamp', () => {
  const sandbox = loadApp();
  const r = blocked();
  const html = sandbox.triageCardHtml(r);
  for (const line of r.blocked_reason.split('\n')) {
    assert.ok(html.includes(line), 'full reason line shown: ' + line);
  }
  assert.ok(!html.includes('-webkit-line-clamp'), 'no clamp on the blocked card');
});

test('a failed card still clamps its reason to two lines', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml({
    prd_id: 'p', task_id: 't', status: 'failed',
    error: 'l1\nl2\nl3\nl4', first_seen_at: new Date().toISOString(),
  });
  assert.ok(html.includes('-webkit-line-clamp:2'), 'failed clamp kept');
});

// ── the source label ────────────────────────────────────────────────

test('every recognized reason_source gets its small source label', () => {
  const sandbox = loadApp();
  const expected = {
    blocked_reason: EN.triage_reason_source_blocked_reason,
    verdict: EN.triage_reason_source_verdict,
    task_error: EN.triage_reason_source_task_error,
    task_review: EN.triage_reason_source_task_review,
  };
  for (const [source, label] of Object.entries(expected)) {
    const html = sandbox.triageCardHtml(blocked({ reason_source: source }));
    assert.ok(html.includes('class="triage-reason-source"'), 'label line for ' + source);
    assert.ok(html.includes(label), 'label text for ' + source);
  }
});

test('an unrecognized reason_source gets no label and no invented hint', () => {
  const sandbox = loadApp();
  for (const source of ['none', 'mystery', 'constructor', 42, null, undefined]) {
    const html = sandbox.triageCardHtml(blocked({ reason_source: source }));
    assert.ok(!html.includes('triage-reason-source'), `no label for source ${JSON.stringify(source)}`);
    assert.ok(!html.includes('class="triage-unblock"'), `no hint for source ${JSON.stringify(source)}`);
  }
});

// ── the "how to unblock" line ───────────────────────────────────────

test('the blocked card shows the next step named for its reason source', () => {
  const sandbox = loadApp();
  const expected = {
    blocked_reason: EN.triage_unblock_blocked_reason,
    verdict: EN.triage_unblock_verdict,
    task_error: EN.triage_unblock_error,
    task_review: EN.triage_unblock_review,
  };
  for (const [source, hint] of Object.entries(expected)) {
    const html = sandbox.triageCardHtml(blocked({ reason_source: source }));
    assert.ok(html.includes('class="triage-unblock"'), 'hint line for ' + source);
    assert.ok(html.includes(hint), 'hint text for ' + source);
  }
  // Every hint names a concrete way back: the review, or reset — or cancel.
  assert.ok(sandbox.triageCardHtml(blocked({ reason_source: 'task_review' })).includes('resolve the review'), 'review names its resolution');
  for (const source of ['blocked_reason', 'verdict', 'task_error']) {
    const html = sandbox.triageCardHtml(blocked({ reason_source: source }));
    assert.ok(/reset the task/i.test(html), 'reset named for ' + source);
    assert.ok(/cancel the Automaton/i.test(html), 'cancel named for ' + source);
  }
});

test('a failed card shows the error next step', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml({
    prd_id: 'p', task_id: 't', status: 'failed', error: 'it broke',
  });
  assert.ok(html.includes('class="triage-unblock"'), 'hint line on failed');
  assert.ok(html.includes(EN.triage_unblock_error), 'error hint');
  assert.ok(!html.includes('triage-reason-source'), 'no source label on failed');
});

test('a blocked record with no reason text is honest: no fake reason, a named next step, no hint', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(blocked({ blocked_reason: '', reason_source: 'task_review' }));
  assert.ok(html.includes(EN.triage_reason_none), 'honest no-reason line');
  assert.ok(html.includes('check the task output'), 'the next step is named');
  assert.ok(html.includes('reset the task'), 'the way back is named');
  assert.ok(!html.includes('class="triage-unblock"'), 'no duplicate hint line');
});

test('merge-pending, empty and null records never get the blocked guidance', () => {
  const sandbox = loadApp();
  const merge = sandbox.triageCardHtml({ prd_id: 'p', status: 'merge_pending', branch: 'x', reason_source: 'verdict' });
  assert.ok(!merge.includes('triage-reason-source'), 'no label on merge_pending');
  assert.ok(!merge.includes('class="triage-unblock"'), 'no hint on merge_pending');
  const empty = sandbox.triageCardHtml({ status: 'failed', reason_source: 'verdict' });
  assert.ok(!empty.includes('triage-reason-source'), 'no label on a failed record');
  const none = sandbox.triageCardHtml(null);
  assert.ok(none.includes(EN.triage_record_empty), 'null still gets the placeholder');
  assert.ok(!none.includes('triage-unblock'), 'no hint without a record');
});

test('the guidance sits between the reason and the waiting time, in order', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(blocked({ reason_source: 'verdict' }));
  const at = (s) => html.indexOf(s);
  const order = [at('class="triage-reason"'), at('class="triage-reason-source"'), at('class="triage-unblock"'), at('class="triage-wait"'), at('class="triage-actions"')];
  for (const i of order) assert.ok(i >= 0, 'part missing from the card');
  for (let i = 1; i < order.length; i++) assert.ok(order[i - 1] < order[i], 'part ' + i + ' out of order');
});

// ── the button explanations ─────────────────────────────────────────

test('the card buttons each carry a short line saying what they do', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(blocked({ notes: [{ source: 'review', text: 'held' }] }));
  assert.ok(html.includes(EN.triage_btn_desc_retry), 'Retry explanation');
  assert.ok(html.includes(EN.triage_btn_desc_resolve_review), 'Resolve review explanation');
  assert.ok(html.includes(EN.triage_btn_desc_cancel), 'Cancel explanation');
});

test('each explanation lives inside its own button', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml(blocked({ notes: [{ source: 'review', text: 'held' }] }));
  const inside = (action, text) => {
    const rest = html.split(`data-action="${action}"`).pop().split('</button>')[0];
    assert.ok(rest.includes(text), `${action} button carries its explanation`);
  };
  inside('retry', EN.triage_btn_desc_retry);
  inside('resolve-review', EN.triage_btn_desc_resolve_review);
  inside('cancel', EN.triage_btn_desc_cancel);
  const openRest = html.split('data-action="open-detail"').pop().split('</button>')[0];
  assert.ok(!openRest.includes('triage-btn-desc'), 'Open details stays bare');
});

// ── escaping ────────────────────────────────────────────────────────

test('a hostile reason and reason_source survive as text only', () => {
  const sandbox = loadApp();
  const payload = '<img src=x onerror="alert(1)"><script>document.title=1</script>';
  const html = sandbox.triageCardHtml(blocked({ blocked_reason: payload, reason_source: payload }));
  assert.ok(!html.includes('<img'), 'no live img tag');
  assert.ok(!html.includes('<script'), 'no live script tag');
  assert.ok(html.includes('&lt;img'), 'reason brackets escaped');
  assert.ok(!html.includes('triage-reason-source'), 'hostile source names no label');
  assert.ok(!html.includes('class="triage-unblock"'), 'hostile source names no hint');
});

// ── the model contract ──────────────────────────────────────────────

test('window.AutomataTriageModel exposes triageUnblockHint for the card code', () => {
  const sandbox = loadApp();
  const M = vm.runInContext('window.AutomataTriageModel', sandbox);
  assert.equal(typeof M.triageUnblockHint, 'function');
  const expect = (record, kind, key) => {
    const out = M.triageUnblockHint(record);
    assert.equal(out.kind, kind, 'kind');
    assert.equal(out.key, key, 'key');
  };
  expect(blocked({ reason_source: 'task_error' }), 'error', 'triage_unblock_error');
  expect(blocked({ reason_source: 'task_review' }), 'review', 'triage_unblock_review');
  expect(blocked({ reason_source: 'verdict' }), 'verdict', 'triage_unblock_verdict');
  expect(blocked({ reason_source: 'blocked_reason' }), 'blocked_reason', 'triage_unblock_blocked_reason');
  assert.equal(M.triageUnblockHint(blocked({ reason_source: 'other' })), null);
  assert.equal(M.triageUnblockHint(blocked({ blocked_reason: '' })), null);
  assert.equal(M.triageUnblockHint({ status: 'failed' }), null);
  assert.equal(M.triageUnblockHint(null), null);
});

test('the hint key it picks exists in the English bundle with that exact value', () => {
  const sandbox = loadApp();
  const M = vm.runInContext('window.AutomataTriageModel', sandbox);
  const cases = [
    ['blocked_reason', 'triage_unblock_blocked_reason'],
    ['verdict', 'triage_unblock_verdict'],
    ['task_error', 'triage_unblock_error'],
    ['task_review', 'triage_unblock_review'],
  ];
  for (const [source, key] of cases) {
    const out = M.triageUnblockHint(blocked({ reason_source: source }));
    assert.equal(out.key, key, `hint for ${source}`);
    assert.ok(EN[out.key] && EN[out.key].length > 0, key + ' exists in en');
  }
});

test('the German bundle renders the same English copy for the guidance', () => {
  const sandbox = loadAppWith('de');
  const de = bundle('de');
  const html = sandbox.triageCardHtml(blocked({ reason_source: 'verdict' }));
  assert.ok(html.includes(de.triage_unblock_verdict), 'de card shows its unblock line');
  assert.ok(html.includes(de.triage_reason_source_verdict), 'de card shows its source label');
  assert.equal(de.triage_unblock_verdict, EN.triage_unblock_verdict, 'de keeps the English copy');
  const failed = sandbox.triageCardHtml({ prd_id: 'p', status: 'failed', error: 'x' });
  assert.ok(failed.includes(de.triage_unblock_error), 'de failed hint');
});

test('a merge_blocked record still routes to its own card', () => {
  const sandbox = loadApp();
  const html = sandbox.triageCardHtml({ prd_id: 'p', status: 'merge_blocked', branch: 'x', blocked_reason: 'b' });
  assert.ok(!html.includes('class="triage-unblock"'), 'not the blocked guidance');
  assert.ok(!html.includes('triage-reason-source'), 'not the blocked guidance');
});
