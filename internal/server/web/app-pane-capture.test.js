// PWA terminal live-update stall (docs/plans/2026-10-20-pwa-terminal-live-update-findings.md):
// the pane_capture handler dropped frames arriving <33ms apart and while the
// xterm viewport was scrolled up, and the server only resends on change, so the
// screen froze on a stale frame. These tests cover the trailing-edge throttle,
// the held-while-scrolled-up frame + jump-to-live, and the silence re-subscribe.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const sandbox = buildSandbox();
  sandbox.WebSocket.OPEN = 1;
  sandbox.timers = [];
  sandbox.setTimeout = (fn) => { sandbox.timers.push(fn); return sandbox.timers.length; };
  sandbox.clearTimeout = () => {};
  sandbox.perfNow = 1000;
  sandbox.performance = { now: () => sandbox.perfNow };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  vm.runInContext(`
    __writes = [];
    __sent = [];
    __scrolled = 0;
    state.activeView = 'session-detail';
    state.activeSession = 's1';
    state._termHasContent = true;
    state._lastPaneFrame = null;
    state.terminal = {
      buffer: { active: { viewportY: 0, baseY: 0 } },
      write(d) { __writes.push(d); },
      reset() {},
      scrollToBottom() { __scrolled++; this.buffer.active.viewportY = this.buffer.active.baseY; },
    };
    state.ws = { readyState: WebSocket.OPEN, send(m) { __sent.push(JSON.parse(m)); } };
  `, sandbox);
  sandbox.timers.length = 0; // drop timers scheduled by app.js load-time code
  return sandbox;
}
const frame = (txt) => `{ session_id: 's1', lines: [${JSON.stringify(txt)}] }`;
const run = (sb, code) => vm.runInContext(code, sb);

test('frames 10ms apart: the second is held then drawn by the flush timer', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('one')})`);
  sb.perfNow += 10;
  run(sb, `handlePaneCapture(${frame('two')})`);
  assert.equal(sb.__writes.length, 1);
  assert.equal(sb.timers.length, 1, 'flush timer scheduled');
  sb.timers.shift()();
  assert.equal(sb.__writes.length, 2);
  assert.ok(sb.__writes[1].endsWith('two'));
});

test('burst keeps only the newest dropped frame', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('a')})`);
  sb.perfNow += 5; run(sb, `handlePaneCapture(${frame('b')})`);
  sb.perfNow += 5; run(sb, `handlePaneCapture(${frame('c')})`);
  assert.equal(sb.timers.length, 1, 'one timer for the burst');
  sb.timers.shift()();
  assert.ok(sb.__writes[sb.__writes.length - 1].endsWith('c'));
});

test('frame after the throttle window is drawn immediately', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('a')})`);
  sb.perfNow += 50;
  run(sb, `handlePaneCapture(${frame('b')})`);
  assert.equal(sb.__writes.length, 2);
  assert.equal(sb.timers.length, 0);
});

test('flush for a session no longer on screen draws nothing', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('a')})`);
  sb.perfNow += 5; run(sb, `handlePaneCapture(${frame('b')})`);
  run(sb, `state.activeSession = 'other'`);
  sb.timers.shift()();
  assert.equal(sb.__writes.length, 1);
});

test('scrolled up: frame is held and shown after jump-to-live', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('a')})`);
  sb.perfNow += 50;
  run(sb, `state.terminal.buffer.active.baseY = 20; state.terminal.buffer.active.viewportY = 5;`);
  run(sb, `handlePaneCapture(${frame('b')})`);
  assert.equal(sb.__writes.length, 1, 'not drawn while scrolled up');
  assert.ok(run(sb, `state._pendingPaneFrame`), 'frame retained');
  run(sb, `_paneJumpToLive()`);
  assert.equal(sb.__scrolled, 1);
  assert.equal(sb.__writes.length, 2);
  assert.ok(sb.__writes[1].endsWith('b'));
  assert.equal(run(sb, `state._pendingPaneFrame`), null);
});

test('identical keyframe resend is not redrawn', () => {
  const sb = load();
  run(sb, `handlePaneCapture(${frame('a')})`);
  sb.perfNow += 50; run(sb, `handlePaneCapture(${frame('b')})`);
  sb.perfNow += 50; run(sb, `handlePaneCapture(${frame('b')})`);
  assert.equal(sb.__writes.length, 2);
});

test('_paneNeedsResubscribe: strict threshold', () => {
  const sb = load();
  assert.equal(run(sb, `_paneNeedsResubscribe(0, PANE_SILENCE_MS)`), false);
  assert.equal(run(sb, `_paneNeedsResubscribe(0, PANE_SILENCE_MS + 1)`), true);
});

test('_paneSilenceCheck re-sends subscribe once per silence window', () => {
  const sb = load();
  run(sb, `state._lastPaneFrameAt = Date.now() - PANE_SILENCE_MS - 1000; _paneSilenceCheck();`);
  assert.equal(JSON.stringify(sb.__sent.filter(m => m.type === 'subscribe').map(m => m.data.session_id)), '["s1"]');
  run(sb, `_paneSilenceCheck();`);
  assert.equal(sb.__sent.filter(m => m.type === 'subscribe').length, 1);
});

test('_paneSilenceCheck does nothing when fresh or off the session view', () => {
  const sb = load();
  run(sb, `state._lastPaneFrameAt = Date.now(); _paneSilenceCheck();`);
  run(sb, `state._lastPaneFrameAt = 1; state.activeView = 'list'; _paneSilenceCheck();`);
  assert.equal(sb.__sent.length, 0);
});
