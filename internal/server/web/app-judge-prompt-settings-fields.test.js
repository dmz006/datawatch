// Pins the two judge prompt delivery fields on the Settings > General >
// Automata card: both render with a label, a help entry and a '?' link to the
// judge settings section of the Automation tab how-to, and their locale keys
// exist in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'autonomous.judge_prompt_file_threshold_lines',
  'autonomous.judge_delivery_wait_per_thousand_lines_seconds',
];

function load() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function fields() {
  return JSON.parse(vm.runInContext(
    'JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === "autonomous").fields)', load()));
}

function bundle(l) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
}

test('both judge prompt fields render next to the judge size setting', () => {
  const fs_ = fields();
  const keys = fs_.map(f => f.key);
  const size = keys.indexOf('autonomous.judge_size_threshold_lines');
  for (const k of KEYS) {
    const f = fs_.find(x => x.key === k);
    assert.ok(f, k + ' missing from autonomous card fields');
    assert.equal(f.type, 'number');
    assert.equal(f.min, 0);
    assert.ok(f.labelKey && f.helpKey, k + ' needs labelKey and helpKey');
    assert.ok(Math.abs(keys.indexOf(k) - size) <= 4, k + ' is not near the judge size setting');
  }
});

test('both fields have a ? help entry linking the judge settings section', () => {
  const sandbox = load();
  for (const k of KEYS) {
    const f = fields().find(x => x.key === k);
    assert.equal(f.helpDocs, 'howto/automation-tab.md#judge-settings');
    sandbox.__f = f;
    const html = vm.runInContext('settingsHelpLink(__f)', sandbox);
    assert.ok(html.includes('/diagrams.html#docs/howto/automation-tab.md#judge-settings'), 'no ? link for ' + k);
    assert.ok(html.includes('>?</a>'));
  }
  const doc = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'automation-tab.md'), 'utf8');
  assert.ok(/^## Judge settings$/m.test(doc), 'docs section "Judge settings" missing');
});

test('label, help, error and link-label keys exist in all five locales', () => {
  const keys = new Set(['settings_judge_help_link_label']);
  for (const k of KEYS) {
    const f = fields().find(x => x.key === k);
    keys.add(f.labelKey); keys.add(f.helpKey);
    assert.equal(f.helpLabelKey, 'settings_judge_help_link_label');
  }
  keys.add('settings_judge_prompt_file_threshold_error');
  keys.add('settings_judge_delivery_wait_error');
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + l);
    }
  }
});

test('labels carry no tracker identifiers or PRD wording', () => {
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of Object.keys(b).filter(x => /^settings_judge_(prompt_file|delivery_wait|help_link)/.test(x))) {
      assert.ok(!/\b(BL|PRD)\s?-?\d*\b/.test(b[k]), k + ' in ' + l + ' has tracker/PRD wording');
    }
  }
});
