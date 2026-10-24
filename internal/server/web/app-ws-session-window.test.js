// The PWA's side of the bounded session list: a `sessions` frame carries
// summary rows for the default view, session_state merges one row into the
// record already held, session_removed deletes, history comes from REST, and
// an older daemon's hello_ack keeps the legacy stream.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// loadAppJS loads the real app.js with a scripted WebSocket (sockets records
// each one; their sent frames are in sent) and a fetch stub (calls records
// each URL; `route` returns the JSON body for it).
function loadAppJS(route = () => ({ sessions: [], total: 0 })) {
  const sandbox = buildSandbox();
  const sockets = [];
  sandbox.WebSocket = function () {
    const listeners = {};
    const sent = [];
    const ws = {
      readyState: 0, sent,
      close() {},
      send(frame) { sent.push(JSON.parse(frame)); },
      addEventListener(t, fn) { (listeners[t] = listeners[t] || []).push(fn); },
      emit(t, ev) { (listeners[t] || []).forEach(fn => fn(ev || {})); },
    };
    sockets.push(ws);
    return ws;
  };
  sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CLOSING = 2;
  sandbox.WebSocket.CLOSED = 3;
  sandbox._sockets = sockets;
  sandbox._fetchUrls = [];
  sandbox.fetch = url => {
    sandbox._fetchUrls.push(String(url));
    const body = route(String(url));
    return Promise.resolve({
      ok: true, status: 200, headers: { get() { return null; } },
      json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

const run = (sb, code) => vm.runInContext(code, sb);
const handle = (sb, msg) => { sb._handle = msg; run(sb, 'handleMessage(_handle)'); };

// A held record, as the detail view and the legacy stream see it.
const FULL = {
  id: 's1', full_id: 'h-s1', name: 'one', state: 'running', updated_at: '2026-10-24T04:00:00Z',
  task: 'the whole task text', last_response: 'a full answer', input_mode: 'none', channel_ready: true,
};
// A summary row: the window / summary shape, no task or last_response.
const ROW = (extra = {}) => Object.assign({
  id: 's1', full_id: 'h-s1', name: 'one', state: 'running', updated_at: '2026-10-24T04:05:00Z',
  last_activity: '2026-10-24T04:05:00Z', created_at: '2026-10-24T03:00:00Z',
}, extra);
const WINDOW_FRAME = (rows, extra = {}) => ({ type: 'sessions', data: Object.assign({ sessions: rows, version: 'v', window_minutes: 5, total: rows.length, more: false }, extra) });

test('a sessions frame of summary rows merges without losing held task text', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(FULL)}]`);
  handle(sb, WINDOW_FRAME([ROW({ status_line: 'working' })]));
  assert.equal(run(sb, 'state.sessions.length'), 1);
  assert.equal(run(sb, 'state.sessions[0].task'), 'the whole task text');
  assert.equal(run(sb, 'state.sessions[0].last_response'), 'a full answer');
  assert.equal(run(sb, 'state.sessions[0].input_mode'), 'none');
  assert.equal(run(sb, 'state.sessions[0].channel_ready'), true);
  assert.equal(run(sb, 'state.sessions[0].status_line'), 'working');
});

test('a summary key the row omits is empty now, not stale', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(ROW({ status_line: 'old line', tokens_in: 9 }))}]`);
  handle(sb, WINDOW_FRAME([ROW()]));
  assert.equal(run(sb, 'state.sessions[0].status_line'), undefined);
  assert.equal(run(sb, 'state.sessions[0].tokens_in'), undefined);
});

test('a full record in a frame replaces the held one (no stale fields)', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(Object.assign({}, FULL, { channel_ready: true }))}]`);
  const full = Object.assign({}, FULL, { state: 'complete', updated_at: '2026-10-24T04:06:00Z' });
  delete full.channel_ready;
  handle(sb, { type: 'sessions', data: { sessions: [full], window_minutes: 5, total: 1, more: false } });
  assert.equal(run(sb, 'state.sessions[0].state'), 'complete');
  assert.equal(run(sb, 'state.sessions[0].channel_ready'), undefined);
});

test('history rows fetched over REST survive a later sessions frame', async () => {
  const hist = { id: 'old', full_id: 'h-old', name: 'archived one', state: 'complete', updated_at: '2026-10-20T00:00:00Z', last_activity: '2026-10-20T00:00:00Z' };
  const sb = loadAppJS(url => (url.includes('view=summary') ? { sessions: [hist], total: 1 } : {}));
  run(sb, 'state.connected = true; state.showHistory = false; loadSessionsList()');
  await flushAsync(20);
  assert.equal(run(sb, 'state.sessions.some(s => s.full_id === "h-old")'), true);
  handle(sb, WINDOW_FRAME([ROW()]));
  assert.equal(run(sb, 'state.sessions.some(s => s.full_id === "h-old")'), true);
  assert.equal(run(sb, 'state.sessions.some(s => s.full_id === "h-s1")'), true);
});

test('a websocket-only session the frame omits leaves the list', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(ROW({ full_id: 'h-gone', id: 'gone' }))}]`);
  handle(sb, WINDOW_FRAME([ROW()]));
  assert.equal(run(sb, 'state.sessions.some(s => s.full_id === "h-gone")'), false);
});

test('session_state merges one summary row into the held record', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(FULL)}]`);
  handle(sb, { type: 'session_state', data: { session: ROW({ state: 'waiting_input', updated_at: '2026-10-24T04:07:00Z' }) } });
  assert.equal(run(sb, 'state.sessions[0].state'), 'waiting_input');
  assert.equal(run(sb, 'state.sessions[0].task'), 'the whole task text');
  assert.equal(run(sb, 'state.sessions[0].input_mode'), 'none');
  assert.equal(run(sb, 'state.sessions.length'), 1);
});

test('session_state with a row for a new session adds it', () => {
  const sb = loadAppJS();
  handle(sb, { type: 'session_state', data: { session: ROW({ full_id: 'h-new', id: 'new' }) } });
  assert.equal(run(sb, 'state.sessions[0].full_id'), 'h-new');
});

test('session_removed deletes the session from the list', () => {
  const sb = loadAppJS();
  run(sb, `state.sessions = [${JSON.stringify(FULL)}, ${JSON.stringify(ROW({ full_id: 'h-keep', id: 'keep' }))}]`);
  handle(sb, { type: 'session_removed', data: { session_id: 'h-s1' } });
  assert.equal(run(sb, 'state.sessions.length'), 1);
  assert.equal(run(sb, 'state.sessions[0].full_id'), 'h-keep');
});

test('window_minutes from the frame sets the recent window', () => {
  const sb = loadAppJS();
  run(sb, 'state._recentMinutes = 5');
  handle(sb, WINDOW_FRAME([ROW()], { window_minutes: 12 }));
  assert.equal(run(sb, 'state._recentMinutes'), 12);
});

test('a frame without window_minutes keeps the configured window', () => {
  const sb = loadAppJS();
  run(sb, 'state._recentMinutes = 7');
  handle(sb, { type: 'sessions', data: { sessions: [ROW()] } });
  assert.equal(run(sb, 'state._recentMinutes'), 7);
});

test('open sends one hello that asks for the window form', () => {
  const sb = loadAppJS();
  run(sb, 'connect()');
  const ws = sb._sockets[0];
  ws.readyState = 1;
  ws.emit('open');
  const hellos = ws.sent.filter(m => m.type === 'hello');
  assert.equal(hellos.length, 1);
  assert.deepEqual(hellos[0].data, { session_list: 'window', session_view: 'summary' });
});

test('hello_ack with window marks the window form on', () => {
  const sb = loadAppJS();
  handle(sb, { type: 'hello_ack', data: { session_list: 'window', session_view: 'summary', capabilities: ['ws.session_list_window'] } });
  assert.equal(run(sb, 'state._sessionListWindow'), true);
});

test('an older daemon hello_ack falls back and REST keeps the list working', async () => {
  const sb = loadAppJS(url => (url.includes('view=summary') ? { sessions: [ROW()], total: 1 } : {}));
  run(sb, 'state.connected = true; state.activeView = "sessions"');
  handle(sb, { type: 'hello_ack', data: { session_list: 'full', session_view: 'full', capabilities: [] } });
  await flushAsync(20);
  assert.equal(run(sb, 'state._sessionListWindow'), false);
  assert.ok(sb._fetchUrls.some(u => u.startsWith('/api/sessions?view=summary&limit=50')));
  assert.equal(run(sb, 'state.sessions.some(s => s.full_id === "h-s1")'), true);
});

test('history on pages the REST list with history=1 and never the websocket', async () => {
  const sb = loadAppJS(() => ({ sessions: [], total: 0 }));
  run(sb, 'state.connected = true; state.showHistory = true; loadSessionsList()');
  await flushAsync(20);
  const url = sb._fetchUrls.find(u => u.includes('view=summary'));
  assert.ok(url, 'no summary request');
  assert.ok(url.includes('limit=50') && url.includes('history=1'), url);
  run(sb, 'connect()');
  const ws = sb._sockets[0];
  ws.readyState = 1;
  ws.emit('open');
  assert.deepEqual(ws.sent.map(m => m.type), ['hello']);
});

test('a summary row with no task fetches the full record on detail open, once', async () => {
  const sb = loadAppJS(url => (url === '/api/sessions/h-s1' ? FULL : { sessions: [], total: 0 }));
  run(sb, `state.sessions = [${JSON.stringify(ROW())}]`);
  run(sb, `_ensureFullSession('h-s1', state.sessions[0]); _ensureFullSession('h-s1', state.sessions[0])`);
  await flushAsync(20);
  assert.equal(sb._fetchUrls.filter(u => u === '/api/sessions/h-s1').length, 1);
  assert.equal(run(sb, 'state.sessions[0].task'), 'the whole task text');
  assert.equal(run(sb, 'state.sessions[0].input_mode'), 'none');
});
