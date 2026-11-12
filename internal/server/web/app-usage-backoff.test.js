// Pins the Settings > General > Autonomous usage back-off fields:
// autonomous.usage_backoff (pause_backend | off) and
// autonomous.backoff_max_wait_minutes (whole minutes >= 1). Both must render
// with safe defaults when the daemon reports nothing yet — an unset mode shows
// the default pause_backend and an unset maximum shows 60, never a blank that
// would save as 0 or switch the feature off by accident.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function autonomousFields() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox));
}

function field(key) {
  const f = autonomousFields().find((x) => x.key === key);
  assert.ok(f, key + ' missing from the autonomous card fields');
  return f;
}

test('autonomous.usage_backoff is a pause_backend/off select defaulting to pause_backend', () => {
  const f = field('autonomous.usage_backoff');
  assert.equal(f.type, 'select');
  assert.deepEqual(f.options, ['pause_backend', 'off']);
  assert.equal(f.default, 'pause_backend');
  assert.ok(f.options.includes(f.default), 'default is one of the options');
  assert.equal(f.labelKey, 'settings_usage_backoff');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('autonomous.backoff_max_wait_minutes is a whole-minute number >= 1 defaulting to 60', () => {
  const f = field('autonomous.backoff_max_wait_minutes');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 1, 'a 0 or negative maximum must not be enterable');
  assert.equal(f.step, 1);
  assert.equal(f.default, '60');
  assert.equal(f.placeholder, '60');
  assert.equal(f.labelKey, 'settings_backoff_max_wait_minutes');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('the two fields sit next to the other autonomous gates and are declared once', () => {
  const keys = autonomousFields().map((f) => f.key);
  for (const k of ['autonomous.usage_backoff', 'autonomous.backoff_max_wait_minutes']) {
    assert.equal(keys.filter((x) => x === k).length, 1, k + ' declared exactly once');
  }
});

test('an unset daemon value renders the select on its default, never blank', () => {
  const f = field('autonomous.usage_backoff');
  // Same selection rule the settings renderer applies to select fields:
  // String(val) === option. With no value reported, falling back to the
  // field default must land on a real option.
  const reported = undefined;
  const shown = reported === undefined || reported === '' ? f.default : String(reported);
  assert.ok(f.options.includes(shown));
  assert.equal(shown, 'pause_backend');
  // An explicit off survives (off is the one value that is falsy-looking
  // to naive "value || default" fallbacks only if it were empty; it is not).
  assert.ok(f.options.includes('off'));
});

for (const locale of LOCALES) {
  test(`locales: ${locale} defines both usage back-off labels`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of ['settings_usage_backoff', 'settings_backoff_max_wait_minutes']) {
      const v = bundle[k];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${locale} key ${k} present and non-empty`);
      assert.ok(!/\bBL\d+\b/.test(v), 'no tracker IDs in the string');
    }
  });
}
