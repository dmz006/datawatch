// Usage calculator frame — PWA side.
//
// window.usageCalcFrameHtml(body, opts) draws what every variant shares: the
// verdict first, the variant switcher when the setting is "all", the chosen
// variant, the reading form, the policy radios, the extra-credit and
// remaining-work sections and the optional view counter. These tests pin the
// frame's behaviour on hand-written response bodies; they make no network call
// and read no wall clock.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { workedExampleBody, emptyBody } = require('./usage-calculator-fixture');

function bundle() {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
}

// loadApp runs the real app.js in a vm sandbox with a recording fetch. The
// default fetch answers every call with a failure; a test that needs a body
// replaces it.
function loadApp() {
  const sandbox = buildSandbox();
  sandbox.fetchCalls = [];
  sandbox.fetch = (url, init) => {
    if (!String(url).includes('/burn-guard')) sandbox.fetchCalls.push({ url, init: init || {} }); // the burn guard card has its own fetches
    return Promise.resolve({ ok: false, status: 500, text() { return Promise.resolve('no reply'); } });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle();
  return sandbox;
}

function frame(sandbox, body, opts) {
  sandbox._body = body;
  sandbox._opts = opts || {};
  return vm.runInContext('window.usageCalcFrameHtml(_body, _opts)', sandbox);
}

test('a stale series shows the last-copy marker with the keeper and the copy time', () => {
  const s = loadApp();
  const stale = frame(s, workedExampleBody({ stale: true, copied_at: '2026-10-23T20:00:00-04:00', keeper: 'peer-b' }), {});
  assert.ok(stale.includes('Last copy from peer-b at'), 'keeper marker missing: ' + stale.slice(0, 200));
  assert.ok(stale.includes('20:00'), 'copy time missing');
  const fresh = frame(s, workedExampleBody({ stale: false }), {});
  assert.ok(!fresh.includes('Last copy from'), 'marker must not show on a fresh series');
});

test('the week-average notice shows only when recent_fallback is true', () => {
  const s = loadApp();
  const notice = 'No earlier reading at least 2 hours back; the week average is used, so this figure is smoother than the recent burn.';
  const fallback = frame(s, workedExampleBody({ advice: { recent_fallback: true, fallback_notice: notice } }), {});
  assert.ok(fallback.includes(notice), 'fallback notice missing');
  const normal = frame(s, workedExampleBody(), {});
  assert.ok(!normal.includes('smoother than the recent burn'), 'notice must not show without a fallback');
});

test('refresh is one GET of the advice endpoint, and a policy change refetches with the policy query and saves nothing', async () => {
  const s = loadApp();
  const body = workedExampleBody({ policy: 'use-all' });
  s.fetch = (url, init) => {
    if (!String(url).includes('/burn-guard')) s.fetchCalls.push({ url, init: init || {} });
    return Promise.resolve({ ok: true, status: 200, json() { return Promise.resolve(JSON.parse(JSON.stringify(body))); } });
  };

  vm.runInContext('window.usageCalcRefresh()', s);
  await flushAsync();
  assert.equal(s.fetchCalls.length, 1, 'one refresh is one request');
  assert.equal(s.fetchCalls[0].url, '/api/usage/advice');
  assert.equal(vm.runInContext('window._usageCalc.body !== null', s), true);

  vm.runInContext("window.usageCalcSetPolicy('pace')", s);
  await flushAsync();
  assert.equal(s.fetchCalls.length, 2);
  assert.equal(s.fetchCalls[1].url, '/api/usage/advice?policy=pace');
  assert.ok(s.fetchCalls.every(c => (c.init.method || 'GET') === 'GET'), 'a policy change must not write anything');
});

test('a failed refresh keeps the last body and shows the daemon error', async () => {
  const s = loadApp();
  s.fetch = () => Promise.resolve({ ok: false, status: 503, text() { return Promise.resolve('usage calculator: not wired on this daemon'); } });
  vm.runInContext('window.usageCalcRefresh()', s);
  await flushAsync();
  const state = vm.runInContext('window._listLoaders.automation_usage.state', s);
  assert.equal(state.error, 'usage calculator: not wired on this daemon');
  assert.equal(state.loading, false);
});

test('the card sits right after the capacity card on the Automata tab and loads through the list loader', () => {
  const s = loadApp();
  // Both cards live on the Resources sub-tab; select it before rendering the body.
  vm.runInContext("_automationState.subtab = 'resources'", s);
  const body = vm.runInContext('_automationBodyHTML()', s);
  const cap = body.indexOf('id="automationCapacityCard"');
  const usage = body.indexOf('id="automationUsageCard"');
  assert.ok(cap >= 0 && usage > cap, 'usage card slot must follow the capacity card slot');
  // The lane fill card sits between the capacity and usage cards; nothing else does.
  const between = body.slice(cap, usage);
  assert.ok(between.includes('id="automationLaneFillCard"'), 'the lane fill card sits between the two cards');
  assert.equal(between.split('id="').length - 1, 2, 'no other element sits between the two cards');
  for (const other of ['inbox', 'automations', 'progress', 'setup', 'housekeeping']) {
    vm.runInContext(`_automationState.subtab = '${other}'`, s);
    assert.ok(!vm.runInContext('_automationBodyHTML()', s).includes('id="automationUsageCard"'), 'no usage card slot on ' + other);
  }
  const ctl = vm.runInContext('_automationUsageLoader()', s);
  assert.equal(vm.runInContext('window._listLoaders.automation_usage', s), ctl, 'registered under its loader key');
  assert.equal(vm.runInContext('_automationUsageLoader()', s), ctl, 'one loader, reused');
});

test('the reading body is built from the form values and rejects an out-of-range percent', () => {
  const s = loadApp();
  const ok = vm.runInContext("usageCalcReadingBody({ percent: '72', session: '', readAt: '2026-10-23T22:08', now: 0 })", s);
  assert.equal(ok.body.percent, 72);
  assert.equal('lanes' in ok.body, false, 'the reading no longer carries a lanes field');
  assert.equal(ok.body.source, 'typed');
  assert.equal('session_percent' in ok.body, false, 'an empty session field is not sent');
  assert.match(ok.body.at, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);

  const withSession = vm.runInContext("usageCalcReadingBody({ percent: '70', session: '12.5', readAt: '', now: Date.UTC(2026, 9, 23, 22, 8) })", s);
  assert.equal(withSession.body.session_percent, 12.5);
  assert.equal(withSession.body.at, '2026-10-23T22:08:00.000Z', 'an empty read time falls back to the given now');

  for (const percent of ['1001', '-1', '']) {
    const bad = vm.runInContext(`usageCalcReadingBody({ percent: '${percent}', session: '', readAt: '', now: 0 })`, s);
    assert.equal(bad.error, 'Enter a number from 0 to 1000.', 'percent ' + JSON.stringify(percent));
  }
  const badSession = vm.runInContext("usageCalcReadingBody({ percent: '70', session: '101', readAt: '', now: 0 })", s);
  assert.equal(badSession.error, 'Enter a number from 0 to 100, or leave it empty.');
});

test('a points figure must be a number of 0 or more', () => {
  const s = loadApp();
  assert.deepEqual(JSON.parse(JSON.stringify(vm.runInContext("usageCalcPointsBody('15.5')", s))), { body: { points: 15.5 } });
  assert.equal(vm.runInContext("usageCalcPointsBody('-1').error", s), 'Enter a number of points, 0 or more.');
  assert.equal(vm.runInContext("usageCalcPointsBody('').error", s), 'Enter a number of points, 0 or more.');
});

