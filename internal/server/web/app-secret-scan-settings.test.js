'use strict';
// Static checks for the pre-push secret scan settings fields
// (git.secret_scan.*) in app.js and their locale strings.
// Pure static-file test: no browser, no network, no daemon.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const lines = src.split('\n');

const FIELDS = [
  { key: 'git.secret_scan.mode', labelKey: 'settings_git_secret_scan_mode', helpKey: 'settings_git_secret_scan_mode_help' },
  { key: 'git.secret_scan.detectors', labelKey: 'settings_git_secret_scan_detectors', helpKey: 'settings_git_secret_scan_detectors_help' },
  { key: 'git.secret_scan.allow_list.paths', labelKey: 'settings_git_secret_scan_allow_paths', helpKey: 'settings_git_secret_scan_allow_paths_help' },
  { key: 'git.secret_scan.allow_list.fingerprints', labelKey: 'settings_git_secret_scan_allow_fingerprints', helpKey: 'settings_git_secret_scan_allow_fingerprints_help' },
];

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

// Extract the single app.js line defining a settings entry by its `key: '...'`.
function entryLine(key) {
  const marker = `key: '${key}'`;
  const idx = lines.findIndex((l) => l.includes(marker));
  assert.ok(idx >= 0, `app.js must define a settings entry with ${marker}`);
  return lines[idx];
}

// Pull a `name: value` fragment out of the entry line without evaluating app.js.
function fragment(line, name) {
  const m = line.match(new RegExp(`${name}:\\s*(\\[.*?\\]|'[^']*'|true|false)`));
  return m ? m[1] : null;
}

test('app.js defines the four git.secret_scan settings entries with the right keys and types', () => {
  for (const f of FIELDS) {
    const line = entryLine(f.key);
    assert.strictEqual(fragment(line, 'labelKey'), `'${f.labelKey}'`, `${f.key}: labelKey`);
    assert.strictEqual(fragment(line, 'helpKey'), `'${f.helpKey}'`, `${f.key}: helpKey`);
    // Both keys follow the settings_git_secret_scan_* naming; the help key is
    // the label key with a _help suffix.
    assert.match(f.labelKey, /^settings_git_secret_scan_[a-z0-9_]+$/, `${f.key}: labelKey naming`);
    assert.strictEqual(f.helpKey, `${f.labelKey}_help`, `${f.key}: helpKey naming`);
  }

  const modeLine = entryLine('git.secret_scan.mode');
  assert.strictEqual(fragment(modeLine, 'type'), `'select'`, 'mode entry must be a select');
  assert.strictEqual(fragment(modeLine, 'options'), "['off', 'shadow', 'enforce']", 'mode options must be exactly off, shadow, enforce');

  for (const f of FIELDS.slice(1)) {
    const line = entryLine(f.key);
    assert.strictEqual(fragment(line, 'type'), `'text'`, `${f.key}: must be a text field`);
    assert.strictEqual(fragment(line, 'csv'), 'true', `${f.key}: must be csv: true`);
  }
});

test('labelKey and helpKey exist with non-empty strings in all five locale bundles', () => {
  const bundles = {};
  for (const loc of LOCALES) {
    const file = path.join(__dirname, 'locales', `${loc}.json`);
    bundles[loc] = JSON.parse(fs.readFileSync(file, 'utf8'));
    for (const f of FIELDS) {
      for (const k of [f.labelKey, f.helpKey]) {
        const v = bundles[loc][k];
        assert.ok(typeof v === 'string' && v.trim().length > 0,
          `locales/${loc}.json: ${k} must be a non-empty string`);
      }
    }
  }
});

// Translation rule (applied to label keys only): a non-English label must not
// be a byte-identical copy of the English label, UNLESS the English text is a
// proper noun or a technical token list (e.g. a comma-separated list of
// detector names) that would legitimately stay the same in every locale.
function isProperNounOrTokenList(s) {
  // A technical token list: comma-separated snake_case / dotted identifiers.
  return /^[A-Za-z0-9_.]+(,\s*[A-Za-z0-9_.]+)+$/.test(s.trim());
}

test('non-English label keys are translated (not byte-identical copies of English)', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const loc of LOCALES.slice(1)) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${loc}.json`), 'utf8'));
    for (const f of FIELDS) {
      const enLabel = en[f.labelKey];
      if (isProperNounOrTokenList(enLabel)) continue; // technical token list: identical is fine
      assert.notStrictEqual(bundle[f.labelKey], enLabel,
        `locales/${loc}.json: ${f.labelKey} must be translated, not a copy of the English text`);
    }
  }
});

test('the English mode help text names all three modes', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const help = en.settings_git_secret_scan_mode_help;
  for (const mode of ['shadow', 'enforce', 'off']) {
    assert.match(help, new RegExp(`\\b${mode}\\b`),
      `English mode help must mention the '${mode}' mode`);
  }
});

test('no secret-scan label or help string uses the word PRD in any bundle', () => {
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${loc}.json`), 'utf8'));
    for (const f of FIELDS) {
      for (const k of [f.labelKey, f.helpKey]) {
        // Case-sensitive whole word.
        assert.doesNotMatch(bundle[k], /\bPRD\b/,
          `locales/${loc}.json: ${k} must not contain the word 'PRD'`);
      }
    }
  }
});
