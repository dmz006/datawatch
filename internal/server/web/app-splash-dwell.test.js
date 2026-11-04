// Launch-splash minimum-dwell (app.js splash gating, run on WebSocket open).
//
// Contract under test — the real app.js code path, loaded through the shared
// stub browser and driven with node:test's FAKE TIMERS (t.mock.timers,
// never real-time waits):
//   1. First visit, splash_min_seconds 6: splash alive at 5.9 s after the
//      connection, removed at 6 s + the 700 ms fade. Dwell is min_seconds
//      from the moment the gating allowed the splash to show (the
//      connection), NOT from page start.
//   2. Connection 4 s after page start: splash stays until 4 s + 6 s = 10 s
//      after page start — dwell is measured from when the splash is allowed
//      to show, not from 6 s after page start.
//   3. Version changed: dwell is splash_min_seconds_new_version (9), not
//      splash_min_seconds (6).
//   4. Same version, last shown >24 h ago: dwell is splash_min_seconds.
//   5. Same version, shown <24 h ago: removed immediately, no dwell at all.
//   6. splash_min_seconds 0 (legacy): 3000 ms window measured from page
//      start — elapses 2 s after a 1 s-late connection, and is 0 (immediate)
//      once the connection arrives after page start + 3000 ms.
//   7. Early dismiss — click, pointerdown and keydown each start the 700 ms
//      fade immediately instead of waiting out the dwell.
//   8. Input before the connection is up is ignored; the splash keeps its
//      full dwell once the connection lands.
//   9. After an early dismiss the pending dwell timer cannot re-fire: exactly
//      one removal, no error.
//  10. The "previous version" splash line is written only when the daemon
//      version actually changed since the last splash display.
//
// How the real code is engaged: app.js's top-level connect() builds a
// WebSocket (spied here), the fake-clock Date now() recorded into
// window._splashStart, fetch('/api/config') answered with the ui settings
// under test, then the test fires the ws 'open' handler. The dwell/fade
// timers live on the host fake clock; every timing assertion below is a
// t.mock.timers.tick() on that clock — no test ever waits on wall time.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

// Fixed fake "page start": app.js records window._splashStart = Date.now()
// at load, so with the clock enabled at this value, t=0 IS page start.
const FAKE_EPOCH = 1_700_000_000_000;
const FADE_MS = 700; // app.js _scheduleSplashDismiss fade window
const HOUR = 3_600_000;

// enableFakeTimers turns on node:test's fake Date/setTimeout/clearTimeout on
// the host realm at page start; the harness wires THE SAME clock into the vm
// sandbox, so app.js's Date.now() and its dwell timers only ever advance
// through the returned tick().
function enableFakeTimers(t) {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: FAKE_EPOCH });
  return (ms) => t.mock.timers.tick(Math.max(0, ms));
}

// makeSplashEl is the #splash element stub: remove() counts calls, the class
// list records additions, and isConnected flips off on removal — the exact
// surface app.js's _scheduleSplashDismiss reads (it guards "another path
// already took it out" on isConnected and adds the 'fade-out' class).
function makeSplashEl() {
  const classes = new Set();
  const el = {
    id: 'splash',
    style: {},
    isConnected: true,
    removed: 0,
    _classes: classes,
    addEventListener() {}, removeEventListener() {},
    remove() { el.removed++; el.isConnected = false; },
    classList: {
      add(c) { classes.add(c); },
      remove(c) { classes.delete(c); },
      toggle(c) { classes.has(c) ? classes.delete(c) : classes.add(c); },
      contains(c) { return classes.has(c); },
    },
  };
  return el;
}

// makeHarness loads the REAL app.js into a fresh stub browser with:
//   - the host fake-Timers clock wired in as sandbox Date/setTimeout/clearTimeout,
//   - a WebSocket spy whose 'open' handler the test fires on demand,
//   - fetch('/api/config') answering { ui } with the settings under test,
//   - recorded document listeners (the early-dismiss hooks) + dispatch,
//   - pre-seeded localStorage (cs_splash_time / cs_splash_version) and,
//     when given, state._daemonVersion (normally set from the WS hello).
// It settles the app's own bootstrap chain (DOMContentLoaded → initI18n →
// health probe → connect()) so the ws spy exists before the caller fires
// its 'open' handler.
async function makeHarness(opts) {
  const { ui = {}, ls = {}, daemonVersion = null } = opts;
  const splash = makeSplashEl();
  const prevEl = { textContent: '', style: {} }; // #splashVersionPrev (hidden by CSS until shown)
  const sandbox = buildSandbox({ byId: { splash, splashVersionPrev: prevEl } });

  // Same fake clock app.js sees as the test ticks (enableFakeTimers ran first).
  sandbox.Date = Date;
  sandbox.setTimeout = (fn, ms, ...rest) => setTimeout(fn, ms, ...rest);
  sandbox.clearTimeout = (id) => { clearTimeout(id); };
  // A vm context has no host JavaScript builtins; hand over the ones app.js
  // reaches for at boot (route parsing, deep links, base64).
  sandbox.URL = URL;
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.encodeURIComponent = encodeURIComponent;
  sandbox.decodeURIComponent = decodeURIComponent;
  sandbox.btoa = (s) => Buffer.from(s, 'binary').toString('base64');
  sandbox.atob = (s) => Buffer.from(s, 'base64').toString('binary');
  sandbox.structuredClone = structuredClone;

  sandbox.fetch = (url) => {
    const u = String(url);
    if (u.includes('/api/config')) {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ui }) });
    }
    if (u.includes('/locales/')) {
      // The app's real English strings for the keys the splash uses, so
      // t() renders like the browser (initI18n settles on this bundle).
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({
        splash_version_previous: 'previous %1$s',
        splash_status_connected: 'Connected',
        status_connected: 'Connected',
      }) });
    }
    return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', json: () => Promise.resolve(null), text: () => Promise.resolve('not found') });
  };

  const sockets = [];
  const WS = function (url) {
    const handlers = {};
    const sock = {
      url, readyState: 1, sent: [], handlers,
      send(p) { sock.sent.push(p); },
      close() {},
      addEventListener(type, fn) { (handlers[type] || (handlers[type] = [])).push(fn); },
      removeEventListener() {},
    };
    sockets.push(sock);
    return sock;
  };
  // Browser constants: CONNECTING=0 OPEN=1 CLOSING=2 CLOSED=3; the spy
  // socket starts OPEN since the test decides when 'open' fires.
  WS.CONNECTING = 0; WS.OPEN = 1; WS.CLOSING = 2; WS.CLOSED = 3;
  sandbox.WebSocket = WS;

  // document listeners (pointerdown/click/keydown early-dismiss hooks)
  // recorded so tests can fire exactly one of them — like a real tap/click/key.
  const docListeners = new Map();
  sandbox.document.addEventListener = (type, fn) => {
    if (!docListeners.has(type)) docListeners.set(type, []);
    docListeners.get(type).push(fn);
  };
  sandbox.document.removeEventListener = (type, fn) => {
    const arr = docListeners.get(type);
    if (!arr) return;
    const i = arr.indexOf(fn);
    if (i !== -1) arr.splice(i, 1);
  };

  for (const [k, v] of Object.entries(ls)) sandbox.localStorage.setItem(k, String(v));

  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);

  // app.js parks its whole bootstrap (i18n → health probe → top-level
  // connect()) behind a DOMContentLoaded listener; in the stub world the
  // document's addEventListener was ours, so fire the captured callbacks
  // now — exactly what the browser does after the initial parse.
  const domReady = docListeners.get('DOMContentLoaded') || [];
  for (const fn of domReady.splice(0)) fn();

  // The app's real English strings for the two splash keys (t() would
  // otherwise fall back to the bare key names).
  sandbox._i18n.bundle.splash_version_previous = 'previous %1$s';
  sandbox._i18n.bundle.splash_status_connected = 'Connected';

  if (daemonVersion) vm.runInContext(`state._daemonVersion = ${JSON.stringify(daemonVersion)}`, sandbox);

  // app.js bootstraps connect() asynchronously (initI18n().finally(() =>
  // ... fetch('/api/health').then(... → connect()))); settle that chain now,
  // while the fake clock is still at page start, so the only timers that
  // ever fire afterwards are the ones the test itself lets run.
  await flushAsync();

  return {
    sandbox, splash, prevEl,
    // open() fires the app's ws 'open' handler on the live socket (app.js's
    // top-level connect() created it); the fetch decision chain settles in
    // the microtask queue, so callers await flushAsync() right after.
    open() {
      const sock = sockets[sockets.length - 1];
      assert.ok(sock && (sock.handlers.open?.length || 0) > 0, 'app.js should have attached an open handler to the ws spy');
      (sock.handlers.open || []).forEach((f) => f());
      return sock;
    },
    // dispatch fires one registered document listener chain, as the browser
    // would for pointerdown / click / keydown.
    dispatch(type) { (docListeners.get(type) || []).slice().forEach((fn) => fn({ type })); },
    hasDocListener: (type) => (docListeners.get(type) || []).length > 0,
  };
}

// ── case 1: first visit, minimum dwell ─────────────────────────────────────

test('first visit, splash_min_seconds 6: splash alive at 5.9 s after connection, gone at 6 s + 700 ms fade', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } }); // first visit: no cs_splash_time
  assert.equal(h.splash.isConnected, true, 'splash is mounted before the connection');
  assert.equal(vm.runInContext('window._splashStart', h.sandbox), FAKE_EPOCH, 'page start is recorded on the fake clock');

  h.open();
  await flushAsync();

  tick(5900); // 5.9 s after the connection
  assert.equal(h.splash.isConnected, true, 'splash must still be up at 5.9 s after the connection');
  assert.equal(h.splash.removed, 0);

  tick(100); // dwell end at exactly 6 s — fade starts, removal still pending
  assert.equal(h.splash.isConnected, true, 'removal waits for the 700 ms fade');
  assert.ok(h.splash.classList.contains('fade-out'), 'fade-out class is added when the dwell elapses');

  tick(699); // 6.699 s — still inside the fade window
  assert.equal(h.splash.isConnected, true);

  tick(2); // 6.701 s ≈ 6 s dwell + 700 ms fade
  assert.equal(h.splash.removed, 1, 'splash removed exactly once');
  assert.equal(h.splash.isConnected, false);

  assert.equal(h.sandbox.localStorage.getItem('cs_splash_time'), String(FAKE_EPOCH), 'gating records the display time');
});

// ── case 2: dwell measured from the connection, not page start ─────────────

test('connection 4 s after page start, splash_min_seconds 6: splash stays until 10 s after page start, not 6 s', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  tick(4000); // daemon comes up 4 s after page start
  h.open();
  await flushAsync();

  tick(3900); // 3.9 s after connection = 7.9 s after page start
  assert.equal(h.splash.isConnected, true, 'past 6 s after PAGE START the splash must survive — dwell counts from the connection (6+4=10 s from page start)');
  tick(2000); // 5.9 s after connection
  assert.equal(h.splash.isConnected, true, 'still up at 5.9 s after the connection');

  tick(100); // dwell end at 6 s after connection
  assert.equal(h.splash.isConnected, true);
  assert.ok(h.splash.classList.contains('fade-out'));
  tick(700); // 6.7 s after connection = 10.7 s after page start
  assert.equal(h.splash.removed, 1, 'splash stays until 6 s + fade AFTER the connection — i.e. 10 s (dwell) after page start, never 6 s');
});

// ── case 3: version change uses splash_min_seconds_new_version ─────────────

test('version changed, splash_min_seconds_new_version 9: dwell is 9 s, not the 6 s ordinary minimum', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({
    ui: { splash_min_seconds: 6, splash_min_seconds_new_version: 9 },
    ls: { cs_splash_time: FAKE_EPOCH - HOUR, cs_splash_version: 'v1.0.0-alpha.1' },
    daemonVersion: 'v1.0.0-alpha.10',
  });
  h.open();
  await flushAsync();

  tick(6700); // 6.7 s — the ordinary 6 s minimum + fade would already be gone
  assert.equal(h.splash.isConnected, true, 'the 6 s splash_min_seconds cannot fire on a version change');

  tick(2200); // 8.9 s after the connection
  assert.equal(h.splash.isConnected, true, 'still up at 8.9 s — the 9 s new-version dwell applies');

  tick(100); // 9 s: dwell elapses, fade starts
  assert.ok(h.splash.classList.contains('fade-out'));
  tick(700); // 9.7 s
  assert.equal(h.splash.removed, 1, 'removed at 9 s (new-version minimum) + 700 ms fade');
});

// ── case 4: same version, last shown >24 h ago ──────────────────────────────

test('same version, last shown >24 h ago: dwell is splash_min_seconds (5 s), not the new-version 9 s', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({
    // Different values in the two settings so the assertion can tell them apart.
    ui: { splash_min_seconds: 5, splash_min_seconds_new_version: 9 },
    ls: { cs_splash_time: FAKE_EPOCH - 25 * HOUR, cs_splash_version: 'v1.0.0-alpha.5' },
    daemonVersion: 'v1.0.0-alpha.5', // unchanged → hoursSince >= 24 is the only show rule
  });
  h.open();
  await flushAsync();

  tick(4900); // 4.9 s after connection — still inside the 5 s dwell
  assert.equal(h.splash.isConnected, true);
  assert.equal(h.splash.classList.contains('fade-out'), false);
  tick(100); // 5.0 s: dwell elapses, fade starts
  assert.equal(h.splash.isConnected, true);
  assert.ok(h.splash.classList.contains('fade-out'), 'same-version re-show dwells the plain splash_min_seconds (5 s), not the 9 s new-version rule (9 s would still be up here)');
  tick(699); // 5.699 s — fade still in flight
  assert.equal(h.splash.isConnected, true);
  tick(2); // 5.701 s ≈ 5 s dwell + 700 ms fade
  assert.equal(h.splash.removed, 1, 'a same-version re-show (>24 h) dwells the plain splash_min_seconds');
});

// ── case 5: same version, shown <24 h ago → immediate removal ──────────────

test('same version, shown <24 h ago: splash removed immediately, no dwell, no fade', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({
    ui: { splash_min_seconds: 6 },
    ls: { cs_splash_time: FAKE_EPOCH - HOUR, cs_splash_version: 'v1.0.0-alpha.5' },
    daemonVersion: 'v1.0.0-alpha.5',
  });
  h.open();
  await flushAsync();
  assert.equal(h.splash.removed, 1, 'gating says no splash: it is taken down as the decision settles');
  assert.equal(h.splash.classList.contains('fade-out'), false, 'the skip path removes without the fade (rule unchanged)');
  tick(15000); // well past any dwell the settings might imply
  assert.equal(h.splash.removed, 1, 'nothing pending behind the scenes: no second removal');
});

// ── case 6: splash_min_seconds 0 keeps the legacy 3000 ms page-start budget ─

test('splash_min_seconds 0: splash goes at 3000 ms after PAGE START (here 1 s after a late connection)', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 0 } }); // first visit, no new version
  tick(1000); // connection arrives 1 s after page start
  h.open();
  await flushAsync();

  tick(1900); // 1.9 s into the remaining 2 s of the legacy budget (3000 ms from page start, 1 s already elapsed)
  assert.equal(h.splash.isConnected, true, 'still up at 1.9 s into the remaining 2 s of legacy budget');
  assert.equal(h.splash.classList.contains('fade-out'), false);
  tick(100); // 2 s remaining budget elapsed = exactly 3000 ms after page start
  assert.equal(h.splash.isConnected, true, 'removal still waits for the fade');
  assert.ok(h.splash.classList.contains('fade-out'), 'legacy budget ends at 3000 ms after page start, not 3000 ms after the connection');
  tick(700); // 3.7 s after page start
  assert.equal(h.splash.removed, 1);
});

test('splash_min_seconds 0: connection arriving after 3000 ms since page start removes the splash on the spot', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 0 } });
  tick(4500); // the legacy 3 s page-start budget is long gone
  h.open();
  await flushAsync();
  assert.equal(h.splash.isConnected, true, 'dwell remaining is 0 — the splash is dismissed on the spot');
  tick(1); // the zero-delay dwell fires at once: fade starts immediately
  assert.equal(h.splash.isConnected, true, '…but the 700 ms fade still runs');
  assert.ok(h.splash.classList.contains('fade-out'), 'dwell of 0 fires immediately — fade starts at once');
  tick(700);
  assert.equal(h.splash.removed, 1, 'immediate (modulo fade), never a fresh 3 s or 6 s window');
});

// ── cases 7–8: early dismiss via click / pointerdown / keydown ─────────────

test('early dismiss: click 2 s after the connection starts the fade at once', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  h.open();
  await flushAsync();
  tick(2000);
  assert.equal(h.splash.isConnected, true);
  assert.ok(h.hasDocListener('click'), 'the connected path armed the click early-dismiss hook');

  h.dispatch('click');
  assert.equal(h.splash.isConnected, true, 'the fade is in flight, removal still pending');
  assert.ok(h.splash.classList.contains('fade-out'), 'click starts the 700 ms fade immediately, without waiting for the 6 s dwell');

  tick(699);
  assert.equal(h.splash.isConnected, true);
  tick(2);
  assert.equal(h.splash.removed, 1, 'removed at 2 s + 700 ms, i.e. the click, not the dwell, ended it');
});

test('early dismiss: pointerdown behaves exactly like click', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  h.open();
  await flushAsync();
  tick(1000);
  h.dispatch('pointerdown');
  assert.ok(h.splash.classList.contains('fade-out'), 'pointerdown starts the fade at once');
  assert.equal(h.splash.isConnected, true);
  tick(700);
  assert.equal(h.splash.removed, 1);
});

test('early dismiss: keydown behaves exactly like click', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  h.open();
  await flushAsync();
  tick(3000);
  h.dispatch('keydown');
  assert.ok(h.splash.classList.contains('fade-out'), 'keydown starts the fade at once');
  assert.equal(h.splash.isConnected, true);
  tick(700);
  assert.equal(h.splash.removed, 1);
});

// ── case 9: input before the connection is up is ignored ───────────────────

test('a tap before the connection is up is ignored and the splash keeps its full dwell', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  h.dispatch('pointerdown'); // pre-connection tap — no hooks armed yet
  h.dispatch('click');
  h.dispatch('keydown');
  assert.equal(h.splash.removed, 0);
  assert.equal(h.splash.classList.contains('fade-out'), false, 'no early-dismiss hook exists before the connection is up');
  assert.ok(!h.hasDocListener('click'), 'nothing armed yet');

  tick(8000); // connection arrives late
  h.open();
  await flushAsync();
  tick(5900); // 5.9 s after the connection — the pre-connection taps did not shorten or end the dwell
  assert.equal(h.splash.isConnected, true, 'splash keeps its full minimum dwell from when the connection allowed it to show');
  tick(100);
  assert.ok(h.splash.classList.contains('fade-out'));
  tick(700);
  assert.equal(h.splash.removed, 1);
});

// ── case 10: no double removal after an early dismiss ──────────────────────

test('after an early dismiss the pending dwell timer cannot fire a second removal', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({ ui: { splash_min_seconds: 6 } });
  h.open();
  await flushAsync();
  tick(2000);
  h.dispatch('click'); // begins the fade; the 6 s dwell timer is now superseded
  tick(700);
  assert.equal(h.splash.removed, 1, 'the dismiss removal happened exactly once');
  assert.equal(h.splash._splashDismiss, null, 'the dismiss record was cleared by the fade completion');

  tick(10000); // far past where the (cleared) 6 s dwell timer would have fired
  assert.equal(h.splash.removed, 1, 'no double removal: the dwell timer fired no second exit');
  assert.doesNotThrow(() => { h.dispatch('click'); }, 'late input after removal is a no-op, not an error');
  assert.equal(h.splash.removed, 1);
});

// ── case 11: the previous-version line ─────────────────────────────────────

test('version line: shown (with the old version) when the version changed', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({
    ui: { splash_min_seconds_new_version: 9 },
    ls: { cs_splash_time: FAKE_EPOCH - HOUR, cs_splash_version: 'v1.0.0-alpha.1' },
    daemonVersion: 'v1.0.0-alpha.10',
  });
  h.open();
  await flushAsync();
  assert.equal(h.prevEl.textContent, 'previous v1.0.0-alpha.1', 'the previous-version line names the version the splash last showed for');
  assert.equal(h.prevEl.style.display, '', 'the line is shown on a version change');
  assert.equal(h.sandbox.localStorage.getItem('cs_splash_version'), 'v1.0.0-alpha.10', 'the newly shown version is recorded');
});

test('version line: left untouched when the version did not change', async (t) => {
  const tick = enableFakeTimers(t);
  const h = await makeHarness({
    ui: { splash_min_seconds: 6 },
    ls: { cs_splash_time: FAKE_EPOCH - 25 * HOUR, cs_splash_version: 'v1.0.0-alpha.5' },
    daemonVersion: 'v1.0.0-alpha.5', // same version — plain >24 h re-show
  });
  h.open();
  await flushAsync();
  assert.equal(h.splash.isConnected, true, 'the splash itself still shows on a >24 h re-show');
  assert.equal(h.prevEl.textContent, '', 'no "previous version" text on a same-version re-show');
  assert.equal(h.prevEl.style.display, undefined, 'the line stays CSS-hidden — never revealed');
});
