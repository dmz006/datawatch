'use strict';
// Regression: the PWA question answer form (window.openQuestion).
//
// The queue card's row button hands off to window.openQuestion(id), which
// loads the question's record (GET /api/questions/{id}) and opens the answer
// dialog: the question text, the options as radio choices with their
// trade-offs, the recommendation (option label and rationale), the default
// as a translated line naming the option or "hold", and the free text box.
// An already answered question additionally shows the current answer's quote
// with a line saying this answer replaces it (that record's supersedes gets
// filled in). Confirm POSTs to /api/questions/{id}/answer with surface pwa
// and the quote sent verbatim: newlines and leading spaces must survive
// exactly (the server applies the "blank values are no-text" rule; trimming
// here would be a protocol bug). If the box is blank and no option is
// chosen, the client refuses with the translated line instead of POSTing.
// Server errors — 409 the question is no longer open, 400 the
// secret-looking-text refusal with the safe routes — surface that message
// in the form rather than a generic error. On success, the dialog closes
// and the queue card refreshes through window.refreshAutomationQuestions
// when present, so that question shows as answered before the next poll.
//
// Run: node --test app-questions-answer.test.js

const fs = require('fs');
const path = require('path');
const test = require('node:test');
const assert = require('node:assert');

const vm = require('vm');
const { buildSandbox, loadScript, flushAsync, makeStubElement } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

const TS = '2026-07-15T09:00:00Z';

// Question fixture: an open question with a recommendation, an option
// default, and a trade-off per option — the full surface of the form.
const OPEN = {
  question: {
    id: 'q42',
    label: 'GPU choice',
    asked_by: 'automaton:worker-7',
    text: 'Which GPU should I use for the fine-tune run?',
    options: [
      { id: 'gpu-b200', label: 'B200', tradeoffs: 'fastest but priciest' },
      { id: 'gpu-h100', label: 'H100', tradeoffs: 'cheaper but slower' },
    ],
    recommendation: { option_id: 'gpu-h100', rationale: 'fits the budget' },
    default: 'gpu-b200',
    state: 'open',
    created_at: TS,
  },
  answer: null,
  answer_count: 0,
  current_answer_id: '',
};

// Same question, already answered once: one answer record to supersede.
const ANSWERED = {
  question: Object.assign({}, OPEN.question, { state: 'answered' }),
  answer: {
    id: 'a1',
    chosen_option_id: 'gpu-b200',
    quote: 'take the B200',
    actor: 'operator',
    surface: 'pwa',
    created_at: TS,
  },
  answer_count: 1,
  current_answer_id: 'a1',
};

function makeSandbox({ post, fixture = OPEN } = {}) {
  const byId = {
    // Confirm button: showModal wires its .onclick here.
    genericModalConfirmBtn: makeStubElement(),
    // Response body: the app writes translated server messages here.
    questionAnswerError: makeStubElement(),
    // Free text box: the app reads .value before POSTing.
    questionAnswerQuote: { value: '' },
    // Dialog host: success closes it via .remove() here.
    genericModal: { removed: 0, remove() { this.removed++; } },
  };
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;

  sandbox._qa = { fetchCalls: [], refresh: 0 };
  sandbox.refreshAutomationQuestions = () => { sandbox._qa.refresh += 1; };

  sandbox.fetch = (url, opts) => {
    const o = opts || {};
    const call = { url: String(url), method: o.method || 'GET', raw: o.body != null ? o.body : undefined };
    sandbox._qa.fetchCalls.push(call);
    if (call.method === 'POST') {
      const r = post || { status: 201 };
      return Promise.resolve({
        ok: r.status >= 200 && r.status < 300,
        status: r.status,
        statusText: r.statusText || '',
        text: () => Promise.resolve(r.text != null ? r.text : ''),
        json: () => Promise.resolve(r.json != null ? r.json : {}),
      });
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(fixture) });
  };
  return sandbox;
}

// openDialog drives window.openQuestion through the real path (the GET
// fetch plus showModal) and settles any pending promises.
function openDialog(sandbox) {
  return vm.runInContext('window.openQuestion("q42")', sandbox).finally(() => flushAsync());
}

// quoteBody returns the POST body as JSON for the single answer request
// made so far in this sandbox (throws when there is none or more than one).
function answerBody(sandbox) {
  const calls = sandbox._qa.fetchCalls.filter(c => c.method === 'POST');
  assert.equal(calls.length, 1);
  return JSON.parse(calls[0].raw);
}

test('GET loads the question and the form shows text, options with trade-offs, recommendation with rationale, default, and the free text box', async () => {
  const s = makeSandbox();
  await openDialog(s);

  const calls = s._qa.fetchCalls.filter(c => c.method === 'GET');
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/questions/q42');

  const html = s._questionAnswerBodyHTML(OPEN.question, null);
  assert.ok(html.includes('Which GPU should I use for the fine-tune run?'), 'question text');
  assert.ok(html.includes('type="radio" name="questionAnswerChoice" value="gpu-b200"'), 'option radio 1');
  assert.ok(html.includes('value="gpu-h100"'), 'option radio 2');
  assert.ok(html.includes('fastest but priciest'), 'option trade-off 1');
  assert.ok(html.includes('cheaper but slower'), 'option trade-off 2');
  assert.ok(html.includes(en.automation_questions_answer_rec.replace('%1$s', 'H100')), 'recommendation as label');
  assert.ok(html.includes('fits the budget'), 'recommendation rationale');
  assert.ok(html.includes(en.automation_questions_answer_default.replace('%1$s', 'B200')), 'default names the option');
  assert.ok(html.includes('id="questionAnswerQuote"'), 'free text box');
});

test('already answered question shows the current answer quote with the replaces-it line', async () => {
  const s = makeSandbox({ fixture: ANSWERED });
  await openDialog(s);

  const html = s._questionAnswerBodyHTML(ANSWERED.question, ANSWERED.answer);
  assert.ok(html.includes('take the B200'), 'current answer quote');
  assert.ok(html.includes(en.automation_questions_answer_supersedes_note), 'replaces-the-current line');
});

test('default hold is shown as hold', () => {
  const s = makeSandbox();
  const q = Object.assign({}, OPEN.question, { default: 'hold' });
  const html = s._questionAnswerBodyHTML(q, null);
  assert.ok(html.includes(en.automation_questions_answer_default.replace('%1$s', 'hold')), 'hold default');
});

test('option only: the request body carries chosen_option_id, blank quote, surface pwa, and no supersedes', async () => {
  const s = makeSandbox();
  await openDialog(s);
  s.document.querySelector = (sel) => sel === 'input[name="questionAnswerChoice"]:checked' ? { value: 'gpu-h100' } : null;
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const body = answerBody(s);
  assert.deepEqual(body, { chosen_option_id: 'gpu-h100', quote: '', surface: 'pwa' });
});

test('text only: the request body carries the quote, no chosen_option_id, surface pwa', async () => {
  const s = makeSandbox();
  await openDialog(s);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "I want the B200, cheaper than B200 pricing is not possible";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const body = answerBody(s);
  assert.equal(body.chosen_option_id, undefined, 'no choice made');
  assert.equal(body.quote, 'I want the B200, cheaper than B200 pricing is not possible');
  assert.equal(body.surface, 'pwa');
});

test('both choice and text: the body carries chosen_option_id and the quote together', async () => {
  const s = makeSandbox();
  await openDialog(s);
  s.document.querySelector = (sel) => sel === 'input[name="questionAnswerChoice"]:checked' ? { value: 'gpu-b200' } : null;
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "agreed, B200, but keep h100 warm as fallback";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const body = answerBody(s);
  assert.equal(body.chosen_option_id, 'gpu-b200');
  assert.equal(body.quote, 'agreed, B200, but keep h100 warm as fallback');
});

test('the quote is sent verbatim: newlines and leading spaces survive', async () => {
  const s = makeSandbox();
  await openDialog(s);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "  lead line\\n    indented\\nsecond";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const body = answerBody(s);
  assert.equal(body.quote, '  lead line\n    indented\nsecond');
});

test('an answered question posts supersedes with the current answer id; an open one omits it', async () => {
  const s1 = makeSandbox({ fixture: ANSWERED });
  await openDialog(s1);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "changed my mind; take the H100";', s1);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s1);
  await flushAsync();
  const b1 = answerBody(s1);
  assert.equal(b1.supersedes, 'a1', 'answered question supersedes the current answer');
  assert.equal(b1.quote, 'changed my mind; take the H100');

  const s2 = makeSandbox({ fixture: OPEN });
  await openDialog(s2);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "take the B200";', s2);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s2);
  await flushAsync();
  const b2 = answerBody(s2);
  assert.equal(b2.supersedes, undefined, 'open question posts no supersedes');
  assert.equal(b2.quote, 'take the B200');
});

test('client-side refuse: neither a choice nor non-blank text does not POST and shows the translated line', async () => {
  const s = makeSandbox();
  await openDialog(s);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "   \\n  ";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  assert.equal(s._qa.fetchCalls.filter(c => c.method === 'POST').length, 0, 'no POST when empty');
  const err = s.document.getElementById('questionAnswerError').innerHTML;
  assert.equal(err, en.automation_questions_answer_required);
});

test('server 409 the question is no longer open: the server message lands in the form and the dialog stays', async () => {
  const s = makeSandbox({ post: { status: 409, text: JSON.stringify({ error: 'question q42 is no longer open' }) } });
  await openDialog(s);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "still there?";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const err = s.document.getElementById('questionAnswerError').innerHTML;
  assert.ok(err.includes('question q42 is no longer open'), 'server message shown in the form; got: ' + err);
  assert.equal(s.document.getElementById('genericModal').removed, 1, 'dialog did not close (open removed it once at creation)');
  assert.equal(s._qa.refresh, 0, 'queue is not refreshed after a refused answer');
});

test('server 400 secret-looking text: the refusal message with the safe routes lands in the form', async () => {
  const refusal = 'answer text looks like a secret — keep it here and instead store it in the vault and tell this session the reference to the reference';
  const s = makeSandbox({ post: { status: 400, text: JSON.stringify({ error: refusal, reason: 'secret_detected' }) } });
  await openDialog(s);
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "sk-abc123def456ghi";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  const err = s.document.getElementById('questionAnswerError').innerHTML;
  assert.ok(err.includes('looks like a secret'), 'secret refusal message shown in the form; got: ' + err);
  assert.ok(err.includes('reference'), 'refusal names the safe routes');
});

test('success closes the dialog and refreshes the queue through window.refreshAutomationQuestions', async () => {
  const s = makeSandbox();
  await openDialog(s);
  const removedAtOpen = s.document.getElementById('genericModal').removed;
  vm.runInContext('document.getElementById("questionAnswerQuote").value = "proceed with the B200";', s);
  vm.runInContext('document.getElementById("genericModalConfirmBtn").onclick()', s);
  await flushAsync();

  assert.equal(answerBody(s).quote, 'proceed with the B200');
  assert.ok(s.document.getElementById('genericModal').removed > removedAtOpen, 'dialog closed on success');
  assert.equal(s._qa.refresh, 1, 'queue refresh called exactly once');
});
