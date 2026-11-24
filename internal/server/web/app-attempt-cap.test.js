// Attempt cap — PWA side.
//
// window.attemptCapTaskBadgesHtml(task, cap) shows the task's total attempt
// count, a badge while it is held at the cap and the wait before its next
// attempt. The Autonomous settings group carries the four
// autonomous.attempt_cap.* fields, docsLinkForKey maps them to the glossary
// section, the project policy editor carries the attempt_cap field, and every
// attempt_cap_ and settings_attempt_cap_ locale key is in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function badges(task, cap) {
  const sandbox = loadApp();
  sandbox._task = task;
  sandbox._cap = cap;
  return vm.runInContext('window.attemptCapTaskBadgesHtml(_task, _cap)', sandbox);
}

test('a task row shows the attempts started, with the cap when it is known', () => {
  assert.ok(badges({ attempts_total: 4 }, 6).includes('Attempts started: 4 of 6'));
  assert.ok(badges({ attempts_total: 4 }, 0).includes('Attempts started: 4<'));
});

test('a task with no attempts started and no hold gets no badge', () => {
  assert.equal(badges({ status: 'pending' }, 6), '');
  assert.equal(badges(null, 6), '');
});

test('a task held at the cap shows the held label', () => {
  const html = badges({ status: 'review_needed', attempts_total: 6, attempt_cap_held: true }, 6);
  assert.ok(html.includes('Held at the attempt cap'), html);
  assert.ok(html.includes('attempt-cap-held'), html);
});

test('a wait before the next attempt is shown only while it is in the future', () => {
  const later = new Date(Date.now() + 10 * 60 * 1000).toISOString();
  const past = new Date(Date.now() - 10 * 60 * 1000).toISOString();
  assert.ok(badges({ attempts_total: 3, attempt_not_before: later }, 6).includes('Next attempt not before'));
  assert.ok(!badges({ attempts_total: 3, attempt_not_before: past }, 6).includes('Next attempt not before'));
});

function autonomousFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('autonomous.attempt_cap.')))",
    sandbox));
}

test('the Autonomous settings group has the four attempt_cap fields with the right types', () => {
  const fields = autonomousFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  assert.equal(fields.length, 4, fields.map(f => f.key).join(','));
  assert.equal(byKey['autonomous.attempt_cap.enabled'].type, 'toggle');
  assert.equal(byKey['autonomous.attempt_cap.max_total'].type, 'number');
  assert.equal(byKey['autonomous.attempt_cap.max_total'].min, 1);
  assert.equal(byKey['autonomous.attempt_cap.free_attempts'].type, 'number');
  assert.equal(byKey['autonomous.attempt_cap.backoff_minutes'].csv, true);
  for (const f of fields) {
    assert.ok(f.labelKey && f.hintKey && f.docsKey, 'every field has a label, a description and a docs link: ' + f.key);
  }
});

test('docsLinkForKey maps the attempt_cap keys to the glossary section', () => {
  const sandbox = loadApp();
  for (const key of ['autonomous.attempt_cap.max_total', 'settings_attempt_cap_enabled', 'attempt_cap_held_label', 'project_policy_attempt_cap']) {
    sandbox._key = key;
    assert.ok(vm.runInContext('docsLinkForKey(_key)', sandbox).includes('datawatch-definitions.md#attempt-cap'), key);
  }
});

test('the project policy editor carries the attempt_cap field', () => {
  const sandbox = loadApp();
  const f = JSON.parse(vm.runInContext(
    "JSON.stringify(PROJECT_POLICY_GROUPS.flatMap(g => g.fields).find(x => x.key === 'attempt_cap') || null)", sandbox));
  assert.deepEqual(f, { key: 'attempt_cap', kind: 'int', min: 1 });
});

test('every attempt cap locale key is in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/['"]((?:settings_)?attempt_cap_[a-z_]+|project_policy_attempt_cap)['"]/g)) used.add(m[1]);
  assert.ok(used.size >= 10, 'expected the attempt cap keys in app.js, found ' + used.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) assert.ok(typeof b[k] === 'string' && b[k] !== '', `${lang} lacks ${k}`);
  }
});

test('no attempt cap string carries PRD wording or a tracker id', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const [k, v] of Object.entries(b)) {
      if (!/attempt_cap/.test(k)) continue;
      assert.ok(!/\bPRDs?\b/.test(v) && !/\b(BL|F)\d+/.test(v), `${lang}.${k}: ${v}`);
    }
  }
});
