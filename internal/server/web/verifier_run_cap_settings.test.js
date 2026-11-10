// Settings > Automaton: the autonomous.verifier_max_run_seconds entry (the
// verifier run cap). Covers that the entry sits in the autonomous card with
// min 0, its label/help keys, the how-to link, and that all five locale
// bundles carry both of its keys.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEY = 'autonomous.verifier_max_run_seconds';
const LABEL_KEY = 'settings_verifier_max_run_seconds';
const HELP_KEY = 'settings_verifier_max_run_seconds_help';

const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function autonomousFields(sb) {
  return JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous').fields)", sb));
}

test('verifier run cap entry is in the autonomous settings with min 0', () => {
  const fields = autonomousFields(loadApp());
  const idx = fields.findIndex(f => f.key === KEY);
  assert.ok(idx >= 0, KEY + ' missing from the autonomous settings table');
  const entry = fields[idx];
  assert.equal(entry.type, 'number');
  assert.equal(entry.min, 0);
  assert.equal(entry.placeholder, '2700');
  assert.equal(entry.labelKey, LABEL_KEY);
  assert.equal(entry.helpKey, HELP_KEY);
  assert.equal(entry.helpDocs, 'howto/automaton-verifier-run-cap.md');
  assert.match(entry.label, /Automaton/);
  assert.doesNotMatch(entry.label, /PRD/);
  // sits right after the no-progress timeout entry
  assert.equal(fields[idx - 1].key, 'autonomous.verifier_no_progress_timeout_seconds');
});

test('all five locale bundles carry both verifier run cap keys', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of [LABEL_KEY, HELP_KEY]) {
      assert.equal(typeof b[k], 'string', lang + ' missing ' + k);
      assert.ok(b[k].trim().length > 0, lang + ' has empty ' + k);
    }
  }
});

test('the English label and help name the Automaton, not the PRD', () => {
  const en = bundle('en');
  assert.match(en[LABEL_KEY], /Automaton/);
  assert.doesNotMatch(en[LABEL_KEY], /PRD/);
});
