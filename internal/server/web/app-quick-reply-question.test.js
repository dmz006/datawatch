'use strict';

// quick-reply → open question routing (sessions quick commands, card quick
// commands, chat chips, alerts select):
//
//   (a) the session has an open question and the quick reply matches one of
//       its options → the option is POSTed to /api/questions/{id}/answer
//       with surface pwa (the "Approved"/yes quick reply resolves the Yes
//       option) and nothing is typed into the session;
//   (b) the session has no open question → the quick reply is typed into
//       the session exactly as before (send_input on the card path,
//       `send <short>: <text>` on the alerts path);
//   (c) the session has an open question but the quick reply matches none
//       of its options → the reply is refused with a message naming the
//       pressed label and the option labels; nothing is answered, nothing
//       is typed;
//   (d) the session has more than one open question → the reply is refused
//       and the message points to the Questions page;
//   (e) the answer POST fails (409 already answered elsewhere) → the
//       server's message shows; nothing is typed.
//
// The real app.js (including quickReplyRoute, _questionAnswerSubmit,
// apiFetch, showToast, cardSendCmd, alertSendCmd, chatQuickReply) is loaded
// in a Node vm sandbox and the global fetch is stubbed — so these tests
// exercise the same routing the browser runs.

const test = require('node:test');
const assert = require('node:assert');
const vm = require('node:vm');
const path = require('node:path');
const { buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

// One open question for session 'sess-abc' (the short id the daemon
// stores) with a Yes/No option pair.
const QUESTION = {
  id: 'q-1',
  state: 'open',
  session_id: 'sess-abc',
  options: [{ id: 'yes', label: 'Yes' }, { id: 'no', label: 'No' }],
};

function response(status, body) {
  const text = typeof body === 'string' ? body : JSON.stringify(body);
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(text),
  };
}

// makeWorld wires the sandbox for one scenario:
//   questions        array of question records served by the list call
//   answerStatus     status of POST /api/questions/{id}/answer (200 ok)
//   answerError      { status, message } → answer call fails with that
//                     {"error": message} body (409/400 path)
function makeWorld({ questions, answerStatus = 200, answerError = null } = {}) {
  const sb = buildSandbox({ byId: {} });
  vm.createContext(sb);
  loadScript(path.join(__dirname, 'app.js'), sb);
  // The real en bundle so t() resolves the quick_reply_* strings.
  sb._i18n.bundle = require('./locales/en.json');

  const fetchCalls = [];
  const sendCalls = [];
  const toasts = [];
  let refreshes = 0;

  sb.fetch = function (url, init) {
    fetchCalls.push({ url: String(url), init: init || null });
    const u = String(url);
    if (u.indexOf('/api/questions/') === 0 && u.indexOf('/answer') !== -1) {
      if (answerError) {
        return Promise.resolve(response(answerError.status, { error: answerError.message }));
      }
      return Promise.resolve(response(answerStatus, Object.assign({}, QUESTION, { state: 'answered' })));
    }
    if (u.indexOf('/api/questions') === 0) {
      return Promise.resolve(response(200, { count: questions.length, questions }));
    }
    if (u.indexOf('/api/command') === 0) {
      return Promise.resolve(response(200, { status: 'accepted' }));
    }
    return Promise.resolve(response(200, {}));
  };

  // The tmux send sink: a quick reply "typed into the session" lands here.
  sb.send = function (type, data) { sendCalls.push([type, data]); return true; };

  // Toast sink: showToast routes through pushToAlertDock.
  sb.pushToAlertDock = function (message, type) { toasts.push([String(message), String(type)]); };

  // The question card refresh hook quickReplyRoute calls on success.
  sb.refreshAutomationQuestions = function () { refreshes += 1; };

  const run = (code) => vm.runInContext(code, sb, { filename: 'test-context.js' });
  return {
    sb, fetchCalls, sendCalls, toasts,
    refreshes: () => refreshes,
    setActiveSession: (id) => { run(`state.activeSession = ${JSON.stringify(id)};`); },
    flush: () => flushAsync(),
  };
}

test('quick reply answers the open question with the matching option (surface pwa), types nothing', async () => {
  const w = makeWorld({ questions: [QUESTION] });
  // The session select carries the full id; the question record stores the
  // short id. The "Approved" label (value "yes") must resolve to the Yes
  // option through the approve/approved/yes synonym group.
  w.setActiveSession('local-sess-abc');
  w.sb.cardSendCmd('local-sess-abc', 'yes', 'Approved');
  await w.flush();

  const list = w.fetchCalls.filter(c => c.url.indexOf('/api/questions?') === 0);
  assert.ok(list.length >= 1, 'the open question list was fetched');

  const answer = w.fetchCalls.find(c => c.url === '/api/questions/q-1/answer');
  assert.ok(answer, 'the answer was POSTed to /api/questions/{id}/answer');
  assert.strictEqual(answer.init.method, 'POST');
  const body = JSON.parse(answer.init.body);
  assert.strictEqual(body.surface, 'pwa');
  assert.strictEqual(body.chosen_option_id, 'yes');
  assert.strictEqual(body.quote, '');

  assert.strictEqual(w.sendCalls.length, 0, 'nothing was typed into the session');

  const okToast = w.toasts.find(([m, t]) => t === 'success');
  assert.ok(okToast, 'a success toast was shown');
  assert.ok(okToast[0].indexOf('Yes') !== -1, 'the toast names the option chosen: ' + okToast[0]);
  assert.strictEqual(w.refreshes(), 1, 'the question card was refreshed');
});

test('no open question: the quick reply is typed into the session as before', async () => {
  const w = makeWorld({ questions: [] });
  w.setActiveSession('local-sess-abc');

  // Card quick command path.
  w.sb.cardSendCmd('local-sess-abc', 'yes');
  await w.flush();
  assert.strictEqual(w.sendCalls.length, 1, 'the card quick command was typed into the session');
  // The data object was created inside the VM realm, so compare by JSON
  // rather than deepStrictEqual (cross-realm prototypes).
  assert.strictEqual(JSON.stringify(w.sendCalls[0]),
    JSON.stringify(['send_input', { session_id: 'local-sess-abc', text: 'yes' }]));

  // Alerts quick-reply select path (sends `send <short>: <text>`).
  w.sb.alertSendCmd('local-sess-abc', 'Stop');
  await w.flush();
  const cmd = w.fetchCalls.find(c => c.url === '/api/command');
  assert.ok(cmd, 'the alerts quick reply used the command endpoint as before');
  // The body is the existing raw object shape (alertSendCmd's original
  // body); accept it stringified or as the object itself.
  const cmdBody = typeof cmd.init.body === 'string' ? JSON.parse(cmd.init.body) : cmd.init.body;
  assert.strictEqual(cmdBody.text, 'send abc: Stop');

  assert.ok(!w.fetchCalls.some(c => c.url.indexOf('/answer') !== -1), 'nothing was answered');
  assert.strictEqual(w.toasts.filter(([m, t]) => t === 'error').length, 0, 'no error toast');
});

test('open question, no matching option: the quick reply is refused and nothing is sent', async () => {
  const w = makeWorld({ questions: [QUESTION] });
  w.setActiveSession('local-sess-abc');

  // The "Stop" chip matches neither the yes nor the no option.
  w.sb.chatQuickReply('Stop');
  await w.flush();

  assert.ok(!w.fetchCalls.some(c => c.url.indexOf('/answer') !== -1), 'no answer was posted');
  assert.strictEqual(w.sendCalls.length, 0, 'nothing was typed into the session');

  const errToast = w.toasts.find(([m, t]) => t === 'error');
  assert.ok(errToast, 'an error toast was shown');
  const msg = errToast[0];
  assert.ok(msg.indexOf('Stop') !== -1, 'the message names the pressed label: ' + msg);
  assert.ok(msg.indexOf('Yes') !== -1 && msg.indexOf('No') !== -1, 'the message names the option labels: ' + msg);
});

test('more than one open question: the quick reply is refused and points to the Questions page', async () => {
  const w = makeWorld({ questions: [
    QUESTION,
    { id: 'q-2', state: 'open', session_id: 'sess-abc', options: [{ id: 'keep', label: 'Keep' }] },
  ]});
  w.setActiveSession('local-sess-abc');

  w.sb.cardSendCmd('local-sess-abc', 'yes', 'Approved');
  await w.flush();

  assert.ok(!w.fetchCalls.some(c => c.url.indexOf('/answer') !== -1), 'no answer was posted');
  assert.strictEqual(w.sendCalls.length, 0, 'nothing was typed into the session');

  const errToast = w.toasts.find(([m, t]) => t === 'error');
  assert.ok(errToast, 'an error toast was shown');
  assert.ok(errToast[0].toLowerCase().indexOf('questions page') !== -1,
    'the message points to the Questions page: ' + errToast[0]);
});

test('answer rejected (409 already answered elsewhere): the server message shows, nothing is typed', async () => {
  const w = makeWorld({
    questions: [QUESTION],
    answerError: { status: 409, message: 'question was answered by another surface at 2026-11-09T09:00:00Z' },
  });
  w.setActiveSession('local-sess-abc');

  w.sb.cardSendCmd('local-sess-abc', 'yes', 'Approved');
  await w.flush();

  assert.strictEqual(w.sendCalls.length, 0, 'nothing was typed into the session');
  const errToast = w.toasts.find(([m, t]) => t === 'error');
  assert.ok(errToast, 'an error toast was shown');
  assert.ok(errToast[0].indexOf('question was answered by another surface') !== -1,
    'the server message is shown: ' + errToast[0]);
  assert.strictEqual(w.refreshes(), 0, 'the question card was not refreshed');
});
