// Triage record card — PWA side.
//
// window.triageCardHtml(record) turns the single triage record a failed or
// blocked Automaton carries (PRD.Triage from GET /api/autonomous/prds/<id>,
// and each entry of GET /api/autonomous/triage) into the record card.
// window.triageListHtml(body) is the Automation sub-tab's list renderer.
// The triage_* locale keys are pinned here as well.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
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
  sandbox._body = body;
  return vm.runInContext('window.triageListHtml(_body)', sandbox);
}

// ── failed record rendering ─────────────────────────────────────────────────

test('a failed record shows name, automation, member, status chip, task, error, end reason, first seen', () => {
  const html = card();
  assert.ok(html.includes('Triage'), 'card title missing: ' + html);
  assert.ok(html.includes('Refresh indexes'), 'automaton name missing');
  assert.ok(html.includes('Automation: Nightly pipeline (member m1)'), 'automation and member missing: ' + html);
  assert.ok(html.includes('triage-status-failed'), 'failed status chip class missing');
  assert.ok(html.includes('>failed<'), 'failed status label missing');
  assert.ok(html.includes('Task: Build migration script'), 'failed task missing');
  assert.ok(html.includes('migration step 3 timed out after 600s'), 'error text missing');
  assert.ok(html.includes('Sessions ended: retry budget exhausted'), 'session end reason missing');
  assert.ok(html.includes('First seen'), 'first seen line missing');
  assert.ok(/First seen \S/.test(html), 'first seen date missing');
  assert.ok(html.includes('Not yet alerted'), 'not-alerted line missing');
});

test('a blocked record shows the blocked chip and its own block reason, without the failed chip', () => {
  const html = card({ status: 'blocked', error: '', blocked_reason: 'waiting for the operator to approve credentials' });
  assert.ok(html.includes('triage-status-blocked'), 'blocked status chip class missing');
  assert.ok(html.includes('>blocked<'), 'blocked status label missing: ' + html);
  assert.ok(html.includes('Blocked: waiting for the operator to approve credentials'), 'block reason missing');
  assert.ok(!html.includes('triage-status-failed'), 'failed chip must not appear on a blocked record');
});

test('an alerted record shows the alert time instead of the not-alerted line', () => {
  const html = card({ alerted_at: '2026-10-24T11:30:00Z' });
  assert.ok(html.includes('Alerted '), 'alerted line missing: ' + html);
  assert.ok(!html.includes('Not yet alerted'), 'not-alerted line must not show once alerted');
});

// ── retry budget ────────────────────────────────────────────────────────────

test('the retry budget shows all four categories with used, limit and left, plus the ceiling', () => {
  const html = card();
  assert.ok(html.includes('Retry budget'), 'retry budget heading missing');
  for (const label of ['Infra', 'Content', 'Scope', 'Policy']) {
    assert.ok(html.includes('>' + label + '<'), label + ' label missing');
  }
  assert.ok(html.includes('1 of 3 used · 2 left'), 'category row missing: ' + html);
  assert.ok(html.includes('Ceiling: 12 left of 30'), 'ceiling line missing');
});

test('a category with limit 0 says no auto-retry allowed, and a zero ceiling does not pretend there is one', () => {
  const html = card({ retry_budget: {
    infra: category({ limit: 0, used: 0, left: 0 }),
    content: category({ limit: 2, used: 2, left: 0 }),
    scope: category(), policy: category(),
    ceiling_total: 0, ceiling_left: 0,
  } });
  assert.ok(html.includes('no auto-retry allowed'), 'limit-0 category line missing: ' + html);
  assert.ok(html.includes('2 of 2 used · 0 left'), 'exhausted category line missing');
});

test('a record with no retry budget at all renders the heading and says no auto-retry was allowed', () => {
  const html = card({ retry_budget: undefined });
  assert.ok(html.includes('Retry budget'), 'retry budget heading still shows');
  assert.ok(html.includes('no auto-retry allowed'), 'no-budget line missing: ' + html);
});

// ── missing optional fields and empty inputs ─────────────────────────────────

test('a record with only id and status still renders, falling back to the id and skipping absent fields', () => {
  const html = cardRaw({ prd_id: 'p9', status: 'failed' });
  assert.ok(html.includes('p9'), 'prd id fallback missing');
  assert.ok(html.includes('>failed<'), 'status label missing');
  assert.ok(html.includes('Not yet alerted'), 'not-alerted line missing');
  assert.ok(html.includes('no auto-retry allowed'), 'no-budget fallback missing');
  assert.ok(!html.includes('Automation:'), 'absent automation must not render a line');
  assert.ok(!html.includes('Task:'), 'absent task must not render a line');
  assert.ok(!html.includes('Sessions ended:'), 'absent end reason must not render a line');
});

test('a null record renders the placeholder card, and a null list body renders the empty state', () => {
  assert.ok(cardRaw(null).includes('No triage record yet.'), 'null record placeholder missing');
  assert.ok(cardRaw('nope').includes('No triage record yet.'), 'non-object record placeholder missing');
  assert.ok(list(null).includes('No Automata are waiting for triage.'), 'null body empty state missing');
  assert.ok(list({ records: [] }).includes('No Automata are waiting for triage.'), 'empty records empty state missing');
  assert.ok(list({ records: null }).includes('No Automata are waiting for triage.'), 'null records empty state missing');
});

test('a host of records renders one card per record', () => {
  const html = list({ records: [record(), record({ prd_id: 'p2', prd_title: 'Second automaton', status: 'blocked' })], alert_minutes: 30 });
  assert.equal(html.split('class="prd-capacity-card triage-card"').length - 1, 2, 'expected two cards: ' + html);
  assert.ok(html.includes('Refresh indexes'), 'first record name missing');
  assert.ok(html.includes('Second automaton'), 'second record name missing');
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
  const html = list({ records: [{ prd_id: HOSTILE, status: 'failed', prd_title: HOSTILE }] });
  assert.ok(!html.includes('<img'), 'live hostile tag must be escaped in list: ' + html);
  assert.ok(html.includes(ENTITY_IMG), 'escaped hostile text missing: ' + html);
});

test('a hostile or inherited status value never maps to a locale key and is escaped', () => {
  for (const bad of ['constructor', 'hasOwnProperty', 'undefined', HOSTILE]) {
    const html = cardRaw({ prd_id: 'p1', status: bad });
    assert.ok(html.includes('triage-status-unknown'), 'unknown status class missing for ' + bad);
    assert.ok(!html.includes('triage-status-triage_status'), bad + ' must not map to a key');
    if (bad === HOSTILE) assert.ok(!html.includes('<img'), 'hostile status must be escaped');
  }
});

test('a hostile list body that is not an object renders the empty state, not a throw', () => {
  assert.ok(list('records').includes('No Automata are waiting for triage.'), 'string body must hit the empty state');
  assert.ok(list({ records: 'nope' }).includes('No Automata are waiting for triage.'), 'string records must hit the empty state');
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('every triage_* key exists in every locale bundle with a non-empty value', () => {
  const keys = [
    'triage_card_title', 'triage_record_empty', 'triage_empty', 'triage_failed',
    'triage_status_failed', 'triage_status_blocked', 'automation_triage_owner', 'triage_member',
    'triage_failed_task', 'triage_blocked_reason', 'triage_session_end_reason',
    'triage_first_seen', 'triage_alerted_at', 'triage_not_alerted',
    'triage_retry_budget', 'triage_budget_infra', 'triage_budget_content',
    'triage_budget_scope', 'triage_budget_policy', 'triage_budget_used_left',
    'triage_budget_none', 'triage_budget_ceiling',
  ];
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing or empty ' + k);
    }
  }
});
