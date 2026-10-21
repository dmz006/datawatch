// Coalescing regression test for loadSessionSchedules: a burst of
// same-session calls (poll tick + submit + cancel landing in the same
// task, e.g.) must issue at most ONE in-flight request plus ONE trailing
// refresh, not N requests. Different sessions must each get their own
// request, and the rendered output / element guard must keep working.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, makeStubElement, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadApp(sandbox) {
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
}

// installApiFetchCapture replaces app.js's own apiFetch (a top-level
// function -> global binding) with one that records every call path and
// hands the test back a deferred promise per call, so the test controls
// EXACTLY when each request settles.
function installApiFetchCapture(sandbox) {
  const calls = [];
  const deferred = [];
  sandbox.apiFetch = function (pathname) {
    calls.push(pathname);
    let resolve, reject;
    const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
    deferred.push({ promise, resolve, reject, pathname });
    return promise;
  };
  return { calls, deferred };
}

function callLoad(sandbox, sessionId) {
  return vm.runInContext(`loadSessionSchedules(${JSON.stringify(sessionId)})`, sandbox);
}

test('100 synchronous calls for one session issue at most 1 in-flight + 1 trailing request', async () => {
  const sandbox = buildSandbox();
  const el = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? el : makeStubElement());
  loadApp(sandbox);
  const { calls, deferred } = installApiFetchCapture(sandbox);

  for (let i = 0; i < 100; i++) callLoad(sandbox, 'sess-1');

  // Exactly one request may be in flight; the other 99 calls must have
  // coalesced onto it instead of issuing their own fetch.
  assert.equal(calls.length, 1, `expected 1 in-flight request, got ${calls.length}`);
  assert.match(calls[0], /^\/api\/schedules\?session_id=sess-1&state=pending$/);

  // Settle the in-flight request; a trailing refresh is owed to the
  // coalesced callers, and it must be exactly one.
  deferred[0].resolve([{ id: 'sc1', command: 'continue', run_at: 'in 5m' }]);
  await flushAsync(30);
  assert.equal(calls.length, 2, `expected exactly 1 trailing refresh, got ${calls.length - 1}`);

  // Settle the trailing refresh too; nothing else is owed now.
  deferred[1].resolve([]);
  await flushAsync(30);
  assert.equal(calls.length, 2, 'no third request may follow once pending callers are served');
});

test('different sessions each get their own in-flight request when a burst lands', async () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? makeStubElement() : makeStubElement());
  loadApp(sandbox);
  const { calls, deferred } = installApiFetchCapture(sandbox);

  for (let i = 0; i < 100; i++) {
    callLoad(sandbox, 'sess-a');
    callLoad(sandbox, 'sess-b');
  }

  // One in-flight per session; coalescing must NOT leak across sessions.
  const a = calls.filter(p => p.includes('session_id=sess-a')).length;
  const b = calls.filter(p => p.includes('session_id=sess-b')).length;
  assert.equal(a, 1, `sess-a should have 1 in-flight request, got ${a}`);
  assert.equal(b, 1, `sess-b should have 1 in-flight request, got ${b}`);

  // Settle both; each session owes exactly one trailing refresh.
  for (const d of deferred.slice()) d.resolve([]);
  await flushAsync(30);
  assert.equal(calls.filter(p => p.includes('session_id=sess-a')).length, 2, 'sess-a: 1 in-flight + 1 trailing');
  assert.equal(calls.filter(p => p.includes('session_id=sess-b')).length, 2, 'sess-b: 1 in-flight + 1 trailing');
  await flushAsync(30);
  assert.equal(calls.filter(p => p.includes('session_id=sess-a')).length, 2, 'sess-a: no extra requests after trailing');
  assert.equal(calls.filter(p => p.includes('session_id=sess-b')).length, 2, 'sess-b: no extra requests after trailing');
});

test('a failed in-flight request still triggers the single trailing refresh and renders nothing', async () => {
  const sandbox = buildSandbox();
  const el = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? el : makeStubElement());
  loadApp(sandbox);
  const { calls, deferred } = installApiFetchCapture(sandbox);

  for (let i = 0; i < 50; i++) callLoad(sandbox, 'sess-err');
  assert.equal(calls.length, 1);

  deferred[0].reject(new Error('network down'));
  await flushAsync(30);
  assert.equal(calls.length, 2, 'trailing refresh must run even when the in-flight request fails');
  assert.equal(el.style.display, 'none', 'failed load must hide the panel');

  deferred[1].resolve([{ id: 'sc9', command: 'hi', run_at: 'in 1m' }]);
  await flushAsync(30);
  assert.equal(calls.length, 2, 'no further refresh once pending callers are served');
  assert.equal(el.style.display, 'block');
  assert.match(el.innerHTML, /Scheduled \(1\)/);
  assert.match(el.innerHTML, /sc9|hi/);
});

test('the sessionSchedules element guard still short-circuits when the element is absent', async () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = () => null;
  loadApp(sandbox);
  const { calls } = installApiFetchCapture(sandbox);

  assert.doesNotThrow(() => { for (let i = 0; i < 10; i++) callLoad(sandbox, 'sess-x'); });
  await flushAsync(5);
  assert.equal(calls.length, 0, 'no request may be issued without the sessionSchedules element');
});

test('loadSessionSchedules stays exported on window', () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? makeStubElement() : makeStubElement());
  loadApp(sandbox);
  assert.equal(typeof sandbox.loadSessionSchedules, 'function');
  assert.equal(typeof sandbox.window.loadSessionSchedules, 'function');
});

// ── updateSession storm regression ──────────────────────────────────────────────
// updateSession is invoked once per session on every `sessions` state push and
// on the post-reconnect /api/sessions refresh (one call per fetched session).
// Before the refactor, each of those calls ended in an unconditional
// loadSessionSchedules (via updateSessionDetailButtons), so a 25-session push
// while sitting in one session's detail view issued up to 25 schedule fetches.
// Now the ONLY update-Session schedule refresh allowed is for the ONE session
// the detail view is showing, only when its STATE actually changed, and it must
// go through the coalescing loader (so even that is capped at 1 in-flight + 1
// trailing).

function loadAppState(sandbox, { view, activeSession, sessions = [] } = {}) {
  vm.runInContext(`
    state.activeView = ${JSON.stringify(view)};
    state.activeSession = ${JSON.stringify(activeSession || null)};
    state.sessions = ${JSON.stringify(sessions)};
  `, sandbox);
}

function callUpdateSession(sandbox, sid, sessState) {
  vm.runInContext(`updateSession({ full_id: ${JSON.stringify(sid)}, state: ${JSON.stringify(sessState)} })`, sandbox);
}

function schedCallsFor(sandbox, pathname, sid) {
  return pathname.filter(p => p.includes('session_id=' + sid)).length;
}

test('25 updateSession calls with the detail view open on one session issue ≤ 2 total schedule requests', async () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => (id === 'inputBar' ? makeStubElement() : (id === 'sessionSchedules' ? makeStubElement() : makeStubElement()));
  loadApp(sandbox);
  // Detail view is open on sess-1, which currently reports 'running'.
  loadAppState(sandbox, { view: 'session-detail', activeSession: 'sess-1', sessions: [{ full_id: 'sess-1', state: 'running' }] });
  const { calls, deferred } = installApiFetchCapture(sandbox);

  // A burst: sess-1 transitions running → waiting_input (an "on input"
  // schedule could now be pending/firing), and 24 other sessions also
  // update.
  callUpdateSession(sandbox, 'sess-1', 'waiting_input');
  for (let i = 2; i <= 25; i += 1) callUpdateSession(sandbox, 'sess-' + i, 'complete');

  // sess-1's refresh coalesces to exactly ONE in-flight request.
  assert.equal(schedCallsFor(sandbox, calls, 'sess-1'), 1, 'viewed session should have at most 1 in-flight request');

  // The 25-1 = 24 non-viewed sessions must have issued ZERO of their own.
  for (let i = 2; i <= 25; i += 1) {
    assert.equal(schedCallsFor(sandbox, calls, 'sess-' + i), 0, `non-viewed sess-${i} must issue no schedule request`);
  }

  // Settle; any trailing refresh owed to coalesced callers would be at most
  // ONE more — the cap is never more than 2 requests total for the viewed
  // session.
  for (const d of deferred.slice()) d.resolve([]);
  await flushAsync(30);
  assert.ok(schedCallsFor(sandbox, calls, 'sess-1') <= 2, `viewed session may issue at most 2 requests, got ${schedCallsFor(sandbox, calls, 'sess-1')}`);

  // Settle whatever is left in flight; nothing further may follow.
  for (const d of deferred.slice(1)) d.resolve([]);
  await flushAsync(30);
  assert.ok(schedCallsFor(sandbox, calls, 'sess-1') <= 2, 'no request beyond the 2 may follow for the viewed session');
  assert.equal(calls.filter(p => !p.includes('session_id=sess-1')).length, 0, 'no request for any other session may ever appear');
});

test('updateSession for non-viewed sessions issues no schedule requests', async () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? makeStubElement() : makeStubElement());
  loadApp(sandbox);
  // Detail view is open on sess-99, which is NOT part of the burst.
  loadAppState(sandbox, { view: 'session-detail', activeSession: 'sess-99', sessions: [{ full_id: 'sess-99', state: 'running' }] });
  const { calls } = installApiFetchCapture(sandbox);

  for (let i = 1; i <= 25; i += 1) {
    callUpdateSession(sandbox, 'sess-' + i, i % 2 === 0 ? 'running' : 'complete');
  }
  await flushAsync(30);

  assert.equal(calls.length, 0, 'no schedule request may be issued for non-viewed sessions');
});

test('viewed session with UNCHANGED state issues no schedule request on updateSession', async () => {
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => (id === 'sessionSchedules' ? makeStubElement() : makeStubElement());
  loadApp(sandbox);
  loadAppState(sandbox, { view: 'session-detail', activeSession: 'sess-1', sessions: [{ full_id: 'sess-1', state: 'running' }] });
  const { calls } = installApiFetchCapture(sandbox);

  // Same state re-reported (the common WS churn case) — no refresh is due.
  for (let i = 0; i < 10; i += 1) callUpdateSession(sandbox, 'sess-1', 'running');
  await flushAsync(30);

  assert.equal(calls.length, 0, 'unchanged-state updates must not trigger a schedule fetch');
});
