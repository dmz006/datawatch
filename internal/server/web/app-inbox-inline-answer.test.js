// Inbox inline answers and the held-question hold line.
//
// Pins, against the REAL app.js loaded into the shared stub browser:
//   • an open question row shows one button per option; a click posts that
//     answer in place (POST /api/questions/{id}/answer, chosen_option_id
//     only, no text) and never opens the answer dialog,
//   • a question with no options shows no option buttons,
//   • an open held question shows the change-the-hold line with a link to
//     Settings under Automata; a would-be (shadow) entry shows no such line,
//   • openAutomataSettings selects the Automata Settings tab and navigates
//     to Settings. It writes no configuration.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._i18n.fallback = bundle('en');
  return sandbox;
}

const question = (extra) => Object.assign({
  id: 'q_201',
  text: 'Which backend should the fine-tune use?',
  asked_by: 'pipeline',
  options: [
    { id: 'a', label: 'Use A100', tradeoffs: 'cheaper, slower' },
    { id: 'b', label: 'Use B200', tradeoffs: 'faster, dearer' },
  ],
}, extra);

test('open question: one option button per option, carrying the ids as data attributes', () => {
  const s = loadApp();
  const html = vm.runInContext('window.questionsCardHtml({questions: [' + JSON.stringify(question()) + ']})', s);
  assert.ok(html.includes('class="btn-secondary questions-option-btn"'), 'option buttons rendered');
  assert.ok(html.includes('data-question-id="q_201" data-option-id="a"'), 'option a carries its ids');
  assert.ok(html.includes('data-question-id="q_201" data-option-id="b"'), 'option b carries its ids');
  assert.ok(html.includes('Use A100') && html.includes('Use B200'), 'option labels shown');
  assert.ok(html.includes('title="faster, dearer"'), 'trade-off shown as the button title');
  assert.ok(html.includes('window.answerQuestionOption(this)'), 'click hands off to the in-place answer');
  assert.ok(html.includes('questions-open-btn'), 'the free-text dialog button is still there');
});

test('open question with no options: no option buttons', () => {
  const s = loadApp();
  const html = vm.runInContext('window.questionsCardHtml({questions: [' + JSON.stringify(question({ options: [] })) + ']})', s);
  assert.ok(!html.includes('questions-option-btn'), 'no option buttons');
  assert.ok(html.includes('q_201'), 'the row still renders');
});

test('clicking an option posts that answer in place: POST /api/questions/{id}/answer with the option, no text, never the dialog', async () => {
  const s = loadApp();
  const calls = [];
  s.fetch = (url, o) => {
    calls.push({ url: String(url), method: (o && o.method) || 'GET', body: o && o.body ? JSON.parse(o.body) : null });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}), text: () => Promise.resolve('') });
  };
  let dialogOpened = false;
  s.showModal = () => { dialogOpened = true; };
  const btn = { dataset: { questionId: 'q_201', optionId: 'b' }, disabled: false };
  vm.runInContext('window.answerQuestionOption(_btn)', Object.assign(s, { _btn: btn }));
  assert.equal(btn.disabled, true, 'the button is disabled while the answer is in flight');
  await flushAsync();
  const post = calls.find(c => c.method === 'POST');
  assert.ok(post, 'a POST was sent');
  assert.equal(post.url, '/api/questions/q_201/answer', 'posts to the answer route');
  assert.deepEqual(post.body, { surface: 'pwa', chosen_option_id: 'b', quote: '' }, 'exact body: the chosen option and no text');
  assert.equal(dialogOpened, false, 'the answer dialog is not opened');
});

test('a failed option answer re-enables the buttons and shows the server message', async () => {
  const s = loadApp();
  s.fetch = () => Promise.resolve({ ok: false, status: 409, text: () => Promise.resolve('{"error":"question is no longer open"}') });
  const toasts = [];
  s.showToast = (msg, kind) => { toasts.push({ msg, kind }); };
  const btn = { dataset: { questionId: 'q_201', optionId: 'a' }, disabled: false };
  vm.runInContext('window.answerQuestionOption(_btn)', Object.assign(s, { _btn: btn }));
  await flushAsync();
  assert.equal(btn.disabled, false, 'the button is enabled again');
  assert.deepEqual(toasts, [{ msg: 'question is no longer open', kind: 'error' }], 'server message shown');
});

test('held question (open): shows the change-the-hold line with a Settings link; shadow entries do not', () => {
  const s = loadApp();
  const en = bundle('en');
  const entry = { question_id: 'q_102', automaton_id: 'prd_42', task_id: 'task_7', state: 'review_held', reason: 'Blocked' };
  const open = vm.runInContext('window.heldQuestionRowHtml(' + JSON.stringify(entry) + ')', s);
  assert.ok(open.includes('class="held-question-change"'), 'open hold has the change line');
  assert.ok(open.includes(en.triage_question_held_change), 'the change line uses the translated hold text');
  assert.ok(open.includes('class="held-question-settings-link"') && open.includes('window.openAutomataSettings()'), 'link to Settings under Automata');
  const shadow = vm.runInContext('window.heldQuestionRowHtml(' + JSON.stringify(entry) + ', true)', s);
  assert.ok(!shadow.includes('held-question-change'), 'a would-be entry has no change line');
});

test('openAutomataSettings: selects the Automata tab and navigates to Settings', () => {
  const s = loadApp();
  const nav = [];
  s.navigate = (view) => { nav.push(view); };
  vm.runInContext('window.openAutomataSettings()', s);
  assert.equal(vm.runInContext('_settingsTab', s), 'automata', 'Automata tab selected');
  assert.deepEqual(nav, ['settings'], 'navigated to Settings');
});
