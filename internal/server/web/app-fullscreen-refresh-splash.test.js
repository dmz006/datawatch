// bl434 — shared full-page refresh treatment (mountFullPageRefreshSplash /
// dismissFullPageRefreshSplash / performFullPageRefresh in app.js).
//
// Contract under test:
//   1. mountFullPageRefreshSplash({eyeBolt, text}) mounts a fixed
//      full-viewport #fullPageRefreshSplash overlay with a <canvas> and
//      starts the EXISTING window.DWSplashArt primitive: startSessionLoading
//      (eye + bolt) when eyeBolt is true, startEyeOnly otherwise; records
//      state._fullPageRefreshSplashMountedAt + stop handle + overlay ref.
//   2. dismissFullPageRefreshSplash(minDwellMs) honors min-dwell exactly
//      like _dismissTermLoadingSplashWithMinDwell (remove immediately when
//      the dwell has already elapsed; defer via setTimeout otherwise),
//      then calls the stored stop() and removes the overlay; idempotent.
//   3. performFullPageRefresh performs the navigation (opts.reload(), else
//      location.reload()) only AFTER the min-dwell has elapsed and the
//      splash has been dismissed.
//   4. Reduced-motion regression: splash-art.js's loop() draws exactly ONE
//      static frame (t=0) and never schedules a second rAF when
//      prefers-reduced-motion is set (contrast: with motion allowed it does
//      schedule the next frame). Follows splash-art.test.js conventions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const SPLASH_ART_JS = path.join(__dirname, 'splash-art.js');

// Spy DWSplashArt: records which primitive was started (with which canvas)
// and which stop() functions came back, so tests can assert both the exact
// primitive choice and that dismiss actually stopped the animation.
function makeDWSplashArt() {
  const spy = { calls: [], stopped: [] };
  const primitive = (name) => (canvas) => {
    spy.calls.push({ fn: name, canvas });
    return () => { spy.stopped.push(name); };
  };
  return { spy, art: { startScene: primitive('startScene'), startEyeOnly: primitive('startEyeOnly'), startSessionLoading: primitive('startSessionLoading') } };
}

// Load the real app.js with a DWSplashArt spy + test-controlled location.
// byId pre-registers #fullPageRefreshSplash + the canvas inside it (mount()
// looks the canvas up by id after appending, dismiss() looks the overlay
// up by id) so tests can spy their remove() and compare identity.
function loadWithSpy() {
  const canvas = makeStubElement();
  const overlay = makeStubElement();
  const sandbox = buildSandbox({ byId: { fullPageRefreshSplash: overlay, fullPageRefreshSplashCanvas: canvas } });
  // Make appended nodes visible to getElementById (real-DOM behavior the
  // stub harness doesn't simulate by default), so the mounted overlay is
  // the exact node mount/perform create — assertions about remove() and
  // identity then hold the same way they would in a browser.
  const origGetById = sandbox.document.getElementById.bind(sandbox.document);
  const live = {};
  sandbox.document.body.appendChild = (el) => { live[el.id] = el; };
  sandbox.document.getElementById = (id) => live[id] || origGetById(id);
  const { spy, art } = makeDWSplashArt();
  sandbox.DWSplashArt = art;
  sandbox.matchMedia = (q) => ({ matches: q === '(prefers-reduced-motion: reduce)', addEventListener() {}, addListener() {} });
  sandbox.location = { hash: '', href: 'http://localhost/', pathname: '/', reloaded: false, reload() { this.reloaded = true; } };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // app.js's boot code startScenes at load time when #splashCanvas exists —
  // drop that boot record so tests see only their own mount/perform calls.
  spy.calls.length = 0;
  spy.stopped.length = 0;
  return { sandbox, canvas, overlay, spy };
}

function setState(sandbox, patch) {
  sandbox.__patch = patch;
  vm.runInContext('Object.assign(state, __patch)', sandbox);
  delete sandbox.__patch;
}

function run(sandbox, code) {
  return vm.runInContext(code, sandbox);
}

// ── contract names ─────────────────────────────────────────────────────────

test('the three contract names are defined and reachable from the sandbox', () => {
  const { sandbox } = loadWithSpy();
  for (const name of ['mountFullPageRefreshSplash', 'dismissFullPageRefreshSplash', 'performFullPageRefresh']) {
    assert.equal(run(sandbox, `typeof ${name}`), 'function', `${name} should be a top-level function`);
  }
});

// ── (a) mount: overlay + canvas + exact DWSplashArt primitive ─────────────

test('mountFullPageRefreshSplash mounts the overlay + canvas and records state', () => {
  const { sandbox } = loadWithSpy();
  sandbox.__opts = { eyeBolt: false };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  assert.equal(run(sandbox, 'state._fullPageRefreshSplashOverlay && state._fullPageRefreshSplashOverlay.id'), 'fullPageRefreshSplash', 'overlay should be mounted with the fullPageRefreshSplash id');
  assert.match(String(run(sandbox, 'state._fullPageRefreshSplashOverlay.innerHTML')), /fullPageRefreshSplashCanvas/, 'overlay should contain a <canvas> named for the lookup id');
  const mountedAt = run(sandbox, 'state._fullPageRefreshSplashMountedAt');
  assert.ok(typeof mountedAt === 'number' && Date.now() - mountedAt < 10000, 'state._fullPageRefreshSplashMountedAt should be recorded at mount');
});

test('mountFullPageRefreshSplash({eyeBolt:false}) starts startEyeOnly', () => {
  const { sandbox, spy } = loadWithSpy();
  sandbox.__opts = { eyeBolt: false };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  assert.equal(spy.calls.length, 1);
  assert.equal(spy.calls[0].fn, 'startEyeOnly', 'eyeBolt:false must start the plain animated eye primitive');
});

test('mountFullPageRefreshSplash({eyeBolt:true}) starts startSessionLoading', () => {
  const { sandbox, spy, canvas } = loadWithSpy();
  sandbox.__opts = { eyeBolt: true };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  assert.equal(spy.calls.length, 1);
  assert.equal(spy.calls[0].fn, 'startSessionLoading', 'eyeBolt:true must start the eye+bolt primitive');
  assert.equal(spy.calls[0].canvas, canvas, 'the primitive should have been started on the overlay\u2019s canvas');
});

// ── (b) dismiss: min-dwell semantics + idempotence ─────────────────────────

test('dismissFullPageRefreshSplash removes the overlay + calls stop() immediately once min-dwell has already elapsed', () => {
  const { sandbox, overlay, spy } = loadWithSpy();
  overlay.removed = 0;
  overlay.remove = () => { overlay.removed++; };
  sandbox.__opts = { eyeBolt: true };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  assert.equal(spy.stopped.length, 0);
  // Spy the ACTUAL mounted overlay (document.createElement node, reachable
  // via state) — getElementById resolves it through the live-node map.
  const mounted = run(sandbox, 'state._fullPageRefreshSplashOverlay');
  let removed = 0;
  mounted.remove = () => { removed++; };

  const timeouts = [];
  sandbox.setTimeout = (fn, ms) => { timeouts.push({ fn, ms }); return timeouts.length; };

  setState(sandbox, { _fullPageRefreshSplashMountedAt: Date.now() - 10000 }); // long past the 600ms default
  vm.runInContext('dismissFullPageRefreshSplash()', sandbox);
  assert.equal(removed, 1, 'splash should be removed synchronously once min-dwell has elapsed');
  assert.equal(timeouts.length, 0, 'no deferral needed once min-dwell has elapsed');
  assert.deepEqual(spy.stopped, ['startSessionLoading'], 'the stored stop() must be called');
});

test('dismissFullPageRefreshSplash defers removal when min-dwell has not elapsed, then removes + stops', () => {
  const { sandbox, overlay, spy } = loadWithSpy();
  overlay.removed = 0;
  overlay.remove = () => { overlay.removed++; };
  sandbox.__opts = { eyeBolt: true };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  const mounted = run(sandbox, 'state._fullPageRefreshSplashOverlay');
  let removed = 0;
  mounted.remove = () => { removed++; };

  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };

  setState(sandbox, { _fullPageRefreshSplashMountedAt: Date.now() }); // just mounted
  vm.runInContext('dismissFullPageRefreshSplash()', sandbox);
  assert.equal(removed, 0, 'splash must not be removed before the 600ms min-dwell');
  assert.equal(spy.stopped.length, 0);
  assert.equal(scheduled.length, 1, 'exactly one deferred-removal timer');
  assert.ok(scheduled[0].ms > 400 && scheduled[0].ms <= 600, `expected a deferral close to the 600ms default, got ${scheduled[0].ms}`);
  scheduled[0].fn();
  assert.equal(removed, 1, 'deferred removal should remove the overlay');
  assert.deepEqual(spy.stopped, ['startSessionLoading']);
});

test('dismissFullPageRefreshSplash is idempotent — a second call neither re-stops nor double-removes', () => {
  const { sandbox, overlay, spy } = loadWithSpy();
  overlay.removed = 0;
  overlay.remove = () => { overlay.removed++; };
  sandbox.__opts = { eyeBolt: false };
  vm.runInContext('mountFullPageRefreshSplash(__opts)', sandbox);
  const mounted = run(sandbox, 'state._fullPageRefreshSplashOverlay');
  let removed = 0;
  mounted.remove = () => { removed++; };
  setState(sandbox, { _fullPageRefreshSplashMountedAt: Date.now() - 10000 });
  vm.runInContext('dismissFullPageRefreshSplash(); dismissFullPageRefreshSplash();', sandbox);
  assert.ok(removed >= 1, 'overlay remove() invoked');
  assert.deepEqual(spy.stopped, ['startEyeOnly'], 'stop() called exactly once despite two dismiss calls');
});

// ── (c) performFullPageRefresh reloads only after min-dwell ────────────────

test('performFullPageRefresh mounts the splash and performs the navigation only after the min-dwell', () => {
  const { sandbox, overlay, spy } = loadWithSpy();
  overlay.removed = 0;
  overlay.remove = () => { overlay.removed++; };
  // rAF is used for scheduling only (the animation stays in splash-art.js)
  // — fire it synchronously; the (test-controlled) setTimeout carries dwell.
  sandbox.requestAnimationFrame = (cb) => { cb(0); return 1; };
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };
  const reloaded = { done: false };
  sandbox.__opts = { eyeBolt: false, reload: () => { reloaded.done = true; } };
  vm.runInContext('performFullPageRefresh(__opts)', sandbox);

  assert.equal(spy.calls[0] && spy.calls[0].fn, 'startEyeOnly', 'inventory treatment for this row: eyeBolt:false');
  const mounted = run(sandbox, 'state._fullPageRefreshSplashOverlay');
  let removed = 0;
  mounted.remove = () => { removed++; };
  assert.equal(removed, 0, 'overlay still up while the dwell runs');
  assert.equal(reloaded.done, false, 'no navigation before the dwell');
  assert.equal(scheduled.length, 1, 'exactly one dwell timer');
  assert.ok(scheduled[0].ms > 400 && scheduled[0].ms <= 600, `expected a dwell close to the 600ms default, got ${scheduled[0].ms}`);

  scheduled[0].fn();
  assert.equal(removed, 1, 'overlay dismissed before navigation');
  assert.equal(reloaded.done, true, 'opts.reload() called after the dwell');
  assert.deepEqual(spy.stopped, ['startEyeOnly']);
});

test('performFullPageRefresh defaults to location.reload() when no opts.reload is provided', () => {
  const { sandbox, spy } = loadWithSpy();
  sandbox.requestAnimationFrame = (cb) => { cb(0); return 1; };
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };
  const reloaded = { done: false };
  sandbox.location = { hash: '', href: 'http://localhost/', pathname: '/', reload() { reloaded.done = true; } };
  sandbox.__opts = { eyeBolt: true, reload: undefined };
  vm.runInContext('performFullPageRefresh(__opts)', sandbox);
  assert.equal(spy.calls[0].fn, 'startSessionLoading', 'eyeBolt:true (inventory-graded row)');
  assert.equal(reloaded.done, false);
  assert.ok(scheduled.length >= 1, 'dwell is enforced even on the default-reload path');
  scheduled[0].fn();
  assert.equal(reloaded.done, true, 'location.reload() used as the default navigation');
});

// ── (d) reduced-motion regression (splash-art.js contract) ─────────────────

function loadSplashArtMotion({ reduced }) {
  const sandbox = buildSandbox();
  sandbox.matchMedia = (q) => ({ matches: reduced && q === '(prefers-reduced-motion: reduce)', addEventListener() {}, addListener() {} });
  sandbox.performance = { now: () => 0 };
  // rAF stub: fires the FIRST callback synchronously (one frame), counts
  // every schedule. A live loop schedules a second one -> rafCount grows.
  let rafCount = 0;
  sandbox.requestAnimationFrame = (cb) => {
    rafCount++;
    if (rafCount === 1) cb(0);
    return rafCount;
  };
  // No-op 2D ctx that counts frames (one clearRect() per drawn frame) and
  // otherwise absorbs every drawing call splash-art.js makes.
  const target = { gradient: { addColorStop() {} }, frames: 0 };
  const ctx = new Proxy(target, {
    get(t, p) {
      if (p === 'clearRect') return () => { t.frames++; };
      if (p === 'createLinearGradient') return () => t.gradient;
      if (p === 'frames') return t.frames;
      if (typeof p !== 'string' || !(p in t)) t[p] = () => {};
      return t[p];
    },
    set(t, p, v) { t[p] = v; return true; },
  });
  const canvas = { width: 0, height: 0, isConnected: true, getContext() { return ctx; }, getBoundingClientRect() { return { width: 200, height: 200 }; } };
  vm.createContext(sandbox);
  loadScript(SPLASH_ART_JS, sandbox);
  return { sandbox, canvas, rafCount: () => rafCount, frames: () => target.frames };
}

test('prefers-reduced-motion: the splash renders exactly ONE static frame and never enters the animation loop', () => {
  const { sandbox, canvas, rafCount, frames } = loadSplashArtMotion({ reduced: true });
  sandbox.DWSplashArt.startSessionLoading(canvas);
  assert.equal(frames(), 1, 'reduced motion: exactly one static frame drawn (loop() draws t=0)');
  assert.equal(rafCount(), 1, 'reduced motion: loop() must not schedule a second rAF');
});

test('motion allowed (contrast): loop() DOES schedule the next frame after the first one paints', () => {
  const { sandbox, canvas, rafCount, frames } = loadSplashArtMotion({ reduced: false });
  sandbox.DWSplashArt.startEyeOnly(canvas);
  assert.equal(frames(), 1, 'first frame painted in the test harness');
  assert.equal(rafCount(), 2, 'without reduced motion the loop requests its next frame');
});

// ── (e) Wired sites — one test per BL434 inventory row that fires in app.js ─
// Each test engages ONLY that row's entry point (with the minimum state the
// row's branch reads), then asserts (1) the shared overlay was mounted with
// the primitive the inventory table graded for that row, and (2) the
// location.reload() default fired only after the min-dwell timer. rAF is
// fired synchronously and the sandbox timer is captured, so these stay fast
// and clock-free (same trick as section (c) above).

function armRefreshSandbox(sandbox) {
  sandbox.requestAnimationFrame = (cb) => { cb(0); return 1; };
  const scheduled = [];
  sandbox.setTimeout = (fn, ms) => { scheduled.push({ fn, ms }); return scheduled.length; };
  return scheduled;
}

// The 600ms-default dwell timer among whatever the path schedules (a row's
// surrounding handler may also schedule an unrelated no-op timer, e.g. the
// WS-open handler's updateProgressOverlay removal).
function findDwellTimer(scheduled) {
  return scheduled.find((t) => t.ms > 400 && t.ms <= 600);
}

test('site (row 1, eye-only): setLocaleOverride locale switch mounts the eye overlay and reloads only after min-dwell', async () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  // Branch state: 'de' is in window._i18n.supported (app.js defines the
  // default table at load); the sandbox fetch stub resolves /api/config to
  // {ok:false} — its catch treats the whisper sync as best-effort, which is
  // exactly the non-fatal contract this site already had.
  sandbox.__lang = 'de';
  const p = vm.runInContext('setLocaleOverride(__lang)', sandbox);
  assert.ok(p && typeof p.then === 'function', 'setLocaleOverride is async');
  await flushAsync(10);
  assert.equal(spy.calls.length, 1, 'the site engaged the shared splash exactly once');
  assert.equal(spy.calls[0].fn, 'startEyeOnly', 'row 1 graded eye-only — no bolt');
  assert.equal(sandbox.location.reloaded, false, 'no reload before min-dwell');
  const dwell = findDwellTimer(scheduled);
  assert.ok(dwell, 'a min-dwell timer is scheduled');
  dwell.fn();
  assert.equal(sandbox.location.reloaded, true, 'location.reload fires only after min-dwell');
  assert.deepEqual(spy.stopped, ['startEyeOnly'], 'the eye animation was stopped as part of the dismiss');
});

test('site (row 2, eye+bolt): WS-open version catch-up mismatch fires the session-loading treatment, then reload after dwell', async () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  // A fake WebSocket that records the 'open' listener connect() attaches.
  const listeners = {};
  sandbox.WebSocket = function(url, sub) {
    this.url = url;
    this.addEventListener = (type, fn) => { listeners[type] = fn; };
  };
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CONNECTING = 0;
  // Branch state: a KNOWN old daemon version vs. the NEW one /api/health reports.
  sandbox.fetch = (url) => {
    if (String(url) === '/api/health') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: 'v9.9.9' }) });
    }
    return Promise.resolve({ ok: false, status: 404 });
  };
  setState(sandbox, { ws: null, connected: false, activeServer: null, _daemonVersion: 'v1.0.0' });
  vm.runInContext('connect()', sandbox);
  assert.equal(typeof listeners.open, 'function', 'connect() attached the open handler this row lives in');
  listeners.open();
  await flushAsync(10); // let the /api/health fetch chain resolve
  assert.equal(spy.calls.length, 1, 'the version-mismatch branch engaged the shared splash exactly once');
  assert.equal(spy.calls[0].fn, 'startSessionLoading', 'row 2 is the one eye+bolt row (session continuation)');
  assert.equal(sandbox.location.reloaded, false, 'no reload before min-dwell');
  const dwell = findDwellTimer(scheduled);
  assert.ok(dwell, 'a min-dwell timer is scheduled');
  dwell.fn();
  assert.equal(sandbox.location.reloaded, true, 'location.reload fires only after min-dwell');
  assert.deepEqual(spy.stopped, ['startSessionLoading'], 'the eye+bolt animation was stopped as part of the dismiss');
});

test('site (row 3, eye-only): sessions-broadcast daemon version change mounts the eye overlay and reloads only after min-dwell', () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  // Branch state: a stored version that now disagrees with the broadcast.
  setState(sandbox, { activeServer: null, sessions: [], _daemonVersion: 'v1.0.0' });
  sandbox.__msg = { type: 'sessions', data: { version: 'v2.0.0', sessions: [] } };
  vm.runInContext('handleMessage(__msg)', sandbox);
  assert.equal(spy.calls.length, 1, 'the broadcast version-mismatch branch engaged the shared splash');
  assert.equal(spy.calls[0].fn, 'startEyeOnly', 'row 3 graded eye-only — bolt would be misleading');
  assert.equal(sandbox.location.reloaded, false, 'no reload before min-dwell');
  const dwell = findDwellTimer(scheduled);
  assert.ok(dwell, 'a min-dwell timer is scheduled');
  dwell.fn();
  assert.equal(sandbox.location.reloaded, true, 'location.reload fires only after min-dwell');
  assert.deepEqual(spy.stopped, ['startEyeOnly']);
});

test('site (row 5, eye-only): SW controllerchange handler mounts the eye overlay and reloads only after min-dwell', async () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  // A bare serviceWorker surface just enough for registerServiceWorker()
  // to reach the controllerchange listener registration.
  const reg = { scope: '/', update() { return Promise.resolve(); }, waiting: undefined, addEventListener() {} };
  let ccListener = null;
  sandbox.navigator = {
    userAgent: 'node-test',
    serviceWorker: {
      controller: null,
      register: () => Promise.resolve(reg),
      addEventListener: (type, fn) => { if (type === 'controllerchange') ccListener = fn; },
    },
  };
  vm.runInContext('registerServiceWorker()', sandbox);
  await flushAsync(10);
  assert.equal(typeof ccListener, 'function', 'the controllerchange listener was attached');
  ccListener(); // fire the site directly — it is a standalone listener
  assert.equal(spy.calls.length, 1, 'controllerchange engaged the shared splash');
  assert.equal(spy.calls[0].fn, 'startEyeOnly', 'row 5 graded eye-only — PWA asset swap, not a session connect');
  assert.equal(sandbox.location.reloaded, false, 'no reload before min-dwell');
  const dwell = findDwellTimer(scheduled);
  assert.ok(dwell, 'a min-dwell timer is scheduled');
  dwell.fn();
  assert.equal(sandbox.location.reloaded, true, 'location.reload fires only after min-dwell');
  assert.deepEqual(spy.stopped, ['startEyeOnly']);
});

test('site (row 6, eye-only): forcePWAUpdate cleanup fires the eye overlay and reloads only after min-dwell', async () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  // The sandbox navigator exposes no serviceWorker and the sandbox has no
  // caches, so the cleanup branch is a no-op — exactly the gating we keep;
  // only the final navigation changes.
  await sandbox.forcePWAUpdate();
  assert.equal(spy.calls.length, 1, 'forcePWAUpdate engaged the shared splash');
  assert.equal(spy.calls[0].fn, 'startEyeOnly', 'row 6 graded eye-only — cache-bust, not a session connect');
  assert.equal(sandbox.location.reloaded, false, 'cleanup ran, but no reload before min-dwell');
  const dwell = findDwellTimer(scheduled);
  assert.ok(dwell, 'a min-dwell timer is scheduled');
  dwell.fn();
  assert.equal(sandbox.location.reloaded, true, 'location.reload fires only after min-dwell');
  assert.deepEqual(spy.stopped, ['startEyeOnly']);
});

test('leave-as-is (row 4): window.replaySplash still bare-reloads with no shared-splash mount', async () => {
  const { sandbox, spy } = loadWithSpy();
  const scheduled = armRefreshSandbox(sandbox);
  await sandbox.replaySplash();
  assert.equal(spy.calls.length, 0, 'the inventory table grades this row leave-as-is: the boot #splash Earthrise immediately follows the reload');
  assert.equal(sandbox.location.reloaded, true, 'it still reloads, via the original bare path');
  assert.equal((scheduled || []).length, 0);
  const src = require('fs').readFileSync(APP_JS, 'utf8');
  const replay = src.slice(src.indexOf('window.replaySplash'), src.indexOf('window.replaySplash') + 400);
  assert.match(replay, /location\.reload\(\)/, 'source guard: window.replaySplash still uses the bare reload');
  assert.doesNotMatch(replay, /performFullPageRefresh/, 'source guard: no shared-splash wiring at the replaySplash site');
});
