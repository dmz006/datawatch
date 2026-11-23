// Gate repair settings guard.
//
// The Gate repair card on Settings, Automata carries one key
// (autonomous.gate_repair.mode). This test keeps the four locale keys in step
// across the five locale bundles and checks the settings array carries the key
// with the three modes, reporting the default.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const dir = __dirname;
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'settings_gate_repairs',
  'settings_gate_repairs_hint',
  'settings_gate_repairs_mode',
  'settings_gate_repairs_mode_help',
];

const bundles = {};
for (const lang of LANGS) {
  bundles[lang] = JSON.parse(fs.readFileSync(path.join(dir, 'locales', lang + '.json'), 'utf8'));
}

test('every bundle carries the four Gate repair settings keys, non-empty', () => {
  for (const lang of LANGS) {
    for (const key of KEYS) {
      const value = bundles[lang][key];
      assert.equal(typeof value, 'string', lang + '.json is missing string key ' + key);
      assert.ok(value.trim() !== '', lang + '.json has an empty value for ' + key);
    }
  }
});

test('the non-English bundles carry real translations, not English copies', () => {
  const same = [];
  for (const lang of LANGS) {
    if (lang === 'en') continue;
    for (const key of KEYS) {
      if (bundles[lang][key] === bundles.en[key]) same.push(lang + ':' + key);
    }
  }
  assert.deepEqual(same, [], 'untranslated Gate repair keys: ' + same.join(', '));
});

test('app.js carries the gate repair mode setting with the three modes and the report default', () => {
  const source = fs.readFileSync(path.join(dir, 'app.js'), 'utf8');
  const m = source.match(
    /\{\s*key:\s*'autonomous\.gate_repair\.mode'[^}]*type:\s*'select'[^}]*options:\s*\[([^\]]*)\][^}]*default:\s*'(\w+)'\s*\}/
  );
  assert.ok(m, 'app.js settings does not carry autonomous.gate_repair.mode as a select');
  const options = m[1].split(',').map(s => s.trim().replace(/^'(.*)'$/, '$1'));
  assert.deepEqual(options, ['off', 'report', 'on']);
  assert.equal(m[2], 'report');
});
