// Pins the Settings > General > Autonomous row for autonomous.drift_mode
// (block | warn, default block) and its label key in all five locale
// bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];

function driftField() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.drift_mode') || null)",
    sandbox);
  return JSON.parse(json);
}

test('autonomous settings expose drift_mode as a block/warn select defaulting to block', () => {
  const f = driftField();
  assert.ok(f, 'autonomous.drift_mode missing from the autonomous settings card');
  assert.equal(f.type, 'select');
  assert.deepEqual(f.options, ['block', 'warn']);
  assert.equal(f.default, 'block');
  assert.equal(f.labelKey, 'settings_drift_mode');
});

test('settings_drift_mode exists, non-empty, in all five locales without a tracker id', () => {
  for (const l of LOCALE_LIST) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    assert.ok(typeof j.settings_drift_mode === 'string' && j.settings_drift_mode.length > 0, l + ': settings_drift_mode missing');
    assert.ok(!/\b(BL|F)\d+\b/.test(j.settings_drift_mode), l + ': tracker id in label');
  }
});
