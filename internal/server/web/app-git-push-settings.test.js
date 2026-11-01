// Pins the Settings > General git push-on-complete fields: the PWA must
// expose git.push_on_complete (select: never / branch / default_branch) and
// git.push_remote (text) on the Sessions card, with a localised label key in
// every locale bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function gitPushFields() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'sess') || {fields: []}).fields.filter(f => f.key === 'git.push_on_complete' || f.key === 'git.push_remote'))",
    sandbox);
  return JSON.parse(json);
}

test('general settings expose git.push_on_complete as a select with the three policies', () => {
  const fields = gitPushFields();
  const field = fields.find(f => f.key === 'git.push_on_complete');
  assert.ok(field, 'git.push_on_complete missing from general settings fields');
  assert.equal(field.type, 'select', 'git.push_on_complete must be a select');
  assert.deepEqual(field.options, ['never', 'branch', 'default_branch'],
    'git.push_on_complete options must be exactly never/branch/default_branch');
  assert.equal(field.labelKey, 'settings_git_push_on_complete');
});

test('general settings expose git.push_remote as a text field', () => {
  const fields = gitPushFields();
  const field = fields.find(f => f.key === 'git.push_remote');
  assert.ok(field, 'git.push_remote missing from general settings fields');
  assert.equal(field.type, 'text', 'git.push_remote must be a text field');
  assert.equal(field.labelKey, 'settings_git_push_remote');
});

test('both git push settings label keys exist in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_git_push_on_complete === 'string' && bundle.settings_git_push_on_complete.length > 0,
      'settings_git_push_on_complete missing or empty in ' + locale + ' locale');
    assert.ok(typeof bundle.settings_git_push_remote === 'string' && bundle.settings_git_push_remote.length > 0,
      'settings_git_push_remote missing or empty in ' + locale + ' locale');
  }
});
