// Vault unreachable-alert threshold field in Settings — PWA side.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const KEY = 'secrets.vault.unreachable_alert_after_seconds';
const LABEL = 'settings_secrets_vault_unreachable_alert_after_seconds';
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function field() {
  return vm.runInContext(
    `(GENERAL_CONFIG_FIELDS.flatMap(sec => sec.fields).find(f => f.key === '${KEY}'))`, loadApp());
}

test('the field is a numeric input that uses the locale keys', () => {
  const f = field();
  assert.ok(f, 'field present in the settings field list');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0);
  assert.equal(f.placeholder, '120');
  assert.equal(f.labelKey, LABEL);
  assert.equal(f.helpKey, LABEL + '_help');
});

test('saving posts the right config key through the config save path', () => {
  const f = field();
  assert.ok(f.onchange.includes(`'${KEY}'`));
  assert.ok(f.onchange.includes('savePromptNumberSetting'));
  const s = loadApp();
  s.__calls = [];
  vm.runInContext(`saveGeneralConfigPut = function (k, v) { __calls.push([k, v]); };`, s);
  vm.runInContext(`savePromptNumberSetting('${KEY}', '0', 0)`, s);
  vm.runInContext(`savePromptNumberSetting('${KEY}', '300', 0)`, s);
  assert.deepEqual(JSON.parse(JSON.stringify(s.__calls)), [[KEY, 0], [KEY, 300]]);
});

test('all five locales carry the strings; help states default 120 and 0 disables', () => {
  for (const l of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of ['settings_secrets_vault', LABEL, LABEL + '_help']) {
      assert.ok(b[k] && b[k].length > 0, `${l} missing ${k}`);
    }
    assert.ok(b[LABEL + '_help'].includes('120'), `${l} help mentions 120`);
    assert.ok(/\b0\b/.test(b[LABEL + '_help']), `${l} help mentions 0`);
  }
});
