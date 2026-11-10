// Dashboard "Smoke Run" card (id 'smoke').
//
// The card shows real smoke runs: a top list of run envelopes (each with a
// status glyph, a SMOKE/E2E tag, its version/id, pass/fail counts and a
// progress bar) and, once a run is expanded, a detail pane with a progress
// bar, filter pills and one row per section. It reads GET /api/smoke/progress
// (envelope list) and GET /api/smoke/progress/{id} (section detail).
//
// Through its DASH_CARD_FEATURES['smoke'] active() gate the card is dropped
// from the grid while there are no runs recorded, and a single localized
// "off" line (with a link to the card reference) is rendered in its place —
// the same pattern the other feature cards use.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, vm, makeStubElement } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

function makeEnv() {
  const byId = { dashSmokeCard: makeStubElement() };
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  // Load the English bundle so t() returns real text, not keys.
  sandbox.__en = EN;
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  return { sandbox, byId };
}

function evalIn(sandbox, code) {
  return vm.runInContext(code, sandbox);
}

// Set the card's envelope list the way the /api/smoke/progress poll does.
function setEnvelopes(sandbox, envelopes) {
  sandbox.__env = envelopes;
  vm.runInContext(`_dash._smokeEnvelopes = __env;`, sandbox);
}

test('smoke card: active() gate is off with no runs, on with one', () => {
  const { sandbox } = makeEnv();

  assert.equal(evalIn(sandbox, `DASH_CARD_FEATURES['smoke'].active()`), false,
    'no runs yet → the card must be reported inactive');

  setEnvelopes(sandbox, [
    { id: 'run-1', type: 'smoke', pass: 5, fail: 1, active: false },
  ]);
  assert.equal(evalIn(sandbox, `DASH_CARD_FEATURES['smoke'].active()`), true,
    'one recorded run → the card must be reported active');
});

test('smoke card: off body shows the localized reason, not an empty render', () => {
  const { sandbox } = makeEnv();
  const feat = evalIn(sandbox, `_dashCardFeature('smoke')`);
  assert.ok(feat, 'the smoke card must have a feature gate');
  const body = evalIn(sandbox, `_dashCardOffBody(_dashCardFeature('smoke'))`);
  assert.match(body, /No smoke runs recorded on this machine/, 'off line must be the localized reason');
});

test('smoke card: a real run renders as a list row with its version and counts', () => {
  const { sandbox, byId } = makeEnv();
  setEnvelopes(sandbox, [
    { id: 'run-1', type: 'smoke', version: 'v9.99.0', pass: 5, fail: 1, skip: 0,
      active: false, pct: 75, updated_at: '2026-11-09T00:00:00Z' },
  ]);
  vm.runInContext(`_dash._smokeExpandedId = null; _dash._smokeExpandedData = null; _dashRenderSmoke();`, sandbox);

  const html = byId.dashSmokeCard.innerHTML;
  assert.match(html, /SMOKE/, 'row should carry the SMOKE type tag');
  assert.match(html, /v9\.99\.0/, 'row should show the run version');
  assert.match(html, /5\s*✓/, 'row should show the pass count');
  assert.match(html, /1\s*✗/, 'row should show the fail count');
});

test('smoke card: expanding a run shows its detail section rows', () => {
  const { sandbox, byId } = makeEnv();
  setEnvelopes(sandbox, [
    { id: 'run-1', type: 'smoke', version: 'v9.99.0', pass: 2, fail: 1, active: false },
  ]);
  sandbox.__detail = {
    pass: 2, fail: 1, skip: 0, total: 8, active: false,
    sections: [
      { id: 'build', name: 'Build', result: 'pass' },
      { id: 'api', name: 'API', result: 'pass' },
      { id: 'ui', name: 'UI', result: 'fail' },
    ],
  };
  vm.runInContext(
    `_dash._smokeExpandedId = 'run-1'; _dash._smokeExpandedData = __detail; _dashRenderSmoke();`,
    sandbox,
  );

  const html = byId.dashSmokeCard.innerHTML;
  assert.match(html, /❌ Failed/, 'a run with a failure should render its Failed banner');
  assert.match(html, /\d+\/\d+/, 'detail should show a done/total progress figure');
  assert.match(html, /Build/, 'detail should list the Build section');
  assert.match(html, /UI/, 'detail should list the UI section');
  assert.match(html, /pending/, 'detail should surface the pending count');
});
