// tracing.retention_days label + hint — settings field and the five locale
// bundles, plus the render wiring in app.js. The field states the rule (a
// trace is kept while its session exists; 0 = default = no age-based
// pruning; above 0 prunes traces older than that many days, 0 to 3650).
// No daemon, session or tmux is started.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'es', 'fr', 'de', 'ja'];
const LABEL_KEY = 'settings_tracing_retention_days';
const HINT_KEY = 'settings_tracing_retention_days_hint';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

test('all five locale bundles carry the retention label and hint', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.ok(typeof b[LABEL_KEY] === 'string' && b[LABEL_KEY].length > 0, `missing label in ${lang}`);
    assert.ok(typeof b[HINT_KEY] === 'string' && b[HINT_KEY].length > 0, `missing hint in ${lang}`);
    // No stale "0 = default" meaning left: the label must mention the 0..3650
    // range, so an operator does not read 0 as a hidden default of days.
    assert.ok(b[LABEL_KEY].includes('3650'), `label in ${lang} must name the 0..3650 range: ${b[LABEL_KEY]}`);
    assert.ok(b[HINT_KEY].includes('3650'), `hint in ${lang} must name 3650: ${b[HINT_KEY]}`);
  }
});

test('the English hint states the retention rules', () => {
  const hint = bundle('en')[HINT_KEY];
  assert.ok(/session exists/.test(hint), `hint says a trace is kept while its session exists: ${hint}`);
  assert.ok(/0 \(default\)/.test(hint), `hint names 0 as the default: ${hint}`);
  assert.ok(/never prune[sd]? by age|never pruned by age/i.test(hint), `hint: 0 = never prune by age: ${hint}`);
  assert.ok(/older than that many days/i.test(hint), `hint: above 0 prunes traces older than that many days: ${hint}`);
  assert.ok(/3650/.test(hint), `hint names the 0..3650 range: ${hint}`);
});

test('app.js wires the tracing.retention_days field to its label and hint keys', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/\{ key: 'tracing\.retention_days'[^}]*\}/);
  assert.ok(m, 'tracing.retention_days settings field not found');
  assert.ok(m[0].includes(`labelKey: '${LABEL_KEY}'`), `field must point at ${LABEL_KEY}: ${m[0]}`);
  assert.ok(m[0].includes(`hintKey: '${HINT_KEY}'`), `field must point at ${HINT_KEY}: ${m[0]}`);
  // The field's own English fallback label must be the updated wording.
  assert.ok(m[0].includes('3650'), 'fallback label must name the 0..3650 range');
  assert.ok(!/0 = default'\)/.test(m[0]), 'fallback label must not keep the stale 0 = default wording');
});
