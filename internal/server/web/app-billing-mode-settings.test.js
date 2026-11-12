// Pins the billing mode select in the LLM entry editor: the field, its "?"
// help link to howto/cost-report.md, and the five locale keys in every bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'billing_mode_label',
  'billing_mode_option_api',
  'billing_mode_option_allowance',
  'billing_mode_option_local',
  'billing_mode_help',
];
const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

test('LLM editor has a billing mode select with the three options', () => {
  assert.match(src, /id="fe_llm_billing_mode"/);
  for (const v of ['api', 'allowance', 'local']) {
    assert.ok(src.includes("l.billing_mode === '" + v + "'"), 'option ' + v);
  }
  for (const k of KEYS.slice(0, 4)) assert.ok(src.includes("t('" + k + "')"), k);
  assert.match(src, /out\.billing_mode = billingMode/);
});

test('billing mode help links to the cost report howto', () => {
  assert.match(src, /settingsHelpLink\(\{ helpDocs: 'howto\/cost-report\.md', helpKey: 'billing_mode_help' \}\)/);
  assert.ok(fs.existsSync(path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'cost-report.md')));
});

test('every locale carries every billing mode key', () => {
  for (const loc of LOCALES) {
    const b = bundle(loc);
    for (const k of KEYS) {
      assert.equal(typeof b[k], 'string', loc + ' ' + k);
      assert.ok(b[k].trim().length > 0, loc + ' ' + k);
    }
  }
});

test('billing mode text avoids the internal term', () => {
  for (const loc of LOCALES) assert.ok(!/PRD/.test(bundle(loc).billing_mode_help), loc);
});
