'use strict';
// v1 question history — the History control on the question card
//
// The card's History switch re-requests the same route with the two decided
// states (GET /api/questions?state=answered and ?state=withdrawn), the
// search field's text parameter on both, merged and dated, newest first.
// A row shows the question id, label, escaped text, state and date — and
// nothing else: never an answer control. Clicking a row loads
// GET /api/questions/{id}/answers and shows the answer log in a dialog:
// every answer's quote verbatim in an escaped <pre>, the chosen option's
// label, actor, surface and time; a Superseded badge naming superseded_by
// on the superseded, and the Current marker on the answer whose id matches
// current_answer_id. A withdrawn question shows its state and no answer
// form — the history never offers the answer control.
//
// Same harness as the queue and answer-form suites: app.js runs in a vm
// sandbox, the fetch stub records the wire, the stub DOM captures the HTML.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const dir = __dirname;
const { buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(dir, 'app.js');
const en = JSON.parse(fs.readFileSync(path.join(dir, 'locales/en.json'), 'utf8'));

function boot({ fetchImpl } = {}) {
  const s = buildSandbox({
    byId: {
      genericModalConfirmBtn: { addEventListener(){}, remove(){} },
      genericModalCancelBtn: { addEventListener(){}, remove(){} },
      automationQuestionsBody: { innerHTML:'', classList:{ add(){}, remove(){}, toggle(){} } },
      automationQuestionsHistoryBtn: { classList:{ add(){}, remove(){}, toggle(){} } },
      automationPanel: { innerHTML:'' },
      automationSearch: { value:'' },
    },
  });
  vm.createContext(s); // buildSandbox returns a plain object; runInContext needs a context
  loadScript(APP_JS, s);
  s._i18n.bundle = en;
  if (fetchImpl) s.fetch = fetchImpl;
  return s;
}

function renderCardHtml(s, body, { history = false } = {}) {
  s._automationQuestionsState.history = history;
  const html = history ? s.questionsHistoryHtml(body) : s.questionsCardHtml(body);
  const el = s.document.getElementById('automationQuestionsBody');
  el.innerHTML = html;
  return el.innerHTML;
}

const Q_ANSWERED = {
  id: 'q-ans-1', label: 'Deploy gate', state: 'answered',
  text: '<deploy> which gate?</deploy>',
  created_at: '2026-07-14T09:08:05Z',
  options: [{ id: 'optA', label: 'Ship the build' }],
};
const Q_WITHDRAWN = {
  id: 'q-wd-1', label: '', state: 'withdrawn',
  text: 'Pick a lane for the retry',
  created_at: '2026-07-13T22:10:00+02:00',
};

// ─── Locale guards ──────────────────────────────────────────────────────────

test('history labels exist in the locale bundle', () => {
  const requiredKeys = [
    'automation_questions_history',
    'automation_questions_history_list_empty',
    'automation_questions_history_answered',
    'automation_questions_history_withdrawn',
    'automation_questions_history_date',
    'automation_questions_history_time',
    'automation_questions_history_title',
    'automation_questions_history_no_answers',
    'automation_questions_history_chosen',
    'automation_questions_history_actor',
    'automation_questions_history_surface',
    'automation_questions_history_superseded',
    'automation_questions_history_current',
  ];
  const missing = requiredKeys.filter(k => !(k in en));
  assert.equal(missing.length, 0, `missing keys: ${missing.join(', ')}`);
});

test('history labels are not copies of the English values', () => {
  const bundles = {};
  for (const lang of ['de','es','fr','ja']) {
    bundles[lang] = JSON.parse(fs.readFileSync(path.join(dir, `locales/${lang}.json`), 'utf8'));
  }
  const histKeys = Object.keys(en).filter(k => k.startsWith('automation_questions_history'));
  assert.ok(histKeys.length >= 13, 'expected at least the 13 history keys in en');
  for (const key of histKeys) {
    for (const lang of ['de','es','fr','ja']) {
      assert.notEqual(bundles[lang][key], en[key], `${lang}/${key} must not copy the English value`);
    }
  }
});

test('t() resolves the history keys with placeholders', () => {
  const s = boot();
  const date = s._questionsHistoryDateText('2026-07-14T09:08:05Z');
  assert.equal(date.length > 0, true, 'a valid date renders');
  const dateLine = s.window.t('automation_questions_history_date', [date]);
  assert.ok(dateLine.includes(date), 'placeholder carries the date');
  const mark = s.window.t('automation_questions_history_superseded', ['ans-2']);
  assert.ok(mark.includes('ans-2'), 'placeholder carries the superseded_by id');
  const logTitle = s.window.t('automation_questions_history_title', ['q-ans-1']);
  assert.ok(logTitle.includes('q-ans-1'), 'dialog title carries the question id');
});

// ─── History request params ─────────────────────────────────────────────────

test('history mode requests both decided states, newest first, text on both', async () => {
  const older = { id: 'q-ans-old', label: 'Old question', state: 'answered', text: 'old one', created_at: '2026-07-10T08:00:00Z' };
  const impl = (url) => {
    if (String(url).includes('state=answered')) {
      // Newer last in the array: the card must still sort newest first.
      return Promise.resolve({ ok:true, json: async () => ({ count:2, questions:[older, Q_ANSWERED] }) });
    }
    return Promise.resolve({ ok:true, json: async () => ({ count:1, questions:[Q_WITHDRAWN] }) });
  };
  const calls = [];
  const rec = (url, o) => { calls.push(String(url)); return impl(url, o); };
  const s = boot({ fetchImpl: rec });
  vm.runInContext('_automationQuestionsLoader()', s); // create the card's loader, in the realm
  s._automationQuestionsState.history = true;
  await s.window._listLoaders.automation_questions.load();
  assert.deepEqual(calls, [
    '/api/questions?state=answered&limit=50',
    '/api/questions?state=withdrawn&limit=50',
  ]);
  const html = s.document.getElementById('automationQuestionsBody').innerHTML;
  assert.ok(html.includes('q-ans-1') && html.includes('q-wd-1') && html.includes('q-ans-old'), 'all rows render');
  // Newest first: 07-14 answered before 07-13 withdrawn before 07-10 answered.
  const ia = html.indexOf('q-ans-1');
  const iw = html.indexOf('q-wd-1');
  const io = html.indexOf('q-ans-old');
  assert.ok(ia < iw && iw < io, `newest first (got ${ia},${iw},${io})`);
});

test('history mode carries the search text on both decided-state requests', async () => {
  const calls = [];
  const impl = (url) => {
    calls.push(String(url));
    return Promise.resolve({ ok:true, json: async () => ({ count:0, questions:[] }) });
  };
  const s = boot({ fetchImpl: impl });
  vm.runInContext('_automationQuestionsLoader()', s); // create the card's loader, in the realm
  s._automationQuestionsState.searchText = 'deploy <gate>';
  s._automationQuestionsState.history = true;
  await s.window._listLoaders.automation_questions.load();
  assert.deepEqual(calls, [
    '/api/questions?state=answered&limit=50&text=deploy%20%3Cgate%3E',
    '/api/questions?state=withdrawn&limit=50&text=deploy%20%3Cgate%3E',
  ]);
});

test('the history row shows id, label, escaped text, state and date', () => {
  const s = boot();
  const html = renderCardHtml(s, { count: 2, questions: [Q_ANSWERED, Q_WITHDRAWN] }, { history: true });
  assert.ok(html.includes('data-question-id="q-ans-1"'), 'question id rendered');
  assert.ok(html.includes('data-question-id="q-wd-1"'), 'withdrawn question id rendered');
  assert.ok(html.includes('Deploy gate'), 'label rendered');
  assert.ok(html.includes('data-state="answered"'), 'answered state carried');
  assert.ok(html.includes('data-state="withdrawn"'), 'withdrawn state carried');
  // Hostile text stays inert: escaped, no live <deploy> element.
  assert.ok(html.includes('&lt;deploy&gt;'), 'question text is escaped');
  assert.ok(!html.includes('<deploy>'), 'no unescaped markup from the server');
  const d1 = new Date(Q_ANSWERED.created_at);
  const p = n => (n<10 ? '0'+n : ''+n);
  const expected = d1.getFullYear()+'-'+p(d1.getMonth()+1)+'-'+p(d1.getDate());
  assert.ok(html.includes(expected), `answered date (${expected}) rendered`);
});

test('the history row never carries an answer control', () => {
  const s = boot();
  const html = renderCardHtml(s, { count: 2, questions: [Q_ANSWERED, Q_WITHDRAWN] }, { history: true });
  assert.ok(!html.includes('<button'), 'no buttons in history rows');
  assert.ok(!/<input/.test(html), 'no inputs in history rows');
  assert.ok(!/textarea/.test(html), 'no textareas in history rows');
  assert.ok(!html.toLowerCase().includes('radio'), 'no radio choices');
  assert.ok(!html.includes('questionAnswer'), 'no answer-form identifiers');
  assert.ok(!html.includes('window.openQuestion('), 'the row does not open the answer form');
});

// ─── Answer log dialog ──────────────────────────────────────────────────────

const A1 = {
  id: 'ans-1', question_id: 'q-ans-1', actor: 'agent-7', surface: 'pwa',
  chosen_option_id: 'optA', quote: 'Ship <b>now</b>\n  keep', created_at: '2026-07-14T09:10:12Z',
  superseded: true, superseded_by: 'ans-2',
};
const A2 = {
  id: 'ans-2', question_id: 'q-ans-1', actor: 'dmz', surface: 'cli',
  chosen_option_id: '', quote: 'gate it first', created_at: '2026-07-14T11:22:40Z',
};

test('the answer log renders quotes verbatim and escaped in <pre>', () => {
  const s = boot();
  const html = s._questionsHistoryBodyHTML({ answers: [A1, A2], current_answer_id: 'ans-2' }, Q_ANSWERED);
  assert.ok(html.includes('<pre class="questions-history-quote"'), 'quote block uses <pre>');
  const preMatch = html.match(/<pre class="questions-history-quote"[^>]*>([\s\S]*?)<\/pre>/);
  assert.ok(preMatch, 'a pre-escaped quote found');
  // Verbatim: the line break and the two leading spaces survived.
  const decoded = preMatch[1].replace(/&lt;/g,'<').replace(/&gt;/g,'>').replace(/&amp;/g,'&').replace(/&quot;/g,'"').replace(/&#39;/g,"'");
  assert.equal(decoded, 'Ship <b>now</b>\n  keep', 'quote is verbatim incl. newline and leading spaces');
  assert.ok(preMatch[1].includes('&lt;b&gt;'), 'markup in the quote is escaped, not live');
  assert.ok(!preMatch[1].includes('<b>'), 'no live <b> inside the pre');
  assert.ok(html.includes('Ship ') && html.includes('gate it first'), 'both quotes present');
});

test('the answer log shows the chosen option label, actor, surface and time', () => {
  const s = boot();
  const html = s._questionsHistoryBodyHTML({ answers: [A1, A2], current_answer_id: 'ans-2' }, Q_ANSWERED);
  assert.ok(html.includes(en.automation_questions_history_chosen.replace('%1$s','Ship the build')), 'chosen option label translated');
  assert.ok(html.includes(en.automation_questions_history_actor.replace('%1$s','agent-7')), 'actor shown');
  assert.ok(html.includes(en.automation_questions_history_actor.replace('%1$s','dmz')), 'second actor shown');
  assert.ok(html.includes(en.automation_questions_history_surface.replace('%1$s','pwa')), 'surface shown');
  const t1 = s._questionsHistoryTimeText('2026-07-14T09:10:12Z');
  assert.ok(t1.length > 0 && html.includes(en.automation_questions_history_time.replace('%1$s', t1)), 'time label rendered');
});

test('superseded and current markers ride on the right answers', () => {
  const s = boot();
  const html = s._questionsHistoryBodyHTML({ answers: [A1, A2], current_answer_id: 'ans-2' }, Q_ANSWERED);
  // Superseded answers name the superseded_by answer.
  assert.ok(html.includes('questions-history-superseded'), 'superseded badge present');
  assert.ok(html.includes(en.automation_questions_history_superseded.replace('%1$s','ans-2')), 'badge names ans-2');
  // The current answer carries the Current marker…
  const curEntry = html.match(/<div class="questions-history-entry" data-answer-id="ans-2"[\s\S]*?<\/div>\s*<\/div>/);
  assert.ok(curEntry, 'current answer entry found');
  assert.ok(curEntry[0].includes(en.automation_questions_history_current), 'Current marker on the effective answer');
  // …and the superseded one does not.
  const oldEntry = html.match(/<div class="questions-history-entry" data-answer-id="ans-1"[\s\S]*?<\/div>\s*<\/div>/);
  assert.ok(oldEntry, 'superseded answer entry found');
  assert.ok(!oldEntry[0].includes('questions-history-current'), 'Current marker not on the superseded answer');
});

test('the answer log is a log, not an answer form', () => {
  const s = boot();
  const html = s._questionsHistoryBodyHTML({ answers: [A1, A2], current_answer_id: 'ans-2' }, Q_ANSWERED);
  assert.ok(!/<input/.test(html), 'no radio inputs');
  assert.ok(!/textarea/.test(html), 'no quote textarea');
  assert.ok(!html.includes('questionAnswerQuote'), 'no answer-form field ids');
  assert.ok(!html.toLowerCase().includes('submit'), 'no submit control');
});

test('a withdrawn question shows its state and no answer form in the log dialog', () => {
  const s = boot();
  // The card renders the withdrawn row with its state, no controls.
  const html = renderCardHtml(s, { count: 1, questions: [Q_WITHDRAWN] }, { history: true });
  assert.ok(html.includes('data-state="withdrawn"'), 'row carries the withdrawn state');
  assert.ok(!/<input/.test(html) && !/textarea/.test(html) && !html.includes('<button'), 'no form on the withdrawn row');
  // Its dialog is a plain log: the body carries the quote/log, never a
  // radio, a quote box or a submit.
  const wdAnswer = { id: 'ans-9', question_id: 'q-wd-1', actor: 'dmz', surface: 'cli', chosen_option_id: '', quote: 'record only', created_at: '2026-07-13T22:05:00Z' };
  const log = s._questionsHistoryBodyHTML({ answers: [wdAnswer], current_answer_id: 'ans-9' }, Q_WITHDRAWN);
  assert.ok(log.includes('record only'), 'the recorded answer still shows');
  assert.ok(!/type="radio"/.test(log) && !/textarea/.test(log) && !/questionAnswerQuote/.test(log) && !/submit/i.test(log), 'still no answer form');
});
