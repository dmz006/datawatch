// After a weekly reset with no new reading the advice is an estimate from the
// tokens since the reset. Every place the web app draws the percentage says so,
// it is not called a stale copy, and it is never prefilled as a reading.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody } = require('./usage-calculator-fixture');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = l => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${l}.json`), 'utf8'));
const EN = bundle('en');

function loadApp() {
  const sandbox = buildSandbox();
  sandbox.window.fetch = url => Promise.reject(new Error('filtered: ' + url));
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  delete sandbox.window._listLoaders.automation_usage;
  return sandbox;
}
function run(sandbox, expr, body) {
  sandbox._body = body;
  return vm.runInContext(expr, sandbox);
}
function estimatedBody() {
  const body = workedExampleBody();
  body.stale = true;
  body.stale_reason = 'a weekly reset passed since the newest reading';
  delete body.copied_at;
  body.advice.estimated = true;
  return body;
}

test('an estimated week is marked on the card, the graphs and the verdict', () => {
  const s = loadApp();
  const body = estimatedBody();
  for (const html of [
    run(s, 'window.usageCalcFrameHtml(_body, {})', body),
    run(s, '_usageCalcVerdictHtml(_body, "pace-gauge")', body),
  ]) {
    assert.ok(html.includes(EN.usage_calc_estimated), 'the plain note');
    assert.ok(!html.includes('usage-calc-stale'), 'no stale-copy line when no copy is in use');
  }
  const tile = run(s, 'window.usageCalcCardForType("usage-allowance", _body.advice)', body);
  assert.ok(tile.includes(EN.usage_calc_estimated_tag), 'the dashboard card says estimated');
});

test('a read week carries no estimate marker', () => {
  const s = loadApp();
  const html = run(s, 'window.usageCalcFrameHtml(_body, {})', workedExampleBody());
  assert.ok(!html.includes('usage-calc-estimated'));
  assert.ok(!html.includes(EN.usage_calc_estimated_tag));
});

test('an estimate is not prefilled into the reading form', () => {
  const s = loadApp();
  const field = body => /id="usageCalcPercent"[^>]*value="([^"]*)"/.exec(run(s, '_usageCalcReadingForm(_body, {now: 0})', body))[1];
  assert.equal(field(estimatedBody()), '');
  assert.notEqual(field(workedExampleBody()), '');
});

test('the burn guard card marks an estimated projection', () => {
  const s = loadApp();
  const status = estimated => ({ enabled: true, state: 'idle', level: 0, last_projection: { projected: 40, line: 0, margin: 5, over_by: -55, estimated } });
  assert.ok(run(s, 'window.burnGuardCardHtml(_body, {}, {})', status(true)).includes('data-burn-guard="estimated"'));
  assert.ok(!run(s, 'window.burnGuardCardHtml(_body, {}, {})', status(false)).includes('data-burn-guard="estimated"'));
});

test('the estimate strings exist in the five languages', () => {
  for (const l of LOCALES) {
    const b = bundle(l);
    assert.ok(b.usage_calc_estimated && b.usage_calc_estimated_tag, l);
  }
});
