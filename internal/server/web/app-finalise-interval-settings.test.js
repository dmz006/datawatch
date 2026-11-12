// Pins the session finalise interval field in Settings > General > Sessions:
// it is a number field with the English-label fallback, a locale label key and
// a help key, and both keys resolve in the English locale file.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const KEY = 'session.finalise_interval_minutes';
const LOCALE_DIR = path.join(__dirname, 'locales');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function findField(sandbox) {
  const json = vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.reduce((acc, s) => acc.concat(s.fields || []), [])" +
    ".find(f => f.key === '" + KEY + "') || null)",
    sandbox);
  return JSON.parse(json);
}

function english() {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
}

test('finalise interval field is a number field with its label key', () => {
  const field = findField(loadApp());
  assert.ok(field, KEY + ' must be present in GENERAL_CONFIG_FIELDS');
  assert.equal(field.type, 'number');
  assert.equal(field.labelKey, 'settings_finalise_interval');
  assert.equal(field.label, 'Session finalise interval (min)');
  assert.equal(field.placeholder, '10');
});

test('finalise interval field sits in the Sessions section', () => {
  const sandbox = loadApp();
  const id = vm.runInContext(
    "(GENERAL_CONFIG_FIELDS.find(s => (s.fields || []).some(f => f.key === '" + KEY + "')) || {}).id",
    sandbox);
  assert.equal(id, 'sess');
});

test('finalise interval help key resolves in the English locale file', () => {
  const field = findField(loadApp());
  assert.equal(field.helpKey, 'settings_finalise_interval_help');
  const en = english();
  assert.ok(en[field.labelKey], 'label key must exist in en.json');
  assert.ok(en[field.helpKey] && en[field.helpKey].length > 0, 'help key must exist in en.json');
});
