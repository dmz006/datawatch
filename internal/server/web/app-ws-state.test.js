// Connection-state indicator: _wsConnState (pure) and the guarantee that a
// socket close during an outage leaves state.sessions untouched.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  const sockets = [];
  sandbox.WebSocket = function () {
    const listeners = {};
    const ws = {
      readyState: 0,
      close() {}, send() {},
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
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._wsConnState, 'function', '_wsConnState is not defined');
  return sandbox;
}

const st = (sb, c, a, o) => vm.runInContext(`_wsConnState(${c}, ${a}, ${o})`, sb);

test('_wsConnState: connected when open and online', () => {
  const sb = loadAppJS();
  assert.equal(st(sb, true, 0, true), 'connected');
});

test('_wsConnState: reconnecting when closed but online', () => {
  const sb = loadAppJS();
  assert.equal(st(sb, false, 3, true), 'reconnecting');
  assert.equal(st(sb, false, 0, undefined), 'reconnecting');
});

test('_wsConnState: offline when navigator.onLine is false', () => {
  const sb = loadAppJS();
  assert.equal(st(sb, false, 2, false), 'offline');
  assert.equal(st(sb, true, 0, false), 'offline');
});

test('close event keeps state.sessions intact and marks reconnecting', () => {
  const sb = loadAppJS();
  vm.runInContext(`state.sessions = [{ id: 'a' }, { id: 'b' }]; connect();`, sb);
  const ws = sb._sockets[0];
  ws.readyState = 1;
  ws.emit('open');
  assert.equal(vm.runInContext('state.connected', sb), true);
  ws.readyState = 3;
  ws.emit('close', { code: 1006 });
  assert.equal(vm.runInContext('state.connected', sb), false);
  assert.equal(vm.runInContext('state.sessions.length', sb), 2);
  assert.equal(vm.runInContext('state.sessions[0].id', sb), 'a');
  assert.equal(vm.runInContext('_wsConnState(state.connected, state.reconnectAttempt, true)', sb), 'reconnecting');
});

test('send() while disconnected toasts at most once per outage', () => {
  const sb = loadAppJS();
  let n = 0;
  sb.showError = () => { n++; };
  vm.runInContext(`send('x', {}); send('x', {}); send('x', {});`, sb);
  assert.equal(n, 1);
});
