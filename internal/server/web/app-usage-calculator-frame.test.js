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
    sandbox.fetchCalls.push({ url, init: init || {} });
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

test('the switcher shows all three variants for variant all and is absent for a single variant', () => {
  const s = loadApp();
  const all = frame(s, workedExampleBody({ variant: 'all' }), {});
  assert.ok(all.includes('usage-calc-switcher'), 'switcher missing for variant all');
  assert.ok(all.includes('Pace gauge') && all.includes('Lane planner') && all.includes('Burn ledger'));

  const single = frame(s, workedExampleBody({ variant: 'pace-gauge' }), {});
  assert.ok(!single.includes('usage-calc-switcher'), 'switcher must be absent for a single variant');
  assert.ok(!single.includes("usageCalcSetVariant('"), 'no switch button for a single variant');
});

test('a single variant with no renderer on this page says it is not available yet', () => {
  const s = loadApp();
  vm.runInContext("delete window.usageCalcRenderers['burn-ledger']", s);
  const html = frame(s, workedExampleBody({ variant: 'burn-ledger' }), {});
  assert.ok(html.includes('This view is not available yet.'), 'unavailable message missing');
});

test('the view counter is off by default and recording it never calls fetch', () => {
  const s = loadApp();
  assert.equal(vm.runInContext('usageCalcCounterOn()', s), false);
  const html = frame(s, workedExampleBody(), {});
  assert.ok(html.includes('Off'), 'counter state should read Off');

  vm.runInContext("window.usageCalcSetVariant('lane-planner')", s);
  assert.equal(s.localStorage.getItem('datawatch.usageCalc.counts'), null, 'nothing is counted while off');
  assert.equal(s.fetchCalls.length, 0, 'opening a variant must not call the daemon');

  vm.runInContext('window.usageCalcToggleCounter()', s);
  vm.runInContext("window.usageCalcSetVariant('burn-ledger')", s);
  assert.equal(s.fetchCalls.length, 0, 'counting must never call the daemon');
  const counts = JSON.parse(s.localStorage.getItem('datawatch.usageCalc.counts'));
  assert.equal(counts['burn-ledger'].opened, 1, 'the opened variant is counted once it is on');
});

test('the counts show back only on request and the browser copy is cleared on demand', () => {
  const s = loadApp();
  vm.runInContext('window.usageCalcToggleCounter()', s);
  vm.runInContext("window.usageCalcSetVariant('burn-ledger')", s);
  assert.ok(!frame(s, workedExampleBody(), {}).includes('advice used'), 'counts must stay hidden until asked');
  vm.runInContext('window.usageCalcShowCounts()', s);
  assert.ok(frame(s, workedExampleBody(), {}).includes('Burn ledger: opened 1 times, advice used 0 times'));
  vm.runInContext('window.usageCalcClearCounts()', s);
  assert.equal(s.localStorage.getItem('datawatch.usageCalc.counts'), null);
});

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

test('remaining work names the figure in use and shows the queue count beside it', () => {
  const s = loadApp();
  const estimate = frame(s, workedExampleBody(), {});
  assert.ok(estimate.includes('20.0 points (estimate) · queued tasks: 6'), 'estimate label or queue count missing');

  const typed = frame(s, workedExampleBody({
    advice: { remaining_work: { in_use: 'typed', points: 15.5, estimate_points: 20, typed_points: 15.5, queue_tasks: 6, cost_per_task: 3.33 } },
  }), {});
  assert.ok(typed.includes('15.5 points (typed) · queued tasks: 6'), 'typed label or queue count missing');
  assert.ok(typed.includes('Clear typed figure'), 'clear control missing');
  assert.ok(typed.includes('usageCalcSaveRemainingWork()'), 'override field needs its save control');
});

test('the extra-credit line shows the ceiling as 100 plus the extra points', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ advice: { extra_credit_points: 15, ceiling: 115 } }), {});
  assert.ok(html.includes('Ceiling 115% (100 plus 15 extra-credit points)'), 'extra-credit ceiling line missing');
});

test('zero lanes is labelled as the operator choice and never advised', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ advice: { lanes: 0 } }), {});
  assert.ok(html.includes('never advised.'), 'zero-lane wording missing');
  assert.ok(html.includes('usageCalcStepLanes(-1)') && html.includes('disabled'), 'minus must be disabled at zero');
});

test('the empty state shows when no reading is saved, and the form is still there', () => {
  const s = loadApp();
  const html = frame(s, emptyBody(), {});
  assert.ok(html.includes('No readings yet'), 'empty title missing');
  assert.ok(!html.includes('usage-calc-verdict'), 'no verdict without a reading');
  assert.ok(!html.includes('usage-calc-pace-gauge'), 'no variant body without a reading');
  assert.ok(html.includes('usageCalcSubmitReading()'), 'reading form must stay available');
});

test('the policy radios carry the data-policy ids and labels, with the chosen policy checked', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ policy: 'pace' }), {});
  for (const [id, label] of [['use-all', 'Run until used up'], ['pace', 'Pace to the week'], ['finish-early', 'Run flat out']]) {
    assert.ok(html.includes(`data-policy="${id}"`), 'missing data-policy ' + id);
    assert.ok(html.includes(label), 'missing label ' + label);
  }
  assert.equal((html.match(/name="usageCalcPolicy"/g) || []).length, 3, 'the three radios share one group');
  assert.match(html, /data-policy="pace" value="pace" checked/);
  assert.match(html, /data-policy="use-all" value="use-all" onchange/);
  assert.ok(!/data-policy="use-all" value="use-all" checked/.test(html), 'only the chosen policy is checked');
});

test('a daemon error is shown plainly, and the frame still draws the form', () => {
  const s = loadApp();
  const html = frame(s, null, { error: 'usage calculator: not wired on this daemon' });
  assert.ok(html.includes('Could not load usage advice: usage calculator: not wired on this daemon'));
  assert.ok(html.includes('usageCalcSubmitReading()'));
});

test('refresh is one GET of the advice endpoint, and a policy change refetches with the policy query and saves nothing', async () => {
  const s = loadApp();
  const body = workedExampleBody({ policy: 'use-all' });
  s.fetch = (url, init) => {
    s.fetchCalls.push({ url, init: init || {} });
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
  const body = vm.runInContext('_automationBodyHTML()', s);
  const cap = body.indexOf('id="automationCapacityCard"');
  const usage = body.indexOf('id="automationUsageCard"');
  assert.ok(cap >= 0 && usage > cap, 'usage card slot must follow the capacity card slot');
  assert.equal(body.slice(cap, usage).split('id="').length - 1, 1, 'no other element sits between the two cards');
  const ctl = vm.runInContext('_automationUsageLoader()', s);
  assert.equal(vm.runInContext('window._listLoaders.automation_usage', s), ctl, 'registered under its loader key');
  assert.equal(vm.runInContext('_automationUsageLoader()', s), ctl, 'one loader, reused');
});

test('the reading body is built from the form values and rejects an out-of-range percent', () => {
  const s = loadApp();
  const ok = vm.runInContext("usageCalcReadingBody({ percent: '72', session: '', lanes: 2, readAt: '2026-10-23T22:08', now: 0 })", s);
  assert.equal(ok.body.percent, 72);
  assert.equal(ok.body.lanes, 2);
  assert.equal(ok.body.source, 'typed');
  assert.equal('session_percent' in ok.body, false, 'an empty session field is not sent');
  assert.match(ok.body.at, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);

  const withSession = vm.runInContext("usageCalcReadingBody({ percent: '70', session: '12.5', lanes: 0, readAt: '', now: Date.UTC(2026, 9, 23, 22, 8) })", s);
  assert.equal(withSession.body.session_percent, 12.5);
  assert.equal(withSession.body.at, '2026-10-23T22:08:00.000Z', 'an empty read time falls back to the given now');

  for (const percent of ['1001', '-1', '']) {
    const bad = vm.runInContext(`usageCalcReadingBody({ percent: '${percent}', session: '', lanes: 0, readAt: '', now: 0 })`, s);
    assert.equal(bad.error, 'Enter a number from 0 to 1000.', 'percent ' + JSON.stringify(percent));
  }
  const badSession = vm.runInContext("usageCalcReadingBody({ percent: '70', session: '101', lanes: 0, readAt: '', now: 0 })", s);
  assert.equal(badSession.error, 'Enter a number from 0 to 100, or leave it empty.');
});

test('a points figure must be a number of 0 or more', () => {
  const s = loadApp();
  assert.deepEqual(JSON.parse(JSON.stringify(vm.runInContext("usageCalcPointsBody('15.5')", s))), { body: { points: 15.5 } });
  assert.equal(vm.runInContext("usageCalcPointsBody('-1').error", s), 'Enter a number of points, 0 or more.');
  assert.equal(vm.runInContext("usageCalcPointsBody('').error", s), 'Enter a number of points, 0 or more.');
});

test('the reading time defaults to now and the lanes stepper goes up to the maximum', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody(), { now: Date.UTC(2026, 9, 23, 22, 8) });
  assert.match(html, /id="usageCalcReadAt" type="datetime-local" value="\d{4}-\d{2}-\d{2}T\d{2}:\d{2}"/);
  assert.match(html, /id="usageCalcLanesPlus"[^>]*disabled/, 'plus is disabled at the maximum of 3 lanes');
});

test('the suggested-lanes button shows the advised count, and using it counts once when the counter is on', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody(), {});
  assert.ok(html.includes('Use suggested lanes (1)'), 'suggested count missing from the button');
  assert.ok(html.includes('usageCalcUseSuggested()'));

  s._b = workedExampleBody();
  vm.runInContext('window._usageCalc.body = _b', s);
  vm.runInContext('window.usageCalcUseSuggested()', s);
  assert.equal(s.localStorage.getItem('datawatch.usageCalc.counts'), null, 'nothing is counted while off');

  vm.runInContext('window.usageCalcToggleCounter()', s);
  vm.runInContext('window.usageCalcUseSuggested()', s);
  const counts = JSON.parse(s.localStorage.getItem('datawatch.usageCalc.counts'));
  assert.equal(counts['pace-gauge'].used, 1, 'the active variant records one use');
  assert.equal(s.fetchCalls.length, 0, 'using the suggestion never calls the daemon');
});
