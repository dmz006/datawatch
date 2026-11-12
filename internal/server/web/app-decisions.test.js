// Decision line card, waiting_decision task and gate settings — PWA side.
//
// window.decisionLineHtml(decisions) renders the read-only decision line
// card on the Automaton detail from the decisions array of
// GET /api/autonomous/decisions. window.loadDecisionLineCard(prdID) fetches
// it with the prd_id of the Automaton and fills the #prdDecisionLine slot.
// A task whose status is waiting_decision shows a status label and its wait
// reason the way a waiting_capacity task does, and the two decision gate keys
// are controls in the Automation settings.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-decisions.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

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

function openDecision(over) {
  return {
    id: 'q-open-1',
    question: 'Which database should the importer target?',
    label: 'database',
    state: 'open',
    asked_by: 'planner',
    asked_at: '2026-10-29T10:00:00Z',
    prd_id: 'p1',
    task_ids: ['t1'],
    waiting_task_ids: ['t1'],
    waiting_tasks: [{ task_id: 't1', wait_reason: 'waiting on decision q-open-1' }],
    answer: null,
    ...over,
  };
}

function answeredDecision(over) {
  return openDecision({
    id: 'q-ans-2',
    question: 'Use the staging bucket?',
    state: 'answered',
    waiting_task_ids: [],
    waiting_tasks: [],
    answer: {
      id: 'a-7',
      quote: 'Yes, use staging.',
      quote_sha256: 'abc123',
      actor: 'operator',
      surface: 'pwa',
      answered_at: '2026-10-29T11:30:00Z',
    },
    ...over,
  });
}

function card(decisions) {
  const sandbox = loadApp();
  sandbox._decisions = decisions;
  return vm.runInContext('window.decisionLineHtml(_decisions)', sandbox);
}

// slot builds the stand-in for the #prdDecisionLine element of the detail.
function slot(prdID) {
  return {
    innerHTML: 'unset',
    getAttribute(name) { return name === 'data-prd' ? prdID : null; },
  };
}

// load runs loadDecisionLineCard against a stubbed apiFetch and a stubbed
// slot; reply is either a body or an Error for a failed fetch.
async function load(prdID, reply, el) {
  const sandbox = loadApp();
  const urls = [];
  const slotEl = el || slot(prdID);
  sandbox.apiFetch = url => {
    urls.push(url);
    return reply instanceof Error ? Promise.reject(reply) : Promise.resolve(reply);
  };
  sandbox.document.getElementById = id => (id === 'prdDecisionLine' ? slotEl : null);
  vm.runInContext('window.loadDecisionLineCard(' + JSON.stringify(prdID) + ')', sandbox);
  await flushAsync();
  return { urls, slotEl };
}

// ── fetch ───────────────────────────────────────────────────────────────────

test('the card fetches the decisions of this Automaton by its prd_id', async () => {
  const { urls } = await load('p1', { decisions: [] });
  assert.deepEqual(urls, ['/api/autonomous/decisions?prd_id=p1']);
});

test('the prd_id is URL-encoded in the fetch', async () => {
  const { urls } = await load('a b&c', { decisions: [] });
  assert.deepEqual(urls, ['/api/autonomous/decisions?prd_id=a%20b%26c']);
});

test('the fetched decisions fill the slot', async () => {
  const { slotEl } = await load('p1', { decisions: [openDecision()] });
  assert.ok(slotEl.innerHTML.includes('q-open-1'), slotEl.innerHTML);
  assert.ok(slotEl.innerHTML.includes('decision-line-card'), slotEl.innerHTML);
});

test('a failed fetch renders nothing, not an error block', async () => {
  const { slotEl } = await load('p1', new Error('boom'));
  assert.equal(slotEl.innerHTML, '');
});

test('an answer that arrives after the detail moved to another Automaton is dropped', async () => {
  const { slotEl } = await load('p1', { decisions: [openDecision()] }, slot('p2'));
  assert.equal(slotEl.innerHTML, 'unset');
});

// ── rendering ───────────────────────────────────────────────────────────────

test('an open decision shows its id, state and question, and no answer', () => {
  const html = card([openDecision()]);
  assert.ok(html.includes('Decisions'), 'card title missing: ' + html);
  assert.ok(html.includes('q-open-1'), 'decision id missing');
  assert.ok(html.includes('decision-state-open'), 'open state chip class missing');
  assert.ok(html.includes('>open<'), 'open state label missing: ' + html);
  assert.ok(html.includes('Which database should the importer target?'), 'question missing');
  assert.ok(!html.includes('decision-quote'), 'an open decision must not render a quote');
  assert.ok(!html.includes('decision-answer-meta'), 'an open decision must not render answer details');
});

test('an answered decision shows the answer id, actor, time and the quote', () => {
  const html = card([answeredDecision()]);
  assert.ok(html.includes('q-ans-2'), 'decision id missing');
  assert.ok(html.includes('decision-state-answered'), 'answered state chip class missing');
  assert.ok(html.includes('>answered<'), 'answered state label missing: ' + html);
  assert.ok(html.includes('Use the staging bucket?'), 'question missing');
  assert.ok(html.includes('Answer a-7'), 'answer id missing: ' + html);
  assert.ok(html.includes('by operator'), 'actor missing: ' + html);
  assert.ok(/ · \S/.test(html.slice(html.indexOf('by operator'))), 'answer time missing: ' + html);
  assert.ok(html.includes('Yes, use staging.'), 'quote missing');
});

test('the quote renders verbatim: nothing trimmed, cut or reworded, line breaks kept', () => {
  const quote = '  Yes -- use staging,\n\nbut only after the freeze lifts.  ';
  const html = card([answeredDecision({ answer: { ...answeredDecision().answer, quote } })]);
  const m = /<blockquote class="decision-quote"[^>]*>([\s\S]*?)<\/blockquote>/.exec(html);
  assert.ok(m, 'quote block missing: ' + html);
  assert.equal(m[1], quote);
  assert.ok(/white-space:pre-wrap/.test(m[0]), 'quote must keep its line breaks');
});

test('a long quote is not truncated', () => {
  const quote = 'x'.repeat(5000);
  const html = card([answeredDecision({ answer: { ...answeredDecision().answer, quote } })]);
  assert.ok(html.includes(quote), 'long quote was cut');
});

test('hostile text in the question, quote and actor is escaped', () => {
  const html = card([answeredDecision({
    question: HOSTILE,
    answer: { ...answeredDecision().answer, quote: HOSTILE, actor: HOSTILE },
  })]);
  assert.ok(!html.includes(HOSTILE), 'raw markup reached the card: ' + html);
  assert.ok(html.includes(ENTITY_IMG), 'escaped markup missing');
});

test('withdrawn and expired decisions show their own state', () => {
  const html = card([openDecision({ id: 'q-w', state: 'withdrawn' }), openDecision({ id: 'q-e', state: 'expired' })]);
  assert.ok(html.includes('>withdrawn<'), html);
  assert.ok(html.includes('>expired<'), html);
});

test('one line per decision, in the order the server sent them', () => {
  const html = card([openDecision({ id: 'q-first' }), answeredDecision({ id: 'q-second' })]);
  assert.equal((html.match(/class="decision-line-row"/g) || []).length, 2);
  assert.ok(html.indexOf('q-first') < html.indexOf('q-second'), 'order changed');
});

test('an empty list renders the empty state', () => {
  const html = card([]);
  assert.ok(html.includes('decision-line-empty'), 'empty-state class missing: ' + html);
  assert.ok(html.includes('No decisions yet'), 'empty-state text missing: ' + html);
  assert.ok(!html.includes('decision-line-row'), 'no row may render for an empty list');
});

test('a body without a decisions array renders the empty state', () => {
  assert.ok(card(undefined).includes('decision-line-empty'));
  assert.ok(card(null).includes('decision-line-empty'));
});

// ── waiting_decision task ───────────────────────────────────────────────────

function taskHtml(task) {
  const sandbox = loadApp();
  sandbox._prd = { id: 'p1', status: 'running' };
  sandbox._task = { id: 't1', title: 'Import data', spec: 'Import it.', ...task };
  return vm.runInContext('renderTask(_prd, { id: "s1" }, _task, false)', sandbox);
}

test('a waiting_decision task shows its status label and its wait reason', () => {
  const html = taskHtml({ status: 'waiting_decision', wait_reason: 'waiting on decision q-open-1' });
  assert.ok(html.includes('prd-task-wait-badge'), 'wait badge missing: ' + html);
  assert.ok(html.includes('Waiting for a decision'), 'status label missing');
  const m = /<div class="prd-task-wait-reason"[^>]*>([\s\S]*?)<\/div>/.exec(html);
  assert.ok(m, 'wait reason row missing: ' + html);
  assert.ok(m[1].includes('Waiting for a decision'), 'reason row has no label');
  assert.ok(m[1].includes('waiting on decision q-open-1'), 'wait reason text missing: ' + m[1]);
  assert.ok(html.includes('status-waiting_decision'), 'status glyph class missing');
});

test('a waiting_decision task is expanded by default so the reason is visible', () => {
  const html = taskHtml({ status: 'waiting_decision', wait_reason: 'waiting on decision q-open-1' });
  const body = html.slice(html.indexOf('prd-task-expanded-body'));
  assert.ok(!/^prd-task-expanded-body"\s+hidden/.test(body), 'task body is collapsed: ' + body.slice(0, 80));
});

test('the wait reason of a waiting_decision task is escaped', () => {
  const html = taskHtml({ status: 'waiting_decision', wait_reason: HOSTILE });
  assert.ok(!html.includes(HOSTILE), 'raw markup reached the task row');
  assert.ok(html.includes(ENTITY_IMG), 'escaped reason missing');
});

test('a waiting_capacity task keeps its own label and a pending task shows no wait badge', () => {
  const cap = taskHtml({ status: 'waiting_capacity', wait_reason: 'pool full' });
  assert.ok(cap.includes('Waiting for capacity'), cap);
  assert.ok(!cap.includes('Waiting for a decision'), 'capacity task shows the decision label');
  const pending = taskHtml({ status: 'pending' });
  assert.ok(!pending.includes('prd-task-wait-badge'), 'pending task shows a wait badge');
});

// ── settings ────────────────────────────────────────────────────────────────

function autonomousField(key) {
  const sandbox = loadApp();
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

test('the decision gate is a toggle in the Automation settings, beside the other question settings', () => {
  const f = autonomousField('autonomous.questions.decision_gate');
  assert.ok(f, 'autonomous.questions.decision_gate missing from the autonomous card');
  assert.equal(f.type, 'toggle');
  assert.equal(f.labelKey, 'settings_automata_questions_decision_gate');
  const sandbox = loadApp();
  const keys = JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous').fields.map(f => f.key))", sandbox));
  assert.ok(keys.indexOf('autonomous.questions.decision_gate') > keys.indexOf('autonomous.questions.notify_messaging'),
    'decision_gate must follow notify_messaging');
});

test('the decision gate mode is a select of shadow and on, defaulting to shadow', () => {
  const f = autonomousField('autonomous.questions.decision_gate_mode');
  assert.ok(f, 'autonomous.questions.decision_gate_mode missing from the autonomous card');
  assert.equal(f.type, 'select');
  assert.deepEqual(f.options, ['shadow', 'on']);
  assert.equal(f.default, 'shadow');
  assert.equal(f.labelKey, 'settings_automata_questions_decision_gate_mode');
});

test('the settings card renders both controls from the stored config and saves through saveGeneralField', async () => {
  const sandbox = loadApp();
  const els = {};
  sandbox.apiFetch = url => Promise.resolve(
    url === '/api/config'
      ? { autonomous: { questions: { decision_gate: true, decision_gate_mode: 'on' } } }
      : (url === '/api/llms' ? { llms: [] } : []));
  sandbox.document.getElementById = id => (els[id] || (els[id] = { innerHTML: '', style: {}, querySelectorAll() { return []; } }));
  vm.runInContext('loadGeneralConfig()', sandbox);
  await flushAsync();
  const html = els.gcfg_autonomous && els.gcfg_autonomous.innerHTML;
  assert.ok(html, 'autonomous settings card did not render');
  const gate = /<input type="checkbox"([^>]*)onchange="saveGeneralField\('autonomous\.questions\.decision_gate', this\.checked\)"/.exec(html);
  assert.ok(gate, 'decision gate toggle missing');
  assert.ok(/checked/.test(gate[1]), 'decision gate toggle must show the stored value');
  const mode = /<select[^>]*onchange="saveGeneralField\('autonomous\.questions\.decision_gate_mode', this\.value\)">([\s\S]*?)<\/select>/.exec(html);
  assert.ok(mode, 'decision gate mode select missing');
  assert.ok(mode[1].includes('<option value="on" selected>'), 'stored mode must be selected: ' + mode[1]);
  assert.ok(mode[1].includes('<option value="shadow" '), 'shadow option missing');
});

// ── locale keys ─────────────────────────────────────────────────────────────

const KEYS = [
  'decision_line_title',
  'decision_line_empty',
  'decision_state_open',
  'decision_state_answered',
  'decision_state_withdrawn',
  'decision_state_expired',
  'decision_answer_id',
  'decision_answer_actor',
  'prd_task_waiting_decision',
  'settings_automata_questions_decision_gate',
  'settings_automata_questions_decision_gate_mode',
];

for (const lang of LANGS) {
  test(`every decision key is present and non-empty in the ${lang} bundle`, () => {
    const b = bundle(lang);
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' missing or empty in ' + lang);
    }
  });
}
