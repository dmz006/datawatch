// Triage record card — PWA side.
//
// window.triageCardHtml(record) turns the single triage record a failed,
// blocked or merge-pending Automaton carries (PRD.Triage from
// GET /api/autonomous/prds/<id>, and each entry of GET /api/autonomous/triage)
// into the inbox card: headline, kind and owner chips, the reason clamped to
// two lines, the waiting time, and the action buttons. The retry budget lives
// in the detail panel (window.triageBudgetDetailHtml).
// window.triageListHtml(body) is the Automation sub-tab's list renderer.
// The triage_* locale keys are pinned here as well.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const MODEL_JS = path.join(__dirname, 'automata-triage-model.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(MODEL_JS, sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function category(over) {
  return { limit: 3, used: 1, left: 2, ...over };
}

function budget(over) {
  return {
    infra: category(), content: category(), scope: category(), policy: category(),
    ceiling_total: 30, ceiling_left: 12, ...over,
  };
}

function record(over) {
  return {
    automation_id: 'a1',
    automation_name: 'Nightly pipeline',
    member_id: 'm1',
    prd_id: 'p1',
    prd_title: 'Refresh indexes',
    status: 'failed',
    task_id: 't1',
    task_title: 'Build migration script',
    error: 'migration step 3 timed out after 600s',
    blocked_reason: '',
    session_end_reason: 'retry budget exhausted',
    retry_budget: budget(),
    first_seen_at: '2026-10-24T10:00:00Z',
    updated_at: '2026-10-24T11:30:00Z',
    alerted_at: null,
    ...over,
  };
}

function card(over) {
  const sandbox = loadApp();
  sandbox._rec = record(over);
  return vm.runInContext('window.triageCardHtml(_rec)', sandbox);
}

function cardRaw(v) {
  const sandbox = loadApp();
  sandbox._rec = v;
  return vm.runInContext('window.triageCardHtml(_rec)', sandbox);
}

function list(body) {
  const sandbox = loadApp();
  sandbox.listSetMode('inbox', 'list');
  sandbox._body = body;
  return vm.runInContext('window.triageListHtml(_body)', sandbox);
}

function detailBudget(rec) {
  const sandbox = loadApp();
  sandbox._rec = rec;
  return vm.runInContext('window.triageBudgetDetailHtml(_rec)', sandbox);
}

// buttonsIn returns the data-action values of every button in the markup.
function buttonsIn(html) {
  return [...html.matchAll(/<button[^>]*data-action="([^"]+)"/g)].map(m => m[1]);
}

// ── failed record rendering ─────────────────────────────────────────────────

test('a failed record shows the headline, the Automaton kind chip, the owner chip and the reason', () => {
  const html = card();
  assert.ok(html.includes('Triage'), 'card title missing');
  assert.ok(html.includes('Stopped: the task &quot;Build migration script&quot; failed') ||
    html.includes('Stopped: the task "Build migration script" failed'), 'failed headline missing: ' + html);
  assert.ok(html.includes('>Automaton<'), 'kind chip missing');
  assert.ok(html.includes('in Automation Nightly pipeline'), 'owner chip missing: ' + html);
  assert.ok(html.includes('migration step 3 timed out after 600s'), 'reason missing');
});

test('the card shows its parts in order: headline, chips, reason, waiting time, buttons', () => {
  const html = card();
  const at = (cls) => html.indexOf(cls);
  const order = [at('triage-headline'), at('triage-kind-chip'), at('triage-owner-chip'),
    at('triage-reason'), at('triage-wait'), at('triage-actions')];
  for (const i of order) assert.ok(i >= 0, 'part missing from card: ' + html);
  for (let i = 1; i < order.length; i++) assert.ok(order[i - 1] < order[i], 'part ' + i + ' out of order');
});

test('the reason is clamped to two lines by the card style', () => {
  const html = card();
  assert.ok(html.includes('-webkit-line-clamp:2'), 'two-line clamp missing');
});

test('the card does not show the retry budget; the detail panel helper does', () => {
  const html = card();
  assert.ok(!html.includes('triage-budget-row'), 'budget rows must not be on the card');
  assert.ok(!html.includes('Retry budget'), 'budget heading must not be on the card');
  const d = detailBudget(record());
  assert.ok(d.includes('Retry budget'), 'budget heading missing in detail');
  for (const label of ['Infra', 'Content', 'Scope', 'Policy']) {
    assert.ok(d.includes('>' + label + '<'), label + ' label missing in detail');
  }
  assert.ok(d.includes('1 of 3 used · 2 left'), 'category row missing in detail');
  assert.ok(d.includes('Ceiling: 12 left of 30'), 'ceiling line missing in detail');
});

test('a category with limit 0 says no auto-retry allowed in the detail budget', () => {
  const d = detailBudget(record({ retry_budget: {
    infra: category({ limit: 0, used: 0, left: 0 }),
    content: category({ limit: 2, used: 2, left: 0 }),
    scope: category(), policy: category(),
    ceiling_total: 0, ceiling_left: 0,
  } }));
  assert.ok(d.includes('no auto-retry allowed'), 'limit-0 category line missing');
  assert.ok(d.includes('2 of 2 used · 0 left'), 'exhausted category line missing');
});

test('a record with no retry budget gets the heading and no auto-retry line in detail', () => {
  const d = detailBudget(record({ retry_budget: undefined }));
  assert.ok(d.includes('Retry budget'), 'heading still shows');
  assert.ok(d.includes('no auto-retry allowed'), 'no-budget line missing');
});

// ── headline, owner and reason ──────────────────────────────────────────────

test('a blocked record reads its block reason in the headline and the reason line', () => {
  const html = card({ status: 'blocked', error: '', blocked_reason: 'waiting for the operator to approve credentials' });
  assert.ok(html.includes('Blocked: waiting for the operator to approve credentials'), 'blocked headline missing: ' + html);
  assert.ok(!html.includes('Stopped:'), 'failed headline must not show on a blocked record');
});

test('a standalone record with no automation shows no owner chip', () => {
  const html = card({ automation_id: '', automation_name: '' });
  assert.ok(!html.includes('triage-owner-chip'), 'owner chip must not render without an owner');
  assert.ok(!html.includes('in Automation'), 'owner line must not render without an owner');
});

test('a record with no reason text uses the no-reason line with a next step', () => {
  const html = card({ error: '', blocked_reason: '', session_end_reason: '', status: 'failed' });
  assert.ok(html.includes('No reason was recorded.'), 'no-reason line missing: ' + html);
  assert.ok(html.includes('check the task output'), 'next step named');
});

test('a blocked record shows its full reason, the source label and the unblock hint', () => {
  const html = card({
    status: 'blocked', error: '',
    blocked_reason: 'one.\ntwo.\nthree.\nfour.',
    reason_source: 'blocked_reason',
  });
  for (const line of ['one.', 'two.', 'three.', 'four.']) assert.ok(html.includes(line), 'full line shown');
  assert.ok(!html.includes('-webkit-line-clamp'), 'blocked card unclamped');
  assert.ok(html.includes('class="triage-reason-source"'), 'source label');
  assert.ok(html.includes('the Automaton set this reason for itself'), 'source label text');
  assert.ok(html.includes('Clear the cause named in the reason'), 'unblock hint');
});

test('a failed record keeps its two-line clamp and the error hint', () => {
  const html = card();
  assert.ok(html.includes('-webkit-line-clamp:2'), 'failed clamp kept');
  assert.ok(html.includes('The task ran into the error above'), 'error hint');
  assert.ok(!html.includes('triage-reason-source'), 'no source label on failed');
});

test('the card buttons carry their short explanations', () => {
  const html = card({ status: 'blocked', failed_note: undefined, notes: [{ source: 'review', text: 'held' }] });
  assert.ok(html.includes('Resets the task and runs it again from where it failed.'), 'Retry explanation');
  assert.ok(html.includes('Answers the review so the held task runs again.'), 'Resolve review explanation');
  assert.ok(html.includes('Stops the Automaton and archives it.'), 'Cancel explanation');
});

test('a merge-pending record carries no blocked guidance', () => {
  const html = card({ status: 'merge_pending', branch: 'x', reason_source: 'verdict', session_end_reason: 's' });
  assert.ok(!html.includes('triage-reason-source'), 'no source label');
  assert.ok(!html.includes('class="triage-unblock"'), 'no hint');
});

test('the session end reason is the reason when the status text is empty', () => {
  const html = card({ error: '', status: 'failed', session_end_reason: 'retry budget exhausted' });
  assert.ok(html.includes('retry budget exhausted'), 'session end reason missing from reason');
});

// ── waiting time ────────────────────────────────────────────────────────────

test('the waiting time is a human duration computed from first_seen_at', () => {
  const twoHoursAgo = new Date(Date.now() - 2 * 3600 * 1000 - 5 * 60 * 1000).toISOString();
  const html = card({ first_seen_at: twoHoursAgo });
  assert.ok(html.includes('waiting 2h 5m'), 'waiting line missing: ' + html);
});

test('a zero or missing first sighting renders no waiting line', () => {
  assert.ok(!card({ first_seen_at: '0001-01-01T00:00:00Z' }).includes('triage-wait'), 'zero time must not render a wait');
  assert.ok(!card({ first_seen_at: undefined }).includes('triage-wait'), 'missing time must not render a wait');
});

test('less than a minute waiting renders no waiting line', () => {
  const just = new Date(Date.now() - 20 * 1000).toISOString();
  assert.ok(!card({ first_seen_at: just }).includes('triage-wait'), 'sub-minute wait must not render');
});

// ── buttons ─────────────────────────────────────────────────────────────────

test('a failed task record shows Open details, Retry and Cancel, each with its prd id', () => {
  const html = card();
  assert.deepEqual(buttonsIn(html), ['open-detail', 'retry', 'cancel']);
  assert.ok(html.includes('data-prd-id="p1"'), 'prd id missing on buttons');
  assert.ok(html.includes('data-task-id="t1"'), 'task id missing on retry');
});

test('Retry is not shown without a task id', () => {
  assert.deepEqual(buttonsIn(card({ task_id: '' })), ['open-detail', 'cancel']);
});

test('Resolve review shows on a merge-pending record', () => {
  assert.ok(buttonsIn(card({ status: 'merge_pending', task_id: '' })).includes('resolve-review'), 'resolve-review missing on merge_pending');
});

test('Resolve review shows when the record carries a review note', () => {
  const html = card({ notes: [{ at: '2026-10-24T11:00:00Z', source: 'review', text: 'held' }] });
  assert.ok(buttonsIn(html).includes('resolve-review'), 'resolve-review missing for a task held for review');
});

test('Resolve review is not shown on a plain failed record', () => {
  assert.ok(!buttonsIn(card()).includes('resolve-review'), 'resolve-review must not show on a plain failed record');
});

// ── missing optional fields and empty inputs ─────────────────────────────────

test('a record with only id and status still renders the headline and buttons', () => {
  const html = cardRaw({ prd_id: 'p9', status: 'failed' });
  assert.ok(html.includes('Stopped:'), 'headline missing');
  assert.ok(html.includes('data-prd-id="p9"'), 'prd id missing');
  assert.ok(!html.includes('in Automation'), 'absent automation must not render an owner');
  assert.ok(!html.includes('triage-wait'), 'absent first seen must not render a wait');
  assert.ok(buttonsIn(html).includes('cancel'), 'cancel always shows');
  assert.ok(!buttonsIn(html).includes('retry'), 'retry needs a task');
});

test('a null record renders the placeholder card, and a null list body renders the empty state', () => {
  assert.ok(cardRaw(null).includes('No triage record yet.'), 'null record placeholder missing');
  assert.ok(cardRaw('nope').includes('No triage record yet.'), 'non-object record placeholder missing');
  assert.ok(list(null).includes('No Automata are waiting for triage.'), 'null body empty state missing');
  assert.ok(list({ records: [] }).includes('No Automata are waiting for triage.'), 'empty records empty state missing');
  assert.ok(list({ records: null }).includes('No Automata are waiting for triage.'), 'null records empty state missing');
});

test('a host of records renders one card per record', () => {
  const html = list({ records: [record(), record({ prd_id: 'p2', prd_title: 'Second automaton', status: 'blocked', blocked_reason: 'x' })], alert_minutes: 30 });
  assert.equal(html.split('class="prd-capacity-card triage-card"').length - 1, 2, 'expected two cards');
  assert.ok(html.includes('data-prd-id="p2"'), 'second record card missing');
});

// ── click dispatch ──────────────────────────────────────────────────────────

// fakeClick builds the event target the delegated handler walks: a card with
// its prd id, and optionally a button with its data attributes.
function fakeClick(attrs, btnAttrs) {
  const card = { getAttribute: n => (n === 'data-prd-id' ? attrs.prd : null) };
  const btn = btnAttrs ? { getAttribute: n => (btnAttrs[n] === undefined ? null : btnAttrs[n]) } : null;
  return {
    target: {
      closest: sel => (sel === '.triage-card' ? card : sel === '[data-action]' ? btn : null),
    },
  };
}

function dispatch(sandbox, event) {
  sandbox._ev = event;
  vm.runInContext('_triageCardClick(_ev)', sandbox);
}

test('a click on the card body (no button) calls openTriageDetail with the prd and task ids', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.openTriageDetail = (p, tk) => calls.push(['detail', p, tk]);
  dispatch(sandbox, fakeClick({ prd: 'p1' }, null));
  assert.deepEqual(calls, [['detail', 'p1', '']]);
});

test('Open details calls openTriageDetail', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.openTriageDetail = (p, tk) => calls.push(['detail', p, tk]);
  dispatch(sandbox, fakeClick({ prd: 'p1' }, { 'data-action': 'open-detail', 'data-prd-id': 'p1', 'data-task-id': 't1' }));
  assert.deepEqual(calls, [['detail', 'p1', 't1']]);
});

test('Retry calls prdResetTask with the prd and task ids', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.prdResetTask = (p, tk) => calls.push(['reset', p, tk]);
  dispatch(sandbox, fakeClick({ prd: 'p1' }, { 'data-action': 'retry', 'data-prd-id': 'p1', 'data-task-id': 't1' }));
  assert.deepEqual(calls, [['reset', 'p1', 't1']]);
});

test('Resolve review calls prdResolveTaskReview on the task', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.prdResolveTaskReview = (p, tk, r) => calls.push(['review', p, tk, r.action]);
  dispatch(sandbox, fakeClick({ prd: 'p1' }, { 'data-action': 'resolve-review', 'data-prd-id': 'p1', 'data-task-id': 't1' }));
  assert.deepEqual(calls, [['review', 'p1', 't1', 'retry']]);
});

test('Cancel calls automataCancel with the prd id and status', () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.automataCancel = (id, status) => calls.push(['cancel', id, status]);
  dispatch(sandbox, fakeClick({ prd: 'p1' }, { 'data-action': 'cancel', 'data-prd-id': 'p1', 'data-status': 'failed' }));
  assert.deepEqual(calls, [['cancel', 'p1', 'failed']]);
});

// ── hostile text is always escaped ──────────────────────────────────────────

test('hostile text in every rendered field is escaped and never injectable', () => {
  const html = card({
    prd_title: HOSTILE,
    automation_name: HOSTILE,
    task_title: HOSTILE,
    error: HOSTILE,
    blocked_reason: HOSTILE,
    session_end_reason: HOSTILE,
    status: 'blocked',
  });
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive unescaped: ' + html);
});

test('hostile list bodies are escaped in every record card', () => {
  const html = list({ records: [{ prd_id: HOSTILE, status: 'failed', task_id: HOSTILE, prd_title: HOSTILE }] });
  assert.ok(!html.includes('<img'), 'live hostile tag must be escaped in list: ' + html);
  assert.ok(html.includes(ENTITY_IMG), 'escaped hostile text missing: ' + html);
});

test('a hostile list body that is not an object renders the empty state, not a throw', () => {
  assert.ok(list('records').includes('No Automata are waiting for triage.'), 'string body must hit the empty state');
  assert.ok(list({ records: 'nope' }).includes('No Automata are waiting for triage.'), 'string records must hit the empty state');
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('every triage_* key the card, detail and guidance use exists in every locale bundle with a non-empty value', () => {
  const keys = [
    'triage_card_title', 'triage_record_empty', 'triage_empty',
    'automation_triage_owner', 'triage_retry_budget', 'triage_budget_infra', 'triage_budget_content',
    'triage_budget_scope', 'triage_budget_policy', 'triage_budget_used_left',
    'triage_budget_none', 'triage_budget_ceiling',
    'triage_headline_failed', 'triage_headline_blocked', 'triage_headline_merge_pending',
    'triage_headline_none', 'triage_reason_none', 'triage_kind_automaton', 'triage_in_automation',
    'triage_waiting_for', 'triage_action_open_details', 'triage_action_retry',
    'triage_action_resolve_review', 'triage_action_cancel',
    'triage_btn_desc_retry', 'triage_btn_desc_resolve_review', 'triage_btn_desc_cancel',
    'triage_unblock_review', 'triage_unblock_error', 'triage_unblock_verdict', 'triage_unblock_blocked_reason',
    'triage_reason_source_blocked_reason', 'triage_reason_source_verdict',
    'triage_reason_source_task_error', 'triage_reason_source_task_review',
  ];
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing or empty ' + k);
    }
  }
});

test('the non-English bundles translate the new guidance keys (no English word, no English copy)', () => {
  const en = bundle('en');
  const keys = [
    'triage_btn_desc_retry', 'triage_btn_desc_resolve_review', 'triage_btn_desc_cancel',
    'triage_unblock_review', 'triage_unblock_error', 'triage_unblock_verdict', 'triage_unblock_blocked_reason',
    'triage_reason_source_blocked_reason', 'triage_reason_source_verdict',
    'triage_reason_source_task_error', 'triage_reason_source_task_review',
    'triage_reason_none',
  ];
  // Terminology Rule: the English word must not survive into non-English values.
  const englishAutomaton = /(?:^|[^A-Za-z])Automat(?:a|on)s?(?:$|[^A-Za-z])/i;
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' ' + k + ' missing or empty');
      assert.ok(!englishAutomaton.test(b[k]), lang + ' ' + k + ' still carries the untranslated English word');
      assert.notEqual(b[k], en[k], lang + ' ' + k + ' keeps the English copy instead of a translation');
    }
  }
});
