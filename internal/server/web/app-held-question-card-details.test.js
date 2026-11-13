// v1 held-questions — the held row's corrected fields and details hand-off.
//
// Pins, against the REAL app.js (loaded into the shared stub browser via
// testutil_browser_stub.js):
//
//   • the row reads the report's real field names — title and prd_id
//     (plus the question's own text and the record's default), the way
//     GET /api/autonomous/held-questions sends them, and renders the
//     Automaton title and id, the text line and the default line,
//   • an OPEN row opens the full details when clicked anywhere that is
//     not a button or a link — the guarded hand-off to
//     window.openQuestion(question_id) — and a click that lands on the
//     row's Answer button does not double-open (the button's own
//     hand-off runs once, the row's bails out),
//   • a SHADOW (would-be) row stays read-only: no answer button, no row
//     click, only the detail link to the Automaton detail.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));

// loadApp loads the real app.js and installs the REAL en bundle as the
// active i18n bundle, so every t(key) resolves to the real string.
function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._i18n.fallback = bundle('en');
  return sandbox;
}

// The real report shape of an open entry, as GET
// /api/autonomous/held-questions sends it (internal/server/
// held_questions.go): question_id, state, prd_id, task_id,
// automation_id, title, reason, text, recommended_option, default.
const entry = (extra) => Object.assign({
  question_id: 'q_201',
  state: 'review_held',
  prd_id: 'prd_77',
  automation_id: 'auto_5',
  task_id: 'task_11',
  title: 'Fine-tune pipeline',
  reason: 'Blocked on an unanswered decision',
  text: 'Which backend should carry the fine-tune run, and for how long?',
  recommended_option: 'Use B200',
  default: 'hold',
}, extra);

// rowHtml renders one held row through the real app.js.
const rowHtml = (sandbox, e, isShadow) => {
  sandbox._rowEntry = e;
  return vm.runInContext('window.heldQuestionRowHtml(_rowEntry' + (isShadow === undefined ? '' : ', ' + isShadow) + ')', sandbox);
};

// onclickOf pulls a named inline-handler attribute's code out of the
// rendered HTML (the first one found after `needle`), for evaluation
// against a stubbed `event`, exactly the way a browser would run it.
const onclickOf = (html, needle) => {
  const i = html.indexOf(needle);
  assert.notEqual(i, -1, 'the row carries: ' + needle);
  const m = /onclick="([^"]*)"/.exec(html.slice(i));
  assert.ok(m, 'an onclick handler follows: ' + needle);
  return m[1];
};

// runHandler evaluates an inline handler against a stubbed click event
// where closest() answers the given selector, like a real DOM target.
function runHandler(sandbox, code, closestAnswers) {
  const calls = [];
  sandbox.openQuestion = (id) => { calls.push(String(id)); };
  const answer = (sel) => (closestAnswers[sel] ? {} : null);
  sandbox.event = { target: { closest: (sel) => answer(String(sel)) } };
  // An inline handler is a function body (a top-level `return` is legal
  // there), so attach and fire it exactly the way a browser would.
  const fn = vm.runInContext('(function(){' + code + '\n})', sandbox);
  fn();
  return calls;
}

test('row renders the report fields: text, Automaton title and id, and the default', () => {
  const en = bundle('en');
  const box = loadApp();
  const html = rowHtml(box, entry());
  assert.ok(html.includes('held-question-text'), 'the text line is present');
  assert.ok(html.includes('Which backend should carry the fine-tune run, and for how long?'),
    'the question text is the main line');
  assert.ok(html.includes('-webkit-line-clamp:3'), 'the text line is clamped to three lines');
  assert.ok(html.includes('Fine-tune pipeline'), 'the Automaton title renders from title');
  assert.ok(html.includes('held-question-automaton') && html.includes('prd_77'),
    'the Automaton id renders from prd_id');
  assert.ok(html.includes(en.held_question_default_value.replace('%1$s', 'hold')),
    'the entry default is shown explicitly');
  assert.ok(html.includes(en.held_question_recommended.replace('%1$s', 'Use B200')),
    'the recommended option renders');
});

test('row falls back to the old field names when the new ones are absent', () => {
  const box = loadApp();
  const html = rowHtml(box, {
    question_id: 'q_202', automaton_title: 'Legacy pipeline', automaton_id: 'prd_78',
  });
  assert.ok(html.includes('Legacy pipeline'), 'automaton_title fallback renders');
  assert.ok(html.includes('prd_78'), 'automaton_id fallback renders');
  assert.ok(html.includes('data-automaton="prd_78"'), 'the detail link uses the fallback id');
});

test('open row: a click anywhere that is not a button or link opens the details through openQuestion', () => {
  const box = loadApp();
  const html = rowHtml(box, entry());
  assert.ok(html.includes('class="held-question-row"'), 'the row is rendered');
  const handler = onclickOf(html, 'class="held-question-row"');
  const calls = runHandler(box, handler, {});
  assert.deepEqual(calls, ['q_201'], 'a body click opens the question through the guarded openQuestion hand-off');
});

test('open row: a click on the Answer button does not double-open', () => {
  const box = loadApp();
  const html = rowHtml(box, entry());
  const rowHandler = onclickOf(html, 'class="held-question-row"');
  const btnHandler = onclickOf(html, 'class="held-question-answer');
  // The click lands on the button: the row handler must bail out...
  assert.deepEqual(runHandler(box, rowHandler, { button: true }), [],
    'the row handler bails out when the click is on a button');
  // ...and the button's own hand-off runs exactly once.
  assert.deepEqual(runHandler(box, btnHandler, {}), ['q_201'],
    'the button hand-off still opens the question — once, not twice');
  // And a click on a link likewise never double-opens through the row.
  assert.deepEqual(runHandler(box, rowHandler, { a: true }), [],
    'a click that lands on a link does not re-open through the row');
});

test('shadow row stays read-only: no answer control, no row click, only the detail link', () => {
  const box = loadApp();
  const html = rowHtml(box, entry(), true);
  assert.ok(html.includes('class="held-question-row"'), 'the shadow row is rendered');
  assert.ok(!html.includes('held-question-answer'), 'a would-be row has no answer button');
  assert.ok(!/onclick=/.test(html.split('class="held-question-detail-link"')[0]),
    'the shadow row div carries no click handler');
  assert.ok(html.includes('class="held-question-detail-link"'),
    'the shadow row keeps the read-only Automaton detail link');
  assert.ok(html.includes('data-automaton="prd_77"'), 'the detail link still names the Automaton');
  assert.ok(!html.includes('held-question-change'), 'a would-be row has no change line');
});

test('escaping: hostile text, title, default and ids never reach the HTML raw', () => {
  const HOSTILE = '<img src=x onerror=alert(1)>';
  const box = loadApp();
  const html = rowHtml(box, entry({
    text: HOSTILE,
    title: HOSTILE,
    default: HOSTILE,
    prd_id: HOSTILE,
  }));
  assert.ok(!html.includes(HOSTILE), 'raw hostile HTML must never be spliced in');
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  assert.ok(html.includes(esc(HOSTILE)), 'hostile values show up escaped, inert');
});

test('no question id: the row opens nothing and carries no answer control', () => {
  const box = loadApp();
  const html = rowHtml(box, { prd_id: 'prd_79', title: 'No question here' });
  const rowTag = /<div class="held-question-row"[^>]*>/.exec(html)[0];
  assert.ok(!rowTag.includes('onclick='), 'no question id -\u003e the row div itself opens nothing');
  assert.ok(!html.includes('held-question-answer'), 'no question id -\u003e no answer button');
  assert.ok(html.includes('data-automaton="prd_79"'), 'the detail link is untouched');
});
