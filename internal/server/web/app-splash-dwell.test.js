// Splash-dwell timing: the real app.js splash-removal paths — first-visit
// dwell, version-change dwell, the legacy 3000 ms window kept when
// splash_min_seconds is 0, immediate removal when gating says skip, and the
// early-dismiss input handlers — driven with node:test mock.timers. Every
// timing assertion below advances a fake clock; none of these tests wait on
// real time.
//
// The real app.js runs inside the stubbed browser environment from
// testutil_browser_stub.js with fake-clock-driven timers. The WebSocket is a
// recording stub, so each test emits its 'open' event on its own schedule —
// that open is what runs the splash-gating code in app.js.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const T0 = 1_700_000_000_000; // shared fake epoch (ms); all times below are on this clock
const HOUR = 3_600_000;

// A stand-in for the #splash element that records what the removal paths do:
// classList (the 700 ms fade gets added before removal), removedCount (the
// "removed exactly once" guarantee) and isConnected, which app.js checks
// before removing a second time.
function makeSplash() {
  const classes = new Set();
  return {
    isConnected: true,
    style: {},
    dataset: {},
    removedCount: 0,
    classList: {
      add: (c) => { classes.add(c); },
      remove: (c) => { classes.delete(c); },
      contains: (c) => classes.has(c),
      toggle: (c) => (classes.has(c) ? (classes.delete(c), false) : (classes.add(c), true)),
    },
    remove() { this.removedCount += 1; this.isConnected = false; },
  };
}

// A stand-in for #splashVersionPrev that records what the version line gets.
function makeTracker() {
  const el = { text: '', style: {} };
  Object.defineProperty(el, 'textContent', {
    get() { return el.text; },
    set(v) { el.text = String(v); },
  });
  return el;
}

const fadeAdded = (splash) => splash.classList.contains('fade-out');

// Boot the real app.js in a sandbox and call connect(); return the socket so
// the test can emit 'open' at any fake time it likes.
//   ui            — the ui object served by GET /api/config
//   storage       — localStorage preseed (cs_splash_time / cs_splash_version)
//   daemonVersion — state._daemonVersion, as the sessions WS would report it
//   splash / prev — element stand-ins for #splash / #splashVersionPrev
function setup(t, { ui, storage = {}, daemonVersion, splash = makeSplash(), prev = null } = {}) {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: T0 });
  t.after(() => { t.mock.timers.reset(); });

  const byId = { splash };
  if (prev) byId.splashVersionPrev = prev;
  const sandbox = buildSandbox({ byId });
  for (const [k, v] of Object.entries(storage)) sandbox.localStorage.setItem(k, v);

  // buildSandbox ships no-op timers; these tests want the app to schedule
  // on the fake clock so mock.timers.tick can decide when each callback fires.
  sandbox.setTimeout = (fn, ms, ...rest) => setTimeout(fn, ms, ...rest);
  sandbox.clearTimeout = (id) => clearTimeout(id);
  sandbox.Date = Date;

  const sockets = [];
  sandbox.WebSocket = function () {
    const listeners = {};
    const ws = {
      readyState: 0,
      close() { this.readyState = 3; },
      send() {},
      addEventListener(evt, fn) { (listeners[evt] = listeners[evt] || []).push(fn); },
      emit(evt, payload) { (listeners[evt] || []).forEach(fn => fn(payload || {})); },
    };
    sockets.push(ws);
    return ws;
  };
  sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CLOSING = 2;
  sandbox.WebSocket.CLOSED = 3;
  sandbox._sockets = sockets;

  // The early-dismiss paths attach to document; record the real listeners so
  // tests can deliver exactly one click / pointerdown / keydown.
  const docListeners = {};
  sandbox.document.addEventListener = (evt, fn) => { (docListeners[evt] = docListeners[evt] || []).push(fn); };
  sandbox.document.removeEventListener = (evt, fn) => {
    const list = docListeners[evt];
    if (!list) return;
    const i = list.indexOf(fn);
    if (i >= 0) list.splice(i, 1);
  };
  sandbox.__docListeners = docListeners;
  sandbox.__fireDoc = (evt) => (docListeners[evt] || []).slice().forEach(fn => fn());

  sandbox.fetch = (url) => {
    if (url === '/api/config') {
      const body = { ui: ui || {} };
      return Promise.resolve({
        ok: true, status: 200, statusText: 'OK',
        json: () => Promise.resolve(body),
        text: () => Promise.resolve(JSON.stringify(body)),
      });
    }
    return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
  };

  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Real en bundle so t() substitutes %1$s placeholders in the version line.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  if (daemonVersion !== undefined) {
    vm.runInContext('state._daemonVersion = ' + JSON.stringify(daemonVersion) + ';', sandbox);
  }
  vm.runInContext('connect();', sandbox);
  return { sandbox, splash, ws: sandbox._sockets[0] };
}

function open(res) {
  res.ws.readyState = 1;
  res.ws.emit('open');
}

test('case 1: first visit dwells splash_min_seconds (6) after connect, then the 700 ms fade', async (t) => {
  const { splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  open({ ws });
  await flushAsync(); // gating settled at t=0
  t.mock.timers.tick(5900);
  assert.equal(splash.isConnected, true, 'still up 5.9 s after connect');
  assert.equal(fadeAdded(splash), false, 'fade not started while dwelling');
  t.mock.timers.tick(100); // t=6000
  assert.equal(fadeAdded(splash), true, 'fade starts exactly 6 s after connect');
  assert.equal(splash.isConnected, true, 'still on screen during the fade');
  t.mock.timers.tick(699);
  assert.equal(splash.isConnected, true, 'not removed a hair before the fade ends');
  t.mock.timers.tick(1); // t=6700
  assert.equal(splash.removedCount, 1, 'removed once: 6 s dwell + 700 ms fade');
  assert.equal(splash.isConnected, false);
});

test('case 2: dwell runs from the connect that allowed the splash, not from page start', async (t) => {
  const { splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  t.mock.timers.tick(4000); // the connection arrives 4 s after the page began
  open({ ws });
  await flushAsync(); // gating settles at t=4000, so dwell ends at t=10000
  t.mock.timers.tick(5900); // t=9900: 5.9 s after the connect, 9.9 s after page start
  assert.equal(splash.isConnected, true, 'still up — the pre-connect 4 s counts as dwell time');
  assert.equal(fadeAdded(splash), false);
  t.mock.timers.tick(100); // t=10000: exactly 6 s after the connect
  assert.equal(fadeAdded(splash), true, 'dwell ends 6 s after the connect, not 6 s after page start');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(700); // t=10700
  assert.equal(splash.removedCount, 1);
});

test('case 3: version change dwells splash_min_seconds_new_version (9), not the base setting', async (t) => {
  const { splash, ws } = setup(t, {
    ui: { splash_min_seconds: 3, splash_min_seconds_new_version: 9 },
    storage: { cs_splash_time: String(T0 - 1 * HOUR), cs_splash_version: '1.0.0' },
    daemonVersion: '9.9.9', // changed since the last splash
  });
  open({ ws });
  await flushAsync();
  assert.equal(splash.isConnected, true, 'version change always shows the splash');
  t.mock.timers.tick(8900);
  assert.equal(splash.isConnected, true, 'still up at 8.9 s — dwelling 9 s, not the base 3 s');
  assert.equal(fadeAdded(splash), false);
  t.mock.timers.tick(100); // t=9000
  assert.equal(fadeAdded(splash), true, 'fade starts at 9 s (splash_min_seconds_new_version)');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(700); // t=9700
  assert.equal(splash.removedCount, 1);
});

test('case 4: same version but last shown more than 24 h ago dwells splash_min_seconds (8)', async (t) => {
  const { sandbox, splash, ws } = setup(t, {
    ui: { splash_min_seconds: 8 },
    storage: { cs_splash_time: String(T0 - 25 * HOUR), cs_splash_version: '1.0.0' },
    daemonVersion: '1.0.0',
  });
  open({ ws });
  await flushAsync();
  assert.equal(sandbox.localStorage.getItem('cs_splash_time'), String(T0), 'a shown splash re-records when it was last shown');
  t.mock.timers.tick(7900);
  assert.equal(splash.isConnected, true);
  assert.equal(fadeAdded(splash), false);
  t.mock.timers.tick(100); // t=8000
  assert.equal(fadeAdded(splash), true, 'fade starts 8 s in (splash_min_seconds)');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(700); // t=8700
  assert.equal(splash.removedCount, 1);
});

test('case 5: same version shown less than 24 h ago — no dwell, removed immediately', async (t) => {
  const { sandbox, splash, ws } = setup(t, {
    ui: { splash_min_seconds: 8 },
    storage: { cs_splash_time: String(T0 - 2 * HOUR), cs_splash_version: '1.0.0' },
    daemonVersion: '1.0.0',
  });
  open({ ws });
  await flushAsync();
  assert.equal(splash.removedCount, 1, 'removed as soon as gating settles — no dwell');
  assert.equal(fadeAdded(splash), false, 'the immediate-skip path does not fade');
  t.mock.timers.tick(10000);
  assert.equal(splash.removedCount, 1, 'and nothing else is scheduled to remove it again');
  assert.equal(sandbox.localStorage.getItem('cs_splash_time'), String(T0 - 2 * HOUR),
    'a skipped splash does not re-record the shown time');
});

test('case 6: splash_min_seconds 0 keeps the legacy 3000 ms window measured from page start', async (t) => {
  // 6a — connect before the window elapses: dwell ends 3000 ms after page
  // start (here, 2000 ms after the connect), not a fixed delay after connect.
  {
    const { splash, ws } = setup(t, { ui: { splash_min_seconds: 0 } });
    t.mock.timers.tick(1000);
    open({ ws });
    await flushAsync(); // t=1000: 2000 ms of the legacy window remain
    t.mock.timers.tick(1900); // t=2900: 1.9 s after the connect
    assert.equal(splash.isConnected, true, 'still up at 1.9 s after connect');
    assert.equal(fadeAdded(splash), false);
    t.mock.timers.tick(100); // t=3000: the page-start-anchored window ends
    assert.equal(fadeAdded(splash), true, 'fade starts at 3000 ms after page start');
    assert.equal(splash.isConnected, true);
    t.mock.timers.tick(700); // t=3700
    assert.equal(splash.removedCount, 1);
  }
  // 6b — connect after the window already elapsed: 0 ms remain, fade starts
  // the moment gating settles.
  {
    const { splash, ws } = setup(t, { ui: { splash_min_seconds: 0 } });
    t.mock.timers.tick(3500);
    open({ ws });
    await flushAsync(); // nothing left of the legacy window
    t.mock.timers.tick(1);
    assert.equal(fadeAdded(splash), true, 'fade begins immediately — no remaining dwell');
    assert.equal(splash.isConnected, true);
    t.mock.timers.tick(699);
    assert.equal(splash.isConnected, true, 'not a hair before the fade ends');
    t.mock.timers.tick(1); // t=4201
    assert.equal(splash.removedCount, 1);
  }
});

test('case 7: a click 2 s after connect starts the fade immediately', async (t) => {
  const { sandbox, splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  open({ ws });
  await flushAsync();
  assert.ok(sandbox.__docListeners['click'].length >= 1, 'early-dismiss click listener is armed');
  t.mock.timers.tick(2000);
  sandbox.__fireDoc('click');
  assert.equal(fadeAdded(splash), true, 'fade starts at the click, not 6 s after connect');
  assert.equal(splash.isConnected, true, 'still on screen during the fade');
  t.mock.timers.tick(600); // t=2600
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(99);
  assert.equal(splash.isConnected, true, 'not a hair before the fade ends');
  t.mock.timers.tick(1); // t=2700
  assert.equal(splash.removedCount, 1);
});

test('case 8: a pointerdown 1.5 s after connect starts the fade immediately', async (t) => {
  const { sandbox, splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  open({ ws });
  await flushAsync();
  assert.ok(sandbox.__docListeners['pointerdown'].length >= 1, 'early-dismiss pointerdown listener is armed');
  t.mock.timers.tick(1500);
  sandbox.__fireDoc('pointerdown');
  assert.equal(fadeAdded(splash), true, 'fade starts at the pointerdown');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(600); // t=2100
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(99);
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(1); // t=2200
  assert.equal(splash.removedCount, 1);
});

test('case 9: a keydown 1.2 s after connect starts the fade immediately', async (t) => {
  const { sandbox, splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  open({ ws });
  await flushAsync();
  assert.ok(sandbox.__docListeners['keydown'].length >= 1, 'early-dismiss keydown listener is armed');
  t.mock.timers.tick(1200);
  sandbox.__fireDoc('keydown');
  assert.equal(fadeAdded(splash), true, 'fade starts at the keydown');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(600); // t=1800
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(99);
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(1); // t=1900
  assert.equal(splash.removedCount, 1);
});

test('case 10: input before the connection is up is ignored — the splash dwells normally after', async (t) => {
  const { sandbox, splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  t.mock.timers.tick(1500);
  assert.equal((sandbox.__docListeners['click'] || []).length, 0,
    'not connected yet, so no early-dismiss listeners are armed — nothing to dismiss');
  sandbox.__fireDoc('click');
  sandbox.__fireDoc('pointerdown');
  t.mock.timers.tick(500); // t=2000: the connection arrives
  open({ ws });
  await flushAsync();
  t.mock.timers.tick(5900); // t=7900: 5.9 s after the connect
  assert.equal(splash.isConnected, true, 'the pre-connect taps did not dismiss it');
  assert.equal(fadeAdded(splash), false);
  t.mock.timers.tick(100); // t=8000
  assert.equal(fadeAdded(splash), true, 'dwell still ran in full from the connect');
  assert.equal(splash.isConnected, true);
  t.mock.timers.tick(700); // t=8700
  assert.equal(splash.removedCount, 1);
});

test('case 11: after an early dismiss the pending dwell timer never removes twice', async (t) => {
  const { sandbox, splash, ws } = setup(t, { ui: { splash_min_seconds: 6 } });
  open({ ws });
  await flushAsync();
  t.mock.timers.tick(2000);
  sandbox.__fireDoc('click');
  assert.equal(fadeAdded(splash), true);
  sandbox.__fireDoc('click');
  assert.equal(splash.removedCount, 0, 'the click fades; it does not remove instantly');
  t.mock.timers.tick(700); // t=2700: the fade ends — the one, normal removal
  assert.equal(splash.removedCount, 1);
  assert.equal(splash._splashDismiss, null, 'dismiss state cleared so no later path re-enters');
  t.mock.timers.tick(5000); // t=7700: well past the original 6 s dwell + fade
  assert.equal(splash.removedCount, 1, 'no second removal when the dwell timer finally expires');
});

test('case 12: the previous-version line is filled when the version changed', async (t) => {
  const prev = makeTracker();
  const { splash, ws } = setup(t, {
    storage: { cs_splash_time: String(T0 - 1 * HOUR), cs_splash_version: '1.0.0' },
    daemonVersion: '9.9.9',
    splash: makeSplash(),
    prev,
  });
  open({ ws });
  await flushAsync();
  assert.equal(splash.isConnected, true, 'version change shows the splash');
  assert.equal(prev.text, 'previous 1.0.0', 't() substitutes %1$s from the en bundle');
  assert.equal(prev.style.display, '', 'and the line is un-hidden');
});

test('case 13: the previous-version line stays hidden on a plain re-display', async (t) => {
  const prev = makeTracker();
  const { splash, ws } = setup(t, {
    ui: { splash_min_seconds: 6 },
    storage: { cs_splash_time: String(T0 - 25 * HOUR), cs_splash_version: '1.0.0' },
    daemonVersion: '1.0.0',
    splash: makeSplash(),
    prev,
  });
  open({ ws });
  await flushAsync();
  assert.equal(splash.isConnected, true, 'same version after 24 h still shows the splash');
  assert.equal(prev.text, '', 'no version line without a version change');
  assert.ok(!('display' in prev.style), 'and the line is never un-hidden');
});
