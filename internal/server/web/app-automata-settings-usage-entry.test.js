// Settings -> Automata -> Usage reading and extra credit.
//
// The block reuses the usage page's reading and extra credit forms and save
// functions. This test checks that the panel renders both controls, that the
// saves still post to the same endpoints, that the usage settings sections are
// routed to the Automata group, and that every new locale key exists in all
// five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const appSrc = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const KEYS = [
  'settings_usage_entry_title',
  'settings_usage_entry_intro',
  'settings_usage_entry_saved',
  'settings_usage_entry_help',
];

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

test('the Settings block renders the reading and extra credit controls', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('window.settingsUsageEntryHtml', sandbox)(null, '');
  assert.match(html, /id="usageCalcPercent"/);
  assert.match(html, /id="usageCalcReadAt"/);
  assert.match(html, /usageCalcSubmitReading\(\)/);
  assert.match(html, /id="usageCalcExtra"/);
  assert.match(html, /usageCalcSaveExtraCredit\(\)/);
  assert.match(html, /id="usageCalcFormMsg"/);
});

test('the saves post to the same endpoints as the usage page', () => {
  assert.match(appSrc, /_usageCalcWrite\('POST', '\/api\/usage\/readings'/);
  assert.match(appSrc, /_usageCalcWrite\('PUT', '\/api\/usage\/extra-credit'/);
  assert.doesNotMatch(appSrc, /settingsUsageEntry[^\n]*fetch\('\/api\/usage\/readings/);
});

test('usage settings sections are routed to the Automata group with help', () => {
  assert.match(appSrc, /_automataIds = \[[^\]]*'usage_calculator'[^\]]*'usage_burn_guard'[^\]]*\]/);
  assert.match(appSrc, /id: 'usage_calculator', section: 'Usage calculator', docs: 'howto\/usage-calculator\.md'/);
  assert.match(appSrc, /id: 'usage_burn_guard', section: 'Usage burn guard', docs: 'howto\/burn-guard\.md'/);
  assert.match(appSrc, /settings_usage_entry_title: 'howto\/usage-calculator\.md'/);
  assert.match(appSrc, /settings_usage_entry_help: 'howto\/burn-guard\.md'/);
});

test('every new locale key exists in all five bundles without banned words', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, `${lang} missing ${k}`);
      assert.doesNotMatch(bundle[k], /\bPRD\b/);
    }
  }
});
