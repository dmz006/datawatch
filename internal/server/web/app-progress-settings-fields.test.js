// Pins the Settings > General > Autonomous progress fields: each
// autonomous.progress key is declared with a label key that exists, non-empty
// and free of internal terms, in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

const PROGRESS_FIELDS = {
  'autonomous.progress.lanes': { labelKey: 'settings_progress_lanes', type: 'number' },
  'autonomous.progress.inactive_gap_hours': { labelKey: 'settings_progress_inactive_gap_hours', type: 'number' },
  'autonomous.progress.backlog_path': { labelKey: 'settings_progress_backlog_path', type: 'text' },
  'autonomous.progress.since': { labelKey: 'settings_progress_since', type: 'text' },
  'autonomous.progress.snapshot_retention_days': { labelKey: 'settings_progress_snapshot_retention_days', type: 'number' },
};

function autonomousFields() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox);
  return JSON.parse(json);
}

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

for (const [key, want] of Object.entries(PROGRESS_FIELDS)) {
  test(`GENERAL_CONFIG_FIELDS: ${key} is declared with a localized label`, () => {
    const field = autonomousFields().find((f) => f.key === key);
    assert.ok(field, `${key} field is declared in the autonomous section`);
    assert.equal(field.type, want.type);
    assert.equal(field.labelKey, want.labelKey);
    assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
    if (want.type === 'number') assert.equal(field.min, 0, 'numeric progress fields start at 0');
    for (const lang of LOCALES) {
      const v = bundle(lang)[want.labelKey];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${want.labelKey} missing or empty in ${lang}`);
      assert.ok(!/\bPRDs?\b/.test(v), `${lang}: no internal term in ${want.labelKey}`);
      assert.ok(!/\b(BL|B|F|S)\d+\b/.test(v), `${lang}: no tracker ID in ${want.labelKey}`);
    }
  });
}

test('every settings_progress_ locale key is used by a declared field', () => {
  const used = new Set(Object.values(PROGRESS_FIELDS).map((w) => w.labelKey));
  for (const lang of LOCALES) {
    const keys = Object.keys(bundle(lang)).filter((k) => k.startsWith('settings_progress_'));
    assert.deepEqual(keys.sort(), [...used].sort(), `${lang} progress keys match the declared fields`);
  }
});
