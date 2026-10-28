// Pins the Settings > General > Autonomous field list to the real
// per-Automaton concurrency key. autonomous.max_parallel_tasks never had
// any effect and is deprecated; the PWA must expose
// autonomous.max_concurrent_tasks instead and offer no editable control
// for the dead key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function findField(sectionId, key) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === '" + sectionId + "') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

function autonomousKeys() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.map(f => f.key))",
    sandbox);
  return JSON.parse(json);
}

test('autonomous settings expose max_concurrent_tasks, not max_parallel_tasks', () => {
  const keys = autonomousKeys();
  assert.ok(keys.includes('autonomous.max_concurrent_tasks'), 'autonomous.max_concurrent_tasks missing: ' + keys.join(','));
  assert.ok(!keys.includes('autonomous.max_parallel_tasks'), 'deprecated autonomous.max_parallel_tasks still editable');
});

test('autonomous settings expose verification_timeout_seconds with an en label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.verification_timeout_seconds') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.verification_timeout_seconds missing from autonomous card fields');
  assert.equal(parsed.labelKey, 'settings_verification_timeout');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_verification_timeout === 'string' && en.settings_verification_timeout.length > 0,
    'settings_verification_timeout missing from en locale');
});

test('autonomous settings expose task_override_statuses as a csv text field with a label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.task_override_statuses') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.task_override_statuses missing from autonomous card fields');
  assert.equal(parsed.type, 'text');
  assert.ok(parsed.csv, 'autonomous.task_override_statuses must set csv: true');
  assert.equal(parsed.labelKey, 'settings_task_override_statuses');
});

test('settings_task_override_statuses is a non-empty string in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_task_override_statuses === 'string' && bundle.settings_task_override_statuses.length > 0,
      'settings_task_override_statuses missing or empty in ' + locale + ' locale');
  }
});

test('autonomous settings expose session_idle_timeout_seconds as a number with an en label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.session_idle_timeout_seconds') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.session_idle_timeout_seconds missing from autonomous card fields');
  assert.equal(parsed.type, 'number');
  assert.equal(parsed.labelKey, 'settings_session_idle_timeout');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_session_idle_timeout === 'string' && en.settings_session_idle_timeout.length > 0,
    'settings_session_idle_timeout missing from en locale');
});

// Pi worker observability settings (docs/operations.md "Pi worker
// observability"): the loop-detection window on the autonomous card and
// the global pi thinking level on its own card, rendered the same way as
// session_idle_timeout_seconds (label + labelKey + localized label).

test('autonomous settings expose loop_detect_turns as a number with a label key', () => {
  const parsed = findField('autonomous', 'autonomous.loop_detect_turns');
  assert.ok(parsed, 'autonomous.loop_detect_turns missing from autonomous card fields');
  assert.equal(parsed.type, 'number');
  assert.equal(parsed.labelKey, 'settings_loop_detect_turns');
  const en = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_loop_detect_turns === 'string' && en.settings_loop_detect_turns.length > 0,
    'settings_loop_detect_turns missing from en locale');
});

test('pi worker card exposes pi.thinking_level as a select with the valid levels', () => {
  const parsed = findField('piworker', 'pi.thinking_level');
  assert.ok(parsed, 'pi.thinking_level missing from the pi worker card fields');
  assert.equal(parsed.type, 'select');
  // Empty = don't pass --thinking (pi's model default); then the five
  // levels config.ValidatePiThinkingLevel accepts.
  for (const opt of ['', 'off', 'minimal', 'low', 'medium', 'high']) {
    assert.ok(parsed.options.includes(opt), 'pi.thinking_level select missing option: ' + JSON.stringify(opt));
  }
  assert.equal(parsed.labelKey, 'settings_pi_thinking_level');
});

test('pi worker observability labels are non-empty strings in all five locale bundles', () => {
  for (const locale of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_loop_detect_turns === 'string' && bundle.settings_loop_detect_turns.length > 0,
      'settings_loop_detect_turns missing or empty in ' + locale + ' locale');
    assert.ok(typeof bundle.settings_pi_thinking_level === 'string' && bundle.settings_pi_thinking_level.length > 0,
      'settings_pi_thinking_level missing or empty in ' + locale + ' locale');
  }
});
