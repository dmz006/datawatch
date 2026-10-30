// Answer link on the Automaton decision log — PWA side.
//
// window.decisionAnswerLinkHtml(entry) renders the answer id of an
// operator_answer entry as a link, window.decisionAnswerPanelHtml(answer)
// renders the panel body (verbatim quote, chosen option, actor, time) and
// window.loadDecisionAnswerPanel(questionID, answerID, panel) fills the panel
// from GET /api/questions/{id}/answers. The worker marker switch is a toggle
// in the Automata settings card. Every value is text, never markup.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-question-decision-link.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';
const NEW_KEYS = [
  'settings_automata_questions_worker_marker',
  'settings_automata_questions_worker_marker_help',
  'decision_answer_link',
  'decision_answer_panel_quote',
  'decision_answer_panel_option',
  'decision_answer_panel_actor',
  'decision_answer_panel_time',
  'decision_answer_panel_loading',
  'decision_answer_panel_failed',
  'decision_answer_panel_missing',
];

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

function answerEntry(over) {
  return Object.assign({
    kind: 'operator_answer', actor: 'operator', at: '2026-10-29T10:05:00Z',
    note: 'answered', question_id: 'q-1', answer_id: 'a-1', decision_state: 'answered',
  }, over || {});
}

function answer(over) {
  return Object.assign({
    id: 'a-1', question_id: 'q-1', actor: 'dmz', chosen_option_id: 'postgres',
    quote: 'Use postgres, not sqlite.', created_at: '2026-10-29T10:05:00Z',
  }, over || {});
}

test('an operator_answer entry renders its answer id as a link carrying both ids', () => {
  const html = loadApp().decisionAnswerLinkHtml(answerEntry());
  assert.match(html, /class="decision-answer-link"/);
  assert.match(html, /data-question="q-1"/);
  assert.match(html, /data-answer="a-1"/);
  assert.match(html, />Answer a-1</);
  assert.match(html, /class="decision-answer-panel" hidden/);
});

test('other entries and entries without both ids render no link', () => {
  const sb = loadApp();
  assert.equal(sb.decisionAnswerLinkHtml(answerEntry({ kind: 'operator_decision' })), '');
  assert.equal(sb.decisionAnswerLinkHtml(answerEntry({ answer_id: '' })), '');
  assert.equal(sb.decisionAnswerLinkHtml(answerEntry({ question_id: undefined })), '');
  assert.equal(sb.decisionAnswerLinkHtml(null), '');
});

test('hostile ids are escaped in the link', () => {
  const html = loadApp().decisionAnswerLinkHtml(answerEntry({ answer_id: HOSTILE, question_id: HOSTILE }));
  assert.ok(!html.includes(HOSTILE));
  assert.ok(html.includes(ENTITY_IMG));
});

test('the panel shows the quote, chosen option, actor and time', () => {
  const html = loadApp().decisionAnswerPanelHtml(answer());
  assert.match(html, /<blockquote class="decision-answer-quote"[^>]*>Use postgres, not sqlite\.<\/blockquote>/);
  assert.match(html, /Chosen option:<\/span> postgres/);
  assert.match(html, /Answered by:<\/span> dmz/);
  assert.match(html, /Answered at:<\/span> \S/);
});

test('the quote is text: markup comes back escaped and whitespace is kept', () => {
  const quote = '  ' + HOSTILE + '\nsecond line  ';
  const html = loadApp().decisionAnswerPanelHtml(answer({ quote, actor: HOSTILE, chosen_option_id: HOSTILE }));
  assert.ok(!html.includes(HOSTILE));
  assert.ok(html.includes('  ' + ENTITY_IMG + '\nsecond line  '));
});

test('an answer without a chosen option, actor or time omits those rows', () => {
  const html = loadApp().decisionAnswerPanelHtml({ id: 'a-1', quote: 'free text only' });
  assert.match(html, /free text only/);
  assert.doesNotMatch(html, /Chosen option|Answered by|Answered at/);
});

test('a missing answer renders the not-on-record line', () => {
  assert.match(loadApp().decisionAnswerPanelHtml(undefined), /This answer is no longer on record\./);
});

async function load(reply, qid, aid) {
  const sandbox = loadApp();
  const urls = [];
  const panel = makeStubElement();
  sandbox.apiFetch = url => {
    urls.push(url);
    return reply instanceof Error ? Promise.reject(reply) : Promise.resolve(reply);
  };
  sandbox.__panel = panel;
  vm.runInContext('window.loadDecisionAnswerPanel(' + JSON.stringify(qid) + ',' + JSON.stringify(aid) + ', __panel)', sandbox);
  await flushAsync();
  return { urls, panel };
}

test('the panel loads GET /api/questions/{id}/answers and shows the matching answer', async () => {
  const { urls, panel } = await load({ answers: [answer({ id: 'a-0', quote: 'older' }), answer()], current_answer_id: 'a-1' }, 'q-1', 'a-1');
  assert.deepEqual(urls, ['/api/questions/q-1/answers']);
  assert.match(panel.innerHTML, /Use postgres, not sqlite\./);
  assert.doesNotMatch(panel.innerHTML, /older/);
});

test('the question id is encoded in the request path', async () => {
  const { urls } = await load({ answers: [] }, 'q/1 x', 'a-1');
  assert.deepEqual(urls, ['/api/questions/q%2F1%20x/answers']);
});

test('an answer id that is not in the list renders the not-on-record line', async () => {
  const { panel } = await load({ answers: [answer({ id: 'other' })] }, 'q-1', 'a-1');
  assert.match(panel.innerHTML, /no longer on record/);
});

test('a failed fetch renders the failed line, not an error block', async () => {
  const { panel } = await load(new Error('boom'), 'q-1', 'a-1');
  assert.match(panel.innerHTML, /The answer could not be loaded\./);
  assert.doesNotMatch(panel.innerHTML, /boom/);
});

test('the decisions tab carries the link only on operator_answer entries', () => {
  const sb = loadApp();
  const html = vm.runInContext('_renderDetailDecisionsTab', sb)({
    decisions: [
      { kind: 'operator_decision', actor: 'planner', note: 'asked', question_id: 'q-1', at: '2026-10-29T10:00:00Z' },
      answerEntry(),
    ],
  });
  assert.equal((html.match(/decision-answer-link"/g) || []).length, 1);
});

test('the worker marker switch is a toggle in the Automata settings, with help text', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/\{ key: 'autonomous\.questions\.worker_marker'[^\n]*\}/);
  assert.ok(m, 'worker_marker field missing');
  assert.match(m[0], /type: 'toggle'/);
  assert.match(m[0], /labelKey: 'settings_automata_questions_worker_marker'/);
  assert.match(m[0], /helpKey: 'settings_automata_questions_worker_marker_help'/);
  assert.doesNotMatch(m[0], /default: *true/);
});

for (const lang of LANGS) {
  test(`${lang} has every new key, translated and without PRD or tracker ids`, () => {
    const b = bundle(lang);
    for (const k of NEW_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim(), `${lang} missing ${k}`);
      assert.doesNotMatch(b[k], /\bPRDs?\b/, `${lang} ${k} says PRD`);
      assert.doesNotMatch(b[k], /\b(BL|B)\d+\b/, `${lang} ${k} has a tracker id`);
      if (lang !== 'en') assert.notEqual(b[k], bundle('en')[k], `${lang} ${k} is untranslated`);
    }
    assert.match(b.decision_answer_link, /%1\$s/);
  });
}
