// Pins the daily pending-branch alert settings on the Settings card
// "Autonomous Automata planning": a toggle for autonomous.unlanded_alert_enabled
// (default on) and a number for autonomous.unlanded_alert_threshold, each with
// a label and help key that every locale bundle carries, with no PRD wording.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function findField(key) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

test('unlanded alert enabled is a toggle defaulting on', () => {
  const f = findField('autonomous.unlanded_alert_enabled');
  assert.ok(f, 'autonomous.unlanded_alert_enabled missing from the autonomous card');
  assert.equal(f.type, 'toggle');
  assert.equal(f.default, true);
  assert.equal(f.labelKey, 'settings_unlanded_alert_enabled');
  assert.equal(f.helpKey, 'settings_unlanded_alert_enabled_help');
});

test('unlanded alert threshold is a number with placeholder 3', () => {
  const f = findField('autonomous.unlanded_alert_threshold');
  assert.ok(f, 'autonomous.unlanded_alert_threshold missing from the autonomous card');
  assert.equal(f.type, 'number');
  assert.equal(f.placeholder, '3');
  assert.equal(f.labelKey, 'settings_unlanded_alert_threshold');
  assert.equal(f.helpKey, 'settings_unlanded_alert_threshold_help');
});

test('unlanded alert labels exist in every locale without PRD wording', () => {
  const keys = ['settings_unlanded_alert_enabled', 'settings_unlanded_alert_enabled_help',
    'settings_unlanded_alert_threshold', 'settings_unlanded_alert_threshold_help'];
  for (const locale of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, k + ' missing in ' + locale);
      assert.ok(!/\bPRDs?\b/.test(bundle[k]), k + ' uses PRD wording in ' + locale);
    }
  }
});
