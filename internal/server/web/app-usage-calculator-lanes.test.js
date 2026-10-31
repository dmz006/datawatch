// Usage calculator card — the what-if lane control and the running-now line.
//
// The reading form no longer takes a lane count: a reading carries percent,
// session percent, time and source only. The stepper lives in the policy card
// as a what-if that is sent as the lanes query value on the advice request,
// only once the operator has set it, and the card names the lanes running now
// from res.advice.lanes_running_now. Locale text for these keys arrives in a
// later unit, so these tests assert on element ids and data attributes only.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { workedExampleBody } = require('./usage-calculator-fixture');

function loadApp() {
  const sandbox = buildSandbox();
  sandbox.fetchCalls = [];
  sandbox.fetch = (url, init) => {
    sandbox.fetchCalls.push({ url, init: init || {} });
    return Promise.resolve({ ok: false, status: 500, text() { return Promise.resolve('no reply'); } });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function run(sandbox, code, vars) {
  Object.assign(sandbox, vars || {});
  return vm.runInContext(code, sandbox);
}

const LANE_IDS = ['usageCalcLanes', 'usageCalcLanesMinus', 'usageCalcLanesPlus', 'usageCalcZeroNote'];

test('the reading body has no lanes field', () => {
  const s = loadApp();
  const ok = run(s, "usageCalcReadingBody({ percent: '72', session: '12', lanes: 2, readAt: '2026-10-23T22:08', now: 0 })");
  assert.deepEqual(Object.keys(ok.body).sort(), ['at', 'percent', 'session_percent', 'source']);
  assert.equal(ok.body.source, 'typed');
});

test('the reading form has no lanes stepper elements', () => {
  const s = loadApp();
  const html = run(s, '_usageCalcReadingForm(_b, {})', { _b: workedExampleBody() });
  for (const id of LANE_IDS) assert.ok(!html.includes(`id="${id}"`), `${id} must not be in the reading form`);
  assert.ok(html.includes('id="usageCalcPercent"') && html.includes('id="usageCalcReadAt"'), 'the reading inputs remain');
});

test('the what-if stepper sits in the policy form and starts unset', () => {
  const s = loadApp();
  const html = run(s, '_usageCalcPolicyForm(_b, {})', { _b: workedExampleBody() });
  for (const id of LANE_IDS) assert.ok(html.includes(`id="${id}"`), `${id} missing from the policy form`);
  assert.ok(html.includes('id="usageCalcWhatIf" data-whatif-set="false"'), 'the what-if starts unset');
  assert.match(html, /id="usageCalcLanes" data-value="3"/, 'unset shows the chosen count');
  const frameHtml = run(s, 'window.usageCalcFrameHtml(_b, {})', { _b: workedExampleBody() });
  assert.ok(frameHtml.indexOf('id="usageCalcLanes"') > frameHtml.indexOf('name="usageCalcPolicy"'), 'the stepper follows the policy radios');
});

test('an unset what-if sends no lanes parameter', async () => {
  const s = loadApp();
  assert.equal(run(s, 'window._usageCalc.whatIf'), null);
  run(s, 'usageCalcRefresh()');
  await flushAsync();
  assert.equal(s.fetchCalls.length, 1);
  assert.ok(!/lanes=/.test(s.fetchCalls[0].url), s.fetchCalls[0].url);
});

test('a what-if of 0 is sent as lanes=0, alongside the policy', async () => {
  const s = loadApp();
  run(s, 'window._usageCalc.body = _b', { _b: workedExampleBody() });
  run(s, "window._usageCalc.policy = 'pace'");
  run(s, 'usageCalcSetLanes(0)');
  await flushAsync();
  assert.equal(run(s, 'window._usageCalc.whatIf'), 0);
  assert.equal(s.fetchCalls.length, 1);
  assert.match(s.fetchCalls[0].url, /\/api\/usage\/advice\?policy=pace&lanes=0$/);
  assert.equal(s.localStorage.getItem('datawatch.usageCalc.whatIf'), null, 'the what-if is not put in browser storage');
});

test('a set what-if shows its own value in the stepper and is clamped to the maximum', () => {
  const s = loadApp();
  run(s, 'window._usageCalc.body = _b', { _b: workedExampleBody() });
  run(s, 'usageCalcSetLanes(9)');
  assert.equal(run(s, 'window._usageCalc.whatIf'), 3);
  run(s, 'window._usageCalc.whatIf = 2');
  const html = run(s, '_usageCalcPolicyForm(_b, {})', { _b: workedExampleBody() });
  assert.ok(html.includes('data-whatif-set="true"'));
  assert.match(html, /id="usageCalcLanes" data-value="2"/);
});

test('the running-now line renders the number, and the not-known line renders on null', () => {
  const s = loadApp();
  const known = run(s, '_usageCalcPolicyForm(_b, {})', { _b: workedExampleBody({ advice: { lanes_running_now: 2 } }) });
  assert.ok(known.includes('data-running-now="2"'), 'the running count is on the card');
  assert.ok(!known.includes('data-running-now="unknown"'));
  const zero = run(s, '_usageCalcPolicyForm(_b, {})', { _b: workedExampleBody({ advice: { lanes_running_now: 0 } }) });
  assert.ok(zero.includes('data-running-now="0"'), 'zero running is a number, not unknown');
  const unknown = run(s, '_usageCalcPolicyForm(_b, {})', { _b: workedExampleBody({ advice: { lanes_running_now: null } }) });
  assert.ok(unknown.includes('data-running-now="unknown"'), 'null renders the not-known line');
});
