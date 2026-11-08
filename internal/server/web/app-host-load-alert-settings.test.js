// Pins the host load check fields in Settings > General > Autonomous and
// their strings in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function field(key) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox));
}

test('HostLoad: mode is an off, alert or reap select defaulting to alert', () => {
  const f = field('autonomous.host_load_alert.mode');
  assert.ok(f, 'mode field declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(f.options, ['off', 'alert', 'reap']);
  assert.equal(f.default, 'alert');
  assert.equal(f.labelKey, 'settings_host_load_alert_mode');
});

test('HostLoad: multiple is a non-negative number defaulting to 1.5', () => {
  const f = field('autonomous.host_load_alert.multiple');
  assert.ok(f, 'multiple field declared');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0);
  assert.equal(f.default, '1.5');
  assert.equal(f.labelKey, 'settings_host_load_alert_multiple');
});

test('HostLoad: minutes is a non-negative whole number defaulting to 10', () => {
  const f = field('autonomous.host_load_alert.minutes');
  assert.ok(f, 'minutes field declared');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0);
  assert.equal(f.step, 1);
  assert.equal(f.default, '10');
  assert.equal(f.labelKey, 'settings_host_load_alert_minutes');
});

for (const key of ['settings_host_load_alert_mode', 'settings_host_load_alert_multiple', 'settings_host_load_alert_minutes']) {
  for (const lang of LOCALE_LIST) {
    test(`HostLoad: locale ${lang} defines ${key}`, () => {
      const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
      const v = bundle[key];
      assert.ok(typeof v === 'string' && v.trim().length > 0, 'key present and non-empty');
      assert.ok(!/\bPRDs?\b/.test(v), 'never says PRD');
      assert.ok(!/\b(?:BL|B|F|S)\d{1,6}\b/.test(v), 'no tracker IDs');
      if (lang !== 'en') {
        // English placeholder until the app translation lands (AGENT.md rule: ship EN
        // while the translation is pending, never a half-translated value).
        assert.equal(v, JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'))[key], 'English placeholder while the app translation is pending');
      }
    });
  }
}
