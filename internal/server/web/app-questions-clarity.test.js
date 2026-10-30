'use strict';
// Regression: the PWA question answer dialog explains itself.
//
// The dialog title shows the question's label (the id when the label is
// empty) with a small secondary line carrying the id. The answer box has a
// plain label and a helper line saying what is saved and who reads it. Each
// option shows its trade-off as a visible line (not only a tooltip). The
// asker's recommendation and what happens when nobody answers are stated in
// plain sentences, with a no-rationale and a hold variant. Every new key is
// in all five bundles, and the existing answer tests keep passing.
//
// Run: node --test app-questions-clarity.test.js

const fs = require('fs');
const path = require('path');
const test = require('node:test');
const assert = require('node:assert');
const vm = require('vm');
const { spawnSync } = require('child_process');
const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundles = {};
LANGS.forEach(l => { bundles[l] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8')); });
const en = bundles.en;

const NEW_KEYS = [
  'automation_questions_answer_id_line',
  'automation_questions_answer_text_label',
  'automation_questions_answer_text_help',
  'automation_questions_answer_rec_line',
  'automation_questions_answer_rec_line_no_rationale',
  'automation_questions_answer_default_line',
  'automation_questions_answer_default_line_hold',
];
const OLD_KEYS = [
  'automation_questions_answer_title',
  'automation_questions_answer_rec',
  'automation_questions_answer_default',
  'automation_questions_answer_quote_label',
];

const Q = {
  id: 'q42',
  label: 'GPU choice',
  text: 'Which GPU should I use for the fine-tune run?',
  options: [
    { id: 'gpu-b200', label: 'B200', tradeoffs: 'fastest but priciest' },
    { id: 'gpu-h100', label: 'H100', tradeoffs: 'cheaper but slower' },
  ],
  recommendation: { option_id: 'gpu-h100', rationale: 'fits the budget' },
  default: 'gpu-b200',
  state: 'open',
};

function makeSandbox(question) {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;
  sandbox._modals = [];
  sandbox.showModal = (opts) => { sandbox._modals.push(opts); };
  sandbox.fetch = () => Promise.resolve({
    ok: true, status: 200,
    json: () => Promise.resolve({ question, answer: null, answer_count: 0, current_answer_id: '' }),
  });
  return sandbox;
}

async function openTitle(question) {
  const s = makeSandbox(question);
  await vm.runInContext('window.openQuestion("q42")', s).finally(() => flushAsync());
  assert.equal(s._modals.length, 1, 'the dialog opened once');
  return s._modals[0];
}

const fill = (key, ...args) => args.reduce((str, a, i) => str.replace('%' + (i + 1) + '$s', a), en[key]);

test('title shows the question label, and falls back to the id when the label is empty', async () => {
  const withLabel = await openTitle(Q);
  assert.equal(withLabel.title, fill('automation_questions_answer_title', 'GPU choice'));
  assert.ok(!withLabel.title.includes('q42'), 'the id is not in the title when a label exists');

  const noLabel = await openTitle(Object.assign({}, Q, { label: '' }));
  assert.equal(noLabel.title, fill('automation_questions_answer_title', 'q42'));

  const missing = await openTitle(Object.assign({}, Q, { label: undefined }));
  assert.equal(missing.title, fill('automation_questions_answer_title', 'q42'));
});

test('a small secondary line under the title shows the id', async () => {
  const modal = await openTitle(Q);
  assert.ok(modal.body.includes('class="question-answer-id-line"'), 'id line element');
  assert.ok(modal.body.includes(fill('automation_questions_answer_id_line', 'q42')), 'id line text');
  assert.ok(modal.body.indexOf('question-answer-id-line') < modal.body.indexOf('question-answer-text"'), 'id line comes before the question text');
});

test('answer box has the plain label and the helper line, not the old label', () => {
  const html = makeSandbox(Q)._questionAnswerBodyHTML(Q, null);
  assert.ok(html.includes(en.automation_questions_answer_text_label), 'plain label');
  assert.ok(html.includes(en.automation_questions_answer_text_help), 'helper line');
  assert.ok(!html.includes(en.automation_questions_answer_quote_label), 'old optional-when-you-pick label is gone');
  assert.ok(html.includes('id="questionAnswerQuote"'), 'the box itself is unchanged');
  assert.ok(en.automation_questions_answer_text_help.startsWith('Optional when you pick an option. Whatever you type is saved exactly as written'), 'helper wording');
  assert.ok(html.indexOf('id="questionAnswerQuote"') < html.indexOf('question-answer-text-help'), 'helper sits under the box');
});

test('each option shows its trade-off as a visible line, and keeps the tooltip', () => {
  const html = makeSandbox(Q)._questionAnswerBodyHTML(Q, null);
  assert.ok(html.includes('<div class="question-answer-tradeoff"'), 'visible trade-off element');
  assert.ok(html.includes('>fastest but priciest</div>'), 'trade-off 1 as a line');
  assert.ok(html.includes('>cheaper but slower</div>'), 'trade-off 2 as a line');
  assert.ok(html.includes('title="fastest but priciest"'), 'tooltip kept');

  const bare = makeSandbox(Q)._questionAnswerBodyHTML(Object.assign({}, Q, { options: [{ id: 'a', label: 'A' }] }), null);
  assert.ok(!bare.includes('question-answer-tradeoff'), 'no empty trade-off line for an option without one');
});

test('the recommendation is stated plainly, with and without a rationale', () => {
  const s = makeSandbox(Q);
  const html = s._questionAnswerBodyHTML(Q, null);
  assert.ok(html.includes(fill('automation_questions_answer_rec_line', 'H100', 'fits the budget')), 'with rationale');
  assert.ok(html.includes(en.automation_questions_answer_rec.replace('%1$s', 'H100')), 'short recommended line kept');

  const noRat = s._questionAnswerBodyHTML(Object.assign({}, Q, { recommendation: { option_id: 'gpu-h100' } }), null);
  assert.ok(noRat.includes(fill('automation_questions_answer_rec_line_no_rationale', 'H100')), 'no-rationale variant');
  assert.ok(!noRat.includes('because'), 'no dangling because');

  const none = s._questionAnswerBodyHTML(Object.assign({}, Q, { recommendation: null }), null);
  assert.ok(!none.includes('question-answer-rec'), 'no recommendation, no line');
});

test('what happens if nobody answers: names the option, or says the question stays on hold', () => {
  const s = makeSandbox(Q);
  const opt = s._questionAnswerBodyHTML(Q, null);
  assert.ok(opt.includes(fill('automation_questions_answer_default_line', 'B200')), 'option default sentence');
  assert.ok(!opt.includes(en.automation_questions_answer_default_line_hold), 'not the hold variant');

  const hold = s._questionAnswerBodyHTML(Object.assign({}, Q, { default: 'hold' }), null);
  assert.ok(hold.includes(en.automation_questions_answer_default_line_hold), 'hold variant');
  assert.ok(hold.includes(en.automation_questions_answer_default.replace('%1$s', 'hold')), 'short default line kept');

  const empty = s._questionAnswerBodyHTML(Object.assign({}, Q, { default: '' }), null);
  assert.ok(empty.includes(en.automation_questions_answer_default_line_hold), 'empty default is hold');
});

test('operator-facing wording says Automaton or Automata, never PRD, and has no tracker id', () => {
  NEW_KEYS.forEach(k => {
    assert.ok(!/\bPRD\b/i.test(en[k]), k + ' has no PRD');
    assert.ok(!/\b[A-Z]{1,3}\d{2,}\b/.test(en[k]), k + ' has no tracker id');
  });
});

test('every new key is in all five bundles; the old keys are kept', () => {
  LANGS.forEach(l => {
    NEW_KEYS.concat(OLD_KEYS).forEach(k => {
      assert.equal(typeof bundles[l][k], 'string', l + ' has ' + k);
      assert.ok(bundles[l][k].length > 0, l + ' ' + k + ' is non-empty');
    });
  });
  // The queue test guards that no automation_questions_ key is an English
  // copy, so the four non-English bundles carry real translations.
  LANGS.filter(l => l !== 'en').forEach(l => {
    NEW_KEYS.forEach(k => assert.notEqual(bundles[l][k], en[k], l + ' translates ' + k));
  });
  // Placeholders survive in every bundle.
  assert.ok(en.automation_questions_answer_rec_line.includes('%1$s') && en.automation_questions_answer_rec_line.includes('%2$s'));
  assert.ok(en.automation_questions_answer_default_line.includes('%1$s'));
  assert.ok(en.automation_questions_answer_id_line.includes('%1$s'));
  const ph = (str) => (str.match(/%\d+\$s/g) || []).sort().join(',');
  LANGS.forEach(l => NEW_KEYS.forEach(k => assert.equal(ph(bundles[l][k]), ph(en[k]), l + ' placeholders for ' + k)));
});

test('the existing answer tests still pass', () => {
  const r = spawnSync(process.execPath, ['--test', path.join(__dirname, 'app-questions-answer.test.js')], { encoding: 'utf8' });
  assert.equal(r.status, 0, 'app-questions-answer.test.js failed:\n' + r.stdout + r.stderr);
});
