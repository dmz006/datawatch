// Burn guard → Settings page link — PWA side.
//
// The Resources "Usage & burn guard" card no longer hosts the burn guard's
// config controls. This file pins what replaced them: the card is read-only
// (no checkboxes, no number inputs, no save handlers), keeps a single
// "Burn guard settings" link that deep-links into the burn guard section of the
// Settings page on the Automata tab, and keeps its round "?" help entry.
// It does not touch the drawing of the status tile itself (see
// app-burn-guard.test.js).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const STATUS = {
  enabled: true, state: 'guarding', level: 1, rung: 'lower_inflight', line: 60, margin: 5,
  applied: ['lower_inflight'],
  last_projection: { projected: 72.5, line: 60, margin: 5, over_by: 7.5 },
  last_decision: { direction: 'climb', rung: 'lower_inflight', level: 1, previous: 0, reason: 'over the line' },
};
// config is accepted for backward compatibility but no longer drawn.
const CONFIG = {
  enabled: true, margin_points: 5, ladder: ['lower_inflight', 'route_pi_prompt'], step_back: true,
  retry_limit_claude: 2, ask_on_change: true, max_projection_gap_points: 40,
  effort_ceiling: { docs: 'low', final_gate: 'low' },
};

function card(s, status, config, opts) {
  s._st = status; s._cfg = config; s._opts = opts || {};
  return vm.runInContext('window.burnGuardCardHtml(_st, _cfg, _opts)', s);
}

test('the card has no editable controls: no checkboxes, no number inputs, no save handlers', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  assert.ok(!html.includes('<input'), 'no input elements at all');
  assert.ok(!html.includes('type="checkbox"'), 'no checkboxes');
  assert.ok(!html.includes('type="number"'), 'no number inputs');
  assert.ok(!html.includes('onchange='), 'no onchange handlers');
  for (const id of ['burnGuardEnabled', 'burnGuardMargin', 'burnGuardLadder', 'burnGuardStepBack', 'burnGuardAskOnChange', 'burnGuardRetryLimit', 'burnGuardMaxProjectionGap']) {
    assert.ok(!html.includes('id="' + id + '"'), 'no ' + id);
  }
  assert.ok(!html.includes('burnGuardSaveField'), 'no save-field handler');
  assert.ok(!html.includes('burnGuardSaveLadder'), 'no save-ladder handler');
  assert.ok(!html.includes('burnGuardMoveRung'), 'no move-rung handler');
  assert.ok(!html.includes('data-ceiling-kind="docs"'), 'no effort-ceiling row');
  // The read-only card still keeps the run-now action.
  assert.ok(html.includes('data-burn-guard="run"'), 'run now button');
});

test('the card offers a single localized "Burn guard settings" link, not the config fields', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  assert.ok(html.includes('data-burn-guard="settings-link"'), 'settings link element');
  assert.ok(html.includes('onclick="burnGuardOpenSettings()'), 'deep-link handler on the link');
  assert.ok(html.includes('Burn guard settings'), 'localized link label');
  // Exactly one such link; the old per-field labels are gone.
  assert.equal(html.split('data-burn-guard="settings-link"').length - 1, 1, 'one settings link');
  assert.ok(!html.includes('burnGuardEnabled'), 'no enabled checkbox label/id');
  assert.ok(!html.includes('burnGuardLadder'), 'no ladder control');
});

test('burnGuardOpenSettings deep-links into the burn guard section of the Settings page (Automata tab)', () => {
  const s = loadApp();
  s.__opened = null;
  vm.runInContext('_projectsOpenSettings = (sec, tab) => { __opened = [sec, tab]; return true; }', s);
  vm.runInContext('window.burnGuardOpenSettings()', s);
  assert.equal(s.__opened && s.__opened[0], 'usage_burn_guard', 'the burn guard settings section');
  assert.equal(s.__opened && s.__opened[1], 'automata', 'the Automata tab owns the section');
});

test('the card keeps a round help entry that links to the burn guard howto', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  assert.ok(html.includes('/diagrams.html#docs/howto/burn-guard.md'), 'help link points at the howto');
  assert.ok(html.includes('target="_blank" rel="noopener"'), 'help opens in a new tab without leaking the opener');
  assert.ok(html.includes('aria-label="help"'), 'help entry is labelled for assistive tech');
});
