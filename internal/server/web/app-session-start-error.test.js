// New Session dialog — a failed start shows the server's plain-text error.
//
// fetch answers /api/sessions/start with an HTTP error and a plain-text body.
// The toast must carry that text unchanged, the dialog must stay open (it is
// never removed), and the Start button must be enabled again.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function setup(status, body) {
  const startBtn = { disabled: false, textContent: 'Start Session', parentElement: { parentNode: { insertBefore() {} } } };
  const modal = makeStubElement();
  let removed = false;
  modal.remove = () => { removed = true; };
  modal.querySelector = () => startBtn;
  const taskInput = makeStubElement();
  const backendSelect = { value: '', selectedIndex: 0, options: [{ dataset: {} }] };
  const ids = { taskInput, newSessionModal: modal, backendSelect };
  const sb = buildSandbox({ byId: ids });
  sb.fetch = () => Promise.resolve({
    ok: false, status, statusText: 'Err',
    json: () => Promise.reject(new SyntaxError('bad json')),
    text: () => Promise.resolve(body),
  });
  vm.createContext(sb);
  loadScript(path.join(__dirname, 'app.js'), sb);
  const toasts = [];
  sb.showToast = (message, type) => toasts.push({ message, type });
  return { sb, toasts, startBtn, isRemoved: () => removed };
}

test('a plain-text 500 body appears unchanged in the toast and the dialog stays open', async () => {
  const msg = 'no available worker: claude-code is paused until 14:00';
  const { sb, toasts, startBtn, isRemoved } = setup(500, msg);
  sb.submitNewSession();
  assert.equal(startBtn.disabled, true);
  await flushAsync(20);
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].type, 'error');
  assert.ok(toasts[0].message.includes(msg), toasts[0].message);
  assert.equal(isRemoved(), false);
  assert.equal(startBtn.disabled, false);
});

test('a 400 plain-text body is shown the same way', async () => {
  const msg = 'project_dir does not exist';
  const { sb, toasts, startBtn } = setup(400, msg);
  sb.submitNewSession();
  await flushAsync(20);
  assert.ok(toasts[0].message.includes(msg));
  assert.equal(startBtn.disabled, false);
});
