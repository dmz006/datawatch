// Image-send pending indicator (app.js) — while a session input carrying
// [image:<path>] tags is waiting for the daemon to accept it, a small
// status line (the composer banner, same surface as "Uploading image…")
// shows the `image_send_pending` string right above the session input.
// The indicator is purely visual: the input box and the terminal must
// stay fully interactive, and the indicator must ALWAYS clear — on
// acceptance (websocket send, or the next 'output' frame), on websocket
// close, and after a hard 15 s client cap so it can never stick.
//
// Like the sibling tests in this directory, the REAL unmodified app.js is
// loaded into a node:test + vm sandbox with a stub browser environment.
// The stub DOM/timers are no-ops, so the behavioral tests exercise the
// indicator's state machine through expressions evaluated in the SAME
// sandbox (app.js's top-level `state` is a lexical binding, reachable via
// vm.runInContext but not as a sandbox property), capture the scheduled
// cap-timer callback for the 15 s expiry, and statically assert that the
// clear hook is attached at the three real clear points in the source.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS_PATH = path.join(__dirname, 'app.js');
const APP_JS_SRC = fs.readFileSync(APP_JS_PATH, 'utf8');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  sandbox.URL = { revokeObjectURL() {} }; // _clearAllAttachments touches it
  loadScript(APP_JS_PATH, sandbox);
  assert.equal(typeof sandbox.imageSendShowPending, 'function',
    'imageSendShowPending is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._clearImageSendPending, 'function',
    '_clearImageSendPending is not defined -- did it get renamed?');
  return sandbox;
}

// ev runs expr in app.js's realm (same context) and returns the result;
// state is app.js's top-level const, so it is reached this way.
function ev(sandbox, expr) { return vm.runInContext(expr, sandbox); }

test('cap constant is the documented 15 s client limit', () => {
  const sandbox = loadAppJS();
  assert.equal(ev(sandbox, 'IMAGE_SEND_PENDING_CAP_MS'), 15000);
});

test('showing the indicator schedules the cap and sets the pending flag', () => {
  const sandbox = loadAppJS();
  let capCb = null; let scheduledMs = null;
  sandbox.setTimeout = (fn, ms) => { capCb = fn; scheduledMs = ms; return 42; };
  sandbox.imageSendShowPending();
  assert.equal(scheduledMs, 15000, 'cap must be scheduled for 15 s');
  assert.equal(ev(sandbox, 'state.imageSendPendingTimer'), 42);
  assert.equal(ev(sandbox, 'state._imageSendPending'), true);
  assert.equal(sandbox.imageSendShowing(), true);
});

test('the 15 s cap callback clears the indicator', () => {
  const sandbox = loadAppJS();
  let capCb = null; let cleared = 0;
  sandbox.setTimeout = (fn, ms) => { capCb = fn; return 42; };
  sandbox.clearTimeout = () => { cleared++; };
  sandbox.imageSendShowPending();
  assert.ok(typeof capCb === 'function');
  capCb();
  assert.equal(ev(sandbox, 'state._imageSendPending'), false);
  assert.ok(cleared >= 1, 'stall timer is cancelled on clear');
  assert.equal(sandbox.imageSendShowing(), false);
});

test('sendSessionInput shows the pending indicator when image tags ride along', () => {
  const sandbox = loadAppJS();
  ev(sandbox, "state._pendingAttachments = [{path:'uploads/x.png',name:'x.png'}];");
  ev(sandbox, 'state.activeSession = null;'); // no active session: no ws send branch
  assert.equal(ev(sandbox, 'state._imageSendPending'), false);
  vm.runInContext('sendSessionInput()', sandbox);
  assert.equal(ev(sandbox, 'state._imageSendPending'), true,
    'indicator must be up while the tagged send is in flight');
});

test('sendSessionInput does NOT show the indicator for plain text sends', () => {
  const sandbox = loadAppJS();
  ev(sandbox, "state._pendingAttachments = [];");
  ev(sandbox, 'state.activeSession = null;');
  vm.runInContext('sendSessionInput()', sandbox);
  assert.equal(ev(sandbox, 'state._imageSendPending'), false,
    'plain text (no image tags) must not raise the indicator');
});

test('sendSessionInputDirect clears the indicator when the daemon accepts it', () => {
  const sandbox = loadAppJS();
  ev(sandbox, 'WebSocket.OPEN = 1;'); // app.js gates send() on readyState === WebSocket.OPEN
  let accepted = false;
  ev(sandbox, '__ws = { readyState: 1, send: () => { __accepted = true; } }; state.ws = __ws;');
  ev(sandbox, '__accepted = false;');
  ev(sandbox, "state.activeSession = 'sess-1';");
  ev(sandbox, "state._pendingAttachments = [{path:'uploads/y.png',name:'y.png'}];");
  vm.runInContext('sendSessionInputDirect()', sandbox);
  assert.equal(ev(sandbox, '__accepted'), true, 'the tagged text must actually be sent');
  assert.equal(ev(sandbox, 'state._imageSendPending'), false,
    'server acceptance (websocket send) must clear the indicator');
});

test('sendSessionInputDirect leaves the indicator up when the send is refused', () => {
  const sandbox = loadAppJS();
  ev(sandbox, 'state.ws = null;'); // -> outage toast path, send() returns false
  ev(sandbox, 'state._wsOutageToastShown = false;');
  ev(sandbox, "state.activeSession = 'sess-1';");
  ev(sandbox, "state._pendingAttachments = [{path:'uploads/z.png',name:'z.png'}];");
  try {
    vm.runInContext('sendSessionInputDirect()', sandbox);
  } catch (e) { /* stub DOM in the toast path -- only the flag matters here */ }
  assert.equal(ev(sandbox, 'state._imageSendPending'), true,
    'a refused send must NOT clear the indicator (close/cap will)');
});

// Wiring checks: the clear hook must live at the three real clear points
// in the app, in addition to the send-success branches above.
test('ws close and the next output frame both clear a stuck indicator', () => {
  // ws 'close' handler: a dead socket is not a delivery.
  assert.ok(
    APP_JS_SRC.includes('state.ws = null;\n    // A dead socket is not a delivery — drop any stuck pending indicator.\n    _clearImageSendPending();'),
    'ws close handler must call _clearImageSendPending()');
  // next 'output' frame for the session: the daemon accepted the input.
  assert.ok(
    APP_JS_SRC.includes("case 'output':\n      // Next output update for the session: the daemon accepted the input.\n      if (state._imageSendPending) _clearImageSendPending();"),
    "the 'output' frame handler must clear a pending indicator");
});

test('the composer banner (no blocking overlay) is the display surface', () => {
  // Same surface as "Uploading image…" / "Transcribing voice message…":
  // a status line above the composer, which never disables the input or
  // the terminal.
  assert.ok(
    APP_JS_SRC.includes("_composerBanner(t('image_send_pending') || 'Sending image…');"),
    'indicator must render through the composer banner with the new locale key');
  assert.ok(
    APP_JS_SRC.includes('if (!state._imageSendPending) return;\n  state._imageSendPending = false;\n  _composerBanner(null);'),
    'clearing must hide the composer banner again');
});
